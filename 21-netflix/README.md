# Netflix - 串流影音平台

> 完整的 Netflix 系統設計：從個人化推薦到全球 CDN 部署

## 概述

本章節展示如何設計一個生產級的**串流影音平台（Netflix）**，支援：
- **自適應串流**：HLS/DASH、多碼率、自動切換品質
- **個人化推薦**：協同過濾、內容推薦、深度學習
- **訂閱管理**：多方案、自動續訂、付費系統
- **全球 CDN**：Open Connect、ISP 合作、低延遲
- **觀看同步**：多裝置、斷點續播
- **A/B 測試**：實驗框架、流量分配
- **離線下載**：行動裝置支援
- **成本優化**：編碼優化、頻寬節省

## 學習目標

- 理解 **自適應串流**（ABR - Adaptive Bitrate Streaming）
- 掌握 **個人化推薦系統**的多層架構
- 學習 **Open Connect CDN** 的設計理念
- 實踐**訂閱制付費系統**
- 了解**多裝置同步**機制
- 掌握 **A/B 測試框架**
- 學習**成本優化**策略（節省數百萬美元）
- 理解 Netflix 的真實架構

## 核心概念

### 1. 自適應串流（Adaptive Bitrate Streaming）

```
問題：
- 用戶網速不同（4G、Wi-Fi、光纖）
- 網速會動態變化
- 固定碼率導致卡頓或浪費流量

方案：自適應串流（ABR）
1. 影片轉碼為多種碼率（240p~4K）
2. 切分為小片段（2-10 秒）
3. 客戶端根據網速自動選擇碼率
4. 動態調整，無縫切換

協定：
- HLS (HTTP Live Streaming) - Apple
- DASH (Dynamic Adaptive Streaming over HTTP) - ISO 標準
- Netflix 使用優化版 DASH

優勢：
✅ 適應網速變化
✅ 減少緩衝
✅ 節省流量
✅ 提升體驗
```

### 2. Netflix 推薦系統架構

```
三層推薦架構：

第一層：候選生成（Candidate Generation）
目標：從數萬部內容中選出 2000 部候選

方法：
- 協同過濾：基於相似用戶的觀看歷史
- 內容推薦：基於影片特徵（類型、演員、導演）
- 熱門推薦：全站或分類熱門
- 持續觀看：未看完的內容

第二層：排序（Ranking）
目標：從 2000 部候選中排序出前 500 部

方法：
- 深度學習模型（DNN）
- 特徵：用戶畫像、影片特徵、情境特徵
- 預測：點擊率、觀看時長、完成率

第三層：重排序（Re-ranking）
目標：優化最終推薦列表

考慮因素：
- 多樣性：避免同類型影片扎堆
- 新鮮度：推薦新上架內容
- 個人化：已訂閱頻道優先
- 商業目標：自製內容推廣

最終輸出：20 部個人化推薦
```

### 3. Open Connect CDN

```
為什麼 Netflix 不用傳統 CDN？

傳統 CDN 問題：
❌ 成本高（每月數千萬美元）
❌ 延遲不穩定
❌ 高峰期頻寬不足
❌ 無法完全控制

Netflix Open Connect：
✅ 自建 CDN 網路
✅ 與 ISP 深度合作
✅ 在 ISP 機房部署伺服器
✅ 用戶從 ISP 的 Netflix 伺服器獲取影片

架構：
- 全球 > 7000 台伺服器
- 分佈在 > 1000 個 ISP
- 覆蓋 > 95% 的 Netflix 流量
- 延遲：< 10ms（同城）

成本：
- 一次性硬體投資（伺服器、儲存）
- 運營成本（電力、頻寬、維護）
- 比傳統 CDN 節省 > 90%
```

### 4. 訂閱方案設計

```
多層級訂閱：

基本方案：$9.99/月
- 480p 解析度
- 1 個裝置同時觀看
- 標準畫質

標準方案：$15.49/月
- 1080p 解析度
- 2 個裝置同時觀看
- 高畫質

高級方案：$19.99/月
- 4K + HDR
- 4 個裝置同時觀看
- 超高畫質
- 支援離線下載

設計考量：
1. 價格歧視：不同用戶願付價格不同
2. 並發限制：防止帳號共享
3. 畫質限制：高品質吸引付費
4. 自動續訂：提高留存率
```

### 5. 觀看進度同步

```
多裝置同步：

場景：
- 客廳電視看到一半
- 切換到臥室平板繼續看
- 通勤時用手機看

技術方案：
1. 實時同步（每 10 秒）
   - 客戶端 → 服務器更新進度
   - 寫入 Redis（低延遲）

2. 批次寫入資料庫
   - 每 30 秒或停止播放時
   - 減少資料庫壓力

3. 衝突處理
   - 多裝置同時觀看
   - 取最新時間戳的進度

資料結構：
Redis Key: watch_progress:{user_id}:{video_id}
Value: {
  "position": 1234,      // 秒
  "updated_at": "2025-01-15T10:30:00Z",
  "device": "mobile"
}
```

## 技術棧

- **語言**: Golang (API)、Python (ML)、Java (部分服務)
- **對象儲存**: AWS S3
- **CDN**: Open Connect (自建)
- **資料庫**: MySQL (分片)、Cassandra (觀看歷史)
- **快取**: Redis (觀看進度、會話)、Memcached
- **訊息佇列**: Kafka (事件串流)
- **搜尋引擎**: Elasticsearch (影片搜尋)
- **推薦系統**: TensorFlow、PyTorch
- **轉碼**: FFmpeg、AWS MediaConvert
- **監控**: Prometheus + Grafana、Atlas (Netflix 自研)
- **A/B 測試**: 自研框架

## 架構設計

```
┌─────────────────────────────────────────────┐
│     Open Connect CDN (全球 7000+ 伺服器)      │
│        (影片分發、低延遲、高頻寬)               │
└───────────────────┬─────────────────────────┘
                    ↓
┌───────────────────────────────────────────┐
│       Load Balancer (AWS ELB)              │
└───────────────────┬───────────────────────┘
                    ↓
        ┌───────────┴───────────┐
        ↓                       ↓
┌─────────────┐         ┌─────────────┐
│ API Gateway │         │ API Gateway │
│  (Zuul)     │         │  (Zuul)     │
└──────┬──────┘         └──────┬──────┘
       │                       │
       └───────────┬───────────┘
                   ↓
   ┌───────────────┼───────────────────┐
   ↓               ↓                   ↓
┌──────────┐  ┌──────────┐    ┌──────────────┐
│ Streaming│  │Subscribe │    │Recommendation│
│ Service  │  │ Service  │    │   Service    │
└────┬─────┘  └────┬─────┘    └──────┬───────┘
     │             │                  │
     └─────────────┼──────────────────┘
                   ↓
   ┌───────────────┼───────────────────────┐
   ↓               ↓                       ↓
┌──────┐      ┌────────┐          ┌─────────────┐
│Redis │      │ Kafka  │          │Elasticsearch│
│(快取)│      │(事件流)│          │  (搜尋)     │
└──────┘      └────────┘          └─────────────┘
   ↓               ↓
┌────────────────────────────────────────┐
│ MySQL Cluster (16 shards)              │
│ - videos_0 ~ videos_15                 │
│ - subscriptions_0 ~ subscriptions_15   │
└────────────────────────────────────────┘
   ↓
┌────────────────────────────────────────┐
│ Cassandra Cluster                      │
│ - playback_sessions (時序資料)         │
│ - watch_progress (觀看進度)            │
└────────────────────────────────────────┘
   ↓
┌────────────────────────────────────────┐
│ S3 (Object Storage)                    │
│ - videos/original/                     │
│ - videos/240p/ ~ videos/4k/            │
│ - thumbnails/                          │
└────────────────────────────────────────┘
   ↓
┌────────────────────────────────────────┐
│ ML Platform (推薦系統)                  │
│ - Feature Store                        │
│ - Model Training (TensorFlow)          │
│ - Model Serving (TensorFlow Serving)  │
└────────────────────────────────────────┘
```

## 專案結構

```
21-netflix/
├── DESIGN.md              # 詳細設計文檔（蘇格拉底式教學）
├── README.md              # 本文件
├── cmd/
│   └── server/
│       └── main.go        # HTTP 服務器
├── internal/
│   ├── streaming.go       # 串流服務
│   ├── subscription.go    # 訂閱管理
│   ├── recommendation.go  # 推薦系統
│   ├── cdn.go            # CDN 選擇
│   ├── progress.go       # 觀看進度
│   ├── abtest.go         # A/B 測試
│   └── shard.go          # 分片路由
└── docs/
    ├── api.md            # API 文檔
    ├── netflix-case.md   # Netflix 案例研究
    └── open-connect.md   # Open Connect 架構
```

## 資料庫設計

### 影片相關表

```sql
-- 影片表（分片）
CREATE TABLE videos (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    duration INT NOT NULL,                   -- 秒
    release_year INT,
    rating VARCHAR(10),                      -- PG, PG-13, R, NC-17
    maturity_level VARCHAR(20),              -- kids, teens, adults
    status ENUM('draft', 'processing', 'published', 'archived') DEFAULT 'draft',
    thumbnail_url VARCHAR(1024),
    trailer_url VARCHAR(1024),
    original_s3_key VARCHAR(512),
    imdb_id VARCHAR(20),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_status (status),
    INDEX idx_release_year (release_year DESC),
    INDEX idx_published_at (published_at DESC),
    FULLTEXT idx_title_desc (title, description)
);

-- 影片格式表
CREATE TABLE video_formats (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    resolution VARCHAR(10),                  -- 240p, 360p, 480p, 720p, 1080p, 4k
    bitrate INT NOT NULL,                    -- kbps
    codec VARCHAR(20),                       -- h264, h265, vp9, av1
    audio_codec VARCHAR(20),                 -- aac, opus
    s3_key VARCHAR(512),
    hls_playlist_url VARCHAR(1024),
    dash_manifest_url VARCHAR(1024),
    file_size BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (video_id) REFERENCES videos(id) ON DELETE CASCADE,
    UNIQUE KEY uk_video_format (video_id, resolution, codec)
);

-- 影片分類表
CREATE TABLE video_genres (
    video_id BIGINT NOT NULL,
    genre VARCHAR(50) NOT NULL,
    PRIMARY KEY (video_id, genre),
    INDEX idx_genre (genre),
    FOREIGN KEY (video_id) REFERENCES videos(id) ON DELETE CASCADE
);

-- 演員表
CREATE TABLE video_cast (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    person_name VARCHAR(100) NOT NULL,
    role VARCHAR(50),                        -- actor, director, producer, writer
    character_name VARCHAR(100),
    display_order INT DEFAULT 0,
    INDEX idx_video_id (video_id),
    INDEX idx_person_name (person_name),
    FOREIGN KEY (video_id) REFERENCES videos(id) ON DELETE CASCADE
);
```

### 訂閱相關表

```sql
-- 訂閱方案表
CREATE TABLE subscription_plans (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(50) NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    billing_period VARCHAR(20) DEFAULT 'monthly',    -- monthly, yearly
    max_resolution VARCHAR(10),                      -- 480p, 1080p, 4k
    max_concurrent_streams INT,
    supports_download BOOLEAN DEFAULT FALSE,
    supports_hdr BOOLEAN DEFAULT FALSE,
    features JSON,
    status ENUM('active', 'deprecated') DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 用戶訂閱表（分片）
CREATE TABLE subscriptions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    plan_id INT NOT NULL,
    status ENUM('active', 'cancelled', 'expired', 'suspended', 'trial') DEFAULT 'active',
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    cancelled_at TIMESTAMP,
    auto_renew BOOLEAN DEFAULT TRUE,
    payment_method_id VARCHAR(64),
    trial_end_date TIMESTAMP,
    INDEX idx_user_id (user_id, started_at DESC),
    INDEX idx_status (status),
    INDEX idx_expires_at (expires_at),
    FOREIGN KEY (plan_id) REFERENCES subscription_plans(id)
);

-- 付款記錄表
CREATE TABLE payments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    subscription_id BIGINT,
    amount DECIMAL(10,2) NOT NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    payment_method VARCHAR(20),              -- credit_card, paypal, apple_pay, google_pay
    payment_provider VARCHAR(20),            -- stripe, paypal
    payment_provider_id VARCHAR(255),
    status ENUM('pending', 'succeeded', 'failed', 'refunded') DEFAULT 'pending',
    failure_reason TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_user_id (user_id, created_at DESC),
    INDEX idx_subscription_id (subscription_id),
    INDEX idx_status (status),
    FOREIGN KEY (subscription_id) REFERENCES subscriptions(id)
);
```

### 觀看相關表（Cassandra）

```cql
-- 播放會話表（時序資料，使用 Cassandra）
CREATE TABLE playback_sessions (
    session_id UUID PRIMARY KEY,
    user_id TEXT,
    video_id BIGINT,
    device_type TEXT,                        -- mobile, tablet, desktop, tv
    device_id TEXT,
    started_at TIMESTAMP,
    ended_at TIMESTAMP,
    watch_duration INT,                      -- 秒
    current_position INT,                    -- 當前位置
    completion_rate DECIMAL,
    quality_switches INT,                    -- 碼率切換次數
    avg_bitrate INT,
    buffer_events INT,                       -- 緩衝次數
    total_buffer_time INT,                   -- 總緩衝時間（秒）
    client_ip TEXT,
    cdn_server TEXT,
    INDEX (user_id, started_at),
    INDEX (video_id, started_at)
);

-- 觀看進度表（Cassandra）
CREATE TABLE watch_progress (
    user_id TEXT,
    video_id BIGINT,
    position INT,                            -- 當前位置（秒）
    last_watched_at TIMESTAMP,
    device_type TEXT,
    PRIMARY KEY (user_id, video_id)
);
```

### A/B 測試表

```sql
-- 實驗表
CREATE TABLE ab_experiments (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    hypothesis TEXT,
    start_date TIMESTAMP,
    end_date TIMESTAMP,
    status ENUM('draft', 'running', 'completed', 'cancelled') DEFAULT 'draft',
    created_by VARCHAR(64),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_status (status)
);

-- 變體表
CREATE TABLE ab_variants (
    id INT AUTO_INCREMENT PRIMARY KEY,
    experiment_id INT NOT NULL,
    name VARCHAR(50),                        -- control, variant_a, variant_b
    description TEXT,
    traffic_percentage INT,                  -- 0-100
    config JSON,
    FOREIGN KEY (experiment_id) REFERENCES ab_experiments(id) ON DELETE CASCADE
);

-- 用戶分配表
CREATE TABLE ab_assignments (
    user_id VARCHAR(64) NOT NULL,
    experiment_id INT NOT NULL,
    variant_id INT NOT NULL,
    assigned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, experiment_id),
    INDEX idx_experiment (experiment_id),
    FOREIGN KEY (experiment_id) REFERENCES ab_experiments(id),
    FOREIGN KEY (variant_id) REFERENCES ab_variants(id)
);

-- 事件表（Cassandra，時序資料）
CREATE TABLE ab_events (
    event_id UUID PRIMARY KEY,
    user_id TEXT,
    experiment_id INT,
    variant_id INT,
    event_type TEXT,                         -- impression, click, play, complete
    video_id BIGINT,
    metadata TEXT,                           -- JSON
    created_at TIMESTAMP,
    INDEX (experiment_id, created_at),
    INDEX (variant_id, created_at)
);
```

## API 文件

### 1. 影片播放

#### 1.1 獲取影片資訊

```bash
GET /api/v1/videos/{video_id}
Authorization: Bearer {token}

# 回應
{
  "video_id": 12345,
  "title": "Stranger Things S01E01",
  "description": "...",
  "duration": 3000,
  "release_year": 2016,
  "rating": "TV-14",
  "genres": ["Sci-Fi", "Horror", "Drama"],
  "cast": [
    {
      "name": "Millie Bobby Brown",
      "role": "actor",
      "character": "Eleven"
    }
  ],
  "thumbnail_url": "https://cdn.netflix.com/thumbnails/12345.jpg",
  "formats": [
    {
      "resolution": "240p",
      "bitrate": 300,
      "codec": "h264",
      "hls_url": "https://cdn.netflix.com/videos/12345/240p/playlist.m3u8"
    },
    {
      "resolution": "1080p",
      "bitrate": 5000,
      "codec": "h265",
      "hls_url": "https://cdn.netflix.com/videos/12345/1080p/playlist.m3u8"
    }
  ],
  "hls_master_url": "https://cdn.netflix.com/videos/12345/master.m3u8"
}
```

#### 1.2 開始播放

```bash
POST /api/v1/playback/start
Authorization: Bearer {token}
Content-Type: application/json

{
  "video_id": 12345,
  "device_type": "mobile",
  "device_id": "iphone-123"
}

# 回應
{
  "session_id": "abc-def-123",
  "playback_url": "https://cdn-tw-01.netflix.com/videos/12345/master.m3u8",
  "token": "eyJhbGc...",  # 播放令牌（防盜鏈）
  "expires_at": "2025-01-15T11:00:00Z"
}
```

#### 1.3 更新觀看進度

```bash
POST /api/v1/playback/progress
Authorization: Bearer {token}
Content-Type: application/json

{
  "session_id": "abc-def-123",
  "video_id": 12345,
  "position": 1234,      # 當前位置（秒）
  "quality": "1080p",
  "buffer_count": 2
}

# 回應
{
  "success": true
}
```

### 2. 訂閱管理

#### 2.1 獲取訂閱方案

```bash
GET /api/v1/plans

# 回應
{
  "plans": [
    {
      "id": 1,
      "name": "Basic",
      "price": 9.99,
      "currency": "USD",
      "max_resolution": "480p",
      "max_concurrent_streams": 1
    },
    {
      "id": 2,
      "name": "Standard",
      "price": 15.49,
      "currency": "USD",
      "max_resolution": "1080p",
      "max_concurrent_streams": 2
    },
    {
      "id": 3,
      "name": "Premium",
      "price": 19.99,
      "currency": "USD",
      "max_resolution": "4k",
      "max_concurrent_streams": 4,
      "supports_download": true,
      "supports_hdr": true
    }
  ]
}
```

#### 2.2 訂閱方案

```bash
POST /api/v1/subscriptions/subscribe
Authorization: Bearer {token}
Content-Type: application/json

{
  "plan_id": 2,
  "payment_method_id": "pm_1234567890",  # Stripe Payment Method ID
  "auto_renew": true
}

# 回應
{
  "subscription_id": 98765,
  "status": "active",
  "expires_at": "2025-02-15T00:00:00Z",
  "next_billing_date": "2025-02-15T00:00:00Z"
}
```

#### 2.3 取消訂閱

```bash
POST /api/v1/subscriptions/{subscription_id}/cancel
Authorization: Bearer {token}

# 回應
{
  "subscription_id": 98765,
  "status": "cancelled",
  "expires_at": "2025-02-15T00:00:00Z",  # 訂閱有效至期末
  "message": "Subscription will remain active until 2025-02-15"
}
```

### 3. 推薦系統

#### 3.1 個人化推薦

```bash
GET /api/v1/recommendations/personalized?limit=20
Authorization: Bearer {token}

# 回應
{
  "rows": [
    {
      "title": "Because you watched Stranger Things",
      "videos": [
        {"video_id": 123, "title": "Dark", "score": 0.95},
        {"video_id": 456, "title": "The OA", "score": 0.92}
      ]
    },
    {
      "title": "Trending Now",
      "videos": [...]
    },
    {
      "title": "Continue Watching",
      "videos": [...]
    }
  ]
}
```

#### 3.2 相似影片推薦

```bash
GET /api/v1/videos/{video_id}/similar?limit=10
Authorization: Bearer {token}

# 回應
{
  "videos": [
    {
      "video_id": 789,
      "title": "Dark",
      "similarity_score": 0.95,
      "thumbnail_url": "..."
    }
  ]
}
```

### 4. 搜尋

```bash
GET /api/v1/search?q=stranger things&limit=20
Authorization: Bearer {token}

# 回應
{
  "results": [
    {
      "video_id": 12345,
      "title": "Stranger Things",
      "type": "series",
      "thumbnail_url": "...",
      "match_score": 0.98
    }
  ],
  "total": 50
}
```

## 性能指標

### 系統容量

```
用戶規模：2 億月活躍用戶

QPS：
- 影片播放：500,000 次/秒（CDN）
- 推薦請求：100,000 次/秒
- 搜尋：50,000 次/秒
- 訂閱操作：1,000 次/秒

延遲：
- 影片載入（CDN）：P50 < 50ms, P99 < 200ms
- 推薦 API：P50 < 100ms, P99 < 300ms
- 搜尋 API：P50 < 50ms, P99 < 150ms

並發觀看：
- 尖峰時段：5000 萬並發串流
- 平均時段：1000 萬並發串流

轉碼：
- 每月新增：100 部電影 + 500 集影集
- 轉碼時間：每小時影片 15-30 分鐘（5 種解析度）
- 轉碼叢集：100 台 GPU 伺服器

儲存：
- 總內容：8000 部影片
- 平均每部：5 種解析度 × 20GB = 100GB
- 總儲存：800 TB（原始 + 轉碼）
```

### Open Connect 效能

```
伺服器分佈：
- 全球：7000+ 台伺服器
- ISP：1000+ 個合作夥伴
- 覆蓋率：95% 的流量

延遲：
- 同城 ISP：< 10ms
- 跨城市：< 30ms
- 跨國：< 100ms（備援）

快取命中率：> 95%

頻寬：
- 單伺服器：40 Gbps
- 總頻寬：280 Tbps
```

## 成本估算

### 場景：2 億月活躍用戶

```
假設：
- 每人每天觀看 2 小時
- 平均 1080p（5 Mbps）
- 訂閱轉換率：30%（6000 萬付費用戶）

收入：
- 6000 萬 × 平均 $13/月 = $780M/月

成本：

1. CDN（Open Connect）：
   - 硬體折舊：$5M/月
   - 電力：$2M/月
   - 頻寬（ISP 合作）：$3M/月
   - 小計：$10M/月

2. 儲存（S3）：
   - 800 TB × $0.023/GB = $18,400/月
   - 可忽略不計

3. 轉碼：
   - 100 部電影 + 500 集影集/月
   - GPU 伺服器：100 台 × $1000/月 = $100,000/月

4. 推薦系統：
   - GPU 叢集（深度學習）：$200,000/月
   - Spark 資料處理：$100,000/月

5. 資料庫：
   - MySQL：$50,000/月
   - Cassandra：$200,000/月

6. API 伺服器：
   - 500 台 × $500/月 = $250,000/月

7. 頻寬（非 Open Connect）：
   - 備援 CDN：$1M/月

總技術成本：約 $12M/月

單用戶技術成本：$0.06/月

毛利率：98.5%（不含內容授權費）

備註：Netflix 最大成本是內容授權和製作（佔收入 60-70%）
```

### 成本優化策略

```
1. 編碼優化：

   方案：AV1 編碼
   - 比 H.264 節省 30% 檔案大小
   - 節省頻寬和儲存
   - 節省：$3M/月

2. 預載優化：

   方案：智慧預載
   - 只在 Wi-Fi 預載
   - 根據觀看習慣預測
   - 節省 20% 不必要流量
   - 節省：$2M/月

3. Open Connect 擴展：

   方案：更多 ISP 合作
   - 覆蓋率 95% → 98%
   - 減少備援 CDN 使用
   - 節省：$500K/月

4. 儲存分層：

   方案：
   - 熱門內容：SSD
   - 普通內容：HDD
   - 冷門內容：S3 Glacier Deep Archive
   - 節省：40% 儲存成本（微小）

5. 推薦系統優化：

   方案：
   - 快取推薦結果（1 小時）
   - 減少 50% 即時計算
   - 節省：$100K/月

優化後總成本：約 $6.5M/月
單用戶成本：$0.033/月
節省：45%
```

## 關鍵設計決策

### Q1: 為什麼需要多種解析度？

```
原因：
1. 網速差異：
   - 4G：1-10 Mbps
   - Wi-Fi：10-100 Mbps
   - 光纖：100-1000 Mbps

2. 裝置差異：
   - 手機：720p 已足夠
   - 電腦：1080p
   - 電視：4K

3. 流量成本：
   - 手機用戶不想浪費流量

解決方案：自適應串流
- 提供 240p、360p、480p、720p、1080p、4K
- 客戶端自動選擇合適碼率
- 網速變化時自動切換

結論：多解析度是必需的，提升體驗並節省流量。
```

### Q2: 為什麼自建 CDN（Open Connect）？

```
對比：

傳統 CDN（CloudFront、Akamai）：
優勢：✅ 部署快速、✅ 全球覆蓋
劣勢：❌ 成本極高（$50-100M/月）、❌ 無法完全控制

Open Connect（自建）：
優勢：✅ 成本低（$10M/月）、✅ 延遲更低、✅ 完全控制
劣勢：❌ 建置時間長、❌ 需要與 ISP 談判

Netflix 的選擇：
- 流量巨大（30% 全球網路流量）
- 長期投資回報高
- 與 ISP 深度合作（雙贏）

結論：對於 Netflix 規模，自建 CDN 是最佳選擇。
```

### Q3: 推薦系統為什麼使用三層架構？

```
問題：
- 數萬部內容
- 需要即時推薦（< 100ms）
- 深度學習模型計算慢

三層架構：

第一層：候選生成（快速）
- 簡單算法（協同過濾、規則）
- 從數萬部中選出 2000 部
- 延遲：< 10ms

第二層：排序（精確）
- 深度學習模型
- 對 2000 部打分排序
- 延遲：< 50ms

第三層：重排序（商業邏輯）
- 多樣性、新鮮度
- 調整最終列表
- 延遲：< 10ms

總延遲：< 100ms

結論：三層架構平衡了速度和準確性。
```

### Q4: 為什麼使用 Cassandra 儲存觀看記錄？

```
需求：
- 寫入量大（每秒數百萬次）
- 時序資料（按時間查詢）
- 讀取較少

對比：

MySQL：
優勢：✅ ACID、✅ 事務
劣勢：❌ 寫入吞吐量低、❌ 需要複雜分片

Cassandra：
優勢：✅ 寫入快（Log-Structured）、✅ 橫向擴展、✅ 適合時序
劣勢：❌ 最終一致性

結論：觀看記錄是典型的時序資料，Cassandra 是最佳選擇。
```

### Q5: 如何防止帳號共享？

```
挑戰：
- 多人共享一個帳號
- 損失訂閱收入

技術方案：

1. 並發限制：
   - 基本方案：1 個裝置
   - 標準方案：2 個裝置
   - 高級方案：4 個裝置
   - 超過限制：提示「裝置數已滿」

2. 地理位置檢測：
   - 同一帳號在不同城市同時觀看
   - 標記為可疑帳號
   - 要求重新驗證

3. 裝置指紋：
   - 追蹤裝置數量
   - 超過 10 個裝置：標記異常

4. 商業策略：
   - 允許「家庭共享」（同一住址）
   - 價格分級（鼓勵升級方案）
   - 不要太激進（影響用戶體驗）

結論：技術 + 商業策略結合，平衡收入和體驗。
```

## 常見問題

### Q1: 如何處理播放卡頓？

```
原因：
1. 網速不足
2. CDN 伺服器過載
3. 客戶端解碼能力不足

解決方案：

1. 自適應碼率：
   - 即時監測網速
   - 自動降低解析度
   - 減少緩衝

2. CDN 切換：
   - 檢測到伺服器慢
   - 自動切換到其他伺服器

3. 預載緩衝：
   - 預載接下來 30 秒內容
   - 建立緩衝區

監控：
- 追蹤緩衝次數、時長
- 分析卡頓原因
- 優化 CDN 部署
```

### Q2: 如何實現離線下載？

```
需求：
- 行動裝置無網路時觀看
- 只有高級方案支援

實作：

1. 下載管理：
   - 客戶端選擇影片下載
   - 只在 Wi-Fi 下載（節省流量）
   - 儲存到本機加密儲存

2. DRM 保護：
   - 使用 Widevine（Android）或 FairPlay（iOS）
   - 加密影片檔案
   - 防止複製分享

3. 授權驗證：
   - 下載時驗證訂閱狀態
   - 離線播放時驗證授權（有效期 30 天）
   - 定期連網重新驗證

4. 儲存管理：
   - 限制下載數量（因裝置而異）
   - 自動刪除過期內容
```

### Q3: 如何處理不同地區的內容授權？

```
問題：
- 不同地區版權不同
- 某些內容只能在特定國家播放

實作：

1. 地理圍欄（Geo-fencing）：

   檢測用戶 IP：
   - 使用 GeoIP 資料庫
   - 判斷用戶所在國家

   內容過濾：
   - 查詢影片的可用地區
   - 只顯示該地區可觀看的內容

2. 資料庫設計：

   CREATE TABLE video_regions (
       video_id BIGINT,
       country_code VARCHAR(2),  -- US, UK, TW, etc.
       PRIMARY KEY (video_id, country_code)
   );

3. VPN 檢測：

   - 檢測已知 VPN IP 範圍
   - 阻擋或提示用戶關閉 VPN

   注意：過於嚴格會影響用戶體驗
```

### Q4: 推薦系統如何避免「同溫層」？

```
問題：
- 推薦算法只推薦相似內容
- 用戶看不到多樣化內容
- 體驗變差

解決方案：

1. 探索與利用（Exploration vs Exploitation）：

   - 80% 推薦用戶喜歡的內容（利用）
   - 20% 推薦新類型內容（探索）

2. 多樣性排序：

   - 避免連續推薦同類型影片
   - 分散不同類型、導演、演員

3. 驚喜推薦：

   - 隨機推薦高評分但用戶不常看的類型
   - 追蹤用戶反饋（點擊、觀看）

4. A/B 測試：

   - 測試不同多樣性策略
   - 找到最佳平衡點
```

### Q5: 如何監控和優化推薦系統？

```
關鍵指標：

1. 業務指標：
   - 點擊率（CTR）：推薦影片被點擊的比例
   - 觀看完成率：觀看超過 90% 的比例
   - 觀看時長：每日平均觀看時長

2. 推薦品質：
   - 準確率：推薦的影片用戶是否喜歡
   - 覆蓋率：推薦涵蓋多少比例的內容
   - 多樣性：推薦內容的多樣化程度

3. 系統效能：
   - 推薦 API 延遲：P50, P99
   - 模型訓練時間
   - 特徵計算成本

A/B 測試：
- 每週運行 10-20 個推薦實驗
- 測試不同算法、權重、UI
- 根據數據決定上線

告警：
- CTR 下降 > 5% → P1 告警
- API 延遲 > 300ms → P2 告警
- 模型訓練失敗 → P2 告警
```

## 延伸閱讀

### 真實案例

- **Netflix Tech Blog**: [Netflix Technology Blog](https://netflixtechblog.com/)
- **Open Connect**: [Netflix Open Connect](https://openconnect.netflix.com/)
- **Recommendation System**: [Netflix Recommendations: Beyond the 5 stars](https://netflixtechblog.com/netflix-recommendations-beyond-the-5-stars-part-1-55838468f429)
- **A/B Testing**: [It's All A/Bout Testing](https://netflixtechblog.com/its-all-a-bout-testing-the-netflix-experimentation-platform-4e1ca458c15)

### 技術文檔

- **HLS**: [HTTP Live Streaming](https://developer.apple.com/streaming/)
- **DASH**: [MPEG-DASH Standard](https://dashif.org/)
- **Widevine DRM**: [Google Widevine](https://www.widevine.com/)
- **Cassandra**: [Apache Cassandra](https://cassandra.apache.org/)
- **TensorFlow**: [TensorFlow Recommenders](https://www.tensorflow.org/recommenders)

### 相關章節

- **20-youtube**: 影片上傳和轉碼（類似概念）
- **18-instagram**: 社交功能和推薦
- **05-distributed-cache**: Redis 快取（觀看進度）
- **12-distributed-kv-store**: Cassandra 分散式儲存
- **15-news-feed**: 個人化推薦（相似算法）

## 總結

從「簡單播放」到「完整的串流平台」，我們學到了：

1. **自適應串流**：HLS/DASH、多碼率、無縫切換
2. **個人化推薦**：三層架構、深度學習、多樣性平衡
3. **訂閱管理**：多方案、自動續訂、防止帳號共享
4. **Open Connect**：自建 CDN、與 ISP 合作、成本優化（節省 90%）
5. **觀看同步**：多裝置、Redis 快取、斷點續播
6. **A/B 測試**：實驗框架、數據驅動決策
7. **成本優化**：AV1 編碼、智慧預載、儲存分層（節省 45%）

**記住：個人化、用戶體驗、成本效益，三者需要平衡！**

**Netflix 的啟示**：
- 2 億月活躍用戶
- 每天 10 億小時觀看時長
- 190 個國家和地區
- 個人化推薦是核心競爭力
- Open Connect 是成功關鍵（降低 90% CDN 成本）
- 數據驅動（A/B 測試文化）
- 持續創新（AV1 編碼、AI 推薦）

**核心理念：Personalized, scalable, cost-effective.（個人化、可擴展、成本優化）**

---

**下一步**：
- 實作推薦算法（協同過濾、深度學習）
- 搭建 A/B 測試平台
- 優化串流效能
- 探索 AI 推薦的最新技術
