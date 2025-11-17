# YouTube - 影片分享平台

> 完整的 YouTube 系統設計：從影片上傳到推薦算法

## 概述

本章節展示如何設計一個生產級的**影片分享平台（YouTube）**，支持：
- **影片上傳**：分片上傳、斷點續傳、S3 存儲
- **影片轉碼**：FFmpeg、多解析度（360p、720p、1080p、4K）
- **CDN 分發**：CloudFront、自適應碼率（HLS/DASH）
- **推薦系統**：協同過濾、內容推薦
- **評論互動**：評論、點贊、回覆
- **播放統計**：觀看次數、觀看時長、完成率
- **橫向擴展**：分庫分表、轉碼集群
- **成本優化**：CDN、存儲、轉碼優化

## 學習目標

- 理解 **分片上傳**（Chunked Upload）的實現
- 掌握 **S3 Multipart Upload** API
- 學習 **影片轉碼**（FFmpeg、GPU 加速）
- 實踐 **HLS/DASH** 自適應碼率播放
- 了解 **CDN 分發策略**
- 掌握 **推薦算法**（協同過濾、內容推薦）
- 學習 **Elasticsearch 全文搜索**
- 理解 **分庫分表**策略
- 掌握 **成本優化**（每月節省百萬美元）
- 學習 YouTube 的真實架構

## 核心概念

### 1. 分片上傳（Chunked Upload）

```
問題：
- 影片很大（5GB）
- 單次上傳超時
- 網絡中斷需重傳

方案：分片上傳
1. 初始化上傳會話
2. 將文件切分為 5MB 的分片
3. 並行上傳所有分片
4. 服務器合併分片

優勢：
✅ 支持超大文件
✅ 斷點續傳
✅ 並行上傳（提速）
✅ 可靠性高
```

### 2. S3 Multipart Upload

```
AWS S3 原生支持分片上傳：

1. Initiate Multipart Upload
   → 獲得 Upload ID

2. Upload Part (可並行)
   → 返回 ETag

3. Complete Multipart Upload
   → 合併所有分片

優勢：
✅ 無需自己合併文件
✅ 自動處理故障
✅ 最大支持 5TB
```

### 3. 影片轉碼

```
為什麼需要轉碼？
1. 統一格式（MP4、H.264）
2. 多解析度（360p、720p、1080p）
3. 壓縮（減小文件大小）
4. 優化播放（Fast Start）

FFmpeg 轉碼：
ffmpeg -i input.mp4 \
  -vf scale=1280:720 \
  -c:v libx264 -b:v 2500k \
  -c:a aac -b:a 128k \
  -movflags +faststart \
  output_720p.mp4

解析度配置：
- 360p: 640x360, 800 kbps
- 720p: 1280x720, 2.5 Mbps
- 1080p: 1920x1080, 5 Mbps
- 4K: 3840x2160, 20 Mbps
```

### 4. HLS 自適應碼率

```
HLS (HTTP Live Streaming)：
- 將影片切分為 10 秒的片段（.ts）
- 生成播放列表（.m3u8）
- 客戶端根據網速自動切換解析度

目錄結構：
videos/123/
  360p/
    playlist.m3u8
    segment0.ts
    segment1.ts
    ...
  720p/
    playlist.m3u8
    segment0.ts
    ...
  master.m3u8  # 主播放列表

客戶端播放：
1. 下載 master.m3u8
2. 檢測網速，選擇合適解析度
3. 下載對應的 playlist.m3u8
4. 依序下載 .ts 片段
5. 網速變化時自動切換解析度
```

### 5. CDN 分發

```
為什麼需要 CDN？
- 用戶在全球各地
- 直接訪問 S3 延遲高（跨國）
- 帶寬成本高

CDN 架構：
Client (中國) → CloudFront (亞太邊緣節點) → S3 (美國)
                    ↓ 緩存
                第二次訪問直接從邊緣節點返回

優勢：
✅ 低延遲（就近訪問）
✅ 高帶寬（分散流量）
✅ 減輕源站壓力
✅ 節省成本（CDN 緩存命中率 90%+）
```

### 6. 推薦算法

#### 協同過濾（Collaborative Filtering）

```
邏輯：
- 用戶 A 和 B 都觀看了影片 1、2、3
- A 觀看的影片 4，B 可能也感興趣

計算：
1. 找到相似用戶（觀看了相同影片）
2. 統計相似用戶觀看的影片
3. 推薦當前用戶未觀看的影片

適用：
✅ 冷啟動問題小（有歷史數據）
❌ 新影片難推薦
```

#### 內容推薦（Content-Based）

```
邏輯：
- 分析用戶過去觀看的影片特徵
- 推薦相似特徵的影片

特徵：
- 類別（科技、遊戲、美食）
- 標籤（#Python、#Minecraft、#料理）
- 時長（短視頻、長視頻）
- 上傳者

適用：
✅ 新影片可立即推薦
❌ 推薦範圍有限（只看科技類）
```

#### 混合推薦（Hybrid）

```
YouTube 的方案：
1. 候選生成（Candidate Generation）
   - 協同過濾：找 100 個候選影片

2. 排序（Ranking）
   - 機器學習模型（神經網絡）
   - 預測點擊率、觀看時長
   - 排序候選影片

3. 重排序（Re-ranking）
   - 多樣性（不要全是同類型）
   - 新鮮度（推薦最新影片）
   - 用戶偏好（已訂閱頻道優先）

優勢：
✅ 準確性高
✅ 多樣性好
✅ 實時性強
```

### 7. 分庫分表

```
分片策略：按 video_id hash（16 個分片）

shard_id = hash(video_id) % 16

表結構：
- videos_0, videos_1, ..., videos_15
- comments_0, comments_1, ..., comments_15

優勢：
✅ 單分片壓力降低
✅ 查詢性能提升
✅ 橫向擴展

挑戰：
⚠️ 跨分片查詢（需 Elasticsearch）
⚠️ 事務（使用最終一致性）
```

## 技術棧

- **語言**: Golang (標準庫優先)
- **對象存儲**: AWS S3
- **CDN**: CloudFront
- **數據庫**: MySQL (分片)
- **緩存**: Redis (熱門影片、用戶會話)
- **消息隊列**: Kafka (轉碼任務)
- **搜索引擎**: Elasticsearch (影片搜索)
- **轉碼**: FFmpeg, AWS MediaConvert
- **監控**: Prometheus + Grafana

## 架構設計

```
┌─────────────────────────────────────┐
│      CDN (CloudFront)                │
│   (影片、縮圖分發)                    │
└───────────────┬─────────────────────┘
                ↓
┌───────────────────────────────────┐
│       Load Balancer (ALB)          │
└───────────────┬───────────────────┘
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
│ - videos_0 ~ videos_15         │
│ - comments_0 ~ comments_15     │
└────────────────────────────────┘
   ↓
┌────────────────────────────────┐
│ S3 (Object Storage)             │
│ - videos/original/              │
│ - videos/360p/                  │
│ - videos/720p/                  │
│ - videos/1080p/                 │
│ - thumbnails/                   │
└────────────────────────────────┘
   ↓
┌────────────────────────────────┐
│ Transcoding Cluster (GPU)       │
│ - Worker 1 ~ Worker N           │
└────────────────────────────────┘
```

## 項目結構

```
20-youtube/
├── DESIGN.md              # 詳細設計文檔（蘇格拉底式教學）
├── README.md              # 本文件
├── cmd/
│   └── server/
│       └── main.go        # HTTP 服務器
├── internal/
│   ├── upload.go          # 分片上傳
│   ├── transcode.go       # 影片轉碼
│   ├── video.go           # 影片管理
│   ├── recommend.go       # 推薦算法
│   ├── comment.go         # 評論服務
│   ├── view.go            # 播放統計
│   └── shard.go           # 分片路由
└── docs/
    ├── api.md             # API 文檔
    └── youtube-case.md    # YouTube 案例研究
```

## 快速開始

### 1. 數據庫設計

```sql
-- 上傳會話表
CREATE TABLE upload_sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    filename VARCHAR(255) NOT NULL,
    file_size BIGINT NOT NULL,
    chunk_size BIGINT NOT NULL,
    total_chunks INT NOT NULL,
    status ENUM('pending', 'uploading', 'completed', 'failed') DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP,
    INDEX idx_user_id (user_id, created_at DESC)
);

-- 影片表（分片）
CREATE TABLE videos (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    original_s3_key VARCHAR(512),
    duration INT,                     -- 秒
    status ENUM('uploading', 'transcoding', 'ready', 'failed') DEFAULT 'uploading',
    view_count BIGINT DEFAULT 0,
    like_count BIGINT DEFAULT 0,
    comment_count BIGINT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMP,
    INDEX idx_user_id (user_id, created_at DESC),
    INDEX idx_status (status),
    INDEX idx_view_count (view_count DESC)
);

-- 影片格式表
CREATE TABLE video_formats (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    resolution VARCHAR(10),           -- 360p, 720p, 1080p, 4k
    format VARCHAR(10),               -- mp4, webm
    s3_key VARCHAR(512),
    cdn_url VARCHAR(1024),
    file_size BIGINT,
    bitrate INT,                      -- kbps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_video_resolution (video_id, resolution, format)
);

-- 評論表（分片）
CREATE TABLE video_comments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    parent_id BIGINT,                 -- 回覆評論
    content TEXT NOT NULL,
    like_count INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_video_id (video_id, created_at DESC),
    INDEX idx_parent_id (parent_id)
);

-- 觀看記錄表
CREATE TABLE video_views (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    user_id VARCHAR(64),
    watch_duration INT,               -- 觀看時長（秒）
    completion_rate DECIMAL(5,2),    -- 完成率（%）
    device_type VARCHAR(20),          -- mobile, desktop, tv
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_video_id (video_id),
    INDEX idx_user_id (user_id, created_at DESC)
);

-- 標籤表
CREATE TABLE video_tags (
    video_id BIGINT NOT NULL,
    tag VARCHAR(50) NOT NULL,
    PRIMARY KEY (video_id, tag),
    INDEX idx_tag (tag)
);
```

### 2. API 示例

#### 2.1 初始化上傳

```bash
POST /api/upload/initiate
Content-Type: application/json

{
  "user_id": "user123",
  "filename": "my_video.mp4",
  "file_size": 524288000  # 500 MB
}

# 響應
{
  "session_id": "abc123",
  "chunk_size": 5242880,  # 5 MB
  "total_chunks": 100
}
```

#### 2.2 上傳分片

```bash
POST /api/upload/chunk
Content-Type: multipart/form-data

FormData:
- session_id: abc123
- chunk_index: 0
- chunk: (binary data)

# 響應
{
  "status": "uploading",
  "uploaded_chunks": 1,
  "total_chunks": 100
}
```

#### 2.3 完成上傳

```bash
# 所有分片上傳完成後自動觸發

# 響應
{
  "status": "completed",
  "video_id": 12345
}
```

#### 2.4 獲取影片信息

```bash
GET /api/videos/12345

# 響應
{
  "video_id": 12345,
  "title": "My Video",
  "description": "...",
  "duration": 600,  # 10 分鐘
  "status": "ready",
  "formats": [
    {
      "resolution": "360p",
      "cdn_url": "https://cdn.example.com/videos/12345/360p.mp4",
      "file_size": 50000000
    },
    {
      "resolution": "720p",
      "cdn_url": "https://cdn.example.com/videos/12345/720p.mp4",
      "file_size": 120000000
    },
    {
      "resolution": "1080p",
      "cdn_url": "https://cdn.example.com/videos/12345/1080p.mp4",
      "file_size": 250000000
    }
  ],
  "hls_url": "https://cdn.example.com/videos/12345/master.m3u8",
  "thumbnail_url": "https://cdn.example.com/thumbnails/12345/thumb_0.jpg",
  "view_count": 10000,
  "like_count": 500,
  "comment_count": 50
}
```

#### 2.5 搜索影片

```bash
GET /api/search?q=python tutorial&limit=20

# 響應
{
  "videos": [
    {
      "video_id": 12345,
      "title": "Python Tutorial for Beginners",
      "thumbnail_url": "...",
      "duration": 3600,
      "view_count": 1000000,
      "published_at": "2025-01-01T00:00:00Z"
    },
    ...
  ]
}
```

#### 2.6 推薦影片

```bash
GET /api/recommend?user_id=user123&limit=20

# 響應
{
  "videos": [
    {
      "video_id": 67890,
      "title": "Advanced Python",
      "reason": "Based on your watch history"
    },
    ...
  ]
}
```

#### 2.7 添加評論

```bash
POST /api/videos/12345/comments
Content-Type: application/json

{
  "user_id": "user123",
  "content": "Great video!",
  "parent_id": null  # 頂級評論
}

# 響應
{
  "comment_id": 98765,
  "created_at": "2025-01-15T10:30:00Z"
}
```

#### 2.8 記錄觀看

```bash
POST /api/videos/12345/view
Content-Type: application/json

{
  "user_id": "user123",
  "watch_duration": 450,  # 觀看了 7.5 分鐘
  "total_duration": 600,  # 影片總長 10 分鐘
  "device_type": "mobile"
}

# 響應
{
  "success": true
}
```

## 性能指標

```
系統容量（100 台 API 服務器，16 個數據庫分片）：

QPS：
- 影片觀看：1,000,000 次/秒（CDN 緩存）
- 影片上傳：10,000 次/秒（分片上傳）
- 搜索：100,000 次/秒（Elasticsearch）
- 推薦：50,000 次/秒

延遲：
- 影片播放（CDN）：P99 < 100ms
- 搜索：P99 < 200ms
- 推薦：P99 < 500ms

轉碼：
- 單個影片（10 分鐘）：5-10 分鐘（4 個解析度）
- 每天處理：500 小時影片
- 轉碼集群：50 台 GPU 服務器

存儲：
- 每天上傳：500 小時影片
- 每小時影片：4 個解析度 × 平均 5GB = 20 GB
- 每天新增：10 TB
- 累積存儲：3.6 PB/年
```

## 成本估算

### 場景：1 億月活躍用戶

```
假設：
- 每人每天觀看 5 個影片
- 每個影片平均 10 分鐘
- 平均解析度 720p (2.5 Mbps)

帶寬成本：

每天觀看：
- 1 億 × 5 × 10 分鐘 × 2.5 Mbps = 7.5 PB/天
- 每月：225 PB

CDN 成本（CloudFront）：
- 225 PB × $0.085/GB = $19,125,000/月

存儲成本：

每天上傳：500 小時影片
每小時影片：4 個解析度 × 5GB = 20 GB
每月新增：300 TB

S3 成本：
- 300 TB/月 × $0.023/GB = $6,900/月（新增）
- 累積 3.6 PB × $0.023/GB = $82,800/月（總計）

轉碼成本：

500 小時/天 × 4 個解析度 × $0.015/分鐘 = $1,800/天 = $54,000/月

（使用 AWS MediaConvert 定價）

總成本：約 $19,261,700/月

單用戶成本：$0.19/月

主要成本佔比：
- CDN: 99.3%
- 存儲: 0.4%
- 轉碼: 0.3%
```

### 成本優化策略

```
1. CDN 優化：

方案 1：多 CDN 策略
- Cloudflare（便宜）：主要流量
- CloudFront（貴）：備用
- 節省 30% CDN 成本

方案 2：冷門影片降級
- 觀看量 < 1000 的影片直接從 S3 提供
- 節省 20% CDN 成本

方案 3：根據地區定價
- 發達國家：高質量 CDN
- 發展中國家：便宜 CDN
- 節省 15% CDN 成本

優化後 CDN 成本：$9,562,500/月（節省 50%）

2. 存儲優化：

方案 1：冷存儲
- 6 個月未觀看的影片 → S3 Glacier
- 成本：$0.004/GB（節省 83%）

方案 2：刪除低解析度
- 觀看量 < 100 的影片只保留 720p
- 節省 40% 存儲

方案 3：AV1 編碼
- 比 H.264 節省 30% 文件大小
- 節省 30% 存儲和帶寬

優化後存儲成本：$30,000/月（節省 64%）

3. 轉碼優化：

方案 1：按需轉碼
- 上傳後只轉 720p
- 有觀看時才轉其他解析度
- 節省 50% 轉碼成本

方案 2：GPU 加速
- 使用 NVIDIA GPU（提速 10 倍）
- 使用 Spot Instance（節省 70%）
- 節省 60% 轉碼成本

優化後轉碼成本：$21,600/月（節省 60%）

總成本優化後：約 $9,614,100/月

單用戶成本：$0.096/月

總節省：$9,647,600/月（50%）
```

## 關鍵設計決策

### Q1: 為什麼使用分片上傳？

```
單次上傳問題：
❌ 大文件（5GB）上傳超時
❌ 網絡中斷需重傳
❌ 無法並行上傳

分片上傳優勢：
✅ 支持超大文件
✅ 斷點續傳（只需重傳失敗分片）
✅ 並行上傳（提速 10 倍）
✅ 可靠性高

結論：大文件上傳必須使用分片上傳。
```

### Q2: 為什麼需要轉碼？

```
原因：
1. 格式統一（用戶上傳 MOV、AVI、MKV 等）
2. 多解析度（適配不同網速和設備）
3. 壓縮（減小文件大小，節省帶寬）
4. 優化播放（Fast Start，邊下邊播）

解析度選擇：
- 移動網絡：360p、480p
- Wi-Fi：720p、1080p
- 高端用戶：4K

結論：轉碼是必須的，直接用原始影片用戶體驗差。
```

### Q3: 為什麼使用 HLS 而不是 MP4？

```
MP4 問題：
❌ 下載完整文件才能播放
❌ 無法自適應碼率
❌ 跳轉慢（需要重新下載）

HLS 優勢：
✅ 邊下邊播（每段 10 秒）
✅ 自適應碼率（根據網速切換）
✅ 跳轉快（只需下載對應片段）
✅ 支持直播

結論：HLS 是影片平台的最佳選擇。
```

### Q4: 為什麼需要 CDN？

```
無 CDN 問題：
- 中國用戶訪問美國 S3：延遲 300ms+
- 帶寬成本高（S3 出站 $0.09/GB）
- 高峰期源站壓力大

有 CDN 優勢：
✅ 低延遲（就近訪問，< 50ms）
✅ 高帶寬（邊緣節點分散流量）
✅ 節省成本（緩存命中率 90%+）
✅ 減輕源站壓力

結論：CDN 是影片平台的核心基礎設施。
```

### Q5: 推薦算法如何選擇？

```
對比：

協同過濾：
優勢：✅ 簡單、準確
劣勢：❌ 新影片難推薦、冷啟動

內容推薦：
優勢：✅ 新影片可推薦
劣勢：❌ 推薦範圍有限

混合推薦（YouTube 方案）：
✅ 協同過濾 + 內容推薦
✅ 機器學習模型排序
✅ 實時性強

結論：大型平台必須使用混合推薦。
```

## 常見問題

### Q1: 如何處理上傳失敗？

```
場景：
- 用戶上傳到 80%，網絡中斷

方案：
1. 客戶端記錄已上傳的分片（localStorage）
2. 重連後查詢服務器已上傳分片
3. 只上傳未完成的分片

代碼：
GET /api/upload/sessions/abc123

# 響應
{
  "uploaded_chunks": [0, 1, 2, ..., 79],
  "total_chunks": 100
}

客戶端繼續上傳分片 80-99
```

### Q2: 如何防止盜鏈？

```
問題：
- 其他網站直接引用 CDN URL
- 消耗帶寬，增加成本

方案：簽名 URL（Pre-signed URL）

1. 生成簽名 URL（有效期 1 小時）：
   https://cdn.example.com/videos/123.mp4?
   signature=abc123&expires=1705294800

2. CloudFront 驗證簽名
3. 簽名過期或無效 → 403 Forbidden

優勢：
✅ 防止盜鏈
✅ 可控訪問時間
✅ 可限制 IP
```

### Q3: 如何實現影片預覽（10 秒片段）？

```
方案 1：服務器端生成

ffmpeg -i input.mp4 -ss 00:01:00 -t 00:00:10 preview.mp4

問題：
❌ 每個影片需額外存儲
❌ 轉碼成本增加

方案 2：HLS 片段（推薦）

HLS 已經切分為 10 秒片段
預覽：播放前 3 個片段（30 秒）

優勢：
✅ 無需額外存儲
✅ 無需額外轉碼
```

### Q4: 如何實現影片剪輯（用戶自行剪輯）？

```
方案：客戶端剪輯 + 服務器端處理

1. 客戶端選擇開始/結束時間
   start: 00:01:30
   end: 00:02:00

2. 發送到服務器
   POST /api/videos/123/clip
   {
     "start": 90,
     "end": 120
   }

3. 服務器使用 FFmpeg 剪輯
   ffmpeg -i input.mp4 -ss 90 -to 120 -c copy output.mp4

4. 上傳剪輯後的影片

優勢：
✅ 客戶端界面友好
✅ 服務器處理快速（-c copy 無需重新編碼）
```

### Q5: 如何處理版權問題（Content ID）？

```
YouTube Content ID 系統：

1. 版權方上傳原始影片

2. 生成影片指紋（Fingerprint）
   - 音頻指紋：Chromaprint
   - 視頻指紋：Perceptual Hash

3. 用戶上傳影片時比對指紋

4. 匹配到版權內容：
   - 選項 1：阻止上傳
   - 選項 2：允許上傳但廣告收入歸版權方
   - 選項 3：允許上傳但靜音處理

實現：
- 使用 Chromaprint 生成音頻指紋
- 存儲到數據庫
- 用戶上傳時比對（相似度 > 90% = 匹配）
```

### Q6: 如何監控系統健康？

```
關鍵指標：

1. 業務指標：
   - DAU（日活躍用戶）
   - 每日上傳影片數
   - 每日觀看次數
   - 平均觀看時長

2. 性能指標：
   - 影片加載延遲（P50, P99）
   - CDN 緩存命中率
   - 轉碼完成時間

3. 錯誤率：
   - 上傳失敗率 < 1%
   - 播放失敗率 < 0.5%
   - 轉碼失敗率 < 0.1%

4. 成本指標：
   - CDN 帶寬成本
   - 存儲成本
   - 轉碼成本

告警：
- 播放失敗率 > 1% → P0 告警
- CDN 成本異常增長 → P1 告警
- 轉碼隊列積壓 > 1000 → P2 告警
```

## 延伸閱讀

### 真實案例

- **YouTube Architecture**: [YouTube Scalability](https://www.youtube.com/watch?v=w5WVu624fY8)
- **Netflix Video Processing**: [Netflix Tech Blog](https://netflixtechblog.com/)
- **TikTok Engineering**: [TikTok at Scale](https://www.infoq.com/presentations/tiktok-scale/)

### 技術文檔

- **FFmpeg**: [FFmpeg Documentation](https://ffmpeg.org/documentation.html)
- **HLS**: [HTTP Live Streaming Spec](https://datatracker.ietf.org/doc/html/rfc8216)
- **AWS S3 Multipart Upload**: [S3 Documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/mpuoverview.html)
- **CloudFront**: [CDN Best Practices](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Introduction.html)

### 相關章節

- **18-instagram**: 圖片上傳和 CDN（類似概念）
- **21-netflix**: 串流播放（HLS/DASH 詳解）
- **05-distributed-cache**: Redis 緩存（熱門影片緩存）
- **12-distributed-kv-store**: 分片策略

## 總結

從「簡單上傳」到「完整的影片平台」，我們學到了：

1. **分片上傳**：S3 Multipart Upload、斷點續傳
2. **影片轉碼**：FFmpeg、多解析度、HLS
3. **CDN 分發**：全球低延遲、自適應碼率
4. **推薦算法**：協同過濾、內容推薦、混合推薦
5. **橫向擴展**：分庫分表、轉碼集群
6. **成本優化**：多 CDN、冷存儲、按需轉碼（節省 50%）

**記住：可靠性、用戶體驗、成本，三者需要平衡！**

**YouTube 的啟示**：
- 每分鐘上傳 500 小時影片
- 20 億月活躍用戶
- 簡單勝過複雜（S3 + CDN + FFmpeg）
- 成本優化永無止境（每月節省千萬美元）

**核心理念：Scalable, reliable, cost-effective.（可擴展、可靠、成本優化）**
