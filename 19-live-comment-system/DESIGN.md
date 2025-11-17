# Chapter 19: Live Comment System - 直播彈幕系統

> 從 HTTP 輪詢到 WebSocket：打造高並發的直播彈幕系統

## 本章概述

這是一個關於**直播彈幕系統（Live Comment System）**設計的完整指南，使用**蘇格拉底式教學法**（Socratic Method）。你將跟隨 Emma（產品經理）、David（架構師）、Sarah（後端工程師）、Michael（運維工程師）和 Jennifer（前端工程師）一起，從零開始設計一個生產級的直播彈幕系統。

## 學習目標

- 理解**實時通訊**的演進（Polling → Long Polling → WebSocket）
- 掌握 **WebSocket 全雙工通訊**
- 學習**彈幕高並發**處理（10 萬+ 並發用戶）
- 實踐**限流策略**（防止彈幕刷屏）
- 了解**敏感詞過濾**和**人工審核**
- 掌握**彈幕存儲**和**回放**
- 學習 **Redis Pub/Sub 橫向擴展**
- 理解**彈幕排名**和**熱度算法**
- 掌握**性能優化**和**降級策略**
- 學習 Bilibili、Twitch 的真實案例

## 角色介紹

- **Emma**：產品經理，負責定義彈幕系統的產品需求
- **David**：資深架構師，擅長設計高並發系統
- **Sarah**：後端工程師，實現核心彈幕邏輯
- **Michael**：運維工程師，關注系統穩定性和性能
- **Jennifer**：前端工程師，負責彈幕展示效果

---

## Act 1: 從 HTTP 輪詢開始

**場景：產品需求會議**

**Emma**（產品經理）在白板上畫出直播彈幕的核心功能：

```
核心功能：
1. 用戶發送彈幕
2. 其他觀眾實時看到彈幕
3. 彈幕飄過屏幕
```

**Emma**: "我們要做一個直播彈幕系統，就像 Bilibili 那樣。David，最簡單的實現是什麼？"

**David**（架構師）思考片刻：

**David**: "最簡單的方式是 HTTP 輪詢：客戶端每隔 1 秒請求一次新彈幕。"

### 方案 1：HTTP 輪詢

```go
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "net/http"
    "time"
)

// Comment - 彈幕結構
type Comment struct {
    ID        int64     `json:"id"`
    RoomID    string    `json:"room_id"`
    UserID    string    `json:"user_id"`
    Username  string    `json:"username"`
    Content   string    `json:"content"`
    CreatedAt time.Time `json:"created_at"`
}

// PollingCommentService - 輪詢彈幕服務
type PollingCommentService struct {
    db *sql.DB
}

// SendComment - 發送彈幕
func (s *PollingCommentService) SendComment(w http.ResponseWriter, r *http.Request) {
    var req struct {
        RoomID   string `json:"room_id"`
        UserID   string `json:"user_id"`
        Username string `json:"username"`
        Content  string `json:"content"`
    }

    json.NewDecoder(r.Body).Decode(&req)

    // 插入數據庫
    query := `INSERT INTO comments (room_id, user_id, username, content, created_at) VALUES (?, ?, ?, ?, ?)`
    result, err := s.db.Exec(query, req.RoomID, req.UserID, req.Username, req.Content, time.Now())
    if err != nil {
        http.Error(w, "Failed to send comment", http.StatusInternalServerError)
        return
    }

    commentID, _ := result.LastInsertId()
    json.NewEncoder(w).Encode(map[string]interface{}{
        "comment_id": commentID,
    })
}

// GetComments - 獲取最新彈幕（輪詢）
func (s *PollingCommentService) GetComments(w http.ResponseWriter, r *http.Request) {
    roomID := r.URL.Query().Get("room_id")
    sinceID := r.URL.Query().Get("since_id") // 客戶端記錄的最後一條彈幕 ID

    // 查詢新彈幕
    query := `
        SELECT id, room_id, user_id, username, content, created_at
        FROM comments
        WHERE room_id = ? AND id > ?
        ORDER BY id ASC
        LIMIT 100
    `

    rows, err := s.db.Query(query, roomID, sinceID)
    if err != nil {
        http.Error(w, "Failed to get comments", http.StatusInternalServerError)
        return
    }
    defer rows.Close()

    var comments []Comment
    for rows.Next() {
        var c Comment
        rows.Scan(&c.ID, &c.RoomID, &c.UserID, &c.Username, &c.Content, &c.CreatedAt)
        comments = append(comments, c)
    }

    json.NewEncoder(w).Encode(comments)
}
```

**前端代碼（每秒輪詢）**：

```javascript
let lastCommentID = 0;

setInterval(async () => {
    const res = await fetch(`/comments?room_id=room123&since_id=${lastCommentID}`);
    const comments = await res.json();

    comments.forEach(comment => {
        displayComment(comment); // 顯示彈幕
        lastCommentID = Math.max(lastCommentID, comment.id);
    });
}, 1000); // 每 1 秒輪詢一次
```

**Michael**（運維工程師）皺眉：

**Michael**: "這個方案有幾個問題：
1. **延遲高**：最壞情況延遲 1 秒
2. **資源浪費**：90% 的請求都是空響應（沒有新彈幕）
3. **數據庫壓力**：10 萬人觀看 = 10 萬次/秒查詢"

**David**: "你說得對。我們需要 **Long Polling**（長輪詢）。"

---

## Act 2: Long Polling 改進

### 方案 2：Long Polling

```go
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "net/http"
    "time"
)

// LongPollingCommentService - 長輪詢彈幕服務
type LongPollingCommentService struct {
    db *sql.DB
}

// GetComments - 獲取彈幕（長輪詢）
func (s *LongPollingCommentService) GetComments(w http.ResponseWriter, r *http.Request) {
    roomID := r.URL.Query().Get("room_id")
    sinceID := r.URL.Query().Get("since_id")

    ctx := r.Context()
    timeout := time.After(30 * time.Second) // 最多等待 30 秒

    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-timeout:
            // 超時，返回空結果
            json.NewEncoder(w).Encode([]Comment{})
            return
        case <-ticker.C:
            // 每 100ms 檢查一次是否有新彈幕
            query := `
                SELECT id, room_id, user_id, username, content, created_at
                FROM comments
                WHERE room_id = ? AND id > ?
                ORDER BY id ASC
                LIMIT 100
            `

            rows, err := s.db.Query(query, roomID, sinceID)
            if err != nil {
                continue
            }

            var comments []Comment
            for rows.Next() {
                var c Comment
                rows.Scan(&c.ID, &c.RoomID, &c.UserID, &c.Username, &c.Content, &c.CreatedAt)
                comments = append(comments, c)
            }
            rows.Close()

            if len(comments) > 0 {
                // 有新彈幕，立即返回
                json.NewEncoder(w).Encode(comments)
                return
            }
        }
    }
}
```

**優勢**：
- ✅ 延遲低（有新彈幕立即返回）
- ✅ 資源利用率高（減少空請求）

**劣勢**：
- ❌ 仍需頻繁輪詢數據庫（每 100ms）
- ❌ 服務器需維持大量連接
- ❌ 單向通信（只能服務器推送）

**Sarah**: "Long Polling 改善了延遲，但仍然不夠實時。能不能用 WebSocket？"

**David**: "沒錯！WebSocket 是最佳方案。"

---

## Act 3: WebSocket 實時通訊

### 方案 3：WebSocket

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"
    "sync"

    "github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true // 允許跨域
    },
}

// Room - 直播間
type Room struct {
    ID      string
    clients map[*Client]bool
    mu      sync.RWMutex
}

// Client - 客戶端連接
type Client struct {
    conn   *websocket.Conn
    userID string
    username string
    room   *Room
}

// WebSocketCommentService - WebSocket 彈幕服務
type WebSocketCommentService struct {
    rooms map[string]*Room
    mu    sync.RWMutex
}

func NewWebSocketCommentService() *WebSocketCommentService {
    return &WebSocketCommentService{
        rooms: make(map[string]*Room),
    }
}

// GetOrCreateRoom - 獲取或創建直播間
func (s *WebSocketCommentService) GetOrCreateRoom(roomID string) *Room {
    s.mu.Lock()
    defer s.mu.Unlock()

    if room, ok := s.rooms[roomID]; ok {
        return room
    }

    room := &Room{
        ID:      roomID,
        clients: make(map[*Client]bool),
    }
    s.rooms[roomID] = room
    return room
}

// HandleWebSocket - 處理 WebSocket 連接
func (s *WebSocketCommentService) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Upgrade error:", err)
        return
    }

    roomID := r.URL.Query().Get("room_id")
    userID := r.URL.Query().Get("user_id")
    username := r.URL.Query().Get("username")

    room := s.GetOrCreateRoom(roomID)
    client := &Client{
        conn:     conn,
        userID:   userID,
        username: username,
        room:     room,
    }

    // 加入房間
    room.AddClient(client)
    defer room.RemoveClient(client)

    // 讀取客戶端消息
    for {
        var msg map[string]interface{}
        err := conn.ReadJSON(&msg)
        if err != nil {
            break
        }

        // 廣播彈幕給房間所有人
        comment := Comment{
            RoomID:    roomID,
            UserID:    userID,
            Username:  username,
            Content:   msg["content"].(string),
            CreatedAt: time.Now(),
        }

        room.Broadcast(comment)
    }
}

// AddClient - 添加客戶端
func (r *Room) AddClient(client *Client) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.clients[client] = true
}

// RemoveClient - 移除客戶端
func (r *Room) RemoveClient(client *Client) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.clients, client)
    client.conn.Close()
}

// Broadcast - 廣播彈幕
func (r *Room) Broadcast(comment Comment) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    for client := range r.clients {
        err := client.conn.WriteJSON(comment)
        if err != nil {
            log.Printf("Write error: %v", err)
        }
    }
}
```

**前端代碼（WebSocket）**：

```javascript
const ws = new WebSocket('ws://localhost:8080/ws?room_id=room123&user_id=alice&username=Alice');

// 接收彈幕
ws.onmessage = (event) => {
    const comment = JSON.parse(event.data);
    displayComment(comment);
};

// 發送彈幕
function sendComment(content) {
    ws.send(JSON.stringify({
        content: content
    }));
}
```

**優勢**：
- ✅ 延遲極低（< 50ms）
- ✅ 全雙工通訊
- ✅ 節省資源（單一連接）

**Emma**: "太好了！現在彈幕是實時的了。但如果 10 萬人同時在線，系統能撐住嗎？"

---

## Act 4: 高並發優化

**Michael**: "10 萬並發連接會帶來幾個問題：
1. **內存占用**：10 萬個 WebSocket 連接
2. **CPU 壓力**：每條彈幕廣播 10 萬次
3. **網絡帶寬**：10 萬 × 每條彈幕大小"

**David**: "我們需要幾個優化策略。"

### 優化 1：批量廣播

```go
package main

import (
    "sync"
    "time"
)

// BatchBroadcastRoom - 批量廣播的房間
type BatchBroadcastRoom struct {
    ID      string
    clients map[*Client]bool
    mu      sync.RWMutex
    buffer  []Comment
    bufferMu sync.Mutex
}

func NewBatchBroadcastRoom(roomID string) *BatchBroadcastRoom {
    room := &BatchBroadcastRoom{
        ID:      roomID,
        clients: make(map[*Client]bool),
        buffer:  make([]Comment, 0, 100),
    }

    // 啟動批量廣播 goroutine（每 100ms 一次）
    go room.flushBuffer()

    return room
}

// AddComment - 添加彈幕到緩衝區
func (r *BatchBroadcastRoom) AddComment(comment Comment) {
    r.bufferMu.Lock()
    defer r.bufferMu.Unlock()
    r.buffer = append(r.buffer, comment)
}

// flushBuffer - 定時刷新緩衝區
func (r *BatchBroadcastRoom) flushBuffer() {
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for range ticker.C {
        r.bufferMu.Lock()
        if len(r.buffer) == 0 {
            r.bufferMu.Unlock()
            continue
        }

        // 取出所有彈幕
        comments := make([]Comment, len(r.buffer))
        copy(comments, r.buffer)
        r.buffer = r.buffer[:0] // 清空緩衝區
        r.bufferMu.Unlock()

        // 批量廣播
        r.mu.RLock()
        for client := range r.clients {
            client.conn.WriteJSON(comments) // 發送多條彈幕
        }
        r.mu.RUnlock()
    }
}
```

**優勢**：
- ✅ 減少廣播次數（100 條彈幕 → 1 次廣播）
- ✅ 降低 CPU 壓力
- ✅ 提高吞吐量

### 優化 2：Goroutine Pool

```go
package main

import (
    "sync"
)

// WorkerPool - Goroutine 池
type WorkerPool struct {
    workers   int
    taskQueue chan func()
    wg        sync.WaitGroup
}

func NewWorkerPool(workers int) *WorkerPool {
    pool := &WorkerPool{
        workers:   workers,
        taskQueue: make(chan func(), 10000),
    }

    for i := 0; i < workers; i++ {
        pool.wg.Add(1)
        go pool.worker()
    }

    return pool
}

func (p *WorkerPool) worker() {
    defer p.wg.Done()
    for task := range p.taskQueue {
        task()
    }
}

func (p *WorkerPool) Submit(task func()) {
    p.taskQueue <- task
}

// 使用示例
var broadcastPool = NewWorkerPool(100)

func (r *Room) BroadcastAsync(comment Comment) {
    r.mu.RLock()
    clients := make([]*Client, 0, len(r.clients))
    for client := range r.clients {
        clients = append(clients, client)
    }
    r.mu.RUnlock()

    // 提交到線程池
    for _, client := range clients {
        c := client
        broadcastPool.Submit(func() {
            c.conn.WriteJSON(comment)
        })
    }
}
```

---

## Act 5: 彈幕限流

**Emma**: "我們需要限流，防止用戶刷屏。比如每個用戶每秒最多發送 1 條彈幕。"

### 實現：Redis 限流

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/go-redis/redis/v8"
)

// RateLimiter - 限流器
type RateLimiter struct {
    redis *redis.Client
}

// AllowComment - 檢查是否允許發送彈幕
func (r *RateLimiter) AllowComment(ctx context.Context, userID string) (bool, error) {
    key := fmt.Sprintf("rate_limit:comment:%s", userID)

    // 使用 Lua 腳本實現原子操作
    script := `
        local current = redis.call('INCR', KEYS[1])
        if current == 1 then
            redis.call('EXPIRE', KEYS[1], 1)
        end
        return current
    `

    result, err := r.redis.Eval(ctx, script, []string{key}).Int()
    if err != nil {
        return false, err
    }

    return result <= 1, nil // 每秒最多 1 條
}

// 在 WebSocket handler 中使用
func (s *WebSocketCommentService) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    // ... (前面的代碼)

    limiter := &RateLimiter{redis: redisClient}

    for {
        var msg map[string]interface{}
        err := conn.ReadJSON(&msg)
        if err != nil {
            break
        }

        // 限流檢查
        allowed, _ := limiter.AllowComment(context.Background(), userID)
        if !allowed {
            conn.WriteJSON(map[string]string{
                "error": "Too many comments, please slow down",
            })
            continue
        }

        // 處理彈幕
        comment := Comment{
            RoomID:    roomID,
            UserID:    userID,
            Username:  username,
            Content:   msg["content"].(string),
            CreatedAt: time.Now(),
        }

        room.Broadcast(comment)
    }
}
```

### 分級限流

```go
// 普通用戶：1 條/秒
// VIP 用戶：5 條/秒
// 房主：10 條/秒

func (r *RateLimiter) AllowComment(ctx context.Context, userID string, userLevel string) (bool, error) {
    limits := map[string]int{
        "normal": 1,
        "vip":    5,
        "owner":  10,
    }

    limit := limits[userLevel]
    key := fmt.Sprintf("rate_limit:comment:%s", userID)

    script := `
        local current = redis.call('INCR', KEYS[1])
        if current == 1 then
            redis.call('EXPIRE', KEYS[1], 1)
        end
        return current
    `

    result, err := r.redis.Eval(ctx, script, []string{key}).Int()
    if err != nil {
        return false, err
    }

    return result <= limit, nil
}
```

---

## Act 6: 敏感詞過濾和審核

**Emma**: "我們需要過濾敏感詞，避免違規內容。"

### 實現：敏感詞過濾（Trie 樹）

```go
package main

import (
    "strings"
    "sync"
)

// TrieNode - Trie 樹節點
type TrieNode struct {
    children map[rune]*TrieNode
    isEnd    bool
}

// SensitiveWordFilter - 敏感詞過濾器
type SensitiveWordFilter struct {
    root *TrieNode
    mu   sync.RWMutex
}

func NewSensitiveWordFilter() *SensitiveWordFilter {
    return &SensitiveWordFilter{
        root: &TrieNode{
            children: make(map[rune]*TrieNode),
        },
    }
}

// AddWord - 添加敏感詞
func (f *SensitiveWordFilter) AddWord(word string) {
    f.mu.Lock()
    defer f.mu.Unlock()

    node := f.root
    for _, char := range word {
        if _, ok := node.children[char]; !ok {
            node.children[char] = &TrieNode{
                children: make(map[rune]*TrieNode),
            }
        }
        node = node.children[char]
    }
    node.isEnd = true
}

// Filter - 過濾敏感詞（替換為 ***）
func (f *SensitiveWordFilter) Filter(text string) string {
    f.mu.RLock()
    defer f.mu.RUnlock()

    runes := []rune(text)
    result := make([]rune, len(runes))
    copy(result, runes)

    for i := 0; i < len(runes); i++ {
        node := f.root
        j := i

        for j < len(runes) {
            if child, ok := node.children[runes[j]]; ok {
                node = child
                j++

                if node.isEnd {
                    // 找到敏感詞，替換為 ***
                    for k := i; k < j; k++ {
                        result[k] = '*'
                    }
                    i = j - 1
                    break
                }
            } else {
                break
            }
        }
    }

    return string(result)
}

// ContainsSensitiveWord - 檢查是否包含敏感詞
func (f *SensitiveWordFilter) ContainsSensitiveWord(text string) bool {
    f.mu.RLock()
    defer f.mu.RUnlock()

    runes := []rune(text)

    for i := 0; i < len(runes); i++ {
        node := f.root
        j := i

        for j < len(runes) {
            if child, ok := node.children[runes[j]]; ok {
                node = child
                j++

                if node.isEnd {
                    return true
                }
            } else {
                break
            }
        }
    }

    return false
}

// 使用示例
var sensitiveFilter = NewSensitiveWordFilter()

func init() {
    // 加載敏感詞庫
    sensitiveFilter.AddWord("暴力")
    sensitiveFilter.AddWord("色情")
    // ... 更多敏感詞
}

func (s *WebSocketCommentService) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    // ... (前面的代碼)

    for {
        var msg map[string]interface{}
        conn.ReadJSON(&msg)

        content := msg["content"].(string)

        // 敏感詞過濾
        filteredContent := sensitiveFilter.Filter(content)

        comment := Comment{
            RoomID:    roomID,
            UserID:    userID,
            Username:  username,
            Content:   filteredContent,
            CreatedAt: time.Now(),
        }

        room.Broadcast(comment)
    }
}
```

### 人工審核隊列

```go
// 包含敏感詞的彈幕進入審核隊列
func (s *WebSocketCommentService) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    // ...

    for {
        var msg map[string]interface{}
        conn.ReadJSON(&msg)

        content := msg["content"].(string)

        if sensitiveFilter.ContainsSensitiveWord(content) {
            // 進入審核隊列
            s.kafka.Publish("comment.review", map[string]interface{}{
                "room_id":  roomID,
                "user_id":  userID,
                "content":  content,
                "created_at": time.Now(),
            })

            // 告知用戶彈幕審核中
            conn.WriteJSON(map[string]string{
                "status": "pending_review",
            })
            continue
        }

        // 正常發送
        comment := Comment{
            RoomID:    roomID,
            UserID:    userID,
            Username:  username,
            Content:   content,
            CreatedAt: time.Now(),
        }

        room.Broadcast(comment)
    }
}
```

---

## Act 7: 彈幕存儲和回放

**Emma**: "用戶需要能夠回放直播，看到之前的彈幕。"

### 數據庫設計

```sql
CREATE TABLE comments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    username VARCHAR(100),
    content TEXT NOT NULL,
    timestamp INT,                           -- 相對於直播開始的秒數
    status ENUM('normal', 'deleted', 'reviewed') DEFAULT 'normal',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_room_timestamp (room_id, timestamp),
    INDEX idx_created_at (created_at DESC)
);

CREATE TABLE live_sessions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    duration INT,                            -- 直播時長（秒）
    INDEX idx_room_id (room_id, start_time DESC)
);
```

### 實現：彈幕回放

```go
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "net/http"
)

// ReplayService - 回放服務
type ReplayService struct {
    db *sql.DB
}

// GetReplayComments - 獲取回放彈幕
func (s *ReplayService) GetReplayComments(w http.ResponseWriter, r *http.Request) {
    roomID := r.URL.Query().Get("room_id")
    sessionID := r.URL.Query().Get("session_id")
    startTime := r.URL.Query().Get("start_time") // 回放的起始時間（秒）
    endTime := r.URL.Query().Get("end_time")     // 回放的結束時間（秒）

    query := `
        SELECT id, user_id, username, content, timestamp
        FROM comments
        WHERE room_id = ?
          AND session_id = ?
          AND timestamp >= ?
          AND timestamp <= ?
        ORDER BY timestamp ASC
    `

    rows, err := s.db.Query(query, roomID, sessionID, startTime, endTime)
    if err != nil {
        http.Error(w, "Failed to get comments", http.StatusInternalServerError)
        return
    }
    defer rows.Close()

    var comments []Comment
    for rows.Next() {
        var c Comment
        rows.Scan(&c.ID, &c.UserID, &c.Username, &c.Content, &c.Timestamp)
        comments = append(comments, c)
    }

    json.NewEncoder(w).Encode(comments)
}
```

### 優化：冷熱分離

```
熱數據（最近 7 天）：MySQL
冷數據（> 7 天）：對象存儲（S3）

策略：
1. 直播結束後，將彈幕導出為 JSON 文件
2. 上傳到 S3
3. 刪除 MySQL 中的數據

回放時：
1. 檢查是否在熱數據範圍
2. 如果是，從 MySQL 查詢
3. 如果不是，從 S3 下載 JSON
```

```go
func (s *ReplayService) GetReplayComments(ctx context.Context, roomID, sessionID string, startTime, endTime int) ([]Comment, error) {
    // 檢查是否為熱數據
    var createdAt time.Time
    query := `SELECT start_time FROM live_sessions WHERE id = ?`
    s.db.QueryRow(query, sessionID).Scan(&createdAt)

    if time.Since(createdAt) < 7*24*time.Hour {
        // 熱數據，從 MySQL 查詢
        return s.getCommentsFromDB(roomID, sessionID, startTime, endTime)
    } else {
        // 冷數據，從 S3 查詢
        return s.getCommentsFromS3(roomID, sessionID, startTime, endTime)
    }
}
```

---

## Act 8: Redis Pub/Sub 橫向擴展

**Michael**: "如果我們有多個 WebSocket 服務器，用戶 A 連接到服務器 1，用戶 B 連接到服務器 2，他們怎麼互相看到彈幕？"

**David**: "需要用 **Redis Pub/Sub** 做消息分發。"

### 架構設計

```
用戶 A → WebSocket Server 1
                ↓
           Redis Pub/Sub (Channel: room123)
                ↓
用戶 B ← WebSocket Server 2
```

### 實現：Redis Pub/Sub

```go
package main

import (
    "context"
    "encoding/json"

    "github.com/go-redis/redis/v8"
)

// DistributedRoom - 分布式房間
type DistributedRoom struct {
    ID       string
    clients  map[*Client]bool
    mu       sync.RWMutex
    redis    *redis.Client
    pubsub   *redis.PubSub
}

func NewDistributedRoom(roomID string, redisClient *redis.Client) *DistributedRoom {
    room := &DistributedRoom{
        ID:      roomID,
        clients: make(map[*Client]bool),
        redis:   redisClient,
    }

    // 訂閱 Redis 頻道
    room.pubsub = redisClient.Subscribe(context.Background(), "room:"+roomID)

    // 啟動訂閱 goroutine
    go room.subscribe()

    return room
}

// subscribe - 訂閱 Redis 消息
func (r *DistributedRoom) subscribe() {
    ch := r.pubsub.Channel()

    for msg := range ch {
        var comment Comment
        json.Unmarshal([]byte(msg.Payload), &comment)

        // 廣播給本服務器的所有客戶端
        r.mu.RLock()
        for client := range r.clients {
            client.conn.WriteJSON(comment)
        }
        r.mu.RUnlock()
    }
}

// Broadcast - 廣播彈幕（發布到 Redis）
func (r *DistributedRoom) Broadcast(comment Comment) {
    // 發布到 Redis
    data, _ := json.Marshal(comment)
    r.redis.Publish(context.Background(), "room:"+r.ID, data)
}
```

**優勢**：
- ✅ 水平擴展（無狀態）
- ✅ 跨服務器通訊
- ✅ 高可用（Redis 哨兵模式）

---

## Act 9: 彈幕排名和熱度

**Emma**: "我們希望展示熱門彈幕，比如被點贊最多的彈幕。"

### 數據庫設計

```sql
ALTER TABLE comments
ADD COLUMN like_count INT DEFAULT 0;

CREATE TABLE comment_likes (
    comment_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (comment_id, user_id),
    INDEX idx_comment_id (comment_id)
);
```

### 實現：熱門彈幕

```go
package main

import (
    "context"
    "fmt"

    "github.com/go-redis/redis/v8"
)

// HotCommentService - 熱門彈幕服務
type HotCommentService struct {
    redis *redis.Client
    db    *sql.DB
}

// LikeComment - 點贊彈幕
func (s *HotCommentService) LikeComment(ctx context.Context, commentID int64, userID string) error {
    // 1. 檢查是否已點贊
    key := fmt.Sprintf("comment:%d:liked_by", commentID)
    isMember, _ := s.redis.SIsMember(ctx, key, userID).Result()
    if isMember {
        return fmt.Errorf("already liked")
    }

    // 2. 添加到點贊集合
    s.redis.SAdd(ctx, key, userID)

    // 3. 增加熱度分數（Sorted Set）
    hotKey := fmt.Sprintf("hot_comments:%s", "room123") // 可以按房間分
    s.redis.ZIncrBy(ctx, hotKey, 1, fmt.Sprintf("%d", commentID))

    // 4. 更新數據庫（異步）
    go func() {
        query := `UPDATE comments SET like_count = like_count + 1 WHERE id = ?`
        s.db.Exec(query, commentID)
    }()

    return nil
}

// GetHotComments - 獲取熱門彈幕
func (s *HotCommentService) GetHotComments(ctx context.Context, roomID string, limit int) ([]Comment, error) {
    // 從 Redis Sorted Set 獲取 Top N
    key := fmt.Sprintf("hot_comments:%s", roomID)
    commentIDs, err := s.redis.ZRevRange(ctx, key, 0, int64(limit-1)).Result()
    if err != nil {
        return nil, err
    }

    // 從數據庫批量查詢詳情
    if len(commentIDs) == 0 {
        return []Comment{}, nil
    }

    placeholders := ""
    for i := range commentIDs {
        if i > 0 {
            placeholders += ", "
        }
        placeholders += "?"
    }

    query := fmt.Sprintf(`
        SELECT id, user_id, username, content, like_count, created_at
        FROM comments
        WHERE id IN (%s)
    `, placeholders)

    args := make([]interface{}, len(commentIDs))
    for i, id := range commentIDs {
        args[i] = id
    }

    rows, err := s.db.Query(query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var comments []Comment
    for rows.Next() {
        var c Comment
        rows.Scan(&c.ID, &c.UserID, &c.Username, &c.Content, &c.LikeCount, &c.CreatedAt)
        comments = append(comments, c)
    }

    return comments, nil
}
```

---

## Act 10: 完整架構和性能優化

**Michael**: "讓我們總結一下最終的架構。"

### 最終架構圖

```
┌─────────────────────────────────────────────┐
│          Load Balancer (Nginx)               │
└─────────────────┬───────────────────────────┘
                  ↓
      ┌───────────┴───────────┐
      ↓                       ↓
┌─────────────┐         ┌─────────────┐
│ WS Server 1 │         │ WS Server N │
└──────┬──────┘         └──────┬──────┘
       │                       │
       └───────────┬───────────┘
                   ↓
       ┌───────────────────────┐
       │   Redis Pub/Sub       │
       │   (消息分發)           │
       └───────────────────────┘
                   ↓
       ┌───────────┴───────────┐
       ↓           ↓           ↓
   ┌──────┐   ┌──────┐   ┌──────┐
   │Redis │   │Kafka │   │ MySQL│
   │Cache │   │Queue │   │      │
   └──────┘   └──────┘   └──────┘
```

### 性能優化清單

```
1. WebSocket 優化：
   ✅ 批量廣播（100ms 一次）
   ✅ Goroutine Pool（避免無限 goroutine）
   ✅ 連接復用（Keep-Alive）

2. 限流策略：
   ✅ 用戶級限流（每秒 1 條）
   ✅ 房間級限流（每秒 1000 條）
   ✅ 全局限流（每秒 10 萬條）

3. 緩存優化：
   ✅ Redis 緩存熱門彈幕
   ✅ 本地緩存敏感詞庫
   ✅ CDN 緩存靜態資源

4. 存儲優化：
   ✅ 冷熱分離（MySQL + S3）
   ✅ 分庫分表（按房間 ID）
   ✅ 定期清理舊數據

5. 降級策略：
   ✅ Redis 故障 → 降級為單服務器廣播
   ✅ MySQL 故障 → 只保存到 Kafka
   ✅ 過載保護 → 丟棄部分彈幕（顯示"彈幕過多"）
```

### 性能指標

```
系統容量（10 台 WebSocket 服務器）：

並發連接：
- 單服務器：10,000 並發連接
- 集群：100,000 並發連接

吞吐量：
- 發送彈幕：10,000 條/秒
- 廣播彈幕：100,000 × 10,000 = 10 億次/秒（理論）

延遲：
- 彈幕延遲：P99 < 100ms
- 廣播延遲：P99 < 50ms

可用性：
- 系統可用性：99.9%
- 彈幕送達率：99%+
```

---

## 總結與回顧

**Emma**: "我們從簡單的 HTTP 輪詢，演進到了分布式 WebSocket 彈幕系統。讓我們回顧一下關鍵設計決策。"

### 演進歷程

1. **Act 1**: HTTP 輪詢（延遲高、浪費資源）
2. **Act 2**: Long Polling（改善延遲）
3. **Act 3**: WebSocket（實時通訊）
4. **Act 4**: 高並發優化（批量廣播、Goroutine Pool）
5. **Act 5**: 彈幕限流（防刷屏）
6. **Act 6**: 敏感詞過濾（Trie 樹）
7. **Act 7**: 彈幕存儲和回放（冷熱分離）
8. **Act 8**: Redis Pub/Sub（橫向擴展）
9. **Act 9**: 熱門彈幕（Redis Sorted Set）
10. **Act 10**: 完整架構和性能優化

### 核心設計原則

1. **實時性優先**：WebSocket 全雙工通訊
2. **高並發處理**：批量廣播、異步處理
3. **限流保護**：用戶級、房間級、全局限流
4. **內容安全**：敏感詞過濾、人工審核
5. **橫向擴展**：Redis Pub/Sub 跨服務器通訊
6. **降級策略**：故障時優雅降級

### 關鍵技術選型

| 組件 | 技術 | 原因 |
|------|------|------|
| 實時通訊 | WebSocket | 全雙工、低延遲 |
| 消息分發 | Redis Pub/Sub | 簡單、高性能 |
| 限流 | Redis | 分布式計數器 |
| 敏感詞 | Trie 樹 | O(n) 時間複雜度 |
| 存儲 | MySQL + S3 | 冷熱分離 |
| 熱門彈幕 | Redis Sorted Set | 自動排序 |

### 真實案例：Bilibili 的彈幕系統

**David**: "Bilibili 的彈幕系統支持數百萬並發用戶。"

```
Bilibili 彈幕架構：

前端：
- WebSocket 連接
- 彈幕渲染引擎（Canvas）
- 碰撞檢測（防重疊）

後端：
- Golang WebSocket 服務器
- Redis Pub/Sub 消息分發
- Kafka 持久化存儲
- HBase 彈幕歸檔

優化：
1. 彈幕採樣（高峰期只顯示部分彈幕）
2. 智能合併（相似彈幕合併為一條）
3. 優先級隊列（VIP 彈幕優先顯示）
4. CDN 加速（彈幕回放文件）

監控：
- 實時並發數
- 彈幕發送 QPS
- 平均延遲
- 錯誤率
```

### 常見坑

1. **內存洩漏**：未正確關閉 WebSocket 連接
2. **廣播風暴**：10 萬人 × 每秒 100 條彈幕 = 1000 萬次廣播/秒
3. **熱點房間**：單個房間 10 萬人，需要特殊處理
4. **敏感詞庫**：定期更新，避免過時
5. **跨域問題**：WebSocket 需要正確配置 CORS

---

## 練習題

1. **設計題**：如何實現彈幕的「只看TA」功能？（只顯示特定用戶的彈幕）
2. **優化題**：如何減少彈幕廣播的網絡帶寬？（10 萬人 × 1KB/條）
3. **擴展題**：如何支持彈幕禮物動畫？（需要同步顯示）
4. **故障恢復**：如果 Redis Pub/Sub 故障，如何降級？
5. **成本優化**：如何將彈幕存儲成本降低 80%？

---

## 延伸閱讀

- [Bilibili 彈幕系統架構](https://www.bilibili.com/read/cv4058562)
- [Twitch Chat Architecture](https://blog.twitch.tv/en/2015/12/18/twitch-engineering-an-introduction-and-overview-a23917b71a25/)
- [YouTube Live Chat](https://www.youtube.com/watch?v=W8TjYZ7LhO8)
- [WebSocket Protocol RFC 6455](https://datatracker.ietf.org/doc/html/rfc6455)
- [Redis Pub/Sub](https://redis.io/docs/manual/pubsub/)

**核心理念：實時、高並發、內容安全！**
