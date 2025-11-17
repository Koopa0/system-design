# Chapter 17: Notification Service - 通知服務

> 從單一郵件到多渠道通知系統：Push、Email、SMS、In-App 的統一設計

## 本章概述

這是一個關於**通知服務（Notification Service）**設計的完整指南，使用**蘇格拉底式教學法**（Socratic Method）。你將跟隨 Emma（產品經理）、David（架構師）、Sarah（後端工程師）、Michael（運維工程師）和 Jennifer（前端工程師）一起，從零開始設計一個生產級的多渠道通知系統。

## 學習目標

- 理解**多渠道通知**（Push、Email、SMS、In-App）的設計權衡
- 掌握**通知優先級**和**限流**策略
- 學習**通知模板管理**和**個性化**
- 實踐**用戶偏好設置**和**退訂機制**
- 了解**重試和失敗處理**
- 掌握**第三方服務集成**（APNs、FCM、SES、Twilio）
- 學習**通知去重**和**合併**策略
- 理解**通知分析**和**追蹤**
- 掌握**橫向擴展**和**高可用**設計
- 學習 Uber、Airbnb 的真實案例

## 角色介紹

- **Emma**：產品經理，負責定義通知系統的業務需求
- **David**：資深架構師，擅長設計可擴展的系統
- **Sarah**：後端工程師，實現核心通知邏輯
- **Michael**：運維工程師，關注系統穩定性和監控
- **Jennifer**：前端工程師，負責站內通知的展示

---

## Act 1: 從簡單郵件開始

**場景：產品需求會議**

**Emma**（產品經理）走進會議室，在白板上寫下：

```
新功能需求：
- 用戶註冊成功 → 發送歡迎郵件
- 訂單支付成功 → 發送確認郵件
- 密碼重置 → 發送驗證碼郵件
```

**Emma**: "我們需要一個通知系統。最基本的，用戶註冊成功後發送歡迎郵件。David，最簡單的實現是什麼？"

**David**（架構師）思考片刻：

**David**: "最簡單的方式是在註冊接口裡直接調用 SMTP 發送郵件。"

```go
package main

import (
    "fmt"
    "net/smtp"
)

// SimpleEmailService - 簡單郵件服務
type SimpleEmailService struct {
    smtpHost string
    smtpPort string
    username string
    password string
}

// SendWelcomeEmail - 發送歡迎郵件
func (s *SimpleEmailService) SendWelcomeEmail(userEmail, username string) error {
    from := s.username
    to := []string{userEmail}
    subject := "Welcome to Our Platform!"
    body := fmt.Sprintf("Hi %s,\n\nWelcome to our platform!", username)

    message := []byte(fmt.Sprintf("Subject: %s\n\n%s", subject, body))

    auth := smtp.PlainAuth("", s.username, s.password, s.smtpHost)
    addr := fmt.Sprintf("%s:%s", s.smtpHost, s.smtpPort)

    return smtp.SendMail(addr, auth, from, to, message)
}

// 在註冊接口中使用
func RegisterUser(email, username string) error {
    // 1. 創建用戶
    // db.Insert(...)

    // 2. 直接發送郵件
    emailService := &SimpleEmailService{
        smtpHost: "smtp.gmail.com",
        smtpPort: "587",
        username: "noreply@example.com",
        password: "password",
    }

    if err := emailService.SendWelcomeEmail(email, username); err != nil {
        // 郵件發送失敗怎麼辦？
        return fmt.Errorf("failed to send email: %w", err)
    }

    return nil
}
```

**Sarah**（後端工程師）皺眉：

**Sarah**: "這個方案有個問題：如果 SMTP 服務器很慢（比如 3 秒），用戶註冊請求也會等 3 秒。這會影響用戶體驗。"

**David**: "很好的觀察！所以我們需要**異步發送**。"

### 改進方案：異步發送

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "log"
    "net/smtp"
    "time"
)

// AsyncEmailService - 異步郵件服務
type AsyncEmailService struct {
    smtpHost string
    smtpPort string
    username string
    password string
    emailChan chan EmailTask // 郵件任務隊列
}

// EmailTask - 郵件任務
type EmailTask struct {
    To      string
    Subject string
    Body    string
}

// NewAsyncEmailService - 創建異步郵件服務
func NewAsyncEmailService(smtpHost, smtpPort, username, password string) *AsyncEmailService {
    service := &AsyncEmailService{
        smtpHost:  smtpHost,
        smtpPort:  smtpPort,
        username:  username,
        password:  password,
        emailChan: make(chan EmailTask, 1000), // 緩衝隊列
    }

    // 啟動後台 worker
    go service.worker()

    return service
}

// worker - 後台發送郵件
func (s *AsyncEmailService) worker() {
    for task := range s.emailChan {
        if err := s.sendEmail(task); err != nil {
            log.Printf("Failed to send email to %s: %v", task.To, err)
            // TODO: 重試邏輯？
        }
    }
}

// sendEmail - 實際發送郵件
func (s *AsyncEmailService) sendEmail(task EmailTask) error {
    message := []byte(fmt.Sprintf("Subject: %s\n\n%s", task.Subject, task.Body))
    auth := smtp.PlainAuth("", s.username, s.password, s.smtpHost)
    addr := fmt.Sprintf("%s:%s", s.smtpHost, s.smtpPort)
    return smtp.SendMail(addr, auth, s.username, []string{task.To}, message)
}

// SendAsync - 異步發送郵件
func (s *AsyncEmailService) SendAsync(to, subject, body string) error {
    select {
    case s.emailChan <- EmailTask{To: to, Subject: subject, Body: body}:
        return nil
    default:
        return fmt.Errorf("email queue is full")
    }
}

// 在註冊接口中使用
func RegisterUserAsync(email, username string, emailService *AsyncEmailService) error {
    // 1. 創建用戶
    // db.Insert(...)

    // 2. 異步發送郵件（立即返回）
    subject := "Welcome to Our Platform!"
    body := fmt.Sprintf("Hi %s,\n\nWelcome!", username)
    return emailService.SendAsync(email, subject, body)
}
```

**Emma**: "異步發送解決了延遲問題，但如果郵件發送失敗怎麼辦？用戶可能永遠收不到歡迎郵件。"

**David**: "這就需要**持久化隊列**和**重試機制**了。"

---

## Act 2: 持久化和重試機制

**場景：凌晨 2 點，SMTP 服務器故障告警**

**Michael**（運維工程師）在 Slack 上發消息：

```
🚨 SMTP 服務器宕機了！
過去 1 小時有 5000 封郵件發送失敗。
內存隊列丟失了所有任務。
```

**David**: "內存隊列不可靠。我們需要把通知任務持久化到數據庫，並添加重試機制。"

### 設計：通知任務表

```sql
CREATE TABLE notification_tasks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    task_id VARCHAR(64) UNIQUE NOT NULL,           -- 任務 ID（冪等性）
    channel ENUM('email', 'sms', 'push') NOT NULL, -- 通知渠道
    recipient VARCHAR(255) NOT NULL,                -- 接收者（郵箱/手機/設備 Token）
    subject VARCHAR(255),                           -- 主題（郵件用）
    body TEXT NOT NULL,                             -- 內容
    status ENUM('pending', 'sending', 'sent', 'failed') DEFAULT 'pending',
    retry_count INT DEFAULT 0,                      -- 重試次數
    max_retries INT DEFAULT 3,                      -- 最大重試次數
    next_retry_at TIMESTAMP,                        -- 下次重試時間
    error_message TEXT,                             -- 錯誤信息
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_status_next_retry (status, next_retry_at),
    INDEX idx_task_id (task_id)
);
```

### 實現：持久化通知服務

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "log"
    "time"
)

// NotificationTask - 通知任務
type NotificationTask struct {
    ID          int64
    TaskID      string
    Channel     string // email, sms, push
    Recipient   string
    Subject     string
    Body        string
    Status      string
    RetryCount  int
    MaxRetries  int
    NextRetryAt time.Time
    ErrorMsg    string
}

// NotificationService - 通知服務
type NotificationService struct {
    db           *sql.DB
    emailSender  EmailSender
    smsSender    SMSSender
    pushSender   PushSender
}

// EmailSender - 郵件發送接口
type EmailSender interface {
    Send(to, subject, body string) error
}

// SMSSender - 短信發送接口
type SMSSender interface {
    Send(to, message string) error
}

// PushSender - 推送發送接口
type PushSender interface {
    Send(deviceToken, title, body string) error
}

// CreateTask - 創建通知任務
func (s *NotificationService) CreateTask(ctx context.Context, task NotificationTask) error {
    query := `
        INSERT INTO notification_tasks
        (task_id, channel, recipient, subject, body, status, max_retries, next_retry_at)
        VALUES (?, ?, ?, ?, ?, 'pending', ?, NOW())
    `
    _, err := s.db.ExecContext(ctx, query,
        task.TaskID, task.Channel, task.Recipient, task.Subject, task.Body, task.MaxRetries)
    return err
}

// ProcessPendingTasks - 處理待發送任務（定時任務，每 10 秒執行一次）
func (s *NotificationService) ProcessPendingTasks(ctx context.Context) error {
    // 查詢需要發送的任務
    query := `
        SELECT id, task_id, channel, recipient, subject, body, retry_count, max_retries
        FROM notification_tasks
        WHERE status IN ('pending', 'failed')
          AND next_retry_at <= NOW()
        LIMIT 100
    `

    rows, err := s.db.QueryContext(ctx, query)
    if err != nil {
        return err
    }
    defer rows.Close()

    for rows.Next() {
        var task NotificationTask
        if err := rows.Scan(&task.ID, &task.TaskID, &task.Channel, &task.Recipient,
            &task.Subject, &task.Body, &task.RetryCount, &task.MaxRetries); err != nil {
            log.Printf("Failed to scan task: %v", err)
            continue
        }

        // 處理任務
        s.processTask(ctx, &task)
    }

    return rows.Err()
}

// processTask - 處理單個任務
func (s *NotificationService) processTask(ctx context.Context, task *NotificationTask) {
    // 1. 更新狀態為 sending（防止重複處理）
    if err := s.updateTaskStatus(ctx, task.ID, "sending", ""); err != nil {
        log.Printf("Failed to update task status: %v", err)
        return
    }

    // 2. 根據渠道發送通知
    var err error
    switch task.Channel {
    case "email":
        err = s.emailSender.Send(task.Recipient, task.Subject, task.Body)
    case "sms":
        err = s.smsSender.Send(task.Recipient, task.Body)
    case "push":
        err = s.pushSender.Send(task.Recipient, task.Subject, task.Body)
    default:
        err = fmt.Errorf("unknown channel: %s", task.Channel)
    }

    // 3. 處理結果
    if err != nil {
        s.handleFailure(ctx, task, err)
    } else {
        s.updateTaskStatus(ctx, task.ID, "sent", "")
    }
}

// handleFailure - 處理發送失敗
func (s *NotificationService) handleFailure(ctx context.Context, task *NotificationTask, err error) {
    task.RetryCount++

    if task.RetryCount >= task.MaxRetries {
        // 達到最大重試次數，標記為失敗
        s.updateTaskStatus(ctx, task.ID, "failed", err.Error())
        log.Printf("Task %s failed after %d retries: %v", task.TaskID, task.RetryCount, err)
    } else {
        // 計算下次重試時間（指數退避）
        nextRetry := time.Now().Add(time.Duration(1<<task.RetryCount) * time.Minute)

        query := `
            UPDATE notification_tasks
            SET status = 'failed', retry_count = ?, next_retry_at = ?, error_message = ?
            WHERE id = ?
        `
        s.db.ExecContext(ctx, query, task.RetryCount, nextRetry, err.Error(), task.ID)

        log.Printf("Task %s failed (retry %d/%d), next retry at %v",
            task.TaskID, task.RetryCount, task.MaxRetries, nextRetry)
    }
}

// updateTaskStatus - 更新任務狀態
func (s *NotificationService) updateTaskStatus(ctx context.Context, taskID int64, status, errorMsg string) error {
    query := `UPDATE notification_tasks SET status = ?, error_message = ? WHERE id = ?`
    _, err := s.db.ExecContext(ctx, query, status, errorMsg, taskID)
    return err
}
```

**Sarah**: "這個設計有持久化和重試了，但每 10 秒輪詢數據庫會有延遲。有沒有更實時的方案？"

**David**: "可以結合**消息隊列**（Kafka）和數據庫。"

---

## Act 3: 引入消息隊列（Kafka）

**David**: "我們可以用 Kafka 作為實時隊列，數據庫作為持久化備份。這樣既有實時性，又有可靠性。"

### 架構設計

```
API 接口
   ↓
1. 寫入數據庫（notification_tasks）
   ↓
2. 發送到 Kafka（notification.tasks）
   ↓
3. Worker 消費 Kafka
   ↓
4. 發送通知（Email/SMS/Push）
   ↓
5. 更新數據庫狀態
```

### 實現：Kafka Producer

```go
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"

    "github.com/segmentio/kafka-go"
)

// KafkaNotificationService - 基於 Kafka 的通知服務
type KafkaNotificationService struct {
    db     *sql.DB
    writer *kafka.Writer
}

// NewKafkaNotificationService - 創建服務
func NewKafkaNotificationService(db *sql.DB, kafkaBrokers []string) *KafkaNotificationService {
    return &KafkaNotificationService{
        db: db,
        writer: &kafka.Writer{
            Addr:     kafka.TCP(kafkaBrokers...),
            Topic:    "notification.tasks",
            Balancer: &kafka.LeastBytes{},
        },
    }
}

// SendNotification - 發送通知
func (s *KafkaNotificationService) SendNotification(ctx context.Context, task NotificationTask) error {
    // 1. 寫入數據庫（持久化）
    if err := s.createTask(ctx, task); err != nil {
        return fmt.Errorf("failed to create task in DB: %w", err)
    }

    // 2. 發送到 Kafka（實時處理）
    taskJSON, _ := json.Marshal(task)
    if err := s.writer.WriteMessages(ctx, kafka.Message{
        Key:   []byte(task.TaskID),
        Value: taskJSON,
    }); err != nil {
        // Kafka 失敗不影響主流程，Worker 會從數據庫補償
        fmt.Printf("Failed to send to Kafka: %v (will retry from DB)\n", err)
    }

    return nil
}

func (s *KafkaNotificationService) createTask(ctx context.Context, task NotificationTask) error {
    query := `
        INSERT INTO notification_tasks
        (task_id, channel, recipient, subject, body, status, max_retries, next_retry_at)
        VALUES (?, ?, ?, ?, ?, 'pending', ?, NOW())
    `
    _, err := s.db.ExecContext(ctx, query,
        task.TaskID, task.Channel, task.Recipient, task.Subject, task.Body, task.MaxRetries)
    return err
}
```

### 實現：Kafka Consumer（Worker）

```go
package main

import (
    "context"
    "encoding/json"
    "log"

    "github.com/segmentio/kafka-go"
)

// NotificationWorker - 通知 Worker
type NotificationWorker struct {
    reader  *kafka.Reader
    service *NotificationService
}

// NewNotificationWorker - 創建 Worker
func NewNotificationWorker(kafkaBrokers []string, service *NotificationService) *NotificationWorker {
    return &NotificationWorker{
        reader: kafka.NewReader(kafka.ReaderConfig{
            Brokers: kafkaBrokers,
            Topic:   "notification.tasks",
            GroupID: "notification-workers",
        }),
        service: service,
    }
}

// Start - 啟動 Worker
func (w *NotificationWorker) Start(ctx context.Context) error {
    for {
        msg, err := w.reader.ReadMessage(ctx)
        if err != nil {
            return err
        }

        var task NotificationTask
        if err := json.Unmarshal(msg.Value, &task); err != nil {
            log.Printf("Failed to unmarshal task: %v", err)
            continue
        }

        // 處理任務
        w.service.processTask(ctx, &task)
    }
}
```

**Michael**: "如果 Kafka Consumer 處理失敗，任務會丟失嗎？"

**David**: "不會，因為我們有雙重保障：
1. **Kafka 消費失敗** → 任務仍在數據庫（pending 狀態），定時任務會重試
2. **數據庫兜底** → 每 1 分鐘掃描 pending 任務補償處理"

---

## Act 4: 多渠道支持（Email、SMS、Push）

**Emma**: "現在我們需要支持多種通知渠道：郵件、短信、推送通知。它們的集成方式都不同。"

### 渠道對比

| 渠道 | 服務商 | 延遲 | 成本 | 到達率 |
|------|--------|------|------|--------|
| Email | AWS SES, SendGrid | 秒級 | $0.0001/封 | 85-95% |
| SMS | Twilio, AWS SNS | 秒級 | $0.01/條 | 98% |
| Push | APNs (iOS), FCM (Android) | 毫秒級 | 免費 | 90% (需在線) |
| In-App | WebSocket | 毫秒級 | 免費 | 100% (在線時) |

### 設計：統一發送接口

```go
package main

import (
    "fmt"
)

// NotificationSender - 通知發送器接口
type NotificationSender interface {
    Send(task NotificationTask) error
    Channel() string
}

// EmailSenderImpl - 郵件發送器（AWS SES）
type EmailSenderImpl struct {
    sesClient interface{} // AWS SES SDK client
}

func (s *EmailSenderImpl) Send(task NotificationTask) error {
    // 調用 AWS SES API
    fmt.Printf("Sending email to %s: %s\n", task.Recipient, task.Subject)
    // sesClient.SendEmail(...)
    return nil
}

func (s *EmailSenderImpl) Channel() string {
    return "email"
}

// SMSSenderImpl - 短信發送器（Twilio）
type SMSSenderImpl struct {
    twilioClient interface{} // Twilio SDK client
}

func (s *SMSSenderImpl) Send(task NotificationTask) error {
    // 調用 Twilio API
    fmt.Printf("Sending SMS to %s: %s\n", task.Recipient, task.Body)
    // twilioClient.SendSMS(...)
    return nil
}

func (s *SMSSenderImpl) Channel() string {
    return "sms"
}

// PushSenderImpl - 推送通知發送器（FCM/APNs）
type PushSenderImpl struct {
    fcmClient  interface{} // Firebase Cloud Messaging client
    apnsClient interface{} // Apple Push Notification Service client
}

func (s *PushSenderImpl) Send(task NotificationTask) error {
    // 根據設備類型選擇 FCM 或 APNs
    fmt.Printf("Sending push to %s: %s\n", task.Recipient, task.Subject)
    // if iOS: apnsClient.Send(...)
    // if Android: fcmClient.Send(...)
    return nil
}

func (s *PushSenderImpl) Channel() string {
    return "push"
}

// MultiChannelNotificationService - 多渠道通知服務
type MultiChannelNotificationService struct {
    senders map[string]NotificationSender
    db      *sql.DB
}

func NewMultiChannelNotificationService(db *sql.DB) *MultiChannelNotificationService {
    service := &MultiChannelNotificationService{
        senders: make(map[string]NotificationSender),
        db:      db,
    }

    // 註冊發送器
    service.RegisterSender(&EmailSenderImpl{})
    service.RegisterSender(&SMSSenderImpl{})
    service.RegisterSender(&PushSenderImpl{})

    return service
}

func (s *MultiChannelNotificationService) RegisterSender(sender NotificationSender) {
    s.senders[sender.Channel()] = sender
}

func (s *MultiChannelNotificationService) processTask(ctx context.Context, task *NotificationTask) {
    // 1. 更新狀態為 sending
    s.updateTaskStatus(ctx, task.ID, "sending", "")

    // 2. 查找對應的發送器
    sender, ok := s.senders[task.Channel]
    if !ok {
        s.updateTaskStatus(ctx, task.ID, "failed", fmt.Sprintf("unknown channel: %s", task.Channel))
        return
    }

    // 3. 發送通知
    if err := sender.Send(*task); err != nil {
        s.handleFailure(ctx, task, err)
    } else {
        s.updateTaskStatus(ctx, task.ID, "sent", "")
    }
}

func (s *MultiChannelNotificationService) updateTaskStatus(ctx context.Context, taskID int64, status, errorMsg string) error {
    query := `UPDATE notification_tasks SET status = ?, error_message = ? WHERE id = ?`
    _, err := s.db.ExecContext(ctx, query, status, errorMsg, taskID)
    return err
}

func (s *MultiChannelNotificationService) handleFailure(ctx context.Context, task *NotificationTask, err error) {
    // 重試邏輯（同 Act 2）
}
```

**Jennifer**: "推送通知需要設備 Token，我們怎麼知道用戶的設備 Token？"

**David**: "需要設計一個設備管理系統。"

### 設計：設備表

```sql
CREATE TABLE user_devices (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    device_id VARCHAR(128) UNIQUE NOT NULL,     -- 設備唯一標識
    device_type ENUM('ios', 'android', 'web'),
    push_token VARCHAR(255),                    -- FCM/APNs Token
    status ENUM('active', 'inactive') DEFAULT 'active',
    last_active_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_id (user_id),
    INDEX idx_push_token (push_token)
);
```

**David**: "當用戶登錄時，客戶端上傳設備信息和 Push Token。發送推送時，查詢用戶的所有活躍設備。"

---

## Act 5: 通知優先級和限流

**場景：凌晨 3 點，大量垃圾通知**

**Michael**: "我們的營銷部門剛發了一個推廣活動，向 100 萬用戶發送郵件。郵件服務器被打爆了！"

**Emma**: "我們需要**優先級**和**限流**。重要通知（密碼重置）要優先發送，營銷郵件可以慢慢發。"

### 設計：通知優先級

```sql
ALTER TABLE notification_tasks
ADD COLUMN priority ENUM('critical', 'high', 'normal', 'low') DEFAULT 'normal';

-- 查詢時按優先級排序
CREATE INDEX idx_priority_status ON notification_tasks(priority DESC, status, next_retry_at);
```

### 實現：優先級隊列

```go
package main

import (
    "context"
    "database/sql"
)

// ProcessPendingTasksWithPriority - 按優先級處理任務
func (s *NotificationService) ProcessPendingTasksWithPriority(ctx context.Context) error {
    query := `
        SELECT id, task_id, channel, recipient, subject, body, retry_count, max_retries, priority
        FROM notification_tasks
        WHERE status IN ('pending', 'failed')
          AND next_retry_at <= NOW()
        ORDER BY
            CASE priority
                WHEN 'critical' THEN 1
                WHEN 'high' THEN 2
                WHEN 'normal' THEN 3
                WHEN 'low' THEN 4
            END,
            created_at ASC
        LIMIT 100
    `

    rows, err := s.db.QueryContext(ctx, query)
    if err != nil {
        return err
    }
    defer rows.Close()

    for rows.Next() {
        var task NotificationTask
        // ... scan and process
    }

    return rows.Err()
}
```

### 設計：限流（Rate Limiting）

```go
package main

import (
    "context"
    "fmt"
    "sync"
    "time"
)

// RateLimiter - 限流器
type RateLimiter struct {
    limits map[string]int // channel -> max per second
    counts map[string]*rateLimitCounter
    mu     sync.Mutex
}

type rateLimitCounter struct {
    count     int
    resetTime time.Time
}

func NewRateLimiter() *RateLimiter {
    return &RateLimiter{
        limits: map[string]int{
            "email": 100,  // 每秒最多 100 封郵件
            "sms":   10,   // 每秒最多 10 條短信
            "push":  1000, // 每秒最多 1000 個推送
        },
        counts: make(map[string]*rateLimitCounter),
    }
}

// Allow - 檢查是否允許發送
func (r *RateLimiter) Allow(channel string) bool {
    r.mu.Lock()
    defer r.mu.Unlock()

    now := time.Now()
    counter, ok := r.counts[channel]

    // 重置計數器（每秒）
    if !ok || now.After(counter.resetTime) {
        r.counts[channel] = &rateLimitCounter{
            count:     0,
            resetTime: now.Add(time.Second),
        }
        counter = r.counts[channel]
    }

    limit := r.limits[channel]
    if counter.count >= limit {
        return false // 超過限流
    }

    counter.count++
    return true
}

// 在發送前檢查限流
func (s *MultiChannelNotificationService) processTaskWithRateLimit(ctx context.Context, task *NotificationTask, limiter *RateLimiter) {
    // 檢查限流
    if !limiter.Allow(task.Channel) {
        // 延遲 1 秒後重試
        nextRetry := time.Now().Add(time.Second)
        query := `UPDATE notification_tasks SET next_retry_at = ? WHERE id = ?`
        s.db.ExecContext(ctx, query, nextRetry, task.ID)
        return
    }

    // 正常處理
    s.processTask(ctx, task)
}
```

**Sarah**: "如果有 10 個 Worker 同時運行，限流器會不準確（每個 Worker 獨立計數）。"

**David**: "可以用 **Redis 分布式限流**。"

### 實現：Redis 分布式限流

```go
package main

import (
    "context"
    "fmt"
    "strconv"
    "time"

    "github.com/go-redis/redis/v8"
)

// RedisRateLimiter - Redis 分布式限流器
type RedisRateLimiter struct {
    client *redis.Client
    limits map[string]int
}

func NewRedisRateLimiter(client *redis.Client) *RedisRateLimiter {
    return &RedisRateLimiter{
        client: client,
        limits: map[string]int{
            "email": 100,
            "sms":   10,
            "push":  1000,
        },
    }
}

// Allow - 使用 Redis 計數器
func (r *RedisRateLimiter) Allow(ctx context.Context, channel string) (bool, error) {
    key := fmt.Sprintf("rate_limit:%s:%d", channel, time.Now().Unix())
    limit := r.limits[channel]

    // 使用 Lua 腳本保證原子性
    script := `
        local current = redis.call('INCR', KEYS[1])
        if current == 1 then
            redis.call('EXPIRE', KEYS[1], 1)
        end
        return current
    `

    result, err := r.client.Eval(ctx, script, []string{key}).Result()
    if err != nil {
        return false, err
    }

    current, _ := strconv.Atoi(fmt.Sprintf("%v", result))
    return current <= limit, nil
}
```

---

## Act 6: 通知模板管理

**Emma**: "我們有幾十種通知場景，每個都要寫代碼太麻煩了。能不能做一個模板系統？"

### 設計：通知模板表

```sql
CREATE TABLE notification_templates (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    template_key VARCHAR(100) UNIQUE NOT NULL,  -- welcome_email, order_confirm, etc.
    channel ENUM('email', 'sms', 'push') NOT NULL,
    language VARCHAR(10) DEFAULT 'en',          -- en, zh, ja, etc.
    subject VARCHAR(255),                        -- 郵件主題（支持變量）
    body TEXT NOT NULL,                          -- 內容（支持變量）
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_template_key (template_key, channel, language)
);

-- 插入示例模板
INSERT INTO notification_templates (template_key, channel, language, subject, body) VALUES
('welcome_email', 'email', 'en', 'Welcome to {{company_name}}!',
 'Hi {{username}},\n\nWelcome to our platform! Click here to get started: {{link}}'),

('order_confirm', 'email', 'en', 'Order #{{order_id}} Confirmed',
 'Hi {{username}},\n\nYour order #{{order_id}} has been confirmed.\nTotal: ${{total}}\n\nThank you!'),

('password_reset', 'sms', 'en', NULL,
 'Your verification code is: {{code}}. Valid for 5 minutes.');
```

### 實現：模板渲染

```go
package main

import (
    "bytes"
    "context"
    "database/sql"
    "fmt"
    "text/template"
)

// NotificationTemplate - 通知模板
type NotificationTemplate struct {
    ID          int64
    TemplateKey string
    Channel     string
    Language    string
    Subject     string
    Body        string
}

// TemplateService - 模板服務
type TemplateService struct {
    db *sql.DB
}

// GetTemplate - 獲取模板
func (s *TemplateService) GetTemplate(ctx context.Context, key, channel, language string) (*NotificationTemplate, error) {
    query := `
        SELECT id, template_key, channel, language, subject, body
        FROM notification_templates
        WHERE template_key = ? AND channel = ? AND language = ?
    `

    var tpl NotificationTemplate
    err := s.db.QueryRowContext(ctx, query, key, channel, language).Scan(
        &tpl.ID, &tpl.TemplateKey, &tpl.Channel, &tpl.Language, &tpl.Subject, &tpl.Body)
    if err != nil {
        return nil, err
    }

    return &tpl, nil
}

// RenderTemplate - 渲染模板
func (s *TemplateService) RenderTemplate(tpl *NotificationTemplate, vars map[string]interface{}) (string, string, error) {
    // 渲染主題
    subjectTpl, err := template.New("subject").Parse(tpl.Subject)
    if err != nil {
        return "", "", fmt.Errorf("failed to parse subject template: %w", err)
    }

    var subjectBuf bytes.Buffer
    if err := subjectTpl.Execute(&subjectBuf, vars); err != nil {
        return "", "", fmt.Errorf("failed to render subject: %w", err)
    }

    // 渲染內容
    bodyTpl, err := template.New("body").Parse(tpl.Body)
    if err != nil {
        return "", "", fmt.Errorf("failed to parse body template: %w", err)
    }

    var bodyBuf bytes.Buffer
    if err := bodyTpl.Execute(&bodyBuf, vars); err != nil {
        return "", "", fmt.Errorf("failed to render body: %w", err)
    }

    return subjectBuf.String(), bodyBuf.String(), nil
}

// 使用示例
func SendWelcomeEmail(ctx context.Context, templateService *TemplateService, notificationService *KafkaNotificationService, userEmail, username string) error {
    // 1. 獲取模板
    tpl, err := templateService.GetTemplate(ctx, "welcome_email", "email", "en")
    if err != nil {
        return err
    }

    // 2. 渲染模板
    vars := map[string]interface{}{
        "company_name": "Our Platform",
        "username":     username,
        "link":         "https://example.com/get-started",
    }

    subject, body, err := templateService.RenderTemplate(tpl, vars)
    if err != nil {
        return err
    }

    // 3. 發送通知
    task := NotificationTask{
        TaskID:    fmt.Sprintf("welcome_%s_%d", userEmail, time.Now().Unix()),
        Channel:   "email",
        Recipient: userEmail,
        Subject:   subject,
        Body:      body,
        MaxRetries: 3,
    }

    return notificationService.SendNotification(ctx, task)
}
```

**Emma**: "太好了！現在產品經理可以直接修改模板，不用找工程師了。"

---

## Act 7: 用戶偏好設置和退訂

**場景：用戶投訴**

**Emma**: "我們收到用戶投訴：他們收到太多營銷郵件，想要退訂。我們需要支持用戶偏好設置。"

### 設計：用戶通知偏好表

```sql
CREATE TABLE user_notification_preferences (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    category VARCHAR(50) NOT NULL,              -- marketing, transactional, social, etc.
    channel ENUM('email', 'sms', 'push') NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_category_channel (user_id, category, channel)
);

-- 插入默認偏好
INSERT INTO user_notification_preferences (user_id, category, channel, enabled) VALUES
('user123', 'transactional', 'email', TRUE),   -- 交易通知（必須）
('user123', 'marketing', 'email', TRUE),       -- 營銷通知（可退訂）
('user123', 'social', 'push', TRUE);           -- 社交通知（可退訂）
```

### 實現：檢查用戶偏好

```go
package main

import (
    "context"
    "database/sql"
)

// PreferenceService - 偏好服務
type PreferenceService struct {
    db *sql.DB
}

// IsNotificationEnabled - 檢查用戶是否啟用該通知
func (s *PreferenceService) IsNotificationEnabled(ctx context.Context, userID, category, channel string) (bool, error) {
    query := `
        SELECT enabled
        FROM user_notification_preferences
        WHERE user_id = ? AND category = ? AND channel = ?
    `

    var enabled bool
    err := s.db.QueryRowContext(ctx, query, userID, category, channel).Scan(&enabled)
    if err == sql.ErrNoRows {
        // 如果沒有記錄，默認啟用（transactional 除外）
        if category == "transactional" {
            return true, nil
        }
        return true, nil
    }
    if err != nil {
        return false, err
    }

    return enabled, nil
}

// UpdatePreference - 更新用戶偏好
func (s *PreferenceService) UpdatePreference(ctx context.Context, userID, category, channel string, enabled bool) error {
    query := `
        INSERT INTO user_notification_preferences (user_id, category, channel, enabled)
        VALUES (?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE enabled = ?
    `
    _, err := s.db.ExecContext(ctx, query, userID, category, channel, enabled, enabled)
    return err
}

// 在發送通知前檢查偏好
func SendNotificationWithPreference(ctx context.Context, prefService *PreferenceService, notifService *KafkaNotificationService, userID, category string, task NotificationTask) error {
    // 1. 檢查用戶偏好
    enabled, err := prefService.IsNotificationEnabled(ctx, userID, category, task.Channel)
    if err != nil {
        return err
    }

    if !enabled {
        // 用戶已退訂該類型通知
        return fmt.Errorf("user %s has disabled %s notifications via %s", userID, category, task.Channel)
    }

    // 2. 發送通知
    return notifService.SendNotification(ctx, task)
}
```

### 實現：一鍵退訂鏈接

```go
package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "fmt"
    "net/http"
    "time"
)

// UnsubscribeTokenGenerator - 退訂令牌生成器
type UnsubscribeTokenGenerator struct {
    secretKey []byte
}

// GenerateToken - 生成退訂令牌（帶簽名防偽造）
func (g *UnsubscribeTokenGenerator) GenerateToken(userID, category, channel string) string {
    data := fmt.Sprintf("%s:%s:%s:%d", userID, category, channel, time.Now().Unix())

    h := hmac.New(sha256.New, g.secretKey)
    h.Write([]byte(data))
    signature := base64.URLEncoding.EncodeToString(h.Sum(nil))

    token := base64.URLEncoding.EncodeToString([]byte(data + ":" + signature))
    return token
}

// 在郵件中添加退訂鏈接
func AddUnsubscribeLink(body, userID, category, channel string, generator *UnsubscribeTokenGenerator) string {
    token := generator.GenerateToken(userID, category, channel)
    unsubscribeURL := fmt.Sprintf("https://example.com/unsubscribe?token=%s", token)

    return body + fmt.Sprintf("\n\nDon't want these emails? [Unsubscribe](%s)", unsubscribeURL)
}

// HTTP Handler: 處理退訂請求
func UnsubscribeHandler(w http.ResponseWriter, r *http.Request, prefService *PreferenceService) {
    token := r.URL.Query().Get("token")

    // 解析令牌（省略驗證簽名邏輯）
    // userID, category, channel := parseToken(token)

    userID := "user123"
    category := "marketing"
    channel := "email"

    // 更新偏好
    ctx := r.Context()
    if err := prefService.UpdatePreference(ctx, userID, category, channel, false); err != nil {
        http.Error(w, "Failed to unsubscribe", http.StatusInternalServerError)
        return
    }

    w.Write([]byte("You have been unsubscribed successfully."))
}
```

---

## Act 8: 通知去重和合併

**場景：用戶抱怨收到重複通知**

**Jennifer**: "我在 Instagram 點了 100 個贊，收到 100 個推送通知，手機都震麻了！"

**Emma**: "我們需要**通知去重**和**合併**策略。"

### 策略 1：時間窗口去重

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/go-redis/redis/v8"
)

// DeduplicationService - 去重服務
type DeduplicationService struct {
    redis *redis.Client
}

// ShouldSend - 判斷是否應該發送（時間窗口去重）
func (s *DeduplicationService) ShouldSend(ctx context.Context, userID, notificationType string, windowSeconds int) (bool, error) {
    key := fmt.Sprintf("notif_dedup:%s:%s", userID, notificationType)

    // 嘗試設置 key（如果不存在）
    ok, err := s.redis.SetNX(ctx, key, "1", time.Duration(windowSeconds)*time.Second).Result()
    if err != nil {
        return false, err
    }

    return ok, nil // true = 可以發送, false = 重複（跳過）
}

// 使用示例：同一類型通知 5 分鐘內只發送一次
func SendWithDeduplication(ctx context.Context, dedupService *DeduplicationService, notifService *KafkaNotificationService, userID string, task NotificationTask) error {
    notificationType := task.Channel + ":" + task.Subject

    shouldSend, err := dedupService.ShouldSend(ctx, userID, notificationType, 300)
    if err != nil {
        return err
    }

    if !shouldSend {
        fmt.Printf("Skipping duplicate notification for user %s\n", userID)
        return nil
    }

    return notifService.SendNotification(ctx, task)
}
```

### 策略 2：批量合併通知

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/go-redis/redis/v8"
)

// NotificationAggregator - 通知聚合器
type NotificationAggregator struct {
    redis *redis.Client
}

// AddEvent - 添加事件（等待合併）
func (a *NotificationAggregator) AddEvent(ctx context.Context, userID, eventType, eventData string) error {
    key := fmt.Sprintf("notif_agg:%s:%s", userID, eventType)

    // 添加到列表
    if err := a.redis.LPush(ctx, key, eventData).Err(); err != nil {
        return err
    }

    // 設置過期時間（5 分鐘）
    a.redis.Expire(ctx, key, 5*time.Minute)

    return nil
}

// FlushAndSend - 定時任務：合併發送（每 5 分鐘執行一次）
func (a *NotificationAggregator) FlushAndSend(ctx context.Context, notifService *KafkaNotificationService) error {
    // 掃描所有聚合 key
    keys, err := a.redis.Keys(ctx, "notif_agg:*").Result()
    if err != nil {
        return err
    }

    for _, key := range keys {
        // 獲取所有事件
        events, err := a.redis.LRange(ctx, key, 0, -1).Result()
        if err != nil {
            continue
        }

        if len(events) == 0 {
            continue
        }

        // 解析 key: notif_agg:user123:like
        // userID, eventType := parseKey(key)
        userID := "user123"
        eventType := "like"

        // 合併通知
        var body string
        if len(events) == 1 {
            body = fmt.Sprintf("Someone liked your post")
        } else {
            body = fmt.Sprintf("%d people liked your post", len(events))
        }

        task := NotificationTask{
            TaskID:    fmt.Sprintf("agg_%s_%d", key, time.Now().Unix()),
            Channel:   "push",
            Recipient: userID,
            Subject:   "New Activity",
            Body:      body,
        }

        // 發送通知
        notifService.SendNotification(ctx, task)

        // 刪除已處理的事件
        a.redis.Del(ctx, key)
    }

    return nil
}

// 使用示例：用戶點贊事件
func OnUserLiked(ctx context.Context, aggregator *NotificationAggregator, postOwnerID, likerName string) error {
    return aggregator.AddEvent(ctx, postOwnerID, "like", likerName)
}
```

**Emma**: "完美！現在用戶不會被重複通知轟炸了。"

---

## Act 9: 通知追蹤和分析

**Emma**: "我們需要知道通知的效果：有多少用戶打開了郵件？點擊了鏈接？"

### 設計：通知事件表

```sql
CREATE TABLE notification_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    task_id VARCHAR(64) NOT NULL,               -- 關聯 notification_tasks
    user_id VARCHAR(64) NOT NULL,
    event_type ENUM('sent', 'delivered', 'opened', 'clicked', 'bounced', 'unsubscribed'),
    event_data JSON,                             -- 額外數據（如點擊的鏈接）
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_task_id (task_id),
    INDEX idx_user_id (user_id, created_at DESC),
    INDEX idx_event_type (event_type, created_at DESC)
);
```

### 實現：追蹤像素（Email Open Tracking）

```go
package main

import (
    "fmt"
    "net/http"
)

// AddTrackingPixel - 在郵件中添加追蹤像素
func AddTrackingPixel(body, taskID string) string {
    trackingURL := fmt.Sprintf("https://example.com/track/open?task_id=%s", taskID)
    pixel := fmt.Sprintf(`<img src="%s" width="1" height="1" />`, trackingURL)
    return body + pixel
}

// TrackOpenHandler - 處理郵件打開事件
func TrackOpenHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
    taskID := r.URL.Query().Get("task_id")

    // 記錄打開事件
    query := `
        INSERT INTO notification_events (task_id, user_id, event_type)
        SELECT task_id, (SELECT user_id FROM notification_tasks WHERE task_id = ?), 'opened'
        FROM notification_tasks WHERE task_id = ? LIMIT 1
    `
    db.ExecContext(r.Context(), query, taskID, taskID)

    // 返回 1x1 透明像素
    w.Header().Set("Content-Type", "image/gif")
    w.Write([]byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x21, 0xF9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2C, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3B})
}
```

### 實現：點擊追蹤（Link Tracking）

```go
package main

import (
    "crypto/md5"
    "fmt"
    "net/http"
    "net/url"
)

// AddClickTracking - 替換郵件中的所有鏈接為追蹤鏈接
func AddClickTracking(body, taskID string) string {
    // 簡化示例：替換第一個鏈接
    originalURL := "https://example.com/product/123"
    trackingURL := fmt.Sprintf("https://example.com/track/click?task_id=%s&url=%s",
        taskID, url.QueryEscape(originalURL))

    return fmt.Sprintf(body, trackingURL)
}

// TrackClickHandler - 處理鏈接點擊事件
func TrackClickHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
    taskID := r.URL.Query().Get("task_id")
    targetURL := r.URL.Query().Get("url")

    // 記錄點擊事件
    query := `
        INSERT INTO notification_events (task_id, user_id, event_type, event_data)
        SELECT task_id,
               (SELECT user_id FROM notification_tasks WHERE task_id = ?),
               'clicked',
               ?
        FROM notification_tasks WHERE task_id = ? LIMIT 1
    `
    eventData := fmt.Sprintf(`{"url": "%s"}`, targetURL)
    db.ExecContext(r.Context(), query, taskID, eventData, taskID)

    // 重定向到原始 URL
    http.Redirect(w, r, targetURL, http.StatusFound)
}
```

### 分析查詢

```sql
-- 郵件打開率
SELECT
    DATE(nt.created_at) AS date,
    COUNT(DISTINCT nt.id) AS sent,
    COUNT(DISTINCT CASE WHEN ne.event_type = 'opened' THEN ne.task_id END) AS opened,
    ROUND(COUNT(DISTINCT CASE WHEN ne.event_type = 'opened' THEN ne.task_id END) * 100.0 / COUNT(DISTINCT nt.id), 2) AS open_rate
FROM notification_tasks nt
LEFT JOIN notification_events ne ON nt.task_id = ne.task_id
WHERE nt.channel = 'email'
  AND nt.status = 'sent'
  AND nt.created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
GROUP BY DATE(nt.created_at);

-- 點擊率
SELECT
    COUNT(DISTINCT CASE WHEN event_type = 'clicked' THEN task_id END) AS clicked,
    ROUND(COUNT(DISTINCT CASE WHEN event_type = 'clicked' THEN task_id END) * 100.0 / COUNT(DISTINCT task_id), 2) AS click_rate
FROM notification_events
WHERE event_type IN ('opened', 'clicked')
  AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY);
```

---

## Act 10: 橫向擴展和高可用

**Michael**: "我們的用戶增長到 1000 萬了，單個 Worker 處理不過來。我們需要橫向擴展。"

### 最終架構

```
┌─────────────────────────────────────────────────────────────┐
│                         API Gateway                          │
│              (Create Notification Request)                   │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌────────────────────────────────────────────────────────────┐
│                   Notification Service                      │
│  1. Check User Preference (Redis Cache + MySQL)            │
│  2. Render Template                                        │
│  3. Insert to DB (notification_tasks)                      │
│  4. Publish to Kafka (notification.tasks)                  │
└────────────────────────┬───────────────────────────────────┘
                         ↓
                    ┌────────┐
                    │ Kafka  │
                    │ Topic  │
                    └───┬────┘
                        ↓
        ┌───────────────┼───────────────┐
        ↓               ↓               ↓
   ┌─────────┐     ┌─────────┐     ┌─────────┐
   │Worker 1 │     │Worker 2 │     │Worker N │
   │ Email   │     │  SMS    │     │  Push   │
   └────┬────┘     └────┬────┘     └────┬────┘
        │               │               │
        ↓               ↓               ↓
   ┌─────────┐     ┌─────────┐     ┌─────────┐
   │AWS SES  │     │ Twilio  │     │FCM/APNs │
   └─────────┘     └─────────┘     └─────────┘

Cron Job (每 1 分鐘):
   ↓
Scan DB for failed tasks (retry)
```

### 實現：Kafka 消費者組（水平擴展）

```go
package main

import (
    "context"
    "encoding/json"
    "log"

    "github.com/segmentio/kafka-go"
)

// ScalableNotificationWorker - 可擴展的 Worker
type ScalableNotificationWorker struct {
    reader  *kafka.Reader
    service *MultiChannelNotificationService
    limiter *RedisRateLimiter
    dedup   *DeduplicationService
}

func NewScalableNotificationWorker(kafkaBrokers []string, service *MultiChannelNotificationService, limiter *RedisRateLimiter, dedup *DeduplicationService) *ScalableNotificationWorker {
    return &ScalableNotificationWorker{
        reader: kafka.NewReader(kafka.ReaderConfig{
            Brokers: kafkaBrokers,
            Topic:   "notification.tasks",
            GroupID: "notification-workers", // 同一個 GroupID 實現負載均衡
            MinBytes: 10e3, // 10KB
            MaxBytes: 10e6, // 10MB
        }),
        service: service,
        limiter: limiter,
        dedup:   dedup,
    }
}

func (w *ScalableNotificationWorker) Start(ctx context.Context) error {
    for {
        msg, err := w.reader.FetchMessage(ctx)
        if err != nil {
            log.Printf("Failed to fetch message: %v", err)
            continue
        }

        var task NotificationTask
        if err := json.Unmarshal(msg.Value, &task); err != nil {
            log.Printf("Failed to unmarshal task: %v", err)
            w.reader.CommitMessages(ctx, msg) // 提交錯誤消息（避免重複處理）
            continue
        }

        // 處理任務
        if err := w.processTaskWithGuards(ctx, &task); err != nil {
            log.Printf("Failed to process task %s: %v", task.TaskID, err)
            // 不提交，讓 Kafka 重新投遞
            continue
        }

        // 提交 offset
        w.reader.CommitMessages(ctx, msg)
    }
}

func (w *ScalableNotificationWorker) processTaskWithGuards(ctx context.Context, task *NotificationTask) error {
    // 1. 限流檢查
    allowed, err := w.limiter.Allow(ctx, task.Channel)
    if err != nil || !allowed {
        // 延遲重試
        return fmt.Errorf("rate limit exceeded")
    }

    // 2. 去重檢查（可選）
    // shouldSend, _ := w.dedup.ShouldSend(ctx, task.Recipient, task.Channel, 300)
    // if !shouldSend {
    //     return nil
    // }

    // 3. 發送通知
    w.service.processTask(ctx, task)

    return nil
}
```

### 高可用：多區域部署

```yaml
# Kubernetes Deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: notification-worker
spec:
  replicas: 10  # 10 個 Worker 實例
  selector:
    matchLabels:
      app: notification-worker
  template:
    metadata:
      labels:
        app: notification-worker
    spec:
      containers:
      - name: worker
        image: notification-worker:latest
        env:
        - name: KAFKA_BROKERS
          value: "kafka-1:9092,kafka-2:9092,kafka-3:9092"
        - name: REDIS_ADDR
          value: "redis-cluster:6379"
        - name: DB_DSN
          valueFrom:
            secretKeyRef:
              name: db-secret
              key: dsn
        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
          limits:
            memory: "1Gi"
            cpu: "1000m"
```

### 監控指標

```go
package main

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    notificationsSent = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "notifications_sent_total",
            Help: "Total number of notifications sent",
        },
        []string{"channel", "status"}, // email/sms/push, success/failed
    )

    notificationLatency = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "notification_latency_seconds",
            Help:    "Notification processing latency",
            Buckets: prometheus.DefBuckets,
        },
        []string{"channel"},
    )
)

func (s *MultiChannelNotificationService) processTaskWithMetrics(ctx context.Context, task *NotificationTask) {
    timer := prometheus.NewTimer(notificationLatency.WithLabelValues(task.Channel))
    defer timer.ObserveDuration()

    // 處理任務
    s.processTask(ctx, task)

    // 記錄指標
    if task.Status == "sent" {
        notificationsSent.WithLabelValues(task.Channel, "success").Inc()
    } else {
        notificationsSent.WithLabelValues(task.Channel, "failed").Inc()
    }
}
```

---

## 總結與回顧

**Emma**: "我們從一個簡單的同步郵件發送，演進到了一個完整的多渠道通知系統。讓我們回顧一下關鍵設計決策。"

### 演進歷程

1. **Act 1**: 同步發送 → 異步發送（內存隊列）
2. **Act 2**: 持久化（MySQL）+ 重試機制
3. **Act 3**: 引入 Kafka（實時性 + 可靠性）
4. **Act 4**: 多渠道支持（Email、SMS、Push）
5. **Act 5**: 優先級和限流
6. **Act 6**: 模板管理（靈活配置）
7. **Act 7**: 用戶偏好和退訂
8. **Act 8**: 去重和合併（防止騷擾）
9. **Act 9**: 追蹤和分析（數據驅動）
10. **Act 10**: 橫向擴展和高可用

### 核心設計原則

1. **可靠性優先**：數據庫持久化 + Kafka + 重試機制
2. **異步解耦**：不阻塞主流程
3. **多渠道統一**：統一接口，易於擴展
4. **尊重用戶**：偏好設置 + 退訂機制 + 去重
5. **可觀測性**：追蹤、監控、告警
6. **橫向擴展**：Kafka 消費者組 + 無狀態設計

### 關鍵技術選型

| 組件 | 技術 | 原因 |
|------|------|------|
| 隊列 | Kafka | 高吞吐、持久化、消費者組支持 |
| 數據庫 | MySQL | 事務支持、查詢靈活 |
| 緩存 | Redis | 限流、去重、偏好緩存 |
| 郵件 | AWS SES | 成本低、可靠性高 |
| 短信 | Twilio | API 簡單、覆蓋廣 |
| 推送 | FCM/APNs | 官方服務、免費 |

### 性能指標

```
系統容量（10 Worker）：
- 郵件：1000 封/秒
- 短信：100 條/秒
- 推送：10,000 個/秒

延遲：
- P50: 200ms
- P99: 2s

可靠性：
- 送達率：99.9%+（3 次重試）
- 數據丟失率：0%（持久化）
```

### 成本估算

```
100 萬 DAU，平均每天發送 5 個通知：

郵件（50% 通知）：
- 250 萬封/天 × $0.0001 = $250/天 = $7,500/月

短信（10% 通知）：
- 50 萬條/天 × $0.01 = $5,000/天 = $150,000/月

推送（40% 通知）：
- 200 萬個/天 × $0 = $0/月

基礎設施：
- Kafka (3 節點): $500/月
- Redis: $200/月
- MySQL: $300/月
- Worker (10 台): $1,000/月

總計：約 $159,500/月
單用戶成本：$0.16/月
```

### 真實案例：Uber 的通知系統

**David**: "Uber 每天發送數億條通知，他們的架構值得學習。"

```
Uber 的通知架構：
1. 統一網關（Notification Gateway）
2. 智能路由（根據用戶偏好選擇渠道）
3. 模板引擎（支持 A/B 測試）
4. 批量合併（行程更新合併為一條）
5. 多語言支持（200+ 國家）
6. 實時監控（Prometheus + Grafana）

關鍵優化：
- 使用 Apache Pinot 做分析（億級查詢）
- 推送通知優先（比短信便宜 100 倍）
- 智能降級（高峰期降低優先級低的通知）
```

### 常見坑

1. **郵件進垃圾箱**：配置 SPF、DKIM、DMARC
2. **推送 Token 過期**：定期清理無效 Token
3. **限流被封**：遵守第三方服務的 Rate Limit
4. **時區問題**：統一使用 UTC，展示時轉換
5. **隱私合規**：GDPR 要求用戶可刪除所有數據

---

## 練習題

1. **設計題**：如何實現通知的 A/B 測試？（不同用戶看到不同版本的通知內容）
2. **優化題**：如何減少郵件進垃圾箱的概率？
3. **擴展題**：如何支持富文本推送（圖片、按鈕）？
4. **故障恢復**：如果 Kafka 宕機 1 小時，如何保證通知不丟失？
5. **成本優化**：如何將短信成本降低 50%？（提示：智能降級到推送）

---

## 延伸閱讀

- [AWS SES 最佳實踐](https://docs.aws.amazon.com/ses/latest/dg/best-practices.html)
- [Twilio SMS API](https://www.twilio.com/docs/sms)
- [Firebase Cloud Messaging](https://firebase.google.com/docs/cloud-messaging)
- [Apple Push Notification Service](https://developer.apple.com/documentation/usernotifications)
- [Uber's Notification Platform](https://eng.uber.com/notification-platform/)
- [Airbnb's Notification System](https://medium.com/airbnb-engineering/scaling-airbnbs-notification-system-7a7d6f0e0fb4)

**核心理念：可靠、尊重用戶、可擴展！**
