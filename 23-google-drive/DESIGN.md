# Chapter 23: Google Drive - 雲端儲存與協作平台

> 從零開始設計 Google Drive：檔案同步、分享、協作編輯、版本控制的完整實現

## 本章概述

這是一個關於 **Google Drive 系統設計**的完整指南，使用**蘇格拉底式教學法**（Socratic Method）。你將跟隨 Emma（產品經理）、David（架構師）、Sarah（後端工程師）、Michael（運維工程師）和 Alex（前端工程師）一起，從零開始設計一個生產級的雲端儲存平台。

## 學習目標

- 理解**檔案分塊上傳**（Chunking）
- 掌握 **檔案同步機制**（Delta Sync）
- 學習 **衝突解決**策略
- 實踐**檔案分享與權限管理**
- 了解**協作編輯**（Operational Transformation）
- 掌握**版本控制**系統
- 學習**去重與壓縮**技術
- 理解**全文搜尋**實作
- 掌握**離線模式**設計
- 學習 Google Drive 的真實架構

## 角色介紹

- **Emma**：產品經理，負責定義 Google Drive 的產品需求
- **David**：資深架構師，擅長設計分散式儲存系統
- **Sarah**：後端工程師，實現核心業務邏輯
- **Michael**：運維工程師，關注系統穩定性和成本
- **Alex**：前端工程師，負責同步邏輯和離線支援

---

## Act 1: 檔案上傳與儲存

**場景：產品需求會議**

**Emma**（產品經理）在白板上寫下 Google Drive 的核心功能：

```
核心功能：
1. 檔案上傳/下載
2. 多裝置同步
3. 檔案分享
4. 協作編輯
5. 版本控制
6. 全文搜尋
7. 離線存取
```

**Emma**: "我們要做一個雲端儲存服務，就像 Google Drive。David，最簡單的檔案上傳實作是什麼？"

**David**（架構師）思考片刻：

**David**: "最簡單的方式是一次上傳整個檔案到伺服器。但大檔案（幾 GB）會遇到問題：超時、斷點續傳、重複上傳。"

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

// UploadFile - 上傳檔案（單次上傳）
func (s *SimpleUploadService) UploadFile(w http.ResponseWriter, r *http.Request) {
    r.ParseMultipartForm(10 << 30) // 10 GB

    file, header, err := r.FormFile("file")
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

    io.Copy(dst, file)
    w.Write([]byte("Upload successful"))
}
```

**Sarah**: "這有幾個問題：
1. **超時**：大檔案上傳需要很長時間
2. **斷點續傳**：網路中斷需要重新上傳
3. **重複上傳**：相同檔案重複上傳浪費空間
4. **同步效率**：修改一個字就要重傳整個檔案"

**David**: "所以我們需要 **分塊上傳**（Chunked Upload）和 **內容定址儲存**（Content-Addressable Storage）。"

### 方案 2：分塊上傳 + 去重（推薦）

```go
package main

import (
    "context"
    "crypto/sha256"
    "database/sql"
    "encoding/hex"
    "fmt"
    "io"
    "os"
    "time"
)

// ChunkedUploadService - 分塊上傳服務
type ChunkedUploadService struct {
    db         *sql.DB
    chunkStore ChunkStore
}

// ChunkStore - 分塊儲存介面
type ChunkStore interface {
    SaveChunk(chunkHash string, data []byte) error
    GetChunk(chunkHash string) ([]byte, error)
    ChunkExists(chunkHash string) bool
}

// FileMetadata - 檔案元數據
type FileMetadata struct {
    ID          int64     `json:"id"`
    Name        string    `json:"name"`
    Path        string    `json:"path"`
    Size        int64     `json:"size"`
    MimeType    string    `json:"mime_type"`
    OwnerID     string    `json:"owner_id"`
    ParentID    int64     `json:"parent_id"`
    IsFolder    bool      `json:"is_folder"`
    ContentHash string    `json:"content_hash"`  // 檔案內容的 SHA-256
    ChunkHashes []string  `json:"chunk_hashes"`  // 分塊 hash 列表
    CreatedAt   time.Time `json:"created_at"`
    ModifiedAt  time.Time `json:"modified_at"`
}

// InitiateUpload - 初始化上傳
func (s *ChunkedUploadService) InitiateUpload(ctx context.Context, userID, filename string, fileSize int64, parentID int64) (int64, error) {
    query := `
        INSERT INTO files (name, owner_id, parent_id, size, is_folder, status, created_at, modified_at)
        VALUES (?, ?, ?, ?, FALSE, 'uploading', NOW(), NOW())
    `
    result, err := s.db.ExecContext(ctx, query, filename, userID, parentID, fileSize)
    if err != nil {
        return 0, err
    }

    fileID, _ := result.LastInsertId()
    return fileID, nil
}

// UploadChunk - 上傳分塊
func (s *ChunkedUploadService) UploadChunk(ctx context.Context, fileID int64, chunkIndex int, data []byte) (string, error) {
    // 1. 計算分塊的 SHA-256
    hash := sha256.Sum256(data)
    chunkHash := hex.EncodeToString(hash[:])

    // 2. 檢查分塊是否已存在（去重）
    if s.chunkStore.ChunkExists(chunkHash) {
        // 分塊已存在，直接記錄關聯
        return chunkHash, s.recordChunkMapping(ctx, fileID, chunkIndex, chunkHash)
    }

    // 3. 儲存新分塊
    err := s.chunkStore.SaveChunk(chunkHash, data)
    if err != nil {
        return "", err
    }

    // 4. 記錄分塊與檔案的關聯
    err = s.recordChunkMapping(ctx, fileID, chunkIndex, chunkHash)
    return chunkHash, err
}

// recordChunkMapping - 記錄分塊映射
func (s *ChunkedUploadService) recordChunkMapping(ctx context.Context, fileID int64, chunkIndex int, chunkHash string) error {
    query := `
        INSERT INTO file_chunks (file_id, chunk_index, chunk_hash)
        VALUES (?, ?, ?)
    `
    _, err := s.db.ExecContext(ctx, query, fileID, chunkIndex, chunkHash)
    return err
}

// CompleteUpload - 完成上傳
func (s *ChunkedUploadService) CompleteUpload(ctx context.Context, fileID int64) error {
    // 1. 查詢所有分塊
    query := `
        SELECT chunk_index, chunk_hash
        FROM file_chunks
        WHERE file_id = ?
        ORDER BY chunk_index
    `
    rows, err := s.db.QueryContext(ctx, query, fileID)
    if err != nil {
        return err
    }
    defer rows.Close()

    var chunkHashes []string
    for rows.Next() {
        var index int
        var hash string
        rows.Scan(&index, &hash)
        chunkHashes = append(chunkHashes, hash)
    }

    // 2. 計算整個檔案的 hash（用於去重）
    contentHash := s.calculateFileHash(chunkHashes)

    // 3. 檢查是否已存在相同內容的檔案（完整去重）
    var existingFileID int64
    err = s.db.QueryRowContext(ctx, `
        SELECT id FROM files
        WHERE content_hash = ? AND owner_id = (SELECT owner_id FROM files WHERE id = ?)
        AND status = 'active'
        LIMIT 1
    `, contentHash, fileID).Scan(&existingFileID)

    if err == nil {
        // 已存在相同檔案，刪除當前上傳，返回既有檔案
        s.db.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, fileID)
        return fmt.Errorf("duplicate file exists: %d", existingFileID)
    }

    // 4. 更新檔案狀態
    updateQuery := `
        UPDATE files
        SET content_hash = ?, status = 'active', modified_at = NOW()
        WHERE id = ?
    `
    _, err = s.db.ExecContext(ctx, updateQuery, contentHash, fileID)
    return err
}

// calculateFileHash - 計算檔案 hash（基於分塊 hash）
func (s *ChunkedUploadService) calculateFileHash(chunkHashes []string) string {
    h := sha256.New()
    for _, chunkHash := range chunkHashes {
        h.Write([]byte(chunkHash))
    }
    return hex.EncodeToString(h.Sum(nil))
}

// DownloadFile - 下載檔案
func (s *ChunkedUploadService) DownloadFile(ctx context.Context, fileID int64) ([]byte, error) {
    // 1. 查詢分塊列表
    query := `
        SELECT chunk_hash
        FROM file_chunks
        WHERE file_id = ?
        ORDER BY chunk_index
    `
    rows, err := s.db.QueryContext(ctx, query, fileID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var result []byte

    // 2. 依序獲取並拼接分塊
    for rows.Next() {
        var chunkHash string
        rows.Scan(&chunkHash)

        chunkData, err := s.chunkStore.GetChunk(chunkHash)
        if err != nil {
            return nil, err
        }

        result = append(result, chunkData...)
    }

    return result, nil
}
```

### 數據庫設計

```sql
-- 檔案/資料夾表
CREATE TABLE files (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    path TEXT,                               -- 完整路徑（用於快速查詢）
    owner_id VARCHAR(64) NOT NULL,
    parent_id BIGINT,                        -- NULL 表示根目錄
    is_folder BOOLEAN DEFAULT FALSE,
    size BIGINT DEFAULT 0,                   -- bytes
    mime_type VARCHAR(100),
    content_hash VARCHAR(64),                -- SHA-256（用於去重）
    status ENUM('uploading', 'active', 'deleted') DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    INDEX idx_owner_parent (owner_id, parent_id, status),
    INDEX idx_path (path(255)),
    INDEX idx_content_hash (content_hash),
    FOREIGN KEY (parent_id) REFERENCES files(id) ON DELETE CASCADE
);

-- 分塊表
CREATE TABLE chunks (
    chunk_hash VARCHAR(64) PRIMARY KEY,      -- SHA-256
    size INT NOT NULL,
    s3_key VARCHAR(512),                     -- S3 儲存位置
    ref_count INT DEFAULT 0,                 -- 參考計數（用於垃圾回收）
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 檔案-分塊映射表
CREATE TABLE file_chunks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    chunk_index INT NOT NULL,
    chunk_hash VARCHAR(64) NOT NULL,
    UNIQUE KEY uk_file_chunk (file_id, chunk_index),
    INDEX idx_chunk_hash (chunk_hash),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE,
    FOREIGN KEY (chunk_hash) REFERENCES chunks(chunk_hash)
);
```

---

## Act 2: 檔案同步機制

**Alex**（前端工程師）提出問題：

**Alex**: "用戶在手機修改了一個檔案，如何快速同步到電腦？如果只修改了一個字，不應該重新上傳整個檔案。"

**David**: "這就需要 **Delta Sync**（差異同步）。我們只傳輸變更的部分，而不是整個檔案。"

### Delta Sync 原理

```
場景：用戶修改了一個 10 MB 的文件，只改了 1 KB

傳統同步：
- 重新上傳 10 MB
- 浪費頻寬和時間

Delta Sync：
- 計算檔案差異
- 只上傳 1 KB 變更
- 伺服器端重建檔案

技術：
- Rolling Hash（Rabin Fingerprinting）
- rsync 演算法
- Google 的 Diff-Match-Patch
```

### 實作：同步服務

```go
package main

import (
    "context"
    "crypto/sha256"
    "database/sql"
    "encoding/hex"
    "time"
)

// SyncService - 同步服務
type SyncService struct {
    db *sql.DB
}

// DeviceSync - 裝置同步記錄
type DeviceSync struct {
    DeviceID       string    `json:"device_id"`
    LastSyncTime   time.Time `json:"last_sync_time"`
    LastSyncCursor int64     `json:"last_sync_cursor"`
}

// FileChange - 檔案變更
type FileChange struct {
    FileID       int64     `json:"file_id"`
    ChangeType   string    `json:"change_type"`  // created, modified, deleted, moved
    Name         string    `json:"name"`
    Path         string    `json:"path"`
    Size         int64     `json:"size"`
    ContentHash  string    `json:"content_hash"`
    ModifiedAt   time.Time `json:"modified_at"`
    OldPath      string    `json:"old_path,omitempty"`  // 用於 moved
}

// GetChanges - 獲取自上次同步以來的變更
func (s *SyncService) GetChanges(ctx context.Context, userID, deviceID string, lastSyncTime time.Time) ([]FileChange, error) {
    query := `
        SELECT
            f.id, f.name, f.path, f.size, f.content_hash, f.modified_at,
            CASE
                WHEN f.deleted_at IS NOT NULL THEN 'deleted'
                WHEN f.created_at > ? THEN 'created'
                ELSE 'modified'
            END as change_type
        FROM files f
        WHERE f.owner_id = ?
          AND f.modified_at > ?
          AND NOT EXISTS (
              SELECT 1 FROM device_sync_state dss
              WHERE dss.device_id = ?
                AND dss.file_id = f.id
                AND dss.synced_version = f.content_hash
          )
        ORDER BY f.modified_at ASC
        LIMIT 1000
    `

    rows, err := s.db.QueryContext(ctx, query, lastSyncTime, userID, lastSyncTime, deviceID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var changes []FileChange
    for rows.Next() {
        var c FileChange
        rows.Scan(&c.FileID, &c.Name, &c.Path, &c.Size, &c.ContentHash, &c.ModifiedAt, &c.ChangeType)
        changes = append(changes, c)
    }

    return changes, nil
}

// RecordSync - 記錄同步狀態
func (s *SyncService) RecordSync(ctx context.Context, deviceID string, fileID int64, contentHash string) error {
    query := `
        INSERT INTO device_sync_state (device_id, file_id, synced_version, synced_at)
        VALUES (?, ?, ?, NOW())
        ON DUPLICATE KEY UPDATE synced_version = ?, synced_at = NOW()
    `
    _, err := s.db.ExecContext(ctx, query, deviceID, fileID, contentHash, contentHash)
    return err
}

// DetectConflict - 檢測衝突
func (s *SyncService) DetectConflict(ctx context.Context, fileID int64, deviceID string, expectedHash string) (bool, error) {
    var currentHash string
    query := `SELECT content_hash FROM files WHERE id = ?`
    err := s.db.QueryRowContext(ctx, query, fileID).Scan(&currentHash)
    if err != nil {
        return false, err
    }

    // 如果當前 hash 與預期不同，表示有衝突
    return currentHash != expectedHash, nil
}

// CreateConflictCopy - 建立衝突副本
func (s *SyncService) CreateConflictCopy(ctx context.Context, fileID int64, deviceID string) (int64, error) {
    // 查詢原始檔案資訊
    var name, path, ownerID string
    var parentID int64
    var size int64
    var contentHash string

    query := `SELECT name, path, owner_id, parent_id, size, content_hash FROM files WHERE id = ?`
    err := s.db.QueryRowContext(ctx, query, fileID).Scan(&name, &path, &ownerID, &parentID, &size, &contentHash)
    if err != nil {
        return 0, err
    }

    // 建立衝突副本（加上時間戳和裝置標識）
    conflictName := fmt.Sprintf("%s (衝突副本 %s %s)", name, deviceID, time.Now().Format("2006-01-02 15:04"))

    insertQuery := `
        INSERT INTO files (name, path, owner_id, parent_id, size, content_hash, is_folder, status)
        VALUES (?, ?, ?, ?, ?, ?, FALSE, 'active')
    `
    result, err := s.db.ExecContext(ctx, insertQuery, conflictName, path+"/"+conflictName, ownerID, parentID, size, contentHash)
    if err != nil {
        return 0, err
    }

    conflictFileID, _ := result.LastInsertId()

    // 複製分塊映射
    s.db.ExecContext(ctx, `
        INSERT INTO file_chunks (file_id, chunk_index, chunk_hash)
        SELECT ?, chunk_index, chunk_hash
        FROM file_chunks
        WHERE file_id = ?
    `, conflictFileID, fileID)

    return conflictFileID, nil
}
```

### 衝突解決策略

```
衝突場景：
- 用戶 A 在裝置 1 修改檔案
- 用戶 A 在裝置 2 也修改同一檔案
- 兩個裝置同時同步

解決方案 1：Last Write Wins（最後寫入獲勝）
- 簡單粗暴
- 可能丟失資料
- Dropbox 早期使用

解決方案 2：Conflict Copy（衝突副本）
- 保留兩個版本
- 讓用戶手動合併
- Google Drive、Dropbox 使用

解決方案 3：Operational Transformation（OT）
- 自動合併變更
- 複雜但體驗最好
- Google Docs 使用

Google Drive 的選擇：
- 一般檔案：Conflict Copy
- Google Docs：Operational Transformation
```

---

## Act 3: 檔案分享與權限管理

**Emma**: "用戶需要能夠分享檔案給其他人，並設定權限（檢視、編輯、留言）。"

### 數據庫設計

```sql
-- 分享連結表
CREATE TABLE shares (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    shared_by VARCHAR(64) NOT NULL,
    share_type ENUM('link', 'user', 'domain') DEFAULT 'link',
    share_token VARCHAR(64) UNIQUE,          -- 分享連結的 token
    permission ENUM('viewer', 'commenter', 'editor') DEFAULT 'viewer',
    password_hash VARCHAR(128),              -- 可選密碼保護
    expires_at TIMESTAMP,                    -- 可選過期時間
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_file_id (file_id),
    INDEX idx_share_token (share_token),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);

-- 用戶權限表
CREATE TABLE file_permissions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    user_id VARCHAR(64),                     -- NULL 表示公開
    permission ENUM('viewer', 'commenter', 'editor', 'owner') DEFAULT 'viewer',
    granted_by VARCHAR(64) NOT NULL,
    granted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_file_user (file_id, user_id),
    INDEX idx_user_id (user_id),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);

-- 活動日誌表（審計）
CREATE TABLE activity_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    action ENUM('view', 'download', 'upload', 'edit', 'delete', 'share', 'unshare') NOT NULL,
    ip_address VARCHAR(45),
    user_agent TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_file_id (file_id, created_at DESC),
    INDEX idx_user_id (user_id, created_at DESC)
);
```

### 實作：分享服務

```go
package main

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/base64"
    "fmt"
    "time"
)

// SharingService - 分享服務
type SharingService struct {
    db *sql.DB
}

// CreateShareLink - 建立分享連結
func (s *SharingService) CreateShareLink(ctx context.Context, fileID int64, userID string, permission string, expiresIn time.Duration) (string, error) {
    // 1. 檢查權限（只有 owner 或 editor 可以分享）
    hasPermission, err := s.checkPermission(ctx, fileID, userID, []string{"owner", "editor"})
    if err != nil || !hasPermission {
        return "", fmt.Errorf("no permission to share")
    }

    // 2. 生成隨機 token
    token := generateShareToken()

    // 3. 計算過期時間
    var expiresAt *time.Time
    if expiresIn > 0 {
        t := time.Now().Add(expiresIn)
        expiresAt = &t
    }

    // 4. 儲存分享記錄
    query := `
        INSERT INTO shares (file_id, shared_by, share_type, share_token, permission, expires_at)
        VALUES (?, ?, 'link', ?, ?, ?)
    `
    _, err = s.db.ExecContext(ctx, query, fileID, userID, token, permission, expiresAt)
    if err != nil {
        return "", err
    }

    return token, nil
}

// GrantPermission - 授予權限給特定用戶
func (s *SharingService) GrantPermission(ctx context.Context, fileID int64, grantedBy, targetUser, permission string) error {
    // 檢查授權者權限
    hasPermission, err := s.checkPermission(ctx, fileID, grantedBy, []string{"owner", "editor"})
    if err != nil || !hasPermission {
        return fmt.Errorf("no permission to grant")
    }

    query := `
        INSERT INTO file_permissions (file_id, user_id, permission, granted_by)
        VALUES (?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE permission = ?, granted_at = NOW()
    `
    _, err = s.db.ExecContext(ctx, query, fileID, targetUser, permission, grantedBy, permission)
    return err
}

// AccessSharedFile - 透過分享連結存取檔案
func (s *SharingService) AccessSharedFile(ctx context.Context, shareToken string) (*FileMetadata, string, error) {
    // 1. 查詢分享記錄
    var fileID int64
    var permission string
    var expiresAt sql.NullTime

    query := `
        SELECT file_id, permission, expires_at
        FROM shares
        WHERE share_token = ?
    `
    err := s.db.QueryRowContext(ctx, query, shareToken).Scan(&fileID, &permission, &expiresAt)
    if err != nil {
        return nil, "", fmt.Errorf("invalid share link")
    }

    // 2. 檢查是否過期
    if expiresAt.Valid && time.Now().After(expiresAt.Time) {
        return nil, "", fmt.Errorf("share link expired")
    }

    // 3. 查詢檔案資訊
    var file FileMetadata
    fileQuery := `
        SELECT id, name, path, size, mime_type, owner_id, is_folder
        FROM files
        WHERE id = ? AND status = 'active'
    `
    err = s.db.QueryRowContext(ctx, fileQuery, fileID).Scan(
        &file.ID, &file.Name, &file.Path, &file.Size,
        &file.MimeType, &file.OwnerID, &file.IsFolder,
    )
    if err != nil {
        return nil, "", err
    }

    return &file, permission, nil
}

// checkPermission - 檢查用戶權限
func (s *SharingService) checkPermission(ctx context.Context, fileID int64, userID string, requiredPermissions []string) (bool, error) {
    var permission string
    query := `
        SELECT permission
        FROM file_permissions
        WHERE file_id = ? AND user_id = ?
    `
    err := s.db.QueryRowContext(ctx, query, fileID, userID).Scan(&permission)
    if err == sql.ErrNoRows {
        // 檢查是否為擁有者
        var ownerID string
        s.db.QueryRowContext(ctx, `SELECT owner_id FROM files WHERE id = ?`, fileID).Scan(&ownerID)
        if ownerID == userID {
            return true, nil
        }
        return false, nil
    }
    if err != nil {
        return false, err
    }

    for _, required := range requiredPermissions {
        if permission == required {
            return true, nil
        }
    }
    return false, nil
}

func generateShareToken() string {
    b := make([]byte, 32)
    rand.Read(b)
    return base64.URLEncoding.EncodeToString(b)
}

// LogActivity - 記錄活動
func (s *SharingService) LogActivity(ctx context.Context, fileID int64, userID, action, ipAddress, userAgent string) error {
    query := `
        INSERT INTO activity_logs (file_id, user_id, action, ip_address, user_agent)
        VALUES (?, ?, ?, ?, ?)
    `
    _, err := s.db.ExecContext(ctx, query, fileID, userID, action, ipAddress, userAgent)
    return err
}
```

---

## Act 4: 協作編輯（Google Docs）

**Emma**: "Google Docs 是 Google Drive 的殺手級功能，多人可以同時編輯一份文件。這如何實作？"

**David**: "這需要 **Operational Transformation (OT)** 或 **CRDT**（Conflict-free Replicated Data Type）技術。"

### Operational Transformation 原理

```
場景：
- 用戶 A 在位置 0 插入 "Hello "
- 用戶 B 在位置 0 插入 "World "
- 兩個操作同時發生

沒有 OT：
最終結果：不一致（A 看到 "Hello World "，B 看到 "World Hello "）

有 OT：
1. 伺服器接收 A 的操作：insert(0, "Hello ")
2. 伺服器接收 B 的操作：insert(0, "World ")
3. 轉換 B 的操作：考慮 A 的操作，insert(6, "World ")
4. 廣播給所有客戶端
5. 最終結果一致："Hello World "

核心：
- 每個操作有序列號
- 操作可以被轉換（Transform）
- 保證最終一致性
```

### 簡化實作

```go
package main

import (
    "context"
    "encoding/json"
    "sync"
    "time"

    "github.com/gorilla/websocket"
)

// CollaborationService - 協作編輯服務
type CollaborationService struct {
    sessions map[int64]*EditSession  // file_id -> session
    mu       sync.RWMutex
}

// EditSession - 編輯會話
type EditSession struct {
    FileID      int64
    Document    *Document
    Clients     map[string]*Client
    Operations  []Operation
    mu          sync.RWMutex
}

// Client - 客戶端連線
type Client struct {
    UserID     string
    Conn       *websocket.Conn
    Send       chan []byte
    LastSeqNum int
}

// Operation - 編輯操作
type Operation struct {
    Type     string `json:"type"`      // insert, delete, format
    Position int    `json:"position"`
    Text     string `json:"text,omitempty"`
    Length   int    `json:"length,omitempty"`
    SeqNum   int    `json:"seq_num"`
    UserID   string `json:"user_id"`
}

// Document - 文件內容
type Document struct {
    Content string
    Version int
}

// JoinSession - 加入編輯會話
func (s *CollaborationService) JoinSession(ctx context.Context, fileID int64, userID string, conn *websocket.Conn) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    session, exists := s.sessions[fileID]
    if !exists {
        // 建立新會話
        session = &EditSession{
            FileID:     fileID,
            Document:   &Document{Content: "", Version: 0},
            Clients:    make(map[string]*Client),
            Operations: []Operation{},
        }
        s.sessions[fileID] = session

        // 從資料庫載入文件內容
        // ...
    }

    // 加入客戶端
    client := &Client{
        UserID:     userID,
        Conn:       conn,
        Send:       make(chan []byte, 256),
        LastSeqNum: len(session.Operations),
    }

    session.mu.Lock()
    session.Clients[userID] = client
    session.mu.Unlock()

    // 發送當前文件狀態
    s.sendDocumentState(client, session)

    // 啟動訊息處理
    go s.handleClient(session, client)

    return nil
}

// HandleOperation - 處理編輯操作
func (s *CollaborationService) HandleOperation(session *EditSession, op Operation) error {
    session.mu.Lock()
    defer session.mu.Unlock()

    // 1. 應用操作到文件
    switch op.Type {
    case "insert":
        session.Document.Content = session.Document.Content[:op.Position] +
            op.Text +
            session.Document.Content[op.Position:]
    case "delete":
        session.Document.Content = session.Document.Content[:op.Position] +
            session.Document.Content[op.Position+op.Length:]
    }

    // 2. 記錄操作
    op.SeqNum = len(session.Operations)
    session.Operations = append(session.Operations, op)
    session.Document.Version++

    // 3. 廣播給所有客戶端（除了發送者）
    s.broadcastOperation(session, op)

    // 4. 定期持久化
    if session.Document.Version%10 == 0 {
        go s.saveDocument(session)
    }

    return nil
}

// broadcastOperation - 廣播操作
func (s *CollaborationService) broadcastOperation(session *EditSession, op Operation) {
    data, _ := json.Marshal(op)

    for userID, client := range session.Clients {
        if userID != op.UserID {
            select {
            case client.Send <- data:
            default:
                // 客戶端緩衝區滿，移除客戶端
                close(client.Send)
                delete(session.Clients, userID)
            }
        }
    }
}

func (s *CollaborationService) sendDocumentState(client *Client, session *EditSession) {
    // 發送完整文件內容
}

func (s *CollaborationService) handleClient(session *EditSession, client *Client) {
    // WebSocket 訊息處理
}

func (s *CollaborationService) saveDocument(session *EditSession) {
    // 持久化文件內容到資料庫
}
```

---

## Act 5: 版本控制

**Emma**: "用戶希望能夠查看檔案的歷史版本，並在需要時還原。"

### 數據庫設計

```sql
-- 版本記錄表
CREATE TABLE file_versions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    version_number INT NOT NULL,
    content_hash VARCHAR(64) NOT NULL,       -- 該版本的內容 hash
    size BIGINT NOT NULL,
    modified_by VARCHAR(64) NOT NULL,
    change_description TEXT,                 -- 可選的變更說明
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_file_version (file_id, version_number),
    INDEX idx_file_id (file_id, created_at DESC),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);
```

### 實作：版本控制

```go
package main

import (
    "context"
    "database/sql"
    "time"
)

// VersionService - 版本控制服務
type VersionService struct {
    db *sql.DB
}

// FileVersion - 檔案版本
type FileVersion struct {
    ID                int64     `json:"id"`
    FileID            int64     `json:"file_id"`
    VersionNumber     int       `json:"version_number"`
    ContentHash       string    `json:"content_hash"`
    Size              int64     `json:"size"`
    ModifiedBy        string    `json:"modified_by"`
    ChangeDescription string    `json:"change_description"`
    CreatedAt         time.Time `json:"created_at"`
}

// CreateVersion - 建立新版本
func (s *VersionService) CreateVersion(ctx context.Context, fileID int64, contentHash string, size int64, modifiedBy string) error {
    // 1. 獲取當前最大版本號
    var maxVersion int
    query := `SELECT COALESCE(MAX(version_number), 0) FROM file_versions WHERE file_id = ?`
    s.db.QueryRowContext(ctx, query, fileID).Scan(&maxVersion)

    // 2. 建立新版本
    insertQuery := `
        INSERT INTO file_versions (file_id, version_number, content_hash, size, modified_by)
        VALUES (?, ?, ?, ?, ?)
    `
    _, err := s.db.ExecContext(ctx, insertQuery, fileID, maxVersion+1, contentHash, size, modifiedBy)
    if err != nil {
        return err
    }

    // 3. 清理舊版本（保留最近 30 個版本）
    go s.cleanOldVersions(fileID, 30)

    return nil
}

// ListVersions - 列出所有版本
func (s *VersionService) ListVersions(ctx context.Context, fileID int64, limit int) ([]FileVersion, error) {
    query := `
        SELECT id, file_id, version_number, content_hash, size, modified_by, change_description, created_at
        FROM file_versions
        WHERE file_id = ?
        ORDER BY version_number DESC
        LIMIT ?
    `

    rows, err := s.db.QueryContext(ctx, query, fileID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var versions []FileVersion
    for rows.Next() {
        var v FileVersion
        rows.Scan(&v.ID, &v.FileID, &v.VersionNumber, &v.ContentHash, &v.Size, &v.ModifiedBy, &v.ChangeDescription, &v.CreatedAt)
        versions = append(versions, v)
    }

    return versions, nil
}

// RestoreVersion - 還原到指定版本
func (s *VersionService) RestoreVersion(ctx context.Context, fileID int64, versionNumber int, userID string) error {
    // 1. 查詢指定版本的內容 hash
    var contentHash string
    query := `SELECT content_hash FROM file_versions WHERE file_id = ? AND version_number = ?`
    err := s.db.QueryRowContext(ctx, query, fileID, versionNumber).Scan(&contentHash)
    if err != nil {
        return err
    }

    // 2. 更新檔案為該版本
    updateQuery := `UPDATE files SET content_hash = ?, modified_at = NOW() WHERE id = ?`
    _, err = s.db.ExecContext(ctx, updateQuery, contentHash, fileID)
    if err != nil {
        return err
    }

    // 3. 建立新版本記錄（還原操作）
    s.CreateVersion(ctx, fileID, contentHash, 0, userID)

    return nil
}

func (s *VersionService) cleanOldVersions(fileID int64, keepCount int) {
    // 刪除超過 keepCount 的舊版本
    query := `
        DELETE FROM file_versions
        WHERE file_id = ?
          AND version_number < (
              SELECT version_number
              FROM (
                  SELECT version_number
                  FROM file_versions
                  WHERE file_id = ?
                  ORDER BY version_number DESC
                  LIMIT 1 OFFSET ?
              ) tmp
          )
    `
    s.db.Exec(query, fileID, fileID, keepCount)
}
```

---

## Act 6: 全文搜尋

**Emma**: "用戶需要能夠搜尋檔案內容，不只是檔名。"

### 實作：搜尋服務

```go
package main

import (
    "context"

    "github.com/elastic/go-elasticsearch/v8"
)

// SearchService - 搜尋服務
type SearchService struct {
    es *elasticsearch.Client
}

// IndexFile - 索引檔案
func (s *SearchService) IndexFile(ctx context.Context, file *FileMetadata, content string) error {
    doc := map[string]interface{}{
        "file_id":     file.ID,
        "name":        file.Name,
        "path":        file.Path,
        "content":     content,
        "mime_type":   file.MimeType,
        "owner_id":    file.OwnerID,
        "size":        file.Size,
        "modified_at": file.ModifiedAt,
    }

    // 索引到 Elasticsearch
    // ...

    return nil
}

// SearchFiles - 搜尋檔案
func (s *SearchService) SearchFiles(ctx context.Context, userID, keyword string, limit int) ([]FileMetadata, error) {
    // Elasticsearch 查詢
    // ...

    return nil, nil
}
```

---

## 總結

從「簡單上傳」到「完整的雲端儲存平台」，我們學到了：

1. **分塊上傳**：支援大檔案、斷點續傳、去重
2. **檔案同步**：Delta Sync、衝突解決
3. **分享與權限**：連結分享、用戶權限、活動日誌
4. **協作編輯**：Operational Transformation、即時同步
5. **版本控制**：歷史記錄、還原功能
6. **全文搜尋**：Elasticsearch、內容索引

**記住：效能、可靠性、用戶體驗，三者需要平衡！**

**Google Drive 的啟示**：
- 10 億月活躍用戶
- 去重技術節省 90% 儲存空間
- Delta Sync 節省 95% 頻寬
- Operational Transformation 是協作的核心
- 版本控制保護用戶資料

**核心理念：Synced, shared, collaborative.（同步、分享、協作）**
