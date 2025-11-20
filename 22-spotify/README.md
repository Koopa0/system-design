# Spotify - 音樂串流平台

> 完整的 Spotify 系統設計：從個人化推薦到社交音樂分享

## 概述

本章節展示如何設計一個生產級的**音樂串流平台（Spotify）**，支援：
- **音樂播放**：高音質串流、多音質選擇
- **播放列表**：建立、編輯、分享、協作
- **個人化推薦**：Discover Weekly、Daily Mix、Release Radar
- **社交功能**：追蹤朋友、分享音樂、動態消息
- **離線下載**：付費用戶可下載歌曲
- **多裝置同步**：無縫切換裝置
- **歌詞顯示**：即時同步歌詞
- **Podcast 支援**：播客內容管理
- **版權管理**：版稅計算與分潤

## 學習目標

- 理解**音訊編碼**格式（MP3、AAC、OGG Vorbis）
- 掌握**播放列表系統**設計
- 學習**音樂推薦算法**（協同過濾、音訊分析）
- 實踐**社交功能**（Follow、分享、動態）
- 了解**離線下載**機制
- 掌握**多裝置同步**
- 學習**版權管理**和**版稅計算**
- 理解**音訊特徵提取**（Danceability、Energy 等）
- 掌握 Spotify 的真實架構

## 核心概念

### 1. 音訊編碼格式

```
常見格式對比：

MP3：
- 最普及
- 專利已過期
- 檔案較大
- 音質：一般

AAC (Advanced Audio Coding)：
- Apple Music 使用
- 音質比 MP3 好 30%
- 檔案較小
- 需要授權費

OGG Vorbis：
- Spotify 使用
- 開源（無授權費）
- 音質優秀
- 檔案小

Opus：
- 最新標準
- 效率最高
- 低延遲（適合即時通訊）
- 逐漸普及

Spotify 的選擇：OGG Vorbis
原因：
✅ 開源（節省授權費）
✅ 音質好
✅ 檔案小（節省頻寬）
✅ 廣泛支援

音質等級：
- 低音質（Free）：96 kbps (~2.2 MB/首)
- 一般音質（Free）：160 kbps (~3.7 MB/首)
- 高音質（Premium）：320 kbps (~7.4 MB/首)

3 分鐘歌曲檔案大小：
- 96 kbps：2.2 MB
- 160 kbps：3.7 MB
- 320 kbps：7.4 MB
```

### 2. 播放列表系統

```
功能：
1. 個人播放列表：用戶自己建立
2. 協作播放列表：多人共同編輯
3. 公開播放列表：可被搜尋和追蹤
4. 官方播放列表：Spotify 策展

操作：
- 新增/移除歌曲
- 重新排序
- 下載整個播放列表
- 分享連結

挑戰：
- 大型播放列表（1000+ 首）的效能
- 即時同步（多裝置協作）
- 版本衝突處理
```

### 3. 音樂推薦系統

```
Spotify 的三層推薦技術：

第一層：協同過濾（Collaborative Filtering）
- 分析用戶聽歌行為
- 找到相似用戶
- 推薦相似用戶喜歡的歌曲

第二層：自然語言處理（NLP）
- 爬取網路音樂評論、部落格
- 分析歌曲被如何描述
- 建立「文化向量」
- 例：「適合健身」、「適合放鬆」

第三層：音訊分析（Audio Analysis）
- 使用 CNN 分析音訊波形
- 提取特徵：
  * Danceability：適合跳舞程度（0-1）
  * Energy：能量強度（0-1）
  * Valence：正面情緒（0-1）
  * Tempo：節奏（BPM）
  * Loudness：響度（dB）
  * Speechiness：語音成分（0-1）
  * Acousticness：聲學程度（0-1）
  * Instrumentalness：器樂成分（0-1）

混合推薦：
- 協同過濾：50%
- NLP：30%
- 音訊分析：20%

Discover Weekly：
- 每週一更新
- 推薦 30 首新歌
- 準確度 > 80%
- 是 Spotify 的殺手級功能
```

### 4. 社交功能

```
核心功能：
1. 追蹤朋友：看朋友在聽什麼
2. 分享音樂：分享歌曲、專輯、播放列表
3. 協作播放列表：多人共同編輯
4. 動態消息：朋友的聽歌動態

隱私設定：
- 公開活動：所有人可見
- 僅朋友：只有追蹤者可見
- 私密：完全隱藏

Spotify Connect：
- 多裝置控制
- 在電腦播放，用手機控制
- 無縫切換裝置
```

### 5. 離線下載

```
功能：
- 下載歌曲到本機
- 無網路時也能播放
- 只有 Premium 用戶可用

限制：
- 每個裝置最多 10000 首
- 最多 5 個裝置
- 每 30 天需連網驗證授權

DRM 保護：
- 加密儲存
- 防止複製和分享
- 取消訂閱後無法播放

技術方案：
- 使用 Widevine（Android）或 FairPlay（iOS）
- 檔案加密儲存在本機
- 播放時即時解密
```

### 6. 版權管理

```
版權類型：
1. 錄音版權（Recording Rights）：唱片公司
2. 作曲版權（Composition Rights）：作曲家、詞作家
3. 表演版權（Performance Rights）：表演者

版稅計算（Spotify 模式）：
1. 總收入池 = 月訂閱收入 × 70%
2. 每首歌版稅 = (該歌播放次數 / 總播放次數) × 收入池
3. 分配比例：
   - 唱片公司/版權方：70%
   - 藝人：15%
   - 作曲家：15%

範例計算：
假設：
- Spotify 月收入：$1B
- 版稅池：$700M（70%）
- 總播放次數：100B
- 某首歌播放次數：100M

該歌版稅：
$700M × (100M / 100B) = $700,000

每次播放版稅：
$700,000 / 100M = $0.007

實際每次播放版稅：$0.003 - $0.005
```

## 技術棧

- **語言**: Golang (API)、Python (ML/推薦系統)、Java (部分服務)
- **對象儲存**: AWS S3（音訊檔案）
- **CDN**: CloudFront、Akamai
- **資料庫**: MySQL（分片）、Cassandra（播放歷史、時序資料）
- **快取**: Redis（用戶會話、播放進度）、Memcached
- **訊息佇列**: Kafka（事件串流、版稅計算）
- **搜尋引擎**: Elasticsearch（歌曲搜尋）
- **推薦系統**: TensorFlow、Scikit-learn
- **音訊處理**: FFmpeg、Librosa（特徵提取）
- **監控**: Prometheus + Grafana

## 架構設計

```
┌─────────────────────────────────────────────┐
│         CDN (CloudFront / Akamai)            │
│        (音訊檔案、封面圖片分發)                │
└───────────────────┬─────────────────────────┘
                    ↓
┌───────────────────────────────────────────┐
│       Load Balancer (AWS ALB)              │
└───────────────────┬───────────────────────┘
                    ↓
        ┌───────────┴───────────┐
        ↓                       ↓
┌─────────────┐         ┌─────────────┐
│ API Gateway │         │ API Gateway │
└──────┬──────┘         └──────┬──────┘
       │                       │
       └───────────┬───────────┘
                   ↓
   ┌───────────────┼───────────────────────┐
   ↓               ↓                       ↓
┌──────────┐  ┌──────────┐        ┌──────────────┐
│  Music   │  │ Playlist │        │Recommendation│
│ Service  │  │ Service  │        │   Service    │
└────┬─────┘  └────┬─────┘        └──────┬───────┘
     │             │                      │
     └─────────────┼──────────────────────┘
                   ↓
   ┌───────────────┼───────────────────────────┐
   ↓               ↓                           ↓
┌──────┐      ┌────────┐              ┌─────────────┐
│Redis │      │ Kafka  │              │Elasticsearch│
│(快取)│      │(事件流)│              │  (搜尋)     │
└──────┘      └────────┘              └─────────────┘
   ↓               ↓
┌────────────────────────────────────────┐
│ MySQL Cluster (16 shards)              │
│ - tracks_0 ~ tracks_15                 │
│ - playlists_0 ~ playlists_15           │
│ - artists_0 ~ artists_15               │
└────────────────────────────────────────┘
   ↓
┌────────────────────────────────────────┐
│ Cassandra Cluster                      │
│ - playback_history (播放歷史)          │
│ - user_activities (用戶活動)           │
└────────────────────────────────────────┘
   ↓
┌────────────────────────────────────────┐
│ S3 (Object Storage)                    │
│ - tracks/96kbps/                       │
│ - tracks/160kbps/                      │
│ - tracks/320kbps/                      │
│ - covers/                              │
│ - artist-photos/                       │
└────────────────────────────────────────┘
   ↓
┌────────────────────────────────────────┐
│ ML Platform (推薦系統)                  │
│ - Audio Analysis (音訊特徵提取)        │
│ - Collaborative Filtering              │
│ - Model Training (TensorFlow)          │
└────────────────────────────────────────┘
```

## 專案結構

```
22-spotify/
├── DESIGN.md              # 詳細設計文檔（蘇格拉底式教學）
├── README.md              # 本文件
├── cmd/
│   └── server/
│       └── main.go        # HTTP 服務器
├── internal/
│   ├── music.go           # 音樂播放服務
│   ├── playlist.go        # 播放列表服務
│   ├── recommendation.go  # 推薦系統
│   ├── social.go          # 社交功能
│   ├── download.go        # 離線下載
│   ├── royalty.go         # 版稅計算
│   └── shard.go           # 分片路由
└── docs/
    ├── api.md             # API 文檔
    ├── spotify-case.md    # Spotify 案例研究
    └── audio-analysis.md  # 音訊分析技術
```

## 資料庫設計

### 核心表（MySQL）

```sql
-- 藝人表
CREATE TABLE artists (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    bio TEXT,
    avatar_url VARCHAR(1024),
    header_image_url VARCHAR(1024),
    verified BOOLEAN DEFAULT FALSE,
    monthly_listeners BIGINT DEFAULT 0,
    follower_count BIGINT DEFAULT 0,
    genres JSON,                             -- ["Rock", "Pop"]
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_name (name),
    INDEX idx_monthly_listeners (monthly_listeners DESC),
    FULLTEXT idx_name_bio (name, bio)
);

-- 專輯表
CREATE TABLE albums (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    artist_id BIGINT NOT NULL,
    release_date DATE,
    cover_url VARCHAR(1024),
    album_type ENUM('single', 'album', 'compilation', 'ep') DEFAULT 'album',
    total_tracks INT DEFAULT 0,
    label VARCHAR(255),                      -- 唱片公司
    copyright TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_artist_id (artist_id, release_date DESC),
    INDEX idx_release_date (release_date DESC),
    FULLTEXT idx_title (title),
    FOREIGN KEY (artist_id) REFERENCES artists(id)
);

-- 歌曲表（分片）
CREATE TABLE tracks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    artist_id BIGINT NOT NULL,
    album_id BIGINT,
    duration INT NOT NULL,                   -- 毫秒
    track_number INT,
    disc_number INT DEFAULT 1,
    explicit BOOLEAN DEFAULT FALSE,
    isrc VARCHAR(20),                        -- 國際標準錄音代碼
    popularity INT DEFAULT 0,                -- 0-100
    preview_url VARCHAR(1024),               -- 30 秒預覽
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_artist_id (artist_id),
    INDEX idx_album_id (album_id, track_number),
    INDEX idx_popularity (popularity DESC),
    FULLTEXT idx_title (title),
    FOREIGN KEY (artist_id) REFERENCES artists(id),
    FOREIGN KEY (album_id) REFERENCES albums(id)
);

-- 歌曲檔案表（多音質）
CREATE TABLE track_files (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    track_id BIGINT NOT NULL,
    quality ENUM('low', 'normal', 'high') DEFAULT 'normal',
    bitrate INT,                             -- kbps (96, 160, 320)
    codec VARCHAR(20),                       -- ogg, mp3, aac
    s3_key VARCHAR(512),
    cdn_url VARCHAR(1024),
    file_size BIGINT,                        -- bytes
    md5_hash VARCHAR(32),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE,
    UNIQUE KEY uk_track_quality (track_id, quality, codec)
);

-- 歌曲分類表
CREATE TABLE track_genres (
    track_id BIGINT NOT NULL,
    genre VARCHAR(50) NOT NULL,
    PRIMARY KEY (track_id, genre),
    INDEX idx_genre (genre),
    FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE
);

-- 音訊特徵表
CREATE TABLE audio_features (
    track_id BIGINT PRIMARY KEY,
    danceability DECIMAL(3,2),               -- 0.00-1.00
    energy DECIMAL(3,2),                     -- 0.00-1.00
    key INT,                                 -- 0-11 (C, C#, D, ...)
    loudness DECIMAL(5,2),                   -- dB
    mode INT,                                -- 0=小調, 1=大調
    speechiness DECIMAL(3,2),                -- 0.00-1.00
    acousticness DECIMAL(3,2),               -- 0.00-1.00
    instrumentalness DECIMAL(3,2),           -- 0.00-1.00
    liveness DECIMAL(3,2),                   -- 0.00-1.00
    valence DECIMAL(3,2),                    -- 0.00-1.00（正面情緒）
    tempo DECIMAL(6,2),                      -- BPM
    duration_ms INT,
    time_signature INT,                      -- 拍號 (3, 4, 5, 7)
    analysis_version VARCHAR(20),
    analyzed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE
);

-- 播放列表表（分片）
CREATE TABLE playlists (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    owner_id VARCHAR(64) NOT NULL,
    public BOOLEAN DEFAULT TRUE,
    collaborative BOOLEAN DEFAULT FALSE,
    cover_url VARCHAR(1024),
    total_tracks INT DEFAULT 0,
    total_duration INT DEFAULT 0,            -- 毫秒
    follower_count INT DEFAULT 0,
    snapshot_id VARCHAR(64),                 -- 版本標識（用於檢測更新）
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_owner_id (owner_id, created_at DESC),
    INDEX idx_public (public, follower_count DESC),
    FULLTEXT idx_name_desc (name, description)
);

-- 播放列表歌曲表
CREATE TABLE playlist_tracks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    playlist_id BIGINT NOT NULL,
    track_id BIGINT NOT NULL,
    added_by VARCHAR(64) NOT NULL,
    position INT NOT NULL,
    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_playlist_id (playlist_id, position),
    UNIQUE KEY uk_playlist_track (playlist_id, track_id),
    FOREIGN KEY (playlist_id) REFERENCES playlists(id) ON DELETE CASCADE,
    FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE
);

-- 播放列表追蹤表
CREATE TABLE playlist_followers (
    playlist_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    followed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (playlist_id, user_id),
    INDEX idx_user_id (user_id, followed_at DESC),
    FOREIGN KEY (playlist_id) REFERENCES playlists(id) ON DELETE CASCADE
);

-- 訂閱方案表
CREATE TABLE subscription_plans (
    id INT AUTO_INCREMENT PRIMARY KEY,
    plan_type VARCHAR(50) NOT NULL,          -- free, premium, family, student, duo
    name VARCHAR(100) NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    billing_period VARCHAR(20) DEFAULT 'monthly',
    max_quality VARCHAR(20),                 -- low, normal, high
    offline_download BOOLEAN DEFAULT FALSE,
    ad_free BOOLEAN DEFAULT FALSE,
    skip_limit INT,                          -- -1 表示無限制
    features JSON,
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 用戶訂閱表
CREATE TABLE subscriptions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    plan_id INT NOT NULL,
    status ENUM('active', 'cancelled', 'expired', 'trial', 'suspended') DEFAULT 'active',
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    cancelled_at TIMESTAMP,
    trial_end_date TIMESTAMP,
    auto_renew BOOLEAN DEFAULT TRUE,
    payment_method_id VARCHAR(64),
    INDEX idx_user_id (user_id, started_at DESC),
    INDEX idx_status (status),
    INDEX idx_expires_at (expires_at),
    FOREIGN KEY (plan_id) REFERENCES subscription_plans(id)
);

-- 用戶關注表
CREATE TABLE user_follows (
    follower_id VARCHAR(64) NOT NULL,
    following_id VARCHAR(64) NOT NULL,
    followed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (follower_id, following_id),
    INDEX idx_following (following_id)
);

-- 用戶收藏歌曲表
CREATE TABLE user_liked_tracks (
    user_id VARCHAR(64) NOT NULL,
    track_id BIGINT NOT NULL,
    liked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, track_id),
    INDEX idx_track_id (track_id),
    INDEX idx_user_liked_at (user_id, liked_at DESC),
    FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE
);

-- 用戶收藏專輯表
CREATE TABLE user_liked_albums (
    user_id VARCHAR(64) NOT NULL,
    album_id BIGINT NOT NULL,
    liked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, album_id),
    INDEX idx_album_id (album_id),
    INDEX idx_user_liked_at (user_id, liked_at DESC),
    FOREIGN KEY (album_id) REFERENCES albums(id) ON DELETE CASCADE
);

-- 藝人追蹤表
CREATE TABLE artist_followers (
    user_id VARCHAR(64) NOT NULL,
    artist_id BIGINT NOT NULL,
    followed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, artist_id),
    INDEX idx_artist_id (artist_id),
    FOREIGN KEY (artist_id) REFERENCES artists(id) ON DELETE CASCADE
);

-- 下載記錄表
CREATE TABLE downloaded_tracks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    track_id BIGINT NOT NULL,
    device_id VARCHAR(128) NOT NULL,
    quality VARCHAR(20),
    downloaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,
    INDEX idx_user_device (user_id, device_id),
    INDEX idx_expires_at (expires_at),
    UNIQUE KEY uk_user_track_device (user_id, track_id, device_id),
    FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE
);

-- 版權表
CREATE TABLE track_rights (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    track_id BIGINT NOT NULL,
    rights_holder_id VARCHAR(64) NOT NULL,   -- 版權方 ID
    rights_type ENUM('recording', 'composition', 'performance'),
    percentage DECIMAL(5,2) NOT NULL,        -- 分潤比例 (0.00-100.00)
    territory VARCHAR(2),                    -- 地區代碼
    start_date DATE,
    end_date DATE,
    INDEX idx_track_id (track_id),
    INDEX idx_rights_holder (rights_holder_id),
    FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE
);

-- 推薦快取表
CREATE TABLE recommendation_cache (
    user_id VARCHAR(64) NOT NULL,
    recommendation_type VARCHAR(50) NOT NULL, -- discover_weekly, daily_mix_1, release_radar
    track_ids JSON,
    generated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    PRIMARY KEY (user_id, recommendation_type),
    INDEX idx_expires_at (expires_at)
);
```

### 時序資料表（Cassandra）

```cql
-- 播放歷史表（Cassandra）
CREATE TABLE playback_history (
    id UUID PRIMARY KEY,
    user_id TEXT,
    track_id BIGINT,
    played_at TIMESTAMP,
    source TEXT,                             -- playlist, album, artist, search, radio, discover_weekly
    context_id TEXT,                         -- playlist_id 或 album_id
    duration_played INT,                     -- 實際播放毫秒數
    completion_rate DECIMAL,                 -- 完成率 (0.00-1.00)
    skipped BOOLEAN,
    device_type TEXT,                        -- mobile, desktop, tablet, web, speaker
    device_id TEXT,
    country VARCHAR(2),
    INDEX (user_id, played_at),
    INDEX (track_id, played_at),
    INDEX (played_at)
);

-- 用戶活動表（Cassandra）
CREATE TABLE user_activities (
    id UUID PRIMARY KEY,
    user_id TEXT,
    activity_type TEXT,                      -- play, like_track, like_album, create_playlist, follow_playlist, follow_artist, follow_user
    track_id BIGINT,
    album_id BIGINT,
    playlist_id BIGINT,
    artist_id BIGINT,
    target_user_id TEXT,
    created_at TIMESTAMP,
    public BOOLEAN,
    INDEX (user_id, created_at),
    INDEX (created_at)
);

-- 每日版稅統計（Cassandra，每日批次計算）
CREATE TABLE daily_royalty_stats (
    track_id BIGINT,
    date DATE,
    play_count BIGINT,
    unique_listeners BIGINT,
    total_duration_played BIGINT,            -- 毫秒
    PRIMARY KEY (track_id, date),
    INDEX (date)
);
```

## API 文檔

### 1. 音樂播放

#### 1.1 獲取歌曲資訊

```bash
GET /api/v1/tracks/{track_id}
Authorization: Bearer {token}

# 回應
{
  "id": 12345,
  "title": "Shape of You",
  "artist": {
    "id": 678,
    "name": "Ed Sheeran",
    "verified": true
  },
  "album": {
    "id": 910,
    "title": "÷ (Divide)",
    "cover_url": "https://cdn.spotify.com/covers/910.jpg"
  },
  "duration": 233713,  # 毫秒
  "explicit": false,
  "popularity": 95,
  "preview_url": "https://cdn.spotify.com/preview/12345.ogg"
}
```

#### 1.2 獲取播放 URL

```bash
GET /api/v1/tracks/{track_id}/stream?quality=high
Authorization: Bearer {token}

# 回應
{
  "track_id": 12345,
  "quality": "high",
  "bitrate": 320,
  "codec": "ogg",
  "stream_url": "https://cdn.spotify.com/tracks/320/12345.ogg?token=xyz&expires=1705294800",
  "expires_at": "2025-01-15T10:00:00Z"
}
```

#### 1.3 記錄播放

```bash
POST /api/v1/playback/play
Authorization: Bearer {token}
Content-Type: application/json

{
  "track_id": 12345,
  "source": "playlist",
  "context_id": "5678",
  "device_type": "mobile",
  "device_id": "iphone-123"
}

# 回應
{
  "success": true,
  "session_id": "abc-def-123"
}
```

### 2. 播放列表

#### 2.1 建立播放列表

```bash
POST /api/v1/playlists
Authorization: Bearer {token}
Content-Type: application/json

{
  "name": "我的最愛",
  "description": "2025 年最愛的歌曲",
  "public": true
}

# 回應
{
  "id": 98765,
  "name": "我的最愛",
  "description": "2025 年最愛的歌曲",
  "owner_id": "user123",
  "public": true,
  "collaborative": false,
  "total_tracks": 0,
  "follower_count": 0,
  "snapshot_id": "v1"
}
```

#### 2.2 新增歌曲到播放列表

```bash
POST /api/v1/playlists/{playlist_id}/tracks
Authorization: Bearer {token}
Content-Type: application/json

{
  "track_ids": [12345, 67890],
  "position": 0  # 插入位置（可選，預設新增到最後）
}

# 回應
{
  "snapshot_id": "v2"
}
```

#### 2.3 獲取播放列表歌曲

```bash
GET /api/v1/playlists/{playlist_id}/tracks?offset=0&limit=50
Authorization: Bearer {token}

# 回應
{
  "items": [
    {
      "added_at": "2025-01-15T10:00:00Z",
      "added_by": "user123",
      "track": {
        "id": 12345,
        "title": "Shape of You",
        "artist": {...},
        "album": {...},
        "duration": 233713
      }
    }
  ],
  "total": 150,
  "offset": 0,
  "limit": 50
}
```

### 3. 推薦系統

#### 3.1 Discover Weekly

```bash
GET /api/v1/recommendations/discover-weekly
Authorization: Bearer {token}

# 回應
{
  "name": "Discover Weekly",
  "description": "為你精選的 30 首新歌",
  "updated_at": "2025-01-13T00:00:00Z",  # 每週一更新
  "tracks": [
    {
      "id": 11111,
      "title": "New Song",
      "artist": {...},
      "reason": "因為你聽過 Ed Sheeran"
    },
    ...
  ]
}
```

#### 3.2 相似歌曲

```bash
GET /api/v1/tracks/{track_id}/recommendations?limit=20
Authorization: Bearer {token}

# 回應
{
  "seed_track": {
    "id": 12345,
    "title": "Shape of You"
  },
  "tracks": [
    {
      "id": 22222,
      "title": "Similar Song 1",
      "similarity_score": 0.95
    },
    ...
  ]
}
```

### 4. 社交功能

#### 4.1 追蹤用戶

```bash
PUT /api/v1/me/following
Authorization: Bearer {token}
Content-Type: application/json

{
  "type": "user",
  "ids": ["user456"]
}

# 回應
{
  "success": true
}
```

#### 4.2 獲取朋友動態

```bash
GET /api/v1/me/feed?limit=20
Authorization: Bearer {token}

# 回應
{
  "activities": [
    {
      "id": "act-123",
      "user": {
        "id": "user456",
        "name": "John Doe"
      },
      "type": "play",
      "track": {
        "id": 12345,
        "title": "Shape of You"
      },
      "created_at": "2025-01-15T10:30:00Z"
    },
    ...
  ]
}
```

### 5. 搜尋

```bash
GET /api/v1/search?q=shape&type=track,artist,album&limit=20
Authorization: Bearer {token}

# 回應
{
  "tracks": {
    "items": [
      {
        "id": 12345,
        "title": "Shape of You",
        "artist": {...}
      }
    ]
  },
  "artists": {
    "items": [...]
  },
  "albums": {
    "items": [...]
  }
}
```

## 性能指標

### 系統容量

```
用戶規模：4 億月活躍用戶

資料量：
- 歌曲數：7000 萬首
- 專輯數：300 萬張
- 藝人數：800 萬位
- 播放列表：40 億個

QPS：
- 音樂播放：200,000 次/秒
- 搜尋：50,000 次/秒
- 推薦：30,000 次/秒
- 播放列表操作：20,000 次/秒

延遲：
- 音樂載入（CDN）：P50 < 100ms, P99 < 300ms
- 搜尋 API：P50 < 50ms, P99 < 150ms
- 推薦 API：P50 < 100ms, P99 < 300ms

並發播放：
- 尖峰時段：2000 萬並發串流
- 平均時段：500 萬並發串流

儲存：
- 音訊檔案：總計 500 TB
  * 96 kbps：150 TB
  * 160 kbps：200 TB
  * 320 kbps：150 TB
- 封面圖片：50 TB
- 資料庫：20 TB
```

## 成本估算

### 場景：4 億月活躍用戶

```
假設：
- 每人每天聽 2 小時音樂
- 平均音質 160 kbps
- 付費轉換率：25%（1 億付費用戶）

收入：
- 1 億付費用戶 × $9.99/月 = $999M/月

成本：

1. CDN（音訊串流）：
   - 每天播放：4 億 × 2 小時 × 160 kbps = 320 TB/天
   - 每月：9.6 PB
   - CDN 成本：9.6 PB × $0.02/GB = $192,000/月

2. 儲存（S3）：
   - 音訊檔案：500 TB
   - 封面圖片：50 TB
   - 總計：550 TB
   - S3 成本：550 TB × $0.023/GB = $12,650/月

3. 資料庫：
   - MySQL（分片）：$100,000/月
   - Cassandra：$150,000/月
   - Redis：$30,000/月

4. 推薦系統：
   - GPU 叢集：$100,000/月
   - 資料處理：$50,000/月

5. API 伺服器：
   - 300 台 × $500/月 = $150,000/月

6. 頻寬（非 CDN）：
   - API、圖片：$50,000/月

7. 版稅：
   - $999M × 70% = $699.3M/月

總成本：約 $700M/月

毛利率：30%

單用戶技術成本（不含版稅）：$0.0018/月
```

### 成本優化策略

```
1. CDN 優化：

   方案：多層快取
   - 熱門歌曲（播放 > 1000/天）：CDN 快取
   - 普通歌曲：S3 直連
   - 節省：30% CDN 成本

2. 音訊編碼優化：

   方案：動態轉碼
   - 常見歌曲：預先轉碼 3 種音質
   - 冷門歌曲：按需轉碼
   - 節省：20% 儲存成本

3. 推薦系統優化：

   方案：結果快取
   - Discover Weekly 快取 7 天
   - 減少 90% 即時計算
   - 節省：$80,000/月

4. 資料庫優化：

   方案：冷熱分離
   - 熱資料（最近 30 天）：SSD
   - 冷資料：HDD 或 S3 Glacier
   - 節省：40% 資料庫成本

優化後總成本：約 $0.55M/月（技術成本）+ $699.3M/月（版稅）
節省：$90,000/月（14%）
```

## 關鍵設計決策

### Q1: 為什麼選擇 OGG Vorbis 而非 MP3？

```
對比：

MP3：
優勢：✅ 通用性高、✅ 所有裝置支援
劣勢：❌ 音質較差、❌ 檔案較大、❌ 需授權費

AAC：
優勢：✅ 音質好、✅ 檔案小
劣勢：❌ 需授權費

OGG Vorbis：
優勢：✅ 開源（無授權費）、✅ 音質好、✅ 檔案小
劣勢：❌ 部分舊裝置不支援

Spotify 的選擇：OGG Vorbis
原因：
1. 節省授權費（4 億用戶 × 授權費）
2. 音質好（320 kbps 接近無損）
3. 檔案小（節省頻寬和儲存）
4. 現代裝置都支援

結論：對於 Spotify 規模，OGG Vorbis 是最佳選擇。
```

### Q2: 為什麼推薦系統使用三層架構？

```
問題：
- 7000 萬首歌曲
- 需要即時推薦
- 深度學習模型計算慢

單純協同過濾問題：
❌ 新歌曲難推薦（冷啟動）
❌ 小眾音樂被忽略
❌ 推薦範圍有限

三層架構優勢：

第一層：協同過濾（快速）
- 基於用戶行為
- 找到相似用戶和歌曲
- 覆蓋主流音樂

第二層：NLP 分析（補充）
- 分析歌曲描述
- 發現文化關聯
- 解決冷啟動問題

第三層：音訊分析（精準）
- 分析音訊特徵
- 找到真正相似的歌曲
- 推薦小眾音樂

混合權重：
- 協同過濾 50%（主流）
- NLP 30%（文化）
- 音訊分析 20%（精準）

結論：三層混合準確度最高，覆蓋最廣。
```

### Q3: Discover Weekly 如何做到每週準確推薦？

```
Discover Weekly 特點：
- 每週一更新
- 推薦 30 首新歌
- 用戶未聽過
- 準確度 > 80%

實作步驟：

1. 分析用戶品味（過去 4 週）：
   - 最常聽的分類
   - 最常聽的藝人
   - 音訊特徵偏好（Energy、Danceability 等）

2. 候選生成（2000 首）：
   - 相似用戶聽的歌曲（協同過濾）
   - 相似音訊特徵的歌曲
   - 相似藝人的新歌

3. 過濾：
   - 移除已聽過的歌曲
   - 移除不符合音訊偏好的歌曲
   - 移除過於冷門的歌曲（播放 < 100）

4. 排序：
   - 深度學習模型預測點擊率
   - 排序前 30 首

5. 多樣性調整：
   - 避免同一藝人重複
   - 分散不同分類
   - 加入一些「驚喜」歌曲

6. 快取：
   - 週日晚上批次計算
   - 快取 7 天
   - 週一凌晨推送

關鍵：批次預計算 + 快取，避免即時計算壓力。
```

### Q4: 如何處理版權和版稅？

```
挑戰：
- 每次播放都需計算版稅
- 涉及多個版權方
- 不同地區版權不同

版稅計算流程：

1. 即時記錄播放（Kafka）：
   - 用戶 ID
   - 歌曲 ID
   - 播放時長
   - 地區

2. 每日批次聚合（Spark）：
   - 統計每首歌的播放次數
   - 按地區統計
   - 寫入 Cassandra

3. 每月計算版稅：
   - 總收入池 = 月收入 × 70%
   - 每首歌版稅 = (播放次數 / 總播放) × 收入池
   - 按版權表分配給各版權方

4. 自動付款：
   - 生成版稅報表
   - 整合付款系統
   - 寄送郵件通知

版權檢查：
- 上傳歌曲時檢查 ISRC
- 比對版權資料庫
- 只播放有授權的地區

結論：批次處理 + 自動化是關鍵。
```

### Q5: 如何實現多裝置同步？

```
需求：
- 在手機播放
- 切換到電腦繼續
- 播放進度同步

技術方案：

1. 播放狀態（Redis）：
   Key: playback_state:{user_id}
   Value: {
     "track_id": 12345,
     "position": 135000,  # 毫秒
     "playing": true,
     "device_id": "iphone-123",
     "updated_at": "2025-01-15T10:30:00Z"
   }
   TTL: 24 小時

2. 即時更新：
   - 客戶端每 10 秒更新位置到 Redis
   - 暫停/停止時立即更新

3. 裝置切換：
   - 新裝置請求播放狀態
   - 從 Redis 讀取
   - 從上次位置繼續

4. 衝突處理：
   - 多裝置同時播放：取最新時間戳
   - WebSocket 通知其他裝置暫停

5. 批次寫入資料庫：
   - 每 30 秒或停止播放時
   - 寫入 Cassandra（持久化）
   - 減少資料庫壓力

Spotify Connect：
- 控制裝置：手機
- 播放裝置：電腦/音響
- WebSocket 即時通訊
- 低延遲控制（< 100ms）

結論：Redis 快取 + WebSocket 即時通訊。
```

## 常見問題

### Q1: 如何防止帳號共享？

```
問題：
- 多人共用一個帳號
- 損失訂閱收入

技術檢測：

1. 同時播放檢測：
   - 追蹤活躍裝置
   - Family 方案：最多 6 個帳號
   - Premium 方案：1 個帳號

2. 地理位置檢測：
   - 同一帳號在不同城市同時播放
   - 標記為可疑

3. 裝置指紋：
   - 追蹤裝置數量
   - 超過 10 個裝置：異常

商業策略：
- 提供 Family 方案（$14.99/月，6 人）
- 提供 Duo 方案（$12.99/月，2 人）
- 鼓勵合法共享

處理方式：
- 不要太激進（影響用戶體驗）
- 提示「帳號在其他地方使用」
- 引導升級 Family 方案

結論：技術 + 商業策略並行。
```

### Q2: 如何優化搜尋效能？

```
挑戰：
- 7000 萬首歌曲
- 即時搜尋
- 延遲 < 100ms

技術方案：

1. Elasticsearch：
   - 全文搜尋引擎
   - 支援拼音、同義詞
   - 支援模糊搜尋

2. 分片策略：
   - 按語言分片（英文、中文、日文）
   - 加速搜尋

3. 快取熱門搜尋：
   - Redis 快取前 1000 個熱門關鍵字
   - TTL: 1 小時
   - 命中率 > 50%

4. 搜尋建議（Autocomplete）：
   - Trie 樹結構
   - 前綴搜尋
   - 延遲 < 50ms

5. 個人化排序：
   - 根據用戶喜好調整排序
   - 常聽的藝人優先

範例：
搜尋「shape」
1. 檢查 Redis 快取：未命中
2. Elasticsearch 搜尋
3. 個人化排序（用戶常聽 Ed Sheeran → 優先）
4. 返回結果（50ms）
5. 快取到 Redis

結論：Elasticsearch + Redis 快取 + 個人化。
```

### Q3: 音訊特徵如何提取？

```
音訊分析流程：

1. 音訊波形分析：
   - 使用 Librosa（Python）或 Essentia
   - 提取梅爾頻譜（Mel-Spectrogram）
   - 提取 MFCC（Mel-Frequency Cepstral Coefficients）

2. 特徵提取：

   Danceability（適合跳舞）：
   - 分析節奏穩定性
   - 分析低音強度
   - 值：0.0-1.0

   Energy（能量）：
   - 分析響度和動態範圍
   - 快節奏、大聲 = 高能量
   - 值：0.0-1.0

   Valence（正面情緒）：
   - 使用 CNN 模型分析
   - 大調、快節奏 = 正面
   - 小調、慢節奏 = 負面
   - 值：0.0-1.0

   Tempo（節奏）：
   - 使用節拍追蹤演算法
   - 單位：BPM（Beats Per Minute）
   - 範圍：40-200

   Loudness（響度）：
   - 分析平均振幅
   - 單位：dB
   - 範圍：-60 to 0

3. 批次處理：
   - 新歌上架時分析
   - GPU 加速（提速 10 倍）
   - 每首歌約 10-30 秒

4. 儲存到資料庫：
   - audio_features 表
   - 用於推薦系統

工具：
- Librosa：Python 音訊分析庫
- Essentia：C++ 音訊分析庫（更快）
- TensorFlow：CNN 模型訓練

結論：音訊分析是 Spotify 推薦系統的核心競爭力。
```

### Q4: 離線下載如何實現？

```
需求：
- 付費用戶可下載
- 無網路時播放
- 防止盜版

實作：

1. 下載管理：
   - 客戶端選擇歌曲下載
   - 只在 Wi-Fi 下載（預設）
   - 下載加密檔案

2. DRM 保護：
   使用 Widevine（Android）或 FairPlay（iOS）

   流程：
   a. 客戶端請求授權
   b. 伺服器檢查訂閱狀態
   c. 簽發 30 天授權
   d. 客戶端下載加密音訊
   e. 播放時即時解密

3. 授權管理：
   - 每 30 天重新驗證
   - 取消訂閱後無法播放
   - 最多 5 個裝置

4. 儲存管理：
   - 加密儲存在本機
   - 限制：10000 首/裝置
   - 自動清理過期授權

5. 同步：
   - 下載清單同步到雲端
   - 多裝置共享

資料庫記錄：
- downloaded_tracks 表
- 記錄下載時間、過期時間
- 定期清理過期記錄

結論：DRM + 授權管理確保版權。
```

### Q5: 如何監控系統健康？

```
關鍵指標：

1. 業務指標：
   - DAU/MAU
   - 平均收聽時長
   - 訂閱轉換率
   - 付費用戶留存率

2. 技術指標：
   - 播放成功率 > 99.9%
   - 播放啟動延遲 < 200ms
   - API 延遲：P50/P99
   - CDN 快取命中率 > 90%

3. 推薦指標：
   - Discover Weekly 點擊率
   - 推薦歌曲完整播放率
   - 新歌曲發現率

4. 錯誤率：
   - 播放失敗率 < 0.1%
   - API 錯誤率 < 0.5%
   - 下載失敗率 < 1%

5. 成本指標：
   - CDN 頻寬成本
   - 儲存成本
   - 版稅支出

監控工具：
- Prometheus + Grafana（指標）
- ELK Stack（日誌）
- Sentry（錯誤追蹤）

告警：
- 播放失敗率 > 0.5% → P0
- API 延遲 > 500ms → P1
- CDN 成本異常 → P2

A/B 測試：
- 每週 20+ 個實驗
- 測試推薦算法、UI、音質
- 數據驅動決策

結論：全方位監控 + 即時告警。
```

## 延伸閱讀

### 真實案例

- **Spotify Engineering Blog**: [Spotify Labs](https://engineering.atspotify.com/)
- **Discover Weekly**: [How Spotify Discovers Your Weekly Obsessions](https://qz.com/571007/the-magic-that-makes-spotifys-discover-weekly-playlists-so-damn-good)
- **Audio Analysis**: [Spotify Audio Features](https://developer.spotify.com/documentation/web-api/reference/get-audio-features)
- **Recommendations**: [How Spotify's Algorithm Works](https://engineering.atspotify.com/2020/01/16/for-your-ears-only/)

### 技術文檔

- **OGG Vorbis**: [Vorbis.com](https://xiph.org/vorbis/)
- **Librosa**: [Audio Analysis Library](https://librosa.org/)
- **Widevine DRM**: [Google Widevine](https://www.widevine.com/)
- **Elasticsearch**: [Full-Text Search](https://www.elastic.co/elasticsearch/)

### 相關章節

- **21-netflix**: 推薦系統（類似概念）
- **20-youtube**: CDN 和串流（類似技術）
- **05-distributed-cache**: Redis 快取
- **12-distributed-kv-store**: Cassandra 分散式儲存

## 總結

從「簡單播放」到「完整的音樂平台」，我們學到了：

1. **音訊編碼**：OGG Vorbis、多音質、小檔案
2. **播放列表**：建立、編輯、分享、協作
3. **推薦系統**：協同過濾 + NLP + 音訊分析（三層架構）
4. **社交功能**：追蹤、分享、動態消息
5. **離線下載**：DRM 保護、授權管理
6. **版權管理**：版稅計算、自動分潤

**記住：音樂品質、推薦準確度、用戶體驗，三者需要平衡！**

**Spotify 的啟示**：
- 4 億月活躍用戶
- 7000 萬首歌曲
- Discover Weekly 是殺手級功能
- 音訊分析是核心競爭力
- 個人化是關鍵（80% 播放來自推薦）
- 版權管理是營運基礎

**核心理念：Personalized, social, accessible.（個人化、社交化、易取得）**

---

**下一步**：
- 實作音訊特徵提取
- 搭建推薦系統
- 優化搜尋效能
- 探索音樂 AI 的最新技術
