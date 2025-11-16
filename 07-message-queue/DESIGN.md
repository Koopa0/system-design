# Message Queue 系統設計文檔

## 情境：電商平台的訂單處理危機

### 第一天：同步處理的簡單時代

2024 年 5 月 1 日，週三上午 9:00

你是一家電商平台的後端工程師李明。公司剛上線訂單系統，當用戶下單時需要執行多個操作：

1. 創建訂單記錄
2. 扣減庫存
3. 發送確認郵件
4. 增加用戶積分
5. 發送推播通知

**當前實作（同步調用）：**
```go
func CreateOrder(order *Order) error {
    // 1. 寫入訂單資料庫
    if err := db.Insert(order); err != nil {
        return err
    }

    // 2. 同步調用庫存服務
    if err := inventoryService.Deduct(order.Items); err != nil {
        db.Rollback(order.ID)
        return err
    }

    // 3. 同步發送郵件
    if err := emailService.SendConfirmation(order.Email, order); err != nil {
        log.Error("郵件發送失敗，但訂單已創建") // 怎麼處理？
    }

    // 4. 同步增加積分
    if err := pointService.Add(order.UserID, order.Points); err != nil {
        log.Error("積分增加失敗") // 又失敗了...
    }

    // 5. 同步發送推播
    pushService.Send(order.UserID, "訂單已創建") // 如果推播服務掛了呢？

    return nil
}
```

**效能指標：**
```
平均處理時間：
- 寫入資料庫：50ms
- 扣減庫存：100ms（遠端呼叫）
- 發送郵件：200ms（SMTP 連線）
- 增加積分：80ms（遠端呼叫）
- 發送推播：60ms（推播服務）
------------------
總計：490ms ← 使用者要等這麼久！
```

產品經理說：「490ms 太慢了，能不能優化到 100ms 以內？」

---

### 第 15 天：同步處理的災難

2024 年 5 月 15 日，週三下午 2:30

大促活動開始，訂單量瞬間從每秒 10 筆衝到 500 筆。

**災難場景 1：庫存服務當機**

下午 2:45，庫存服務因為流量過大當機。

```
使用者下單 → 訂單服務 → 呼叫庫存服務 → 超時！
                         ↓
                    整個訂單創建失敗 ← 明明可以先創建訂單，稍後再扣庫存
```

客服電話被打爆：「為什麼我下單失敗？頁面一直轉圈圈！」

**災難場景 2：郵件服務變慢**

下午 3:00，郵件服務因為某個客戶的郵箱伺服器回應慢，導致 SMTP 連線超時。

```
每筆訂單的郵件發送：
- 正常：200ms
- 異常：30 秒（超時） ← 使用者要等 30 秒才知道訂單成功或失敗！
```

監控顯示：訂單 API 的 P99 延遲從 500ms 飆升到 30 秒。

**災難場景 3：積分服務回滾困難**

```
時間線：
T1: 訂單創建成功
T2: 庫存扣減成功
T3: 郵件發送成功
T4: 積分增加失敗 ← 怎麼辦？

問題：
- 訂單已經創建，不能回滾
- 庫存已經扣減，不能恢復
- 但積分沒加到，使用者會投訴

解決方案？
- 手動補償？太慢，而且容易遺漏
- 重試機制？如果一直失敗呢？
```

技術總監召開緊急會議：「我們需要解耦這些服務，不能讓一個服務的失敗影響整個訂單流程。」

---

### 第一次嘗試：資料庫表作為任務佇列

2024 年 5 月 16 日，週四上午 10:00

架構師提議：「把非核心操作改成異步，用資料庫表存任務，後台慢慢處理。」

**實作方案：**
```sql
CREATE TABLE task_queue (
    id BIGINT PRIMARY KEY,
    task_type VARCHAR(50),  -- 'send_email', 'add_points', 'send_push'
    payload JSON,
    status VARCHAR(20),     -- 'pending', 'processing', 'completed', 'failed'
    retry_count INT DEFAULT 0,
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);
```

**訂單服務（生產者）：**
```go
func CreateOrder(order *Order) error {
    tx := db.Begin()

    // 1. 創建訂單（核心操作，同步）
    tx.Insert(order)

    // 2. 扣減庫存（核心操作，同步）
    if err := inventoryService.Deduct(order.Items); err != nil {
        tx.Rollback()
        return err
    }

    // 3. 郵件任務（非核心，異步）
    tx.Insert(&Task{
        Type: "send_email",
        Payload: json.Marshal(order),
        Status: "pending",
    })

    // 4. 積分任務（非核心，異步）
    tx.Insert(&Task{
        Type: "add_points",
        Payload: json.Marshal(PointsData{UserID: order.UserID, Points: 100}),
        Status: "pending",
    })

    // 5. 推播任務（非核心，異步）
    tx.Insert(&Task{
        Type: "send_push",
        Payload: json.Marshal(order),
        Status: "pending",
    })

    tx.Commit()
    return nil
}
```

**任務處理器（消費者）：**
```go
func TaskWorker() {
    for {
        // 輪詢資料庫，查找待處理任務
        tasks := db.Query("SELECT * FROM task_queue WHERE status = 'pending' LIMIT 10")

        for _, task := range tasks {
            // 標記為處理中
            db.Update("UPDATE task_queue SET status = 'processing' WHERE id = ?", task.ID)

            // 處理任務
            switch task.Type {
            case "send_email":
                emailService.Send(task.Payload)
            case "add_points":
                pointService.Add(task.Payload)
            case "send_push":
                pushService.Send(task.Payload)
            }

            // 標記為完成
            db.Update("UPDATE task_queue SET status = 'completed' WHERE id = ?", task.ID)
        }

        // 沒有任務時休眠
        time.Sleep(1 * time.Second)
    }
}
```

**效能改善：**
```
訂單創建時間：
- 之前：490ms（同步處理全部）
- 現在：150ms（只處理核心操作）← 改善 70%！

使用者體驗大幅提升 ✅
```

---

### 災難場景：資料庫成為瓶頸

2024 年 5 月 20 日，週一下午 3:00

大促活動第二波，訂單量達到每秒 1,000 筆。

**問題 1：輪詢浪費資源**

```
任務處理器每秒執行：
SELECT * FROM task_queue WHERE status = 'pending' LIMIT 10

即使沒有任務，也要查詢資料庫：
- 每秒 1 次查詢 × 10 個 Worker = 每秒 10 次無效查詢
- 24 小時 = 864,000 次無效查詢
- 浪費資料庫連線、CPU、I/O
```

**問題 2：資料庫寫入壓力**

```
每筆訂單產生 3 個任務：
- 訂單量：1,000/s
- 任務寫入：3,000/s
- 任務更新（status）：3,000/s
- 總 QPS：6,000（寫入操作）

資料庫 CPU：95% ← 快撐不住了！
```

**問題 3：無法即時處理**

```
任務處理器輪詢間隔：1 秒
→ 最快也要 1 秒後才能處理任務
→ 郵件延遲最少 1 秒，最多可能數秒

使用者投訴：「為什麼訂單創建 5 秒後才收到郵件？」
```

**問題 4：水平擴展困難**

```
如果增加 Worker 實例：
- Worker 1 查詢到任務 ID=123
- Worker 2 同時查詢到任務 ID=123
- 兩個 Worker 都處理 → 重複處理！

解決方案：
- 使用 FOR UPDATE 鎖定行 → 效能更差
- 或者分布式鎖 → 又增加複雜度
```

技術總監：「資料庫不是為消息佇列設計的，我們需要專業的解決方案。」

---

### 第二次嘗試：Redis List 作為佇列

2024 年 5 月 22 日，週三上午 9:00

架構師提議：「用 Redis List，LPUSH 寫入、BRPOP 阻塞讀取，效能超高！」

**實作方案：**
```go
// 生產者（訂單服務）
func CreateOrder(order *Order) error {
    // 1. 創建訂單
    db.Insert(order)

    // 2. 扣減庫存
    inventoryService.Deduct(order.Items)

    // 3. 推送任務到 Redis
    redis.LPush("task:email", json.Marshal(order))
    redis.LPush("task:points", json.Marshal(PointsData{UserID: order.UserID, Points: 100}))
    redis.LPush("task:push", json.Marshal(order))

    return nil
}

// 消費者（任務處理器）
func EmailWorker() {
    for {
        // 阻塞式獲取任務（無任務時阻塞，不浪費 CPU）
        result := redis.BRPop("task:email", 0)
        task := result[1] // [0] 是 key，[1] 是 value

        // 處理任務
        emailService.Send(task)
    }
}
```

**效能提升：**
```
資料庫寫入 QPS：
- 之前：6,000（訂單 + 任務寫入 + 任務更新）
- 現在：1,000（只有訂單寫入）← 降低 83%！

任務處理延遲：
- 之前：1-5 秒（輪詢間隔）
- 現在：< 10ms（即時推送）← 改善 99%！

CPU 使用率：
- 之前：Worker 輪詢浪費 CPU
- 現在：BRPOP 阻塞，無 CPU 浪費 ✅
```

---

### 災難場景：Redis List 的消息丟失

2024 年 5 月 25 日，週六凌晨 2:30

運維人員重啟 Redis 進行版本升級，5 分鐘後系統恢復。

隔天早上，客服主管找你：「昨天凌晨的訂單有 500 筆沒有發送郵件，也沒有加積分！」

你排查發現：**Redis 重啟時，記憶體中的任務全部丟失。**

**問題分析：**

**問題 1：Redis 重啟消息丟失**
```
凌晨 2:30:00 - Redis 正常運行，List 中有 500 個待處理任務
凌晨 2:30:05 - 運維執行 `redis-cli SHUTDOWN`
凌晨 2:30:06 - Redis 關閉，記憶體清空
凌晨 2:35:00 - Redis 重啟，List 為空 ← 500 個任務消失！
```

**問題 2：消費者崩潰消息丟失**
```
T1: Worker BRPOP 獲取任務
T2: Worker 開始處理任務
T3: Worker 進程崩潰（OOM、Panic、伺服器當機）
T4: 任務已從 Redis 移除，但未處理完成 ← 消息永久丟失！
```

**問題 3：無 ACK 機制**
```
Redis List 是簡單的資料結構：
- BRPOP 移除並返回元素
- 一旦移除，就無法恢復
- 沒有「處理中」狀態
- 沒有重試機制
```

你嘗試啟用 Redis 的 AOF 持久化：

```bash
# redis.conf
appendonly yes
appendfsync everysec  # 每秒同步一次
```

**新問題：效能下降**
```
啟用 AOF 後：
- 寫入延遲：< 1ms → 5ms（磁盤 I/O）
- 吞吐量：100K ops/s → 20K ops/s
- Redis CPU：15% → 45%

而且即使 AOF，也無法解決 Worker 崩潰導致的消息丟失問題。
```

技術總監：「Redis 不是專業的消息佇列，我們需要支援 ACK、重試、持久化的真正 MQ。」

---

### 技術選型：選擇哪種消息佇列？

2024 年 5 月 27 日，週一上午 10:00

團隊調研了市面上的消息佇列方案。

**候選方案對比：**

**方案 A：RabbitMQ**
```
優勢：
- 功能完整：路由、優先級、延遲佇列
- 成熟穩定：工業界廣泛使用（Instagram、Reddit）
- AMQP 標準：多語言支援

劣勢：
- Erlang 實作：與 Go 專案語言不一致
- 學習曲線陡峭：Exchange 類型（Direct、Topic、Fanout、Headers）
- 效能一般：單機約 20K-50K msg/s
- 運維複雜：叢集配置、mirror queue

配置範例：
exchange: orders_exchange (type: topic)
  ├─ routing key: order.created → queue: email_queue
  ├─ routing key: order.paid → queue: points_queue
  └─ routing key: order.* → queue: analytics_queue
→ 對於簡單場景來說過於複雜
```

**方案 B：Kafka**
```
優勢：
- 超高吞吐：單機 100 萬+ msg/s
- 消息持久化：磁盤順序寫入
- 消息回溯：Consumer 可重複消費
- 生態豐富：Kafka Streams、Kafka Connect

劣勢：
- 設計目標不同：為大數據日誌收集設計
- 重量級：依賴 ZooKeeper（或 KRaft）
- 延遲較高：批量寫入導致 P99 延遲 ~50-100ms
- 過度設計：對於簡單異步任務過於複雜

部署要求：
- 至少 3 個 Kafka Broker
- 至少 3 個 ZooKeeper 節點（或 KRaft 模式）
- 需要專門的運維團隊

適用場景：
- 日誌聚合（每天 TB 級資料）
- 事件流處理（Kafka Streams）
- 大數據管道（ETL）
→ 我們只是要發個郵件、加個積分，不需要這麼重的方案
```

**方案 C：NATS + JetStream**
```
優勢：
- 輕量級：單一二進位檔案，Go 原生實作
- 高效能：100 萬+ msg/s、微秒級延遲
- 簡單易用：API 直觀、配置簡單
- 功能完整：消費者組、ACK、重試、消息回溯
- 易於運維：docker-compose 一行啟動
- 漸進式：
  - Core NATS（簡單場景）：發後即忘
  - JetStream（需持久化）：At-least-once、Exactly-once

部署：
docker run -p 4222:4222 nats:latest -js
→ 就這麼簡單！

API 範例：
// 發送消息
js.Publish("order.created", orderData)

// 接收消息
js.Subscribe("order.created", handler)

→ 符合 Go 專案的簡潔哲學 ✅
```

**最終選擇：NATS + JetStream**

理由：
1. 與專案一致：Go 實作，程式碼風格統一
2. 效能卓越：滿足 100K+ msg/s 需求
3. 學習曲線平緩：適合教學和快速上手
4. 功能完整：涵蓋 MQ 核心需求
5. 運維簡單：不需要專門的 MQ 運維團隊

---

### 實作：NATS JetStream 消息佇列

2024 年 5 月 28 日，週二上午 9:00

你開始實作基於 NATS 的訂單系統。

**步驟 1：部署 NATS Server**

```bash
# docker-compose.yml
version: '3'
services:
  nats:
    image: nats:latest
    ports:
      - "4222:4222"  # 客戶端連線
      - "8222:8222"  # HTTP 監控
    command:
      - "-js"        # 啟用 JetStream
      - "-sd"        # 持久化目錄
      - "/data"
    volumes:
      - ./nats-data:/data
```

```bash
docker-compose up -d
```

**步驟 2：創建 Stream（定義消息存儲規則）**

```go
package queue

import (
    "github.com/nats-io/nats.go"
)

func InitJetStream(nc *nats.Conn) (nats.JetStreamContext, error) {
    js, err := nc.JetStream()
    if err != nil {
        return nil, err
    }

    // 創建 Stream：ORDERS
    _, err = js.AddStream(&nats.StreamConfig{
        Name:        "ORDERS",
        Subjects:    []string{"order.*"},      // 訂閱主題模式
        Storage:     nats.FileStorage,         // 檔案持久化
        MaxAge:      7 * 24 * time.Hour,       // 保留 7 天
        MaxBytes:    10 * 1024 * 1024 * 1024, // 10 GB 上限
        Retention:   nats.WorkQueuePolicy,     // 工作佇列模式（消費後刪除）
        Discard:     nats.DiscardOld,          // 超過上限時刪除舊消息
        Duplicates:  1 * time.Minute,          // 1 分鐘內去重（防止重複發送）
    })

    return js, err
}
```

**Stream 概念解釋：**
```
Stream 是消息的持久化容器，類似 Kafka 的 Topic：

ORDERS Stream
├─ Subject: order.created  → 訂單創建事件
├─ Subject: order.paid     → 訂單支付事件
├─ Subject: order.shipped  → 訂單出貨事件
└─ Subject: order.*        → 所有訂單相關事件

持久化到磁盤：
/data/jetstream/ORDERS/
  ├─ stream.dat    （Stream 元資料）
  ├─ msgs/         （消息資料）
  │   ├─ 1.blk
  │   ├─ 2.blk
  │   └─ 3.blk
  └─ consumers/    （Consumer 狀態）
```

**步驟 3：生產者（發送消息）**

```go
package order

import (
    "encoding/json"
    "github.com/nats-io/nats.go"
)

type OrderService struct {
    js nats.JetStreamContext
    db *sql.DB
}

func (s *OrderService) CreateOrder(order *Order) error {
    // 1. 寫入資料庫（核心操作，同步）
    if err := s.db.Insert(order); err != nil {
        return err
    }

    // 2. 扣減庫存（核心操作，同步）
    if err := inventoryService.Deduct(order.Items); err != nil {
        s.db.Rollback(order.ID)
        return err
    }

    // 3. 發送郵件（非核心，異步）
    emailTask, _ := json.Marshal(EmailTask{
        OrderID: order.ID,
        Email:   order.Email,
        Subject: "訂單確認",
    })
    pubAck, err := s.js.Publish("order.email", emailTask)
    if err != nil {
        log.Error("郵件任務發送失敗", err)
        // 不影響訂單創建，稍後重試
    } else {
        log.Info("郵件任務已發送", "sequence", pubAck.Sequence)
    }

    // 4. 增加積分（非核心，異步）
    pointTask, _ := json.Marshal(PointTask{
        UserID: order.UserID,
        Points: 100,
        Reason: "訂單完成",
    })
    s.js.Publish("order.points", pointTask)

    // 5. 發送推播（非核心，異步）
    pushTask, _ := json.Marshal(PushTask{
        UserID:  order.UserID,
        Message: "您的訂單已創建",
    })
    s.js.Publish("order.push", pushTask)

    return nil
}
```

**步驟 4：消費者（處理消息 - 手動 ACK）**

```go
package worker

type EmailWorker struct {
    js           nats.JetStreamContext
    emailService *EmailService
}

func (w *EmailWorker) Start() error {
    // 訂閱郵件任務
    _, err := w.js.Subscribe("order.email", func(msg *nats.Msg) {
        // 解析任務
        var task EmailTask
        if err := json.Unmarshal(msg.Data, &task); err != nil {
            log.Error("任務解析失敗", err)
            msg.Nak() // 重新入佇列
            return
        }

        // 處理任務
        log.Info("開始發送郵件", "order_id", task.OrderID)
        if err := w.emailService.Send(task.Email, task.Subject); err != nil {
            log.Error("郵件發送失敗", err)

            // 檢查重試次數
            meta, _ := msg.Metadata()
            if meta.NumDelivered > 3 {
                log.Error("重試次數過多，放棄", "delivered", meta.NumDelivered)
                msg.Term() // 終止重試（可轉到死信佇列）
            } else {
                msg.NakWithDelay(time.Second * 30) // 30 秒後重試
            }
            return
        }

        // 處理成功，確認消息
        msg.Ack()
        log.Info("郵件發送成功", "order_id", task.OrderID)
    }, nats.Durable("email-worker"), // 持久化 Consumer
       nats.ManualAck(),              // 手動 ACK
       nats.AckWait(30*time.Second))  // 30 秒未 ACK 視為超時
}
```

**消息流程圖：**
```
發送消息（At-least-once）：

Publisher                    JetStream                    Consumer
   │                             │                            │
   │ Publish("order.email")      │                            │
   │────────────────────────────>│                            │
   │                             │ 1. 寫入 WAL                │
   │                             │ 2. 持久化磁盤              │
   │                             │ 3. 更新索引                │
   │        PubAck{Seq: 123}     │                            │
   │<────────────────────────────│                            │
   │                             │                            │
   │                             │      Msg{Seq: 123}         │
   │                             │───────────────────────────>│
   │                             │                            │ 處理中...
   │                             │                            │
   │                             │          Ack()             │
   │                             │<───────────────────────────│
   │                             │ 標記已消費                 │
   │                             │ 刪除消息（WorkQueue 模式）│


重試機制（未 ACK）：

Consumer                     JetStream
   │                             │
   │      Msg{Seq: 124}          │
   │<────────────────────────────│
   │                             │
   │ 處理失敗                    │
   │ [未 ACK，30 秒超時]          │
   │                             │
   │                             │ 30 秒後...
   │      Msg{Seq: 124}          │ ← 自動重新投遞
   │<────────────────────────────│
   │                             │
   │ 處理成功                    │
   │          Ack()              │
   │────────────────────────────>│
```

**步驟 5：效能測試**

```bash
# 發送 100 萬條消息測試
$ nats bench order.email --msgs 1000000 --size 1024 --pub 10

結果：
Pub Stats: 124,532 msgs/sec ~ 121 MB/sec
 [1] 12,453 msgs/sec
 [2] 12,467 msgs/sec
 [3] 12,445 msgs/sec
 ...
 [10] 12,498 msgs/sec

Pub Latency:
 Min: 45µs
 Avg: 80µs
 P99: 1.2ms
 Max: 5.3ms

→ 單機輕鬆達到 12 萬 msg/s ✅
```

---

## 新挑戰 1：消費者組與負載均衡

### 問題：單一 Worker 處理速度跟不上

2024 年 6 月 1 日，週六下午 2:00

大促活動，訂單量達到每秒 5,000 筆，郵件任務積壓嚴重。

```
消息發送速度：5,000 msg/s
單個 Worker 處理速度：500 msg/s（郵件發送慢）
積壓速度：5,000 - 500 = 4,500 msg/s

1 小時後積壓：4,500 × 3,600 = 1,620 萬條消息 ← 災難！
```

**解決方案：Queue Groups（消費者組）**

```go
// Worker 1（實例 1）
js.QueueSubscribe(
    "order.email",           // Subject
    "email-workers",         // Queue Group 名稱
    handler,
    nats.Durable("email-worker-1"),
    nats.ManualAck(),
)

// Worker 2（實例 2，同一個 Queue Group）
js.QueueSubscribe(
    "order.email",
    "email-workers",         // 相同的 Queue Group
    handler,
    nats.Durable("email-worker-2"),
    nats.ManualAck(),
)

// Worker 3（實例 3）
js.QueueSubscribe(
    "order.email",
    "email-workers",
    handler,
    nats.Durable("email-worker-3"),
    nats.ManualAck(),
)
```

**負載均衡機制：**
```
JetStream 自動將消息分發到不同 Worker：

Queue Group: "email-workers"
├─ Worker 1 ──> 處理消息 1, 4, 7, 10, ...
├─ Worker 2 ──> 處理消息 2, 5, 8, 11, ...
└─ Worker 3 ──> 處理消息 3, 6, 9, 12, ...

吞吐量：
- 單 Worker：500 msg/s
- 3 個 Worker：1,500 msg/s（線性擴展）
- 10 個 Worker：5,000 msg/s（滿足需求）✅
```

**自動容錯：**
```
如果 Worker 2 崩潰：

Queue Group: "email-workers"
├─ Worker 1 ──> 處理消息 1, 3, 5, 7, ...（承擔更多）
└─ Worker 3 ──> 處理消息 2, 4, 6, 8, ...（承擔更多）

未 ACK 的消息會自動重新分配給其他 Worker ✅
```

---

## 新挑戰 2：消息順序性

### 問題：同一用戶的消息亂序

2024 年 6 月 5 日，週三上午 10:00

你發現一個嚴重問題：同一個用戶的訂單事件順序錯亂。

```
使用者 ID=123 的訂單事件：
T1: order.created（訂單創建）  → Worker 1 處理
T2: order.paid（訂單支付）     → Worker 2 處理（更快完成）
T3: order.shipped（訂單出貨）  → Worker 3 處理

實際處理順序：
1. Worker 2: order.paid（10ms）
2. Worker 3: order.shipped（15ms）
3. Worker 1: order.created（20ms）← 最晚處理

結果：資料不一致！
- 支付事件先到，但訂單還沒創建
- 出貨事件先到，但訂單還沒支付
```

**解決方案：分區順序（Partitioned Ordering）**

**方案 1：按 User ID 分區**
```go
// 生產者：按 User ID 路由到不同 Subject
func (s *OrderService) PublishEvent(userID int64, eventType string, data []byte) {
    // 使用 Hash 將 User ID 映射到 0-9（10 個分區）
    partition := userID % 10
    subject := fmt.Sprintf("order.partition.%d.%s", partition, eventType)

    s.js.Publish(subject, data)
}

// 範例：
PublishEvent(123, "created", data)  → "order.partition.3.created"
PublishEvent(123, "paid", data)     → "order.partition.3.paid"
PublishEvent(123, "shipped", data)  → "order.partition.3.shipped"
→ 同一用戶的所有事件都在 partition 3
```

**消費者：每個 Worker 訂閱特定分區**
```go
// Worker 1：訂閱 partition 0
js.Subscribe("order.partition.0.>", handler)

// Worker 2：訂閱 partition 1
js.Subscribe("order.partition.1.>", handler)

// ...

// Worker 10：訂閱 partition 9
js.Subscribe("order.partition.9.>", handler)
```

**順序保證：**
```
User 123（partition 3）的事件：
order.partition.3.created  → Worker 3 處理（序列）
order.partition.3.paid     → Worker 3 處理（序列）← 等待 created 完成
order.partition.3.shipped  → Worker 3 處理（序列）← 等待 paid 完成

User 456（partition 6）的事件：
order.partition.6.created  → Worker 6 處理（並行）
order.partition.6.paid     → Worker 6 處理（並行）

→ 同一用戶保證順序，不同用戶可並行處理 ✅
```

**處理熱點問題：**
```
如果某個分區消息過多（熱點用戶）：

解決方案：增加分區數量
- 從 10 個分區增加到 100 個分區
- 熱點用戶分散到更多分區
- userID % 100

或使用一致性雜湊：
- 虛擬節點
- 更均勻的分布
```

---

## 新挑戰 3：消息持久化與可靠性

### 災難演練：NATS 伺服器重啟

2024 年 6 月 10 日，週一凌晨 3:00

你進行災難演練，模擬 NATS 伺服器當機重啟。

**測試步驟：**
```bash
# 1. 發送 1000 條消息
$ nats pub order.email "test message" --count 1000

# 2. 檢查 Stream 狀態
$ nats stream info ORDERS
Messages: 1,000
Bytes: 12,000
FirstSeq: 1
LastSeq: 1,000

# 3. 停止 NATS
$ docker stop nats-server

# 4. 等待 30 秒

# 5. 啟動 NATS
$ docker start nats-server

# 6. 檢查 Stream 狀態
$ nats stream info ORDERS
Messages: 1,000  ← 消息完全恢復！
Bytes: 12,000
FirstSeq: 1
LastSeq: 1,000

# 7. 檢查 Consumer 狀態
$ nats consumer info ORDERS email-worker
Delivered: 0  ← 重啟前未消費的消息保持未消費狀態
Pending: 1,000

→ 重啟後消息零丟失 ✅
```

**持久化機制：**
```
JetStream 使用 WAL（Write-Ahead Log）：

1. Publisher 發送消息
   ↓
2. JetStream 寫入 WAL（/data/jetstream/ORDERS/wal/）
   ↓
3. 回覆 PubAck 給 Publisher
   ↓
4. 定期將 WAL 刷新到消息段文件（/data/jetstream/ORDERS/msgs/）
   ↓
5. 更新索引（/data/jetstream/ORDERS/stream.dat）

重啟恢復流程：
1. 讀取 stream.dat（元資料）
2. 讀取 msgs/（消息段）
3. 重放 WAL（未刷新的消息）
4. 重建記憶體索引
5. 恢復 Consumer 狀態（consumers/）

→ 保證消息不丟失 ✅
```

---

## 擴展性分析

### 當前架構容量（10K msg/s）

**配置：**
- NATS Server: 1 個實例
- CPU: 2 核心
- 記憶體: 4 GB
- 磁盤: 100 GB SSD

**效能測試結果：**
```
吞吐量：124,532 msg/s（實測）
延遲：P99 < 1.2ms
磁盤寫入：121 MB/s
CPU 使用率：15%
記憶體使用：800 MB

結論：單機處理 10K msg/s 綽綽有餘 ✅
```

---

### 10x 擴展（100K msg/s）

**瓶頸分析：**
- NATS Server: 可處理 100K+（測試已達 124K）
- 網路頻寬: 100K × 1KB = 100 MB/s（千兆網卡 125 MB/s，足夠）
- 磁盤 I/O: SSD 順序寫入 ~500 MB/s（足夠）
- CPU: 2 核心 → 4 核心

**方案：垂直擴展（推薦）**
```
升級配置：
- CPU: 4 核心
- 記憶體: 8 GB
- 磁盤: 200 GB NVMe SSD

成本：$50/月（AWS c5.xlarge）

效能預估：
- 吞吐量: 200K+ msg/s
- 延遲: P99 < 2ms
- 可用性: 99.9%（單機）

→ 滿足 100K msg/s 需求 ✅
```

---

### 100x 擴展（1M msg/s）

**架構升級：JetStream 叢集**

```
                   ┌──── Load Balancer (NATS) ────┐
                   │                                │
     ┌─────────────┴──────────┬────────────────────┴──────┐
     │                        │                            │
┌────▼────┐              ┌───▼─────┐               ┌─────▼───┐
│ NATS    │◄──── Raft ──►│ NATS    │◄──── Raft ───►│ NATS    │
│ Node 1  │              │ Node 2  │                │ Node 3  │
│(Leader) │              │(Follower)                │(Follower)│
└────┬────┘              └────┬────┘               └────┬─────┘
     │                        │                          │
     │ Stream: ORDERS (Replicas: 3)                     │
     │   - 消息自動複製到 3 個節點                       │
     │   - Raft 共識保證一致性                          │
     └────────────┬───────────┴──────────────────────────┘
                  │
     ┌────────────▼───────────────┐
     │  Distributed Storage        │
     │  - 每個節點獨立持久化       │
     │  - 自動故障轉移             │
     │  - Leader 選舉              │
     └────────────────────────────┘
```

**JetStream 叢集配置：**
```go
// 創建具有複本的 Stream
js.AddStream(&nats.StreamConfig{
    Name:     "ORDERS",
    Subjects: []string{"order.*"},
    Storage:  nats.FileStorage,
    Replicas: 3,  // 3 個副本（高可用）
})
```

**效能與成本：**
```
配置：
- 3 個 NATS 節點（c5.2xlarge: 8 核心、16 GB）
- 每個節點 200 GB NVMe SSD
- 跨可用區部署

效能指標：
- 吞吐量: 1M+ msg/s（3 節點並行處理）
- 延遲: P99 < 5ms（叢集內）
- 可用性: 99.99%（自動容錯）
- 資料持久性: 99.999999999%（3 副本）

成本：
- 3 × c5.2xlarge: $450/月
- 600 GB SSD (EBS): $60/月
- 總計: $510/月

對比 Kafka 叢集：
- Kafka: 3 Broker + 3 ZooKeeper = $1,200/月
- NATS: 3 節點 = $510/月
→ 節省 57% 成本 ✅
```

---

## 真實世界案例

### Synadia（NATS 原創公司）

**背景：**
- 2010 年，Derek Collison 在 VMware 開始 NATS 專案
- 2015 年，NATS 加入 CNCF（雲原生基金會）
- 2019 年，推出 JetStream（持久化層）

**設計哲學：**
```
「簡單、高效、可靠」

- 簡單：API 只有 Pub、Sub、Request
- 高效：Go 實作、零依賴
- 可靠：JetStream 提供持久化
```

**採用案例：**
- **Netlify**：邊緣運算消息傳遞
- **MasterCard**：支付系統事件流
- **Siemens**：IoT 設備通訊

---

### Kafka 的誕生（對比）

**背景：**
- 2010 年，LinkedIn 開發 Kafka
- 目標：處理每天數十億條活動日誌
- 2011 年開源，2012 年加入 Apache

**設計目標：**
```
大數據日誌收集：
- 每天 TB 級資料
- 長期儲存（數月、數年）
- 批次處理為主

→ 適合大數據管道，不適合微服務 MQ
```

**延遲對比：**
```
測試場景：1KB 消息

NATS：
- P50: 0.5ms
- P99: 1.2ms
- P99.9: 3ms

Kafka（批次 10ms）：
- P50: 15ms
- P99: 50ms
- P99.9: 100ms

→ NATS 延遲低 10-50 倍 ✅
```

---

### RabbitMQ 的複雜性（對比）

**背景：**
- 2007 年開發（Erlang）
- 實作 AMQP 協議
- 功能豐富但複雜

**Exchange 類型：**
```
1. Direct Exchange
   routing_key 完全匹配：
   order.created → queue_a

2. Topic Exchange
   pattern 匹配：
   order.* → queue_a
   *.created → queue_b

3. Fanout Exchange
   廣播到所有 queue：
   order.created → queue_a, queue_b, queue_c

4. Headers Exchange
   根據 message headers 路由

→ 學習曲線陡峭，配置複雜
```

**NATS 的簡潔設計：**
```
只有 Subject 匹配：
order.*         → 匹配 order.created, order.paid
order.>         → 匹配 order.created, order.a.b.c（多層）
*.created       → 匹配 order.created, user.created

→ 一個概念解決所有路由需求 ✅
```

---

## 總結

### 核心思想

**使用專業的消息佇列解耦服務，實現異步處理、削峰填谷、高可用性。**

```
同步處理 → 資料庫佇列 → Redis List → NATS JetStream

每一步都解決了上一步的問題：
- 同步 → 異步：解決耦合、效能問題
- 資料庫 → Redis：解決輪詢浪費、效能問題
- Redis → NATS：解決消息丟失、無 ACK、無重試問題
```

---

### 關鍵設計原則

**1. 至少一次送達（At-least-once）**
```
保證機制：
- Publisher: 等待 PubAck
- Consumer: 手動 ACK
- JetStream: 自動重試

代價：
- Consumer 需設計冪等性
- 可能重複消費

解決：
- 去重表（記錄已處理消息 ID）
- 冪等操作（如 SET 而非 INCREMENT）
```

**2. 漸進式複雜度**
```
Level 1: Core NATS（火後即忘）
適用：即時通訊、可容忍丟失

Level 2: JetStream（At-least-once）
適用：異步任務、事件驅動

Level 3: JetStream 叢集（高可用）
適用：生產環境、金融交易
```

**3. 水平擴展**
```
Queue Groups：
- 多個 Consumer 自動負載均衡
- Consumer 崩潰自動容錯
- 無狀態設計

分區：
- 保證同一 Key 的消息順序
- 不同 Key 可並行處理
```

**4. 監控與告警**
```
關鍵指標：
- 消息堆積數（Pending）
- 消息處理延遲
- Consumer 重試次數
- 磁盤使用率

告警閾值：
- Pending > 10K → 擴展 Consumer
- 延遲 > 100ms → 檢查效能瓶頸
- 重試 > 3 次 → 檢查業務邏輯
```

---

### 適用場景

**NATS JetStream 適合：**
- 微服務異步通訊
- 任務佇列（郵件、報表）
- 事件驅動架構
- 削峰填谷
- IoT 設備通訊
- 邊緣運算

**不適合：**
- 大數據日誌聚合（推薦 Kafka）
- 複雜企業集成（推薦 RabbitMQ）
- 消息優先級（NATS 不支援）

---

### 與其他 MQ 對比

| 特性 | NATS | Kafka | RabbitMQ | Redis |
|------|------|-------|----------|-------|
| **吞吐量** | 100K+ | 1M+ | 20K | 100K |
| **延遲** | <5ms | ~50ms | ~10ms | <1ms |
| **持久化** | ✅ JetStream | ✅ 磁盤日誌 | ✅ 可選 | △ AOF |
| **消息順序** | ✅ 分區 | ✅ Partition | △ 單 Queue | ❌ |
| **ACK 機制** | ✅ 手動 ACK | ✅ Offset | ✅ ACK | ❌ |
| **重試機制** | ✅ 自動 | △ Consumer | ✅ DLX | ❌ |
| **學習曲線** | 平緩 | 陡峭 | 中等 | 平緩 |
| **運維複雜度** | 低 | 高 | 中 | 低 |
| **生態系統** | 成長中 | 豐富 | 成熟 | 豐富 |
| **適用場景** | 微服務 MQ | 大數據管道 | 企業集成 | 簡單佇列 |

---

### 生產環境檢查清單

**1. 持久化與可靠性**
- [ ] 啟用 JetStream（非 Core NATS）
- [ ] 配置 FileStorage（非 Memory）
- [ ] 設定合理的 MaxAge、MaxBytes
- [ ] 配置副本數 Replicas: 3（叢集模式）
- [ ] 定期備份 JetStream 資料目錄

**2. 效能優化**
- [ ] 使用 NVMe SSD（非 HDD）
- [ ] 批次發送消息（降低網路開銷）
- [ ] 合理設定 AckWait 時間
- [ ] 監控 Pending 消息數

**3. 高可用性**
- [ ] 部署 3 節點 JetStream 叢集
- [ ] 跨可用區部署
- [ ] 配置健康檢查
- [ ] 演練故障切換

**4. 安全性**
- [ ] 啟用 TLS（客戶端 ↔ Server）
- [ ] 配置認證（JWT、NKey）
- [ ] Subject 層級權限控制
- [ ] 定期審計存取日誌

**5. 監控告警**
- [ ] Prometheus 整合
- [ ] Grafana 儀表板
- [ ] 告警：Pending > 10K
- [ ] 告警：延遲 > 100ms
- [ ] 告警：磁盤使用 > 80%

---

### 延伸閱讀

**NATS 官方資源：**
- [NATS 官方文件](https://docs.nats.io/)
- [JetStream 架構](https://docs.nats.io/nats-concepts/jetstream)
- [NATS vs Kafka](https://nats.io/blog/nats-vs-kafka/)

**系統設計主題：**
- **Task Scheduler**：基於 MQ 的延遲任務
- **Event Sourcing**：事件溯源架構
- **CQRS**：命令查詢職責分離

**設計模式：**
- 生產者-消費者模式
- 發布-訂閱模式
- 重試與熔斷模式

---

## 最後的思考

### 為什麼選擇 NATS？

1. **簡單**：API 直觀，5 分鐘上手
2. **高效**：微秒級延遲，百萬級吞吐
3. **可靠**：JetStream 保證不丟失
4. **輕量**：單一二進位，無外部依賴
5. **Go 原生**：與專案語言一致

### 消息佇列不是銀彈

```
不要為了用 MQ 而用 MQ：

適合異步的場景：
- 發送郵件、簡訊（慢、不重要）
- 生成報表（慢、不緊急）
- 日誌收集（可容忍延遲）

不適合異步的場景：
- 扣款操作（需即時反饋）
- 登入驗證（需即時回應）
- 即時查詢（使用者等待結果）
```

### 最重要的一課

**消息佇列解決的核心問題是「解耦」和「削峰」，而不是效能。**

```
錯誤思維：
「我的 API 很慢，加個 MQ 就會變快」
→ 錯！異步只是讓使用者感覺快，實際處理時間沒變

正確思維：
「我要解耦服務，避免同步依賴」
「我要削峰填谷，應對流量突波」
→ 對！這才是 MQ 的價值

關鍵：
- 區分核心與非核心操作
- 核心操作同步處理（如扣款）
- 非核心操作異步處理（如郵件）
```

這就是消息佇列教給我們的——在分散式系統中，用異步解耦實現高可用、高擴展性，是一種優雅而實用的解決方案。
