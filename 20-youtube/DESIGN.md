# Chapter 20: YouTube - 影片分享平台

> 從零開始設計 YouTube：影片上傳、轉碼、CDN、推薦算法的完整實現

## 本章概述

這是一個關於 **YouTube 系統設計**的完整指南，使用**蘇格拉底式教學法**（Socratic Method）。你將跟隨 Emma（產品經理）、David（架構師）、Sarah（後端工程師）、Michael（運維工程師）和 Jennifer（前端工程師）一起，從零開始設計一個生產級的影片分享平台。

## 學習目標

- 理解**影片上傳**和**分片上傳**
- 掌握 **影片轉碼**（FFmpeg、多碼率）
- 學習 **CDN 分發策略**
- 實踐**影片元數據存儲**
- 了解**推薦算法**（協同過濾、內容推薦）
- 掌握**評論系統**和**點贊功能**
- 學習**播放統計**和**分析**
- 理解**橫向擴展**和**高可用**
- 掌握**成本優化**策略
- 學習 YouTube 的真實架構

## 角色介紹

- **Emma**：產品經理，負責定義 YouTube 的產品需求
- **David**：資深架構師，擅長設計可擴展的媒體系統
- **Sarah**：後端工程師，實現核心業務邏輯
- **Michael**：運維工程師，關注系統穩定性和成本
- **Jennifer**：前端工程師，負責播放器和用戶體驗

---

## Act 1: 影片上傳

**場景：產品需求會議**

**Emma**（產品經理）在白板上寫下 YouTube 的核心功能：

```
核心功能：
1. 用戶上傳影片
2. 自動轉碼（多種解析度）
3. 用戶觀看影片
4. 推薦相關影片
```

**Emma**: "我們要做一個影片分享平台，就像 YouTube。David，最簡單的影片上傳實現是什麼？"

**David**（架構師）思考片刻：

**David**: "最簡單的方式是 HTTP POST 上傳，把影片存到服務器。但影片很大（幾 GB），需要特殊處理。"

### 方案 1：單次上傳（不推薦）

```go
package main

import (
    "io"
    "net/http"
    "os"
)

// SimpleUploadService - 簡單上傳服務
type SimpleUploadService struct {
    uploadDir string
}

// UploadVideo - 上傳影片（單次上傳）
func (s *SimpleUploadService) UploadVideo(w http.ResponseWriter, r *http.Request) {
    // 限制最大文件大小（10GB）
    r.ParseMultipartForm(10 << 30) // 10 GB

    file, header, err := r.FormFile("video")
    if err != nil {
        http.Error(w, "Failed to read file", http.StatusBadRequest)
        return
    }
    defer file.Close()

    // 保存到本地
    dst, err := os.Create(s.uploadDir + "/" + header.Filename)
    if err != nil {
        http.Error(w, "Failed to save file", http.StatusInternalServerError)
        return
    }
    defer dst.Close()

    // 複製文件
    _, err = io.Copy(dst, file)
    if err != nil {
        http.Error(w, "Failed to copy file", http.StatusInternalServerError)
        return
    }

    w.Write([]byte("Upload successful"))
}
```

**Sarah**（後端工程師）提出問題：

**Sarah**: "這個方案有幾個問題：
1. **超時**：上傳 5GB 影片可能需要 30 分鐘，HTTP 容易超時
2. **斷點續傳**：如果網絡中斷，需要重新上傳
3. **內存占用**：大文件會占用大量內存
4. **並發上傳**：無法並行上傳"

**David**: "所以我們需要**分片上傳**（Chunked Upload）。"

### 方案 2：分片上傳（推薦）

```go
package main

import (
    "crypto/md5"
    "database/sql"
    "encoding/hex"
    "fmt"
    "io"
    "net/http"
    "os"
    "strconv"
    "time"
)

// ChunkedUploadService - 分片上傳服務
type ChunkedUploadService struct {
    db        *sql.DB
    uploadDir string
    chunkDir  string
}

// UploadSession - 上傳會話
type UploadSession struct {
    ID            string
    UserID        string
    Filename      string
    FileSize      int64
    ChunkSize     int64
    TotalChunks   int
    UploadedChunks map[int]bool
    Status        string // pending, uploading, completed, failed
    CreatedAt     time.Time
}

// InitiateUpload - 初始化上傳
func (s *ChunkedUploadService) InitiateUpload(w http.ResponseWriter, r *http.Request) {
    userID := r.FormValue("user_id")
    filename := r.FormValue("filename")
    fileSize, _ := strconv.ParseInt(r.FormValue("file_size"), 10, 64)

    // 生成上傳會話 ID
    sessionID := generateUploadSessionID()

    // 計算分片數量（每片 5MB）
    chunkSize := int64(5 * 1024 * 1024)
    totalChunks := int((fileSize + chunkSize - 1) / chunkSize)

    // 保存到數據庫
    query := `
        INSERT INTO upload_sessions (id, user_id, filename, file_size, chunk_size, total_chunks, status, created_at)
        VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)
    `
    _, err := s.db.Exec(query, sessionID, userID, filename, fileSize, chunkSize, totalChunks, time.Now())
    if err != nil {
        http.Error(w, "Failed to create upload session", http.StatusInternalServerError)
        return
    }

    // 返回會話信息
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, `{"session_id": "%s", "chunk_size": %d, "total_chunks": %d}`, sessionID, chunkSize, totalChunks)
}

// UploadChunk - 上傳分片
func (s *ChunkedUploadService) UploadChunk(w http.ResponseWriter, r *http.Request) {
    sessionID := r.FormValue("session_id")
    chunkIndex, _ := strconv.Atoi(r.FormValue("chunk_index"))

    // 讀取分片數據
    file, _, err := r.FormFile("chunk")
    if err != nil {
        http.Error(w, "Failed to read chunk", http.StatusBadRequest)
        return
    }
    defer file.Close()

    // 保存分片到臨時目錄
    chunkPath := fmt.Sprintf("%s/%s/%d", s.chunkDir, sessionID, chunkIndex)
    os.MkdirAll(fmt.Sprintf("%s/%s", s.chunkDir, sessionID), 0755)

    dst, err := os.Create(chunkPath)
    if err != nil {
        http.Error(w, "Failed to save chunk", http.StatusInternalServerError)
        return
    }
    defer dst.Close()

    io.Copy(dst, file)

    // 更新數據庫（標記該分片已上傳）
    query := `
        INSERT INTO uploaded_chunks (session_id, chunk_index, uploaded_at)
        VALUES (?, ?, ?)
    `
    s.db.Exec(query, sessionID, chunkIndex, time.Now())

    // 檢查是否所有分片都已上傳
    var uploadedCount int
    s.db.QueryRow(`SELECT COUNT(*) FROM uploaded_chunks WHERE session_id = ?`, sessionID).Scan(&uploadedCount)

    var totalChunks int
    s.db.QueryRow(`SELECT total_chunks FROM upload_sessions WHERE id = ?`, sessionID).Scan(&totalChunks)

    if uploadedCount == totalChunks {
        // 所有分片上傳完成，開始合併
        go s.mergeChunks(sessionID)
        w.Write([]byte(`{"status": "completed"}`))
    } else {
        w.Write([]byte(`{"status": "uploading"}`))
    }
}

// mergeChunks - 合併分片
func (s *ChunkedUploadService) mergeChunks(sessionID string) error {
    // 查詢上傳會話信息
    var filename string
    var totalChunks int
    query := `SELECT filename, total_chunks FROM upload_sessions WHERE id = ?`
    s.db.QueryRow(query, sessionID).Scan(&filename, &totalChunks)

    // 創建最終文件
    finalPath := fmt.Sprintf("%s/%s", s.uploadDir, sessionID+"_"+filename)
    dst, err := os.Create(finalPath)
    if err != nil {
        return err
    }
    defer dst.Close()

    // 按順序合併分片
    for i := 0; i < totalChunks; i++ {
        chunkPath := fmt.Sprintf("%s/%s/%d", s.chunkDir, sessionID, i)
        chunk, err := os.Open(chunkPath)
        if err != nil {
            return err
        }

        io.Copy(dst, chunk)
        chunk.Close()
    }

    // 更新數據庫狀態
    s.db.Exec(`UPDATE upload_sessions SET status = 'completed' WHERE id = ?`, sessionID)

    // 刪除臨時分片
    os.RemoveAll(fmt.Sprintf("%s/%s", s.chunkDir, sessionID))

    // 觸發轉碼任務
    s.triggerTranscoding(sessionID, finalPath)

    return nil
}

func generateUploadSessionID() string {
    h := md5.New()
    h.Write([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
    return hex.EncodeToString(h.Sum(nil))
}

func (s *ChunkedUploadService) triggerTranscoding(sessionID, videoPath string) {
    // 發送到 Kafka 或直接調用轉碼服務（Act 2）
}
```

**數據庫設計**：

```sql
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

CREATE TABLE uploaded_chunks (
    session_id VARCHAR(64) NOT NULL,
    chunk_index INT NOT NULL,
    uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (session_id, chunk_index)
);
```

**Michael**: "分片上傳解決了超時和斷點續傳的問題。但上傳到本地磁盤仍然不夠好，應該直接上傳到 S3。"

### 方案 3：S3 分片上傳

```go
package main

import (
    "context"
    "fmt"

    "github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3UploadService - S3 分片上傳
type S3UploadService struct {
    s3Client *s3.Client
    bucket   string
}

// InitiateMultipartUpload - 初始化 S3 分片上傳
func (s *S3UploadService) InitiateMultipartUpload(ctx context.Context, key string) (string, error) {
    output, err := s.s3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
        Bucket: &s.bucket,
        Key:    &key,
    })
    if err != nil {
        return "", err
    }

    return *output.UploadId, nil
}

// UploadPart - 上傳單個分片
func (s *S3UploadService) UploadPart(ctx context.Context, key, uploadID string, partNumber int, data []byte) (string, error) {
    output, err := s.s3Client.UploadPart(ctx, &s3.UploadPartInput{
        Bucket:     &s.bucket,
        Key:        &key,
        UploadId:   &uploadID,
        PartNumber: int32(partNumber),
        Body:       bytes.NewReader(data),
    })
    if err != nil {
        return "", err
    }

    return *output.ETag, nil
}

// CompleteMultipartUpload - 完成分片上傳
func (s *S3UploadService) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []types.CompletedPart) error {
    _, err := s.s3Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
        Bucket:   &s.bucket,
        Key:      &key,
        UploadId: &uploadID,
        MultipartUpload: &types.CompletedMultipartUpload{
            Parts: parts,
        },
    })
    return err
}
```

---

## Act 2: 影片轉碼

**Emma**: "用戶上傳的影片格式和解析度各不相同（4K、1080p、手機豎屏），我們需要轉碼成統一格式。"

**David**: "沒錯。我們需要轉碼成多種解析度（360p、720p、1080p、4K），讓用戶根據網速選擇。"

### 轉碼流程

```
1. 用戶上傳完成 → 原始影片存儲在 S3
2. 觸發轉碼任務 → Kafka 消息
3. 轉碼 Worker 消費任務
4. FFmpeg 轉碼 → 生成多種解析度
5. 上傳轉碼後的影片到 S3
6. 更新數據庫（影片狀態、CDN URL）
```

### 數據庫設計

```sql
CREATE TABLE videos (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    original_s3_key VARCHAR(512),                -- 原始影片 S3 key
    duration INT,                                 -- 影片長度（秒）
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

CREATE TABLE video_formats (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    resolution VARCHAR(10),                      -- 360p, 720p, 1080p, 4k
    format VARCHAR(10),                          -- mp4, webm
    s3_key VARCHAR(512),
    cdn_url VARCHAR(1024),
    file_size BIGINT,
    bitrate INT,                                 -- kbps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (video_id) REFERENCES videos(id),
    UNIQUE KEY uk_video_resolution (video_id, resolution, format)
);
```

### 實現：轉碼服務

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "os/exec"
)

// TranscodingService - 轉碼服務
type TranscodingService struct {
    db       *sql.DB
    s3Client *s3.Client
    bucket   string
}

// TranscodeVideo - 轉碼影片
func (s *TranscodingService) TranscodeVideo(ctx context.Context, videoID int64, originalS3Key string) error {
    // 1. 下載原始影片
    localPath := "/tmp/" + originalS3Key
    err := s.downloadFromS3(ctx, originalS3Key, localPath)
    if err != nil {
        return err
    }
    defer os.Remove(localPath)

    // 2. 獲取影片元數據（時長、解析度）
    duration, err := s.getVideoDuration(localPath)
    if err != nil {
        return err
    }

    // 更新數據庫
    s.db.Exec(`UPDATE videos SET duration = ?, status = 'transcoding' WHERE id = ?`, duration, videoID)

    // 3. 轉碼為多種解析度
    resolutions := []struct {
        Name    string
        Width   int
        Height  int
        Bitrate string
    }{
        {"360p", 640, 360, "800k"},
        {"720p", 1280, 720, "2500k"},
        {"1080p", 1920, 1080, "5000k"},
    }

    for _, res := range resolutions {
        outputPath := fmt.Sprintf("/tmp/%d_%s.mp4", videoID, res.Name)

        // 使用 FFmpeg 轉碼
        cmd := exec.Command("ffmpeg",
            "-i", localPath,
            "-vf", fmt.Sprintf("scale=%d:%d", res.Width, res.Height),
            "-c:v", "libx264",
            "-b:v", res.Bitrate,
            "-c:a", "aac",
            "-b:a", "128k",
            "-movflags", "+faststart", // 優化在線播放
            outputPath,
        )

        err := cmd.Run()
        if err != nil {
            log.Printf("Failed to transcode %s: %v", res.Name, err)
            continue
        }

        // 上傳到 S3
        s3Key := fmt.Sprintf("videos/%d/%s.mp4", videoID, res.Name)
        err = s.uploadToS3(ctx, outputPath, s3Key)
        if err != nil {
            log.Printf("Failed to upload %s: %v", res.Name, err)
            continue
        }

        // 獲取文件大小
        fileInfo, _ := os.Stat(outputPath)
        fileSize := fileInfo.Size()

        // 保存到數據庫
        cdnURL := fmt.Sprintf("https://cdn.example.com/%s", s3Key)
        query := `
            INSERT INTO video_formats (video_id, resolution, format, s3_key, cdn_url, file_size, bitrate)
            VALUES (?, ?, 'mp4', ?, ?, ?, ?)
        `
        s.db.Exec(query, videoID, res.Name, s3Key, cdnURL, fileSize, res.Bitrate)

        // 刪除臨時文件
        os.Remove(outputPath)
    }

    // 4. 更新影片狀態為 ready
    s.db.Exec(`UPDATE videos SET status = 'ready' WHERE id = ?`, videoID)

    return nil
}

// getVideoDuration - 獲取影片時長（使用 FFprobe）
func (s *TranscodingService) getVideoDuration(videoPath string) (int, error) {
    cmd := exec.Command("ffprobe",
        "-v", "error",
        "-show_entries", "format=duration",
        "-of", "default=noprint_wrappers=1:nokey=1",
        videoPath,
    )

    output, err := cmd.Output()
    if err != nil {
        return 0, err
    }

    duration, _ := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
    return int(duration), nil
}

func (s *TranscodingService) downloadFromS3(ctx context.Context, key, localPath string) error {
    // S3 下載邏輯
    return nil
}

func (s *TranscodingService) uploadToS3(ctx context.Context, localPath, key string) error {
    // S3 上傳邏輯
    return nil
}
```

### Kafka 異步處理

```go
package main

import (
    "context"
    "encoding/json"

    "github.com/segmentio/kafka-go"
)

// TranscodingWorker - 轉碼 Worker
type TranscodingWorker struct {
    reader  *kafka.Reader
    service *TranscodingService
}

func NewTranscodingWorker(kafkaBrokers []string, service *TranscodingService) *TranscodingWorker {
    return &TranscodingWorker{
        reader: kafka.NewReader(kafka.ReaderConfig{
            Brokers: kafkaBrokers,
            Topic:   "video.uploaded",
            GroupID: "transcoding-workers",
        }),
        service: service,
    }
}

func (w *TranscodingWorker) Start(ctx context.Context) error {
    for {
        msg, err := w.reader.ReadMessage(ctx)
        if err != nil {
            return err
        }

        var task struct {
            VideoID       int64  `json:"video_id"`
            OriginalS3Key string `json:"original_s3_key"`
        }

        json.Unmarshal(msg.Value, &task)

        // 轉碼
        err = w.service.TranscodeVideo(ctx, task.VideoID, task.OriginalS3Key)
        if err != nil {
            log.Printf("Transcoding failed for video %d: %v", task.VideoID, err)
        }
    }
}
```

**Michael**: "轉碼是 CPU 密集型任務，我們需要專用的轉碼服務器（GPU 加速）或使用雲服務（AWS MediaConvert）。"

---

## Act 3: CDN 分發和播放

**Jennifer**: "影片轉碼完成後，用戶如何觀看？如果用戶在中國，訪問美國的 S3 會很慢。"

**David**: "這就需要 **CDN**（內容分發網絡）。"

### CDN 架構

```
用戶觀看影片：
Client → CloudFront (全球邊緣節點) → S3 (源站)

優勢：
✅ 低延遲（就近訪問）
✅ 高帶寬（分散流量）
✅ 減輕源站壓力（CDN 緩存）
```

### 自適應碼率播放（Adaptive Bitrate Streaming）

**Jennifer**: "用戶的網速不同，如何自動選擇合適的解析度？"

**David**: "使用 **HLS**（HTTP Live Streaming）或 **DASH** 協議，支持自適應碼率。"

#### HLS 實現

```
1. 生成 HLS 播放列表（.m3u8 文件）
2. 將影片切分為多個 .ts 片段（每段 10 秒）
3. 客戶端根據網速自動切換解析度

目錄結構：
videos/
  123/
    360p/
      playlist.m3u8
      segment0.ts
      segment1.ts
      ...
    720p/
      playlist.m3u8
      segment0.ts
      ...
    1080p/
      playlist.m3u8
      segment0.ts
      ...
    master.m3u8  # 主播放列表
```

#### master.m3u8 示例

```
#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
https://cdn.example.com/videos/123/360p/playlist.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720
https://cdn.example.com/videos/123/720p/playlist.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080
https://cdn.example.com/videos/123/1080p/playlist.m3u8
```

#### FFmpeg 生成 HLS

```bash
# 生成 360p HLS
ffmpeg -i input.mp4 \
  -vf scale=640:360 \
  -c:v libx264 -b:v 800k \
  -c:a aac -b:a 128k \
  -hls_time 10 \
  -hls_list_size 0 \
  -f hls \
  360p/playlist.m3u8

# 生成 720p HLS
ffmpeg -i input.mp4 \
  -vf scale=1280:720 \
  -c:v libx264 -b:v 2500k \
  -c:a aac -b:a 128k \
  -hls_time 10 \
  -hls_list_size 0 \
  -f hls \
  720p/playlist.m3u8
```

---

## Act 4: 影片元數據和搜索

**Emma**: "用戶需要能夠搜索影片（按標題、描述、標籤）。"

### Elasticsearch 索引

```json
{
  "mappings": {
    "properties": {
      "video_id": {"type": "long"},
      "user_id": {"type": "keyword"},
      "username": {"type": "text"},
      "title": {"type": "text"},
      "description": {"type": "text"},
      "tags": {"type": "keyword"},
      "category": {"type": "keyword"},
      "duration": {"type": "integer"},
      "view_count": {"type": "long"},
      "like_count": {"type": "long"},
      "published_at": {"type": "date"},
      "thumbnail_url": {"type": "keyword"}
    }
  }
}
```

### 搜索服務

```go
package main

import (
    "context"
    "encoding/json"
    "strings"

    "github.com/elastic/go-elasticsearch/v8"
)

// VideoSearchService - 影片搜索服務
type VideoSearchService struct {
    es *elasticsearch.Client
}

// SearchVideos - 搜索影片
func (s *VideoSearchService) SearchVideos(ctx context.Context, keyword string, offset, limit int) ([]Video, error) {
    query := map[string]interface{}{
        "query": map[string]interface{}{
            "multi_match": map[string]interface{}{
                "query":  keyword,
                "fields": []string{"title^3", "description", "tags^2"}, // title 權重 3，tags 權重 2
            },
        },
        "from": offset,
        "size": limit,
        "sort": []map[string]interface{}{
            {"view_count": map[string]string{"order": "desc"}},
        },
    }

    queryJSON, _ := json.Marshal(query)

    res, err := s.es.Search(
        s.es.Search.WithContext(ctx),
        s.es.Search.WithIndex("videos"),
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
    var videos []Video

    for _, hit := range hits {
        source := hit.(map[string]interface{})["_source"].(map[string]interface{})
        video := Video{
            ID:          int64(source["video_id"].(float64)),
            Title:       source["title"].(string),
            Description: source["description"].(string),
            // ...
        }
        videos = append(videos, video)
    }

    return videos, nil
}
```

---

## Act 5: 推薦算法

**Emma**: "我們需要推薦系統，向用戶推薦他們可能感興趣的影片。"

**David**: "推薦算法有幾種：協同過濾、內容推薦、混合推薦。"

### 方案 1：協同過濾（Collaborative Filtering）

```
邏輯：
- 用戶 A 和用戶 B 都觀看了影片 1、2、3
- A 觀看的影片 4，B 可能也感興趣

實現：
1. 計算用戶相似度（Jaccard、餘弦相似度）
2. 找到相似用戶
3. 推薦相似用戶觀看的影片
```

```go
package main

import (
    "context"
    "database/sql"
)

// RecommendationService - 推薦服務
type RecommendationService struct {
    db *sql.DB
}

// GetSimilarUsers - 獲取相似用戶
func (s *RecommendationService) GetSimilarUsers(ctx context.Context, userID string, limit int) ([]string, error) {
    // 查找觀看了相同影片的用戶
    query := `
        SELECT v2.user_id, COUNT(*) as common_videos
        FROM video_views v1
        JOIN video_views v2 ON v1.video_id = v2.video_id
        WHERE v1.user_id = ? AND v2.user_id != ?
        GROUP BY v2.user_id
        ORDER BY common_videos DESC
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
        var commonVideos int
        rows.Scan(&similarUserID, &commonVideos)
        similarUsers = append(similarUsers, similarUserID)
    }

    return similarUsers, nil
}

// RecommendVideos - 推薦影片
func (s *RecommendationService) RecommendVideos(ctx context.Context, userID string, limit int) ([]int64, error) {
    // 1. 找到相似用戶
    similarUsers, err := s.GetSimilarUsers(ctx, userID, 50)
    if err != nil {
        return nil, err
    }

    if len(similarUsers) == 0 {
        return []int64{}, nil
    }

    // 2. 找到相似用戶觀看但當前用戶未觀看的影片
    placeholders := strings.Repeat("?,", len(similarUsers)-1) + "?"
    query := fmt.Sprintf(`
        SELECT video_id, COUNT(*) as score
        FROM video_views
        WHERE user_id IN (%s)
          AND video_id NOT IN (
              SELECT video_id FROM video_views WHERE user_id = ?
          )
        GROUP BY video_id
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

### 方案 2：內容推薦（Content-Based）

```
邏輯：
- 分析用戶過去觀看的影片特徵（類別、標籤）
- 推薦相似特徵的影片

實現：
1. 為影片打標籤（#科技、#遊戲、#美食）
2. 計算用戶興趣向量
3. 推薦高匹配度的影片
```

```go
// RecommendByTags - 基於標籤推薦
func (s *RecommendationService) RecommendByTags(ctx context.Context, userID string, limit int) ([]int64, error) {
    // 1. 找到用戶觀看過的影片的所有標籤
    query := `
        SELECT vt.tag, COUNT(*) as freq
        FROM video_views vv
        JOIN video_tags vt ON vv.video_id = vt.video_id
        WHERE vv.user_id = ?
        GROUP BY vt.tag
        ORDER BY freq DESC
        LIMIT 10
    `

    rows, err := s.db.QueryContext(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var tags []string
    for rows.Next() {
        var tag string
        var freq int
        rows.Scan(&tag, &freq)
        tags = append(tags, tag)
    }

    if len(tags) == 0 {
        return []int64{}, nil
    }

    // 2. 找到包含這些標籤的影片（用戶未觀看的）
    placeholders := strings.Repeat("?,", len(tags)-1) + "?"
    query = fmt.Sprintf(`
        SELECT vt.video_id, COUNT(*) as tag_match
        FROM video_tags vt
        WHERE vt.tag IN (%s)
          AND vt.video_id NOT IN (
              SELECT video_id FROM video_views WHERE user_id = ?
          )
        GROUP BY vt.video_id
        ORDER BY tag_match DESC
        LIMIT ?
    `, placeholders)

    args := make([]interface{}, len(tags)+2)
    for i, t := range tags {
        args[i] = t
    }
    args[len(tags)] = userID
    args[len(tags)+1] = limit

    rows, err = s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var videoIDs []int64
    for rows.Next() {
        var videoID int64
        var tagMatch int
        rows.Scan(&videoID, &tagMatch)
        videoIDs = append(videoIDs, videoID)
    }

    return videoIDs, nil
}
```

---

## Act 6: 評論和互動

**Emma**: "用戶需要能夠評論、點贊影片。"

### 數據庫設計

```sql
CREATE TABLE video_comments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    parent_id BIGINT,                            -- 回覆評論
    content TEXT NOT NULL,
    like_count INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_video_id (video_id, created_at DESC),
    INDEX idx_parent_id (parent_id)
);

CREATE TABLE video_likes (
    video_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (video_id, user_id),
    INDEX idx_video_id (video_id)
);

CREATE TABLE comment_likes (
    comment_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (comment_id, user_id)
);
```

### 實現：評論服務

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

// AddComment - 添加評論
func (s *CommentService) AddComment(ctx context.Context, videoID int64, userID, content string, parentID *int64) (int64, error) {
    query := `INSERT INTO video_comments (video_id, user_id, parent_id, content, created_at) VALUES (?, ?, ?, ?, ?)`
    result, err := s.db.ExecContext(ctx, query, videoID, userID, parentID, content, time.Now())
    if err != nil {
        return 0, err
    }

    commentID, _ := result.LastInsertId()

    // 增加影片評論數
    s.db.ExecContext(ctx, `UPDATE videos SET comment_count = comment_count + 1 WHERE id = ?`, videoID)

    return commentID, nil
}

// GetComments - 獲取評論列表（分頁）
func (s *CommentService) GetComments(ctx context.Context, videoID int64, offset, limit int) ([]Comment, error) {
    query := `
        SELECT id, user_id, parent_id, content, like_count, created_at
        FROM video_comments
        WHERE video_id = ? AND parent_id IS NULL
        ORDER BY like_count DESC, created_at DESC
        LIMIT ? OFFSET ?
    `

    rows, err := s.db.QueryContext(ctx, query, videoID, limit, offset)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var comments []Comment
    for rows.Next() {
        var c Comment
        rows.Scan(&c.ID, &c.UserID, &c.ParentID, &c.Content, &c.LikeCount, &c.CreatedAt)
        comments = append(comments, c)
    }

    return comments, nil
}
```

---

## Act 7: 播放統計和分析

**Emma**: "我們需要追蹤用戶觀看行為：觀看次數、觀看時長、完成率。"

### 數據庫設計

```sql
CREATE TABLE video_views (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    video_id BIGINT NOT NULL,
    user_id VARCHAR(64),                         -- 可為空（未登錄用戶）
    watch_duration INT,                          -- 觀看時長（秒）
    completion_rate DECIMAL(5,2),               -- 完成率（%）
    device_type VARCHAR(20),                    -- mobile, desktop, tv
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_video_id (video_id),
    INDEX idx_user_id (user_id, created_at DESC)
);
```

### 實現：播放統計

```go
package main

import (
    "context"
    "database/sql"
    "time"
)

// ViewService - 播放統計服務
type ViewService struct {
    db *sql.DB
}

// RecordView - 記錄觀看
func (s *ViewService) RecordView(ctx context.Context, videoID int64, userID string, watchDuration, totalDuration int, deviceType string) error {
    completionRate := float64(watchDuration) / float64(totalDuration) * 100

    query := `INSERT INTO video_views (video_id, user_id, watch_duration, completion_rate, device_type, created_at) VALUES (?, ?, ?, ?, ?, ?)`
    _, err := s.db.ExecContext(ctx, query, videoID, userID, watchDuration, completionRate, deviceType, time.Now())
    if err != nil {
        return err
    }

    // 增加觀看次數（異步）
    go s.incrementViewCount(videoID)

    return nil
}

func (s *ViewService) incrementViewCount(videoID int64) {
    s.db.Exec(`UPDATE videos SET view_count = view_count + 1 WHERE id = ?`, videoID)
}

// GetViewStats - 獲取觀看統計
func (s *ViewService) GetViewStats(ctx context.Context, videoID int64) (map[string]interface{}, error) {
    var totalViews int64
    var avgWatchDuration float64
    var avgCompletionRate float64

    query := `
        SELECT
            COUNT(*) as total_views,
            AVG(watch_duration) as avg_watch_duration,
            AVG(completion_rate) as avg_completion_rate
        FROM video_views
        WHERE video_id = ?
    `

    err := s.db.QueryRowContext(ctx, query, videoID).Scan(&totalViews, &avgWatchDuration, &avgCompletionRate)
    if err != nil {
        return nil, err
    }

    return map[string]interface{}{
        "total_views":          totalViews,
        "avg_watch_duration":   avgWatchDuration,
        "avg_completion_rate":  avgCompletionRate,
    }, nil
}
```

---

## Act 8: 縮圖生成

**Emma**: "每個影片需要縮圖（Thumbnail），讓用戶在瀏覽時預覽。"

### FFmpeg 生成縮圖

```bash
# 從影片第 5 秒截圖
ffmpeg -i input.mp4 -ss 00:00:05 -vframes 1 thumbnail.jpg

# 生成多個縮圖（每 10 秒一張）
ffmpeg -i input.mp4 -vf fps=1/10 thumbnail_%03d.jpg
```

### 實現：縮圖生成

```go
package main

import (
    "fmt"
    "os/exec"
)

// ThumbnailService - 縮圖服務
type ThumbnailService struct {
    s3Client *s3.Client
    bucket   string
}

// GenerateThumbnails - 生成縮圖
func (s *ThumbnailService) GenerateThumbnails(ctx context.Context, videoID int64, videoPath string) error {
    // 生成 3 張縮圖（5秒、中間、最後 5 秒）
    timestamps := []string{"00:00:05", "50%", "99%"}

    for i, ts := range timestamps {
        outputPath := fmt.Sprintf("/tmp/%d_thumb_%d.jpg", videoID, i)

        cmd := exec.Command("ffmpeg",
            "-i", videoPath,
            "-ss", ts,
            "-vframes", "1",
            "-vf", "scale=320:180",
            outputPath,
        )

        err := cmd.Run()
        if err != nil {
            return err
        }

        // 上傳到 S3
        s3Key := fmt.Sprintf("thumbnails/%d/thumb_%d.jpg", videoID, i)
        s.uploadToS3(ctx, outputPath, s3Key)

        os.Remove(outputPath)
    }

    return nil
}
```

---

## Act 9: 橫向擴展和高可用

**Michael**: "YouTube 每天上傳 500 小時的影片，我們需要橫向擴展。"

### 最終架構

```
┌─────────────────────────────────────────┐
│         CDN (CloudFront)                 │
│      (影片、縮圖分發)                     │
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

---

## Act 10: 成本優化

**Michael**: "存儲和帶寬成本很高，我們需要優化。"

### 成本分析

```
場景：1 億月活躍用戶

假設：
- 平均每人每天觀看 5 個影片
- 每個影片平均 10 分鐘
- 平均 720p（2.5 Mbps）

帶寬：
- 每天：1 億 × 5 × 10 分鐘 × 2.5 Mbps = 7.5 PB/天
- 每月：225 PB

CDN 成本：
- 225 PB × $0.085/GB = $19,125,000/月

存儲：
- 每天上傳 500 小時影片
- 每小時影片 × 4 個解析度 × 5GB = 10 TB/天
- 每月：300 TB

S3 成本：
- 300 TB × $0.023/GB = $6,900/月（累積）

轉碼成本：
- 500 小時/天 × 4 個解析度 × $0.015/分鐘 = $1,800/天 = $54,000/月

總成本：約 $19,185,900/月

單用戶成本：$0.19/月
```

### 優化策略

```
1. CDN 優化：
   - 使用更便宜的 CDN（Cloudflare）
   - 根據地區選擇不同 CDN
   - 冷門影片降級到 S3 直連
   → 節省 30% CDN 成本

2. 存儲優化：
   - 低觀看量影片刪除低解析度版本
   - 使用 S3 Glacier（冷存儲）
   - 智能壓縮（AV1 編碼）
   → 節省 40% 存儲成本

3. 轉碼優化：
   - 使用 GPU 加速（提速 10 倍）
   - 按需轉碼（先轉 720p，有觀看再轉 1080p）
   - 使用 Spot Instance
   → 節省 60% 轉碼成本

優化後總成本：約 $12,000,000/月
單用戶成本：$0.12/月
```

---

## 總結

從「簡單上傳」到「完整的影片平台」，我們學到了：

1. **分片上傳**：支持大文件、斷點續傳
2. **影片轉碼**：FFmpeg、多解析度、HLS
3. **CDN 分發**：全球低延遲、自適應碼率
4. **推薦算法**：協同過濾、內容推薦
5. **橫向擴展**：分庫分表、轉碼集群
6. **成本優化**：CDN、存儲、轉碼優化

**記住：可靠性、用戶體驗、成本，三者需要平衡！**

**YouTube 的啟示**：
- 每分鐘上傳 500 小時影片
- 20 億月活躍用戶
- 簡單勝過複雜（S3 + CDN + FFmpeg）
- 成本優化是永恆的主題

**核心理念：Scalable, reliable, cost-effective.（可擴展、可靠、成本優化）**
