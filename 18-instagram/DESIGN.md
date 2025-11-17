# Chapter 18: Instagram - 圖片社交平台

> 從零開始設計 Instagram：圖片上傳、動態流、推薦算法的完整實現

## 本章概述

這是一個關於 **Instagram 系統設計**的完整指南，使用**蘇格拉底式教學法**（Socratic Method）。你將跟隨 Emma（產品經理）、David（架構師）、Sarah（後端工程師）、Michael（運維工程師）和 Jennifer（前端工程師）一起，從零開始設計一個生產級的圖片社交平台。

## 學習目標

- 理解**圖片/視頻上傳**和**存儲**的設計
- 掌握 **CDN 加速**和**圖片優化**
- 學習**動態流（News Feed）**的設計
- 實踐**關注/粉絲系統**
- 了解**點贊、評論**的高並發設計
- 掌握**推薦算法**（協同過濾、內容推薦）
- 學習**分庫分表**策略
- 理解**數據一致性**問題
- 掌握**橫向擴展**和**高可用**
- 學習 Instagram 的真實架構

## 角色介紹

- **Emma**：產品經理，負責定義 Instagram 的產品需求
- **David**：資深架構師，擅長設計可擴展的社交系統
- **Sarah**：後端工程師，實現核心業務邏輯
- **Michael**：運維工程師，關注系統穩定性和性能
- **Jennifer**：前端工程師，負責用戶體驗

---

## Act 1: 圖片上傳和存儲

**場景：產品需求會議**

**Emma**（產品經理）在白板上畫出 Instagram 的核心功能：

```
核心功能：
1. 用戶上傳圖片
2. 添加濾鏡和描述
3. 發佈到動態
4. 粉絲可以看到更新
```

**Emma**: "我們先從最基本的開始：用戶上傳一張圖片。David，最簡單的實現是什麼？"

**David**（架構師）思考片刻：

**David**: "最簡單的方式是把圖片存儲在 Web 服務器的本地磁盤，數據庫記錄文件路徑。"

### 方案 1：本地存儲（不推薦）

```go
package main

import (
    "database/sql"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "time"
)

// SimplePhotoService - 簡單圖片服務
type SimplePhotoService struct {
    db          *sql.DB
    uploadDir   string
}

// Photo - 圖片結構
type Photo struct {
    ID          int64
    UserID      string
    FilePath    string
    Caption     string
    CreatedAt   time.Time
}

// UploadPhoto - 上傳圖片
func (s *SimplePhotoService) UploadPhoto(w http.ResponseWriter, r *http.Request) {
    // 1. 解析上傳的文件
    r.ParseMultipartForm(10 << 20) // 10 MB 限制
    file, header, err := r.FormFile("photo")
    if err != nil {
        http.Error(w, "Failed to read file", http.StatusBadRequest)
        return
    }
    defer file.Close()

    // 2. 生成唯一文件名
    filename := fmt.Sprintf("%d_%s", time.Now().Unix(), header.Filename)
    filepath := filepath.Join(s.uploadDir, filename)

    // 3. 保存到本地磁盤
    dst, err := os.Create(filepath)
    if err != nil {
        http.Error(w, "Failed to save file", http.StatusInternalServerError)
        return
    }
    defer dst.Close()

    io.Copy(dst, file)

    // 4. 保存到數據庫
    userID := r.FormValue("user_id")
    caption := r.FormValue("caption")

    query := `INSERT INTO photos (user_id, file_path, caption, created_at) VALUES (?, ?, ?, ?)`
    result, err := s.db.Exec(query, userID, filepath, caption, time.Now())
    if err != nil {
        http.Error(w, "Failed to save to DB", http.StatusInternalServerError)
        return
    }

    photoID, _ := result.LastInsertId()
    w.Write([]byte(fmt.Sprintf(`{"photo_id": %d}`, photoID)))
}

// GetPhoto - 獲取圖片
func (s *SimplePhotoService) GetPhoto(w http.ResponseWriter, r *http.Request) {
    photoID := r.URL.Query().Get("id")

    var filepath string
    query := `SELECT file_path FROM photos WHERE id = ?`
    s.db.QueryRow(query, photoID).Scan(&filepath)

    // 讀取文件並返回
    http.ServeFile(w, r, filepath)
}
```

**Michael**（運維工程師）皺眉：

**Michael**: "這個方案有幾個問題：
1. **單點故障**：服務器磁盤壞了，所有圖片都丟失
2. **擴展性差**：磁盤空間有限，無法水平擴展
3. **無備份**：數據沒有冗餘
4. **帶寬瓶頸**：所有圖片都從一台服務器提供"

**David**: "你說得對。我們需要使用**對象存儲**（Object Storage）。"

### 方案 2：對象存儲（S3）

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3PhotoService - 基於 S3 的圖片服務
type S3PhotoService struct {
    db       *sql.DB
    s3Client *s3.Client
    bucket   string
}

// UploadPhoto - 上傳圖片到 S3
func (s *S3PhotoService) UploadPhoto(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // 1. 解析上傳的文件
    r.ParseMultipartForm(10 << 20)
    file, header, err := r.FormFile("photo")
    if err != nil {
        http.Error(w, "Failed to read file", http.StatusBadRequest)
        return
    }
    defer file.Close()

    // 2. 生成唯一的 S3 key
    userID := r.FormValue("user_id")
    key := fmt.Sprintf("photos/%s/%d_%s", userID, time.Now().Unix(), header.Filename)

    // 3. 上傳到 S3
    _, err = s.s3Client.PutObject(ctx, &s3.PutObjectInput{
        Bucket: aws.String(s.bucket),
        Key:    aws.String(key),
        Body:   file,
        ContentType: aws.String(header.Header.Get("Content-Type")),
    })
    if err != nil {
        http.Error(w, "Failed to upload to S3", http.StatusInternalServerError)
        return
    }

    // 4. 保存到數據庫
    caption := r.FormValue("caption")
    url := fmt.Sprintf("https://%s.s3.amazonaws.com/%s", s.bucket, key)

    query := `INSERT INTO photos (user_id, s3_key, url, caption, created_at) VALUES (?, ?, ?, ?, ?)`
    result, err := s.db.Exec(query, userID, key, url, caption, time.Now())
    if err != nil {
        http.Error(w, "Failed to save to DB", http.StatusInternalServerError)
        return
    }

    photoID, _ := result.LastInsertId()
    w.Write([]byte(fmt.Sprintf(`{"photo_id": %d, "url": "%s"}`, photoID, url)))
}
```

**Sarah**: "S3 解決了存儲問題，但用戶訪問 S3 的延遲可能很高。比如中國用戶訪問美國的 S3。"

**David**: "所以我們需要 **CDN**（內容分發網絡）。"

### 方案 3：S3 + CloudFront CDN

```
用戶上傳：
Client → API Server → S3 (us-east-1)

用戶訪問：
Client → CloudFront (全球邊緣節點) → S3 (如果緩存未命中)

優勢：
✅ 低延遲（就近訪問）
✅ 高可用（多個邊緣節點）
✅ 減輕源站壓力（CDN 緩存）
```

**數據庫設計**：

```sql
CREATE TABLE photos (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    s3_key VARCHAR(512) NOT NULL,           -- S3 對象 key
    cdn_url VARCHAR(1024),                  -- CDN URL
    width INT,                              -- 圖片寬度
    height INT,                             -- 圖片高度
    file_size BIGINT,                       -- 文件大小（bytes）
    caption TEXT,                           -- 描述
    location VARCHAR(255),                  -- 地理位置
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_id (user_id, created_at DESC),
    INDEX idx_created_at (created_at DESC)
);
```

---

## Act 2: 圖片優化和多尺寸生成

**Jennifer**: "同一張圖片，在手機和 PC 上顯示的尺寸不同。我們需要生成多個版本（縮略圖、中等尺寸、原圖）。"

**David**: "這是一個典型的**圖片處理流水線**。"

### 設計：異步圖片處理

```
1. 用戶上傳原圖 → S3 (photos/original/...)
2. 觸發 Lambda 函數（或 Worker）
3. 生成多個尺寸：
   - thumbnail: 150x150
   - medium: 640x640
   - large: 1080x1080
4. 上傳到 S3 (photos/thumbnail/..., photos/medium/..., photos/large/...)
5. 更新數據庫
```

```go
package main

import (
    "bytes"
    "context"
    "database/sql"
    "fmt"
    "image"
    "image/jpeg"

    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/disintegration/imaging"
)

// ImageProcessor - 圖片處理器
type ImageProcessor struct {
    s3Client *s3.Client
    bucket   string
    db       *sql.DB
}

// ProcessPhoto - 處理上傳的圖片
func (p *ImageProcessor) ProcessPhoto(ctx context.Context, photoID int64, originalKey string) error {
    // 1. 從 S3 下載原圖
    output, err := p.s3Client.GetObject(ctx, &s3.GetObjectInput{
        Bucket: &p.bucket,
        Key:    &originalKey,
    })
    if err != nil {
        return err
    }
    defer output.Body.Close()

    // 2. 解碼圖片
    img, _, err := image.Decode(output.Body)
    if err != nil {
        return err
    }

    // 3. 生成多個尺寸
    sizes := map[string]int{
        "thumbnail": 150,
        "medium":    640,
        "large":     1080,
    }

    for sizeType, size := range sizes {
        resized := imaging.Fit(img, size, size, imaging.Lanczos)

        // 編碼為 JPEG
        var buf bytes.Buffer
        jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85})

        // 上傳到 S3
        key := fmt.Sprintf("photos/%s/%d.jpg", sizeType, photoID)
        _, err := p.s3Client.PutObject(ctx, &s3.PutObjectInput{
            Bucket:      &p.bucket,
            Key:         &key,
            Body:        bytes.NewReader(buf.Bytes()),
            ContentType: aws.String("image/jpeg"),
        })
        if err != nil {
            return err
        }

        // 更新數據庫
        url := fmt.Sprintf("https://cdn.example.com/%s", key)
        query := fmt.Sprintf(`UPDATE photos SET %s_url = ? WHERE id = ?`, sizeType)
        p.db.ExecContext(ctx, query, url, photoID)
    }

    return nil
}
```

**更新數據庫表**：

```sql
ALTER TABLE photos
ADD COLUMN thumbnail_url VARCHAR(1024),
ADD COLUMN medium_url VARCHAR(1024),
ADD COLUMN large_url VARCHAR(1024);
```

**Michael**: "如果上傳量很大，圖片處理會很慢。我們需要異步處理。"

### 優化：Kafka + Worker

```
用戶上傳 → API Server → 1. 保存原圖到 S3
                       → 2. 發送消息到 Kafka (photo.uploaded)
                       → 3. 立即返回

Worker 消費 Kafka → 下載原圖 → 生成縮略圖 → 上傳 S3 → 更新數據庫
```

---

## Act 3: 動態流（News Feed）

**Emma**: "現在用戶可以上傳圖片了，接下來要實現動態流：用戶看到他們關注的人的最新動態。"

**David**: "這就是我們在 Chapter 15 學過的 News Feed 設計。"

### 數據庫設計

```sql
-- 關注關係表
CREATE TABLE follow_relationships (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    follower_id VARCHAR(64) NOT NULL,       -- 粉絲 ID
    followee_id VARCHAR(64) NOT NULL,       -- 被關注者 ID
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_follower_followee (follower_id, followee_id),
    INDEX idx_follower (follower_id),
    INDEX idx_followee (followee_id)
);

-- 動態流緩存（Redis Sorted Set）
-- Key: feed:{user_id}
-- Score: timestamp
-- Value: photo_id
```

### 方案 1：Fanout-on-Read（拉模式）

```go
package main

import (
    "context"
    "database/sql"
)

// FanoutOnReadService - 拉模式動態流
type FanoutOnReadService struct {
    db *sql.DB
}

// GetFeed - 獲取用戶的動態流
func (s *FanoutOnReadService) GetFeed(ctx context.Context, userID string, limit int) ([]Photo, error) {
    // 1. 查詢用戶關注的所有人
    query := `SELECT followee_id FROM follow_relationships WHERE follower_id = ?`
    rows, err := s.db.QueryContext(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var followees []string
    for rows.Next() {
        var followeeID string
        rows.Scan(&followeeID)
        followees = append(followees, followeeID)
    }

    // 2. 查詢所有關注者的最新照片（合併排序）
    // 這裡需要 IN 查詢，性能較差
    placeholders := ""
    for i := range followees {
        if i > 0 {
            placeholders += ", "
        }
        placeholders += "?"
    }

    query = fmt.Sprintf(`
        SELECT id, user_id, cdn_url, caption, created_at
        FROM photos
        WHERE user_id IN (%s)
        ORDER BY created_at DESC
        LIMIT ?
    `, placeholders)

    args := make([]interface{}, len(followees)+1)
    for i, f := range followees {
        args[i] = f
    }
    args[len(followees)] = limit

    rows, err = s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var photos []Photo
    for rows.Next() {
        var p Photo
        rows.Scan(&p.ID, &p.UserID, &p.URL, &p.Caption, &p.CreatedAt)
        photos = append(photos, p)
    }

    return photos, nil
}
```

**問題**：
- ❌ 查詢慢（IN 查詢，關注 1000 人 = 掃描 1000 個用戶的照片）
- ❌ 不能緩存（每個用戶的關注列表不同）

### 方案 2：Fanout-on-Write（推模式）

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    "github.com/go-redis/redis/v8"
)

// FanoutOnWriteService - 推模式動態流
type FanoutOnWriteService struct {
    db    *sql.DB
    redis *redis.Client
}

// PublishPhoto - 發佈照片（寫擴散）
func (s *FanoutOnWriteService) PublishPhoto(ctx context.Context, photoID int64, userID string) error {
    // 1. 查詢所有粉絲
    query := `SELECT follower_id FROM follow_relationships WHERE followee_id = ?`
    rows, err := s.db.QueryContext(ctx, query, userID)
    if err != nil {
        return err
    }
    defer rows.Close()

    var followers []string
    for rows.Next() {
        var followerID string
        rows.Scan(&followerID)
        followers = append(followers, followerID)
    }

    // 2. 寫入每個粉絲的動態流（Redis Sorted Set）
    timestamp := float64(time.Now().Unix())
    for _, followerID := range followers {
        key := fmt.Sprintf("feed:%s", followerID)
        s.redis.ZAdd(ctx, key, &redis.Z{
            Score:  timestamp,
            Member: photoID,
        })

        // 只保留最近 1000 條
        s.redis.ZRemRangeByRank(ctx, key, 0, -1001)
    }

    return nil
}

// GetFeed - 獲取動態流（從 Redis 讀取）
func (s *FanoutOnWriteService) GetFeed(ctx context.Context, userID string, limit int) ([]Photo, error) {
    // 1. 從 Redis 獲取 photo IDs
    key := fmt.Sprintf("feed:%s", userID)
    photoIDs, err := s.redis.ZRevRange(ctx, key, 0, int64(limit-1)).Result()
    if err != nil {
        return nil, err
    }

    // 2. 從數據庫批量查詢照片詳情
    if len(photoIDs) == 0 {
        return []Photo{}, nil
    }

    placeholders := ""
    for i := range photoIDs {
        if i > 0 {
            placeholders += ", "
        }
        placeholders += "?"
    }

    query := fmt.Sprintf(`
        SELECT id, user_id, cdn_url, caption, created_at
        FROM photos
        WHERE id IN (%s)
    `, placeholders)

    args := make([]interface{}, len(photoIDs))
    for i, id := range photoIDs {
        args[i] = id
    }

    rows, err := s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var photos []Photo
    for rows.Next() {
        var p Photo
        rows.Scan(&p.ID, &p.UserID, &p.URL, &p.Caption, &p.CreatedAt)
        photos = append(photos, p)
    }

    return photos, nil
}
```

**優勢**：
- ✅ 讀取快（直接從 Redis 讀取）
- ✅ 可預計算（寫入時完成）

**劣勢**：
- ❌ 大 V 發帖慢（1000 萬粉絲 = 寫入 1000 萬次）
- ❌ 存儲成本高

### 方案 3：混合模式（推薦）

```
普通用戶（< 10萬粉絲）：Fanout-on-Write
大 V（> 10萬粉絲）：Fanout-on-Read

用戶獲取動態流時：
1. 從 Redis 讀取預計算的動態（普通用戶發的）
2. 實時查詢大 V 的最新動態
3. 合併排序
```

---

## Act 4: 關注和粉絲系統

**Emma**: "用戶需要能夠關注其他用戶，並查看自己的粉絲列表。"

### API 設計

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "time"
)

// FollowService - 關注服務
type FollowService struct {
    db *sql.DB
}

// Follow - 關注用戶
func (s *FollowService) Follow(ctx context.Context, followerID, followeeID string) error {
    // 防止自己關注自己
    if followerID == followeeID {
        return fmt.Errorf("cannot follow yourself")
    }

    query := `
        INSERT INTO follow_relationships (follower_id, followee_id, created_at)
        VALUES (?, ?, ?)
        ON DUPLICATE KEY UPDATE created_at = created_at
    `
    _, err := s.db.ExecContext(ctx, query, followerID, followeeID, time.Now())
    return err
}

// Unfollow - 取消關注
func (s *FollowService) Unfollow(ctx context.Context, followerID, followeeID string) error {
    query := `DELETE FROM follow_relationships WHERE follower_id = ? AND followee_id = ?`
    _, err := s.db.ExecContext(ctx, query, followerID, followeeID)
    return err
}

// GetFollowers - 獲取粉絲列表
func (s *FollowService) GetFollowers(ctx context.Context, userID string, offset, limit int) ([]string, error) {
    query := `
        SELECT follower_id
        FROM follow_relationships
        WHERE followee_id = ?
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit, offset)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var followers []string
    for rows.Next() {
        var followerID string
        rows.Scan(&followerID)
        followers = append(followers, followerID)
    }

    return followers, nil
}

// GetFollowing - 獲取關注列表
func (s *FollowService) GetFollowing(ctx context.Context, userID string, offset, limit int) ([]string, error) {
    query := `
        SELECT followee_id
        FROM follow_relationships
        WHERE follower_id = ?
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit, offset)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var following []string
    for rows.Next() {
        var followeeID string
        rows.Scan(&followeeID)
        following = append(following, followeeID)
    }

    return following, nil
}

// GetFollowCounts - 獲取關注和粉絲數量
func (s *FollowService) GetFollowCounts(ctx context.Context, userID string) (int, int, error) {
    var followerCount, followingCount int

    // 粉絲數
    query := `SELECT COUNT(*) FROM follow_relationships WHERE followee_id = ?`
    s.db.QueryRowContext(ctx, query, userID).Scan(&followerCount)

    // 關注數
    query = `SELECT COUNT(*) FROM follow_relationships WHERE follower_id = ?`
    s.db.QueryRowContext(ctx, query, userID).Scan(&followingCount)

    return followerCount, followingCount, nil
}
```

### 優化：緩存關注數量

```go
package main

import (
    "context"
    "fmt"
    "strconv"

    "github.com/go-redis/redis/v8"
)

// CachedFollowService - 帶緩存的關注服務
type CachedFollowService struct {
    db    *sql.DB
    redis *redis.Client
}

// Follow - 關注用戶（更新緩存）
func (s *CachedFollowService) Follow(ctx context.Context, followerID, followeeID string) error {
    // 1. 數據庫操作
    query := `INSERT INTO follow_relationships (follower_id, followee_id, created_at) VALUES (?, ?, ?)`
    _, err := s.db.ExecContext(ctx, query, followerID, followeeID, time.Now())
    if err != nil {
        return err
    }

    // 2. 更新 Redis 計數器
    followerKey := fmt.Sprintf("user:%s:followers", followeeID)
    followingKey := fmt.Sprintf("user:%s:following", followerID)

    pipe := s.redis.Pipeline()
    pipe.Incr(ctx, followerKey)
    pipe.Incr(ctx, followingKey)
    pipe.Exec(ctx)

    return nil
}

// GetFollowCounts - 從 Redis 獲取計數（快速）
func (s *CachedFollowService) GetFollowCounts(ctx context.Context, userID string) (int, int, error) {
    followerKey := fmt.Sprintf("user:%s:followers", userID)
    followingKey := fmt.Sprintf("user:%s:following", userID)

    pipe := s.redis.Pipeline()
    followerCmd := pipe.Get(ctx, followerKey)
    followingCmd := pipe.Get(ctx, followingKey)
    pipe.Exec(ctx)

    followerCount, _ := strconv.Atoi(followerCmd.Val())
    followingCount, _ := strconv.Atoi(followingCmd.Val())

    return followerCount, followingCount, nil
}
```

---

## Act 5: 點贊和評論

**Emma**: "用戶需要能夠點贊和評論照片。"

### 數據庫設計

```sql
-- 點贊表
CREATE TABLE likes (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    photo_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_photo_user (photo_id, user_id),
    INDEX idx_photo_id (photo_id),
    INDEX idx_user_id (user_id, created_at DESC)
);

-- 評論表
CREATE TABLE comments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    photo_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_photo_id (photo_id, created_at DESC),
    INDEX idx_user_id (user_id, created_at DESC)
);

-- 照片表添加計數器（冗餘但提高性能）
ALTER TABLE photos
ADD COLUMN like_count INT DEFAULT 0,
ADD COLUMN comment_count INT DEFAULT 0;
```

### 實現：點贊

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    "github.com/go-redis/redis/v8"
)

// LikeService - 點贊服務
type LikeService struct {
    db    *sql.DB
    redis *redis.Client
}

// LikePhoto - 點贊照片
func (s *LikeService) LikePhoto(ctx context.Context, photoID int64, userID string) error {
    // 1. 插入點贊記錄
    query := `
        INSERT INTO likes (photo_id, user_id, created_at)
        VALUES (?, ?, ?)
        ON DUPLICATE KEY UPDATE created_at = created_at
    `
    result, err := s.db.ExecContext(ctx, query, photoID, userID, time.Now())
    if err != nil {
        return err
    }

    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        return nil // 已經點過贊了
    }

    // 2. 增加計數器（數據庫）
    query = `UPDATE photos SET like_count = like_count + 1 WHERE id = ?`
    s.db.ExecContext(ctx, query, photoID)

    // 3. 增加計數器（Redis 緩存）
    key := fmt.Sprintf("photo:%d:likes", photoID)
    s.redis.Incr(ctx, key)

    return nil
}

// UnlikePhoto - 取消點贊
func (s *LikeService) UnlikePhoto(ctx context.Context, photoID int64, userID string) error {
    // 1. 刪除點贊記錄
    query := `DELETE FROM likes WHERE photo_id = ? AND user_id = ?`
    result, err := s.db.ExecContext(ctx, query, photoID, userID)
    if err != nil {
        return err
    }

    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        return nil // 沒有點過贊
    }

    // 2. 減少計數器
    query = `UPDATE photos SET like_count = like_count - 1 WHERE id = ?`
    s.db.ExecContext(ctx, query, photoID)

    key := fmt.Sprintf("photo:%d:likes", photoID)
    s.redis.Decr(ctx, key)

    return nil
}

// GetLikeCount - 獲取點贊數（從 Redis 讀取）
func (s *LikeService) GetLikeCount(ctx context.Context, photoID int64) (int, error) {
    key := fmt.Sprintf("photo:%d:likes", photoID)

    count, err := s.redis.Get(ctx, key).Int()
    if err == redis.Nil {
        // 緩存未命中，從數據庫讀取
        var likeCount int
        query := `SELECT like_count FROM photos WHERE id = ?`
        s.db.QueryRowContext(ctx, query, photoID).Scan(&likeCount)

        // 寫入緩存
        s.redis.Set(ctx, key, likeCount, 0)
        return likeCount, nil
    }

    return count, err
}
```

### 實現：評論

```go
package main

import (
    "context"
    "database/sql"
    "time"
)

// CommentService - 評論服務
type CommentService struct {
    db *sql.DB
}

// Comment - 評論結構
type Comment struct {
    ID        int64
    PhotoID   int64
    UserID    string
    Content   string
    CreatedAt time.Time
}

// AddComment - 添加評論
func (s *CommentService) AddComment(ctx context.Context, photoID int64, userID, content string) (int64, error) {
    // 1. 插入評論
    query := `INSERT INTO comments (photo_id, user_id, content, created_at) VALUES (?, ?, ?, ?)`
    result, err := s.db.ExecContext(ctx, query, photoID, userID, content, time.Now())
    if err != nil {
        return 0, err
    }

    commentID, _ := result.LastInsertId()

    // 2. 增加計數器
    query = `UPDATE photos SET comment_count = comment_count + 1 WHERE id = ?`
    s.db.ExecContext(ctx, query, photoID)

    return commentID, nil
}

// GetComments - 獲取評論列表
func (s *CommentService) GetComments(ctx context.Context, photoID int64, offset, limit int) ([]Comment, error) {
    query := `
        SELECT id, photo_id, user_id, content, created_at
        FROM comments
        WHERE photo_id = ?
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    `

    rows, err := s.db.QueryContext(ctx, query, photoID, limit, offset)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var comments []Comment
    for rows.Next() {
        var c Comment
        rows.Scan(&c.ID, &c.PhotoID, &c.UserID, &c.Content, &c.CreatedAt)
        comments = append(comments, c)
    }

    return comments, nil
}
```

---

## Act 6: 推薦算法

**Emma**: "我們需要推薦系統，讓用戶發現他們可能感興趣的照片和用戶。"

**David**: "推薦算法有幾種類型：協同過濾、內容推薦、混合推薦。"

### 方案 1：協同過濾（Collaborative Filtering）

```
邏輯：
- 如果用戶 A 和用戶 B 都喜歡照片 1、2、3
- 那麼 A 喜歡的照片 4，B 可能也喜歡

實現：
1. 計算用戶相似度（Jaccard 相似度、餘弦相似度）
2. 找到相似用戶
3. 推薦相似用戶喜歡的照片
```

```go
package main

import (
    "context"
    "database/sql"
    "math"
)

// RecommendationService - 推薦服務
type RecommendationService struct {
    db *sql.DB
}

// GetSimilarUsers - 獲取相似用戶（基於共同點贊）
func (s *RecommendationService) GetSimilarUsers(ctx context.Context, userID string, limit int) ([]string, error) {
    // 查找和當前用戶有共同點贊的其他用戶
    query := `
        SELECT l2.user_id, COUNT(*) as common_likes
        FROM likes l1
        JOIN likes l2 ON l1.photo_id = l2.photo_id
        WHERE l1.user_id = ? AND l2.user_id != ?
        GROUP BY l2.user_id
        ORDER BY common_likes DESC
        LIMIT ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, userID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var similarUsers []string
    for rows.Next() {
        var similarUserID string
        var commonLikes int
        rows.Scan(&similarUserID, &commonLikes)
        similarUsers = append(similarUsers, similarUserID)
    }

    return similarUsers, nil
}

// RecommendPhotos - 推薦照片（基於協同過濾）
func (s *RecommendationService) RecommendPhotos(ctx context.Context, userID string, limit int) ([]int64, error) {
    // 1. 找到相似用戶
    similarUsers, err := s.GetSimilarUsers(ctx, userID, 50)
    if err != nil {
        return nil, err
    }

    if len(similarUsers) == 0 {
        return []int64{}, nil
    }

    // 2. 找到相似用戶喜歡但當前用戶未喜歡的照片
    placeholders := ""
    for i := range similarUsers {
        if i > 0 {
            placeholders += ", "
        }
        placeholders += "?"
    }

    query := fmt.Sprintf(`
        SELECT photo_id, COUNT(*) as score
        FROM likes
        WHERE user_id IN (%s)
          AND photo_id NOT IN (
              SELECT photo_id FROM likes WHERE user_id = ?
          )
        GROUP BY photo_id
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

    var photoIDs []int64
    for rows.Next() {
        var photoID int64
        var score int
        rows.Scan(&photoID, &score)
        photoIDs = append(photoIDs, photoID)
    }

    return photoIDs, nil
}
```

### 方案 2：內容推薦（Content-Based）

```
邏輯：
- 分析用戶過去喜歡的照片特徵（標籤、地理位置、拍攝時間）
- 推薦相似特徵的照片

實現：
1. 為照片打標籤（#food, #travel, #sunset）
2. 計算用戶興趣向量
3. 推薦高匹配度的照片
```

```sql
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

```go
// RecommendByTags - 基於標籤推薦
func (s *RecommendationService) RecommendByTags(ctx context.Context, userID string, limit int) ([]int64, error) {
    // 1. 找到用戶喜歡的照片的所有標籤
    query := `
        SELECT pt.tag_id, COUNT(*) as freq
        FROM likes l
        JOIN photo_tags pt ON l.photo_id = pt.photo_id
        WHERE l.user_id = ?
        GROUP BY pt.tag_id
        ORDER BY freq DESC
        LIMIT 10
    `

    rows, err := s.db.QueryContext(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var tagIDs []int64
    for rows.Next() {
        var tagID int64
        var freq int
        rows.Scan(&tagID, &freq)
        tagIDs = append(tagIDs, tagID)
    }

    if len(tagIDs) == 0 {
        return []int64{}, nil
    }

    // 2. 找到包含這些標籤的照片（用戶未點贊的）
    placeholders := ""
    for i := range tagIDs {
        if i > 0 {
            placeholders += ", "
        }
        placeholders += "?"
    }

    query = fmt.Sprintf(`
        SELECT pt.photo_id, COUNT(*) as tag_match
        FROM photo_tags pt
        WHERE pt.tag_id IN (%s)
          AND pt.photo_id NOT IN (
              SELECT photo_id FROM likes WHERE user_id = ?
          )
        GROUP BY pt.photo_id
        ORDER BY tag_match DESC
        LIMIT ?
    `, placeholders)

    args := make([]interface{}, len(tagIDs)+2)
    for i, t := range tagIDs {
        args[i] = t
    }
    args[len(tagIDs)] = userID
    args[len(tagIDs)+1] = limit

    rows, err = s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var photoIDs []int64
    for rows.Next() {
        var photoID int64
        var tagMatch int
        rows.Scan(&photoID, &tagMatch)
        photoIDs = append(photoIDs, photoID)
    }

    return photoIDs, nil
}
```

---

## Act 7: 搜索功能

**Emma**: "用戶需要能夠搜索照片（按標籤、描述、用戶名）。"

**David**: "我們需要全文搜索引擎，比如 Elasticsearch。"

### 設計：Elasticsearch 索引

```json
{
  "mappings": {
    "properties": {
      "photo_id": {"type": "long"},
      "user_id": {"type": "keyword"},
      "username": {"type": "text"},
      "caption": {"type": "text"},
      "tags": {"type": "keyword"},
      "location": {"type": "geo_point"},
      "created_at": {"type": "date"},
      "like_count": {"type": "integer"},
      "comment_count": {"type": "integer"}
    }
  }
}
```

### 實現：搜索服務

```go
package main

import (
    "context"
    "encoding/json"

    "github.com/elastic/go-elasticsearch/v8"
)

// SearchService - 搜索服務
type SearchService struct {
    es *elasticsearch.Client
}

// SearchPhotos - 搜索照片
func (s *SearchService) SearchPhotos(ctx context.Context, keyword string, offset, limit int) ([]int64, error) {
    query := map[string]interface{}{
        "query": map[string]interface{}{
            "multi_match": map[string]interface{}{
                "query":  keyword,
                "fields": []string{"caption", "tags", "username"},
            },
        },
        "from": offset,
        "size": limit,
        "sort": []map[string]interface{}{
            {"created_at": map[string]string{"order": "desc"}},
        },
    }

    queryJSON, _ := json.Marshal(query)
    res, err := s.es.Search(
        s.es.Search.WithContext(ctx),
        s.es.Search.WithIndex("photos"),
        s.es.Search.WithBody(strings.NewReader(string(queryJSON))),
    )
    if err != nil {
        return nil, err
    }
    defer res.Body.Close()

    // 解析結果
    var result map[string]interface{}
    json.NewDecoder(res.Body).Decode(&result)

    hits := result["hits"].(map[string]interface{})["hits"].([]interface{})
    var photoIDs []int64
    for _, hit := range hits {
        source := hit.(map[string]interface{})["_source"].(map[string]interface{})
        photoID := int64(source["photo_id"].(float64))
        photoIDs = append(photoIDs, photoID)
    }

    return photoIDs, nil
}
```

---

## Act 8: 分庫分表

**Michael**: "我們的用戶增長到 1 億了，單個數據庫撐不住了。我們需要分庫分表。"

### 分片策略

```
按 user_id 分片（16 個庫）：

shard_id = hash(user_id) % 16

photos 表分片：
- photos_0, photos_1, ..., photos_15

likes 表分片（按 photo_id）：
- likes_0, likes_1, ..., likes_15

follow_relationships 表分片（按 follower_id）：
- follow_relationships_0, ..., follow_relationships_15
```

### 實現：分片路由

```go
package main

import (
    "database/sql"
    "fmt"
    "hash/fnv"
)

// ShardedDB - 分片數據庫
type ShardedDB struct {
    shards []*sql.DB
}

// GetShard - 獲取分片
func (s *ShardedDB) GetShard(key string) *sql.DB {
    h := fnv.New32a()
    h.Write([]byte(key))
    shardID := int(h.Sum32()) % len(s.shards)
    return s.shards[shardID]
}

// InsertPhoto - 插入照片（分片）
func (s *ShardedDB) InsertPhoto(ctx context.Context, userID string, photo Photo) error {
    db := s.GetShard(userID)

    query := `INSERT INTO photos (user_id, s3_key, cdn_url, caption, created_at) VALUES (?, ?, ?, ?, ?)`
    _, err := db.ExecContext(ctx, query, userID, photo.S3Key, photo.URL, photo.Caption, photo.CreatedAt)
    return err
}

// GetUserPhotos - 獲取用戶的所有照片（單分片查詢）
func (s *ShardedDB) GetUserPhotos(ctx context.Context, userID string, limit int) ([]Photo, error) {
    db := s.GetShard(userID)

    query := `SELECT id, s3_key, cdn_url, caption, created_at FROM photos WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`
    rows, err := db.QueryContext(ctx, query, userID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var photos []Photo
    for rows.Next() {
        var p Photo
        rows.Scan(&p.ID, &p.S3Key, &p.URL, &p.Caption, &p.CreatedAt)
        photos = append(photos, p)
    }

    return photos, nil
}
```

**挑戰**：跨分片查詢
```
問題：獲取全站最熱門的照片（需要查詢所有分片）

解決方案：
1. 使用 Elasticsearch 做全局索引
2. 或者使用匯總表（定期聚合）
```

---

## Act 9: 數據一致性

**Sarah**: "分片後，如何保證數據一致性？比如用戶點贊後，點贊計數立即更新。"

**David**: "這是典型的分布式一致性問題。我們有幾種方案。"

### 方案 1：最終一致性（推薦）

```
1. 用戶點贊 → 寫入 likes 表（立即）
2. 異步更新 photos 表的 like_count（延遲幾秒）
3. 用戶界面顯示樂觀更新（前端立即 +1）

優勢：
✅ 高性能（不阻塞）
✅ 高可用（不依賴事務）

劣勢：
⚠️ 短暫不一致（可接受）
```

```go
// LikePhoto - 點贊（最終一致性）
func (s *LikeService) LikePhoto(ctx context.Context, photoID int64, userID string) error {
    // 1. 插入點贊記錄（立即）
    query := `INSERT INTO likes (photo_id, user_id, created_at) VALUES (?, ?, ?)`
    _, err := s.db.ExecContext(ctx, query, photoID, userID, time.Now())
    if err != nil {
        return err
    }

    // 2. 發送消息到 Kafka（異步更新計數）
    s.kafka.Publish("photo.liked", map[string]interface{}{
        "photo_id": photoID,
        "user_id":  userID,
    })

    return nil
}

// Worker 消費 Kafka 消息
func (w *LikeCountWorker) ProcessLike(photoID int64) {
    query := `UPDATE photos SET like_count = like_count + 1 WHERE id = ?`
    w.db.Exec(query, photoID)
}
```

### 方案 2：強一致性（數據庫事務）

```go
// LikePhoto - 點贊（強一致性）
func (s *LikeService) LikePhoto(ctx context.Context, photoID int64, userID string) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // 1. 插入點贊記錄
    query := `INSERT INTO likes (photo_id, user_id, created_at) VALUES (?, ?, ?)`
    _, err = tx.ExecContext(ctx, query, photoID, userID, time.Now())
    if err != nil {
        return err
    }

    // 2. 更新計數器
    query = `UPDATE photos SET like_count = like_count + 1 WHERE id = ?`
    _, err = tx.ExecContext(ctx, query, photoID)
    if err != nil {
        return err
    }

    return tx.Commit()
}
```

**問題**：
- ❌ 跨分片事務（photos 和 likes 可能在不同分片）
- ❌ 性能差（鎖表）

**Instagram 的選擇**：最終一致性 + 樂觀更新

---

## Act 10: 最終架構和性能優化

**Michael**: "讓我們總結一下最終的架構。"

### 最終架構圖

```
┌─────────────────────────────────────────────────────┐
│                   CDN (CloudFront)                   │
│              (圖片、視頻加速)                         │
└───────────────────┬─────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────────┐
│               Load Balancer (ALB)                    │
└───────────────────┬─────────────────────────────────┘
                    ↓
        ┌───────────┴───────────┐
        ↓                       ↓
┌───────────────┐       ┌───────────────┐
│  API Server 1 │       │  API Server N │
└───────┬───────┘       └───────┬───────┘
        │                       │
        └───────────┬───────────┘
                    ↓
        ┌───────────┴───────────────┐
        ↓           ↓               ↓
    ┌──────┐   ┌──────┐       ┌──────────┐
    │Redis │   │Kafka │       │Elasticsearch│
    │Cache │   │Queue │       │  Search  │
    └──────┘   └──────┘       └──────────┘
        ↓           ↓
┌───────────────────────────────────┐
│  Sharded MySQL (16 shards)        │
│  - photos_0 ~ photos_15           │
│  - likes_0 ~ likes_15             │
│  - follow_0 ~ follow_15           │
└───────────────────────────────────┘
        ↓
┌───────────────────────────────────┐
│  S3 (Object Storage)               │
│  - photos/original/                │
│  - photos/thumbnail/               │
│  - photos/medium/                  │
│  - photos/large/                   │
└───────────────────────────────────┘
```

### 性能優化清單

```
1. 圖片優化：
   ✅ 多尺寸生成（thumbnail/medium/large）
   ✅ WebP 格式（比 JPEG 小 30%）
   ✅ 延遲加載（Lazy Loading）
   ✅ CDN 加速

2. 數據庫優化：
   ✅ 分庫分表（按 user_id 分 16 片）
   ✅ 讀寫分離（主從複製）
   ✅ 索引優化（user_id, created_at）

3. 緩存優化：
   ✅ Redis 緩存熱點數據（動態流、點贊數、關注數）
   ✅ 本地緩存（用戶信息）
   ✅ CDN 緩存（圖片、靜態資源）

4. 異步處理：
   ✅ 圖片處理（Kafka + Worker）
   ✅ 動態流更新（Fanout-on-Write）
   ✅ 通知推送（Chapter 17）

5. 限流和降級：
   ✅ API 限流（每用戶 100 req/min）
   ✅ 熱點數據保護（大 V 發帖限流）
   ✅ 降級策略（推薦服務故障 → 返回熱門照片）
```

### 性能指標

```
系統容量（100 台 API 服務器）：

QPS：
- 上傳照片: 10,000 次/秒
- 查看動態流: 100,000 次/秒
- 點贊: 50,000 次/秒

延遲：
- 上傳照片: P99 < 2s（包含圖片上傳到 S3）
- 查看動態流: P99 < 200ms（Redis 緩存命中）
- 點贊: P99 < 100ms

存儲：
- 1 億用戶，平均每人 100 張照片
- 100 億張照片 × 5 個版本 × 500KB = 25 PB
- S3 成本: 25 PB × $0.023/GB = $575,000/月

帶寬：
- 每天 10 億次圖片查看
- 10 億 × 200KB = 200 TB/天
- CDN 成本: 200 TB × $0.085/GB = $17,000/天 = $510,000/月
```

---

## 總結與回顧

**Emma**: "我們從一個簡單的圖片上傳功能，演進到了一個完整的社交平台。讓我們回顧一下關鍵設計決策。"

### 演進歷程

1. **Act 1**: 本地存儲 → S3 對象存儲 → CDN 加速
2. **Act 2**: 同步圖片處理 → 異步處理（Kafka + Worker）
3. **Act 3**: Fanout-on-Read → Fanout-on-Write → 混合模式
4. **Act 4**: 關注系統 + Redis 緩存計數
5. **Act 5**: 點贊、評論 + 冗餘計數器
6. **Act 6**: 協同過濾 + 內容推薦
7. **Act 7**: Elasticsearch 全文搜索
8. **Act 8**: 分庫分表（16 分片）
9. **Act 9**: 最終一致性 + 樂觀更新
10. **Act 10**: 完整架構 + 性能優化

### 核心設計原則

1. **可擴展性**：分庫分表、CDN、無狀態 API
2. **高性能**：Redis 緩存、異步處理、預計算
3. **高可用**：多區域部署、主從複製、降級策略
4. **最終一致性**：接受短暫不一致換取高性能
5. **用戶體驗**：樂觀更新、延遲加載、智能推薦

### 關鍵技術選型

| 組件 | 技術 | 原因 |
|------|------|------|
| 對象存儲 | AWS S3 | 無限擴展、高可用、低成本 |
| CDN | CloudFront | 全球加速、減輕源站壓力 |
| 數據庫 | MySQL (分片) | 事務支持、成熟穩定 |
| 緩存 | Redis | 高性能、數據結構豐富 |
| 隊列 | Kafka | 高吞吐、持久化、解耦 |
| 搜索 | Elasticsearch | 全文搜索、實時索引 |
| 圖片處理 | Lambda / Worker | 彈性擴展、成本優化 |

### 真實案例：Instagram 的架構

**David**: "Instagram 在被 Facebook 收購時（2012 年），只有 13 個工程師支持 3000 萬用戶。他們的架構非常簡潔。"

```
Instagram 早期架構（2012）：

前端：
- Nginx + HAProxy（負載均衡）
- Django（Python Web 框架）

數據庫：
- PostgreSQL（主從複製）
- Redis（緩存動態流）

存儲：
- AWS S3（圖片存儲）
- CloudFront（CDN）

隊列：
- Gearman（異步任務）

監控：
- Munin + Pingdom

關鍵優化：
1. 動態流預計算（Redis Sorted Set）
2. 照片存儲在 S3（無限擴展）
3. CDN 加速（全球低延遲）
4. PostgreSQL 分片（按用戶 ID）
5. 異步任務處理（圖片處理、通知）

教訓：
- 簡單勝過複雜
- 使用成熟的技術棧
- 專注核心功能
- 提前規劃擴展性
```

### 常見坑

1. **圖片存儲**：不要存在本地磁盤，使用 S3
2. **計數器**：用冗餘字段，不要實時 COUNT(*)
3. **動態流**：預計算（Fanout-on-Write），不要實時拉取
4. **熱點數據**：Redis 緩存，避免數據庫壓力
5. **一致性**：接受最終一致性，不要強求強一致性
6. **分片**：提前規劃，避免後期遷移

---

## 練習題

1. **設計題**：如何實現「查看誰點贊了我的照片」功能？（需要考慮性能）
2. **優化題**：如何優化「獲取用戶動態流」的延遲？（< 100ms）
3. **擴展題**：如何支持視頻上傳？（需要轉碼、多碼率）
4. **故障恢復**：如果 Redis 宕機，動態流服務如何降級？
5. **成本優化**：如何將 CDN 成本降低 50%？（提示：智能壓縮、WebP 格式）

---

## 延伸閱讀

- [Instagram Engineering Blog](https://instagram-engineering.com/)
- [Scaling Instagram Infrastructure](https://www.youtube.com/watch?v=hnpzNAPiC0E)
- [How Instagram Works](https://medium.com/@Pinterest_Engineering/how-instagram-feeds-work-849b7b5b6c0a)
- [Instagram's Architecture at 14 Million Users](https://www.slideshare.net/iammutex/instagram-architecture-14m-users)
- [Facebook Photo Storage](https://engineering.fb.com/2009/04/30/core-infra/needle-in-a-haystack-efficient-storage-of-billions-of-photos/)

**核心理念：簡單、可擴展、用戶體驗至上！**
