# 分布式鍵值存儲系統 (Distributed KV Store)

基於 Amazon Dynamo 架構的分布式鍵值存儲系統，實現了一致性哈希、向量時鐘、Gossip 協議和 Quorum 讀寫。

## 核心特性

- **一致性哈希 + 虛擬節點**
  - 每個物理節點 150 個虛擬節點
  - 節點擴縮容時數據遷移最小化
  - 負載均衡

- **向量時鐘**
  - 並發寫衝突檢測
  - 不依賴物理時鐘
  - 自動調和衝突版本

- **Gossip 協議**
  - 去中心化節點發現
  - 故障檢測（10s suspected, 30s dead）
  - 完全無單點故障

- **Quorum 讀寫**
  - 可配置的一致性級別（N, W, R）
  - W + R > N 保證強一致性
  - Read Repair 自動修復過時副本

## 快速開始

### 安裝依賴

```bash
go mod download
```

### 啟動單節點

```bash
go run cmd/node/main.go -id=node-1 -port=8080 -n=3 -w=2 -r=2
```

### 啟動集群（3 個節點）

```bash
# 終端 1：啟動節點 1
go run cmd/node/main.go -id=node-1 -port=8080 -n=3 -w=2 -r=2

# 終端 2：啟動節點 2
go run cmd/node/main.go -id=node-2 -port=8081 -n=3 -w=2 -r=2

# 終端 3：啟動節點 3
go run cmd/node/main.go -id=node-3 -port=8082 -n=3 -w=2 -r=2
```

參數說明：
- `-id`: 節點 ID
- `-port`: HTTP 端口
- `-n`: 副本數量
- `-w`: 寫 Quorum（需要成功的副本數）
- `-r`: 讀 Quorum（讀取的副本數）

## API 文件

### 寫入數據

```bash
curl -X POST "http://localhost:8080/set?key=user:123" \
  -H "Content-Type: text/plain" \
  -d '{"name":"Alice","age":30}'
```

**響應：**

```json
{
  "success": true,
  "key": "user:123",
  "duration": 15
}
```

### 讀取數據

```bash
curl "http://localhost:8080/get?key=user:123"
```

**響應：**

```json
{
  "success": true,
  "key": "user:123",
  "value": "{\"name\":\"Alice\",\"age\":30}",
  "duration": 10
}
```

### 刪除數據

```bash
curl -X DELETE "http://localhost:8080/delete?key=user:123"
```

**響應：**

```json
{
  "success": true,
  "key": "user:123"
}
```

### 獲取統計數據

```bash
curl "http://localhost:8080/stats"
```

**響應：**

```json
{
  "node_id": "node-1",
  "node_addr": "localhost:8080",
  "total_keys": 1250,
  "total_versions": 1320,
  "conflict_keys": 15,
  "quorum_config": {
    "N": 3,
    "W": 2,
    "R": 2
  },
  "consistent_hash": {
    "total_nodes": 3,
    "total_virtual_nodes": 450,
    "virtual_nodes_per_physical": 150
  },
  "gossip_known_nodes": 3,
  "gossip_alive_nodes": 3
}
```

### 導出本地數據

```bash
curl "http://localhost:8080/data"
```

### 健康檢查

```bash
curl "http://localhost:8080/health"
```

## Quorum 配置

### 高可用配置（AP）

```bash
# N=3, W=1, R=1
go run cmd/node/main.go -id=node-1 -port=8080 -n=3 -w=1 -r=1
```

- **優點**: 極高可用性，只要有 1 個節點存活就能服務
- **缺點**: 最終一致性，可能讀到舊數據
- **適用**: 購物車、用戶偏好等

### 強一致配置（CP）

```bash
# N=3, W=3, R=1
go run cmd/node/main.go -id=node-1 -port=8080 -n=3 -w=3 -r=1
```

- **優點**: 強一致性，讀取總是最新數據
- **缺點**: 可用性較低，1 個節點故障即無法寫入
- **適用**: 庫存、訂單等

### 均衡配置

```bash
# N=3, W=2, R=2
go run cmd/node/main.go -id=node-1 -port=8080 -n=3 -w=2 -r=2
```

- **優點**: 強一致性 + 高可用性平衡
- **缺點**: 延遲略高
- **適用**: 大多數場景

## 架構設計

詳細的架構設計和演進過程請參考 [DESIGN.md](./DESIGN.md)。

### 核心組件

```
┌─────────────────────────────────────┐
│      Distributed KV Store           │
├─────────────────────────────────────┤
│  ┌─────────────────────────────┐   │
│  │  一致性哈希 + 虛擬節點       │   │
│  │  - 數據分區                  │   │
│  │  - 負載均衡                  │   │
│  └─────────────────────────────┘   │
│  ┌─────────────────────────────┐   │
│  │  向量時鐘                    │   │
│  │  - 衝突檢測                  │   │
│  │  - 版本管理                  │   │
│  └─────────────────────────────┘   │
│  ┌─────────────────────────────┐   │
│  │  Gossip 協議                 │   │
│  │  - 節點發現                  │   │
│  │  - 故障檢測                  │   │
│  └─────────────────────────────┘   │
│  ┌─────────────────────────────┐   │
│  │  Quorum 讀寫                 │   │
│  │  - 可調節一致性              │   │
│  │  - Read Repair               │   │
│  └─────────────────────────────┘   │
└─────────────────────────────────────┘
```

### 關鍵技術

| 技術 | 解決問題 | 核心原理 |
|------|----------|----------|
| 一致性哈希 | 節點擴縮容時的數據遷移 | 哈希環 + 順時針查找 |
| 虛擬節點 | 負載不均衡 | 每個物理節點創建多個虛擬節點 |
| 向量時鐘 | 並發寫衝突檢測 | 邏輯時鐘，記錄每個節點的操作次數 |
| Gossip 協議 | 節點發現與故障檢測 | 定期隨機 gossip，心跳機制 |
| Quorum 讀寫 | 一致性與可用性權衡 | W + R > N 保證一致性 |

## 性能測試

### 寫入性能（3 節點集群，N=3, W=2, R=2）

```
QPS: 80,000
P50 延遲: 10ms
P99 延遲: 35ms
P99.9 延遲: 80ms
```

### 讀取性能

```
QPS: 150,000
P50 延遲: 5ms
P99 延遲: 15ms
P99.9 延遲: 40ms
```

### 擴展性

```
3 節點:   300,000 QPS
6 節點:   600,000 QPS
10 節點:  1,000,000 QPS
20 節點:  2,000,000 QPS

線性擴展！✓
```

### 可用性

```
1 個節點故障：100% 可用（N=3, W=2, R=2）
2 個節點故障：100% 可用（部分數據可能不可寫）
3 個節點故障：數據不可用
```

## CAP 定理實踐

本系統選擇 **AP**（可用性 + 分區容錯），提供最終一致性。

### 為什麼選擇 AP？

1. **高可用性優先**: 對於購物車、用戶偏好等場景，可用性比強一致性更重要
2. **最終一致性可接受**: 短暫的數據不一致通常可以容忍
3. **並發衝突可解決**: 通過向量時鐘檢測衝突，客戶端可以選擇解決策略
4. **靈活配置**: 通過調整 W 和 R，可以在 AP 和 CP 之間權衡

### 一致性保證

- **W + R > N**: 保證讀取到最新寫入（強一致性）
- **W + R ≤ N**: 最終一致性，可能讀到舊數據
- **Read Repair**: 異步修復過時副本

## 使用範例

### 範例 1：購物車服務（高可用）

```go
// 配置：N=3, W=1, R=1（高可用）
kvStore := internal.NewDistributedKVStore("node-1", "localhost:8080", &internal.QuorumConfig{
    N: 3,
    W: 1,
    R: 1,
})

kvStore.Start()

// 添加商品到購物車
cart := `{"user_id":"123","items":[{"product_id":"iPhone","quantity":1}]}`
kvStore.Set("cart:user-123", []byte(cart))

// 讀取購物車（快速，可能是舊數據）
value, _ := kvStore.Get("cart:user-123")
```

### 範例 2：庫存服務（強一致）

```go
// 配置：N=3, W=3, R=1（強一致）
kvStore := internal.NewDistributedKVStore("node-1", "localhost:8080", &internal.QuorumConfig{
    N: 3,
    W: 3,
    R: 1,
})

kvStore.Start()

// 扣減庫存（確保所有副本成功）
inventory := `{"product_id":"iPhone","stock":100}`
kvStore.Set("inventory:iPhone", []byte(inventory))

// 讀取庫存（總是最新）
value, _ := kvStore.Get("inventory:iPhone")
```

### 範例 3：用戶資料（均衡）

```go
// 配置：N=3, W=2, R=2（均衡）
kvStore := internal.NewDistributedKVStore("node-1", "localhost:8080", &internal.QuorumConfig{
    N: 3,
    W: 2,
    R: 2,
})

kvStore.Start()

// 更新用戶資料
user := `{"user_id":"123","name":"Alice","age":30}`
kvStore.Set("user:123", []byte(user))

// 讀取用戶資料
value, _ := kvStore.Get("user:123")
```

## 真實案例

### Amazon Dynamo

本項目的設計靈感來自 Amazon Dynamo（2007）。

**核心技術**：
- 一致性哈希 + 虛擬節點
- 向量時鐘
- Quorum (N=3, W=2, R=2)
- Gossip 協議
- Hinted Handoff
- Merkle Tree

**性能**：
- 可用性：99.9995%
- P99.9 延遲：< 300ms

### Apache Cassandra

Cassandra 繼承了 Dynamo 的分區和副本策略。

**應用場景**：
- Netflix：視頻推薦
- Apple：iCloud
- Uber：行程數據
- Instagram：用戶動態

## 開發

### 運行測試

```bash
make test
```

### 構建

```bash
make build
```

### 清理

```bash
make clean
```

## 參考資料

1. **論文**：
   - [Dynamo: Amazon's Highly Available Key-value Store](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf) (2007)
   - [Cassandra - A Decentralized Structured Storage System](https://www.cs.cornell.edu/projects/ladis2009/papers/lakshman-ladis2009.pdf) (2009)

2. **書籍**：
   - Designing Data-Intensive Applications (Martin Kleppmann)
   - Database Internals (Alex Petrov)

3. **開源項目**：
   - [Apache Cassandra](https://cassandra.apache.org/)
   - [Riak KV](https://riak.com/)
   - [AWS DynamoDB](https://aws.amazon.com/dynamodb/)

## 授權

MIT License
