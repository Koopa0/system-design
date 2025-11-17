# News Feed - 動態流系統

> Twitter/Facebook 動態流設計：從 Pull 到 Push 再到 Hybrid 模型

## 概述

本章節展示如何設計一個高性能的社交媒體動態流系統（News Feed），支持：
- **秒級響應**：P99 延遲 < 100ms
- **高吞吐**：支持百萬級 DAU
- **混合模型**：兼顧讀寫性能
- **明星用戶**：優雅處理粉絲數百萬的用戶

## 學習目標

- 理解 **Pull vs Push vs Hybrid** 模型的權衡
- 掌握 **Fanout-on-Write** 和 **Fanout-on-Read** 的實現
- 學習 **Feed 排序算法**（EdgeRank）
- 實踐 **Cursor 分頁**優化
- 了解 **Redis 緩存策略**
- 學習 **Twitter 的真實架構演進**

## 核心概念

### 1. Pull 模型 (Fanout-on-Read)

```
用戶刷新動態：
1. 查詢所有關注者的 ID
2. 查詢每個關注者的最新帖子
3. 合併排序後返回

優勢：
✅ 寫入快（只需插入一條帖子）
✅ 存儲少

劣勢：
❌ 讀取慢（每次實時計算）
❌ 數據庫壓力大（N+1 查詢問題）

適用場景：小規模、讀少寫多
```

### 2. Push 模型 (Fanout-on-Write)

```
用戶發帖：
1. 插入帖子到 posts 表
2. 查詢該用戶的所有粉絲
3. Fanout：將帖子推送到每個粉絲的 Feed

用戶刷新動態：
1. 直接讀取自己的 Feed（已預計算）

優勢：
✅ 讀取快（直接返回預計算結果）
✅ 數據庫壓力小

劣勢：
❌ 寫入慢（需要 Fanout 給所有粉絲）
❌ 存儲多（每個用戶一份 Feed）
❌ 明星用戶問題（100萬粉絲 = 100萬次寫入）

適用場景：中規模、讀多寫少
```

### 3. Hybrid 模型（推薦）

```
核心思想：
- 普通用戶（粉絲數 < 10,000）：Fanout-on-Write
- 明星用戶（粉絲數 > 10,000）：Fanout-on-Read

讀取流程：
1. 從 Feed 表讀取（普通用戶的帖子，已 Fanout）
2. 實時查詢關注的明星用戶帖子（Pull）
3. 合併排序後返回

優勢：
✅ 讀取快（大部分帖子已預計算）
✅ 寫入快（明星用戶跳過 Fanout）
✅ 兼顧性能和成本

劣勢：
⚠️ 實現複雜度高

適用場景：大規模社交網絡（Twitter、Facebook、Instagram）
```

### 4. Feed 排序算法

**EdgeRank 公式**：
```
Score = Affinity × Weight × Time_Decay

- Affinity（親密度）：用戶與發帖人的互動頻率
- Weight（權重）：內容類型權重（視頻 > 圖片 > 文字）
- Time_Decay（時間衰減）：新帖子優先

現代方法：
- 機器學習排序（XGBoost、Deep Learning）
- 預測用戶是否會互動（點讚/評論/分享）
- A/B 測試持續優化
```

### 5. Cursor 分頁

```
❌ OFFSET 分頁問題：
- 性能差（OFFSET 10000 需要跳過 10000 條記錄）
- 數據不一致（新帖子插入導致重複）

✅ Cursor 分頁（推薦）：
- 使用上一頁最後一條記錄作為游標
- WHERE created_at < cursor_time
- 性能穩定、數據一致
```

## 技術棧

- **語言**: Golang (標準庫優先)
- **緩存**: Redis (Sorted Set)
- **消息隊列**: Kafka (異步 Fanout)
- **數據庫**: MySQL/PostgreSQL
- **緩存**: Memcached (帖子詳情)

## 架構演進

### 階段 1：Pull 模型（慢）
- ❌ P99 延遲：1+ 秒
- ❌ 數據庫壓力大

### 階段 2：Fanout-on-Write
- ✅ P99 延遲：20ms
- ❌ 明星用戶發帖慢（17 分鐘）

### 階段 3：Hybrid 模型（最終）
- ✅ P99 延遲：< 100ms
- ✅ 明星用戶寫入快（10ms）
- ✅ 讀取快（60ms）

## 性能指標

```
最終系統性能（Hybrid 模型 + Redis 緩存）：

讀取延遲：
- P50: 20ms
- P99: 100ms
- P99.9: 300ms

寫入延遲：
- 普通用戶：50ms（包含 Fanout）
- 明星用戶：10ms（跳過 Fanout）

吞吐量：
- 讀取 QPS：100,000+
- 寫入 QPS：10,000+

容量：
- 支持：100 萬 DAU
- Feed 存儲：6.4 GB（Redis）
- 帖子存儲：1 TB（MySQL）
```

## 項目結構

```
15-news-feed/
├── DESIGN.md              # 詳細設計文檔（蘇格拉底式教學）
├── README.md              # 本文件
├── cmd/
│   └── server/
│       └── main.go        # HTTP 服務器
├── internal/
│   ├── timeline.go        # Pull 模型實現
│   ├── fanout.go          # Push 模型實現
│   ├── hybrid.go          # Hybrid 模型實現
│   ├── ranking.go         # Feed 排序算法
│   ├── pagination.go      # Cursor 分頁
│   └── cache.go           # Redis 緩存
└── docs/
    ├── performance.md     # 性能測試報告
    └── twitter-case.md    # Twitter 案例研究
```

## 快速開始

### 1. 數據庫設計

```sql
-- 用戶表
CREATE TABLE users (
    id VARCHAR(64) PRIMARY KEY,
    username VARCHAR(100) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 關注關係表
CREATE TABLE follows (
    follower_id VARCHAR(64) NOT NULL,
    followee_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (follower_id, followee_id),
    INDEX idx_followee (followee_id)
);

-- 帖子表
CREATE TABLE posts (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    content TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_created (user_id, created_at DESC)
);

-- Feed 表（Fanout-on-Write 結果）
CREATE TABLE feed (
    user_id VARCHAR(64) NOT NULL,
    post_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (user_id, created_at DESC, post_id),
    INDEX idx_post (post_id)
);
```

### 2. Redis 設計

```bash
# Feed 列表（Sorted Set）
# Key: feed:{user_id}
# Score: Unix 時間戳
# Member: post_id

ZADD feed:Alice 1733160000 post_123
ZADD feed:Alice 1733159000 post_124

# 讀取最新 10 篇
ZREVRANGE feed:Alice 0 9

# 帖子詳情緩存（Hash）
# Key: post:{post_id}
HSET post:123 user_id "Bob"
HSET post:123 content "Hello World"
HSET post:123 created_at "1733160000"
```

### 3. API 示例

```bash
# 發布帖子
POST /posts
{
  "user_id": "Bob",
  "content": "Hello World!"
}

# 獲取動態流（第 1 頁）
GET /timeline?user_id=Alice&limit=10
{
  "posts": [...],
  "next_cursor": "eyJDcmVhdGVkQXQiOjE3MzMxNjAwMDB9",
  "has_more": true
}

# 獲取動態流（第 2 頁）
GET /timeline?user_id=Alice&limit=10&cursor=eyJDcmVhdGVkQXQiOjE3MzMxNjAwMDB9
```

## 關鍵設計決策

### 為什麼選擇 Hybrid 模型？

| 場景 | Pull | Push | Hybrid |
|------|------|------|--------|
| 讀延遲 | 慢（1s+） | 快（20ms） | 快（60ms） |
| 寫延遲 | 快（10ms） | 慢（1s+） | 快（10ms） |
| 明星用戶 | ✅ 支持 | ❌ 不支持 | ✅ 支持 |
| 存儲成本 | 低 | 高 | 中 |
| 適用規模 | 小 | 中 | 大 |

**結論**：Hybrid 兼顧讀寫性能，適合大規模社交網絡。

### 為什麼用 Cursor 分頁？

- ✅ **性能穩定**：無論第幾頁，查詢速度一致
- ✅ **數據一致**：新插入的帖子不影響當前分頁
- ✅ **簡單實用**：基於時間戳，實現簡單

### 為什麼用 Redis Sorted Set？

- ✅ **天然排序**：按時間戳自動排序
- ✅ **範圍查詢**：ZREVRANGE 快速獲取最新 N 篇
- ✅ **容量控制**：ZREMRANGEBYRANK 自動清理舊數據
- ✅ **高性能**：100,000+ QPS

## 常見問題

### Q1: Feed 如何實時更新？

**方案 1**：Long Polling
```
客戶端每 30 秒輪詢一次 /timeline
```

**方案 2**：WebSocket（推薦）
```
服務端有新帖子時，主動推送給客戶端
```

**方案 3**：Server-Sent Events（SSE）
```
單向推送，適合 Feed 更新通知
```

### Q2: 如何處理刪除的帖子？

```
用戶刪除帖子時：
1. 軟刪除：posts.deleted_at = NOW()
2. 異步清理：從所有粉絲的 Feed 中刪除
3. Redis 緩存失效：DEL post:{post_id}
```

### Q3: 如何估算成本？

```
場景：100 萬 DAU

Redis 成本：
- Feed 存儲：6.4 GB
- 帖子緩存：50 GB
- 總計：60 GB → AWS ElastiCache (r6g.xlarge) = $150/月

MySQL 成本：
- 帖子存儲：1 TB
- AWS RDS (db.r5.2xlarge) = $730/月

Kafka 成本：
- AWS MSK (kafka.m5.large × 3) = $450/月

總計：約 $1,330/月（100 萬 DAU）
```

### Q4: 如何擴展到 1000 萬 DAU？

```
水平擴展策略：

1. Redis Cluster（分片）：
   - 按 user_id hash 分片
   - 10-20 個節點

2. MySQL 分庫分表：
   - 按 user_id 分庫（16 個庫）
   - 按 created_at 分表（按月）

3. Kafka 分區：
   - 增加 Topic 分區數（100+）

4. 應用服務器：
   - 無狀態，水平擴展（50+ 實例）

成本：約 $10,000/月（1000 萬 DAU）
```

## 延伸閱讀

### 真實案例

- **Twitter**: [Scaling Timeline](https://blog.twitter.com/engineering/en_us/topics/infrastructure/2013/new-tweets-per-second-record-and-how)
- **Facebook**: [News Feed Architecture](https://engineering.fb.com/2013/03/20/core-data/tao-the-power-of-the-graph/)
- **Instagram**: [Feed Ranking](https://engineering.instagram.com/feed-ranking-2a8e4de0a735)

### 開源項目

- [Redis](https://github.com/redis/redis) - 高性能緩存
- [Kafka](https://github.com/apache/kafka) - 分布式消息隊列
- [Mastodon](https://github.com/mastodon/mastodon) - 開源社交網絡（參考實現）

### 論文與文章

- **EdgeRank: Facebook's News Feed Algorithm** (2010)
- **The Architecture Twitter Uses to Deal with 150M Active Users** (2013)
- **DDIA Chapter 11**: Stream Processing

### 相關章節

- **07-message-queue**: Kafka 消息隊列
- **05-distributed-cache**: Redis 分布式緩存
- **09-event-driven**: 事件驅動架構
- **18-instagram**: Instagram 系統設計

## 總結

從「凌晨三點的系統告警」（P99 延遲 8.5秒）到「秒級響應的 News Feed」（P99 < 100ms），我們學到了：

1. **模型選擇**：Hybrid 模型兼顧讀寫性能
2. **明星用戶**：跳過 Fanout，使用 Pull 模型
3. **緩存策略**：Redis Sorted Set 加速讀取
4. **分頁優化**：Cursor 分頁保證性能和一致性
5. **異步處理**：Kafka 解耦寫入和 Fanout

**記住：讀多寫少的場景，用空間換時間（預計算）！**

**核心理念：Right model for the right scale.（根據規模選擇合適的模型）**
