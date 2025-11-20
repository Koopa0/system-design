# Chapter 21: Netflix - 串流影音平台

> 從零開始設計 Netflix：個人化推薦、自適應串流、全球 CDN、付費訂閱的完整實現

## 本章概述

這是一個關於 **Netflix 系統設計**的完整指南，使用**蘇格拉底式教學法**（Socratic Method）。你將跟隨 Emma（產品經理）、David（架構師）、Sarah（後端工程師）、Michael（運維工程師）和 Lisa（數據科學家）一起，從零開始設計一個生產級的串流影音平台。

## 學習目標

- 理解**自適應串流**（ABR - Adaptive Bitrate Streaming）
- 掌握 **個人化推薦系統**（Machine Learning）
- 學習 **全球 CDN 架構**（Open Connect）
- 實踐**觀看歷程追蹤**
- 了解**訂閱管理**和**付費系統**
- 掌握**影片預載**（Prefetching）
- 學習**離線下載**功能
- 理解**A/B 測試**框架
- 掌握**成本優化**策略
- 學習 Netflix 的真實架構

## 角色介紹

- **Emma**：產品經理，負責定義 Netflix 的產品需求
- **David**：資深架構師，擅長設計全球化的串流系統
- **Sarah**：後端工程師，實現核心業務邏輯
- **Michael**：運維工程師，關注系統穩定性和全球部署
- **Lisa**：數據科學家，負責推薦算法和 A/B 測試

---

## Act 1: 影片播放與自適應串流

**場景：產品需求會議**

**Emma**（產品經理）在白板上寫下 Netflix 的核心功能：

```
核心功能：
1. 用戶註冊/登入
2. 瀏覽影片目錄
3. 播放影片（自適應碼率）
4. 個人化推薦
5. 觀看歷程記錄
6. 多裝置同步
```

**Emma**: "我們要做一個串流平台，就像 Netflix。David，如果用戶點擊播放，最簡單的實作方式是什麼？"

**David**（架構師）思考片刻：

**David**: "最簡單的方式是提供一個影片 URL，讓用戶下載播放。但這有幾個問題：網速慢的用戶會卡頓、浪費流量、無法快進。"

### 方案 1：直接下載播放（不推薦）

```go
package main

import (
    "net/http"
)

// SimpleVideoService - 簡單影片服務
type SimpleVideoService struct {
    videoDir string
}

// PlayVideo - 播放影片（直接下載）
func (s *SimpleVideoService) PlayVideo(w http.ResponseWriter, r *http.Request) {
    videoID := r.URL.Query().Get("video_id")

    // 直接返回影片檔案
    videoPath := s.videoDir + "/" + videoID + ".mp4"
    http.ServeFile(w, r, videoPath)
}
```

**Sarah**（後端工程師）提出問題：

**Sarah**: "這個方案有幾個問題：
1. **無法自適應**：網速慢的用戶會卡頓
2. **浪費流量**：手機用戶下載 4K 影片
3. **無法快進**：必須下載到該位置才能播放
4. **無法統計**：不知道用戶看到哪裡"

**David**: "所以我們需要 **HLS**（HTTP Live Streaming）或 **DASH** 協議。Netflix 使用的是自己優化的版本。"

### 方案 2：HLS 自適應串流（推薦）

```go
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

// StreamingService - 串流服務
type StreamingService struct {
    db       *sql.DB
    cdnURL   string
}

// Video - 影片資訊
type Video struct {
    ID          int64     `json:"id"`
    Title       string    `json:"title"`
    Description string    `json:"description"`
    Duration    int       `json:"duration"`      // 秒
    ReleaseYear int       `json:"release_year"`
    Rating      string    `json:"rating"`        // PG, PG-13, R, etc.
    Genres      []string  `json:"genres"`
    ThumbnailURL string   `json:"thumbnail_url"`
    MasterPlaylistURL string `json:"master_playlist_url"`
}

// VideoFormat - 影片格式（不同碼率）
type VideoFormat struct {
    ID         int64  `json:"id"`
    VideoID    int64  `json:"video_id"`
    Resolution string `json:"resolution"`    // 240p, 360p, 480p, 720p, 1080p, 4k
    Bitrate    int    `json:"bitrate"`       // kbps
    Codec      string `json:"codec"`         // h264, h265, vp9, av1
    S3Key      string `json:"s3_key"`
    PlaylistURL string `json:"playlist_url"`
}

// GetVideo - 獲取影片資訊
func (s *StreamingService) GetVideo(ctx context.Context, videoID int64, userID string) (*Video, error) {
    // 檢查用戶是否有觀看權限（訂閱狀態）
    hasAccess, err := s.checkUserAccess(ctx, userID, videoID)
    if err != nil {
        return nil, err
    }
    if !hasAccess {
        return nil, fmt.Errorf("user does not have access to this video")
    }

    // 查詢影片資訊
    var video Video
    query := `
        SELECT id, title, description, duration, release_year, rating, thumbnail_url
        FROM videos
        WHERE id = ? AND status = 'published'
    `
    err = s.db.QueryRowContext(ctx, query, videoID).Scan(
        &video.ID,
        &video.Title,
        &video.Description,
        &video.Duration,
        &video.ReleaseYear,
        &video.Rating,
        &video.ThumbnailURL,
    )
    if err != nil {
        return nil, err
    }

    // 查詢分類
    genreQuery := `SELECT genre FROM video_genres WHERE video_id = ?`
    rows, err := s.db.QueryContext(ctx, genreQuery, videoID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    for rows.Next() {
        var genre string
        rows.Scan(&genre)
        video.Genres = append(video.Genres, genre)
    }

    // 生成 HLS Master Playlist URL
    video.MasterPlaylistURL = fmt.Sprintf("%s/videos/%d/master.m3u8", s.cdnURL, videoID)

    return &video, nil
}

// GetVideoFormats - 獲取所有可用格式
func (s *StreamingService) GetVideoFormats(ctx context.Context, videoID int64) ([]VideoFormat, error) {
    query := `
        SELECT id, video_id, resolution, bitrate, codec, s3_key, playlist_url
        FROM video_formats
        WHERE video_id = ?
        ORDER BY bitrate ASC
    `

    rows, err := s.db.QueryContext(ctx, query, videoID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var formats []VideoFormat
    for rows.Next() {
        var f VideoFormat
        err := rows.Scan(&f.ID, &f.VideoID, &f.Resolution, &f.Bitrate, &f.Codec, &f.S3Key, &f.PlaylistURL)
        if err != nil {
            continue
        }
        formats = append(formats, f)
    }

    return formats, nil
}

// checkUserAccess - 檢查用戶訂閱狀態
func (s *StreamingService) checkUserAccess(ctx context.Context, userID string, videoID int64) (bool, error) {
    var subscriptionStatus string
    var subscriptionExpiry time.Time

    query := `
        SELECT status, expires_at
        FROM subscriptions
        WHERE user_id = ?
        ORDER BY created_at DESC
        LIMIT 1
    `

    err := s.db.QueryRowContext(ctx, query, userID).Scan(&subscriptionStatus, &subscriptionExpiry)
    if err == sql.ErrNoRows {
        return false, nil
    }
    if err != nil {
        return false, err
    }

    // 檢查訂閱是否有效
    if subscriptionStatus != "active" {
        return false, nil
    }
    if time.Now().After(subscriptionExpiry) {
        return false, nil
    }

    return true, nil
}

// StartPlayback - 開始播放（記錄事件）
func (s *StreamingService) StartPlayback(ctx context.Context, userID string, videoID int64, deviceType string) error {
    query := `
        INSERT INTO playback_sessions (user_id, video_id, device_type, started_at)
        VALUES (?, ?, ?, ?)
    `
    _, err := s.db.ExecContext(ctx, query, userID, videoID, deviceType, time.Now())
    if err != nil {
        return err
    }

    // 發送到 Kafka 進行即時分析
    go s.sendPlaybackEvent(userID, videoID, "start")

    return nil
}

func (s *StreamingService) sendPlaybackEvent(userID string, videoID int64, eventType string) {
    // 發送到 Kafka（用於推薦系統、A/B 測試）
}
```

**數據庫設計**：

```sql
-- 影片表
CREATE TABLE videos (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    duration INT NOT NULL,                   -- 秒
    release_year INT,
    rating VARCHAR(10),                      -- PG, PG-13, R, NC-17
    status ENUM('draft', 'processing', 'published', 'archived') DEFAULT 'draft',
    thumbnail_url VARCHAR(1024),
    original_s3_key VARCHAR(512),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMP,
    INDEX idx_status (status),
    INDEX idx_release_year (release_year DESC),
    FULLTEXT idx_title (title)
);

-- 影片格式表（多碼率）
CREATE TABLE video_formats (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    resolution VARCHAR(10),                  -- 240p, 360p, 480p, 720p, 1080p, 4k
    bitrate INT NOT NULL,                    -- kbps
    codec VARCHAR(20),                       -- h264, h265, vp9, av1
    s3_key VARCHAR(512),
    playlist_url VARCHAR(1024),              -- HLS playlist URL
    file_size BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (video_id) REFERENCES videos(id),
    UNIQUE KEY uk_video_format (video_id, resolution, codec)
);

-- 影片分類表
CREATE TABLE video_genres (
    video_id BIGINT NOT NULL,
    genre VARCHAR(50) NOT NULL,
    PRIMARY KEY (video_id, genre),
    INDEX idx_genre (genre)
);

-- 播放會話表
CREATE TABLE playback_sessions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    video_id BIGINT NOT NULL,
    device_type VARCHAR(20),                 -- mobile, tablet, desktop, tv
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ended_at TIMESTAMP,
    watch_duration INT,                      -- 觀看時長（秒）
    current_position INT,                    -- 當前播放位置（秒）
    completion_rate DECIMAL(5,2),           -- 完成率
    quality_switches INT DEFAULT 0,          -- 碼率切換次數
    avg_bitrate INT,                         -- 平均碼率
    buffer_events INT DEFAULT 0,             -- 緩衝次數
    INDEX idx_user_id (user_id, started_at DESC),
    INDEX idx_video_id (video_id, started_at DESC)
);
```

---

## Act 2: 個人化推薦系統

**Emma**: "Netflix 的核心競爭力是個人化推薦。每個用戶看到的首頁都不一樣。Lisa，我們如何做到這一點？"

**Lisa**（數據科學家）興奮地說：

**Lisa**: "Netflix 使用多層推薦系統：
1. **協同過濾**：找到相似用戶
2. **內容推薦**：分析影片特徵
3. **深度學習**：神經網絡預測點擊率
4. **個人化排序**：為每個用戶排序結果"

### Netflix 推薦架構

```
第一層：候選生成（Candidate Generation）
- 協同過濾：1000 個候選
- 內容推薦：1000 個候選
- 熱門影片：500 個候選
- 總計：2500 個候選

第二層：排序（Ranking）
- 深度學習模型預測點擊率
- 排序前 100 個

第三層：重排序（Re-ranking）
- 多樣性（不要全是動作片）
- 新鮮度（推薦新上架）
- 個人化（已訂閱導演優先）
- 最終推薦 20 個
```

### 實作：協同過濾

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "math"
)

// RecommendationService - 推薦服務
type RecommendationService struct {
    db *sql.DB
}

// UserVector - 用戶向量（觀看歷史）
type UserVector map[int64]float64  // video_id -> rating/watch_time

// GetUserWatchHistory - 獲取用戶觀看歷史
func (s *RecommendationService) GetUserWatchHistory(ctx context.Context, userID string, limit int) (UserVector, error) {
    query := `
        SELECT video_id,
               (watch_duration / NULLIF(v.duration, 0)) as completion_score
        FROM playback_sessions ps
        JOIN videos v ON ps.video_id = v.id
        WHERE ps.user_id = ?
          AND watch_duration > 60  -- 至少看了 1 分鐘
        ORDER BY ps.started_at DESC
        LIMIT ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    vector := make(UserVector)
    for rows.Next() {
        var videoID int64
        var score float64
        rows.Scan(&videoID, &score)
        vector[videoID] = score
    }

    return vector, nil
}

// CosineSimilarity - 計算餘弦相似度
func CosineSimilarity(v1, v2 UserVector) float64 {
    var dotProduct, norm1, norm2 float64

    for videoID, score1 := range v1 {
        if score2, exists := v2[videoID]; exists {
            dotProduct += score1 * score2
        }
        norm1 += score1 * score1
    }

    for _, score2 := range v2 {
        norm2 += score2 * score2
    }

    if norm1 == 0 || norm2 == 0 {
        return 0
    }

    return dotProduct / (math.Sqrt(norm1) * math.Sqrt(norm2))
}

// FindSimilarUsers - 找到相似用戶
func (s *RecommendationService) FindSimilarUsers(ctx context.Context, userID string, limit int) ([]string, error) {
    // 獲取當前用戶的觀看歷史
    currentVector, err := s.GetUserWatchHistory(ctx, userID, 100)
    if err != nil {
        return nil, err
    }

    // 獲取其他用戶（至少有 3 個共同觀看的影片）
    query := `
        SELECT DISTINCT ps2.user_id
        FROM playback_sessions ps1
        JOIN playback_sessions ps2 ON ps1.video_id = ps2.video_id
        WHERE ps1.user_id = ?
          AND ps2.user_id != ?
        GROUP BY ps2.user_id
        HAVING COUNT(DISTINCT ps1.video_id) >= 3
        LIMIT 1000
    `

    rows, err := s.db.QueryContext(ctx, query, userID, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    type SimilarUser struct {
        UserID     string
        Similarity float64
    }

    var candidates []SimilarUser

    for rows.Next() {
        var otherUserID string
        rows.Scan(&otherUserID)

        // 計算相似度
        otherVector, _ := s.GetUserWatchHistory(ctx, otherUserID, 100)
        similarity := CosineSimilarity(currentVector, otherVector)

        candidates = append(candidates, SimilarUser{
            UserID:     otherUserID,
            Similarity: similarity,
        })
    }

    // 排序（相似度由高到低）
    // ... 排序邏輯

    var similarUsers []string
    for i := 0; i < limit && i < len(candidates); i++ {
        similarUsers = append(similarUsers, candidates[i].UserID)
    }

    return similarUsers, nil
}

// RecommendByCollaborativeFiltering - 協同過濾推薦
func (s *RecommendationService) RecommendByCollaborativeFiltering(ctx context.Context, userID string, limit int) ([]int64, error) {
    // 1. 找到相似用戶
    similarUsers, err := s.FindSimilarUsers(ctx, userID, 50)
    if err != nil {
        return nil, err
    }

    if len(similarUsers) == 0 {
        return []int64{}, nil
    }

    // 2. 找到相似用戶觀看但當前用戶未觀看的影片
    placeholders := ""
    for i := range similarUsers {
        if i > 0 {
            placeholders += ","
        }
        placeholders += "?"
    }

    query := fmt.Sprintf(`
        SELECT ps.video_id, COUNT(*) as score
        FROM playback_sessions ps
        WHERE ps.user_id IN (%s)
          AND ps.video_id NOT IN (
              SELECT video_id FROM playback_sessions WHERE user_id = ?
          )
          AND ps.watch_duration > 300  -- 至少看了 5 分鐘
        GROUP BY ps.video_id
        ORDER BY score DESC
        LIMIT ?
    `, placeholders)

    args := make([]interface{}, len(similarUsers)+2)
    for i, u := range similarUsers {
        args[i] = u
    }
    args[len(similarUsers)] = userID
    args[len(similarUsers)+1] = limit

    rows, err := s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var videoIDs []int64
    for rows.Next() {
        var videoID int64
        var score int
        rows.Scan(&videoID, &score)
        videoIDs = append(videoIDs, videoID)
    }

    return videoIDs, nil
}
```

### 實作：內容推薦

```go
// RecommendByContent - 基於內容推薦
func (s *RecommendationService) RecommendByContent(ctx context.Context, userID string, limit int) ([]int64, error) {
    // 1. 分析用戶偏好的分類
    query := `
        SELECT vg.genre, COUNT(*) as freq
        FROM playback_sessions ps
        JOIN video_genres vg ON ps.video_id = vg.video_id
        WHERE ps.user_id = ?
          AND ps.watch_duration > 300
        GROUP BY vg.genre
        ORDER BY freq DESC
        LIMIT 5
    `

    rows, err := s.db.QueryContext(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var preferredGenres []string
    for rows.Next() {
        var genre string
        var freq int
        rows.Scan(&genre, &freq)
        preferredGenres = append(preferredGenres, genre)
    }

    if len(preferredGenres) == 0 {
        return []int64{}, nil
    }

    // 2. 推薦這些分類的熱門影片（用戶未觀看）
    placeholders := ""
    for i := range preferredGenres {
        if i > 0 {
            placeholders += ","
        }
        placeholders += "?"
    }

    query = fmt.Sprintf(`
        SELECT v.id, COUNT(DISTINCT vg.genre) as genre_match
        FROM videos v
        JOIN video_genres vg ON v.id = vg.video_id
        WHERE vg.genre IN (%s)
          AND v.status = 'published'
          AND v.id NOT IN (
              SELECT video_id FROM playback_sessions WHERE user_id = ?
          )
        GROUP BY v.id
        ORDER BY genre_match DESC, v.published_at DESC
        LIMIT ?
    `, placeholders)

    args := make([]interface{}, len(preferredGenres)+2)
    for i, g := range preferredGenres {
        args[i] = g
    }
    args[len(preferredGenres)] = userID
    args[len(preferredGenres)+1] = limit

    rows, err = s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var videoIDs []int64
    for rows.Next() {
        var videoID int64
        var genreMatch int
        rows.Scan(&videoID, &genreMatch)
        videoIDs = append(videoIDs, videoID)
    }

    return videoIDs, nil
}

// GetPersonalizedRecommendations - 混合推薦
func (s *RecommendationService) GetPersonalizedRecommendations(ctx context.Context, userID string, limit int) ([]int64, error) {
    // 協同過濾：50%
    cfVideos, _ := s.RecommendByCollaborativeFiltering(ctx, userID, limit/2)

    // 內容推薦：50%
    contentVideos, _ := s.RecommendByContent(ctx, userID, limit/2)

    // 合併去重
    videoSet := make(map[int64]bool)
    var result []int64

    for _, vid := range cfVideos {
        if !videoSet[vid] {
            videoSet[vid] = true
            result = append(result, vid)
        }
    }

    for _, vid := range contentVideos {
        if !videoSet[vid] {
            videoSet[vid] = true
            result = append(result, vid)
        }
    }

    // 限制數量
    if len(result) > limit {
        result = result[:limit]
    }

    return result, nil
}
```

---

## Act 3: 訂閱管理與付費系統

**Emma**: "Netflix 是訂閱制，用戶需要選擇方案（基本、標準、高級），並每月自動扣款。"

### 訂閱方案

```
基本方案：$9.99/月
- 480p 解析度
- 1 個裝置同時觀看

標準方案：$15.49/月
- 1080p 解析度
- 2 個裝置同時觀看

高級方案：$19.99/月
- 4K 解析度
- 4 個裝置同時觀看
```

### 數據庫設計

```sql
-- 訂閱方案表
CREATE TABLE subscription_plans (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(50) NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    max_resolution VARCHAR(10),              -- 480p, 1080p, 4k
    max_concurrent_streams INT,              -- 同時觀看裝置數
    features JSON,                           -- 其他功能
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 用戶訂閱表
CREATE TABLE subscriptions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    plan_id INT NOT NULL,
    status ENUM('active', 'cancelled', 'expired', 'suspended') DEFAULT 'active',
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    auto_renew BOOLEAN DEFAULT TRUE,
    payment_method_id VARCHAR(64),
    INDEX idx_user_id (user_id, started_at DESC),
    INDEX idx_status (status),
    INDEX idx_expires_at (expires_at),
    FOREIGN KEY (plan_id) REFERENCES subscription_plans(id)
);

-- 付款記錄表
CREATE TABLE payments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    subscription_id BIGINT NOT NULL,
    amount DECIMAL(10,2) NOT NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    payment_method VARCHAR(20),              -- credit_card, paypal, apple_pay
    payment_provider_id VARCHAR(255),        -- Stripe payment intent ID
    status ENUM('pending', 'succeeded', 'failed', 'refunded') DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_id (user_id, created_at DESC),
    INDEX idx_status (status),
    FOREIGN KEY (subscription_id) REFERENCES subscriptions(id)
);
```

### 實作：訂閱服務

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    "github.com/stripe/stripe-go/v74"
    "github.com/stripe/stripe-go/v74/paymentintent"
)

// SubscriptionService - 訂閱服務
type SubscriptionService struct {
    db *sql.DB
}

// SubscriptionPlan - 訂閱方案
type SubscriptionPlan struct {
    ID                   int     `json:"id"`
    Name                 string  `json:"name"`
    Price                float64 `json:"price"`
    MaxResolution        string  `json:"max_resolution"`
    MaxConcurrentStreams int     `json:"max_concurrent_streams"`
}

// Subscribe - 用戶訂閱
func (s *SubscriptionService) Subscribe(ctx context.Context, userID string, planID int, paymentMethodID string) error {
    // 1. 檢查是否已有活躍訂閱
    var existingSubID int64
    query := `SELECT id FROM subscriptions WHERE user_id = ? AND status = 'active' LIMIT 1`
    err := s.db.QueryRowContext(ctx, query, userID).Scan(&existingSubID)
    if err != sql.ErrNoRows {
        return fmt.Errorf("user already has an active subscription")
    }

    // 2. 獲取方案價格
    var price float64
    planQuery := `SELECT price FROM subscription_plans WHERE id = ?`
    err = s.db.QueryRowContext(ctx, planQuery, planID).Scan(&price)
    if err != nil {
        return err
    }

    // 3. 使用 Stripe 建立付款
    params := &stripe.PaymentIntentParams{
        Amount:   stripe.Int64(int64(price * 100)), // 轉為分
        Currency: stripe.String(string(stripe.CurrencyUSD)),
        PaymentMethod: stripe.String(paymentMethodID),
        Confirm: stripe.Bool(true),
    }

    pi, err := paymentintent.New(params)
    if err != nil {
        return err
    }

    // 4. 建立訂閱記錄
    now := time.Now()
    expiresAt := now.AddDate(0, 1, 0) // 一個月後

    subQuery := `
        INSERT INTO subscriptions (user_id, plan_id, status, started_at, expires_at, payment_method_id)
        VALUES (?, ?, 'active', ?, ?, ?)
    `
    result, err := s.db.ExecContext(ctx, subQuery, userID, planID, now, expiresAt, paymentMethodID)
    if err != nil {
        return err
    }

    subscriptionID, _ := result.LastInsertId()

    // 5. 記錄付款
    paymentQuery := `
        INSERT INTO payments (user_id, subscription_id, amount, payment_method, payment_provider_id, status)
        VALUES (?, ?, ?, 'credit_card', ?, 'succeeded')
    `
    s.db.ExecContext(ctx, paymentQuery, userID, subscriptionID, price, pi.ID)

    return nil
}

// CheckConcurrentStreams - 檢查並發觀看數
func (s *SubscriptionService) CheckConcurrentStreams(ctx context.Context, userID string) (bool, error) {
    // 1. 獲取用戶訂閱方案
    var maxStreams int
    query := `
        SELECT sp.max_concurrent_streams
        FROM subscriptions sub
        JOIN subscription_plans sp ON sub.plan_id = sp.id
        WHERE sub.user_id = ?
          AND sub.status = 'active'
          AND sub.expires_at > NOW()
        ORDER BY sub.started_at DESC
        LIMIT 1
    `

    err := s.db.QueryRowContext(ctx, query, userID).Scan(&maxStreams)
    if err != nil {
        return false, err
    }

    // 2. 計算當前並發觀看數（過去 5 分鐘內的活躍會話）
    var currentStreams int
    streamQuery := `
        SELECT COUNT(*)
        FROM playback_sessions
        WHERE user_id = ?
          AND started_at > DATE_SUB(NOW(), INTERVAL 5 MINUTE)
          AND (ended_at IS NULL OR ended_at > DATE_SUB(NOW(), INTERVAL 1 MINUTE))
    `

    s.db.QueryRowContext(ctx, streamQuery, userID).Scan(&currentStreams)

    // 3. 檢查是否超過限制
    if currentStreams >= maxStreams {
        return false, nil
    }

    return true, nil
}

// AutoRenewSubscriptions - 自動續訂（定時任務）
func (s *SubscriptionService) AutoRenewSubscriptions(ctx context.Context) error {
    // 查詢即將到期的訂閱（24 小時內）
    query := `
        SELECT id, user_id, plan_id, payment_method_id
        FROM subscriptions
        WHERE status = 'active'
          AND auto_renew = TRUE
          AND expires_at BETWEEN NOW() AND DATE_ADD(NOW(), INTERVAL 24 HOUR)
    `

    rows, err := s.db.QueryContext(ctx, query)
    if err != nil {
        return err
    }
    defer rows.Close()

    for rows.Next() {
        var subID int64
        var userID string
        var planID int
        var paymentMethodID string

        rows.Scan(&subID, &userID, &planID, &paymentMethodID)

        // 嘗試扣款
        err := s.renewSubscription(ctx, subID, userID, planID, paymentMethodID)
        if err != nil {
            // 續訂失敗，發送通知
            fmt.Printf("Failed to renew subscription %d: %v\n", subID, err)
        }
    }

    return nil
}

func (s *SubscriptionService) renewSubscription(ctx context.Context, subID int64, userID string, planID int, paymentMethodID string) error {
    // 獲取方案價格
    var price float64
    s.db.QueryRowContext(ctx, `SELECT price FROM subscription_plans WHERE id = ?`, planID).Scan(&price)

    // Stripe 扣款
    params := &stripe.PaymentIntentParams{
        Amount:   stripe.Int64(int64(price * 100)),
        Currency: stripe.String(string(stripe.CurrencyUSD)),
        PaymentMethod: stripe.String(paymentMethodID),
        Confirm: stripe.Bool(true),
    }

    pi, err := paymentintent.New(params)
    if err != nil {
        // 扣款失敗，標記訂閱為 suspended
        s.db.ExecContext(ctx, `UPDATE subscriptions SET status = 'suspended' WHERE id = ?`, subID)
        return err
    }

    // 更新訂閱到期時間
    s.db.ExecContext(ctx, `
        UPDATE subscriptions
        SET expires_at = DATE_ADD(expires_at, INTERVAL 1 MONTH)
        WHERE id = ?
    `, subID)

    // 記錄付款
    s.db.ExecContext(ctx, `
        INSERT INTO payments (user_id, subscription_id, amount, payment_method, payment_provider_id, status)
        VALUES (?, ?, ?, 'credit_card', ?, 'succeeded')
    `, userID, subID, price, pi.ID)

    return nil
}
```

---

## Act 4: 全球 CDN 架構（Open Connect）

**Michael**: "Netflix 每天提供數十億小時的影片，如何確保全球用戶都能快速觀看？"

**David**: "Netflix 使用 **Open Connect**，這是他們自建的 CDN 網絡。"

### Open Connect 架構

```
傳統 CDN 問題：
❌ 成本高（每 GB $0.05-0.15）
❌ 延遲不穩定
❌ 高峰期頻寬不足

Netflix Open Connect：
✅ 與 ISP 合作，在 ISP 機房部署伺服器
✅ 用戶直接從 ISP 的 Netflix 伺服器獲取影片
✅ 延遲低（< 10ms）
✅ 成本低（一次性硬體成本）

架構：
全球 > 7000 台伺服器
分佈在 > 1000 個 ISP
覆蓋 > 95% 的 Netflix 流量
```

### CDN 選擇邏輯

```go
package main

import (
    "context"
    "net"
    "sort"
)

// CDNService - CDN 服務
type CDNService struct {
    servers []CDNServer
}

// CDNServer - CDN 伺服器
type CDNServer struct {
    ID        string
    Location  string  // 地理位置
    IP        string
    Capacity  int     // 剩餘容量（%）
    Latency   int     // 延遲（ms）
}

// SelectBestCDN - 選擇最佳 CDN 伺服器
func (s *CDNService) SelectBestCDN(ctx context.Context, clientIP string) (*CDNServer, error) {
    // 1. 根據客戶端 IP 獲取地理位置
    clientLocation := s.getLocationFromIP(clientIP)

    // 2. 過濾同一地區的伺服器
    var candidates []CDNServer
    for _, server := range s.servers {
        if server.Location == clientLocation && server.Capacity > 20 {
            candidates = append(candidates, server)
        }
    }

    // 3. 如果同地區沒有可用伺服器，擴展到鄰近地區
    if len(candidates) == 0 {
        nearbyLocations := s.getNearbyLocations(clientLocation)
        for _, server := range s.servers {
            for _, loc := range nearbyLocations {
                if server.Location == loc && server.Capacity > 20 {
                    candidates = append(candidates, server)
                }
            }
        }
    }

    // 4. 按延遲和容量排序
    sort.Slice(candidates, func(i, j int) bool {
        // 優先選擇低延遲
        if candidates[i].Latency != candidates[j].Latency {
            return candidates[i].Latency < candidates[j].Latency
        }
        // 其次選擇高容量
        return candidates[i].Capacity > candidates[j].Capacity
    })

    if len(candidates) == 0 {
        return nil, fmt.Errorf("no available CDN servers")
    }

    return &candidates[0], nil
}

func (s *CDNService) getLocationFromIP(ip string) string {
    // 使用 GeoIP 資料庫查詢
    return "Taiwan-Taipei"
}

func (s *CDNService) getNearbyLocations(location string) []string {
    // 返回鄰近地區
    nearby := map[string][]string{
        "Taiwan-Taipei": {"Taiwan-Taichung", "Taiwan-Kaohsiung", "Japan-Tokyo", "HongKong"},
    }
    return nearby[location]
}
```

---

## Act 5: 觀看歷程同步（多裝置）

**Emma**: "用戶在電視看到一半，切換到手機繼續看，需要從上次的位置繼續播放。"

### 實作：觀看進度同步

```go
package main

import (
    "context"
    "database/sql"
    "time"
)

// WatchProgressService - 觀看進度服務
type WatchProgressService struct {
    db    *sql.DB
    cache *redis.Client
}

// UpdateWatchProgress - 更新觀看進度
func (s *WatchProgressService) UpdateWatchProgress(ctx context.Context, userID string, videoID int64, position int) error {
    // 1. 更新到 Redis（即時）
    cacheKey := fmt.Sprintf("watch_progress:%s:%d", userID, videoID)
    s.cache.Set(ctx, cacheKey, position, 24*time.Hour)

    // 2. 異步寫入資料庫（每 30 秒批次寫入）
    go s.batchUpdateDatabase(userID, videoID, position)

    return nil
}

// GetWatchProgress - 獲取觀看進度
func (s *WatchProgressService) GetWatchProgress(ctx context.Context, userID string, videoID int64) (int, error) {
    // 1. 先查 Redis
    cacheKey := fmt.Sprintf("watch_progress:%s:%d", userID, videoID)
    position, err := s.cache.Get(ctx, cacheKey).Int()
    if err == nil {
        return position, nil
    }

    // 2. Redis 沒有，查資料庫
    query := `
        SELECT current_position
        FROM playback_sessions
        WHERE user_id = ? AND video_id = ?
        ORDER BY started_at DESC
        LIMIT 1
    `

    var pos int
    err = s.db.QueryRowContext(ctx, query, userID, videoID).Scan(&pos)
    if err != nil {
        return 0, err
    }

    return pos, nil
}

func (s *WatchProgressService) batchUpdateDatabase(userID string, videoID int64, position int) {
    // 批次更新邏輯（減少資料庫寫入）
}

// GetContinueWatching - 獲取「繼續觀看」列表
func (s *WatchProgressService) GetContinueWatching(ctx context.Context, userID string, limit int) ([]int64, error) {
    query := `
        SELECT video_id
        FROM playback_sessions
        WHERE user_id = ?
          AND completion_rate < 95  -- 未看完
          AND watch_duration > 60   -- 至少看了 1 分鐘
        ORDER BY started_at DESC
        LIMIT ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var videoIDs []int64
    for rows.Next() {
        var videoID int64
        rows.Scan(&videoID)
        videoIDs = append(videoIDs, videoID)
    }

    return videoIDs, nil
}
```

---

## Act 6: A/B 測試框架

**Lisa**: "Netflix 每天運行數百個 A/B 測試，測試不同的推薦算法、UI 設計、影片縮圖。"

### A/B 測試設計

```sql
CREATE TABLE ab_experiments (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    start_date TIMESTAMP,
    end_date TIMESTAMP,
    status ENUM('draft', 'running', 'completed', 'cancelled') DEFAULT 'draft',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ab_variants (
    id INT AUTO_INCREMENT PRIMARY KEY,
    experiment_id INT NOT NULL,
    name VARCHAR(50),                        -- control, variant_a, variant_b
    traffic_percentage INT,                  -- 流量分配比例
    config JSON,                             -- 變體配置
    FOREIGN KEY (experiment_id) REFERENCES ab_experiments(id)
);

CREATE TABLE ab_assignments (
    user_id VARCHAR(64) NOT NULL,
    experiment_id INT NOT NULL,
    variant_id INT NOT NULL,
    assigned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, experiment_id),
    FOREIGN KEY (experiment_id) REFERENCES ab_experiments(id),
    FOREIGN KEY (variant_id) REFERENCES ab_variants(id)
);

CREATE TABLE ab_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    experiment_id INT NOT NULL,
    variant_id INT NOT NULL,
    event_type VARCHAR(50),                  -- impression, click, play, complete
    video_id BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_experiment (experiment_id, created_at)
);
```

### 實作：A/B 測試

```go
package main

import (
    "context"
    "crypto/md5"
    "database/sql"
    "encoding/binary"
    "encoding/json"
)

// ABTestService - A/B 測試服務
type ABTestService struct {
    db *sql.DB
}

// Variant - A/B 測試變體
type Variant struct {
    ID                int             `json:"id"`
    Name              string          `json:"name"`
    TrafficPercentage int             `json:"traffic_percentage"`
    Config            json.RawMessage `json:"config"`
}

// GetVariant - 獲取用戶的 A/B 測試變體
func (s *ABTestService) GetVariant(ctx context.Context, userID string, experimentID int) (*Variant, error) {
    // 1. 檢查是否已分配
    var variantID int
    query := `SELECT variant_id FROM ab_assignments WHERE user_id = ? AND experiment_id = ?`
    err := s.db.QueryRowContext(ctx, query, userID, experimentID).Scan(&variantID)

    if err == nil {
        // 已分配，返回變體
        return s.getVariantByID(ctx, variantID)
    }

    // 2. 未分配，進行分配
    variant, err := s.assignVariant(ctx, userID, experimentID)
    if err != nil {
        return nil, err
    }

    return variant, nil
}

// assignVariant - 分配變體（基於 hash）
func (s *ABTestService) assignVariant(ctx context.Context, userID string, experimentID int) (*Variant, error) {
    // 1. 獲取所有變體
    query := `SELECT id, name, traffic_percentage, config FROM ab_variants WHERE experiment_id = ?`
    rows, err := s.db.QueryContext(ctx, query, experimentID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var variants []Variant
    for rows.Next() {
        var v Variant
        rows.Scan(&v.ID, &v.Name, &v.TrafficPercentage, &v.Config)
        variants = append(variants, v)
    }

    // 2. 使用一致性 hash 分配
    hash := s.hashUserExperiment(userID, experimentID)
    bucket := hash % 100  // 0-99

    var selectedVariant *Variant
    var cumulative int
    for i := range variants {
        cumulative += variants[i].TrafficPercentage
        if bucket < cumulative {
            selectedVariant = &variants[i]
            break
        }
    }

    if selectedVariant == nil {
        // 預設返回對照組
        selectedVariant = &variants[0]
    }

    // 3. 記錄分配
    s.db.ExecContext(ctx, `
        INSERT INTO ab_assignments (user_id, experiment_id, variant_id)
        VALUES (?, ?, ?)
    `, userID, experimentID, selectedVariant.ID)

    return selectedVariant, nil
}

func (s *ABTestService) hashUserExperiment(userID string, experimentID int) int {
    data := fmt.Sprintf("%s:%d", userID, experimentID)
    hash := md5.Sum([]byte(data))
    return int(binary.BigEndian.Uint32(hash[:4]))
}

// TrackEvent - 追蹤事件
func (s *ABTestService) TrackEvent(ctx context.Context, userID string, experimentID int, eventType string, videoID int64) error {
    // 獲取用戶的變體
    variant, err := s.GetVariant(ctx, userID, experimentID)
    if err != nil {
        return err
    }

    // 記錄事件
    query := `
        INSERT INTO ab_events (user_id, experiment_id, variant_id, event_type, video_id)
        VALUES (?, ?, ?, ?, ?)
    `
    _, err = s.db.ExecContext(ctx, query, userID, experimentID, variant.ID, eventType, videoID)
    return err
}

// GetExperimentResults - 獲取實驗結果
func (s *ABTestService) GetExperimentResults(ctx context.Context, experimentID int) (map[string]interface{}, error) {
    query := `
        SELECT
            v.name,
            COUNT(DISTINCT e.user_id) as unique_users,
            COUNT(CASE WHEN e.event_type = 'play' THEN 1 END) as plays,
            COUNT(CASE WHEN e.event_type = 'complete' THEN 1 END) as completions
        FROM ab_events e
        JOIN ab_variants v ON e.variant_id = v.id
        WHERE e.experiment_id = ?
        GROUP BY v.id, v.name
    `

    rows, err := s.db.QueryContext(ctx, query, experimentID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    results := make(map[string]interface{})
    for rows.Next() {
        var variantName string
        var uniqueUsers, plays, completions int
        rows.Scan(&variantName, &uniqueUsers, &plays, &completions)

        results[variantName] = map[string]interface{}{
            "unique_users": uniqueUsers,
            "plays":        plays,
            "completions":  completions,
            "play_rate":    float64(plays) / float64(uniqueUsers),
            "completion_rate": float64(completions) / float64(plays),
        }
    }

    return results, nil
}
```

---

## Act 7: 影片預載（Prefetching）

**Michael**: "Netflix 會在用戶觀看時，預先下載接下來可能觀看的內容，減少緩衝時間。"

### 預載策略

```
1. 當前影片預載：
   - 預載接下來 30 秒的內容
   - 根據網速調整預載量

2. 下一集預載：
   - 影集看到 90% 時，預載下一集的前 2 分鐘
   - 提升連續觀看體驗

3. 推薦影片預載：
   - 首頁前 3 個推薦影片預載前 30 秒
   - 提高點擊播放的速度
```

---

## Act 8: 成本優化

**Michael**: "Netflix 每月串流 1 億小時影片，成本是最大挑戰。"

### 成本分析

```
場景：2 億月活躍用戶

假設：
- 每人每天觀看 2 小時
- 平均 1080p（5 Mbps）

頻寬：
- 每天：2 億 × 2 小時 × 5 Mbps = 1 EB/天
- 每月：30 EB

CDN 成本（Open Connect）：
- 自建 CDN，一次性硬體成本
- 運營成本：$100M/年（含伺服器、電力、頻寬）

存儲成本：
- 5000 部電影 + 3000 部影集 = 8000 部內容
- 平均每部 × 5 個解析度 × 20GB = 100GB
- 總計：800 TB
- S3 成本：800 TB × $0.023/GB = $18,400/月

轉碼成本：
- 每月新增 100 部內容
- 每部 × 5 個解析度 × $500 = $250,000/月

推薦系統成本：
- GPU 伺服器（深度學習）：$50,000/月
- 資料處理（Spark）：$30,000/月

總成本：約 $9M/月 + $100M/年（CDN）
單用戶成本：約 $0.05/月（不含內容授權）
```

### 優化策略

```
1. 編碼優化：
   - 使用 AV1 編碼（比 H.264 節省 30%）
   - 動態優化（動作片高碼率，談話性節目低碼率）
   → 節省 30% 頻寬

2. 預載優化：
   - 只在 Wi-Fi 時預載
   - 根據用戶行為調整預載策略
   → 節省 20% 頻寬

3. Open Connect：
   - 與更多 ISP 合作
   - 在用戶端 ISP 部署伺服器
   → 覆蓋率提升至 98%

4. 儲存分層：
   - 熱門內容：SSD
   - 普通內容：HDD
   - 冷門內容：S3 Glacier
   → 節省 40% 儲存成本

優化後總成本：約 $6M/月
單用戶成本：$0.03/月
```

---

## 總結

從「簡單播放」到「完整的串流平台」，我們學到了：

1. **自適應串流**：HLS/DASH、多碼率、自動切換
2. **個人化推薦**：協同過濾、內容推薦、深度學習
3. **訂閱管理**：多方案、自動續訂、並發限制
4. **全球 CDN**：Open Connect、ISP 合作、低延遲
5. **觀看同步**：Redis 快取、多裝置同步
6. **A/B 測試**：實驗框架、流量分配、數據分析
7. **成本優化**：AV1 編碼、預載優化、儲存分層

**記住：用戶體驗、個人化、成本效益，三者需要平衡！**

**Netflix 的啟示**：
- 2 億月活躍用戶
- 每天 10 億小時觀看
- 190 個國家和地區
- 個人化推薦是核心競爭力
- Open Connect 降低 95% CDN 成本
- 成本優化永無止境

**核心理念：Personalized, scalable, cost-effective.（個人化、可擴展、成本優化）**
