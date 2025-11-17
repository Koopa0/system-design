# Live Comment System - 直播彈幕系統

> 高並發實時彈幕系統：從 HTTP 輪詢到 WebSocket 分布式架構

## 概述

本章節展示如何設計一個生產級的**直播彈幕系統（Live Comment System）**，支持：
- **實時通訊**：WebSocket 全雙工通訊，延遲 < 100ms
- **高並發**：10 萬+ 並發連接，1 萬+ 條彈幕/秒
- **限流保護**：用戶級、房間級、全局限流
- **內容安全**：敏感詞過濾、人工審核
- **彈幕回放**：冷熱分離存儲
- **橫向擴展**：Redis Pub/Sub 跨服務器通訊
- **熱門彈幕**：實時排名和熱度計算

## 學習目標

- 理解 **Polling、Long Polling、WebSocket** 的演進
- 掌握 **WebSocket 全雙工通訊** 實現
- 學習**高並發優化**（批量廣播、Goroutine Pool）
- 實踐**限流策略**（Redis 分布式限流）
- 了解**敏感詞過濾**（Trie 樹算法）
- 掌握**彈幕存儲**（MySQL + S3 冷熱分離）
- 學習 **Redis Pub/Sub** 橫向擴展
- 理解**熱門彈幕**（Redis Sorted Set）
- 掌握**性能優化**和**降級策略**
- 學習 Bilibili、Twitch 的真實案例

## 核心概念

### 1. 實時通訊演進

#### HTTP 輪詢（Polling）

```
客戶端每 1 秒請求一次新彈幕

優勢：
✅ 實現簡單

劣勢：
❌ 延遲高（最壞 1 秒）
❌ 資源浪費（90%+ 空請求）
❌ 數據庫壓力大

適用場景：低實時性要求
```

#### Long Polling（長輪詢）

```
客戶端發起請求，服務器掛起連接，直到有新彈幕或超時

優勢：
✅ 延遲低（準實時）
✅ 減少空請求

劣勢：
❌ 仍需頻繁輪詢數據庫
❌ 服務器維持大量連接
❌ 單向通信

適用場景：中等實時性要求
```

#### WebSocket（推薦）

```
建立持久化的全雙工連接

優勢：
✅ 延遲極低（< 50ms）
✅ 全雙工通訊
✅ 節省資源（單一連接）
✅ 服務器主動推送

劣勢：
⚠️ 需要特殊基礎設施
⚠️ 狀態管理複雜（需橫向擴展）

適用場景：高實時性要求（直播彈幕）
```

### 2. 高並發優化

#### 批量廣播

```
問題：
- 10 萬並發用戶
- 每條彈幕廣播 10 萬次
- CPU 壓力巨大

方案：批量廣播（每 100ms 一次）
- 收集 100ms 內的所有彈幕
- 一次性廣播給所有用戶
- 減少廣播次數 100 倍

優勢：
✅ 降低 CPU 壓力
✅ 提高吞吐量
```

#### Goroutine Pool

```
問題：
- 每次廣播創建 10 萬個 goroutine
- 內存占用過高
- GC 壓力大

方案：Goroutine Pool
- 預創建 100 個 worker goroutine
- 任務提交到隊列
- worker 從隊列消費任務

優勢：
✅ 控制並發數
✅ 減少內存占用
✅ 降低 GC 壓力
```

### 3. 限流策略

#### 用戶級限流

```
普通用戶：1 條/秒
VIP 用戶：5 條/秒
房主：10 條/秒

實現：Redis INCR + EXPIRE

INCR rate_limit:user:alice
EXPIRE rate_limit:user:alice 1

if count > limit:
    return "Too many comments"
```

#### 房間級限流

```
單個房間：1000 條/秒

防止熱點房間壓垮系統
```

#### 全局限流

```
整個系統：10 萬條/秒

超過限流 → 丟棄部分彈幕
顯示"彈幕過多，已省略部分彈幕"
```

### 4. 敏感詞過濾

```
算法：Trie 樹（字典樹）

構建：
root
  └─ 暴
      └─ 力 (end)

查找：O(n) 時間複雜度（n 為文本長度）

過濾：
輸入："這是暴力內容"
輸出："這是***內容"

優勢：
✅ 高效（單次掃描）
✅ 支持多敏感詞
✅ 易於更新
```

### 5. 彈幕存儲（冷熱分離）

```
熱數據（最近 7 天）：
- 存儲：MySQL
- 用途：實時查詢、回放
- 成本：高

冷數據（> 7 天）：
- 存儲：S3（JSON 文件）
- 用途：歷史回放
- 成本：低（1/10）

策略：
1. 直播結束後，導出彈幕為 JSON
2. 上傳到 S3
3. 刪除 MySQL 數據
4. 回放時按需加載
```

### 6. Redis Pub/Sub 橫向擴展

```
問題：
- 用戶 A 連接 Server 1
- 用戶 B 連接 Server 2
- 如何互相看到彈幕？

方案：Redis Pub/Sub

用戶 A → Server 1 → Redis PUBLISH room:123
                        ↓
用戶 B ← Server 2 ← Redis SUBSCRIBE room:123

優勢：
✅ 無狀態（易於擴展）
✅ 跨服務器通訊
✅ 高可用
```

### 7. 熱門彈幕

```
數據結構：Redis Sorted Set

ZADD hot_comments:room123 5 "comment_id_1"  # 5 個贊
ZADD hot_comments:room123 10 "comment_id_2" # 10 個贊

查詢 Top 10：
ZREVRANGE hot_comments:room123 0 9

點贊：
ZINCRBY hot_comments:room123 1 "comment_id_1"

優勢：
✅ 自動排序
✅ O(log N) 插入
✅ O(log N + M) 範圍查詢
```

## 技術棧

- **語言**: Golang (標準庫優先)
- **實時通訊**: WebSocket (gorilla/websocket)
- **消息分發**: Redis Pub/Sub
- **數據庫**: MySQL (熱數據)
- **對象存儲**: AWS S3 (冷數據)
- **緩存**: Redis (限流、熱門彈幕)
- **消息隊列**: Kafka (審核隊列)
- **監控**: Prometheus + Grafana

## 架構設計

```
┌─────────────────────────────────────┐
│    Load Balancer (Nginx)             │
└─────────────┬───────────────────────┘
              ↓
      ┌───────┴───────┐
      ↓               ↓
┌─────────┐     ┌─────────┐
│WS Srv 1 │     │WS Srv N │
└────┬────┘     └────┬────┘
     │               │
     └───────┬───────┘
             ↓
   ┌─────────────────┐
   │ Redis Pub/Sub   │
   │  (消息分發)      │
   └─────────────────┘
             ↓
   ┌─────────┴─────────┐
   ↓         ↓         ↓
┌──────┐ ┌──────┐ ┌──────┐
│Redis │ │Kafka │ │MySQL │
│Cache │ │Queue │ │      │
└──────┘ └──────┘ └──────┘
```

## 項目結構

```
19-live-comment-system/
├── DESIGN.md              # 詳細設計文檔（蘇格拉底式教學）
├── README.md              # 本文件
├── cmd/
│   └── server/
│       └── main.go        # WebSocket 服務器
├── internal/
│   ├── room.go            # 房間管理
│   ├── client.go          # 客戶端連接
│   ├── ratelimit.go       # 限流器
│   ├── filter.go          # 敏感詞過濾（Trie 樹）
│   ├── storage.go         # 彈幕存儲
│   ├── pubsub.go          # Redis Pub/Sub
│   └── hotcomment.go      # 熱門彈幕
└── docs/
    ├── api.md             # WebSocket API 文檔
    └── bilibili-case.md   # Bilibili 案例研究
```

## 快速開始

### 1. 數據庫設計

```sql
-- 彈幕表
CREATE TABLE comments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    username VARCHAR(100),
    content TEXT NOT NULL,
    timestamp INT,                    -- 相對於直播開始的秒數
    like_count INT DEFAULT 0,
    status ENUM('normal', 'deleted', 'reviewed') DEFAULT 'normal',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_room_timestamp (room_id, timestamp),
    INDEX idx_created_at (created_at DESC)
);

-- 直播場次表
CREATE TABLE live_sessions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    duration INT,                     -- 直播時長（秒）
    comment_count INT DEFAULT 0,      -- 彈幕總數
    INDEX idx_room_id (room_id, start_time DESC)
);

-- 彈幕點贊表
CREATE TABLE comment_likes (
    comment_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (comment_id, user_id),
    INDEX idx_comment_id (comment_id)
);

-- 敏感詞表
CREATE TABLE sensitive_words (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    word VARCHAR(100) NOT NULL,
    level ENUM('high', 'medium', 'low') DEFAULT 'medium',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_word (word)
);
```

### 2. Redis 設計

```bash
# 限流（用戶級）
# Key: rate_limit:user:{user_id}
# Value: counter
INCR rate_limit:user:alice
EXPIRE rate_limit:user:alice 1  # 1 秒過期

# 熱門彈幕（Sorted Set）
# Key: hot_comments:{room_id}
# Score: like_count
# Value: comment_id
ZADD hot_comments:room123 10 "comment_id_1"
ZREVRANGE hot_comments:room123 0 9  # Top 10

# 彈幕點贊狀態（Set）
# Key: comment:{comment_id}:liked_by
# Value: user_id
SADD comment:12345:liked_by alice bob
SISMEMBER comment:12345:liked_by alice  # 檢查是否點贊

# Pub/Sub（消息分發）
# Channel: room:{room_id}
PUBLISH room:123 '{"user":"Alice","content":"Hello"}'
SUBSCRIBE room:123
```

### 3. WebSocket API

#### 3.1 建立連接

```javascript
const ws = new WebSocket('ws://localhost:8080/ws?room_id=room123&user_id=alice&username=Alice');

// 連接成功
ws.onopen = () => {
    console.log('Connected');
};

// 接收彈幕
ws.onmessage = (event) => {
    const comments = JSON.parse(event.data);
    comments.forEach(comment => {
        displayComment(comment);
    });
};

// 連接關閉
ws.onclose = () => {
    console.log('Disconnected');
};
```

#### 3.2 發送彈幕

```javascript
function sendComment(content) {
    ws.send(JSON.stringify({
        type: 'comment',
        content: content
    }));
}

sendComment('Hello World!');
```

#### 3.3 點贊彈幕

```javascript
function likeComment(commentID) {
    ws.send(JSON.stringify({
        type: 'like',
        comment_id: commentID
    }));
}
```

#### 3.4 獲取熱門彈幕

```bash
GET /api/hot-comments?room_id=room123&limit=10

# 響應
{
  "comments": [
    {
      "id": 12345,
      "user_id": "alice",
      "username": "Alice",
      "content": "Amazing stream!",
      "like_count": 100,
      "created_at": "2025-01-15T10:30:00Z"
    },
    ...
  ]
}
```

#### 3.5 回放彈幕

```bash
GET /api/replay-comments?room_id=room123&session_id=67890&start_time=0&end_time=600

# 響應
{
  "comments": [
    {
      "id": 1,
      "username": "Alice",
      "content": "First!",
      "timestamp": 5  # 第 5 秒發送
    },
    {
      "id": 2,
      "username": "Bob",
      "content": "Nice!",
      "timestamp": 10
    },
    ...
  ]
}
```

## 性能指標

```
系統容量（10 台 WebSocket 服務器）：

並發連接：
- 單服務器：10,000 WebSocket 連接
- 集群：100,000 WebSocket 連接

吞吐量：
- 發送彈幕：10,000 條/秒
- 廣播彈幕：10 億次/秒（理論，批量廣播）

延遲：
- 彈幕發送 → 其他用戶收到：P99 < 100ms
- WebSocket 連接建立：P99 < 500ms

可用性：
- 系統可用性：99.9%
- 彈幕送達率：99%+

資源占用：
- 單連接內存：~10KB
- 10 萬連接：~1GB 內存
- CPU：< 50%（批量廣播優化後）
```

## 成本估算

### 場景：單個熱門直播間，10 萬並發用戶

```
基礎設施成本：

WebSocket 服務器：10 台 (c5.2xlarge)
- 10 × $250/月 = $2,500/月

Redis Cluster：3 節點 (r6g.xlarge)
- 3 × $200/月 = $600/月

MySQL：1 主 2 從 (db.r5.large)
- 3 × $150/月 = $450/月

Kafka：3 節點 (kafka.m5.large)
- 3 × $200/月 = $600/月

負載均衡：ALB
- $200/月

總成本：約 $4,350/月

單用戶成本：$0.0435/月（10 萬用戶）

帶寬成本：

假設：
- 平均彈幕大小：200 bytes
- 每秒彈幕數：100 條
- 每條彈幕廣播給 10 萬人

帶寬：100 × 200 bytes × 10 萬 = 2 GB/秒 = 5.2 PB/月

CDN 成本：5.2 PB × $0.085/GB = $442,000/月

優化後（批量廣播 + 壓縮）：
- 實際帶寬：約 500 TB/月
- 成本：$42,500/月

總成本（優化後）：$46,850/月
```

### 成本優化建議

```
1. 彈幕採樣：
   - 高峰期只顯示 50% 彈幕
   - 節省 50% 帶寬

2. 批量壓縮：
   - 批量發送 + gzip 壓縮
   - 節省 70% 帶寬

3. CDN 優化：
   - 使用更便宜的 CDN 廠商
   - 節省 30% 成本

4. 冷數據遷移：
   - 7 天後遷移到 S3
   - 節省 90% 存儲成本
```

## 關鍵設計決策

### Q1: 為什麼選擇 WebSocket 而不是 Long Polling？

| 方案 | 延遲 | 資源占用 | 雙向通訊 | 適用場景 |
|------|------|----------|----------|----------|
| Polling | 高（1s+） | 低 | ❌ | 低實時性 |
| Long Polling | 中（100ms） | 中 | ❌ | 中實時性 |
| **WebSocket** | 低（<50ms） | 高 | ✅ | **高實時性** |

**結論**：直播彈幕需要極低延遲，WebSocket 是最佳選擇。

### Q2: 為什麼需要批量廣播？

```
問題：
- 10 萬並發用戶
- 每秒 100 條彈幕
- 每條彈幕廣播 10 萬次 = 1000 萬次廣播/秒
- CPU 無法承受

方案：批量廣播（每 100ms 一次）
- 100ms 內收集 10 條彈幕
- 一次性廣播給所有用戶
- 廣播次數：1000 萬 → 10 萬（減少 100 倍）

優勢：
✅ 降低 CPU 壓力 100 倍
✅ 用戶體驗影響小（100ms 延遲可接受）
```

### Q3: 為什麼使用 Trie 樹過濾敏感詞？

```
對比：

方案 1：逐個檢查（暴力）
for each word in sensitive_words:
    if word in text:
        replace(word, "***")

時間複雜度：O(n × m)（n=敏感詞數，m=文本長度）
問題：敏感詞庫 1 萬個 → 太慢

方案 2：Trie 樹
構建 Trie 樹，一次掃描文本

時間複雜度：O(m)（m=文本長度）
優勢：
✅ 與敏感詞數量無關
✅ 支持前綴匹配
✅ 內存高效
```

### Q4: 為什麼使用 Redis Pub/Sub？

```
問題：
- 用戶 A 連接 Server 1
- 用戶 B 連接 Server 2
- 如何互相看到彈幕？

方案對比：

方案 1：數據庫輪詢
- Server 1 寫入 MySQL
- Server 2 輪詢 MySQL
問題：❌ 延遲高、數據庫壓力大

方案 2：HTTP 廣播
- Server 1 HTTP 請求 Server 2
問題：❌ 需要維護服務器列表、複雜度高

方案 3：Redis Pub/Sub（推薦）
- Server 1 PUBLISH 到 Redis
- Server 2 SUBSCRIBE Redis
優勢：
✅ 簡單易用
✅ 低延遲（毫秒級）
✅ 自動服務發現
```

### Q5: 為什麼需要限流？

```
場景：惡意用戶刷屏

無限流：
- 用戶每秒發送 1000 條彈幕
- 廣播：1000 × 10 萬 = 1 億次/秒
- 系統崩潰

有限流：
- 每用戶每秒最多 1 條
- 刷屏用戶被限制
- 系統穩定

分級限流：
- 普通用戶：1 條/秒
- VIP 用戶：5 條/秒
- 房主：10 條/秒

優勢：
✅ 保護系統
✅ 提升用戶體驗（無刷屏）
✅ 公平性
```

## 常見問題

### Q1: 如何處理 WebSocket 斷線重連？

```
客戶端：
1. 檢測到連接斷開
2. 指數退避重連（1s, 2s, 4s, 8s, ...）
3. 重連成功後，拉取未收到的彈幕

服務器：
1. 檢測到連接斷開，清理內存狀態
2. 從房間移除客戶端
3. 減少在線人數計數
```

### Q2: 如何實現「只看TA」功能？

```
客戶端過濾：
- 服務器發送所有彈幕
- 客戶端根據 user_id 過濾顯示

優勢：✅ 簡單
劣勢：❌ 浪費帶寬

服務器過濾：
- 客戶端訂閱特定用戶：
  ws.send({type: 'filter', user_id: 'alice'})
- 服務器只發送該用戶的彈幕

優勢：✅ 節省帶寬
劣勢：⚠️ 複雜度增加
```

### Q3: 如何防止彈幕重疊？

```
問題：
- 多條彈幕同時飄過
- 在屏幕上重疊

方案：碰撞檢測（客戶端）

1. 將屏幕分為多個軌道（track）
2. 彈幕進入時選擇空閒軌道
3. 記錄每個軌道的佔用時間
4. 避免在同一軌道上重疊

算法：
tracks = [0, 0, 0, 0, 0]  # 5 個軌道

function findFreeTrack():
    for i in range(len(tracks)):
        if current_time > tracks[i]:
            return i
    return -1  # 所有軌道都滿了，丟棄彈幕

function displayComment(comment):
    track = findFreeTrack()
    if track >= 0:
        show(comment, track)
        tracks[track] = current_time + comment.duration
```

### Q4: 如何實現彈幕禮物動畫？

```
需求：
- 用戶送禮物（火箭、遊艇）
- 全屏動畫
- 所有人同時看到

方案：
1. 禮物事件走單獨的 WebSocket 消息類型
   {type: 'gift', gift_type: 'rocket', from: 'alice'}

2. 服務器廣播給所有人（優先級高）

3. 客戶端收到後立即播放動畫（暫停彈幕）

4. 動畫結束後恢復彈幕

同步問題：
- 不同用戶網絡延遲不同
- 動畫開始時間可能相差幾百毫秒
- 解決方案：使用服務器時間戳 + 客戶端同步
```

### Q5: 如何監控彈幕系統健康？

```
關鍵指標：

1. 業務指標：
   - 在線人數
   - 彈幕發送 QPS
   - 平均延遲

2. 性能指標：
   - WebSocket 連接數
   - 廣播延遲（P50, P99）
   - Redis Pub/Sub 延遲

3. 錯誤率：
   - 連接失敗率 < 1%
   - 彈幕發送失敗率 < 0.1%
   - 限流觸發率

4. 資源使用率：
   - CPU < 70%
   - 內存 < 80%
   - 網絡帶寬 < 80%

告警：
- 在線人數突降 50% → P0 告警（可能服務器宕機）
- 平均延遲 > 500ms → P1 告警
- Redis Pub/Sub 故障 → P0 告警
```

### Q6: 如何處理熱點房間？

```
問題：
- 某個房間 10 萬人在線
- 其他房間平均 100 人
- 熱點房間壓力巨大

方案 1：彈幕採樣
- 高峰期只廣播 50% 的彈幕
- 顯示"彈幕過多，已省略部分彈幕"

方案 2：分層廣播
- VIP 用戶：看到所有彈幕
- 普通用戶：看到採樣後的彈幕

方案 3：專用服務器
- 為熱點房間分配專用服務器
- 自動擴容
```

## 延伸閱讀

### 真實案例

- **Bilibili 彈幕系統**: [彈幕系統架構](https://www.bilibili.com/read/cv4058562)
- **Twitch Chat**: [Twitch Engineering](https://blog.twitch.tv/en/2015/12/18/twitch-engineering-an-introduction-and-overview-a23917b71a25/)
- **YouTube Live Chat**: [YouTube Engineering](https://www.youtube.com/watch?v=W8TjYZ7LhO8)

### 技術文檔

- **WebSocket Protocol**: [RFC 6455](https://datatracker.ietf.org/doc/html/rfc6455)
- **Redis Pub/Sub**: [Redis Documentation](https://redis.io/docs/manual/pubsub/)
- **Gorilla WebSocket**: [GitHub](https://github.com/gorilla/websocket)

### 相關章節

- **16-chat-system**: WebSocket 聊天系統（1對1、群聊）
- **17-notification-service**: 通知服務（實時推送）
- **05-distributed-cache**: Redis 分布式緩存

## 總結

從「HTTP 輪詢」到「分布式 WebSocket 彈幕系統」，我們學到了：

1. **實時通訊演進**：Polling → Long Polling → WebSocket
2. **高並發優化**：批量廣播、Goroutine Pool
3. **限流保護**：用戶級、房間級、全局限流
4. **內容安全**：敏感詞過濾（Trie 樹）、人工審核
5. **彈幕存儲**：冷熱分離（MySQL + S3）
6. **橫向擴展**：Redis Pub/Sub 跨服務器通訊
7. **熱門彈幕**：Redis Sorted Set 實時排名
8. **降級策略**：故障時優雅降級

**記住：實時性、高並發、內容安全，三者缺一不可！**

**Bilibili 的啟示**：
- 支持數百萬並發用戶
- 彈幕是核心競爭力
- 簡單勝過複雜（WebSocket + Redis）
- 優化永無止境（批量廣播、採樣、壓縮）

**核心理念：Real-time, scalable, and safe.（實時、可擴展、安全）**
