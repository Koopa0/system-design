# Message Queue

輕量級分布式消息隊列系統，基於 NATS JetStream 實現異步任務處理、服務解耦與削峰填谷。

## 設計目標

實作生產級消息隊列系統，展示 **At-least-once 語義**、**消費者組負載均衡**、**消息持久化** 等核心概念。

## 核心功能

- **發布訂閱**：基於 Subject 的消息路由
- **持久化**：JetStream 磁盤存儲，重啟不丟失
- **At-least-once**：發送端 ACK + 接收端手動確認
- **Queue Groups**：自動負載均衡，水平擴展
- **消息順序**：分區內保證順序
- **重試機制**：未 ACK 消息自動重新投遞

## 系統設計

### 架構

```
Publisher                    NATS JetStream              Consumer
┌─────────┐                 ┌─────────────┐            ┌─────────┐
│ Order   │                 │   Stream:   │            │ Worker  │
│ Service │  Publish(msg)   │   ORDERS    │  Subscribe │  Pool   │
│         │────────────────>│             │<───────────│         │
│         │                 │  Storage:   │            │ [ACK]   │
│         │  <───PubAck───  │  File (7d)  │  ──Msg──>  │         │
└─────────┘                 │             │            └─────────┘
                            │  Subjects:  │
Payment                     │  - order.*  │            Inventory
Service ──────────────────> │  - payment.*│<────────── Service
                            └─────────────┘
                                   │
                                   ▼
                            ┌─────────────┐
                            │ Persistent  │
                            │   Storage   │
                            │   (Disk)    │
                            └─────────────┘
```

### 關鍵設計決策

**為何使用 NATS 而非 Redis/Kafka/RabbitMQ？**

- **vs Redis**：NATS 是專業 MQ，提供 ACK、重試、消費者組等完整功能
- **vs Kafka**：NATS 更輕量、延遲更低（<5ms vs ~50ms），適合微服務通訊
- **vs RabbitMQ**：NATS 配置更簡單、性能更高、與 Go 專案語言一致

**Trade-offs**：

優勢：
- 輕量級：單一二進制檔案，Docker 一行啟動
- 高性能：100K+ msg/s 吞吐量、微秒級延遲
- 簡單易用：API 直觀、學習曲線平緩

代價：
- 生態較小：相比 Kafka/RabbitMQ 工具鏈較少
- 可能重複消費：At-least-once 語義，需 Consumer 冪等性

## API

### 發送消息

```http
POST /api/v1/messages
Content-Type: application/json

{
  "subject": "order.created",
  "data": {
    "order_id": "ORD-123",
    "user_id": 456,
    "amount": 99.99
  }
}
```

回應：
```json
{
  "success": true,
  "sequence": 12345,
  "timestamp": "2025-01-15T10:30:00Z"
}
```

### 查詢 Stream 狀態

```http
GET /api/v1/streams/ORDERS
```

回應：
```json
{
  "stream": "ORDERS",
  "messages": 12345,
  "bytes": 12582912,
  "first_seq": 1,
  "last_seq": 12345,
  "consumer_count": 3
}
```

### 查詢 Consumer 狀態

```http
GET /api/v1/consumers/ORDERS/order-processor
```

回應：
```json
{
  "stream": "ORDERS",
  "consumer": "order-processor",
  "num_pending": 42,
  "num_ack_pending": 5,
  "num_redelivered": 2,
  "delivered": {
    "consumer_seq": 100,
    "stream_seq": 12300
  }
}
```

## 使用方式

### 啟動服務

```bash
# 1. 啟動 NATS Server
docker-compose up -d

# 2. 啟動應用服務
go run cmd/server/main.go

# 3. 發送測試消息
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "subject": "order.created",
    "data": {"order_id": "ORD-123", "amount": 99.99}
  }'

# 4. 查看 Consumer 處理日誌
# 觀察終端輸出，應看到消息被處理
```

### 測試 Queue Groups（負載均衡）

```bash
# 終端 1：啟動 Consumer 1
go run cmd/consumer/main.go --group order-processor --id 1

# 終端 2：啟動 Consumer 2
go run cmd/consumer/main.go --group order-processor --id 2

# 終端 3：發送 10 條消息
for i in {1..10}; do
  curl -X POST http://localhost:8080/api/v1/messages \
    -H "Content-Type: application/json" \
    -d "{\"subject\": \"order.created\", \"data\": {\"order_id\": \"ORD-$i\"}}"
done

# 觀察：消息會自動在兩個 Consumer 之間負載均衡
```

### 測試重試機制

```bash
# 1. 啟動 Consumer（模擬處理失敗）
FAIL_RATE=0.5 go run cmd/consumer/main.go

# 2. 發送消息
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"subject": "order.created", "data": {"order_id": "ORD-999"}}'

# 3. 觀察日誌
# 應看到：處理失敗 → NAK → 30 秒後自動重試 → 成功
```

## 測試

```bash
# 單元測試
go test ./internal/... -v

# 整合測試（需 NATS 運行）
docker-compose up -d
go test ./internal/... -v -tags=integration

# 性能測試
go test -bench=. -benchmem ./internal/...
```

## 效能基準

```bash
# 使用 NATS Bench 工具測試
$ nats bench orders --msgs 100000 --size 1024 --pub 10 --sub 10

Pub Stats: 102,345 msgs/sec ~ 100 MB/sec
Sub Stats: 101,234 msgs/sec ~ 99 MB/sec
```

結果（單機 NATS）：
- 吞吐量：100K+ msg/s
- 延遲：P50=1.2ms、P99=4.8ms
- CPU：~30%（4 核心）
- 記憶體：~200 MB

## 擴展性

### 水平擴展（Consumer）

```bash
# 增加 Consumer 數量，吞吐量線性增長
# 1 Consumer → 10K msg/s
# 5 Consumers → 50K msg/s
# 10 Consumers → 100K msg/s

# 使用 Queue Groups 自動負載均衡
docker-compose up --scale consumer=5
```

### 垂直擴展（Server）

當單機達到瓶頸（>100K msg/s）：
- 升級 CPU/記憶體：2 核 4GB → 4 核 8GB
- 使用 SSD：提升磁盤 I/O

### 叢集擴展（高可用）

詳見 `DESIGN.md` → 100x 擴展：
- JetStream 叢集（3 節點 Raft）
- 跨區域部署（Super Cluster）
- 可達 1M+ msg/s

## 已知限制

1. **At-least-once 語義**
   - 可能重複消費（網絡重試、Consumer 崩潰）
   - 需 Consumer 實現冪等性（如數據庫唯一約束）

2. **無消息優先級**
   - NATS 不支持消息優先級
   - 若需要可用多個 Stream + 優先級調度

3. **有限的消息順序保證**
   - 全局順序：需單 Consumer（無法擴展）
   - 分區順序：相同鍵的消息保證順序

4. **存儲限制**
   - 受磁盤容量限制
   - 需配置消息過期時間（如 7 天）

5. **教學簡化**
   - 單機部署（生產環境應用叢集）
   - 無 TLS 加密（生產環境必須啟用）

## 實作細節

詳見程式碼註解：
- `internal/queue.go` - NATS JetStream 核心實現、Stream 管理
- `internal/handler.go` - HTTP API 處理
- `cmd/server/main.go` - 服務啟動、Publisher 範例
- `cmd/consumer/main.go` - Consumer 實現、Queue Groups、手動 ACK

完整設計文檔請參考 [DESIGN.md](./DESIGN.md)。
