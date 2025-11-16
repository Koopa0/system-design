# Chat System - 即時通訊系統

> WhatsApp/WeChat 聊天系統設計：從 Polling 到 WebSocket 再到分布式架構

## 概述

本章節展示如何設計一個高性能的即時通訊系統（Chat System），支持：
- **即時通訊**：毫秒級消息延遲
- **高並發**：支持千萬級在線用戶
- **可靠傳輸**：消息不丟失、不重複
- **多端同步**：支持多設備實時同步
- **橫向擴展**：無狀態架構，易於擴展

## 學習目標

- 理解 **Polling vs Long Polling vs WebSocket** 的權衡
- 掌握 **WebSocket 全雙工通訊**的實現
- 學習 **群聊的並行 Fanout** 策略
- 實踐 **離線消息**存儲和推送
- 了解 **已讀回執**（Delivered/Read）機制
- 學習 **多端同步**的 Cursor 方案
- 掌握 **消息可靠性**（冪等性、ACK、重試）
- 了解 **Redis Pub/Sub 橫向擴展**
- 學習 **WhatsApp 的真實架構**

## 核心概念

### 1. Polling（輪詢）

```
客戶端每 N 秒請求一次：
GET /messages?user_id=Alice&since_id=100

優勢：
✅ 實現簡單
✅ HTTP 標準協議

劣勢：
❌ 延遲高（N 秒）
❌ 浪費資源（大量空輪詢）
❌ 服務器壓力大

適用場景：低實時性要求（如郵件）
```

### 2. Long Polling（長輪詢）

```
客戶端發起請求，服務器掛起連接，直到有消息或超時：
GET /messages/long-poll?user_id=Alice&timeout=30s

服務器偽代碼：
1. 檢查是否有新消息
2. 如果有，立即返回
3. 如果沒有，掛起請求（最多 30 秒）
4. 超時後返回空響應

優勢：
✅ 延遲低（準實時）
✅ HTTP 標準協議

劣勢：
❌ 仍需頻繁建立連接
❌ 服務器需要維持大量連接
❌ 單向通信（只能服務器推送）

適用場景：中等實時性要求
```

### 3. WebSocket（推薦）

```
建立持久化的全雙工連接：
1. 客戶端發起 HTTP 升級請求
2. 服務器升級協議到 WebSocket
3. 雙向實時通訊

優勢：
✅ 延遲極低（毫秒級）
✅ 全雙工通訊
✅ 節省資源（單一連接）
✅ 服務器可主動推送

劣勢：
⚠️ 需要支持 WebSocket 的基礎設施
⚠️ 狀態管理複雜（需要橫向擴展方案）

適用場景：高實時性要求（聊天、遊戲）
```

### 4. 群聊的並行 Fanout

```
用戶在群組發消息：
1. 插入消息到數據庫
2. 查詢群組成員列表
3. 並行 Fanout 給所有在線成員（WebSocket）
4. 離線成員存入離線消息隊列

並行策略：
- 使用 Goroutines 並行發送
- WaitGroup 等待所有發送完成
- 失敗重試機制

優化：
- 限制群組人數（500-1000 人）
- 超大群組使用延遲加載
```

### 5. 離線消息存儲

```
用戶離線時：
1. 消息存入 offline_messages 表
2. 記錄未讀數量

用戶上線時：
1. 查詢所有離線消息
2. 按時間順序推送
3. 清空離線消息表

存儲策略：
- MySQL：持久化存儲（7 天）
- Redis：最近 100 條緩存
```

### 6. 已讀回執（Read Receipts）

```
消息狀態：
- Sent：消息已發送（客戶端本地）
- Delivered：消息已送達（服務器確認）
- Read：消息已讀（接收方確認）

實現：
1. 發送方發送消息 → Sent
2. 服務器收到消息 → 返回 Delivered ACK
3. 接收方讀取消息 → 發送 Read ACK
4. 服務器轉發 Read ACK 給發送方
```

### 7. 多端同步（Multi-Device Sync）

```
核心問題：
- 用戶在手機和電腦同時登錄
- 如何保證消息同步？

方案 1：Fanout 所有設備
- 消息發送給用戶的所有在線設備
- 每個設備維護自己的 cursor（最後讀取的消息 ID）

方案 2：服務器存儲游標
- 服務器記錄每個設備的同步狀態
- 設備上線時拉取未同步的消息

推薦：方案 1（簡單可靠）
```

### 8. 消息可靠性

```
冪等性（Idempotency）：
- 客戶端生成 client_msg_id（UUID）
- 服務器檢查是否重複
- 防止重複發送

ACK 機制：
- 客戶端發送消息 → 等待 ACK
- 服務器收到消息 → 返回 ACK（帶 server_msg_id）
- 超時未收到 ACK → 重試（使用相同 client_msg_id）

消息持久化：
- 寫入 MySQL（主存儲）
- 寫入 Redis（快速查詢）
- Binlog 備份（災難恢復）
```

### 9. 橫向擴展（Horizontal Scaling）

```
問題：
- WebSocket 是有狀態的（每個連接綁定一個服務器）
- 如何橫向擴展？

方案：Redis Pub/Sub
1. 用戶 Alice 連接到 Server A
2. 用戶 Bob 連接到 Server B
3. Alice 發消息給 Bob：
   - Server A 收到消息
   - Server A 發佈到 Redis Channel: user:Bob
   - Server B 訂閱 Redis Channel: user:Bob
   - Server B 推送給 Bob

優化：
- 使用 Consistent Hashing 分配用戶
- 減少跨服務器通訊
```

## 技術棧

- **語言**: Golang (標準庫優先)
- **協議**: WebSocket (`gorilla/websocket` 或 `nhooyr.io/websocket`)
- **數據庫**: MySQL/PostgreSQL
- **緩存**: Redis (Pub/Sub + 消息緩存)
- **消息隊列**: Kafka (離線消息、審核、推送通知)
- **負載均衡**: Nginx (WebSocket 支持)

## 架構演進

### 階段 1：Polling（慢）

```
延遲：5-10 秒
資源浪費：90%+ 空輪詢
不推薦
```

### 階段 2：Long Polling

```
延遲：1-2 秒
連接數：中等
適合中小規模
```

### 階段 3：WebSocket（推薦）

```
延遲：< 100ms
連接數：持久化連接
適合大規模實時系統
```

### 階段 4：分布式 WebSocket + Redis Pub/Sub

```
延遲：< 100ms
擴展性：水平擴展
容錯性：高可用
最終架構
```

## 性能指標

```
最終系統性能（WebSocket + Redis Pub/Sub + MySQL）：

消息延遲：
- P50: 50ms
- P99: 200ms
- P99.9: 500ms

吞吐量：
- 單服務器：10,000 並發連接
- 集群：100 萬+ 並發連接

可靠性：
- 消息送達率：99.99%
- 消息丟失率：< 0.01%
- 消息重複率：0%（冪等性保證）

可用性：
- 系統可用性：99.95%
- 故障恢復時間：< 30 秒
```

## 項目結構

```
16-chat-system/
├── DESIGN.md              # 詳細設計文檔（蘇格拉底式教學）
├── README.md              # 本文件
├── cmd/
│   └── server/
│       └── main.go        # HTTP/WebSocket 服務器
├── internal/
│   ├── polling.go         # Polling 實現
│   ├── longpoll.go        # Long Polling 實現
│   ├── websocket.go       # WebSocket 實現
│   ├── group_chat.go      # 群聊實現
│   ├── offline.go         # 離線消息
│   ├── read_receipt.go    # 已讀回執
│   ├── sync.go            # 多端同步
│   ├── reliability.go     # 消息可靠性
│   └── scaling.go         # 橫向擴展（Redis Pub/Sub）
└── docs/
    ├── performance.md     # 性能測試報告
    └── whatsapp-case.md   # WhatsApp 案例研究
```

## 快速開始

### 1. 數據庫設計

```sql
-- 用戶表
CREATE TABLE users (
    id VARCHAR(64) PRIMARY KEY,
    username VARCHAR(100) UNIQUE NOT NULL,
    status ENUM('online', 'offline') DEFAULT 'offline',
    last_seen TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 消息表
CREATE TABLE messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    client_msg_id VARCHAR(64) UNIQUE NOT NULL, -- 冪等性
    from_user VARCHAR(64) NOT NULL,
    to_user VARCHAR(64) NOT NULL,
    content TEXT,
    msg_type ENUM('text', 'image', 'video', 'file') DEFAULT 'text',
    status ENUM('sent', 'delivered', 'read') DEFAULT 'sent',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_from_user (from_user, created_at DESC),
    INDEX idx_to_user (to_user, created_at DESC),
    INDEX idx_client_msg_id (client_msg_id)
);

-- 離線消息表
CREATE TABLE offline_messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    message_id BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user (user_id, created_at DESC),
    FOREIGN KEY (message_id) REFERENCES messages(id)
);

-- 群組表
CREATE TABLE groups (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    created_by VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 群組成員表
CREATE TABLE group_members (
    group_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id),
    INDEX idx_user (user_id)
);

-- 群組消息表
CREATE TABLE group_messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    client_msg_id VARCHAR(64) UNIQUE NOT NULL,
    group_id VARCHAR(64) NOT NULL,
    from_user VARCHAR(64) NOT NULL,
    content TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_group (group_id, created_at DESC)
);
```

### 2. Redis 設計

```bash
# 用戶在線狀態
# Key: online:{user_id}
# Value: server_id (哪台服務器)
SET online:Alice server-1
EXPIRE online:Alice 3600

# 用戶設備列表
# Key: devices:{user_id}
# Value: Set of device_id
SADD devices:Alice device-1 device-2

# 離線消息緩存（最近 100 條）
# Key: offline:{user_id}
# Value: List of message_id
LPUSH offline:Alice 12345
LTRIM offline:Alice 0 99

# 消息緩存（最近 1000 條）
# Key: msg:{message_id}
# Value: JSON
HSET msg:12345 from "Alice" to "Bob" content "Hello" created_at "1733160000"
EXPIRE msg:12345 86400

# Pub/Sub（橫向擴展）
# Channel: user:{user_id}
PUBLISH user:Bob '{"from":"Alice","content":"Hello","msg_id":12345}'
```

### 3. API 示例

#### 3.1 WebSocket 連接

```bash
# 建立 WebSocket 連接
wscat -c ws://localhost:8080/ws?user_id=Alice&device_id=device-1

# 服務器返回連接成功
{"type": "connected", "user_id": "Alice"}
```

#### 3.2 發送消息（1對1）

```json
// 客戶端 → 服務器
{
  "type": "send_message",
  "client_msg_id": "uuid-12345",
  "to_user": "Bob",
  "content": "Hello Bob!",
  "msg_type": "text"
}

// 服務器 → 客戶端（ACK）
{
  "type": "message_ack",
  "client_msg_id": "uuid-12345",
  "server_msg_id": 67890,
  "status": "delivered"
}

// 服務器 → 接收方（Bob）
{
  "type": "new_message",
  "server_msg_id": 67890,
  "from_user": "Alice",
  "content": "Hello Bob!",
  "created_at": "2025-01-15T10:30:00Z"
}
```

#### 3.3 發送群組消息

```json
// 客戶端 → 服務器
{
  "type": "send_group_message",
  "client_msg_id": "uuid-67890",
  "group_id": "group-001",
  "content": "Hello everyone!",
  "msg_type": "text"
}

// 服務器並行 Fanout 給所有在線成員
```

#### 3.4 已讀回執

```json
// 接收方 → 服務器
{
  "type": "mark_as_read",
  "message_id": 67890
}

// 服務器 → 發送方
{
  "type": "message_read",
  "message_id": 67890,
  "read_by": "Bob",
  "read_at": "2025-01-15T10:31:00Z"
}
```

#### 3.5 同步消息（多端）

```json
// 設備上線時拉取未同步消息
{
  "type": "sync_messages",
  "since_id": 67800
}

// 服務器返回
{
  "type": "sync_response",
  "messages": [
    {"id": 67801, "from": "Alice", "content": "Hi"},
    {"id": 67802, "from": "Charlie", "content": "Hello"},
    ...
  ],
  "latest_id": 67890
}
```

## 關鍵設計決策

### 為什麼選擇 WebSocket？

| 場景 | Polling | Long Polling | WebSocket |
|------|---------|--------------|-----------|
| 延遲 | 高（5-10s） | 中（1-2s） | 低（<100ms） |
| 資源利用 | 低（90%+ 空輪詢） | 中 | 高 |
| 雙向通訊 | ❌ | ❌ | ✅ |
| 擴展性 | 一般 | 一般 | 高（需配合 Redis） |
| 適用規模 | 小 | 中 | 大 |

**結論**：WebSocket 延遲低、支持雙向通訊，適合大規模聊天系統。

### 為什麼用 Redis Pub/Sub 橫向擴展？

- ✅ **簡單易用**：標準的發布訂閱模式
- ✅ **低延遲**：毫秒級消息轉發
- ✅ **高吞吐**：單節點 100,000+ QPS
- ✅ **無狀態**：服務器可隨意擴展

### 為什麼需要冪等性（client_msg_id）？

```
場景：網絡不穩定
1. 客戶端發送消息（client_msg_id: uuid-123）
2. 網絡超時，客戶端重試
3. 如果沒有冪等性 → 消息重複
4. 使用 client_msg_id → 服務器識別重複，只插入一次
```

### 為什麼離線消息存儲 7 天？

```
權衡：
- 存儲成本 vs 用戶體驗
- WhatsApp：30 天
- WeChat：永久存儲
- 本設計：7 天（中等方案）

超過 7 天的消息：
- 從離線表刪除
- 仍存在主消息表（可查詢歷史）
```

## 常見問題

### Q1: 如何處理網絡斷線重連？

```
客戶端：
1. 檢測到連接斷開
2. 指數退避重連（1s, 2s, 4s, 8s, ...）
3. 重連成功後，拉取未同步的消息（since_id）

服務器：
1. 檢測到連接斷開，清理內存狀態
2. 設置 Redis TTL（online:{user_id}），3 分鐘後自動下線
```

### Q2: 如何防止消息丟失？

```
三層保障：

1. 客戶端本地存儲（SQLite）：
   - 發送前先寫本地
   - 收到 ACK 後標記為已送達

2. 服務器持久化（MySQL）：
   - 先寫數據庫，再推送
   - Binlog 備份

3. ACK + 重試機制：
   - 客戶端超時重試（使用相同 client_msg_id）
   - 服務器冪等性保證
```

### Q3: 如何實現消息加密？

```
方案 1：傳輸加密（TLS/WSS）
- WebSocket over TLS (wss://)
- 防止中間人攻擊

方案 2：端到端加密（E2EE）
- Signal Protocol（WhatsApp 使用）
- 客戶端加密，服務器只轉發密文
- 服務器無法讀取消息內容

推薦：TLS + E2EE 雙重保護
```

### Q4: 如何實現消息撤回？

```
流程：
1. 客戶端發送撤回請求（message_id）
2. 服務器檢查時間限制（如 2 分鐘內）
3. 更新消息狀態為 deleted
4. 推送撤回通知給接收方
5. 接收方移除本地消息（顯示"已撤回"）

數據庫：
- 軟刪除：messages.deleted_at = NOW()
- 保留記錄（審計、取證）
```

### Q5: 如何估算成本？

```
場景：100 萬 DAU（日活躍用戶）

假設：
- 平均在線用戶：30 萬（峰值）
- 每用戶每天發送消息：50 條
- 消息大小：平均 500 bytes
- 在線時長：平均 2 小時

資源需求：

1. WebSocket 連接：
   - 30 萬並發連接
   - 單服務器：10,000 連接
   - 需要：30 台服務器（8 核 16GB）
   - AWS EC2 (c5.2xlarge): 30 × $250/月 = $7,500/月

2. Redis（Pub/Sub + 緩存）：
   - 連接狀態：30 萬 × 100 bytes = 30 MB
   - 消息緩存：1000 條 × 500 bytes × 100 萬用戶 = 500 GB
   - AWS ElastiCache (r6g.4xlarge × 3): $1,200/月

3. MySQL（消息存儲）：
   - 每天消息：100 萬 × 50 = 5000 萬條
   - 每條 500 bytes → 25 GB/天
   - 保留 30 天 → 750 GB
   - AWS RDS (db.r5.2xlarge): $730/月

4. Kafka（離線消息、推送）：
   - AWS MSK (kafka.m5.large × 3): $450/月

5. 負載均衡：
   - AWS ALB: $200/月

總計：約 $10,080/月（100 萬 DAU）
單用戶成本：$0.01/月
```

### Q6: 如何擴展到 1000 萬 DAU？

```
水平擴展策略：

1. WebSocket 服務器：
   - 300 台服務器（10,000 連接/台）
   - 使用 Consistent Hashing 分配用戶
   - 成本：300 × $250 = $75,000/月

2. Redis Cluster：
   - 10-20 個分片
   - 按 user_id hash 分片
   - 成本：$5,000/月

3. MySQL 分庫分表：
   - 按 user_id 分庫（32 個庫）
   - 按 created_at 分表（按月）
   - 成本：$20,000/月

4. Kafka 擴容：
   - 增加分區數（200+）
   - 成本：$2,000/月

5. CDN（媒體文件）：
   - 圖片、視頻等存儲在 S3 + CloudFront
   - 成本：$5,000/月

總計：約 $107,000/月（1000 萬 DAU）
單用戶成本：$0.0107/月
```

### Q7: 如何監控系統健康度？

```
關鍵指標（SLI）：

1. 消息延遲：
   - P50 < 50ms
   - P99 < 200ms

2. 消息送達率：
   - > 99.99%

3. WebSocket 連接穩定性：
   - 異常斷線率 < 1%

4. 服務器健康：
   - CPU < 70%
   - 內存 < 80%
   - 連接數 < 80% 上限

告警閾值：
- P99 延遲 > 500ms → 告警
- 消息丟失率 > 0.1% → 告警
- 服務器 CPU > 85% → 告警

工具：
- Prometheus + Grafana（指標監控）
- ELK Stack（日誌分析）
- Sentry（錯誤追蹤）
```

## 延伸閱讀

### 真實案例

- **WhatsApp**: [Scaling to 2 Billion Users](https://www.wired.com/2015/09/whatsapp-serves-900-million-users-50-engineers/) - Erlang/FreeBSD 架構
- **Discord**: [How Discord Stores Billions of Messages](https://discord.com/blog/how-discord-stores-billions-of-messages) - Cassandra + ScyllaDB
- **Slack**: [Scaling Slack's Job Queue](https://slack.engineering/scaling-slacks-job-queue/) - Redis + Kafka

### 開源項目

- [Gorilla WebSocket](https://github.com/gorilla/websocket) - Golang WebSocket 庫
- [Socket.IO](https://github.com/socketio/socket.io) - Node.js 實時通訊框架
- [ejabberd](https://github.com/processone/ejabberd) - Erlang XMPP 服務器（WhatsApp 前身）
- [Centrifugo](https://github.com/centrifugal/centrifugo) - 可擴展的實時消息服務器

### 協議與標準

- **WebSocket Protocol**: [RFC 6455](https://datatracker.ietf.org/doc/html/rfc6455)
- **XMPP**: Extensible Messaging and Presence Protocol（傳統 IM 協議）
- **MQTT**: 輕量級物聯網消息協議
- **Signal Protocol**: 端到端加密協議（WhatsApp 使用）

### 論文與文章

- **Erlang/OTP**: [Designing for Scalability with Erlang/OTP](https://www.erlang.org/doc/)
- **WhatsApp Architecture**: [1 Million Connections per Second](https://blog.whatsapp.com/1-million-is-so-2011)
- **WebSocket at Scale**: [Slack's Journey to WebSockets](https://slack.engineering/flannel-an-application-level-edge-cache-to-make-slack-scale/)

### 相關章節

- **05-distributed-cache**: Redis 分布式緩存
- **07-message-queue**: Kafka 消息隊列
- **09-event-driven**: 事件驅動架構
- **15-news-feed**: News Feed 動態流系統
- **17-notification-service**: 通知服務（推送通知）

## 總結

從「每 5 秒輪詢一次的低效聊天」到「毫秒級 WebSocket 實時通訊」，我們學到了：

1. **協議選擇**：WebSocket 適合高實時性場景
2. **群聊優化**：並行 Fanout + 限制群組人數
3. **可靠傳輸**：冪等性（client_msg_id）+ ACK + 重試
4. **多端同步**：每個設備維護自己的 cursor
5. **橫向擴展**：Redis Pub/Sub 解決 WebSocket 有狀態問題
6. **離線消息**：MySQL 持久化 + Redis 緩存
7. **性能監控**：P99 延遲、送達率、連接穩定性

**記住：實時系統的核心是低延遲 + 高可靠性！**

**核心理念：Choose the right protocol for real-time communication.（為實時通訊選擇正確的協議）**

**WhatsApp 的啟示：**
- 50 個工程師支持 20 億用戶
- Erlang/OTP 的並發模型（每個用戶一個輕量級進程）
- FreeBSD 優化（> 100 萬並發連接/服務器）
- 簡單勝過複雜：專注核心功能，避免過度設計
