# Notification Service - 通知服務

> 多渠道統一通知系統：Email、SMS、Push、In-App 的設計與實現

## 概述

本章節展示如何設計一個生產級的**多渠道通知服務（Notification Service）**，支持：
- **多渠道**：Email、SMS、Push Notification、In-App
- **高可靠**：持久化存儲、重試機制、送達率 99.9%+
- **可擴展**：Kafka + 消費者組，水平擴展
- **智能化**：優先級、限流、去重、合併
- **用戶友好**：偏好設置、退訂機制、多語言支持
- **可觀測**：追蹤（打開率、點擊率）、監控、告警

## 學習目標

- 理解 **Email、SMS、Push、In-App** 四種渠道的特點和選擇
- 掌握 **異步發送** 和 **持久化隊列** 的設計
- 學習 **Kafka + MySQL** 雙重保障機制
- 實踐 **重試機制**（指數退避）
- 了解 **第三方服務集成**（AWS SES、Twilio、FCM、APNs）
- 掌握 **通知優先級** 和 **限流**（Redis 分布式限流）
- 學習 **通知模板管理** 和 **個性化渲染**
- 實踐 **用戶偏好設置** 和 **退訂機制**
- 了解 **通知去重** 和 **批量合併** 策略
- 學習 **追蹤和分析**（打開率、點擊率）
- 掌握 **橫向擴展** 和 **高可用** 設計
- 學習 Uber、Airbnb 的真實案例

## 核心概念

### 1. 通知渠道對比

| 渠道 | 延遲 | 成本 | 到達率 | 適用場景 |
|------|------|------|--------|----------|
| **Email** | 秒級 | $0.0001/封 | 85-95% | 交易確認、週報、營銷 |
| **SMS** | 秒級 | $0.01/條 | 98% | 驗證碼、緊急通知 |
| **Push** | 毫秒級 | 免費 | 90% (需在線) | 實時提醒、社交互動 |
| **In-App** | 毫秒級 | 免費 | 100% (在線時) | 站內消息、系統通知 |

**選擇建議**：
- **關鍵通知**（密碼重置）：SMS > Email > Push
- **實時通知**（點贊、評論）：Push > In-App
- **營銷通知**：Email（成本最低）
- **多渠道備份**：Email + Push（提高到達率）

### 2. 架構演進

#### 階段 1：同步發送（不推薦）

```
用戶註冊 → 調用 SMTP 發送郵件（阻塞 3 秒）→ 返回註冊成功

問題：
❌ 延遲高（用戶等待）
❌ 可靠性差（SMTP 故障 = 註冊失敗）
```

#### 階段 2：異步發送（內存隊列）

```
用戶註冊 → 加入內存隊列 → 立即返回
          ↓
       後台 Worker 發送郵件

問題：
❌ 內存隊列不持久（服務重啟 = 任務丟失）
❌ 無重試機制
```

#### 階段 3：持久化 + 重試

```
用戶註冊 → 寫入 MySQL (notification_tasks)
          ↓
       定時任務掃描 pending 任務
          ↓
       發送通知（失敗自動重試）

優勢：
✅ 持久化（不丟失）
✅ 重試機制（指數退避）

問題：
⚠️ 延遲高（輪詢間隔）
```

#### 階段 4：Kafka + MySQL（推薦）

```
API 接口
   ↓
1. 寫入 MySQL（持久化）
2. 發送到 Kafka（實時隊列）
   ↓
Worker 消費 Kafka
   ↓
發送通知（Email/SMS/Push）
   ↓
更新 MySQL 狀態（sent/failed）

兜底機制：
定時任務掃描 MySQL 的 pending 任務（Kafka 失敗時補償）

優勢：
✅ 實時性（Kafka 毫秒級）
✅ 可靠性（MySQL 持久化）
✅ 可擴展（Kafka 消費者組）
```

### 3. 重試策略（指數退避）

```
發送失敗 → 等待 1 分鐘 → 重試 (1/3)
         ↓ 失敗
         等待 2 分鐘 → 重試 (2/3)
         ↓ 失敗
         等待 4 分鐘 → 重試 (3/3)
         ↓ 失敗
         標記為 failed（人工介入）

優勢：
- 避免雪崩（瞬時大量重試）
- 給第三方服務恢復時間
```

### 4. 通知優先級

```sql
priority ENUM('critical', 'high', 'normal', 'low')

critical: 密碼重置、安全告警（立即發送）
high:     訂單確認、支付成功（5 分鐘內）
normal:   社交通知、點贊評論（10 分鐘內）
low:      營銷郵件、週報（可延遲數小時）

查詢時按優先級排序：
ORDER BY priority DESC, created_at ASC
```

### 5. 限流（Rate Limiting）

```
為什麼需要限流？
1. 第三方服務限制（AWS SES: 1000 封/秒）
2. 成本控制（短信很貴）
3. 防止被封（發送過快 = 垃圾郵件）

實現：Redis 分布式限流

INCR rate_limit:email:1733160000  # 當前秒的計數器
EXPIRE rate_limit:email:1733160000 1  # 1 秒後過期

if counter > 100:  # 超過每秒 100 封
    return False  # 限流
```

### 6. 通知去重和合併

#### 去重（Deduplication）

```
場景：用戶點擊註冊按鈕 3 次

方案：Redis 時間窗口
key = "notif_dedup:user123:welcome_email"
SETNX key 1 EX 300  # 5 分鐘內只發一次

if key exists:
    skip  # 跳過重複通知
```

#### 合併（Aggregation）

```
場景：Instagram 收到 100 個贊

方案：批量聚合（5 分鐘窗口）
1. 收到贊事件 → 加入 Redis List
2. 定時任務（5 分鐘）：
   - 讀取所有贊事件
   - 合併為一條通知："Alice and 99 others liked your post"
   - 發送
```

### 7. 用戶偏好設置

```sql
CREATE TABLE user_notification_preferences (
    user_id VARCHAR(64),
    category VARCHAR(50),  -- marketing, transactional, social
    channel ENUM('email', 'sms', 'push'),
    enabled BOOLEAN DEFAULT TRUE
);

發送前檢查：
if not is_enabled(user_id, 'marketing', 'email'):
    skip  # 用戶已退訂營銷郵件
```

### 8. 通知追蹤

#### Email 打開追蹤

```html
<!-- 在郵件底部添加 1x1 透明像素 -->
<img src="https://example.com/track/open?task_id=12345" width="1" height="1" />

用戶打開郵件 → 瀏覽器加載圖片 → 記錄 opened 事件
```

#### 點擊追蹤

```
原始鏈接：https://example.com/product/123

追蹤鏈接：https://example.com/track/click?task_id=12345&url=...

用戶點擊 → 記錄 clicked 事件 → 重定向到原始鏈接
```

#### 分析指標

```
打開率 = opened / sent * 100%
點擊率 = clicked / opened * 100%
轉化率 = converted / clicked * 100%
```

## 技術棧

- **語言**: Golang (標準庫優先)
- **消息隊列**: Kafka（實時隊列）
- **數據庫**: MySQL/PostgreSQL（持久化）
- **緩存**: Redis（限流、去重、偏好緩存）
- **郵件服務**: AWS SES, SendGrid
- **短信服務**: Twilio, AWS SNS
- **推送服務**: Firebase Cloud Messaging (FCM), Apple Push Notification Service (APNs)
- **監控**: Prometheus + Grafana
- **日誌**: ELK Stack

## 架構設計

### 最終架構圖

```
┌──────────────────────────────────────────────────────┐
│                    API Gateway                        │
│          POST /notifications/send                     │
└────────────────────┬─────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────┐
│             Notification Service                     │
│  1. Check User Preference (Redis + MySQL)           │
│  2. Check Deduplication (Redis)                     │
│  3. Render Template                                 │
│  4. Insert to DB (notification_tasks)               │
│  5. Publish to Kafka                                │
└────────────────────┬────────────────────────────────┘
                     ↓
                ┌────────┐
                │ Kafka  │
                │ Topic  │
                └───┬────┘
                    ↓
    ┌───────────────┼───────────────┐
    ↓               ↓               ↓
┌─────────┐    ┌─────────┐    ┌─────────┐
│Worker 1 │    │Worker 2 │    │Worker N │
│ Email   │    │  SMS    │    │  Push   │
└────┬────┘    └────┬────┘    └────┬────┘
     │              │              │
     ↓              ↓              ↓
┌─────────┐    ┌─────────┐    ┌─────────┐
│AWS SES  │    │ Twilio  │    │FCM/APNs │
└─────────┘    └─────────┘    └─────────┘

Cron Job (每 1 分鐘):
   ↓
Scan DB for failed/pending tasks (retry/compensation)
```

## 項目結構

```
17-notification-service/
├── DESIGN.md              # 詳細設計文檔（蘇格拉底式教學）
├── README.md              # 本文件
├── cmd/
│   └── server/
│       └── main.go        # HTTP 服務器 + Kafka Producer
├── internal/
│   ├── service.go         # 通知服務（創建任務）
│   ├── worker.go          # Kafka Consumer（處理任務）
│   ├── senders/
│   │   ├── email.go       # 郵件發送器（AWS SES）
│   │   ├── sms.go         # 短信發送器（Twilio）
│   │   └── push.go        # 推送發送器（FCM/APNs）
│   ├── template.go        # 模板管理和渲染
│   ├── preference.go      # 用戶偏好管理
│   ├── ratelimit.go       # 限流器（Redis）
│   ├── dedup.go           # 去重服務（Redis）
│   ├── aggregator.go      # 通知聚合器
│   └── tracker.go         # 追蹤服務（打開、點擊）
└── docs/
    ├── api.md             # API 文檔
    └── uber-case.md       # Uber 案例研究
```

## 快速開始

### 1. 數據庫設計

```sql
-- 通知任務表
CREATE TABLE notification_tasks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    task_id VARCHAR(64) UNIQUE NOT NULL,        -- 任務 ID（冪等性）
    user_id VARCHAR(64) NOT NULL,
    channel ENUM('email', 'sms', 'push', 'in_app') NOT NULL,
    category VARCHAR(50),                        -- marketing, transactional, social
    priority ENUM('critical', 'high', 'normal', 'low') DEFAULT 'normal',
    recipient VARCHAR(255) NOT NULL,             -- 接收者（郵箱/手機/設備 Token）
    subject VARCHAR(255),
    body TEXT NOT NULL,
    status ENUM('pending', 'sending', 'sent', 'failed') DEFAULT 'pending',
    retry_count INT DEFAULT 0,
    max_retries INT DEFAULT 3,
    next_retry_at TIMESTAMP,
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_status_next_retry (status, next_retry_at),
    INDEX idx_priority (priority DESC, created_at ASC),
    INDEX idx_user_id (user_id, created_at DESC),
    INDEX idx_task_id (task_id)
);

-- 通知模板表
CREATE TABLE notification_templates (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    template_key VARCHAR(100) UNIQUE NOT NULL,
    channel ENUM('email', 'sms', 'push', 'in_app') NOT NULL,
    language VARCHAR(10) DEFAULT 'en',
    subject VARCHAR(255),
    body TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_template_key (template_key, channel, language)
);

-- 用戶通知偏好表
CREATE TABLE user_notification_preferences (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    category VARCHAR(50) NOT NULL,
    channel ENUM('email', 'sms', 'push', 'in_app') NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_category_channel (user_id, category, channel)
);

-- 用戶設備表（Push Token）
CREATE TABLE user_devices (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    device_id VARCHAR(128) UNIQUE NOT NULL,
    device_type ENUM('ios', 'android', 'web'),
    push_token VARCHAR(255),
    status ENUM('active', 'inactive') DEFAULT 'active',
    last_active_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_id (user_id),
    INDEX idx_push_token (push_token)
);

-- 通知事件表（追蹤）
CREATE TABLE notification_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    task_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    event_type ENUM('sent', 'delivered', 'opened', 'clicked', 'bounced', 'unsubscribed'),
    event_data JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_task_id (task_id),
    INDEX idx_user_id (user_id, created_at DESC),
    INDEX idx_event_type (event_type, created_at DESC)
);
```

### 2. Redis 設計

```bash
# 限流（每渠道每秒）
# Key: rate_limit:{channel}:{timestamp}
# Value: counter
SET rate_limit:email:1733160000 95
EXPIRE rate_limit:email:1733160000 1  # 1 秒過期

# 去重（時間窗口）
# Key: notif_dedup:{user_id}:{notification_type}
# Value: 1
SETEX notif_dedup:user123:welcome_email 300 1  # 5 分鐘

# 通知聚合（批量合併）
# Key: notif_agg:{user_id}:{event_type}
# Value: List of events
LPUSH notif_agg:user123:like "Alice"
EXPIRE notif_agg:user123:like 300  # 5 分鐘窗口

# 用戶偏好緩存
# Key: pref:{user_id}:{category}:{channel}
# Value: 1 (enabled) or 0 (disabled)
SET pref:user123:marketing:email 1
EXPIRE pref:user123:marketing:email 3600  # 1 小時
```

### 3. API 示例

#### 3.1 發送通知

```bash
POST /notifications/send
Content-Type: application/json

{
  "user_id": "user123",
  "template_key": "welcome_email",
  "channel": "email",
  "category": "transactional",
  "priority": "high",
  "variables": {
    "username": "Alice",
    "company_name": "Our Platform",
    "link": "https://example.com/get-started"
  }
}

# 響應
{
  "task_id": "notif_abc123",
  "status": "pending",
  "message": "Notification task created successfully"
}
```

#### 3.2 批量發送

```bash
POST /notifications/send-batch
Content-Type: application/json

{
  "template_key": "order_confirm",
  "channel": "email",
  "category": "transactional",
  "recipients": [
    {
      "user_id": "user123",
      "email": "alice@example.com",
      "variables": {"order_id": "12345", "total": "99.99"}
    },
    {
      "user_id": "user456",
      "email": "bob@example.com",
      "variables": {"order_id": "12346", "total": "149.99"}
    }
  ]
}
```

#### 3.3 更新用戶偏好

```bash
PUT /notifications/preferences
Content-Type: application/json

{
  "user_id": "user123",
  "category": "marketing",
  "channel": "email",
  "enabled": false  # 退訂營銷郵件
}
```

#### 3.4 查詢通知狀態

```bash
GET /notifications/tasks/notif_abc123

# 響應
{
  "task_id": "notif_abc123",
  "status": "sent",
  "channel": "email",
  "recipient": "alice@example.com",
  "sent_at": "2025-01-15T10:30:00Z",
  "events": [
    {"type": "sent", "created_at": "2025-01-15T10:30:00Z"},
    {"type": "delivered", "created_at": "2025-01-15T10:30:05Z"},
    {"type": "opened", "created_at": "2025-01-15T10:35:12Z"}
  ]
}
```

#### 3.5 獲取分析報告

```bash
GET /notifications/analytics?start_date=2025-01-01&end_date=2025-01-15

# 響應
{
  "total_sent": 1000000,
  "by_channel": {
    "email": {"sent": 500000, "delivered": 475000, "opened": 150000, "clicked": 30000},
    "sms": {"sent": 100000, "delivered": 98000},
    "push": {"sent": 400000, "delivered": 360000, "opened": 100000}
  },
  "open_rate": "31.6%",
  "click_rate": "6.3%"
}
```

## 性能指標

```
系統容量（10 Worker，每個 4 核 8GB）：

吞吐量：
- Email: 1,000 封/秒（受 AWS SES 限制）
- SMS: 100 條/秒（受 Twilio 限制）
- Push: 10,000 個/秒

延遲：
- 創建任務: P50 < 50ms, P99 < 200ms
- 發送通知: P50 < 500ms, P99 < 3s（包含第三方 API 調用）

可靠性：
- 數據丟失率: 0%（MySQL 持久化 + Kafka）
- 送達率: 99.9%+（3 次重試 + 指數退避）

可用性：
- 系統可用性: 99.95%（多區域部署 + 自動故障轉移）
```

## 成本估算

### 場景：100 萬 DAU，平均每天 5 條通知

```
通知分布：
- Email: 50% (250 萬封/天)
- SMS: 10% (50 萬條/天)
- Push: 40% (200 萬個/天)

成本計算：

1. Email (AWS SES):
   - 250 萬封/天 × $0.0001 = $250/天
   - 月成本: $7,500

2. SMS (Twilio):
   - 50 萬條/天 × $0.01 = $5,000/天
   - 月成本: $150,000

3. Push (FCM/APNs):
   - 免費

4. 基礎設施:
   - Kafka (3 節點): $500/月
   - Redis (Cluster): $300/月
   - MySQL (RDS): $400/月
   - Worker (10 台 EC2 c5.xlarge): $1,500/月
   - 負載均衡: $200/月
   - 監控 (Datadog): $500/月

總成本: 約 $160,400/月
單用戶成本: $0.16/月

成本優化建議:
1. 智能降級：Push > Email > SMS（優先使用免費渠道）
2. 批量發送：合併相似通知（減少 50% 發送量）
3. 去重：避免重複發送（節省 10-20%）
4. 用戶偏好：只發送用戶想要的通知（提高滿意度 + 降低成本）
```

### 擴展到 1000 萬 DAU

```
通知量：5000 萬/天

成本：
- Email: $75,000/月
- SMS: $1,500,000/月
- 基礎設施: $10,000/月（50 Worker + 更大 Kafka/Redis/MySQL）

總成本: 約 $1,585,000/月
單用戶成本: $0.16/月（不變）

架構變化：
1. Kafka 擴展到 10+ 節點（處理 50M 消息/天）
2. MySQL 分庫分表（按 user_id hash 分 16 個庫）
3. Redis Cluster（10+ 節點）
4. Worker 擴展到 50+ 台（水平擴展）
5. 多區域部署（US-East, US-West, EU, Asia）
```

## 關鍵設計決策

### Q1: 為什麼選擇 Kafka + MySQL 雙重保障？

| 方案 | 優勢 | 劣勢 |
|------|------|------|
| 僅 Kafka | 實時性好 | Kafka 故障 = 數據丟失 |
| 僅 MySQL | 可靠性高 | 延遲高（輪詢） |
| **Kafka + MySQL** | 實時 + 可靠 | 略複雜 |

**結論**：雙重保障是最佳實踐（類似 Uber、Airbnb）。

### Q2: 為什麼需要優先級？

```
場景：凌晨 3 點，營銷部門發送 100 萬封郵件

問題：
- 關鍵通知（密碼重置）被阻塞在隊列後面
- 用戶無法及時收到驗證碼

解決方案：
- critical 優先處理（< 1 分鐘）
- low 延遲處理（可等待數小時）
```

### Q3: 為什麼需要限流？

```
原因：
1. 第三方服務限制（AWS SES: 1000 封/秒）
2. 成本控制（短信 $0.01/條）
3. 防止被封（發送過快 = 垃圾郵件標記）

實現：
- Redis 分布式計數器
- 超過限流 → 延遲 1 秒重試（不丟棄）
```

### Q4: 為什麼需要去重？

```
場景：
1. 用戶點擊註冊按鈕 3 次（網絡慢）
2. 重試機制導致重複發送
3. Kafka Consumer 重複消費

方案：
- task_id 唯一性約束（數據庫層）
- Redis 時間窗口去重（應用層）
```

### Q5: 如何選擇通知渠道？

```go
func ChooseChannel(urgency string, userPreference string, cost float64) string {
    if urgency == "critical" {
        return "sms"  // 最高到達率
    }

    if userPreference == "push" && isOnline() {
        return "push"  // 免費 + 實時
    }

    if cost < 0.001 {
        return "email"  // 成本低
    }

    return "email"  // 默認
}
```

## 常見問題

### Q1: 如何防止郵件進垃圾箱？

```
1. 配置 SPF 記錄（防偽造）:
   TXT @ "v=spf1 include:amazonses.com ~all"

2. 配置 DKIM 簽名（防篡改）:
   AWS SES 自動配置

3. 配置 DMARC 策略（防釣魚）:
   TXT _dmarc "v=DMARC1; p=quarantine; rua=mailto:admin@example.com"

4. 內容優化:
   - 避免垃圾詞彙（"免費", "中獎", "立即購買"）
   - 文本/HTML 比例合理（不要純圖片）
   - 提供退訂鏈接（CAN-SPAM 法規）

5. 發送頻率控制:
   - 逐步增加發送量（Warm-up）
   - 監控投訴率（< 0.1%）
```

### Q2: 推送 Token 過期怎麼辦？

```
問題：
- 用戶重裝應用 → 舊 Token 失效
- iOS 定期刷新 Token

解決方案：
1. 客戶端定期更新 Token（每次啟動）
2. 發送失敗時標記 Token 為 invalid
3. 定期清理無效 Token（30 天無活動）

代碼：
if err == "InvalidToken" {
    db.Exec("UPDATE user_devices SET status = 'inactive' WHERE push_token = ?", token)
}
```

### Q3: 如何實現多語言通知？

```sql
-- 每種語言一條模板記錄
INSERT INTO notification_templates (template_key, channel, language, subject, body) VALUES
('welcome_email', 'email', 'en', 'Welcome!', 'Hi {{username}}, welcome...'),
('welcome_email', 'email', 'zh', '歡迎！', '你好 {{username}}，歡迎...'),
('welcome_email', 'email', 'ja', 'ようこそ！', 'こんにちは {{username}}...');

-- 根據用戶語言偏好選擇
SELECT * FROM notification_templates
WHERE template_key = 'welcome_email'
  AND channel = 'email'
  AND language = (SELECT language FROM users WHERE id = 'user123');
```

### Q4: 如何處理時區？

```go
// 數據庫統一存儲 UTC
created_at TIMESTAMP  -- UTC

// 展示時轉換為用戶時區
func FormatTime(utcTime time.Time, userTimezone string) string {
    loc, _ := time.LoadLocation(userTimezone)  // "America/New_York"
    return utcTime.In(loc).Format("2006-01-02 15:04:05")
}

// 定時任務（如每日摘要）根據用戶時區發送
// 例：美東用戶 8:00 AM，北京用戶 8:00 AM（不同 UTC 時間）
```

### Q5: 如何實現 A/B 測試？

```sql
-- 模板表添加 variant 字段
ALTER TABLE notification_templates ADD COLUMN variant VARCHAR(10) DEFAULT 'A';

-- 創建多個版本
INSERT INTO notification_templates (template_key, channel, language, variant, subject, body) VALUES
('promo_email', 'email', 'en', 'A', 'Save 20%!', 'Limited time offer...'),
('promo_email', 'email', 'en', 'B', 'Exclusive Deal', 'Just for you...');

-- 隨機分配
variant := "A"
if hash(user_id) % 2 == 1 {
    variant = "B"
}

-- 追蹤結果
SELECT variant, COUNT(*) as sent, SUM(opened) as opened
FROM notification_events
WHERE template_key = 'promo_email'
GROUP BY variant;
```

### Q6: 如何保證 GDPR 合規？

```
GDPR 要求：
1. 用戶同意：發送前獲得明確同意
2. 數據刪除：用戶可要求刪除所有數據
3. 數據可攜：用戶可導出所有數據
4. 透明度：告知用戶數據使用方式

實現：
-- 刪除用戶所有通知數據
DELETE FROM notification_tasks WHERE user_id = 'user123';
DELETE FROM notification_events WHERE user_id = 'user123';
DELETE FROM user_notification_preferences WHERE user_id = 'user123';

-- 導出用戶數據
SELECT * FROM notification_tasks WHERE user_id = 'user123';
```

### Q7: 如何監控系統健康？

```
關鍵指標（SLI）:
1. 任務成功率: > 99.9%
2. 發送延遲: P99 < 5s
3. 隊列積壓: < 10000 條
4. 錯誤率: < 0.1%

告警規則：
- 任務成功率 < 99% → P0 告警
- 隊列積壓 > 50000 → P1 告警
- 第三方服務錯誤率 > 5% → P2 告警

Prometheus 查詢：
rate(notifications_sent_total{status="failed"}[5m]) > 0.01
```

## 延伸閱讀

### 真實案例

- **Uber Engineering**: [Building Uber's Notification Platform](https://eng.uber.com/notification-platform/)
- **Airbnb Engineering**: [Scaling Airbnb's Notification System](https://medium.com/airbnb-engineering/scaling-airbnbs-notification-system-7a7d6f0e0fb4)
- **LinkedIn**: [Building LinkedIn's Real-Time Notification System](https://engineering.linkedin.com/blog/2020/building-linkedins-real-time-notification-system)
- **Slack**: [Scaling Slack's Notification System](https://slack.engineering/scaling-slacks-job-queue/)

### 第三方服務文檔

- **Email**: [AWS SES](https://aws.amazon.com/ses/), [SendGrid](https://sendgrid.com/docs/), [Mailgun](https://www.mailgun.com/)
- **SMS**: [Twilio](https://www.twilio.com/docs/sms), [AWS SNS](https://aws.amazon.com/sns/)
- **Push**: [Firebase Cloud Messaging](https://firebase.google.com/docs/cloud-messaging), [APNs](https://developer.apple.com/documentation/usernotifications)
- **統一平台**: [OneSignal](https://onesignal.com/), [Pusher](https://pusher.com/), [Courier](https://www.courier.com/)

### 開源項目

- [Novu](https://github.com/novuhq/novu) - 開源通知基礎設施
- [Knock](https://knock.app/) - 通知即服務平台
- [Courier](https://github.com/trycourier/courier) - 多渠道通知 API

### 相關章節

- **05-distributed-cache**: Redis 分布式緩存（限流、去重）
- **07-message-queue**: Kafka 消息隊列
- **16-chat-system**: WebSocket 實時通訊（In-App 通知）
- **18-instagram**: 社交通知的實際應用

## 總結

從「同步發送單封郵件」到「多渠道智能通知系統」，我們學到了：

1. **雙重保障**：Kafka（實時）+ MySQL（可靠）
2. **智能路由**：優先級 + 限流 + 去重 + 合併
3. **用戶友好**：偏好設置 + 退訂機制 + 多語言
4. **可觀測性**：追蹤（打開率、點擊率）+ 監控 + 告警
5. **橫向擴展**：Kafka 消費者組 + 無狀態 Worker
6. **成本優化**：智能降級（Push > Email > SMS）

**記住：可靠性、用戶體驗、成本優化，三者缺一不可！**

**核心理念：Notify users reliably, respect their preferences, and measure everything.（可靠地通知用戶，尊重他們的偏好，並測量一切）**
