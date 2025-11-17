# Chat System 系統設計文檔

## 週五下午的產品需求

2024 年 12 月 15 日下午 4:00

產品經理 Jennifer 召集技術團隊開會。

**Jennifer**：「我們要做一個即時通訊系統，像 WhatsApp 那樣！用戶可以發送訊息、創建群組、看到已讀回執。」

**Emma**（後端工程師）：「聽起來很簡單，不就是發送訊息、存到資料庫嗎？」

**David**（架構師）笑了：「如果只是這樣，我們下班前就能做完。但即時通訊有很多細節需要考慮：

1. 如何讓用戶**即時收到**訊息？
2. 用戶**離線時**，訊息如何保存？
3. 群聊時，如何快速發給所有成員？
4. 如何保證訊息**不丟失、不重複**？
5. 如何顯示**已讀/未讀**狀態？

讓我們一步步來。」

---

## 第一幕：最簡單的方案 - 輪詢（Polling）

**Emma**：「我有個想法：用戶定期向伺服器請求新訊息！」

**David**：「沒錯，這是最簡單的方案，叫做**輪詢**（Polling）。」

### 輪詢實現

```
客戶端輪詢流程：

每 3 秒發送請求：
GET /messages?user_id=Alice&after_id=100

伺服器返回：
{
  "messages": [
    {"id": 101, "from": "Bob", "content": "Hi!", "timestamp": 1733160000},
    {"id": 102, "from": "Charlie", "content": "Hello", "timestamp": 1733160005}
  ]
}

客戶端收到後更新 UI，然後再等 3 秒，繼續請求...
```

### 代碼實現（Polling）

```go
// internal/polling.go
package internal

import (
    "database/sql"
    "time"
)

type Message struct {
    ID        int64
    From      string
    To        string
    Content   string
    Timestamp time.Time
}

type PollingService struct {
    db *sql.DB
}

// GetNewMessages 獲取新訊息（輪詢）
func (s *PollingService) GetNewMessages(userID string, afterID int64) ([]Message, error) {
    query := `
        SELECT id, from_user, to_user, content, created_at
        FROM messages
        WHERE to_user = ? AND id > ?
        ORDER BY id ASC
        LIMIT 100
    `

    rows, err := s.db.Query(query, userID, afterID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var messages []Message
    for rows.Next() {
        var msg Message
        if err := rows.Scan(&msg.ID, &msg.From, &msg.To, &msg.Content, &msg.Timestamp); err != nil {
            continue
        }
        messages = append(messages, msg)
    }

    return messages, nil
}

// SendMessage 發送訊息
func (s *PollingService) SendMessage(from, to, content string) error {
    query := `
        INSERT INTO messages (from_user, to_user, content, created_at)
        VALUES (?, ?, ?, ?)
    `

    _, err := s.db.Exec(query, from, to, content, time.Now())
    return err
}
```

### 性能測試

```
場景：1000 個在線用戶，每 3 秒輪詢一次

每秒請求數（QPS）：
- 1000 用戶 / 3 秒 = 333 QPS

數據庫查詢：
- 每次輪詢 1 次 SELECT
- 333 queries/s（大部分返回空結果）❌

問題：
1. 浪費資源：用戶沒有新訊息時，仍然要發請求
2. 延遲高：用戶最多等 3 秒才能收到訊息
3. 如果縮短輪詢間隔（如 1 秒），QPS 會增加 3 倍 ❌
```

**Emma**：「太浪費了！大部分請求都是空的！」

**David**：「沒錯。有沒有辦法讓伺服器**主動推送**訊息給客戶端？」

---

## 第二幕：Long Polling 的改進

**Michael**（後端工程師）：「我知道！用 **Long Polling**（長輪詢）！」

**David**：「對！Long Polling 的核心思想：客戶端發請求後，伺服器**不立即返回**，而是等到有新訊息時才返回。」

### Long Polling 流程

```
Long Polling 流程：

1. 客戶端發送請求：
   GET /messages/long_poll?user_id=Alice&timeout=30

2. 伺服器收到請求後：
   - 如果有新訊息：立即返回
   - 如果沒有新訊息：等待（最多 30 秒）

3. 等待期間，如果有新訊息到達：
   - 伺服器立即返回訊息
   - 客戶端收到後，立即發起下一次請求

4. 如果 30 秒後仍無訊息：
   - 伺服器返回空結果
   - 客戶端立即發起下一次請求
```

### Long Polling 實現

```go
// internal/long_polling.go
package internal

import (
    "context"
    "database/sql"
    "sync"
    "time"
)

type LongPollingService struct {
    db *sql.DB

    // 訂閱管理：user_id -> channel
    subscribers map[string][]chan Message
    mu          sync.RWMutex
}

func NewLongPollingService(db *sql.DB) *LongPollingService {
    return &LongPollingService{
        db:          db,
        subscribers: make(map[string][]chan Message),
    }
}

// LongPoll 長輪詢（等待新訊息）
func (s *LongPollingService) LongPoll(ctx context.Context, userID string, afterID int64, timeout time.Duration) ([]Message, error) {
    // 1. 先查詢是否有現存的新訊息
    messages, err := s.getNewMessages(userID, afterID)
    if err != nil {
        return nil, err
    }

    if len(messages) > 0 {
        // 有新訊息，立即返回
        return messages, nil
    }

    // 2. 沒有新訊息，訂閱並等待
    ch := make(chan Message, 10)
    s.subscribe(userID, ch)
    defer s.unsubscribe(userID, ch)

    // 3. 等待新訊息或超時
    timer := time.NewTimer(timeout)
    defer timer.Stop()

    select {
    case msg := <-ch:
        // 收到新訊息
        return []Message{msg}, nil

    case <-timer.C:
        // 超時，返回空
        return []Message{}, nil

    case <-ctx.Done():
        // 請求取消
        return nil, ctx.Err()
    }
}

// SendMessage 發送訊息（並通知訂閱者）
func (s *LongPollingService) SendMessage(from, to, content string) error {
    // 1. 保存訊息到資料庫
    query := `
        INSERT INTO messages (from_user, to_user, content, created_at)
        VALUES (?, ?, ?, ?)
    `

    result, err := s.db.Exec(query, from, to, content, time.Now())
    if err != nil {
        return err
    }

    id, _ := result.LastInsertId()

    msg := Message{
        ID:        id,
        From:      from,
        To:        to,
        Content:   content,
        Timestamp: time.Now(),
    }

    // 2. 通知訂閱者（推送給正在等待的客戶端）
    s.notify(to, msg)

    return nil
}

func (s *LongPollingService) subscribe(userID string, ch chan Message) {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.subscribers[userID] = append(s.subscribers[userID], ch)
}

func (s *LongPollingService) unsubscribe(userID string, ch chan Message) {
    s.mu.Lock()
    defer s.mu.Unlock()

    channels := s.subscribers[userID]
    for i, c := range channels {
        if c == ch {
            // 刪除這個 channel
            s.subscribers[userID] = append(channels[:i], channels[i+1:]...)
            close(ch)
            break
        }
    }
}

func (s *LongPollingService) notify(userID string, msg Message) {
    s.mu.RLock()
    channels := s.subscribers[userID]
    s.mu.RUnlock()

    // 發送給所有訂閱者
    for _, ch := range channels {
        select {
        case ch <- msg:
            // 成功發送
        default:
            // channel 已滿，跳過
        }
    }
}

func (s *LongPollingService) getNewMessages(userID string, afterID int64) ([]Message, error) {
    query := `
        SELECT id, from_user, to_user, content, created_at
        FROM messages
        WHERE to_user = ? AND id > ?
        ORDER BY id ASC
        LIMIT 100
    `

    rows, err := s.db.Query(query, userID, afterID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var messages []Message
    for rows.Next() {
        var msg Message
        if err := rows.Scan(&msg.ID, &msg.From, &msg.To, &msg.Content, &msg.Timestamp); err != nil {
            continue
        }
        messages = append(messages, msg)
    }

    return messages, nil
}
```

### 性能對比

```
Short Polling vs Long Polling

場景：1000 個在線用戶，每分鐘平均收到 1 條訊息

Short Polling（每 3 秒輪詢）：
- QPS：1000 / 3 = 333
- 大部分請求返回空結果 ❌
- 延遲：0-3 秒

Long Polling（30 秒超時）：
- QPS：1000 / 30 = 33（沒有訊息時）
- 有訊息時立即返回 ✅
- 延遲：< 100ms ✅

資源節省：333 / 33 = 10 倍！
```

**Emma**：「太棒了！延遲降低，資源節省 10 倍！」

**David**：「但 Long Polling 仍有問題：

1. 連線數問題：每個用戶佔用一個 HTTP 連線，1000 用戶 = 1000 連線
2. 伺服器需要維護大量等待的請求
3. 請求-響應模型：伺服器只能在客戶端發請求時才能推送

有沒有更好的方案？」

**Michael**：「**WebSocket**！全雙工通訊！」

---

## 第三幕：WebSocket 的革命

**David**：「沒錯！WebSocket 是真正的雙向通訊協議。」

### WebSocket vs HTTP

```
HTTP（請求-響應模型）：
客戶端 → 請求 → 伺服器
客戶端 ← 響應 ← 伺服器
（每次通訊都要建立新連線）

WebSocket（全雙工通訊）：
客戶端 → 握手 → 伺服器（升級到 WebSocket）
客戶端 ⇄ 雙向通訊 ⇄ 伺服器
（連線保持，雙向推送）

優勢：
1. 低延遲：訊息即時推送，無需等待
2. 低開銷：一次握手，長期連線
3. 雙向：伺服器可以主動推送
```

### WebSocket 實現

```go
// internal/websocket.go
package internal

import (
    "encoding/json"
    "log"
    "net/http"
    "sync"

    "github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true // 生產環境應該檢查 Origin
    },
}

type WebSocketService struct {
    // 在線用戶：user_id -> WebSocket 連線
    clients map[string]*websocket.Conn
    mu      sync.RWMutex

    db *sql.DB
}

func NewWebSocketService(db *sql.DB) *WebSocketService {
    return &WebSocketService{
        clients: make(map[string]*websocket.Conn),
        db:      db,
    }
}

// HandleWebSocket 處理 WebSocket 連線
func (s *WebSocketService) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    // 1. 升級 HTTP 連線到 WebSocket
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("WebSocket upgrade failed: %v", err)
        return
    }

    // 2. 從查詢參數獲取 user_id
    userID := r.URL.Query().Get("user_id")
    if userID == "" {
        conn.Close()
        return
    }

    // 3. 註冊連線
    s.registerClient(userID, conn)
    defer s.unregisterClient(userID)

    log.Printf("User %s connected", userID)

    // 4. 監聽客戶端訊息
    for {
        var msg map[string]interface{}
        err := conn.ReadJSON(&msg)
        if err != nil {
            // 連線斷開
            log.Printf("User %s disconnected: %v", userID, err)
            break
        }

        // 處理訊息
        s.handleMessage(userID, msg)
    }
}

func (s *WebSocketService) handleMessage(from string, msg map[string]interface{}) {
    msgType, ok := msg["type"].(string)
    if !ok {
        return
    }

    switch msgType {
    case "send_message":
        // 發送訊息
        to, _ := msg["to"].(string)
        content, _ := msg["content"].(string)

        if err := s.SendMessage(from, to, content); err != nil {
            log.Printf("Failed to send message: %v", err)
        }

    case "typing":
        // 打字狀態（可選）
        to, _ := msg["to"].(string)
        s.sendTypingStatus(from, to)
    }
}

// SendMessage 發送訊息（通過 WebSocket 推送）
func (s *WebSocketService) SendMessage(from, to, content string) error {
    // 1. 保存到資料庫
    query := `
        INSERT INTO messages (from_user, to_user, content, created_at)
        VALUES (?, ?, ?, ?)
    `

    result, err := s.db.Exec(query, from, to, content, time.Now())
    if err != nil {
        return err
    }

    id, _ := result.LastInsertId()

    msg := Message{
        ID:        id,
        From:      from,
        To:        to,
        Content:   content,
        Timestamp: time.Now(),
    }

    // 2. 推送給接收者（如果在線）
    s.pushToClient(to, map[string]interface{}{
        "type":    "new_message",
        "message": msg,
    })

    return nil
}

func (s *WebSocketService) registerClient(userID string, conn *websocket.Conn) {
    s.mu.Lock()
    defer s.mu.Unlock()

    // 如果已有連線，關閉舊連線
    if oldConn, ok := s.clients[userID]; ok {
        oldConn.Close()
    }

    s.clients[userID] = conn
}

func (s *WebSocketService) unregisterClient(userID string) {
    s.mu.Lock()
    defer s.mu.Unlock()

    if conn, ok := s.clients[userID]; ok {
        conn.Close()
        delete(s.clients, userID)
    }
}

func (s *WebSocketService) pushToClient(userID string, data interface{}) {
    s.mu.RLock()
    conn, ok := s.clients[userID]
    s.mu.RUnlock()

    if !ok {
        // 用戶離線，訊息已保存在資料庫，用戶上線後會拉取
        return
    }

    // 發送訊息
    if err := conn.WriteJSON(data); err != nil {
        log.Printf("Failed to push to client %s: %v", userID, err)
    }
}

func (s *WebSocketService) sendTypingStatus(from, to string) {
    s.pushToClient(to, map[string]interface{}{
        "type": "typing",
        "from": from,
    })
}
```

### HTTP Server

```go
// cmd/server/main.go
package main

import (
    "database/sql"
    "log"
    "net/http"

    _ "github.com/go-sql-driver/mysql"
)

func main() {
    // 連接資料庫
    db, err := sql.Open("mysql", "user:password@tcp(localhost:3306)/chat")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // 創建 WebSocket 服務
    wsService := internal.NewWebSocketService(db)

    // 路由
    http.HandleFunc("/ws", wsService.HandleWebSocket)

    // 啟動伺服器
    log.Println("Server started on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### 客戶端示例（JavaScript）

```javascript
// 連接 WebSocket
const ws = new WebSocket('ws://localhost:8080/ws?user_id=Alice');

// 連線成功
ws.onopen = () => {
    console.log('Connected to chat server');
};

// 收到訊息
ws.onmessage = (event) => {
    const data = JSON.parse(event.data);

    if (data.type === 'new_message') {
        const msg = data.message;
        displayMessage(msg.from, msg.content, msg.timestamp);
    } else if (data.type === 'typing') {
        showTypingIndicator(data.from);
    }
};

// 發送訊息
function sendMessage(to, content) {
    ws.send(JSON.stringify({
        type: 'send_message',
        to: to,
        content: content
    }));
}

// 發送打字狀態
function sendTypingStatus(to) {
    ws.send(JSON.stringify({
        type: 'typing',
        to: to
    }));
}

// 連線關閉
ws.onclose = () => {
    console.log('Disconnected from chat server');
    // 自動重連
    setTimeout(() => location.reload(), 3000);
};
```

### 性能對比

```
Polling vs Long Polling vs WebSocket

場景：1000 個在線用戶

延遲：
- Polling：0-3 秒 ❌
- Long Polling：< 100ms ✅
- WebSocket：< 10ms ✅✅

QPS（無訊息時）：
- Polling：333 QPS ❌
- Long Polling：33 QPS ✅
- WebSocket：0 QPS ✅✅（不需要輪詢）

連線數：
- Polling：每次請求建立新連線 ❌
- Long Polling：1000 個長連線 ⚠️
- WebSocket：1000 個長連線 ✅（更輕量）

資源佔用（每個連線）：
- HTTP 連線：~10 KB
- WebSocket 連線：~2 KB
```

**Emma**：「WebSocket 延遲只有 10ms！太快了！」

**David**：「沒錯。現在我們來處理更複雜的場景：**群聊**。」

---

## 第四幕：群聊的挑戰

**Jennifer**（產品經理）：「我們要支持群聊！用戶可以創建群組，發送訊息給所有成員。」

**David**：「群聊的核心問題：如何快速將訊息發給所有成員？

假設一個群組有 100 個成員，發送一條訊息：
- 需要推送給 100 個用戶
- 如果 50 個用戶在線 → 50 次 WebSocket 推送
- 如果 50 個用戶離線 → 50 條離線訊息

這就是 **Fanout 問題**（類似 News Feed）。」

### 群聊實現

```go
// internal/group_chat.go
package internal

import (
    "database/sql"
    "sync"
    "time"
)

type GroupMessage struct {
    ID        int64
    GroupID   string
    From      string
    Content   string
    Timestamp time.Time
}

type GroupChatService struct {
    db      *sql.DB
    clients map[string]*websocket.Conn
    mu      sync.RWMutex
}

// SendGroupMessage 發送群聊訊息
func (s *GroupChatService) SendGroupMessage(groupID, from, content string) error {
    // 1. 保存訊息到資料庫
    query := `
        INSERT INTO group_messages (group_id, from_user, content, created_at)
        VALUES (?, ?, ?, ?)
    `

    result, err := s.db.Exec(query, groupID, from, content, time.Now())
    if err != nil {
        return err
    }

    msgID, _ := result.LastInsertId()

    msg := GroupMessage{
        ID:        msgID,
        GroupID:   groupID,
        From:      from,
        Content:   content,
        Timestamp: time.Now(),
    }

    // 2. 查詢群組成員
    members, err := s.getGroupMembers(groupID)
    if err != nil {
        return err
    }

    // 3. Fanout：推送給所有在線成員
    for _, memberID := range members {
        if memberID == from {
            continue // 跳過發送者
        }

        // 推送給在線用戶
        s.pushToClient(memberID, map[string]interface{}{
            "type":    "new_group_message",
            "message": msg,
        })

        // 離線用戶的訊息已在資料庫，上線後會拉取
    }

    return nil
}

func (s *GroupChatService) getGroupMembers(groupID string) ([]string, error) {
    query := `
        SELECT user_id FROM group_members WHERE group_id = ?
    `

    rows, err := s.db.Query(query, groupID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var members []string
    for rows.Next() {
        var userID string
        if err := rows.Scan(&userID); err != nil {
            continue
        }
        members = append(members, userID)
    }

    return members, nil
}

func (s *GroupChatService) pushToClient(userID string, data interface{}) {
    s.mu.RLock()
    conn, ok := s.clients[userID]
    s.mu.RUnlock()

    if !ok {
        return // 用戶離線
    }

    conn.WriteJSON(data)
}
```

### 性能瓶頸

```
場景：一個 1000 人的大群

用戶 A 發送訊息：
1. 寫入資料庫：10ms
2. 查詢群組成員：SELECT 1000 rows = 20ms
3. Fanout 推送：
   - 假設 500 人在線
   - 每次 WriteJSON：1ms
   - 總計：500ms ❌

問題：
- Fanout 是序列的（一個一個推送）
- 500ms 太慢！用戶會感覺延遲
```

**Emma**：「500ms 太慢了！能並行推送嗎？」

**David**：「可以用 **goroutine** 並行推送！」

### 並行 Fanout 優化

```go
// internal/group_chat_optimized.go
package internal

// SendGroupMessage 發送群聊訊息（優化版：並行 Fanout）
func (s *GroupChatService) SendGroupMessage(groupID, from, content string) error {
    // 1. 保存訊息
    msgID, err := s.saveMessage(groupID, from, content)
    if err != nil {
        return err
    }

    msg := GroupMessage{
        ID:        msgID,
        GroupID:   groupID,
        From:      from,
        Content:   content,
        Timestamp: time.Now(),
    }

    // 2. 查詢群組成員
    members, err := s.getGroupMembers(groupID)
    if err != nil {
        return err
    }

    // 3. 並行 Fanout（使用 goroutine）
    var wg sync.WaitGroup
    for _, memberID := range members {
        if memberID == from {
            continue
        }

        wg.Add(1)
        go func(uid string) {
            defer wg.Done()
            s.pushToClient(uid, map[string]interface{}{
                "type":    "new_group_message",
                "message": msg,
            })
        }(memberID)
    }

    // 4. 等待所有推送完成（或設置超時）
    done := make(chan struct{})
    go func() {
        wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        // 全部完成
        return nil
    case <-time.After(3 * time.Second):
        // 超時（有些推送可能失敗，但訊息已保存）
        return nil
    }
}
```

### 性能提升

```
序列 Fanout vs 並行 Fanout

場景：1000 人群組，500 人在線

序列 Fanout：
- 每次推送：1ms
- 總計：500 × 1ms = 500ms ❌

並行 Fanout（goroutine）：
- 並行推送（假設 100 個併發）
- 總計：500 / 100 × 1ms ≈ 5-10ms ✅

提升：50-100 倍！
```

**Emma**：「並行推送太快了！只需要 10ms！」

**David**：「沒錯。但還有一個問題：**離線訊息**。」

---

## 第五幕：離線訊息存儲

**Sarah**（DBA）：「用戶離線時，訊息如何保存？用戶上線後如何拉取？」

**David**：「我們需要一個 **離線訊息隊列**（Offline Message Queue）。」

### 離線訊息設計

```
離線訊息流程：

1. 用戶 A 發訊息給 用戶 B：
   - 檢查 B 是否在線
   - 如果在線：直接推送 ✅
   - 如果離線：寫入 offline_messages 表 ✅

2. 用戶 B 上線：
   - 連接 WebSocket
   - 拉取所有離線訊息
   - 推送給客戶端
   - 標記為已送達

3. 客戶端收到後：
   - 發送 ACK（確認收到）
   - 伺服器刪除離線訊息
```

### 數據庫設計

```sql
-- 離線訊息表
CREATE TABLE offline_messages (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    to_user VARCHAR(64) NOT NULL,
    from_user VARCHAR(64) NOT NULL,
    content TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_to_user (to_user, created_at)
);

-- 訊息狀態表（用於去重和已讀回執）
CREATE TABLE message_status (
    message_id BIGINT PRIMARY KEY,
    to_user VARCHAR(64) NOT NULL,
    delivered BOOLEAN DEFAULT FALSE,  -- 是否已送達
    read BOOLEAN DEFAULT FALSE,        -- 是否已讀
    delivered_at TIMESTAMP NULL,
    read_at TIMESTAMP NULL,
    INDEX idx_to_user (to_user)
);
```

### 離線訊息實現

```go
// internal/offline.go
package internal

type OfflineService struct {
    db      *sql.DB
    clients map[string]*websocket.Conn
    mu      sync.RWMutex
}

// SendMessage 發送訊息（支持離線）
func (s *OfflineService) SendMessage(from, to, content string) error {
    // 1. 保存到 messages 表
    query := `
        INSERT INTO messages (from_user, to_user, content, created_at)
        VALUES (?, ?, ?, ?)
    `

    result, err := s.db.Exec(query, from, to, content, time.Now())
    if err != nil {
        return err
    }

    msgID, _ := result.LastInsertId()

    msg := Message{
        ID:        msgID,
        From:      from,
        To:        to,
        Content:   content,
        Timestamp: time.Now(),
    }

    // 2. 檢查接收者是否在線
    if s.isOnline(to) {
        // 在線：直接推送
        s.pushToClient(to, map[string]interface{}{
            "type":    "new_message",
            "message": msg,
        })
    } else {
        // 離線：保存到離線訊息表
        s.saveOfflineMessage(msg)
    }

    return nil
}

func (s *OfflineService) isOnline(userID string) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()

    _, ok := s.clients[userID]
    return ok
}

func (s *OfflineService) saveOfflineMessage(msg Message) error {
    query := `
        INSERT INTO offline_messages (to_user, from_user, content, created_at)
        VALUES (?, ?, ?, ?)
    `

    _, err := s.db.Exec(query, msg.To, msg.From, msg.Content, msg.Timestamp)
    return err
}

// OnUserConnect 用戶上線時，推送離線訊息
func (s *OfflineService) OnUserConnect(userID string, conn *websocket.Conn) error {
    // 1. 註冊連線
    s.registerClient(userID, conn)

    // 2. 拉取離線訊息
    offlineMessages, err := s.getOfflineMessages(userID)
    if err != nil {
        return err
    }

    // 3. 推送離線訊息
    for _, msg := range offlineMessages {
        conn.WriteJSON(map[string]interface{}{
            "type":    "new_message",
            "message": msg,
        })
    }

    // 4. 刪除已推送的離線訊息
    s.deleteOfflineMessages(userID)

    return nil
}

func (s *OfflineService) getOfflineMessages(userID string) ([]Message, error) {
    query := `
        SELECT id, from_user, to_user, content, created_at
        FROM offline_messages
        WHERE to_user = ?
        ORDER BY created_at ASC
        LIMIT 1000
    `

    rows, err := s.db.Query(query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var messages []Message
    for rows.Next() {
        var msg Message
        if err := rows.Scan(&msg.ID, &msg.From, &msg.To, &msg.Content, &msg.Timestamp); err != nil {
            continue
        }
        messages = append(messages, msg)
    }

    return messages, nil
}

func (s *OfflineService) deleteOfflineMessages(userID string) error {
    query := "DELETE FROM offline_messages WHERE to_user = ?"
    _, err := s.db.Exec(query, userID)
    return err
}

func (s *OfflineService) registerClient(userID string, conn *websocket.Conn) {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.clients[userID] = conn
}
```

### 離線訊息容量限制

```go
// 限制離線訊息數量（避免用戶離線太久，訊息過多）
const MAX_OFFLINE_MESSAGES = 1000

func (s *OfflineService) saveOfflineMessage(msg Message) error {
    // 1. 檢查離線訊息數量
    var count int
    countQuery := "SELECT COUNT(*) FROM offline_messages WHERE to_user = ?"
    s.db.QueryRow(countQuery, msg.To).Scan(&count)

    if count >= MAX_OFFLINE_MESSAGES {
        // 刪除最舊的訊息
        deleteQuery := `
            DELETE FROM offline_messages
            WHERE to_user = ?
            ORDER BY created_at ASC
            LIMIT 1
        `
        s.db.Exec(deleteQuery, msg.To)
    }

    // 2. 插入新訊息
    query := `
        INSERT INTO offline_messages (to_user, from_user, content, created_at)
        VALUES (?, ?, ?, ?)
    `

    _, err := s.db.Exec(query, msg.To, msg.From, msg.Content, msg.Timestamp)
    return err
}
```

---

## 第六幕：已讀回執（Read Receipts）

**Jennifer**（產品經理）：「我們需要顯示訊息的狀態：
- 已送達（Delivered）：訊息已送達接收者的設備
- 已讀（Read）：接收者已打開並閱讀訊息

像 WhatsApp 的雙勾 ✓✓。」

**David**：「這需要客戶端的配合。」

### 訊息狀態流程

```
訊息狀態轉換：

1. 發送（Sent）：
   - 用戶 A 發送訊息
   - 伺服器保存訊息
   - 狀態：Sent

2. 送達（Delivered）：
   - 訊息推送到用戶 B 的設備
   - 客戶端發送 ACK：delivered
   - 狀態：Delivered ✓

3. 已讀（Read）：
   - 用戶 B 打開聊天視窗
   - 客戶端發送 ACK：read
   - 狀態：Read ✓✓
```

### 已讀回執實現

```go
// internal/read_receipt.go
package internal

type MessageStatus struct {
    MessageID   int64
    ToUser      string
    Delivered   bool
    Read        bool
    DeliveredAt *time.Time
    ReadAt      *time.Time
}

type ReadReceiptService struct {
    db      *sql.DB
    clients map[string]*websocket.Conn
    mu      sync.RWMutex
}

// MarkAsDelivered 標記訊息為已送達
func (s *ReadReceiptService) MarkAsDelivered(messageID int64, userID string) error {
    query := `
        UPDATE message_status
        SET delivered = TRUE, delivered_at = ?
        WHERE message_id = ? AND to_user = ?
    `

    _, err := s.db.Exec(query, time.Now(), messageID, userID)
    if err != nil {
        return err
    }

    // 通知發送者（訊息已送達）
    s.notifySender(messageID, "delivered")

    return nil
}

// MarkAsRead 標記訊息為已讀
func (s *ReadReceiptService) MarkAsRead(messageID int64, userID string) error {
    query := `
        UPDATE message_status
        SET read = TRUE, read_at = ?
        WHERE message_id = ? AND to_user = ?
    `

    _, err := s.db.Exec(query, time.Now(), messageID, userID)
    if err != nil {
        return err
    }

    // 通知發送者（訊息已讀）
    s.notifySender(messageID, "read")

    return nil
}

func (s *ReadReceiptService) notifySender(messageID int64, status string) {
    // 1. 查詢訊息的發送者
    var fromUser string
    query := "SELECT from_user FROM messages WHERE id = ?"
    s.db.QueryRow(query, messageID).Scan(&fromUser)

    // 2. 推送狀態更新給發送者
    s.pushToClient(fromUser, map[string]interface{}{
        "type":       "message_status",
        "message_id": messageID,
        "status":     status, // "delivered" or "read"
    })
}

func (s *ReadReceiptService) pushToClient(userID string, data interface{}) {
    s.mu.RLock()
    conn, ok := s.clients[userID]
    s.mu.RUnlock()

    if !ok {
        return
    }

    conn.WriteJSON(data)
}

// HandleWebSocketMessage 處理客戶端訊息
func (s *ReadReceiptService) HandleWebSocketMessage(userID string, msg map[string]interface{}) {
    msgType, _ := msg["type"].(string)

    switch msgType {
    case "delivered":
        // 客戶端確認已送達
        messageID, _ := msg["message_id"].(float64)
        s.MarkAsDelivered(int64(messageID), userID)

    case "read":
        // 客戶端確認已讀
        messageID, _ := msg["message_id"].(float64)
        s.MarkAsRead(int64(messageID), userID)
    }
}
```

### 客戶端示例

```javascript
// 收到訊息時，自動發送「已送達」確認
ws.onmessage = (event) => {
    const data = JSON.parse(event.data);

    if (data.type === 'new_message') {
        const msg = data.message;
        displayMessage(msg);

        // 發送「已送達」確認
        ws.send(JSON.stringify({
            type: 'delivered',
            message_id: msg.id
        }));

        // 如果聊天視窗是打開的，自動發送「已讀」確認
        if (isChatWindowOpen(msg.from)) {
            ws.send(JSON.stringify({
                type: 'read',
                message_id: msg.id
            }));
        }
    } else if (data.type === 'message_status') {
        // 收到訊息狀態更新（已送達/已讀）
        updateMessageStatus(data.message_id, data.status);
    }
};

// 用戶打開聊天視窗時，標記所有訊息為已讀
function openChatWindow(withUser) {
    const unreadMessages = getUnreadMessages(withUser);

    unreadMessages.forEach(msg => {
        ws.send(JSON.stringify({
            type: 'read',
            message_id: msg.id
        }));
    });
}
```

### 群聊已讀回執

```
群聊的已讀回執更複雜：

問題：
- 一個群組有 100 個成員
- 如何顯示「誰已讀、誰未讀」？

方案 A：顯示具體已讀人數
「100 人中 已讀 67 人」

方案 B：只顯示是否有人已讀
「✓ 已送達」「✓✓ 已讀」

WhatsApp 的做法：
- 群聊中只顯示「已送達」和「已讀」
- 點擊訊息可以查看詳細列表（誰已讀、誰未讀）
```

---

## 第七幕：訊息同步機制

**Emma**：「如果用戶在多個設備登入（手機 + 電腦），訊息如何同步？」

**David**：「這是 **多設備同步** 問題。核心思想：每個設備都維護一個**訊息 ID 游標**（Cursor）。」

### 多設備同步設計

```
同步流程：

1. 用戶在手機和電腦同時登入：
   - 手機：device_id = "mobile_1", cursor = 100
   - 電腦：device_id = "desktop_1", cursor = 95

2. 新訊息到達（message_id = 101）：
   - 伺服器推送給兩個設備
   - 手機收到後，更新 cursor = 101
   - 電腦收到後，更新 cursor = 101

3. 電腦離線，手機收到訊息 102-110：
   - 手機 cursor = 110
   - 電腦 cursor = 101（離線）

4. 電腦重新上線：
   - 發送同步請求：GET /sync?user_id=Alice&device_id=desktop_1&after=101
   - 伺服器返回訊息 102-110
   - 電腦更新 cursor = 110

5. 同步完成：
   - 手機和電腦都是 cursor = 110 ✅
```

### 訊息同步實現

```go
// internal/sync.go
package internal

type SyncService struct {
    db      *sql.DB
    clients map[string]map[string]*websocket.Conn // user_id -> device_id -> conn
    mu      sync.RWMutex
}

// RegisterDevice 註冊設備
func (s *SyncService) RegisterDevice(userID, deviceID string, conn *websocket.Conn) {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.clients[userID] == nil {
        s.clients[userID] = make(map[string]*websocket.Conn)
    }

    s.clients[userID][deviceID] = conn
}

// SendMessage 發送訊息（推送給所有設備）
func (s *SyncService) SendMessage(from, to, content string) error {
    // 1. 保存訊息
    query := `
        INSERT INTO messages (from_user, to_user, content, created_at)
        VALUES (?, ?, ?, ?)
    `

    result, err := s.db.Exec(query, from, to, content, time.Now())
    if err != nil {
        return err
    }

    msgID, _ := result.LastInsertId()

    msg := Message{
        ID:        msgID,
        From:      from,
        To:        to,
        Content:   content,
        Timestamp: time.Now(),
    }

    // 2. 推送給接收者的所有在線設備
    s.pushToAllDevices(to, map[string]interface{}{
        "type":    "new_message",
        "message": msg,
    })

    return nil
}

func (s *SyncService) pushToAllDevices(userID string, data interface{}) {
    s.mu.RLock()
    devices := s.clients[userID]
    s.mu.RUnlock()

    if devices == nil {
        return
    }

    // 推送給所有設備
    for deviceID, conn := range devices {
        if err := conn.WriteJSON(data); err != nil {
            // 設備離線，移除連線
            s.removeDevice(userID, deviceID)
        }
    }
}

func (s *SyncService) removeDevice(userID, deviceID string) {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.clients[userID] != nil {
        delete(s.clients[userID], deviceID)
    }
}

// SyncMessages 同步訊息（設備重新上線時）
func (s *SyncService) SyncMessages(userID string, afterID int64, limit int) ([]Message, error) {
    query := `
        SELECT id, from_user, to_user, content, created_at
        FROM messages
        WHERE to_user = ? AND id > ?
        ORDER BY id ASC
        LIMIT ?
    `

    rows, err := s.db.Query(query, userID, afterID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var messages []Message
    for rows.Next() {
        var msg Message
        if err := rows.Scan(&msg.ID, &msg.From, &msg.To, &msg.Content, &msg.Timestamp); err != nil {
            continue
        }
        messages = append(messages, msg)
    }

    return messages, nil
}
```

### HTTP 同步 API

```go
// cmd/server/main.go

// 同步 API（用於設備重新上線）
http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
    userID := r.URL.Query().Get("user_id")
    afterID := parseInt(r.URL.Query().Get("after"))
    limit := 100

    messages, err := syncService.SyncMessages(userID, afterID, limit)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    json.NewEncoder(w).Encode(map[string]interface{}{
        "messages": messages,
        "count":    len(messages),
    })
})
```

---

## 第八幕：訊息可靠性保證

**Sarah**（DBA）：「如何保證訊息**不丟失、不重複**？」

**David**：「這需要**消息確認機制**（ACK）和**冪等性設計**。」

### 訊息可靠性設計

```
訊息發送流程（帶 ACK）：

1. 客戶端發送訊息：
   {
     "client_msg_id": "uuid_12345",  // 客戶端生成的唯一 ID
     "type": "send_message",
     "to": "Bob",
     "content": "Hello"
   }

2. 伺服器收到後：
   - 檢查 client_msg_id 是否已存在（防重複）
   - 如果不存在：保存訊息，分配 server_msg_id
   - 返回 ACK：
     {
       "type": "ack",
       "client_msg_id": "uuid_12345",
       "server_msg_id": 101,
       "status": "success"
     }

3. 客戶端收到 ACK：
   - 更新訊息狀態為「已發送」
   - 如果超時未收到 ACK：重試（最多 3 次）

4. 冪等性保證：
   - 客戶端重試時，使用相同的 client_msg_id
   - 伺服器檢測到重複 ID，直接返回之前的結果
```

### 冪等性實現

```go
// internal/reliable.go
package internal

type ReliableService struct {
    db      *sql.DB
    clients map[string]*websocket.Conn
    mu      sync.RWMutex

    // 去重緩存：client_msg_id -> server_msg_id
    dedup sync.Map
}

// SendMessage 發送訊息（冪等性保證）
func (s *ReliableService) SendMessage(clientMsgID, from, to, content string) (int64, error) {
    // 1. 檢查是否重複（冪等性）
    if serverMsgID, ok := s.dedup.Load(clientMsgID); ok {
        // 重複請求，返回之前的結果
        return serverMsgID.(int64), nil
    }

    // 2. 檢查資料庫是否已存在（防止緩存失效）
    var existingID int64
    checkQuery := "SELECT id FROM messages WHERE client_msg_id = ?"
    err := s.db.QueryRow(checkQuery, clientMsgID).Scan(&existingID)
    if err == nil {
        // 已存在，返回現有 ID
        s.dedup.Store(clientMsgID, existingID)
        return existingID, nil
    }

    // 3. 保存新訊息
    query := `
        INSERT INTO messages (client_msg_id, from_user, to_user, content, created_at)
        VALUES (?, ?, ?, ?, ?)
    `

    result, err := s.db.Exec(query, clientMsgID, from, to, content, time.Now())
    if err != nil {
        return 0, err
    }

    serverMsgID, _ := result.LastInsertId()

    // 4. 緩存結果（24 小時後自動清理）
    s.dedup.Store(clientMsgID, serverMsgID)

    // 5. 推送訊息
    s.pushToClient(to, map[string]interface{}{
        "type": "new_message",
        "message": Message{
            ID:        serverMsgID,
            From:      from,
            To:        to,
            Content:   content,
            Timestamp: time.Now(),
        },
    })

    return serverMsgID, nil
}

// HandleSendMessage 處理客戶端發送訊息請求
func (s *ReliableService) HandleSendMessage(from string, msg map[string]interface{}) {
    clientMsgID, _ := msg["client_msg_id"].(string)
    to, _ := msg["to"].(string)
    content, _ := msg["content"].(string)

    serverMsgID, err := s.SendMessage(clientMsgID, from, to, content)

    // 返回 ACK
    ackMsg := map[string]interface{}{
        "type":          "ack",
        "client_msg_id": clientMsgID,
    }

    if err != nil {
        ackMsg["status"] = "error"
        ackMsg["error"] = err.Error()
    } else {
        ackMsg["status"] = "success"
        ackMsg["server_msg_id"] = serverMsgID
    }

    s.pushToClient(from, ackMsg)
}

func (s *ReliableService) pushToClient(userID string, data interface{}) {
    s.mu.RLock()
    conn, ok := s.clients[userID]
    s.mu.RUnlock()

    if !ok {
        return
    }

    conn.WriteJSON(data)
}
```

### 客戶端重試機制

```javascript
// 客戶端發送訊息（帶重試）
async function sendMessage(to, content) {
    const clientMsgID = generateUUID();
    const maxRetries = 3;
    let retries = 0;

    while (retries < maxRetries) {
        try {
            // 發送訊息
            ws.send(JSON.stringify({
                client_msg_id: clientMsgID,
                type: 'send_message',
                to: to,
                content: content
            }));

            // 等待 ACK（5 秒超時）
            const ack = await waitForAck(clientMsgID, 5000);

            if (ack.status === 'success') {
                // 成功
                updateMessageStatus(clientMsgID, 'sent', ack.server_msg_id);
                return;
            } else {
                throw new Error(ack.error);
            }
        } catch (error) {
            retries++;
            console.log(`Retry ${retries}/${maxRetries}: ${error}`);

            if (retries >= maxRetries) {
                updateMessageStatus(clientMsgID, 'failed');
                alert('訊息發送失敗，請稍後重試');
            }
        }
    }
}

function waitForAck(clientMsgID, timeout) {
    return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
            reject(new Error('ACK timeout'));
        }, timeout);

        // 監聽 ACK
        const listener = (event) => {
            const data = JSON.parse(event.data);
            if (data.type === 'ack' && data.client_msg_id === clientMsgID) {
                clearTimeout(timer);
                ws.removeEventListener('message', listener);
                resolve(data);
            }
        };

        ws.addEventListener('message', listener);
    });
}
```

---

## 第九幕：擴展性優化

**David**：「現在我們有 100 萬在線用戶，一台伺服器無法支撐。如何擴展？」

### 水平擴展架構

```
擴展架構：

                    ┌──────────────┐
                    │ Load Balancer│
                    │  (Nginx)     │
                    └──────┬───────┘
                           │
            ┌──────────────┼──────────────┐
            │              │              │
            ↓              ↓              ↓
    ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
    │ Chat Server 1│ │ Chat Server 2│ │ Chat Server 3│
    │ (WebSocket)  │ │ (WebSocket)  │ │ (WebSocket)  │
    └──────┬───────┘ └──────┬───────┘ └──────┬───────┘
           │                │                │
           └────────────────┼────────────────┘
                            │
                            ↓
                    ┌──────────────┐
                    │  Redis Pub/Sub│ ← 訊息路由
                    └──────┬───────┘
                            │
                            ↓
                    ┌──────────────┐
                    │    MySQL     │ ← 訊息存儲
                    └──────────────┘

問題：
用戶 A 連接到 Server 1
用戶 B 連接到 Server 2
A 發訊息給 B，如何路由？

解決方案：Redis Pub/Sub
```

### Redis Pub/Sub 實現

```go
// internal/cluster.go
package internal

import (
    "encoding/json"

    "github.com/go-redis/redis/v8"
)

type ClusterService struct {
    db          *sql.DB
    redisClient *redis.Client
    clients     map[string]*websocket.Conn
    mu          sync.RWMutex

    serverID string // 當前伺服器 ID
}

func NewClusterService(db *sql.DB, rdb *redis.Client, serverID string) *ClusterService {
    s := &ClusterService{
        db:          db,
        redisClient: rdb,
        clients:     make(map[string]*websocket.Conn),
        serverID:    serverID,
    }

    // 訂閱 Redis Pub/Sub（接收其他伺服器的訊息）
    go s.subscribeMessages()

    return s
}

// SendMessage 發送訊息（跨伺服器）
func (s *ClusterService) SendMessage(from, to, content string) error {
    // 1. 保存到資料庫
    query := `
        INSERT INTO messages (from_user, to_user, content, created_at)
        VALUES (?, ?, ?, ?)
    `

    result, err := s.db.Exec(query, from, to, content, time.Now())
    if err != nil {
        return err
    }

    msgID, _ := result.LastInsertId()

    msg := Message{
        ID:        msgID,
        From:      from,
        To:        to,
        Content:   content,
        Timestamp: time.Now(),
    }

    // 2. 發布到 Redis Pub/Sub（通知所有伺服器）
    payload, _ := json.Marshal(map[string]interface{}{
        "type":    "new_message",
        "to":      to,
        "message": msg,
    })

    s.redisClient.Publish(context.Background(), "chat:messages", payload)

    return nil
}

// subscribeMessages 訂閱 Redis Pub/Sub
func (s *ClusterService) subscribeMessages() {
    pubsub := s.redisClient.Subscribe(context.Background(), "chat:messages")
    defer pubsub.Close()

    ch := pubsub.Channel()

    for msg := range ch {
        var data map[string]interface{}
        if err := json.Unmarshal([]byte(msg.Payload), &data); err != nil {
            continue
        }

        // 檢查接收者是否在本伺服器
        to, _ := data["to"].(string)
        if s.isLocalClient(to) {
            // 推送給本地客戶端
            s.pushToClient(to, data)
        }
    }
}

func (s *ClusterService) isLocalClient(userID string) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()

    _, ok := s.clients[userID]
    return ok
}

func (s *ClusterService) pushToClient(userID string, data interface{}) {
    s.mu.RLock()
    conn, ok := s.clients[userID]
    s.mu.RUnlock()

    if !ok {
        return
    }

    conn.WriteJSON(data)
}
```

### 性能指標

```
單伺服器 vs 集群

單伺服器：
- 最大連線數：10,000（受限於文件描述符）
- 記憶體：10,000 × 2KB = 20 MB
- CPU：100% 時無法處理新請求

集群（10 台伺服器）：
- 最大連線數：100,000
- 記憶體：200 MB
- CPU：水平擴展，可支持更多用戶 ✅

成本：
- 單伺服器：AWS c5.xlarge = $150/月
- 集群（10 台）：$1,500/月
- 支持 100 萬 DAU（假設峰值 10% 同時在線 = 10 萬連線）
```

---

## 第十幕：真實案例 - WhatsApp 的架構

**David**：「讓我分享 WhatsApp 的真實架構。」

### WhatsApp 的架構演進

**2009 年：創立初期**

```
架構：
- Erlang（天生支持高並發）
- FreeBSD（系統優化）
- 單機支持 100 萬 WebSocket 連線

技術選擇：
- Erlang：輕量級進程（每個連線一個進程）
- 無資料庫：訊息轉發，不持久化（後來改變）
```

**2014 年：被 Facebook 收購時**

```
規模：
- 用戶數：4.5 億
- 日活：3 億
- 每日訊息：500 億條
- 伺服器：50 台（驚人的效率！）

架構：
- Erlang/OTP：訊息路由
- FreeBSD：系統優化（每台伺服器支持 200 萬連線）
- Mnesia：分布式資料庫（Erlang 自帶）
- 離線訊息：暫存在磁碟，用戶上線後推送
```

**2024 年：現在**

```
規模：
- 用戶數：20 億+
- 每日訊息：1000 億+

架構優化：
1. 多數據中心：
   - 全球分布（降低延遲）
   - 就近路由

2. 訊息加密：
   - 端到端加密（Signal Protocol）
   - 伺服器無法解密訊息內容

3. 多媒體處理：
   - 圖片/視頻上傳到 CDN
   - 訊息只傳 URL

4. 狀態同步：
   - 多設備同步（手機、電腦、Web）
   - 增量同步（只同步差異）

技術細節：
- 每台伺服器：200 萬 WebSocket 連線
- 記憶體優化：每個連線只佔用 2KB
- 網路優化：自定義協議（比 WebSocket 更高效）
```

### WhatsApp 的關鍵優化

**1. 連線管理**

```
Erlang 輕量級進程：
- 每個 WebSocket 連線一個進程
- 進程創建成本：< 1KB 記憶體
- 進程切換：微秒級

vs

傳統 Thread：
- 每個連線一個執行緒
- 執行緒創建成本：1-2 MB
- 執行緒切換：毫秒級

提升：1000 倍記憶體效率
```

**2. 訊息路由**

```
訊息路由表（記憶體中）：
user_id -> server_id

範例：
Alice -> Server_1
Bob -> Server_3

Alice 發訊息給 Bob：
1. Alice → Server_1
2. Server_1 查路由表 → Bob 在 Server_3
3. Server_1 → Server_3（內部通訊）
4. Server_3 → Bob

延遲：< 10ms
```

**3. 離線訊息**

```
離線訊息策略：
- 保存在發送者所在的伺服器（記憶體/磁碟）
- 用戶上線時，檢查所有伺服器是否有離線訊息
- 推送後刪除

優化：
- 不用中央資料庫（避免單點）
- 訊息保留時間：30 天
- 超過 30 天自動刪除
```

---

## 核心設計原則總結

### 1. 通訊協議選擇

```
┌─────────────┬──────────┬───────────┬──────────┐
│   協議      │  延遲    │  資源佔用  │  適用場景 │
├─────────────┼──────────┼───────────┼──────────┤
│ Polling     │ 0-3s     │ 高        │ 不推薦   │
│ Long Polling│ < 100ms  │ 中        │ 舊瀏覽器 │
│ WebSocket   │ < 10ms   │ 低        │ 推薦 ✅  │
└─────────────┴──────────┴───────────┴──────────┘

推薦：WebSocket（現代瀏覽器全支持）
```

### 2. 群聊 Fanout 優化

```
問題：群聊訊息需要發給所有成員

方案：並行 Fanout（goroutine）
- 序列：500ms
- 並行：10ms

提升：50 倍
```

### 3. 離線訊息

```
問題：用戶離線時訊息如何保存

方案：
- 保存到 offline_messages 表
- 用戶上線時推送
- 限制數量（1000 條）
```

### 4. 已讀回執

```
問題：如何顯示訊息狀態

方案：
- Sent：訊息已發送
- Delivered：已送達設備（客戶端 ACK）
- Read：已讀（客戶端 ACK）
```

### 5. 多設備同步

```
問題：用戶多設備登入如何同步

方案：
- 每個設備維護 cursor
- 推送給所有在線設備
- 離線設備重連後同步
```

### 6. 訊息可靠性

```
問題：如何保證不丟失、不重複

方案：
- 客戶端生成唯一 ID（client_msg_id）
- 伺服器冪等性檢查
- ACK 確認機制
- 客戶端重試（最多 3 次）
```

### 7. 水平擴展

```
問題：單伺服器無法支撐百萬用戶

方案：
- Redis Pub/Sub 訊息路由
- 負載均衡（Nginx）
- 多伺服器集群
```

---

## 延伸閱讀

### 開源項目

- **[gorilla/websocket](https://github.com/gorilla/websocket)**: Golang WebSocket 庫
- **[Socket.IO](https://github.com/socketio/socket.io)**: Node.js 實時通訊庫
- **[Centrifugo](https://github.com/centrifugal/centrifugo)**: 可擴展的實時訊息伺服器
- **[Ejabberd](https://github.com/processone/ejabberd)**: XMPP 聊天伺服器（Erlang）

### 真實案例

- **WhatsApp**: [1M WebSocket Connections](https://blog.whatsapp.com/1-million-is-so-2011)
- **Discord**: [How Discord Stores Billions of Messages](https://discord.com/blog/how-discord-stores-billions-of-messages)
- **Slack**: [Scaling Slack's Real-Time Messaging](https://slack.engineering/real-time-messaging/)

### 論文與文章

- **Signal Protocol**: 端到端加密協議
- **XMPP**: 可擴展訊息和狀態協議
- **MQTT**: 輕量級訊息協議（IoT）

### 相關章節

- **02-room-management**: WebSocket 長連接管理
- **07-message-queue**: 消息隊列（Kafka）
- **15-news-feed**: Fanout 模型
- **17-notification-service**: 推送通知

---

從「最簡單的輪詢」到「百萬級 WebSocket 集群」，Chat System 經歷了：

1. **Polling** → 延遲高、資源浪費 ❌
2. **Long Polling** → 延遲降低，資源節省 10 倍 ✅
3. **WebSocket** → 延遲 < 10ms，真正實時 ✅✅
4. **群聊 Fanout** → 並行推送，10ms 完成 ✅
5. **離線訊息** → 用戶上線自動推送 ✅
6. **已讀回執** → 雙勾顯示狀態 ✅
7. **多設備同步** → Cursor 機制 ✅
8. **訊息可靠性** → 冪等性 + ACK + 重試 ✅
9. **水平擴展** → Redis Pub/Sub 集群 ✅

**記住：即時通訊的核心是低延遲和高可靠性。WebSocket + 冪等性設計是標準方案。**

**核心理念：Real-time matters. Reliability matters more.（實時很重要，可靠性更重要）**
