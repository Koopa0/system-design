# Task Scheduler 系統設計文檔

## 情境：電商平台的訂單超時危機

### 第一天：簡單的訂單超時處理

2024 年 7 月 1 日，週一上午 9:00

你是一家電商平台的後端工程師王強。產品經理提出新需求：

「用戶下單後如果 30 分鐘內不支付，要自動取消訂單並釋放庫存。」

你想了想，這很簡單，用資料庫輪詢就能實現：

**實作方案（資料庫輪詢）：**
```sql
CREATE TABLE orders (
    id BIGINT PRIMARY KEY,
    user_id BIGINT,
    status VARCHAR(20),  -- 'pending', 'paid', 'cancelled'
    created_at TIMESTAMP,
    INDEX idx_status_created (status, created_at)
);
```

**定時任務（每分鐘執行）：**
```go
func CancelTimeoutOrders() {
    // 查找 30 分鐘前創建且未支付的訂單
    orders := db.Query(`
        SELECT id FROM orders
        WHERE status = 'pending'
          AND created_at < NOW() - INTERVAL 30 MINUTE
    `)

    for _, order := range orders {
        // 取消訂單
        db.Update("UPDATE orders SET status = 'cancelled' WHERE id = ?", order.ID)

        // 釋放庫存
        inventoryService.Release(order.ID)

        log.Info("訂單已自動取消", "order_id", order.ID)
    }
}

// Cron: 每分鐘執行一次
// * * * * * CancelTimeoutOrders()
```

**效能指標：**
```
訂單量：每天 1,000 筆
查詢頻率：每分鐘 1 次
資料庫負載：極低
精度：1 分鐘（可接受）

→ 簡單可靠 ✅
```

產品經理很滿意，系統順利上線。

---

### 第 30 天：大促活動的災難

2024 年 7 月 30 日，週二下午 2:00

雙倍積分大促活動開始，訂單量從每天 1,000 筆暴增到每分鐘 5,000 筆。

下午 3:00，DBA 緊急告警：**資料庫 CPU 飆升到 95%！**

你查看慢查詢日誌，發現超時檢查查詢執行時間異常：

```sql
SELECT id FROM orders
WHERE status = 'pending'
  AND created_at < NOW() - INTERVAL 30 MINUTE

執行時間：
- 之前：10ms（1,000 筆訂單）
- 現在：5 秒！（100 萬筆待處理訂單）
```

**問題分析：**
```
訂單堆積：
- 每分鐘新增：5,000 筆
- 30 分鐘窗口：5,000 × 30 = 15 萬筆待檢查訂單
- 索引掃描：即使有索引，掃描 15 萬筆仍需時間

查詢頻率：
- 每分鐘執行 1 次
- 每天執行：1,440 次
- 每次掃描：15 萬筆

資料庫負載：
- 1,440 × 15 萬 = 2.16 億次行掃描/天
- CPU 無法承受 ❌
```

更嚴重的問題：輪詢間隔 1 分鐘，訂單取消會延遲 0-60 秒。

技術總監：「資料庫輪詢不可行，我們需要更高效的方案。」

---

### 第一次嘗試：每個訂單一個 Goroutine

2024 年 8 月 1 日，週三上午 10:00

你想：「既然輪詢效率低，那就讓每個訂單自己倒數計時！」

**實作方案：**
```go
func CreateOrder(order *Order) error {
    // 1. 寫入資料庫
    db.Insert(order)

    // 2. 啟動 goroutine 倒數計時
    go func(orderID int64) {
        // 等待 30 分鐘
        time.Sleep(30 * time.Minute)

        // 檢查訂單狀態
        order := db.QueryOne("SELECT status FROM orders WHERE id = ?", orderID)
        if order.Status == "pending" {
            // 仍未支付，取消訂單
            db.Update("UPDATE orders SET status = 'cancelled' WHERE id = ?", orderID)
            inventoryService.Release(orderID)
            log.Info("訂單已自動取消", "order_id", orderID)
        }
    }(order.ID)

    return nil
}
```

**測試結果：**
```
優勢：
- 精確：恰好 30 分鐘後執行
- 無輪詢：不消耗資料庫資源
- 簡單：程式碼清晰

第一天看起來很好 ✅
```

---

### 災難場景：記憶體爆炸

2024 年 8 月 3 日，週五下午 4:30

系統運行兩天後，監控告警：**應用伺服器記憶體使用 8 GB，持續上升！**

你緊急排查，發現可怕的真相：

**記憶體佔用計算：**
```
每個 goroutine 記憶體佔用：
- Stack 初始大小：2 KB
- 包含 order ID、callback 等：約 2 KB

大促期間訂單量：
- 每分鐘：5,000 筆
- 30 分鐘窗口：5,000 × 30 = 15 萬筆訂單
- 15 萬個 goroutine 同時存活

記憶體佔用：
15 萬 × 2 KB = 300 MB（最小估計）

實際更糟：
- 每個 goroutine 的 closure 佔用額外記憶體
- 垃圾回收壓力大
- 實際佔用：約 2 GB

如果訂單量再增加 10 倍呢？
150 萬 goroutine = 20 GB 記憶體 ← 伺服器會 OOM！
```

**更嚴重的問題：不可靠**
```
問題 1：進程重啟
- 部署新版本 → 進程重啟
- 15 萬個 goroutine 全部消失
- 訂單永遠不會被取消 ← 庫存永遠鎖住！

問題 2：無法分散式
- 任務綁定在單一進程
- 無法水平擴展
- 無法容錯

問題 3：無法持久化
- 任務在記憶體中
- 無法追蹤、無法查詢
- 無法人工介入
```

你只好緊急回滾到輪詢方案，重新思考架構。

---

### 第二次嘗試：優先級佇列

2024 年 8 月 5 日，週日上午 11:00

架構師建議：「用優先級佇列（Min-Heap），按執行時間排序，最早的在堆頂。」

**實作方案：**
```go
type Task struct {
    OrderID   int64
    ExecuteAt time.Time
}

type TaskHeap []Task

// 實作 heap.Interface
func (h TaskHeap) Less(i, j int) bool {
    return h[i].ExecuteAt.Before(h[j].ExecuteAt)
}

type Scheduler struct {
    heap TaskHeap
    mu   sync.Mutex
}

func (s *Scheduler) AddTask(orderID int64, delay time.Duration) {
    s.mu.Lock()
    defer s.mu.Unlock()

    task := Task{
        OrderID:   orderID,
        ExecuteAt: time.Now().Add(delay),
    }
    heap.Push(&s.heap, task)
}

func (s *Scheduler) Run() {
    ticker := time.NewTicker(100 * time.Millisecond)
    for range ticker.C {
        s.mu.Lock()
        now := time.Now()

        // 檢查堆頂任務
        for s.heap.Len() > 0 {
            task := s.heap[0]
            if task.ExecuteAt.After(now) {
                break // 堆頂任務還沒到時間
            }

            // 執行任務
            heap.Pop(&s.heap)
            s.mu.Unlock()
            s.executeTask(task)
            s.mu.Lock()
        }
        s.mu.Unlock()
    }
}
```

**效能改善：**
```
優勢：
- 插入任務：O(log N)
- 檢查堆頂：O(1)
- 記憶體可控：只儲存任務資料結構

記憶體佔用：
15 萬任務 × 32 bytes（Task 結構）= 4.8 MB ← 可接受 ✅

比 goroutine 方案省 99% 記憶體 ✅
```

---

### 新問題：仍需輪詢

2024 年 8 月 10 日，週五下午 3:00

系統運行了 5 天，你發現新的瓶頸：

**問題 1：輪詢浪費 CPU**
```
ticker := time.NewTicker(100 * time.Millisecond)

每秒檢查：10 次
每天檢查：10 × 86,400 = 864,000 次

大部分檢查：
- 堆頂任務還沒到時間
- 白白消耗 CPU

如果降低檢查頻率（如 1 秒）：
- 精度降低到 1 秒
- 用戶體驗變差
```

**問題 2：鎖競爭**
```go
s.mu.Lock()
defer s.mu.Unlock()

高併發下：
- AddTask 與 Run 搶鎖
- 每次 Pop/Push 都要鎖全部堆
- 成為效能瓶頸
```

**問題 3：仍不可靠**
```
進程重啟：
- 堆在記憶體中
- 重啟後任務全部丟失

解決方案？
- 持久化到資料庫？→ 又回到輪詢方案
- 持久化到 Redis？→ 仍需輪詢
```

技術總監：「我們需要一個既高效又可靠的演算法。」

---

### 靈感：時間輪算法

2024 年 8 月 12 日，週日晚上 10:00

你在研究 Netty 和 Kafka 的原始碼時，發現它們都使用一種叫「時間輪」（Timing Wheel）的演算法。

**核心概念：像時鐘一樣的圓形槽位陣列**

```
想像一個時鐘，有 60 個槽位（代表 60 秒）：

   Slot 0 (00秒): [Task A, Task B]
   Slot 1 (01秒): []
   Slot 2 (02秒): [Task C]
   ...
   Slot 30 (30秒): [Task D]
   ...
   Slot 59 (59秒): []

指針每秒轉動一格：
- T=0 秒 → 指針在 Slot 0 → 執行 Task A, B
- T=1 秒 → 指針在 Slot 1 → 無任務
- T=2 秒 → 指針在 Slot 2 → 執行 Task C
- T=30 秒 → 指針在 Slot 30 → 執行 Task D
```

**比喻：銀行的號碼牌系統**
```
傳統方式（優先級佇列）：
- 所有人排一條隊
- 每次叫號都要找下一個號碼
- 效率低

時間輪方式：
- 按預約時間分組（10:00、10:01、10:02...）
- 到了 10:00 就叫 10:00 的所有人
- O(1) 效率 ✅
```

**關鍵優勢：**
```
1. O(1) 插入：
   slot = (currentSlot + delaySeconds) % 60
   wheel[slot].append(task)

2. O(1) 觸發：
   只檢查當前槽位，不需要掃描全部

3. 無輪詢：
   指針定時轉動（time.Ticker），不用一直檢查

4. 記憶體高效：
   任務分散在各槽位，不需要全局排序
```

---

### 實作：時間輪調度器

2024 年 8 月 13 日，週一上午 9:00

你開始實作時間輪：

**基礎結構：**
```go
package scheduler

import (
    "sync"
    "time"
)

const (
    SlotCount    = 3600             // 3600 個槽位 = 1 小時
    TickDuration = 1 * time.Second  // 每秒轉動一次
)

type Task struct {
    ID        string
    OrderID   int64
    Round     int       // 需要轉幾圈
    Callback  func()
}

type TimeWheel struct {
    slots       [SlotCount][]*Task  // 槽位陣列
    currentSlot int                 // 當前指針位置
    mu          sync.Mutex
    ticker      *time.Ticker
}

func NewTimeWheel() *TimeWheel {
    return &TimeWheel{
        slots:       [SlotCount][]*Task{},
        currentSlot: 0,
        ticker:      time.NewTicker(TickDuration),
    }
}

// 添加延遲任務
func (tw *TimeWheel) AddTask(task *Task, delaySeconds int) {
    tw.mu.Lock()
    defer tw.mu.Unlock()

    // 計算槽位位置
    slot := (tw.currentSlot + delaySeconds) % SlotCount

    // 計算需要轉幾圈
    round := delaySeconds / SlotCount
    task.Round = round

    // 加入對應槽位
    tw.slots[slot] = append(tw.slots[slot], task)
}

// 啟動時間輪
func (tw *TimeWheel) Start() {
    go func() {
        for range tw.ticker.C {
            tw.tick()
        }
    }()
}

// 指針轉動
func (tw *TimeWheel) tick() {
    tw.mu.Lock()

    // 移動指針
    tw.currentSlot = (tw.currentSlot + 1) % SlotCount

    // 獲取當前槽位的任務
    tasks := tw.slots[tw.currentSlot]
    tw.slots[tw.currentSlot] = nil  // 清空槽位

    tw.mu.Unlock()

    // 執行任務（不持鎖）
    for _, task := range tasks {
        if task.Round > 0 {
            // 還需要等待，圈數遞減，重新加入槽位
            task.Round--
            tw.mu.Lock()
            tw.slots[tw.currentSlot] = append(tw.slots[tw.currentSlot], task)
            tw.mu.Unlock()
        } else {
            // 時間到了，執行任務
            go task.Callback()
        }
    }
}
```

**使用範例：**
```go
func main() {
    wheel := NewTimeWheel()
    wheel.Start()

    // 創建訂單時，添加 30 分鐘超時任務
    wheel.AddTask(&Task{
        OrderID: 123,
        Callback: func() {
            CancelOrder(123)
        },
    }, 30*60) // 1800 秒
}
```

**效能測試：**
```
添加 10 萬個任務：
- 插入時間：10ms（O(1) × 10萬）
- 記憶體：10萬 × 64 bytes = 6.4 MB

觸發任務：
- 每秒檢查：1 個槽位（O(1)）
- CPU 使用：< 1%

對比優先級佇列：
- 插入：O(log N) vs O(1) ← 快 100 倍
- 觸發：O(1) vs O(1)（持平）
- 鎖競爭：單槽位 vs 全局 ← 減少 99%

→ 時間輪完勝 ✅
```

---

### 範例：30 分鐘訂單超時

**問題：如何處理長延遲任務？**

```
30 分鐘 = 1800 秒
槽位數 = 3600（1 小時）

插入任務：
delaySeconds = 1800
slot = (currentSlot + 1800) % 3600
round = 1800 / 3600 = 0  ← 不需要轉圈

如果是 2 小時超時（7200 秒）：
slot = (currentSlot + 7200) % 3600
round = 7200 / 3600 = 2  ← 需要轉 2 圈

第一圈：Round = 2 → Round = 1
第二圈：Round = 1 → Round = 0
第三圈：Round = 0 → 執行任務 ✅
```

**具體流程：**
```
T=0 (10:00:00):
- 添加 30 分鐘任務
- CurrentSlot = 0
- Slot = (0 + 1800) % 3600 = 1800
- Wheel[1800] = [Task(Round=0)]

T=1800s (10:30:00):
- CurrentSlot 轉到 1800
- 檢查 Wheel[1800]
- Task.Round = 0 → 執行 ✅
- 取消訂單、釋放庫存
```

---

## 新挑戰 1：持久化與可靠性

### 問題：進程重啟任務丟失

2024 年 8 月 15 日，週四下午 4:00

技術總監質疑：「時間輪在記憶體中，如果進程重啟，15 萬個任務全部丟失怎麼辦？」

你意識到需要持久化方案。

**解決方案：時間輪（記憶體）+ NATS JetStream（持久化）**

```
架構：
1. 任務創建 → 發送到 NATS（持久化到磁碟）
2. Worker 訂閱 NATS → 加載任務到時間輪（記憶體）
3. 時間到 → 執行任務 → ACK（從 NATS 刪除）
4. 進程重啟 → 從 NATS 重新加載未完成的任務

流程：
┌─────────┐  1. Publish   ┌──────────┐
│ Client  │──────────────>│   NATS   │
└─────────┘               │JetStream │
                          │(Persist) │
                          └────┬─────┘
                               │ 2. Subscribe
                          ┌────▼─────┐
                          │ Timing   │
                          │  Wheel   │
                          │(Memory)  │
                          └────┬─────┘
                               │ 3. Time's up
                          ┌────▼─────┐
                          │ Execute  │
                          │ & ACK    │
                          └──────────┘
```

**實作：**
```go
package scheduler

import (
    "encoding/json"
    "github.com/nats-io/nats.go"
)

type PersistentScheduler struct {
    wheel *TimeWheel
    js    nats.JetStreamContext
}

func NewPersistentScheduler(nc *nats.Conn) (*PersistentScheduler, error) {
    js, err := nc.JetStream()
    if err != nil {
        return nil, err
    }

    // 創建 Stream
    js.AddStream(&nats.StreamConfig{
        Name:     "SCHEDULED_TASKS",
        Subjects: []string{"task.delay.*"},
        Storage:  nats.FileStorage,
        MaxAge:   7 * 24 * time.Hour,
    })

    scheduler := &PersistentScheduler{
        wheel: NewTimeWheel(),
        js:    js,
    }

    // 訂閱任務，加載到時間輪
    scheduler.loadTasks()

    return scheduler, nil
}

// 添加任務（持久化）
func (s *PersistentScheduler) AddTask(orderID int64, delaySeconds int) error {
    taskData, _ := json.Marshal(TaskData{
        OrderID:   orderID,
        ExecuteAt: time.Now().Add(time.Duration(delaySeconds) * time.Second),
    })

    // 發布到 NATS（持久化）
    _, err := s.js.Publish("task.delay.order", taskData)
    return err
}

// 從 NATS 加載任務到時間輪
func (s *PersistentScheduler) loadTasks() {
    s.js.Subscribe("task.delay.*", func(msg *nats.Msg) {
        var taskData TaskData
        json.Unmarshal(msg.Data, &taskData)

        // 計算剩餘延遲時間
        remainingDelay := int(time.Until(taskData.ExecuteAt).Seconds())
        if remainingDelay < 0 {
            remainingDelay = 0  // 已過期，立即執行
        }

        // 加入時間輪
        s.wheel.AddTask(&Task{
            OrderID: taskData.OrderID,
            Callback: func() {
                // 執行任務
                CancelOrder(taskData.OrderID)

                // 確認消息（從 NATS 刪除）
                msg.Ack()
            },
        }, remainingDelay)
    }, nats.Durable("task-scheduler"), nats.ManualAck())
}
```

**可靠性保證：**
```
場景 1：正常執行
- 任務發布到 NATS → 磁碟持久化 ✅
- Worker 訂閱 → 加載到時間輪
- 時間到 → 執行 → ACK → NATS 刪除 ✅

場景 2：Worker 崩潰
- 任務在 NATS 中（已持久化）✅
- 未 ACK → 30 秒後重新投遞
- 其他 Worker 接收 → 執行 ✅

場景 3：Worker 重啟
- 重新訂閱 NATS
- 加載所有未完成任務到時間輪
- 計算剩餘延遲時間
- 繼續執行 ✅

→ 任務零丟失 ✅
```

---

## 新挑戰 2：Cron 定時任務

### 需求：每天凌晨 2 點生成報表

2024 年 8 月 20 日，週二上午 10:00

產品經理新需求：「除了延遲任務，我們還需要定時任務，比如每天凌晨 2:00 生成銷售報表。」

**問題：Cron 表達式如何整合到時間輪？**

**解決方案：計算下次執行時間，加入時間輪**

```go
type CronTask struct {
    Expression string  // "0 2 * * *" = 每天 2:00
    Callback   func()
}

func (s *PersistentScheduler) AddCronTask(cronTask *CronTask) error {
    // 解析 Cron 表達式
    schedule, err := ParseCron(cronTask.Expression)
    if err != nil {
        return err
    }

    // 計算下次執行時間
    nextRun := schedule.Next(time.Now())
    delaySeconds := int(time.Until(nextRun).Seconds())

    // 加入時間輪
    s.wheel.AddTask(&Task{
        Callback: func() {
            // 執行任務
            cronTask.Callback()

            // 重新計算下次執行時間
            s.AddCronTask(cronTask)  // 遞迴添加
        },
    }, delaySeconds)

    return nil
}
```

**範例：每天 2:00 生成報表**
```go
scheduler.AddCronTask(&CronTask{
    Expression: "0 2 * * *",
    Callback: func() {
        GenerateDailySalesReport()
    },
})

執行流程：
2024-08-20 10:00 - 添加任務
                  - 下次執行：2024-08-21 02:00
                  - 延遲：16 小時 = 57600 秒
                  - Slot = (currentSlot + 57600) % 3600
                  - Round = 57600 / 3600 = 16 圈

2024-08-21 02:00 - 執行任務（生成報表）
                  - 重新計算：下次 2024-08-22 02:00
                  - 再次加入時間輪

→ 週期性任務 ✅
```

---

## 新挑戰 3：分散式調度

### 問題：多個 Worker 如何避免重複執行？

2024 年 8 月 25 日，週日下午 3:00

為了高可用，你部署了 3 個 Worker 實例。但發現問題：**同一個任務被執行了 3 次！**

**問題分析：**
```
3 個 Worker 都訂閱 NATS：
- Worker 1 收到 Task A
- Worker 2 也收到 Task A
- Worker 3 也收到 Task A
→ Task A 被執行 3 次，訂單被取消 3 次 ❌
```

**解決方案：NATS Queue Groups**

```go
// 之前（錯誤）：
js.Subscribe("task.delay.*", handler)

// 現在（正確）：使用 Queue Group
js.QueueSubscribe(
    "task.delay.*",
    "task-scheduler-group",  // Queue Group 名稱
    handler,
)

機制：
Queue Group: "task-scheduler-group"
├─ Worker 1 ──> 處理 Task A, D, G
├─ Worker 2 ──> 處理 Task B, E, H
└─ Worker 3 ──> 處理 Task C, F, I

NATS 自動負載均衡：
- 每個任務只發給一個 Worker ✅
- Worker 崩潰 → 未 ACK 的任務重新分配
- 無需分散式鎖 ✅
```

**完整實作：**
```go
func (s *PersistentScheduler) loadTasks() {
    s.js.QueueSubscribe(
        "task.delay.*",
        "task-scheduler-group",  // 所有 Worker 使用相同 Group
        func(msg *nats.Msg) {
            var taskData TaskData
            json.Unmarshal(msg.Data, &taskData)

            remainingDelay := int(time.Until(taskData.ExecuteAt).Seconds())
            if remainingDelay < 0 {
                remainingDelay = 0
            }

            s.wheel.AddTask(&Task{
                OrderID: taskData.OrderID,
                Callback: func() {
                    CancelOrder(taskData.OrderID)
                    msg.Ack()  // 確認消息
                },
            }, remainingDelay)
        },
        nats.Durable("task-scheduler"),
        nats.ManualAck(),
    )
}
```

---

## 新挑戰 4：任務執行失敗與重試

### 災難場景：訂單服務當機

2024 年 9 月 1 日，週日凌晨 2:30

訂單服務進行版本升級，5 分鐘內不可用。這期間有 500 個超時任務要執行，但全部失敗。

**問題：執行失敗的任務怎麼辦？**

**解決方案：指數退避重試 + 死信佇列**

```go
func (s *PersistentScheduler) executeTask(msg *nats.Msg, taskData TaskData) {
    // 執行任務
    err := CancelOrder(taskData.OrderID)

    if err != nil {
        // 執行失敗
        meta, _ := msg.Metadata()

        if meta.NumDelivered >= 5 {
            // 重試次數過多，進入死信佇列
            log.Error("任務失敗次數過多", "order_id", taskData.OrderID, "delivered", meta.NumDelivered)

            // 發送到死信佇列
            s.js.Publish("task.dlq", msg.Data)

            // 確認原消息（不再重試）
            msg.Term()
        } else {
            // 指數退避重試
            delay := time.Duration(math.Pow(2, float64(meta.NumDelivered))) * time.Second
            log.Warn("任務執行失敗，延遲重試", "order_id", taskData.OrderID, "delay", delay)

            // NAK with delay
            msg.NakWithDelay(delay)
        }
    } else {
        // 執行成功
        msg.Ack()
    }
}
```

**重試時間表：**
```
第 1 次失敗：2^1 = 2 秒後重試
第 2 次失敗：2^2 = 4 秒後重試
第 3 次失敗：2^3 = 8 秒後重試
第 4 次失敗：2^4 = 16 秒後重試
第 5 次失敗：2^5 = 32 秒後重試
第 6 次失敗：進入死信佇列（人工介入）

優勢：
- 避免瞬時故障（網路抖動）
- 給系統恢復時間
- 避免雪崩效應
```

**死信佇列處理：**
```go
// 監控死信佇列
js.Subscribe("task.dlq", func(msg *nats.Msg) {
    var taskData TaskData
    json.Unmarshal(msg.Data, &taskData)

    // 記錄到資料庫
    db.Insert(&FailedTask{
        OrderID:   taskData.OrderID,
        Reason:    "重試次數過多",
        CreatedAt: time.Now(),
    })

    // 發送告警
    alertManager.Send("任務執行失敗", taskData.OrderID)

    // 人工介入處理
})
```

---

## 擴展性分析

### 當前架構容量（10K 任務/小時）

**配置：**
- Worker: 1 個實例
- 時間輪槽位: 3600（1 小時精度）
- NATS: 單機
- 記憶體: 2 GB

**效能測試：**
```
插入 10K 任務：
- 時間：10ms（O(1) × 10K）
- 記憶體：10K × 64 bytes = 640 KB

執行任務：
- 每秒觸發：約 3 個任務（10K / 3600）
- CPU 使用：< 1%

結論：單機綽綽有餘 ✅
```

---

### 10x 擴展（100K 任務/小時）

**瓶頸分析：**
- 時間輪：O(1) 操作，無瓶頸
- NATS：可處理 100K+ msg/s
- 任務執行：可能成為瓶頸（HTTP 回調延遲）

**方案：水平擴展 Worker**
```
部署 3 個 Worker 實例：
- Queue Group 自動負載均衡
- 每個 Worker 處理 33K 任務
- 故障自動容錯

成本：3 × $50 = $150/月
```

---

### 100x 擴展（1M 任務/小時）

**架構升級：**

```
                   ┌──── NATS 叢集 (3 節點) ────┐
                   │                             │
     ┌─────────────┴──────────┬─────────────────┴──────┐
     │                        │                         │
┌────▼────┐              ┌───▼─────┐             ┌────▼─────┐
│ NATS    │◄──── Raft ──►│ NATS    │◄─── Raft ──►│ NATS     │
│ Node 1  │              │ Node 2  │              │ Node 3   │
└────┬────┘              └────┬────┘              └────┬─────┘
     │                        │                         │
     │ Stream: SCHEDULED_TASKS (Replicas: 3)           │
     └────────────┬───────────┴─────────────────────────┘
                  │ Queue Subscribe
     ┌────────────┴───────────────────────────────┐
     │                                             │
┌────▼─────┐  ┌──────────┐  ...  ┌──────────┐    │
│ Worker 1 │  │Worker 2  │       │Worker 10 │    │
│ [Wheel]  │  │ [Wheel]  │       │ [Wheel]  │    │
└──────────┘  └──────────┘       └──────────┘    │
```

**配置：**
- 10 個 Worker 實例（c5.large）
- 3 個 NATS 節點（c5.xlarge）
- 每個 Worker 處理 100K 任務

**效能指標：**
- 吞吐量: 1M+ 任務/小時
- 延遲: P99 < 100ms
- 可用性: 99.99%（自動容錯）

**成本：**
- 10 × Worker: $500/月
- 3 × NATS: $450/月
- 總計: $950/月

---

## 真實世界案例

### Netty HashedWheelTimer（時間輪鼻祖）

**背景：**
- Netty 是高效能網路框架（Java）
- 需要處理大量連線超時、心跳檢測

**設計：**
```java
HashedWheelTimer timer = new HashedWheelTimer(
    tickDuration = 100,  // 100ms 一格
    ticksPerWheel = 512  // 512 個槽位
);

// 添加超時任務
timer.newTimeout(new TimerTask() {
    public void run(Timeout timeout) {
        // 處理超時
    }
}, 30, TimeUnit.SECONDS);
```

**效能：**
- 單機處理 100 萬+ 定時任務
- 記憶體佔用極低
- CPU 使用 < 1%

---

### Kafka Purgatory（延遲操作）

**背景：**
- Kafka 需要處理延遲的 Produce/Fetch 請求
- 例如：等待 ISR 副本確認

**設計：**
```scala
// 延遲操作放入 Purgatory（煉獄）
purgatory.tryCompleteElseWatch(delayedProduce, watchKeys)

// 內部使用時間輪
TimingWheel(
  tickMs = 1,
  wheelSize = 20,
  startMs = Time.SYSTEM.milliseconds
)
```

**多層時間輪：**
```
Level 1: 20 槽位 × 1ms = 20ms
Level 2: 20 槽位 × 20ms = 400ms
Level 3: 20 槽位 × 400ms = 8s
Level 4: 20 槽位 × 8s = 160s

任務降級：
- 160s 任務在 Level 4
- 降到 Level 3（8s）
- 降到 Level 2（400ms）
- 降到 Level 1（20ms）
- 執行 ✅
```

---

### Linux Kernel Timer Wheel（作業系統級）

**背景：**
- Linux 核心需要處理數百萬個定時器
- 例如：TCP 超時重傳、進程調度

**設計（簡化）：**
```c
// 5 層時間輪
struct tvec_base {
    struct list_head tv1[256];  // 0-255 jiffies
    struct list_head tv2[64];   // 256-16K jiffies
    struct list_head tv3[64];   // 16K-1M jiffies
    struct list_head tv4[64];   // 1M-64M jiffies
    struct list_head tv5[64];   // 64M+ jiffies
};

// 1 jiffy ≈ 1-10ms（取決於 HZ 配置）
```

**效能：**
- 支援數百萬定時器
- O(1) 插入與觸發
- 所有 Unix 系統都使用此架構

---

## 總結

### 核心思想

**使用時間輪算法實現高效能任務調度，結合 NATS JetStream 保證可靠性，適合秒級精度的延遲與定時任務。**

```
資料庫輪詢 → Goroutine → 優先級佇列 → 時間輪 + NATS

每一步都解決了上一步的問題：
- 輪詢 → Goroutine：解決資料庫壓力
- Goroutine → 佇列：解決記憶體爆炸
- 佇列 → 時間輪：解決輪詢浪費、鎖競爭
- 記憶體 → NATS：解決持久化、分散式
```

---

### 關鍵設計原則

**1. 時間輪算法（核心）**
```
O(1) 插入：slot = (current + delay) % slots
O(1) 觸發：只檢查當前槽位
記憶體高效：任務分散在槽位

圈數（Round）處理長延遲：
- 延遲 > 槽位數 → 需要轉多圈
- 每圈遞減 Round
- Round = 0 → 執行
```

**2. 記憶體 + 持久化**
```
時間輪：高效能記憶體操作
NATS：可靠磁碟持久化
最佳平衡：效能與可靠性
```

**3. 分散式互斥**
```
Queue Groups：無鎖設計
自動負載均衡：NATS 內建
容錯機制：Worker 崩潰自動重新分配
```

**4. 指數退避重試**
```
避免瞬時故障：給系統恢復時間
避免雪崩：不要立即重試
死信佇列：人工介入
```

---

### 適用場景

**時間輪 + NATS 適合：**
- 訂單超時取消（30 分鐘）
- 會議室預訂釋放（2 小時）
- 定時報表生成（每日凌晨）
- 資料同步任務（每小時）
- 優惠券過期處理
- 試用期到期提醒

**不適合：**
- 毫秒級精度需求（用專業調度器）
- 複雜 DAG 工作流（用 Airflow/Temporal）
- 超大規模（億級任務，用多層時間輪）

---

### 與其他方案對比

| 特性 | 時間輪+NATS | Redis ZSet | Cron 輪詢 | Quartz | Temporal |
|------|------------|-----------|-----------|--------|----------|
| **精度** | 秒級 | 秒級 | 分鐘級 | 秒級 | 毫秒級 |
| **插入** | O(1) | O(log N) | O(1) | O(log N) | O(1) |
| **觸發** | O(1) | O(log N) | O(N) | O(1) | O(1) |
| **記憶體** | 低 | 中 | 無 | 中 | 高 |
| **可靠性** | ✅ NATS | △ AOF | ✅ DB | ✅ DB | ✅ DB |
| **分散式** | ✅ Queue | △ 需鎖 | ❌ | ✅ | ✅ |
| **複雜度** | 低 | 低 | 極低 | 高 | 極高 |
| **適用** | 延遲任務 | 簡單延遲 | 定時任務 | 企業調度 | 工作流 |

---

### 生產環境檢查清單

**1. 可靠性**
- [ ] 啟用 NATS JetStream 持久化
- [ ] 配置 FileStorage（非 Memory）
- [ ] 設定 Queue Groups（避免重複執行）
- [ ] 配置重試機制（指數退避）
- [ ] 設定死信佇列

**2. 效能**
- [ ] 合理設定槽位數（3600 = 1 小時精度）
- [ ] 使用 NVMe SSD（NATS 持久化）
- [ ] 監控槽位分布（避免熱點）
- [ ] 批次執行相同時間任務

**3. 高可用**
- [ ] 部署 3+ Worker 實例
- [ ] NATS 叢集（3 節點）
- [ ] 跨可用區部署
- [ ] 健康檢查與自動重啟

**4. 監控告警**
- [ ] 任務堆積數量
- [ ] 執行延遲（實際 vs 預期）
- [ ] 失敗率與重試次數
- [ ] 死信佇列大小
- [ ] 槽位利用率

**5. 安全性**
- [ ] 任務簽名驗證
- [ ] 回調 URL 白名單
- [ ] 速率限制（防止濫用）
- [ ] NATS 認證與授權

---

### 延伸閱讀

**論文與演算法：**
- [Hashed and Hierarchical Timing Wheels](http://www.cs.columbia.edu/~nahum/w6998/papers/ton97-timing-wheels.pdf)（經典論文）
- Netty HashedWheelTimer 實作
- Kafka Purgatory 機制
- Linux Kernel Timer Wheel

**開源專案：**
- **Netty**：HashedWheelTimer（Java）
- **Asynq**：分散式任務佇列（Go + Redis）
- **Temporal**：工作流引擎（支援複雜 DAG）
- **Apache Airflow**：資料管道調度

**系統設計主題：**
- **Message Queue**：任務持久化基礎
- **Event-Driven**：事件驅動調度
- **Rate Limiter**：令牌桶演算法（類似時間輪）

---

## 最後的思考

### 為什麼時間輪如此高效？

1. **O(1) 操作**：插入與觸發都是常數時間
2. **空間換時間**：用槽位陣列換取效能
3. **無需排序**：任務自然按時間分布
4. **無需輪詢**：指針定時轉動

### 時間輪 vs 其他資料結構

```
優先級佇列（Min-Heap）：
- 插入：O(log N)
- 觸發：O(1)
- 缺點：需要全局排序、鎖競爭

時間輪：
- 插入：O(1)
- 觸發：O(1)
- 優勢：無需排序、無鎖（單槽位）

跳表（Skip List）：
- 插入：O(log N)
- 觸發：O(log N)
- 優勢：支援範圍查詢（時間輪不需要）
```

### 最重要的一課

**時間輪是用「空間」換「時間」的經典演算法——用槽位陣列的空間開銷，換取 O(1) 的時間效能。**

```
關鍵思想：
1. 不要排序全部任務（O(N log N)）
2. 而是按時間分組到槽位（O(1)）
3. 指針轉到哪個槽位，執行哪個槽位的任務

類比：
- 圖書館按分類擺放書籍（不是按書名全局排序）
- 銀行按預約時間分組叫號（不是按號碼全局排序）
- 作業系統按優先級分層調度（不是全部進程排序）

這就是時間輪教給我們的——在系統設計中，合理的資料結構選擇，可以帶來數量級的效能提升。
```
