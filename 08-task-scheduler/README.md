# Task Scheduler

分布式任務調度系統，基於時間輪算法 + NATS JetStream 實現延遲任務、定時任務與週期任務調度。

## 設計目標

實作生產級任務調度系統，展示**時間輪算法**、**延遲隊列**、**Cron 表達式解析**等核心概念。

## 核心功能

- **延遲任務**：30 分鐘後執行（訂單超時取消）
- **定時任務**：指定時間執行（每日凌晨報表）
- **週期任務**：Cron 表達式（每小時同步）
- **重試機制**：指數退避 + 死信隊列
- **分布式調度**：Queue Groups 避免重複執行
- **持久化**：NATS JetStream 重啟不丟失

## 系統設計

### 時間輪算法

```
Timing Wheel (3600 槽位 = 1 小時)

Slot 0  ──> [Task A, Task B]
Slot 1  ──> []
Slot 2  ──> [Task C]
...
Slot 30 ──> [Task D] ← 30 秒後執行
...
Slot 3599 ──> [Task E]
         ↑
      當前指針

每秒轉動一格：
T=0  → 執行 Slot 0 的任務
T=1  → 執行 Slot 1 的任務
T=30 → 執行 Slot 30 的任務（Task D）
```

**O(1) 性能：**
- 插入任務：計算槽位 `slot = (current + delay) % 3600`
- 觸發任務：只檢查當前槽位

### 架構

```
Client                  NATS JetStream           Worker (時間輪)
┌─────────┐            ┌─────────────┐          ┌──────────────┐
│ Order   │  AddTask   │   Stream:   │          │  ┌─────────┐ │
│ Service │───────────>│  SCHEDULED  │Subscribe │  │ Wheel   │ │
│         │            │   _TASKS    │<─────────│  │ ┌──┐┌──┐│ │
│         │            │             │          │  │ │S0││S1││ │
└─────────┘            │ (持久化7天)  │          │  │ └──┘└──┘│ │
                       └─────────────┘          │  └────┬────┘ │
                                                │       │Tick  │
                                                │       ▼      │
                                                │   Execute    │
                                                └──────────────┘
```

### 關鍵設計決策

**為何使用時間輪算法？**

- **vs 資料庫輪詢**：O(1) vs O(N) 掃描，性能提升 100x+
- **vs sleep + goroutine**：記憶體高效（不需每個任務一個 goroutine）
- **vs 優先級隊列**：插入更快（O(1) vs O(log N)）

**Trade-offs：**

優勢：
- 極高性能：O(1) 插入與觸發
- 記憶體高效：任務分散在槽位中
- 精度可控：槽位數 = 精度（3600 槽位 = 秒級）

代價：
- 長延遲任務需多圈計數
- 槽位數量影響記憶體（3600 槽位 ≈ 數百 KB）

## API

### 添加延遲任務

```http
POST /api/v1/tasks/delay
Content-Type: application/json

{
  "delay_seconds": 1800,
  "callback_url": "http://order-service/api/timeout",
  "data": {
    "order_id": "ORD-123",
    "user_id": 456
  }
}
```

回應：
```json
{
  "task_id": "task-abc-123",
  "execute_at": "2025-01-15T11:00:00Z",
  "status": "scheduled"
}
```

### 添加定時任務（Cron）

```http
POST /api/v1/tasks/cron
Content-Type: application/json

{
  "cron": "0 0 2 * * *",
  "callback_url": "http://report-service/api/generate",
  "data": {
    "report_type": "daily_sales"
  }
}
```

### 查詢任務狀態

```http
GET /api/v1/tasks/{task_id}
```

回應：
```json
{
  "task_id": "task-abc-123",
  "status": "pending",
  "execute_at": "2025-01-15T11:00:00Z",
  "retry_count": 0,
  "created_at": "2025-01-15T10:30:00Z"
}
```

## 使用方式

### 啟動服務

```bash
# 1. 啟動 NATS Server
docker-compose up -d

# 2. 啟動 Scheduler Worker
go run cmd/server/main.go

# 3. 添加測試任務（30 秒後執行）
curl -X POST http://localhost:8081/api/v1/tasks/delay \
  -H "Content-Type: application/json" \
  -d '{
    "delay_seconds": 30,
    "callback_url": "http://httpbin.org/post",
    "data": {"order_id": "ORD-123"}
  }'

# 4. 觀察日誌（30 秒後應看到任務執行）
```

### 測試定時任務

```bash
# 每分鐘執行一次
curl -X POST http://localhost:8081/api/v1/tasks/cron \
  -H "Content-Type: application/json" \
  -d '{
    "cron": "0 * * * * *",
    "callback_url": "http://httpbin.org/post",
    "data": {"task": "minutely"}
  }'
```

## 效能基準

```
時間輪性能測試：

添加 100K 任務：
- 耗時：120ms
- 平均：833K tasks/s
- 記憶體：~50 MB

觸發任務：
- 每秒檢查一個槽位：O(1)
- 延遲：<1ms
```

## 擴展性

### 水平擴展

```bash
# 啟動多個 Worker（Queue Groups 自動負載均衡）
docker-compose up --scale worker=3

# 每個 Worker 獨立時間輪，處理 1/3 的任務
```

### 容量估算

- 10K 任務：單機足夠
- 100K 任務：3-5 個 Worker
- 1M 任務：10 個 Worker + NATS 叢集

## 已知限制

1. **精度限制**
   - 當前：秒級精度（3600 槽位）
   - 若需毫秒級：需增加槽位數或改用其他算法

2. **長延遲任務**
   - 超過 1 小時：需多圈計數
   - 生產環境建議：分層時間輪（秒/分/時/天）

3. **Cron 語法**
   - 當前：簡化版（教學用）
   - 生產環境：使用 `github.com/robfig/cron`

4. **任務冪等性**
   - Worker 重啟可能導致任務重複執行
   - 需業務層保證冪等性

## 實作細節

詳見程式碼註解：
- `internal/wheel.go` - 時間輪算法核心實現
- `internal/scheduler.go` - 任務調度器（NATS 整合）
- `internal/executor.go` - 任務執行器（HTTP 回調、重試）
- `internal/cron.go` - Cron 表達式解析（簡化版）

完整設計文檔請參考 [DESIGN.md](./DESIGN.md)。
