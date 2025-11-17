# Instagram - 圖片社交平台

> 完整的 Instagram 系統設計：從圖片上傳到推薦算法

## 概述

本章節展示如何設計一個生產級的**圖片社交平台（Instagram）**，支持：
- **圖片/視頻上傳**：S3 對象存儲 + CDN 加速
- **動態流（News Feed）**：Fanout-on-Write 混合模式
- **社交功能**：關注、點贊、評論
- **推薦系統**：協同過濾 + 內容推薦
- **全文搜索**：Elasticsearch
- **橫向擴展**：分庫分表（16 分片）
- **高可用**：多區域部署 + 降級策略

## 學習目標

- 理解 **S3 對象存儲** 和 **CDN 加速** 的設計
- 掌握 **圖片多尺寸生成** 和 **異步處理**
- 學習 **動態流（News Feed）** 的三種模式
- 實踐 **關注/粉絲系統** 和 **Redis 緩存**
- 了解 **點贊、評論** 的高並發設計
- 掌握 **推薦算法**（協同過濾、內容推薦）
- 學習 **Elasticsearch 全文搜索**
- 理解 **分庫分表** 策略
- 掌握 **最終一致性** 設計
- 學習 Instagram 的真實架構

## 核心概念

### 1. 圖片存儲演進

#### 本地存儲（不推薦）

```
問題：
❌ 單點故障（磁盤壞了 = 數據丟失）
❌ 擴展性差（磁盤空間有限）
❌ 無備份（數據沒有冗餘）
❌ 帶寬瓶頸（單台服務器提供）
```

#### S3 對象存儲（推薦）

```
優勢：
✅ 無限擴展（按需付費）
✅ 高可用（99.999999999% 持久性）
✅ 自動備份（多區域複製）
✅ 低成本（$0.023/GB/月）

架構：
Client → API Server → S3 (us-east-1)
```

#### S3 + CDN（最佳實踐）

```
優勢：
✅ 全球低延遲（就近訪問）
✅ 減輕源站壓力（CDN 緩存）
✅ 自動擴展（無需運維）

架構：
Client → CloudFront (全球邊緣節點) → S3
```

### 2. 圖片處理流水線

```
1. 用戶上傳原圖
   ↓
2. 保存到 S3 (photos/original/...)
   ↓
3. 觸發 Lambda / Worker
   ↓
4. 生成多個尺寸：
   - thumbnail: 150x150
   - medium: 640x640
   - large: 1080x1080
   ↓
5. 上傳到 S3
   ↓
6. 更新數據庫

優勢：
- 節省帶寬（手機加載 thumbnail，PC 加載 large）
- 提升用戶體驗（快速加載）
```

### 3. 動態流（News Feed）三種模式

#### Fanout-on-Read（拉模式）

```
用戶查看動態流時：
1. 查詢關注列表（1000 人）
2. 查詢這 1000 人的最新照片
3. 合併排序

優勢：
✅ 寫入快（直接插入照片表）
✅ 存儲成本低

劣勢：
❌ 讀取慢（IN 查詢 1000 人）
❌ 無法緩存（每個用戶不同）
```

#### Fanout-on-Write（推模式）

```
用戶發布照片時：
1. 查詢所有粉絲（10 萬人）
2. 寫入每個粉絲的動態流（Redis Sorted Set）

用戶查看動態流時：
1. 從 Redis 讀取（秒級）

優勢：
✅ 讀取快（直接從 Redis 讀取）
✅ 可預計算

劣勢：
❌ 大 V 發帖慢（1000 萬粉絲 = 寫入 1000 萬次）
❌ 存儲成本高
```

#### 混合模式（推薦）

```
普通用戶（< 10 萬粉絲）：Fanout-on-Write
大 V（> 10 萬粉絲）：Fanout-on-Read

用戶查看動態流時：
1. 從 Redis 讀取預計算的動態（普通用戶發的）
2. 實時查詢大 V 的最新動態
3. 合併排序

優勢：
✅ 兼顧讀寫性能
✅ 成本可控
```

### 4. 關注/粉絲系統

```sql
CREATE TABLE follow_relationships (
    follower_id VARCHAR(64),  -- 粉絲
    followee_id VARCHAR(64),  -- 被關注者
    created_at TIMESTAMP,
    UNIQUE (follower_id, followee_id)
);

-- Redis 緩存計數
SET user:alice:followers 10000    -- 粉絲數
SET user:alice:following 500      -- 關注數
```

### 5. 點贊和評論

```
設計要點：
1. 冗餘計數器（photos.like_count）
   - 避免實時 COUNT(*)
   - 提高查詢性能

2. Redis 緩存熱點數據
   - photo:123:likes → 1000
   - 減少數據庫壓力

3. 最終一致性
   - 插入 likes 表（立即）
   - 更新 like_count（異步，Kafka）
   - 前端樂觀更新（立即 +1）
```

### 6. 推薦算法

#### 協同過濾（Collaborative Filtering）

```
邏輯：
- 用戶 A 和 B 都喜歡照片 1、2、3
- A 喜歡的照片 4，B 可能也喜歡

實現：
1. 計算用戶相似度（共同點贊數）
2. 找到相似用戶
3. 推薦相似用戶喜歡的照片
```

#### 內容推薦（Content-Based）

```
邏輯：
- 分析用戶過去喜歡的照片特徵（標籤、地理位置）
- 推薦相似特徵的照片

實現：
1. 為照片打標籤（#food, #travel, #sunset）
2. 計算用戶興趣向量
3. 推薦高匹配度的照片
```

### 7. 分庫分表

```
分片策略：按 user_id hash（16 個分片）

shard_id = hash(user_id) % 16

表結構：
- photos_0, photos_1, ..., photos_15
- likes_0, likes_1, ..., likes_15
- follow_relationships_0, ..., follow_relationships_15

優勢：
✅ 橫向擴展（單分片壓力降低）
✅ 查詢性能提升（數據量減少）

挑戰：
⚠️ 跨分片查詢（需要 Elasticsearch 全局索引）
⚠️ 分布式事務（使用最終一致性）
```

## 技術棧

- **語言**: Golang (標準庫優先)
- **對象存儲**: AWS S3
- **CDN**: CloudFront
- **數據庫**: MySQL (分片)
- **緩存**: Redis (動態流、計數器)
- **消息隊列**: Kafka (異步任務)
- **搜索引擎**: Elasticsearch (全文搜索)
- **圖片處理**: AWS Lambda / Worker
- **監控**: Prometheus + Grafana

## 架構設計

```
┌─────────────────────────────────────────┐
│         CDN (CloudFront)                 │
│      (圖片、視頻加速)                     │
└───────────────┬─────────────────────────┘
                ↓
┌───────────────────────────────────────┐
│       Load Balancer (ALB)              │
└───────────────┬───────────────────────┘
                ↓
        ┌───────┴───────┐
        ↓               ↓
┌───────────┐     ┌───────────┐
│API Server │     │API Server │
└─────┬─────┘     └─────┬─────┘
      │                 │
      └────────┬────────┘
               ↓
   ┌───────────┼───────────┐
   ↓           ↓           ↓
┌──────┐  ┌──────┐  ┌────────────┐
│Redis │  │Kafka │  │Elasticsearch│
└──────┘  └──────┘  └────────────┘
   ↓           ↓
┌────────────────────────────────┐
│ Sharded MySQL (16 shards)      │
│ - photos_0 ~ photos_15         │
│ - likes_0 ~ likes_15           │
│ - follow_0 ~ follow_15         │
└────────────────────────────────┘
   ↓
┌────────────────────────────────┐
│ S3 (Object Storage)             │
│ - photos/original/              │
│ - photos/thumbnail/             │
│ - photos/medium/                │
│ - photos/large/                 │
└────────────────────────────────┘
```

## 項目結構

```
18-instagram/
├── DESIGN.md              # 詳細設計文檔（蘇格拉底式教學）
├── README.md              # 本文件
├── cmd/
│   └── server/
│       └── main.go        # HTTP 服務器
├── internal/
│   ├── photo.go           # 圖片上傳和管理
│   ├── feed.go            # 動態流服務
│   ├── follow.go          # 關注服務
│   ├── like.go            # 點贊服務
│   ├── comment.go         # 評論服務
│   ├── recommend.go       # 推薦服務
│   ├── search.go          # 搜索服務
│   └── shard.go           # 分片路由
└── docs/
    ├── api.md             # API 文檔
    └── instagram-case.md  # Instagram 案例研究
```

## 快速開始

### 1. 數據庫設計

```sql
-- 照片表（分片）
CREATE TABLE photos (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    s3_key VARCHAR(512) NOT NULL,
    cdn_url VARCHAR(1024),
    thumbnail_url VARCHAR(1024),
    medium_url VARCHAR(1024),
    large_url VARCHAR(1024),
    width INT,
    height INT,
    file_size BIGINT,
    caption TEXT,
    location VARCHAR(255),
    like_count INT DEFAULT 0,
    comment_count INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_id (user_id, created_at DESC),
    INDEX idx_created_at (created_at DESC)
);

-- 關注關係表（分片）
CREATE TABLE follow_relationships (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    follower_id VARCHAR(64) NOT NULL,
    followee_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_follower_followee (follower_id, followee_id),
    INDEX idx_follower (follower_id),
    INDEX idx_followee (followee_id)
);

-- 點贊表（分片）
CREATE TABLE likes (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    photo_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_photo_user (photo_id, user_id),
    INDEX idx_photo_id (photo_id),
    INDEX idx_user_id (user_id, created_at DESC)
);

-- 評論表（分片）
CREATE TABLE comments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    photo_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_photo_id (photo_id, created_at DESC),
    INDEX idx_user_id (user_id, created_at DESC)
);

-- 標籤表
CREATE TABLE tags (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL
);

-- 照片標籤關聯表
CREATE TABLE photo_tags (
    photo_id BIGINT NOT NULL,
    tag_id BIGINT NOT NULL,
    PRIMARY KEY (photo_id, tag_id),
    INDEX idx_tag_id (tag_id)
);
```

### 2. Redis 設計

```bash
# 動態流（Sorted Set）
# Key: feed:{user_id}
# Score: timestamp
# Value: photo_id
ZADD feed:alice 1733160000 12345
ZREVRANGE feed:alice 0 19  # 獲取最新 20 條

# 關注計數
SET user:alice:followers 10000
SET user:alice:following 500

# 點贊計數
SET photo:12345:likes 1000

# 點贊狀態（Set）
# Key: photo:{photo_id}:liked_by
SADD photo:12345:liked_by alice bob charlie
SISMEMBER photo:12345:liked_by alice  # 檢查 alice 是否點贊
```

### 3. API 示例

#### 3.1 上傳照片

```bash
POST /photos/upload
Content-Type: multipart/form-data

FormData:
- photo: (binary)
- caption: "Beautiful sunset 🌅"
- location: "San Francisco, CA"
- tags: ["sunset", "travel"]

# 響應
{
  "photo_id": 12345,
  "cdn_url": "https://cdn.example.com/photos/large/12345.jpg",
  "thumbnail_url": "https://cdn.example.com/photos/thumbnail/12345.jpg"
}
```

#### 3.2 獲取動態流

```bash
GET /feed?user_id=alice&limit=20

# 響應
{
  "photos": [
    {
      "photo_id": 12345,
      "user_id": "bob",
      "username": "Bob Smith",
      "cdn_url": "https://cdn.example.com/photos/large/12345.jpg",
      "caption": "Beautiful sunset",
      "like_count": 1000,
      "comment_count": 50,
      "liked_by_me": false,
      "created_at": "2025-01-15T10:30:00Z"
    },
    ...
  ]
}
```

#### 3.3 關注用戶

```bash
POST /follow
Content-Type: application/json

{
  "follower_id": "alice",
  "followee_id": "bob"
}

# 響應
{
  "success": true
}
```

#### 3.4 點贊照片

```bash
POST /photos/12345/like
Content-Type: application/json

{
  "user_id": "alice"
}

# 響應
{
  "success": true,
  "like_count": 1001
}
```

#### 3.5 評論照片

```bash
POST /photos/12345/comments
Content-Type: application/json

{
  "user_id": "alice",
  "content": "Wow! Amazing photo!"
}

# 響應
{
  "comment_id": 67890,
  "created_at": "2025-01-15T10:35:00Z"
}
```

#### 3.6 搜索照片

```bash
GET /search?q=sunset&limit=20

# 響應
{
  "photos": [
    {
      "photo_id": 12345,
      "cdn_url": "...",
      "caption": "Beautiful sunset 🌅",
      "like_count": 1000
    },
    ...
  ]
}
```

#### 3.7 推薦照片

```bash
GET /recommendations?user_id=alice&limit=20

# 響應
{
  "photos": [
    {
      "photo_id": 54321,
      "cdn_url": "...",
      "reason": "Based on your interest in #travel"
    },
    ...
  ]
}
```

## 性能指標

```
系統容量（100 台 API 服務器，16 個數據庫分片）：

QPS：
- 上傳照片: 10,000 次/秒
- 查看動態流: 100,000 次/秒（Redis 緩存）
- 點贊: 50,000 次/秒
- 評論: 20,000 次/秒
- 搜索: 10,000 次/秒

延遲：
- 上傳照片: P99 < 2s（包含上傳到 S3）
- 查看動態流: P99 < 200ms（Redis 緩存命中率 95%）
- 點贊: P99 < 100ms
- 搜索: P99 < 500ms

可用性：
- 系統可用性: 99.95%
- 圖片可用性: 99.999999999%（S3 SLA）
```

## 成本估算

### 場景：1 億用戶，平均每人 100 張照片

```
存儲成本：

照片數量: 100 億張
每張照片 5 個版本（original + 4 個尺寸）
每個版本平均 500KB

總存儲: 100 億 × 5 × 500KB = 25 PB

S3 成本:
- 25 PB × $0.023/GB/月 = $575,000/月

帶寬成本：

每天照片查看: 10 億次
每次平均 200KB（medium 版本）

總帶寬: 10 億 × 200KB = 200 TB/天 = 6 PB/月

CDN 成本:
- 6 PB × $0.085/GB = $510,000/月

計算成本：

API 服務器: 100 台 (c5.2xlarge)
- 100 × $250/月 = $25,000/月

數據庫: 16 分片 (db.r5.4xlarge)
- 16 × $1,200/月 = $19,200/月

Redis Cluster: 10 節點 (r6g.4xlarge)
- 10 × $800/月 = $8,000/月

Kafka: 6 節點 (kafka.m5.2xlarge)
- 6 × $600/月 = $3,600/月

Elasticsearch: 10 節點 (r5.2xlarge.search)
- 10 × $700/月 = $7,000/月

總成本: 約 $1,147,800/月
單用戶成本: $0.011/月
```

### 成本優化建議

```
1. 圖片優化：
   - WebP 格式（比 JPEG 小 30%）
   - 延遲刪除不活躍照片（> 2 年未訪問）
   - 智能壓縮（根據設備自動選擇尺寸）
   → 節省 30% 存儲和帶寬成本

2. CDN 優化：
   - 增加緩存時間（減少回源）
   - 智能路由（選擇最便宜的 CDN）
   - 合併請求（雪碧圖）
   → 節省 20% CDN 成本

3. 計算優化：
   - Auto Scaling（根據流量動態調整）
   - Spot Instance（節省 70% EC2 成本）
   - 無服務器圖片處理（Lambda）
   → 節省 40% 計算成本
```

## 關鍵設計決策

### Q1: 為什麼選擇 S3 而不是自建存儲？

| 方案 | 優勢 | 劣勢 | 成本 |
|------|------|------|------|
| 自建存儲 | 一次性投入 | 運維成本高、擴展困難 | 高 |
| **S3** | 無限擴展、高可用、低運維 | 按量付費 | **低** |

**結論**：S3 是最佳選擇（類似 Instagram、Dropbox）。

### Q2: 為什麼需要多尺寸圖片？

```
場景：
- 手機查看動態流：150x150 縮略圖（10KB）
- PC 查看照片詳情：1080x1080 大圖（500KB）

如果只有一個版本（1080x1080）：
- 手機加載慢（500KB vs 10KB = 50 倍差距）
- 浪費帶寬（$0.085/GB）

方案：
- 生成 4 個尺寸（thumbnail/medium/large/original）
- 根據設備自動選擇

優勢：
✅ 加載速度快 50 倍
✅ 節省 90% 帶寬成本
```

### Q3: 為什麼動態流用 Fanout-on-Write？

```
對比：

Fanout-on-Read（實時查詢）：
- 讀取：查詢 1000 個關注者的照片 → 慢（1 秒）
- 寫入：直接插入照片表 → 快（10ms）

Fanout-on-Write（預計算）：
- 讀取：從 Redis 讀取 → 快（10ms）
- 寫入：寫入 10 萬個粉絲的動態流 → 慢（1 秒）

Instagram 的選擇：
- 讀取頻率 >> 寫入頻率（100:1）
- 用戶更關心讀取速度
- 所以選擇 Fanout-on-Write

大 V 優化：
- 粉絲 > 10 萬 → 改用 Fanout-on-Read
- 避免寫入放大
```

### Q4: 為什麼需要冗餘計數器？

```
場景：查詢照片點贊數

方案 1：實時 COUNT(*)
SELECT COUNT(*) FROM likes WHERE photo_id = 12345;

問題：
❌ 慢（掃描 100 萬行）
❌ 鎖表（影響其他查詢）

方案 2：冗餘字段（photos.like_count）
SELECT like_count FROM photos WHERE id = 12345;

優勢：
✅ 快（索引查詢）
✅ 不鎖表

代價：
⚠️ 需要維護一致性（最終一致性可接受）
```

### Q5: 為什麼選擇最終一致性？

```
場景：用戶點贊照片

強一致性（事務）：
BEGIN TRANSACTION
  INSERT INTO likes ...
  UPDATE photos SET like_count = like_count + 1 ...
COMMIT

問題：
❌ 慢（鎖表）
❌ 跨分片事務（photos 和 likes 可能在不同分片）

最終一致性：
1. INSERT INTO likes ...（立即）
2. 發送 Kafka 消息
3. Worker 更新 like_count（延遲 1-2 秒）
4. 前端樂觀更新（用戶看到立即 +1）

優勢：
✅ 快（無鎖）
✅ 高可用（不依賴事務）
✅ 用戶體驗好（樂觀更新）

Instagram 的選擇：
- 短暫不一致可接受（1-2 秒）
- 性能 > 強一致性
```

## 常見問題

### Q1: 如何處理熱點數據？

```
問題：
- 大 V 發帖（1000 萬粉絲）
- 1000 萬次 Redis 寫入 → 慢

解決方案：
1. 異步 Fanout（Kafka + Worker）
   - 發帖立即返回
   - 後台慢慢寫入粉絲動態流

2. 批量寫入（Pipeline）
   - Redis Pipeline 批量寫入 1000 條/次
   - 提升 10 倍性能

3. 降級策略
   - 超級大 V（> 1000 萬粉絲）改用 Fanout-on-Read
   - 避免寫入放大
```

### Q2: 如何實現「查看誰點贊了我的照片」？

```
數據結構：

MySQL:
SELECT user_id FROM likes WHERE photo_id = 12345 LIMIT 100

Redis Set:
SADD photo:12345:liked_by alice bob charlie
SMEMBERS photo:12345:liked_by

方案：
- 最近 1000 個點贊存 Redis（快速查詢）
- 完整數據存 MySQL（分頁查詢）
```

### Q3: 如何支持視頻上傳？

```
視頻處理流程：

1. 用戶上傳原視頻 → S3
2. 觸發轉碼任務（AWS MediaConvert / FFmpeg）
3. 生成多個碼率：
   - 360p（手機）
   - 720p（PC）
   - 1080p（高清）
4. 上傳到 S3
5. 生成視頻縮略圖（第 1 秒截圖）
6. 更新數據庫

挑戰：
- 轉碼耗時（1 分鐘視頻 → 5 分鐘轉碼）
- 需要異步處理 + 進度通知
```

### Q4: 如何防止刷贊？

```
檢測：
1. 頻率限制（每用戶每天最多點贊 1000 次）
2. IP 限制（同一 IP 每小時最多 100 次）
3. 設備限制（同一設備 ID 每小時最多 100 次）
4. 行為分析（點贊間隔 < 1 秒 = 可疑）

處罰：
- 警告 → 限流 → 封號
```

### Q5: 如何實現「Stories」功能？

```
Stories 特點：
- 24 小時後自動刪除
- 查看狀態（誰看過）
- 高並發（大量查看）

設計：
1. 存儲：
   - Redis Sorted Set（過期自動刪除）
   - Key: stories:{user_id}
   - Score: expire_at
   - Value: story_id

2. 查看記錄：
   - Redis Set
   - Key: story:{story_id}:viewed_by
   - Value: user_id

3. 定時清理：
   - Cron Job 每小時刪除過期 Stories
```

### Q6: 如何監控系統健康？

```
關鍵指標：

1. 業務指標：
   - DAU（日活躍用戶）
   - 上傳量（照片/秒）
   - 動態流查看量（次/秒）

2. 性能指標：
   - API 延遲（P50, P99）
   - 數據庫 QPS
   - Redis 命中率

3. 錯誤率：
   - 上傳失敗率 < 0.1%
   - API 錯誤率 < 0.5%

4. 資源使用率：
   - CPU < 70%
   - 內存 < 80%
   - 磁盤 < 85%

告警：
- P99 延遲 > 1s → P1 告警
- 錯誤率 > 1% → P0 告警
- S3 不可用 → P0 告警
```

## 延伸閱讀

### 真實案例

- **Instagram Engineering Blog**: [instagram-engineering.com](https://instagram-engineering.com/)
- **Scaling Instagram**: [Scaling Instagram Infrastructure](https://www.youtube.com/watch?v=hnpzNAPiC0E)
- **Instagram at 14M Users**: [Instagram's Architecture](https://www.slideshare.net/iammutex/instagram-architecture-14m-users)
- **Facebook Photo Storage**: [Needle in a Haystack](https://engineering.fb.com/2009/04/30/core-infra/needle-in-a-haystack-efficient-storage-of-billions-of-photos/)

### 技術文檔

- **AWS S3**: [S3 Best Practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/Welcome.html)
- **CloudFront**: [CDN Best Practices](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Introduction.html)
- **Elasticsearch**: [Full-Text Search](https://www.elastic.co/guide/en/elasticsearch/reference/current/index.html)
- **Image Optimization**: [WebP Format](https://developers.google.com/speed/webp)

### 相關章節

- **05-distributed-cache**: Redis 分布式緩存
- **07-message-queue**: Kafka 消息隊列
- **15-news-feed**: News Feed 動態流系統
- **17-notification-service**: 通知服務（點贊通知、評論通知）

## 總結

從「本地存儲單張圖片」到「支持 1 億用戶的圖片社交平台」，我們學到了：

1. **存儲演進**：本地磁盤 → S3 → CDN
2. **圖片優化**：多尺寸 + WebP + 延遲加載
3. **動態流**：混合模式（Fanout-on-Write + Fanout-on-Read）
4. **社交功能**：關注、點贊、評論 + Redis 緩存
5. **推薦算法**：協同過濾 + 內容推薦
6. **全文搜索**：Elasticsearch
7. **橫向擴展**：分庫分表（16 分片）
8. **最終一致性**：性能 > 強一致性

**記住：簡單、可擴展、用戶體驗至上！**

**Instagram 的啟示**：
- 13 個工程師支持 3000 萬用戶（2012 年）
- 簡單勝過複雜
- 使用成熟的技術棧（PostgreSQL、Redis、S3）
- 專注核心功能（圖片分享）
- 提前規劃擴展性

**核心理念：Build fast, scale smart, delight users.（快速構建、智能擴展、取悅用戶）**
