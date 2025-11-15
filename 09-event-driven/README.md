# Event-Driven Architecture

事件驅動架構系統，基於 NATS JetStream 實現 Event Sourcing、CQRS 與 Saga 模式。

## 設計目標

實作事件驅動架構，展示**Event Sourcing**、**CQRS 讀寫分離**、**Saga 分布式事務**等核心模式。

## 核心功能

- **Event Sourcing**：所有狀態變更記錄為事件
- **CQRS**：命令端（寫）與查詢端（讀）分離
- **Saga 模式**：事件驅動的分布式事務協調
- **事件重播**：從事件流重建系統狀態
- **最終一致性**：異步事件處理
- **完整審計**：事件永久保存，可追溯

## 系統設計

### Event Sourcing

```
傳統方式（只存儲狀態）：
  orders表: {id: 123, status: 'completed'}
  ❌ 無法知道如何到達這個狀態

Event Sourcing（存儲事件）：
  events: [
    OrderCreated{id:123, amount:99},
    OrderPaid{id:123},
    OrderShipped{id:123},
    OrderCompleted{id:123}
  ]
  ✅ 完整歷史，可重播
```

### CQRS 架構

```
Write Side                Event Store              Read Side
┌──────────┐            ┌─────────────┐          ┌──────────┐
│ Command  │   Events   │    NATS     │ Subscribe│  Read    │
│ Handler  │───────────>│ JetStream   │─────────>│  Model   │
│          │            │ (append-only)│          │(Postgres)│
└──────────┘            └─────────────┘          └──────────┘
     │                                                  │
     │ 優化寫入（快速追加）                             │ 優化查詢（索引、JOIN）
```

## API

### 命令端（寫）

```http
POST /api/v1/orders
Content-Type: application/json

{
  "user_id": 123,
  "items": [{"product_id": 1, "quantity": 2}],
  "amount": 199.98
}
```

### 查詢端（讀）

```http
GET /api/v1/orders/123
```

回應（從 Read Model 查詢）：
```json
{
  "order_id": 123,
  "status": "completed",
  "events": [
    {"type": "OrderCreated", "timestamp": "2025-01-15T10:00:00Z"},
    {"type": "OrderPaid", "timestamp": "2025-01-15T10:05:00Z"},
    {"type": "OrderCompleted", "timestamp": "2025-01-15T11:00:00Z"}
  ]
}
```

## 使用方式

### 啟動服務

```bash
# 1. 啟動依賴服務
docker-compose up -d

# 2. 啟動服務
go run cmd/server/main.go

# 3. 創建訂單（產生事件）
curl -X POST http://localhost:8082/api/v1/orders \
  -H "Content-Type: application/json" \
  -d '{"user_id": 123, "amount": 99.99}'

# 4. 查詢訂單（從 Read Model）
curl http://localhost:8082/api/v1/orders/1
```

## 已知限制

1. **最終一致性**
   - 寫入事件後查詢可能看到舊數據（10-50ms 延遲）

2. **事件版本**
   - 當前未實現事件 Upcasting
   - 事件結構變更需額外處理

3. **快照機制**
   - 未實現快照，大量事件重放較慢
   - 生產環境建議每 100 個事件快照一次

## 實作細節

詳見程式碼註解：
- `internal/eventstore.go` - NATS JetStream 事件存儲
- `internal/aggregate.go` - 聚合根與事件應用
- `internal/projection.go` - CQRS 投影到讀模型
- `internal/saga.go` - Saga 模式實現

完整設計文檔請參考 [DESIGN.md](./DESIGN.md)。
