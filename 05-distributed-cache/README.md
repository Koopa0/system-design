# Distributed Cache

分散式快取系統，展示多種淘汰演算法、一致性雜湊、快取策略等核心概念。

## 設計目標

實作生產級快取系統，涵蓋從本地快取到分散式快取的完整演進過程。

## 核心功能

- LRU 淘汰演算法（Least Recently Used）
- LFU 淘汰演算法（Least Frequently Used）
- 一致性雜湊（Consistent Hashing）
- Cache-Aside 策略
- Write-Through 策略
- Write-Back 策略
- 分散式快取協調
- 副本機制

## 問題定義

快取是提升系統效能的關鍵技術，但也帶來諸多挑戰：

### 挑戰一：淘汰策略

當快取滿時，應該淘汰哪些資料？
- LRU：淘汰最久未使用
- LFU：淘汰使用頻率最低

### 挑戰二：分散式擴展

單機快取容量有限，如何擴展到多節點？
- 傳統雜湊：節點增減導致大量資料重新分配
- 一致性雜湊：只影響少部分資料

### 挑戰三：快取更新

如何保證快取與資料庫的一致性？
- Cache-Aside：應用程式控制
- Write-Through：同步更新
- Write-Back：非同步批量更新

## 系統設計

### 架構演進

#### 單機快取

```
Application → Local Cache (LRU/LFU)
```

優點：
- 簡單直接
- 效能極高

缺點：
- 容量受限
- 無法共享

#### 分散式快取

```
Application → Load Balancer → Cache Nodes (with Consistent Hashing)
```

優點：
- 容量可擴展
- 多節點共享
- 高可用性

缺點：
- 網路延遲
- 一致性問題

### 淘汰演算法對比

| 演算法 | 優點 | 缺點 | 適用場景 |
|-------|------|------|---------|
| LRU | 實作簡單，效能好 | 無法防止快取污染 | 一般場景 |
| LFU | 防止快取污染，保護熱點 | 實作複雜，冷啟動問題 | 穩定存取模式 |

### LRU 實作

資料結構：
- 雙向鏈結串列：維護存取順序
- HashMap：快速查找

時間複雜度：O(1)

```go
type LRU struct {
    capacity int
    cache    map[string]*list.Element   // key -> 鏈表節點
    list     *list.List                 // 雙向鏈結串列
}
```

### LFU 實作

資料結構：
- HashMap：key -> 節點資訊
- 頻率桶：每個頻率對應一個 LRU 鏈表
- minFreq：追蹤最小頻率

時間複雜度：O(1)

```go
type LFU struct {
    cache    map[string]*lfuNode
    freqMap  map[int]*list.List  // 頻率 -> LRU 鏈表
    minFreq  int
}
```

### 一致性雜湊

解決問題：
- 傳統雜湊：`hash(key) % N`
- 節點增減時，大部分資料需要重新分配

一致性雜湊：
- 雜湊環（0 到 2^32-1）
- 虛擬節點（解決資料分布不均）
- 節點增減只影響 1/N 的資料

```
實體節點：[node1, node2, node3]
虛擬節點（150 個/節點）：
  node1-0, node1-1, ..., node1-149
  node2-0, node2-1, ..., node2-149
  node3-0, node3-1, ..., node3-149
```

### 快取策略對比

| 策略 | 讀取 | 寫入 | 一致性 | 複雜度 |
|------|-----|------|--------|--------|
| Cache-Aside | 快取 → DB | 刪快取 → 更新DB | 最終一致 | 簡單 |
| Write-Through | 快取 → DB | 同步更新 | 強一致 | 中等 |
| Write-Back | 快取 | 只寫快取 | 弱一致 | 複雜 |

#### Cache-Aside（推薦）

讀取流程：
1. 查詢快取
2. 快取命中：返回
3. 快取未命中：查 DB → 寫快取

寫入流程：
1. 刪除快取
2. 更新資料庫

優點：
- 簡單易用
- 最常用策略

缺點：
- 需要應用程式處理快取邏輯

#### Write-Through

讀取流程：與 Cache-Aside 相同

寫入流程：
1. 更新資料庫
2. 更新快取

優點：
- 一致性好
- 應用程式邏輯簡單

缺點：
- 寫入延遲高

#### Write-Back

讀取流程：與 Cache-Aside 相同

寫入流程：
1. 更新快取
2. 標記為髒資料
3. 非同步批量寫入資料庫

優點：
- 寫入效能極高

缺點：
- 資料可能遺失
- 一致性最弱

## 使用方式

### 執行服務

```bash
# 1. 執行服務（展示各種快取）
go run cmd/server/main.go

# 或使用 Makefile
make run
```

輸出示範：
```
=== LRU 快取示範 ===
已寫入 3 筆資料，當前快取：[c b a]
存取 'a' 後，當前快取：[a c b]
寫入 'd' 後，當前快取：[d a c] (淘汰了 'b')

=== LFU 快取示範 ===
當前快取統計：大小=3, 最小頻率=1, 頻率分布=map[1:2 4:1]
寫入 'd' 後，頻率分布=map[1:2 2:1 4:1] (淘汰了頻率最低的 'c')

=== 分散式快取示範 ===
資料分布：
  node1: 2 筆資料
  node2: 1 筆資料
  node3: 2 筆資料
```

### 程式碼範例

#### LRU 快取

```go
lru := cache.NewLRU(100)  // 容量 100

lru.Put("key1", "value1")
value, ok := lru.Get("key1")
```

#### LFU 快取

```go
lfu := cache.NewLFU(100)

lfu.Put("key1", "value1")
value, ok := lfu.Get("key1")

// 查看統計
stats := lfu.GetStats()
fmt.Printf("頻率分布：%v\n", stats.FreqDist)
```

#### 分散式快取

```go
nodes := []string{"node1", "node2", "node3"}
dc := cache.NewDistributedCache(nodes, func() cache.Cache {
    return cache.NewLRU(1000)
})

dc.Set("user:1001", userData)
value, ok := dc.Get("user:1001")

// 動態新增節點
dc.AddNode("node4", func() cache.Cache {
    return cache.NewLRU(1000)
})
```

#### Cache-Aside 策略

```go
lru := cache.NewLRU(1000)
store := NewDataStore()  // 實作 DataStore 介面

aside := strategy.NewCacheAside(lru, store)

// 讀取（自動處理快取邏輯）
value, err := aside.Get(ctx, "user:1001")

// 寫入（先刪快取，再更新 DB）
err := aside.Set(ctx, "user:1001", userData)
```

## 測試

### 單元測試

```bash
go test -v ./...
```

### 並發測試

```bash
go test -v -race ./internal/cache
```

測試場景：
- LRU 淘汰正確性
- LFU 頻率計算正確性
- 一致性雜湊資料分布
- 併發安全性

## 效能基準

### LRU 效能

| 操作 | 時間複雜度 | 實際效能 |
|------|-----------|---------|
| Get | O(1) | <100ns |
| Put | O(1) | <100ns |
| 記憶體 | O(n) | 約 100 bytes/項 |

### LFU 效能

| 操作 | 時間複雜度 | 實際效能 |
|------|-----------|---------|
| Get | O(1) | <200ns |
| Put | O(1) | <200ns |
| 記憶體 | O(n) | 約 150 bytes/項 |

### 一致性雜湊

| 操作 | 時間複雜度 | 實際效能 |
|------|-----------|---------|
| Get | O(log N) | <500ns |
| Add Node | O(N log N) | - |
| Remove Node | O(N log N) | - |

## 擴展性

### 從單機到分散式

**單機快取（<10GB 資料）**：
- LRU 或 LFU
- 適合大部分場景

**分散式快取（10GB-1TB 資料）**：
- 3-10 個節點
- 使用一致性雜湊
- 考慮副本機制

**大規模分散式（>1TB 資料）**：
- 10+ 個節點
- 分層快取（L1 本地 + L2 遠端）
- 讀寫分離

### 副本策略

```go
dc := cache.NewDistributedCacheWithReplication(
    nodes,
    func() cache.Cache { return cache.NewLRU(1000) },
    3,  // 3 個副本
)
```

優點：
- 高可用：節點失敗不影響服務
- 讀取效能：可從任意副本讀取

缺點：
- 儲存空間：N 倍副本
- 一致性：副本間可能不一致

## 監控指標

建議監控：
- 快取命中率
- 淘汰率
- 記憶體使用量
- 節點資料分布
- 副本一致性

## 已知限制

1. **記憶體限制**：單機快取受限於記憶體大小
2. **網路延遲**：分散式快取有網路開銷（1-2ms）
3. **一致性問題**：快取與資料庫可能短暫不一致
4. **資料遷移**：節點增減時需處理資料遷移

## 快取問題與解決

### 快取穿透

問題：查詢不存在的 key，導致每次都查詢資料庫

解決：
1. 快取空值（TTL 較短）
2. Bloom Filter 預先過濾

### 快取雪崩

問題：大量快取同時過期，導致資料庫壓力暴增

解決：
1. 隨機 TTL（避免同時過期）
2. 永不過期 + 非同步更新

### 快取擊穿

問題：熱點資料過期瞬間，大量請求打到資料庫

解決：
1. 分散式鎖（只有一個請求查詢資料庫）
2. 永不過期

## 實作細節

詳見程式碼註解：
- `internal/cache/lru.go` - LRU 演算法
- `internal/cache/lfu.go` - LFU 演算法
- `pkg/consistent/consistent.go` - 一致性雜湊
- `internal/strategy/aside.go` - Cache-Aside 策略
- `internal/cache/distributed.go` - 分散式快取
