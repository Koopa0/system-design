# Google Drive - 雲端儲存與協作平台

> 完整的 Google Drive 系統設計：從檔案同步到協作編輯

## 概述

本章節展示如何設計一個生產級的**雲端儲存平台（Google Drive）**，支援：
- **檔案上傳/下載**：分塊上傳、斷點續傳、去重
- **多裝置同步**：Delta Sync、衝突解決
- **檔案分享**：連結分享、權限管理、密碼保護
- **協作編輯**：Operational Transformation、即時同步
- **版本控制**：歷史記錄、還原功能
- **全文搜尋**：Elasticsearch、內容索引
- **離線模式**：本地快取、離線存取
- **儲存優化**：內容去重、壓縮

## 學習目標

- 理解**檔案分塊**（Chunking）技術
- 掌握**內容定址儲存**（Content-Addressable Storage）
- 學習 **Delta Sync**（差異同步）
- 實踐**衝突解決**策略
- 了解 **Operational Transformation**（協作編輯）
- 掌握**版本控制**系統
- 學習**去重技術**（節省 90% 空間）
- 理解**權限管理**模型
- 掌握 Google Drive 的真實架構

## 核心概念

### 1. 檔案分塊（Chunking）

```
為什麼需要分塊？

問題：
- 大檔案（10 GB）上傳超時
- 網路中斷需要重新上傳
- 修改一個字就要重傳整個檔案

方案：分塊上傳
1. 將檔案切分為固定大小的塊（4 MB）
2. 每個塊獨立上傳
3. 支援斷點續傳
4. 只上傳變更的塊

分塊策略：

固定大小分塊：
- 每塊固定 4 MB
- 簡單但不適合 Delta Sync

內容定義分塊（Content-Defined Chunking）：
- 使用 Rolling Hash（Rabin Fingerprinting）
- 根據內容特徵切分
- 檔案開頭插入內容不會影響後續塊
- Dropbox、Google Drive 使用

優勢：
✅ 支援大檔案
✅ 斷點續傳
✅ Delta Sync（只傳變更的塊）
✅ 去重（相同塊只儲存一次）
```

### 2. 內容定址儲存（Content-Addressable Storage）

```
原理：
- 每個分塊用內容的 SHA-256 當作 ID
- 相同內容的塊共享同一份儲存

範例：
- 用戶 A 上傳「報告.pdf」
- 用戶 B 也上傳相同的「報告.pdf」
- 計算 SHA-256 發現相同
- 只儲存一份，節省空間

去重效果：
- 完整檔案去重：節省 50-70%
- 分塊去重：節省 80-90%
- Google Drive 實際節省：> 90%

範例計算：
假設：
- 1000 個用戶
- 每人上傳 1 GB「Windows 10 ISO」
- 沒有去重：1000 GB
- 有去重：1 GB
- 節省：99.9%

實作：
1. 上傳分塊時計算 SHA-256
2. 檢查該 hash 是否已存在
3. 存在：直接引用，增加參考計數
4. 不存在：儲存新分塊
```

### 3. Delta Sync（差異同步）

```
場景：
- 修改 10 MB 檔案的一個字
- 傳統：重新上傳 10 MB
- Delta Sync：只上傳變更的分塊（4 KB）

原理：
1. 客戶端計算檔案的分塊列表
2. 與伺服器端的分塊列表比對
3. 只上傳新增或變更的分塊
4. 伺服器端重建檔案

範例：
原始檔案分塊：[A, B, C, D]
修改後分塊：[A, B, C', D]

只需上傳：C'（新分塊）
節省：75% 頻寬

Dropbox 的優化：
- 使用 Content-Defined Chunking
- 檔案開頭插入內容不會影響所有分塊
- 只影響第一個分塊

效果：
- 平均節省 95% 頻寬
- 同步速度提升 20 倍
```

### 4. 衝突解決

```
衝突場景：
- 裝置 A 離線修改檔案 → 版本 V1
- 裝置 B 離線修改同一檔案 → 版本 V2
- 兩個裝置同時上線同步

解決方案：

方案 1：Last Write Wins（最後寫入獲勝）
- 簡單粗暴
- 可能丟失資料
- 適用：非重要檔案

方案 2：Conflict Copy（衝突副本）
- 保留兩個版本
- 檔名：「報告.pdf」和「報告 (衝突副本 2025-01-15).pdf」
- 讓用戶手動合併
- Google Drive、Dropbox 使用

方案 3：Operational Transformation（自動合併）
- 自動合併變更
- 複雜但體驗最好
- Google Docs 使用

Google Drive 策略：
- 二進位檔案（圖片、影片）：Conflict Copy
- 文字檔案（.txt、.md）：嘗試自動合併
- Google Docs：Operational Transformation
```

### 5. Operational Transformation（OT）

```
問題：多人同時編輯文件

場景：
文件內容：「Hello」
用戶 A：在位置 5 插入 " World" → 「Hello World」
用戶 B：在位置 0 插入 "Hi " → 「Hi Hello」

如何確保一致性？

OT 原理：
1. 每個操作有序列號
2. 伺服器接收操作後轉換（Transform）
3. 轉換考慮已執行的操作
4. 廣播轉換後的操作給所有客戶端

範例：
初始：「Hello」

A 的操作（先到達）：insert(5, " World")
伺服器執行：「Hello World」

B 的操作（後到達）：insert(0, "Hi ")
伺服器轉換：考慮 A 的操作，位置不變
伺服器執行：insert(0, "Hi ") → 「Hi Hello World」

廣播給 A：insert(0, "Hi ")
A 執行：「Hi Hello World」

最終：所有客戶端一致 → 「Hi Hello World」

關鍵：
- 操作可交換（Commutative）
- 操作可轉換（Transformable）
- 保證最終一致性
```

### 6. 權限管理

```
權限層級：

Owner（擁有者）：
- 完整控制
- 刪除檔案
- 修改權限

Editor（編輯者）：
- 編輯內容
- 分享檔案
- 無法刪除

Commenter（留言者）：
- 檢視內容
- 新增留言
- 無法編輯

Viewer（檢視者）：
- 只能檢視
- 可下載（可選）

繼承機制：
- 資料夾的權限繼承給子檔案
- 子檔案可覆寫繼承的權限

分享類型：

1. 連結分享：
   - 公開連結（任何人）
   - 知道連結的人
   - 組織內的人
   - 可設密碼保護
   - 可設過期時間

2. 用戶分享：
   - 指定 Email
   - 自動發送通知

3. 網域分享：
   - 整個組織可存取
   - 企業版功能
```

### 7. 版本控制

```
Google Drive 版本策略：

保留規則：
- 最近 30 天：保留所有版本
- 30-90 天：每天保留 1 個版本
- 90 天以上：每週保留 1 個版本
- Google Docs：永久保留所有版本

儲存優化：
- 只儲存差異（Delta）
- 不是完整副本
- 節省 > 95% 空間

版本操作：
- 檢視歷史版本
- 還原到指定版本
- 比較版本差異
- 命名重要版本

自動版本：
- 每次儲存建立版本
- Google Docs：每次編輯
- 其他檔案：每次上傳

手動版本：
- 用戶可命名版本
- 標記重要里程碑
```

## 技術棧

- **語言**: Golang（API）、Python（ML）、JavaScript（前端）
- **對象儲存**: AWS S3、Google Cloud Storage
- **CDN**: CloudFront、Cloud CDN
- **資料庫**: MySQL（元數據、分片）、Cassandra（活動日誌）
- **快取**: Redis（同步狀態、會話）、Memcached
- **訊息佇列**: Kafka（同步事件）
- **搜尋引擎**: Elasticsearch（全文搜尋）
- **協作**: WebSocket（即時通訊）
- **監控**: Prometheus + Grafana

## 架構設計

```
┌─────────────────────────────────────────────┐
│         CDN (CloudFront)                     │
│        (檔案下載加速)                         │
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
│  Upload  │  │   Sync   │        │Collaboration │
│ Service  │  │ Service  │        │   Service    │
└────┬─────┘  └────┬─────┘        └──────┬───────┘
     │             │                      │
     └─────────────┼──────────────────────┘
                   ↓
   ┌───────────────┼───────────────────────────┐
   ↓               ↓                           ↓
┌──────┐      ┌────────┐              ┌─────────────┐
│Redis │      │ Kafka  │              │Elasticsearch│
│(快取)│      │(同步)  │              │  (搜尋)     │
└──────┘      └────────┘              └─────────────┘
   ↓               ↓
┌────────────────────────────────────────┐
│ MySQL Cluster (16 shards)              │
│ - files_0 ~ files_15                   │
│ - chunks_0 ~ chunks_15                 │
│ - permissions_0 ~ permissions_15       │
└────────────────────────────────────────┘
   ↓
┌────────────────────────────────────────┐
│ Cassandra Cluster                      │
│ - activity_logs (活動日誌)             │
│ - sync_events (同步事件)               │
└────────────────────────────────────────┘
   ↓
┌────────────────────────────────────────┐
│ S3 (Object Storage)                    │
│ - chunks/ (分塊儲存，按 hash 分散)      │
│ - thumbnails/ (縮圖)                   │
└────────────────────────────────────────┘
```

## 專案結構

```
23-google-drive/
├── DESIGN.md              # 詳細設計文檔（蘇格拉底式教學）
├── README.md              # 本文件
├── cmd/
│   └── server/
│       └── main.go        # HTTP 服務器
├── internal/
│   ├── upload.go          # 分塊上傳
│   ├── sync.go            # 檔案同步
│   ├── sharing.go         # 分享與權限
│   ├── collaboration.go   # 協作編輯
│   ├── version.go         # 版本控制
│   ├── search.go          # 全文搜尋
│   └── shard.go           # 分片路由
└── docs/
    ├── api.md             # API 文檔
    ├── gdrive-case.md     # Google Drive 案例研究
    └── ot-algorithm.md    # OT 算法詳解
```

## 資料庫設計

### 核心表（MySQL）

```sql
-- 檔案/資料夾表（分片）
CREATE TABLE files (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    path TEXT,                               -- 完整路徑
    owner_id VARCHAR(64) NOT NULL,
    parent_id BIGINT,                        -- NULL = 根目錄
    is_folder BOOLEAN DEFAULT FALSE,
    size BIGINT DEFAULT 0,                   -- bytes
    mime_type VARCHAR(100),
    content_hash VARCHAR(64),                -- 整個檔案的 SHA-256
    thumbnail_url VARCHAR(1024),
    status ENUM('uploading', 'active', 'deleted') DEFAULT 'active',
    starred BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    INDEX idx_owner_parent (owner_id, parent_id, status),
    INDEX idx_path (path(255)),
    INDEX idx_content_hash (content_hash),
    INDEX idx_modified_at (modified_at DESC),
    FULLTEXT idx_name (name),
    FOREIGN KEY (parent_id) REFERENCES files(id) ON DELETE CASCADE
);

-- 分塊表（分片）
CREATE TABLE chunks (
    chunk_hash VARCHAR(64) PRIMARY KEY,      -- SHA-256
    size INT NOT NULL,
    s3_bucket VARCHAR(100),
    s3_key VARCHAR(512),
    compression VARCHAR(20),                 -- none, gzip, brotli
    ref_count INT DEFAULT 0,                 -- 參考計數
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_accessed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_ref_count (ref_count),
    INDEX idx_last_accessed (last_accessed_at)
);

-- 檔案-分塊映射表
CREATE TABLE file_chunks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    chunk_index INT NOT NULL,                -- 分塊順序
    chunk_hash VARCHAR(64) NOT NULL,
    chunk_size INT NOT NULL,
    UNIQUE KEY uk_file_chunk (file_id, chunk_index),
    INDEX idx_chunk_hash (chunk_hash),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE,
    FOREIGN KEY (chunk_hash) REFERENCES chunks(chunk_hash)
);

-- 分享連結表
CREATE TABLE shares (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    shared_by VARCHAR(64) NOT NULL,
    share_type ENUM('link', 'user', 'domain') DEFAULT 'link',
    share_token VARCHAR(64) UNIQUE,
    permission ENUM('viewer', 'commenter', 'editor') DEFAULT 'viewer',
    password_hash VARCHAR(128),
    allow_download BOOLEAN DEFAULT TRUE,
    expires_at TIMESTAMP,
    view_count INT DEFAULT 0,
    download_count INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_file_id (file_id),
    INDEX idx_share_token (share_token),
    INDEX idx_expires_at (expires_at),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);

-- 權限表
CREATE TABLE file_permissions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    user_id VARCHAR(64),                     -- NULL = 公開
    permission ENUM('viewer', 'commenter', 'editor', 'owner') DEFAULT 'viewer',
    granted_by VARCHAR(64) NOT NULL,
    inherited BOOLEAN DEFAULT FALSE,         -- 是否從父資料夾繼承
    granted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_file_user (file_id, user_id),
    INDEX idx_user_id (user_id),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);

-- 版本記錄表
CREATE TABLE file_versions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    version_number INT NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    size BIGINT NOT NULL,
    modified_by VARCHAR(64) NOT NULL,
    change_description TEXT,
    is_named_version BOOLEAN DEFAULT FALSE,  -- 用戶命名的版本
    version_name VARCHAR(100),
    keep_forever BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_file_version (file_id, version_number),
    INDEX idx_file_id (file_id, created_at DESC),
    INDEX idx_named_version (file_id, is_named_version),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);

-- 裝置同步狀態表
CREATE TABLE device_sync_state (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    device_id VARCHAR(128) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    file_id BIGINT NOT NULL,
    synced_version VARCHAR(64),              -- 已同步的 content_hash
    synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_device_file (device_id, file_id),
    INDEX idx_user_device (user_id, device_id),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);

-- 上傳會話表（分塊上傳追蹤）
CREATE TABLE upload_sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    file_id BIGINT,
    filename VARCHAR(255) NOT NULL,
    file_size BIGINT NOT NULL,
    chunk_size INT DEFAULT 4194304,          -- 4 MB
    total_chunks INT NOT NULL,
    uploaded_chunks INT DEFAULT 0,
    status ENUM('pending', 'uploading', 'completed', 'failed') DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,
    INDEX idx_user_id (user_id, created_at DESC),
    INDEX idx_status (status),
    INDEX idx_expires_at (expires_at)
);

-- 留言表
CREATE TABLE comments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    file_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    parent_id BIGINT,                        -- 回覆留言
    content TEXT NOT NULL,
    resolved BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_file_id (file_id, created_at DESC),
    INDEX idx_parent_id (parent_id),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE,
    FOREIGN KEY (parent_id) REFERENCES comments(id) ON DELETE CASCADE
);

-- 儲存配額表
CREATE TABLE storage_quotas (
    user_id VARCHAR(64) PRIMARY KEY,
    quota_bytes BIGINT NOT NULL,             -- 總配額
    used_bytes BIGINT DEFAULT 0,             -- 已使用
    plan_type VARCHAR(50),                   -- free, basic, premium, enterprise
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_plan_type (plan_type)
);
```

### 時序資料表（Cassandra）

```cql
-- 活動日誌表（Cassandra）
CREATE TABLE activity_logs (
    id UUID PRIMARY KEY,
    file_id BIGINT,
    user_id TEXT,
    action TEXT,                             -- view, download, upload, edit, delete, share, unshare, comment
    ip_address TEXT,
    user_agent TEXT,
    device_id TEXT,
    metadata TEXT,                           -- JSON
    created_at TIMESTAMP,
    INDEX (file_id, created_at),
    INDEX (user_id, created_at),
    INDEX (created_at)
);

-- 同步事件表（Cassandra）
CREATE TABLE sync_events (
    id UUID PRIMARY KEY,
    user_id TEXT,
    device_id TEXT,
    file_id BIGINT,
    event_type TEXT,                         -- file_created, file_modified, file_deleted, file_moved
    old_path TEXT,
    new_path TEXT,
    content_hash TEXT,
    created_at TIMESTAMP,
    INDEX (user_id, device_id, created_at),
    INDEX (file_id, created_at)
);

-- 即時協作會話（Cassandra）
CREATE TABLE collaboration_sessions (
    session_id UUID PRIMARY KEY,
    file_id BIGINT,
    participants SET<TEXT>,                  -- 參與者列表
    operations LIST<TEXT>,                   -- OT 操作歷史（JSON）
    started_at TIMESTAMP,
    last_activity_at TIMESTAMP,
    INDEX (file_id, started_at)
);
```

## API 文檔

### 1. 檔案上傳

#### 1.1 初始化上傳

```bash
POST /api/v1/files/upload/initiate
Authorization: Bearer {token}
Content-Type: application/json

{
  "filename": "報告.pdf",
  "file_size": 10485760,  # 10 MB
  "mime_type": "application/pdf",
  "parent_id": 123,
  "content_hash": "sha256:abc..."  # 可選，用於快速去重檢查
}

# 回應
{
  "upload_session_id": "upload-xyz",
  "file_id": 456,
  "chunk_size": 4194304,  # 4 MB
  "total_chunks": 3,
  "existing_chunks": [],  # 已存在的分塊（去重）
  "expires_at": "2025-01-15T11:00:00Z"
}
```

#### 1.2 上傳分塊

```bash
POST /api/v1/files/upload/chunk
Authorization: Bearer {token}
Content-Type: multipart/form-data

FormData:
- upload_session_id: upload-xyz
- chunk_index: 0
- chunk_hash: sha256:def...
- chunk: (binary data)

# 回應
{
  "chunk_index": 0,
  "chunk_hash": "sha256:def...",
  "status": "uploaded",
  "deduplicated": false,  # 是否去重
  "uploaded_chunks": 1,
  "total_chunks": 3
}
```

#### 1.3 完成上傳

```bash
POST /api/v1/files/upload/complete
Authorization: Bearer {token}
Content-Type: application/json

{
  "upload_session_id": "upload-xyz"
}

# 回應
{
  "file_id": 456,
  "name": "報告.pdf",
  "size": 10485760,
  "content_hash": "sha256:abc...",
  "status": "active",
  "download_url": "https://drive.google.com/file/d/456/view"
}
```

### 2. 檔案同步

#### 2.1 獲取變更

```bash
GET /api/v1/sync/changes?device_id=device123&since=2025-01-15T10:00:00Z&limit=100
Authorization: Bearer {token}

# 回應
{
  "changes": [
    {
      "file_id": 456,
      "change_type": "modified",
      "name": "報告.pdf",
      "path": "/文件/報告.pdf",
      "size": 10485760,
      "content_hash": "sha256:abc...",
      "modified_at": "2025-01-15T10:30:00Z"
    },
    {
      "file_id": 789,
      "change_type": "deleted",
      "name": "舊檔案.txt",
      "deleted_at": "2025-01-15T10:35:00Z"
    }
  ],
  "has_more": false,
  "sync_cursor": "cursor-xyz"  # 用於下次同步
}
```

#### 2.2 上傳變更

```bash
POST /api/v1/sync/upload
Authorization: Bearer {token}
Content-Type: application/json

{
  "device_id": "device123",
  "changes": [
    {
      "file_id": 456,
      "action": "modify",
      "content_hash": "sha256:new...",
      "base_version": "sha256:abc...",  # 基於哪個版本修改
      "chunk_hashes": ["sha256:c1...", "sha256:c2..."]
    }
  ]
}

# 回應
{
  "results": [
    {
      "file_id": 456,
      "status": "success",
      "conflict": false
    }
  ]
}
```

### 3. 檔案分享

#### 3.1 建立分享連結

```bash
POST /api/v1/files/{file_id}/share
Authorization: Bearer {token}
Content-Type: application/json

{
  "share_type": "link",
  "permission": "viewer",
  "password": "optional-password",  # 可選
  "expires_in": 604800  # 7 天（秒）
}

# 回應
{
  "share_id": 789,
  "share_url": "https://drive.google.com/file/d/456/view?usp=sharing",
  "share_token": "abc123xyz",
  "permission": "viewer",
  "expires_at": "2025-01-22T10:00:00Z"
}
```

#### 3.2 授予用戶權限

```bash
POST /api/v1/files/{file_id}/permissions
Authorization: Bearer {token}
Content-Type: application/json

{
  "user_email": "user@example.com",
  "permission": "editor",
  "notify": true  # 是否發送通知郵件
}

# 回應
{
  "permission_id": 101,
  "user_id": "user123",
  "permission": "editor",
  "granted_at": "2025-01-15T10:00:00Z"
}
```

#### 3.3 存取分享檔案

```bash
GET /api/v1/share/{share_token}
# 可選：Authorization: Bearer {token}（已登入用戶）

# 回應
{
  "file": {
    "id": 456,
    "name": "報告.pdf",
    "size": 10485760,
    "mime_type": "application/pdf",
    "thumbnail_url": "..."
  },
  "permission": "viewer",
  "download_url": "https://cdn.example.com/files/456/download?token=xyz",
  "requires_password": false
}
```

### 4. 協作編輯

#### 4.1 加入協作會話

```bash
POST /api/v1/collaboration/{file_id}/join
Authorization: Bearer {token}

# 回應（WebSocket 升級）
{
  "session_id": "session-xyz",
  "websocket_url": "wss://collab.drive.google.com/session-xyz",
  "document_state": {
    "content": "Hello World",
    "version": 42
  },
  "participants": [
    {"user_id": "user123", "name": "Alice"},
    {"user_id": "user456", "name": "Bob"}
  ]
}
```

#### 4.2 WebSocket 訊息格式

```json
// 客戶端 → 伺服器：編輯操作
{
  "type": "operation",
  "op": {
    "type": "insert",
    "position": 5,
    "text": " there",
    "seq_num": 43
  }
}

// 伺服器 → 客戶端：廣播操作
{
  "type": "operation",
  "op": {
    "type": "insert",
    "position": 5,
    "text": " there",
    "seq_num": 43,
    "user_id": "user123"
  }
}

// 客戶端 → 伺服器：游標位置
{
  "type": "cursor",
  "position": 10
}

// 伺服器 → 客戶端：其他用戶游標
{
  "type": "cursor",
  "user_id": "user456",
  "user_name": "Bob",
  "position": 15,
  "color": "#FF5733"
}
```

### 5. 版本控制

#### 5.1 列出版本

```bash
GET /api/v1/files/{file_id}/versions?limit=30
Authorization: Bearer {token}

# 回應
{
  "versions": [
    {
      "version_id": 10,
      "version_number": 10,
      "content_hash": "sha256:abc...",
      "size": 10485760,
      "modified_by": "user123",
      "modified_by_name": "Alice",
      "change_description": "更新第三章",
      "is_named_version": false,
      "created_at": "2025-01-15T10:00:00Z"
    },
    {
      "version_id": 9,
      "version_number": 9,
      "is_named_version": true,
      "version_name": "最終版本",
      "created_at": "2025-01-14T15:00:00Z"
    }
  ]
}
```

#### 5.2 還原版本

```bash
POST /api/v1/files/{file_id}/versions/{version_number}/restore
Authorization: Bearer {token}

# 回應
{
  "file_id": 456,
  "restored_version": 5,
  "current_version": 11,  # 還原操作建立新版本
  "content_hash": "sha256:old..."
}
```

#### 5.3 命名版本

```bash
POST /api/v1/files/{file_id}/versions/{version_number}/name
Authorization: Bearer {token}
Content-Type: application/json

{
  "version_name": "最終版本",
  "keep_forever": true
}

# 回應
{
  "version_id": 9,
  "version_name": "最終版本",
  "keep_forever": true
}
```

### 6. 搜尋

```bash
GET /api/v1/search?q=報告&type=file,folder&owner=me&limit=20
Authorization: Bearer {token}

# 回應
{
  "results": [
    {
      "file_id": 456,
      "name": "2025 年度報告.pdf",
      "path": "/文件/2025 年度報告.pdf",
      "mime_type": "application/pdf",
      "size": 10485760,
      "modified_at": "2025-01-15T10:00:00Z",
      "thumbnail_url": "...",
      "match_score": 0.95,
      "highlight": "2025 年度<em>報告</em>..."
    }
  ],
  "total": 50,
  "has_more": true
}
```

## 性能指標

### 系統容量

```
用戶規模：10 億月活躍用戶

資料量：
- 儲存總量：> 10 EB
- 檔案數：> 2 兆個
- 每日上傳：> 100 TB

QPS：
- 檔案上傳：50,000 次/秒
- 檔案下載：500,000 次/秒
- 同步請求：200,000 次/秒
- 搜尋：100,000 次/秒

延遲：
- 檔案列表：P50 < 50ms, P99 < 200ms
- 分塊上傳：P50 < 100ms, P99 < 300ms
- 同步檢查：P50 < 100ms, P99 < 250ms
- 搜尋：P50 < 100ms, P99 < 300ms

同步效能：
- Delta Sync 節省頻寬：95%
- 去重節省儲存：90%
- 平均同步時間：< 5 秒

儲存：
- 原始容量：10 EB
- 去重後：1 EB（節省 90%）
- 分塊大小：4 MB
- 總分塊數：> 250 億個
```

## 成本估算

### 場景：10 億月活躍用戶

```
假設：
- 每人平均儲存：15 GB
- 每人每天同步：500 MB
- 付費轉換率：5%（5000 萬付費用戶）

收入：
- 免費用戶：15 GB 免費
- 付費用戶：100 GB @ $1.99/月
- 收入：5000 萬 × $1.99 = $99.5M/月

成本：

1. 儲存（S3）：
   原始容量：10 億 × 15 GB = 15 EB
   去重後：15 EB × 10% = 1.5 EB
   成本：1.5 EB × $0.023/GB = $34.5M/月

2. 頻寬：
   每日同步：10 億 × 500 MB = 500 PB/天
   Delta Sync 後：500 PB × 5% = 25 PB/天
   每月：750 PB
   成本：750 PB × $0.09/GB = $67.5M/月（出站）

3. 資料庫：
   MySQL（分片）：$500,000/月
   Cassandra：$300,000/月
   Redis：$100,000/月

4. API 伺服器：
   2000 台 × $500/月 = $1M/月

5. CDN：
   檔案下載加速：$5M/月

總成本：約 $108.9M/月

毛利率：-9.4%（虧損）

備註：
- Google Drive 透過 Google One 訂閱（含 Gmail、Photos）
- 實際毛利來自整個生態系統
- 儲存成本持續下降（每年 -20%）
```

### 成本優化策略

```
1. 去重優化：

   當前：分塊去重（90%）
   優化：
   - 跨用戶去重（95%）
   - 壓縮（gzip）：額外節省 30%
   - 節省：$10M/月

2. Delta Sync 優化：

   當前：節省 95% 頻寬
   優化：
   - Content-Defined Chunking 優化
   - 減少不必要的同步檢查
   - 節省：$5M/月

3. 冷資料遷移：

   方案：
   - 6 個月未存取 → S3 Infrequent Access（節省 50%）
   - 1 年未存取 → Glacier（節省 83%）
   - 節省：$15M/月

4. CDN 優化：

   方案：
   - 熱門檔案：CDN
   - 冷門檔案：S3 直連
   - 節省：$2M/月

優化後總成本：約 $76.9M/月
毛利率：22.7%
節省：$32M/月（29%）
```

## 關鍵設計決策

### Q1: 為什麼使用分塊儲存而非整個檔案？

```
對比：

整個檔案儲存：
優勢：✅ 簡單、✅ 下載快
劣勢：❌ 大檔案上傳超時、❌ 無法去重、❌ Delta Sync 困難

分塊儲存：
優勢：✅ 支援大檔案、✅ 斷點續傳、✅ 去重、✅ Delta Sync
劣勢：❌ 複雜、❌ 下載需拼接

Google Drive 的選擇：分塊儲存（4 MB/塊）
原因：
1. 支援任意大小檔案
2. 去重節省 90% 儲存
3. Delta Sync 節省 95% 頻寬
4. 成本大幅降低

結論：對於雲端儲存，分塊是必需的。
```

### Q2: 衝突如何處理？

```
場景：兩個裝置離線修改同一檔案

方案對比：

Last Write Wins：
優勢：✅ 簡單
劣勢：❌ 可能丟失資料
適用：非關鍵檔案

Conflict Copy：
優勢：✅ 不丟失資料、✅ 用戶自主決定
劣勢：❌ 需要手動合併
適用：一般檔案

Operational Transformation：
優勢：✅ 自動合併、✅ 體驗最好
劣勢：❌ 複雜、❌ 只適用結構化資料
適用：Google Docs

Google Drive 策略：
- 二進位檔案：Conflict Copy
- 結構化文件：OT
- 讓用戶選擇保留哪個版本

結論：根據檔案類型選擇策略。
```

### Q3: 為什麼需要版本控制？

```
需求：
1. 誤刪恢復
2. 查看變更歷史
3. 團隊協作（誰改了什麼）
4. 合規要求（審計）

儲存優化：
- 不儲存完整副本
- 只儲存差異（Delta）
- 使用分塊去重
- 節省 > 95% 空間

範例：
100 MB 檔案，100 個版本
- 完整儲存：10 GB
- Delta 儲存：200 MB（節省 98%）

保留策略：
- 最近 30 天：所有版本
- 30-90 天：每日 1 個
- 90 天以上：每週 1 個
- 重要版本：永久保留

結論：版本控制是必需的，但需優化儲存。
```

### Q4: Delta Sync 如何實作？

```
問題：
- 修改 10 MB 檔案的 1 個字
- 如何只傳輸變更部分？

方案：Content-Defined Chunking（CDC）

原理：
1. 使用 Rolling Hash（Rabin Fingerprinting）
2. 根據內容特徵切分（而非固定位置）
3. 檔案開頭插入內容不會影響所有分塊

範例：
原始檔案：「Hello World」
分塊（假設）：["Hello ", "World"]

修改：在開頭插入「Hi 」
新檔案：「Hi Hello World」
新分塊：["Hi ", "Hello ", "World"]

變化：
- 固定位置切分：所有分塊都變了
- CDC 切分：只有第一個分塊變了

上傳：只上傳新分塊「Hi 」
節省：> 90% 頻寬

實際效果：
- Dropbox：節省 95% 頻寬
- Google Drive：節省 95% 頻寬
- 同步速度提升 20 倍

結論：CDC 是 Delta Sync 的關鍵技術。
```

### Q5: 如何實現即時協作？

```
挑戰：
- 多人同時編輯
- 操作順序不同
- 需要保證一致性

技術：Operational Transformation（OT）

核心概念：
1. 操作（Operation）：insert, delete, format
2. 轉換（Transform）：調整操作使其可交換
3. 序列號：保證操作順序

範例：
初始：「Hello」

操作 A：insert(5, " World") → 「Hello World」
操作 B：insert(0, "Hi ") → 「Hi Hello」

如果同時發生：
- 伺服器先執行 A：「Hello World」
- 伺服器轉換 B：insert(0, "Hi ") → 「Hi Hello World」

結果：所有客戶端一致

Google Docs 實作：
- WebSocket 即時通訊
- 操作壓縮（合併連續操作）
- 衝突檢測和解決
- 延遲 < 100ms

替代方案：CRDT（Conflict-free Replicated Data Type）
- 更簡單
- 不需要中央伺服器
- 但檔案大小會膨脹

結論：OT 是成熟的協作編輯方案。
```

## 常見問題

### Q1: 如何防止儲存濫用？

```
問題：
- 免費用戶上傳大量檔案
- 惡意用戶上傳垃圾檔案

防範措施：

1. 配額限制：
   - 免費：15 GB
   - 付費：100 GB / 2 TB / 30 TB
   - 超過配額：無法上傳

2. 上傳速率限制：
   - 每小時最多 750 MB
   - 每天最多 10 GB
   - 防止腳本濫用

3. 檔案類型限制：
   - 禁止：惡意軟體、病毒
   - 掃描：上傳時病毒掃描
   - 移除：違規檔案

4. 去重檢測：
   - 上傳前檢查 hash
   - 相同檔案不占用配額
   - 但計入用戶檔案列表

5. 垃圾檔案清理：
   - 回收桶保留 30 天
   - 自動清理過期檔案
   - 釋放儲存空間

監控：
- 追蹤異常上傳模式
- 標記可疑帳號
- 人工審核

結論：多層防護確保公平使用。
```

### Q2: 大檔案（100 GB）如何處理？

```
挑戰：
- 上傳時間長（數小時）
- 網路容易中斷
- 伺服器記憶體壓力

解決方案：

1. 分塊上傳：
   - 分塊大小：4 MB
   - 總分塊數：100 GB / 4 MB = 25,600 塊
   - 平行上傳：同時上傳 4 個分塊

2. 斷點續傳：
   - 記錄已上傳分塊
   - 重連後只上傳缺少的分塊
   - 不需要重新開始

3. 上傳優化：
   - 壓縮：gzip 壓縮（節省 30%）
   - 去重：檢查已存在分塊
   - 預計算 hash：客戶端計算後上傳

4. 進度追蹤：
   - 即時進度條
   - 預估剩餘時間
   - 背景上傳（可關閉視窗）

5. 伺服器端：
   - 串流處理（不載入整個檔案到記憶體）
   - 直接寫入 S3
   - 分散式處理

範例：
上傳 100 GB 影片
- 分塊：25,600 塊
- 平行上傳 4 塊
- 每塊 100 KB/s
- 總速度：400 KB/s
- 總時間：約 70 小時

優化後：
- 去重：50% 已存在 → 35 小時
- 壓縮：30% → 25 小時
- 更快網路：10 Mbps → 2.5 小時

結論：分塊 + 優化可處理超大檔案。
```

### Q3: 如何確保資料安全？

```
安全措施：

1. 傳輸加密：
   - HTTPS（TLS 1.3）
   - 端到端加密（可選）

2. 儲存加密：
   - S3 伺服器端加密（AES-256）
   - Google 管理金鑰或客戶管理金鑰

3. 存取控制：
   - OAuth 2.0 認證
   - 權限管理（Owner、Editor、Viewer）
   - 分享連結可設密碼

4. 審計日誌：
   - 記錄所有存取
   - IP、裝置、時間戳
   - 可疑活動告警

5. 備份：
   - 多地域複製（3 份）
   - 版本控制（防誤刪）
   - 災難恢復計畫

6. 合規：
   - GDPR（歐盟資料保護）
   - HIPAA（醫療資料）
   - SOC 2 Type II 認證

7. 病毒掃描：
   - 上傳時掃描
   - 定期重新掃描
   - 隔離可疑檔案

8. 兩步驟驗證：
   - 強制企業帳號
   - 建議個人帳號

結論：多層安全保護資料。
```

### Q4: 如何實現離線模式？

```
需求：
- 無網路時存取檔案
- 離線編輯
- 上線後自動同步

實作：

1. 本地快取：
   - SQLite 儲存元數據
   - 檔案儲存在本地資料夾
   - LRU 淘汰舊檔案

2. 離線標記：
   - 用戶選擇「離線可用」
   - 下載完整檔案到本地
   - 優先級下載

3. 衝突處理：
   - 離線編輯記錄變更
   - 上線時檢測衝突
   - 建立衝突副本或自動合併

4. 同步佇列：
   - 記錄離線期間的變更
   - 上線後依序同步
   - 失敗重試（指數退避）

5. 儲存管理：
   - 限制離線檔案大小
   - 自動清理長期未用檔案
   - 用戶可手動管理

6. 協作限制：
   - 離線時無法協作編輯
   - 顯示警告訊息
   - 上線後才能看到其他人變更

資料結構：
```sql
CREATE TABLE offline_files (
    file_id BIGINT PRIMARY KEY,
    local_path TEXT,
    content_hash TEXT,
    last_synced_at TIMESTAMP,
    priority INT  -- 下載優先級
);

CREATE TABLE pending_changes (
    id INTEGER PRIMARY KEY,
    file_id BIGINT,
    change_type TEXT,  -- create, modify, delete
    content_hash TEXT,
    timestamp TIMESTAMP
);
```

結論：離線模式需要精心設計的同步機制。
```

### Q5: 如何監控系統健康？

```
關鍵指標：

1. 業務指標：
   - DAU/MAU
   - 儲存成長率
   - 同步成功率
   - 分享使用率

2. 技術指標：
   - 上傳成功率 > 99%
   - 下載成功率 > 99.9%
   - 同步延遲：P99 < 5 秒
   - API 延遲：P50/P99

3. 儲存指標：
   - 去重率 > 85%
   - 儲存利用率
   - 分塊分佈

4. 成本指標：
   - 儲存成本/GB
   - 頻寬成本/GB
   - 每用戶成本

5. 錯誤率：
   - 上傳失敗率 < 1%
   - 同步失敗率 < 0.5%
   - 衝突率 < 0.1%

監控工具：
- Prometheus + Grafana（指標）
- ELK Stack（日誌）
- Sentry（錯誤追蹤）
- Custom Dashboard（業務指標）

告警：
- 上傳失敗率 > 2% → P0
- 同步延遲 > 10 秒 → P1
- 儲存成本異常 → P2

A/B 測試：
- 測試新同步算法
- 測試 UI 改進
- 數據驅動決策

結論：全方位監控確保服務品質。
```

## 延伸閱讀

### 真實案例

- **Dropbox Tech Blog**: [Dropbox Engineering](https://dropbox.tech/)
- **Google Drive**: [How Google Drive Works](https://www.google.com/drive/about.html)
- **Operational Transformation**: [OT Explained](https://operational-transformation.github.io/)
- **Content-Defined Chunking**: [CDC in Dropbox](https://dropbox.tech/infrastructure/streaming-file-synchronization)

### 技術文檔

- **Rabin Fingerprinting**: [CDC Algorithm](https://en.wikipedia.org/wiki/Rabin_fingerprint)
- **WebSocket**: [Real-time Communication](https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API)
- **Elasticsearch**: [Full-Text Search](https://www.elastic.co/elasticsearch/)

### 相關章節

- **21-netflix**: 內容分發（CDN）
- **05-distributed-cache**: Redis 快取
- **12-distributed-kv-store**: 分散式儲存
- **16-chat-system**: WebSocket 即時通訊

## 總結

從「簡單上傳」到「完整的雲端儲存平台」，我們學到了：

1. **分塊上傳**：支援大檔案、斷點續傳、去重（節省 90% 空間）
2. **Delta Sync**：只傳輸變更（節省 95% 頻寬）
3. **檔案分享**：連結、權限、密碼保護
4. **協作編輯**：Operational Transformation、即時同步
5. **版本控制**：歷史記錄、還原、差異儲存
6. **衝突解決**：Conflict Copy、自動合併

**記住：效能、可靠性、成本，三者需要平衡！**

**Google Drive 的啟示**：
- 10 億月活躍用戶
- 去重節省 > 90% 儲存成本
- Delta Sync 節省 > 95% 頻寬
- Operational Transformation 實現即時協作
- 版本控制保護用戶資料
- 多層安全保障

**核心理念：Synced, shared, collaborative.（同步、分享、協作）**

---

**下一步**：
- 實作 Content-Defined Chunking
- 搭建協作編輯系統
- 優化同步效能
- 探索 CRDT 技術
