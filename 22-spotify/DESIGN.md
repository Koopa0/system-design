# Chapter 22: Spotify - 音樂串流平台

> 從零開始設計 Spotify：音樂播放、個人化推薦、播放列表、社交分享的完整實現

## 本章概述

這是一個關於 **Spotify 系統設計**的完整指南，使用**蘇格拉底式教學法**（Socratic Method）。你將跟隨 Emma（產品經理）、David（架構師）、Sarah（後端工程師）、Michael（運維工程師）和 Lisa（數據科學家）一起，從零開始設計一個生產級的音樂串流平台。

## 學習目標

- 理解**音樂串流**和**音訊編碼**
- 掌握 **播放列表系統**設計
- 學習 **音樂推薦算法**（Discover Weekly）
- 實踐**社交功能**（Follow、分享）
- 了解**離線下載**和**快取策略**
- 掌握**多裝置同步**
- 學習**版權管理**和**藝人分潤**
- 理解**即時歌詞**顯示
- 掌握**Podcast 系統**
- 學習 Spotify 的真實架構

## 角色介紹

- **Emma**：產品經理，負責定義 Spotify 的產品需求
- **David**：資深架構師，擅長設計音樂串流系統
- **Sarah**：後端工程師，實現核心業務邏輯
- **Michael**：運維工程師，關注系統穩定性和成本
- **Lisa**：數據科學家，負責音樂推薦算法

---

## Act 1: 音樂播放與串流

**場景：產品需求會議**

**Emma**（產品經理）在白板上寫下 Spotify 的核心功能：

```
核心功能：
1. 播放音樂（高音質）
2. 搜尋歌曲、專輯、藝人
3. 建立播放列表
4. 個人化推薦
5. 離線下載
6. 多裝置同步
7. 社交分享
```

**Emma**: "我們要做一個音樂串流平台，就像 Spotify。David，音樂播放和影片播放有什麼不同？"

**David**（架構師）思考片刻：

**David**: "主要差異在於：
1. **檔案更小**：一首歌 3-5 分鐘，約 3-10 MB
2. **音質要求**：需要多種音質（96kbps、160kbps、320kbps）
3. **連續播放**：使用者通常連續聽很多首歌
4. **快取友善**：小檔案容易快取在本地"

### 音訊編碼格式

```
常見格式：
1. MP3：通用但檔案較大
2. AAC：音質好，檔案小（Apple Music）
3. OGG Vorbis：開源，音質好（Spotify 使用）
4. Opus：最新標準，效率最高

Spotify 的選擇：OGG Vorbis
- 開源（無授權費）
- 音質好
- 檔案小

音質等級：
- 低音質：96 kbps (~2.2 MB/首)
- 一般音質：160 kbps (~3.7 MB/首)
- 高音質：320 kbps (~7.4 MB/首)
```

### 數據庫設計

```sql
-- 藝人表
CREATE TABLE artists (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    bio TEXT,
    avatar_url VARCHAR(1024),
    verified BOOLEAN DEFAULT FALSE,
    monthly_listeners BIGINT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_name (name),
    INDEX idx_monthly_listeners (monthly_listeners DESC)
);

-- 專輯表
CREATE TABLE albums (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    artist_id BIGINT NOT NULL,
    release_date DATE,
    cover_url VARCHAR(1024),
    album_type ENUM('single', 'album', 'compilation') DEFAULT 'album',
    total_tracks INT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_artist_id (artist_id, release_date DESC),
    INDEX idx_release_date (release_date DESC),
    FOREIGN KEY (artist_id) REFERENCES artists(id)
);

-- 歌曲表
CREATE TABLE tracks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    artist_id BIGINT NOT NULL,
    album_id BIGINT,
    duration INT NOT NULL,                   -- 秒
    track_number INT,
    explicit BOOLEAN DEFAULT FALSE,          -- 是否有不雅內容
    isrc VARCHAR(20),                        -- 國際標準錄音代碼
    popularity INT DEFAULT 0,                -- 0-100
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
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
    bitrate INT,                             -- kbps
    codec VARCHAR(20),                       -- ogg, mp3, aac
    s3_key VARCHAR(512),
    cdn_url VARCHAR(1024),
    file_size BIGINT,
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
```

### 實作：音樂播放服務

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "time"
)

// MusicService - 音樂服務
type MusicService struct {
    db     *sql.DB
    cdnURL string
}

// Track - 歌曲資訊
type Track struct {
    ID          int64    `json:"id"`
    Title       string   `json:"title"`
    ArtistID    int64    `json:"artist_id"`
    ArtistName  string   `json:"artist_name"`
    AlbumID     int64    `json:"album_id"`
    AlbumTitle  string   `json:"album_title"`
    Duration    int      `json:"duration"`
    Explicit    bool     `json:"explicit"`
    Popularity  int      `json:"popularity"`
    CoverURL    string   `json:"cover_url"`
    PreviewURL  string   `json:"preview_url"`  // 30 秒預覽
}

// TrackFile - 歌曲檔案
type TrackFile struct {
    Quality  string `json:"quality"`
    Bitrate  int    `json:"bitrate"`
    URL      string `json:"url"`
    FileSize int64  `json:"file_size"`
}

// GetTrack - 獲取歌曲資訊
func (s *MusicService) GetTrack(ctx context.Context, trackID int64) (*Track, error) {
    query := `
        SELECT
            t.id, t.title, t.duration, t.explicit, t.popularity,
            a.id, a.name,
            al.id, al.title, al.cover_url
        FROM tracks t
        JOIN artists a ON t.artist_id = a.id
        LEFT JOIN albums al ON t.album_id = al.id
        WHERE t.id = ?
    `

    var track Track
    err := s.db.QueryRowContext(ctx, query, trackID).Scan(
        &track.ID,
        &track.Title,
        &track.Duration,
        &track.Explicit,
        &track.Popularity,
        &track.ArtistID,
        &track.ArtistName,
        &track.AlbumID,
        &track.AlbumTitle,
        &track.CoverURL,
    )
    if err != nil {
        return nil, err
    }

    // 生成 30 秒預覽 URL
    track.PreviewURL = fmt.Sprintf("%s/preview/%d.ogg", s.cdnURL, trackID)

    return &track, nil
}

// GetTrackFiles - 獲取歌曲檔案（不同音質）
func (s *MusicService) GetTrackFiles(ctx context.Context, trackID int64, userID string) ([]TrackFile, error) {
    // 檢查用戶訂閱等級（決定可用音質）
    maxQuality, err := s.getUserMaxQuality(ctx, userID)
    if err != nil {
        return nil, err
    }

    query := `
        SELECT quality, bitrate, cdn_url, file_size
        FROM track_files
        WHERE track_id = ?
        ORDER BY bitrate ASC
    `

    rows, err := s.db.QueryContext(ctx, query, trackID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var files []TrackFile
    for rows.Next() {
        var f TrackFile
        rows.Scan(&f.Quality, &f.Bitrate, &f.URL, &f.FileSize)

        // 過濾掉超過用戶訂閱等級的音質
        if s.isQualityAllowed(f.Quality, maxQuality) {
            files = append(files, f)
        }
    }

    return files, nil
}

// StartPlayback - 開始播放（記錄）
func (s *MusicService) StartPlayback(ctx context.Context, userID string, trackID int64, source string) error {
    // 記錄播放事件
    query := `
        INSERT INTO playback_history (user_id, track_id, played_at, source)
        VALUES (?, ?, ?, ?)
    `
    _, err := s.db.ExecContext(ctx, query, userID, trackID, time.Now(), source)
    if err != nil {
        return err
    }

    // 異步更新統計（歌曲熱度、藝人收聽數）
    go s.updatePlaybackStats(trackID)

    // 發送到 Kafka 用於推薦系統
    go s.sendPlaybackEvent(userID, trackID, source)

    return nil
}

func (s *MusicService) getUserMaxQuality(ctx context.Context, userID string) (string, error) {
    // 查詢用戶訂閱方案
    var planType string
    query := `
        SELECT sp.plan_type
        FROM subscriptions sub
        JOIN subscription_plans sp ON sub.plan_id = sp.id
        WHERE sub.user_id = ? AND sub.status = 'active'
        ORDER BY sub.created_at DESC
        LIMIT 1
    `
    err := s.db.QueryRowContext(ctx, query, userID).Scan(&planType)
    if err == sql.ErrNoRows {
        return "low", nil  // 免費用戶只能聽低音質
    }
    if err != nil {
        return "", err
    }

    // 根據方案決定最高音質
    qualityMap := map[string]string{
        "free":    "low",      // 96 kbps
        "premium": "high",     // 320 kbps
        "family":  "high",     // 320 kbps
        "student": "normal",   // 160 kbps
    }

    return qualityMap[planType], nil
}

func (s *MusicService) isQualityAllowed(quality, maxQuality string) bool {
    qualityLevel := map[string]int{
        "low":    1,
        "normal": 2,
        "high":   3,
    }
    return qualityLevel[quality] <= qualityLevel[maxQuality]
}

func (s *MusicService) updatePlaybackStats(trackID int64) {
    // 更新歌曲播放次數
    // 更新藝人月收聽數
}

func (s *MusicService) sendPlaybackEvent(userID string, trackID int64, source string) {
    // 發送到 Kafka
}
```

---

## Act 2: 播放列表系統

**Emma**: "播放列表是 Spotify 的核心功能。使用者可以建立自己的播放列表，也可以追蹤別人的播放列表。"

### 數據庫設計

```sql
-- 播放列表表
CREATE TABLE playlists (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    owner_id VARCHAR(64) NOT NULL,
    public BOOLEAN DEFAULT TRUE,
    collaborative BOOLEAN DEFAULT FALSE,     -- 是否允許多人協作
    cover_url VARCHAR(1024),
    total_tracks INT DEFAULT 0,
    total_duration INT DEFAULT 0,            -- 總時長（秒）
    follower_count INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_owner_id (owner_id, created_at DESC),
    INDEX idx_public (public, follower_count DESC)
);

-- 播放列表歌曲表
CREATE TABLE playlist_tracks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    playlist_id BIGINT NOT NULL,
    track_id BIGINT NOT NULL,
    added_by VARCHAR(64) NOT NULL,           -- 誰加入的
    position INT NOT NULL,                   -- 排序位置
    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_playlist_id (playlist_id, position),
    FOREIGN KEY (playlist_id) REFERENCES playlists(id) ON DELETE CASCADE,
    FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE,
    UNIQUE KEY uk_playlist_track (playlist_id, track_id)
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

-- 播放歷史表（時序資料，使用 Cassandra）
CREATE TABLE playback_history (
    id UUID PRIMARY KEY,
    user_id TEXT,
    track_id BIGINT,
    played_at TIMESTAMP,
    source TEXT,                             -- playlist, album, artist, search, radio
    context_id TEXT,                         -- 播放列表 ID 或專輯 ID
    duration_played INT,                     -- 實際播放秒數
    skipped BOOLEAN,
    INDEX (user_id, played_at),
    INDEX (track_id, played_at)
);
```

### 實作：播放列表服務

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "time"
)

// PlaylistService - 播放列表服務
type PlaylistService struct {
    db *sql.DB
}

// Playlist - 播放列表
type Playlist struct {
    ID            int64     `json:"id"`
    Name          string    `json:"name"`
    Description   string    `json:"description"`
    OwnerID       string    `json:"owner_id"`
    Public        bool      `json:"public"`
    Collaborative bool      `json:"collaborative"`
    CoverURL      string    `json:"cover_url"`
    TotalTracks   int       `json:"total_tracks"`
    TotalDuration int       `json:"total_duration"`
    FollowerCount int       `json:"follower_count"`
    CreatedAt     time.Time `json:"created_at"`
}

// CreatePlaylist - 建立播放列表
func (s *PlaylistService) CreatePlaylist(ctx context.Context, ownerID, name, description string, public bool) (*Playlist, error) {
    query := `
        INSERT INTO playlists (name, description, owner_id, public)
        VALUES (?, ?, ?, ?)
    `
    result, err := s.db.ExecContext(ctx, query, name, description, ownerID, public)
    if err != nil {
        return nil, err
    }

    playlistID, _ := result.LastInsertId()

    return &Playlist{
        ID:          playlistID,
        Name:        name,
        Description: description,
        OwnerID:     ownerID,
        Public:      public,
        CreatedAt:   time.Now(),
    }, nil
}

// AddTrackToPlaylist - 新增歌曲到播放列表
func (s *PlaylistService) AddTrackToPlaylist(ctx context.Context, playlistID, trackID int64, userID string) error {
    // 1. 檢查權限（是否為擁有者或協作者）
    hasPermission, err := s.checkPlaylistPermission(ctx, playlistID, userID)
    if err != nil {
        return err
    }
    if !hasPermission {
        return fmt.Errorf("no permission to modify this playlist")
    }

    // 2. 獲取當前最大位置
    var maxPosition int
    s.db.QueryRowContext(ctx, `
        SELECT COALESCE(MAX(position), 0)
        FROM playlist_tracks
        WHERE playlist_id = ?
    `, playlistID).Scan(&maxPosition)

    // 3. 新增歌曲
    query := `
        INSERT INTO playlist_tracks (playlist_id, track_id, added_by, position)
        VALUES (?, ?, ?, ?)
    `
    _, err = s.db.ExecContext(ctx, query, playlistID, trackID, userID, maxPosition+1)
    if err != nil {
        return err
    }

    // 4. 更新播放列表統計
    go s.updatePlaylistStats(playlistID)

    return nil
}

// RemoveTrackFromPlaylist - 從播放列表移除歌曲
func (s *PlaylistService) RemoveTrackFromPlaylist(ctx context.Context, playlistID, trackID int64, userID string) error {
    hasPermission, err := s.checkPlaylistPermission(ctx, playlistID, userID)
    if err != nil {
        return err
    }
    if !hasPermission {
        return fmt.Errorf("no permission to modify this playlist")
    }

    query := `DELETE FROM playlist_tracks WHERE playlist_id = ? AND track_id = ?`
    _, err = s.db.ExecContext(ctx, query, playlistID, trackID)
    if err != nil {
        return err
    }

    // 重新排序剩餘歌曲
    go s.reorderPlaylistTracks(playlistID)
    go s.updatePlaylistStats(playlistID)

    return nil
}

// ReorderPlaylist - 重新排序播放列表
func (s *PlaylistService) ReorderPlaylist(ctx context.Context, playlistID int64, userID string, trackOrder []int64) error {
    hasPermission, err := s.checkPlaylistPermission(ctx, playlistID, userID)
    if err != nil {
        return err
    }
    if !hasPermission {
        return fmt.Errorf("no permission to modify this playlist")
    }

    // 使用交易更新所有歌曲的位置
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    stmt, err := tx.PrepareContext(ctx, `
        UPDATE playlist_tracks
        SET position = ?
        WHERE playlist_id = ? AND track_id = ?
    `)
    if err != nil {
        return err
    }
    defer stmt.Close()

    for position, trackID := range trackOrder {
        _, err := stmt.ExecContext(ctx, position+1, playlistID, trackID)
        if err != nil {
            return err
        }
    }

    return tx.Commit()
}

// FollowPlaylist - 追蹤播放列表
func (s *PlaylistService) FollowPlaylist(ctx context.Context, playlistID int64, userID string) error {
    query := `INSERT INTO playlist_followers (playlist_id, user_id) VALUES (?, ?)`
    _, err := s.db.ExecContext(ctx, query, playlistID, userID)
    if err != nil {
        return err
    }

    // 更新追蹤數
    s.db.ExecContext(ctx, `
        UPDATE playlists
        SET follower_count = follower_count + 1
        WHERE id = ?
    `, playlistID)

    return nil
}

// GetPlaylistTracks - 獲取播放列表歌曲
func (s *PlaylistService) GetPlaylistTracks(ctx context.Context, playlistID int64, offset, limit int) ([]Track, error) {
    query := `
        SELECT
            t.id, t.title, t.duration, t.explicit,
            a.id, a.name,
            al.id, al.title, al.cover_url,
            pt.added_at
        FROM playlist_tracks pt
        JOIN tracks t ON pt.track_id = t.id
        JOIN artists a ON t.artist_id = a.id
        LEFT JOIN albums al ON t.album_id = al.id
        WHERE pt.playlist_id = ?
        ORDER BY pt.position ASC
        LIMIT ? OFFSET ?
    `

    rows, err := s.db.QueryContext(ctx, query, playlistID, limit, offset)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var tracks []Track
    for rows.Next() {
        var track Track
        var addedAt time.Time
        rows.Scan(
            &track.ID, &track.Title, &track.Duration, &track.Explicit,
            &track.ArtistID, &track.ArtistName,
            &track.AlbumID, &track.AlbumTitle, &track.CoverURL,
            &addedAt,
        )
        tracks = append(tracks, track)
    }

    return tracks, nil
}

func (s *PlaylistService) checkPlaylistPermission(ctx context.Context, playlistID int64, userID string) (bool, error) {
    var ownerID string
    var collaborative bool

    query := `SELECT owner_id, collaborative FROM playlists WHERE id = ?`
    err := s.db.QueryRowContext(ctx, query, playlistID).Scan(&ownerID, &collaborative)
    if err != nil {
        return false, err
    }

    // 擁有者或協作播放列表
    return ownerID == userID || collaborative, nil
}

func (s *PlaylistService) updatePlaylistStats(playlistID int64) {
    // 計算總歌曲數和總時長
    var totalTracks int
    var totalDuration int

    query := `
        SELECT COUNT(*), COALESCE(SUM(t.duration), 0)
        FROM playlist_tracks pt
        JOIN tracks t ON pt.track_id = t.id
        WHERE pt.playlist_id = ?
    `
    s.db.QueryRow(query, playlistID).Scan(&totalTracks, &totalDuration)

    // 更新
    s.db.Exec(`
        UPDATE playlists
        SET total_tracks = ?, total_duration = ?
        WHERE id = ?
    `, totalTracks, totalDuration, playlistID)
}

func (s *PlaylistService) reorderPlaylistTracks(playlistID int64) {
    // 重新排序邏輯
}
```

---

## Act 3: 音樂推薦系統（Discover Weekly）

**Lisa**: "Spotify 最有名的功能是 **Discover Weekly**，每週一推薦 30 首新歌曲，準確度非常高。"

**Emma**: "這是如何做到的？"

**Lisa**: "Spotify 使用三種推薦技術：
1. **協同過濾**（Collaborative Filtering）：分析用戶行為
2. **自然語言處理**（NLP）：分析歌詞、評論、部落格
3. **音訊分析**（Audio Analysis）：分析歌曲的旋律、節奏、音色"

### Spotify 推薦架構

```
第一層：協同過濾
- 矩陣分解（Matrix Factorization）
- 找到相似用戶和歌曲

第二層：NLP 分析
- 爬取網路上的音樂評論、部落格
- 分析歌曲被如何描述
- 建立歌曲的「文化向量」

第三層：音訊分析
- 使用 CNN 分析音訊特徵
- 提取：節奏、調性、能量、舞蹈性
- 找到音訊相似的歌曲

最終：混合推薦
- 協同過濾：50%
- NLP：30%
- 音訊分析：20%
```

### 數據庫設計

```sql
-- 音訊特徵表（從音訊分析提取）
CREATE TABLE audio_features (
    track_id BIGINT PRIMARY KEY,
    danceability DECIMAL(3,2),               -- 0.00-1.00 適合跳舞程度
    energy DECIMAL(3,2),                     -- 0.00-1.00 能量
    key INT,                                 -- 0-11 調性
    loudness DECIMAL(5,2),                   -- dB
    mode INT,                                -- 0=小調, 1=大調
    speechiness DECIMAL(3,2),                -- 0.00-1.00 語音成分
    acousticness DECIMAL(3,2),               -- 0.00-1.00 聲學程度
    instrumentalness DECIMAL(3,2),           -- 0.00-1.00 器樂成分
    liveness DECIMAL(3,2),                   -- 0.00-1.00 現場演出感
    valence DECIMAL(3,2),                    -- 0.00-1.00 正面情緒
    tempo DECIMAL(6,2),                      -- BPM
    duration_ms INT,
    time_signature INT,                      -- 拍號
    FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE
);

-- 用戶音樂品味表（預計算）
CREATE TABLE user_taste_profile (
    user_id VARCHAR(64) PRIMARY KEY,
    favorite_genres JSON,                    -- {"rock": 0.3, "pop": 0.5}
    favorite_artists JSON,                   -- [123, 456, 789]
    audio_preferences JSON,                  -- {"energy": 0.8, "danceability": 0.6}
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

-- 推薦快取表（每週預計算）
CREATE TABLE recommendation_cache (
    user_id VARCHAR(64) NOT NULL,
    recommendation_type VARCHAR(50),         -- discover_weekly, daily_mix, release_radar
    track_ids JSON,                          -- [123, 456, 789, ...]
    generated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,
    PRIMARY KEY (user_id, recommendation_type),
    INDEX idx_expires_at (expires_at)
);
```

### 實作：推薦服務

```go
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "math"
)

// RecommendationService - 推薦服務
type RecommendationService struct {
    db *sql.DB
}

// AudioFeature - 音訊特徵
type AudioFeature struct {
    TrackID          int64   `json:"track_id"`
    Danceability     float64 `json:"danceability"`
    Energy           float64 `json:"energy"`
    Loudness         float64 `json:"loudness"`
    Speechiness      float64 `json:"speechiness"`
    Acousticness     float64 `json:"acousticness"`
    Instrumentalness float64 `json:"instrumentalness"`
    Liveness         float64 `json:"liveness"`
    Valence          float64 `json:"valence"`
    Tempo            float64 `json:"tempo"`
}

// GetSimilarTracksByAudio - 基於音訊特徵找相似歌曲
func (s *RecommendationService) GetSimilarTracksByAudio(ctx context.Context, trackID int64, limit int) ([]int64, error) {
    // 1. 獲取目標歌曲的音訊特徵
    var targetFeature AudioFeature
    query := `
        SELECT track_id, danceability, energy, loudness, speechiness,
               acousticness, instrumentalness, liveness, valence, tempo
        FROM audio_features
        WHERE track_id = ?
    `
    err := s.db.QueryRowContext(ctx, query, trackID).Scan(
        &targetFeature.TrackID,
        &targetFeature.Danceability,
        &targetFeature.Energy,
        &targetFeature.Loudness,
        &targetFeature.Speechiness,
        &targetFeature.Acousticness,
        &targetFeature.Instrumentalness,
        &targetFeature.Liveness,
        &targetFeature.Valence,
        &targetFeature.Tempo,
    )
    if err != nil {
        return nil, err
    }

    // 2. 獲取候選歌曲（同類型）
    genreQuery := `
        SELECT DISTINCT t.id
        FROM tracks t
        JOIN track_genres tg1 ON t.id = tg1.track_id
        WHERE tg1.genre IN (
            SELECT genre FROM track_genres WHERE track_id = ?
        )
        AND t.id != ?
        LIMIT 1000
    `
    rows, err := s.db.QueryContext(ctx, genreQuery, trackID, trackID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var candidateIDs []int64
    for rows.Next() {
        var id int64
        rows.Scan(&id)
        candidateIDs = append(candidateIDs, id)
    }

    // 3. 計算每個候選歌曲的相似度
    type SimilarTrack struct {
        TrackID    int64
        Similarity float64
    }

    var similarities []SimilarTrack

    for _, candidateID := range candidateIDs {
        var candidate AudioFeature
        query := `
            SELECT track_id, danceability, energy, loudness, speechiness,
                   acousticness, instrumentalness, liveness, valence, tempo
            FROM audio_features
            WHERE track_id = ?
        `
        err := s.db.QueryRowContext(ctx, query, candidateID).Scan(
            &candidate.TrackID,
            &candidate.Danceability,
            &candidate.Energy,
            &candidate.Loudness,
            &candidate.Speechiness,
            &candidate.Acousticness,
            &candidate.Instrumentalness,
            &candidate.Liveness,
            &candidate.Valence,
            &candidate.Tempo,
        )
        if err != nil {
            continue
        }

        // 計算歐式距離
        similarity := s.calculateAudioSimilarity(targetFeature, candidate)
        similarities = append(similarities, SimilarTrack{
            TrackID:    candidateID,
            Similarity: similarity,
        })
    }

    // 4. 排序（相似度由高到低）
    // ... 排序邏輯

    var result []int64
    for i := 0; i < limit && i < len(similarities); i++ {
        result = append(result, similarities[i].TrackID)
    }

    return result, nil
}

// calculateAudioSimilarity - 計算音訊相似度（歐式距離）
func (s *RecommendationService) calculateAudioSimilarity(a, b AudioFeature) float64 {
    // 正規化後計算歐式距離
    distance := math.Sqrt(
        math.Pow(a.Danceability-b.Danceability, 2) +
            math.Pow(a.Energy-b.Energy, 2) +
            math.Pow((a.Loudness+60)/60-(b.Loudness+60)/60, 2) +  // Loudness 範圍約 -60 to 0
            math.Pow(a.Speechiness-b.Speechiness, 2) +
            math.Pow(a.Acousticness-b.Acousticness, 2) +
            math.Pow(a.Instrumentalness-b.Instrumentalness, 2) +
            math.Pow(a.Liveness-b.Liveness, 2) +
            math.Pow(a.Valence-b.Valence, 2) +
            math.Pow((a.Tempo-b.Tempo)/200, 2),  // Tempo 正規化
    )

    // 轉換為相似度（距離越小，相似度越高）
    similarity := 1.0 / (1.0 + distance)
    return similarity
}

// GenerateDiscoverWeekly - 生成 Discover Weekly 播放列表
func (s *RecommendationService) GenerateDiscoverWeekly(ctx context.Context, userID string) ([]int64, error) {
    // 1. 分析用戶最近聽的歌曲（過去 4 週）
    recentQuery := `
        SELECT DISTINCT track_id
        FROM playback_history
        WHERE user_id = ?
          AND played_at > DATE_SUB(NOW(), INTERVAL 4 WEEK)
          AND duration_played > 30  -- 至少聽了 30 秒
        ORDER BY played_at DESC
        LIMIT 50
    `

    rows, err := s.db.QueryContext(ctx, recentQuery, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var recentTracks []int64
    for rows.Next() {
        var trackID int64
        rows.Scan(&trackID)
        recentTracks = append(recentTracks, trackID)
    }

    // 2. 為每首最近聽的歌找相似歌曲
    candidateMap := make(map[int64]float64)  // track_id -> score

    for _, trackID := range recentTracks {
        similarTracks, _ := s.GetSimilarTracksByAudio(ctx, trackID, 10)
        for i, similarID := range similarTracks {
            // 權重遞減（第一首權重最高）
            score := float64(10-i) / 10.0
            candidateMap[similarID] += score
        }
    }

    // 3. 過濾掉已經聽過的歌曲
    listenedQuery := `
        SELECT DISTINCT track_id
        FROM playback_history
        WHERE user_id = ?
    `
    rows, err = s.db.QueryContext(ctx, listenedQuery, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    listenedSet := make(map[int64]bool)
    for rows.Next() {
        var trackID int64
        rows.Scan(&trackID)
        listenedSet[trackID] = true
    }

    // 4. 排序並選出前 30 首
    type ScoredTrack struct {
        TrackID int64
        Score   float64
    }

    var scored []ScoredTrack
    for trackID, score := range candidateMap {
        if !listenedSet[trackID] {
            scored = append(scored, ScoredTrack{trackID, score})
        }
    }

    // 排序...

    var result []int64
    for i := 0; i < 30 && i < len(scored); i++ {
        result = append(result, scored[i].TrackID)
    }

    // 5. 快取結果（一週）
    s.cacheRecommendations(userID, "discover_weekly", result, 7*24*time.Hour)

    return result, nil
}

func (s *RecommendationService) cacheRecommendations(userID, recType string, trackIDs []int64, ttl time.Duration) {
    jsonData, _ := json.Marshal(trackIDs)
    expiresAt := time.Now().Add(ttl)

    query := `
        INSERT INTO recommendation_cache (user_id, recommendation_type, track_ids, expires_at)
        VALUES (?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE track_ids = ?, generated_at = NOW(), expires_at = ?
    `
    s.db.Exec(query, userID, recType, jsonData, expiresAt, jsonData, expiresAt)
}
```

---

## Act 4: 社交功能

**Emma**: "用戶希望看到朋友在聽什麼，並分享自己喜歡的歌曲。"

### 數據庫設計

```sql
-- 用戶關注表
CREATE TABLE user_follows (
    follower_id VARCHAR(64) NOT NULL,
    following_id VARCHAR(64) NOT NULL,
    followed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (follower_id, following_id),
    INDEX idx_following (following_id)
);

-- 用戶活動動態（時序資料，使用 Cassandra）
CREATE TABLE user_activities (
    id UUID PRIMARY KEY,
    user_id TEXT,
    activity_type TEXT,                      -- play, like, playlist_create, playlist_follow
    track_id BIGINT,
    playlist_id BIGINT,
    created_at TIMESTAMP,
    public BOOLEAN,
    INDEX (user_id, created_at),
    INDEX (created_at)
);

-- 用戶收藏表
CREATE TABLE user_liked_tracks (
    user_id VARCHAR(64) NOT NULL,
    track_id BIGINT NOT NULL,
    liked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, track_id),
    INDEX idx_track_id (track_id),
    INDEX idx_user_liked_at (user_id, liked_at DESC)
);
```

### 實作：社交服務

```go
package main

import (
    "context"
    "database/sql"
    "time"
)

// SocialService - 社交服務
type SocialService struct {
    db *sql.DB
}

// Activity - 用戶活動
type Activity struct {
    ID           string    `json:"id"`
    UserID       string    `json:"user_id"`
    Username     string    `json:"username"`
    ActivityType string    `json:"activity_type"`
    TrackID      int64     `json:"track_id,omitempty"`
    TrackTitle   string    `json:"track_title,omitempty"`
    PlaylistID   int64     `json:"playlist_id,omitempty"`
    PlaylistName string    `json:"playlist_name,omitempty"`
    CreatedAt    time.Time `json:"created_at"`
}

// FollowUser - 追蹤用戶
func (s *SocialService) FollowUser(ctx context.Context, followerID, followingID string) error {
    query := `INSERT INTO user_follows (follower_id, following_id) VALUES (?, ?)`
    _, err := s.db.ExecContext(ctx, query, followerID, followingID)
    return err
}

// UnfollowUser - 取消追蹤
func (s *SocialService) UnfollowUser(ctx context.Context, followerID, followingID string) error {
    query := `DELETE FROM user_follows WHERE follower_id = ? AND following_id = ?`
    _, err := s.db.ExecContext(ctx, query, followerID, followingID)
    return err
}

// GetFriendsFeed - 獲取朋友動態
func (s *SocialService) GetFriendsFeed(ctx context.Context, userID string, limit int) ([]Activity, error) {
    // 1. 獲取追蹤的用戶 ID
    followingQuery := `
        SELECT following_id
        FROM user_follows
        WHERE follower_id = ?
    `
    rows, err := s.db.QueryContext(ctx, followingQuery, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var followingIDs []string
    for rows.Next() {
        var id string
        rows.Scan(&id)
        followingIDs = append(followingIDs, id)
    }

    if len(followingIDs) == 0 {
        return []Activity{}, nil
    }

    // 2. 查詢朋友的公開活動（從 Cassandra）
    // 這裡簡化為 MySQL 示例
    // 實際應該從 Cassandra 的 user_activities 表查詢

    return []Activity{}, nil
}

// LikeTrack - 收藏歌曲
func (s *SocialService) LikeTrack(ctx context.Context, userID string, trackID int64) error {
    // 1. 新增到收藏
    query := `INSERT INTO user_liked_tracks (user_id, track_id) VALUES (?, ?)`
    _, err := s.db.ExecContext(ctx, query, userID, trackID)
    if err != nil {
        return err
    }

    // 2. 記錄活動
    go s.recordActivity(userID, "like", trackID, 0, true)

    return nil
}

// GetLikedTracks - 獲取收藏的歌曲
func (s *SocialService) GetLikedTracks(ctx context.Context, userID string, offset, limit int) ([]Track, error) {
    query := `
        SELECT
            t.id, t.title, t.duration,
            a.name,
            al.title, al.cover_url,
            ult.liked_at
        FROM user_liked_tracks ult
        JOIN tracks t ON ult.track_id = t.id
        JOIN artists a ON t.artist_id = a.id
        LEFT JOIN albums al ON t.album_id = al.id
        WHERE ult.user_id = ?
        ORDER BY ult.liked_at DESC
        LIMIT ? OFFSET ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit, offset)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var tracks []Track
    for rows.Next() {
        var track Track
        var likedAt time.Time
        rows.Scan(
            &track.ID, &track.Title, &track.Duration,
            &track.ArtistName,
            &track.AlbumTitle, &track.CoverURL,
            &likedAt,
        )
        tracks = append(tracks, track)
    }

    return tracks, nil
}

func (s *SocialService) recordActivity(userID, activityType string, trackID, playlistID int64, public bool) {
    // 記錄到 Cassandra user_activities 表
}
```

---

## Act 5: 離線下載與快取

**Michael**: "行動用戶希望在沒有網路時也能聽音樂，我們需要支援離線下載。"

### 實作：下載管理

```go
package main

import (
    "context"
    "database/sql"
    "time"
)

// DownloadService - 下載服務
type DownloadService struct {
    db *sql.DB
}

// DownloadTrack - 下載歌曲（記錄）
func (s *DownloadService) DownloadTrack(ctx context.Context, userID string, trackID int64, quality string) error {
    // 1. 檢查訂閱狀態（只有付費用戶可下載）
    isPremium, err := s.checkPremiumStatus(ctx, userID)
    if err != nil {
        return err
    }
    if !isPremium {
        return fmt.Errorf("download requires premium subscription")
    }

    // 2. 檢查下載配額（每個裝置最多 10000 首）
    var downloadCount int
    query := `
        SELECT COUNT(*)
        FROM downloaded_tracks
        WHERE user_id = ? AND device_id = ?
    `
    // ... 檢查邏輯

    // 3. 記錄下載
    insertQuery := `
        INSERT INTO downloaded_tracks (user_id, track_id, device_id, quality, downloaded_at, expires_at)
        VALUES (?, ?, ?, ?, ?, ?)
    `
    expiresAt := time.Now().AddDate(0, 0, 30)  // 30 天後過期
    _, err = s.db.ExecContext(ctx, insertQuery, userID, trackID, "device-123", quality, time.Now(), expiresAt)

    return err
}

func (s *DownloadService) checkPremiumStatus(ctx context.Context, userID string) (bool, error) {
    var planType string
    query := `
        SELECT sp.plan_type
        FROM subscriptions sub
        JOIN subscription_plans sp ON sub.plan_id = sp.id
        WHERE sub.user_id = ? AND sub.status = 'active'
        LIMIT 1
    `
    err := s.db.QueryRowContext(ctx, query, userID).Scan(&planType)
    if err != nil {
        return false, err
    }

    return planType != "free", nil
}
```

---

## Act 6: 版權管理與藝人分潤

**Emma**: "Spotify 需要與唱片公司和藝人分享收入。每次播放都需要計算版稅。"

### 版稅計算

```
Spotify 版稅模式：
1. 總收入池：每月訂閱收入 × 70%
2. 每首歌的版稅 = (該歌播放次數 / 總播放次數) × 收入池
3. 分配：
   - 唱片公司/版權方：70%
   - 藝人：15%
   - 作曲家：15%

範例：
- 總收入：$1B/月
- 版稅池：$700M
- 總播放：100B 次
- 某首歌播放：100M 次
- 該歌版稅：($700M × 100M / 100B) = $700

每次播放平均版稅：$0.003 - $0.005
```

### 數據庫設計

```sql
-- 版權表
CREATE TABLE track_rights (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    track_id BIGINT NOT NULL,
    rights_holder_id VARCHAR(64),            -- 版權方 ID
    rights_type ENUM('recording', 'composition', 'performance'),
    percentage DECIMAL(5,2),                 -- 分潤比例
    territory VARCHAR(2),                    -- 地區代碼（US, UK, TW, etc.）
    start_date DATE,
    end_date DATE,
    FOREIGN KEY (track_id) REFERENCES tracks(id),
    INDEX idx_track_id (track_id)
);

-- 月度版稅報表
CREATE TABLE monthly_royalties (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    rights_holder_id VARCHAR(64) NOT NULL,
    track_id BIGINT NOT NULL,
    month DATE NOT NULL,                     -- 月份
    play_count BIGINT,                       -- 播放次數
    royalty_amount DECIMAL(12,2),            -- 版稅金額
    paid BOOLEAN DEFAULT FALSE,
    paid_at TIMESTAMP,
    INDEX idx_rights_holder (rights_holder_id, month),
    INDEX idx_month (month)
);
```

---

## 總結

從「簡單播放」到「完整的音樂平台」，我們學到了：

1. **音樂串流**：OGG Vorbis、多音質、小檔案快取
2. **播放列表**：建立、編輯、分享、協作
3. **推薦系統**：協同過濾 + NLP + 音訊分析
4. **社交功能**：追蹤、分享、動態
5. **離線下載**：付費功能、裝置管理
6. **版權管理**：版稅計算、分潤機制

**記住：音樂品質、推薦準確度、用戶體驗，三者需要平衡！**

**Spotify 的啟示**：
- 4 億月活躍用戶
- 7000 萬首歌曲
- 每天 1 億小時播放
- Discover Weekly 是殺手級功能
- 音訊分析是核心競爭力
- 版權管理是營運基礎

**核心理念：Personalized, social, accessible.（個人化、社交化、易取得）**
