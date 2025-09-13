package internal

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketHub WebSocket 連接中心
type WebSocketHub struct {
	manager     *Manager
	logger      *slog.Logger
	upgrader    websocket.Upgrader
	connections map[string]map[string]*Connection // roomID -> playerID -> Connection
	mu          sync.RWMutex
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// Connection WebSocket 連接
type Connection struct {
	PlayerID     string
	RoomID       string
	Conn         *websocket.Conn
	Send         chan []byte
	Hub          *WebSocketHub
	LastPing     time.Time
	mu           sync.Mutex
	closeOnce    sync.Once  // 確保 channel 只關閉一次
}

// NewWebSocketHub 創建 WebSocket Hub
func NewWebSocketHub(manager *Manager, logger *slog.Logger) *WebSocketHub {
	hub := &WebSocketHub{
		manager: manager,
		logger:  logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// 在生產環境應該檢查來源
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		connections: make(map[string]map[string]*Connection),
		stopCh:      make(chan struct{}),
	}

	// 啟動房間事件監聽
	hub.wg.Add(1)
	go hub.roomEventLoop()

	return hub
}

// ServeWS 處理 WebSocket 連接
func (hub *WebSocketHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// 從路徑獲取房間 ID
	roomID := r.PathValue("room_id")
	if roomID == "" {
		http.Error(w, "缺少房間 ID", http.StatusBadRequest)
		return
	}

	// 從查詢參數獲取玩家 ID
	playerID := r.URL.Query().Get("player_id")
	if playerID == "" {
		http.Error(w, "缺少玩家 ID", http.StatusBadRequest)
		return
	}

	// 驗證玩家是否在房間中
	room, err := hub.manager.GetRoom(roomID)
	if err != nil {
		http.Error(w, "房間不存在", http.StatusNotFound)
		return
	}

	room.Mu.RLock()
	_, exists := room.Players[playerID]
	room.Mu.RUnlock()
	if !exists {
		http.Error(w, "玩家不在房間中", http.StatusForbidden)
		return
	}

	// 升級為 WebSocket 連接
	conn, err := hub.upgrader.Upgrade(w, r, nil)
	if err != nil {
		hub.logger.Error("升級 WebSocket 失敗", "error", err)
		return
	}

	// 創建連接物件
	connection := &Connection{
		PlayerID: playerID,
		RoomID:   roomID,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		Hub:      hub,
		LastPing: time.Now(),
	}

	// 註冊連接
	hub.register(connection)

	// 啟動讀寫 goroutine
	go connection.writePump()
	go connection.readPump()

	hub.logger.Info("WebSocket 連接建立",
		"room_id", roomID,
		"player_id", playerID)
}

// register 註冊連接
func (hub *WebSocketHub) register(conn *Connection) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	if hub.connections[conn.RoomID] == nil {
		hub.connections[conn.RoomID] = make(map[string]*Connection)
	}

	// 關閉舊連接（如果存在）
	if oldConn, exists := hub.connections[conn.RoomID][conn.PlayerID]; exists {
		oldConn.closeOnce.Do(func() {
			close(oldConn.Send)
		})
		oldConn.Conn.Close()
	}

	hub.connections[conn.RoomID][conn.PlayerID] = conn
}

// unregister 取消註冊連接
func (hub *WebSocketHub) unregister(conn *Connection) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	if roomConns, exists := hub.connections[conn.RoomID]; exists {
		if actualConn, exists := roomConns[conn.PlayerID]; exists && actualConn == conn {
			delete(roomConns, conn.PlayerID)
			
			// 使用 sync.Once 確保 channel 只關閉一次
			conn.closeOnce.Do(func() {
				close(conn.Send)
			})

			// 如果房間沒有連接了，清理房間
			if len(roomConns) == 0 {
				delete(hub.connections, conn.RoomID)
			}
		}
	}
}

// broadcast 廣播消息到房間
func (hub *WebSocketHub) broadcast(roomID string, message []byte) {
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	if roomConns, exists := hub.connections[roomID]; exists {
		for _, conn := range roomConns {
			select {
			case conn.Send <- message:
			default:
				// 連接緩衝區滿了，關閉連接
				hub.logger.Warn("連接緩衝區滿",
					"room_id", roomID,
					"player_id", conn.PlayerID)
			}
		}
	}
}

// roomEventLoop 監聽房間事件
func (hub *WebSocketHub) roomEventLoop() {
	defer hub.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 定期檢查所有房間的事件
			hub.checkRoomEvents()
		case <-hub.stopCh:
			return
		}
	}
}

// checkRoomEvents 檢查房間事件
func (hub *WebSocketHub) checkRoomEvents() {
	hub.mu.RLock()
	roomIDs := make([]string, 0, len(hub.connections))
	for roomID := range hub.connections {
		roomIDs = append(roomIDs, roomID)
	}
	hub.mu.RUnlock()

	for _, roomID := range roomIDs {
		room, err := hub.manager.GetRoom(roomID)
		if err != nil {
			continue
		}

		// 讀取所有可用的事件
		for {
			select {
			case event := <-room.Events():
				message, err := json.Marshal(event)
				if err != nil {
					hub.logger.Error("序列化事件失敗", "error", err)
					continue
				}
				hub.broadcast(roomID, message)
			default:
				// 沒有更多事件，跳出內部循環
				goto nextRoom
			}
		}
		nextRoom:
	}
}

// Stop 停止 WebSocket Hub
func (hub *WebSocketHub) Stop() {
	close(hub.stopCh)
	hub.wg.Wait()

	// 關閉所有連接
	hub.mu.Lock()
	for _, roomConns := range hub.connections {
		for _, conn := range roomConns {
			// 先關閉 Send channel，再關閉連接
			conn.closeOnce.Do(func() {
				close(conn.Send)
			})
			conn.Conn.Close()
		}
	}
	hub.connections = make(map[string]map[string]*Connection)
	hub.mu.Unlock()

	hub.logger.Info("WebSocket Hub 已停止")
}

// readPump 讀取客戶端消息
func (c *Connection) readPump() {
	defer func() {
		c.Hub.unregister(c)
		c.Conn.Close()
	}()

	// 設置讀取參數
	if err := c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		c.Hub.logger.Error("設置讀取期限失敗", "error", err)
	}
	c.Conn.SetPongHandler(func(string) error {
		if err := c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			c.Hub.logger.Error("設置讀取期限失敗", "error", err)
		}
		c.mu.Lock()
		c.LastPing = time.Now()
		c.mu.Unlock()
		return nil
	})

	for {
		// 讀取消息
		messageType, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Hub.logger.Error("WebSocket 讀取錯誤",
					"error", err,
					"room_id", c.RoomID,
					"player_id", c.PlayerID)
			}
			break
		}

		// 處理消息（如果是文本消息）
		if messageType == websocket.TextMessage {
			c.handleMessage(message)
		}
	}
}

// writePump 寫入消息到客戶端
func (c *Connection) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				c.Hub.logger.Error("設置寫入期限失敗", "error", err)
			}
			if !ok {
				// Hub 關閉了通道，優雅關閉連接
				// 設置關閉期限
				deadline := time.Now().Add(time.Second)
				if err := c.Conn.SetWriteDeadline(deadline); err == nil {
					// 嘗試發送關閉消息，忽略錯誤（連接可能已關閉）
					_ = c.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				}
				return
			}

			// 寫入消息
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

			// 批量發送隊列中的消息
			n := len(c.Send)
			for i := 0; i < n; i++ {
				if err := c.Conn.WriteMessage(websocket.TextMessage, <-c.Send); err != nil {
					c.Hub.logger.Error("發送消息失敗", "error", err)
					return
				}
			}

		case <-ticker.C:
			// 發送 ping
			if err := c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				c.Hub.logger.Error("設置寫入期限失敗", "error", err)
			}
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage 處理客戶端消息
func (c *Connection) handleMessage(message []byte) {
	// 簡單的消息處理
	var msg map[string]any
	if err := json.Unmarshal(message, &msg); err != nil {
		c.Hub.logger.Error("解析客戶端消息失敗",
			"error", err,
			"room_id", c.RoomID,
			"player_id", c.PlayerID)
		return
	}

	// 根據消息類型處理
	if msgType, ok := msg["type"].(string); ok {
		switch msgType {
		case "ping":
			// 回應 pong
			response, _ := json.Marshal(map[string]string{
				"type": "pong",
			})
			select {
			case c.Send <- response:
			default:
			}
		case "chat":
			// 廣播聊天消息（簡單示例）
			if text, ok := msg["text"].(string); ok {
				chatMsg := map[string]any{
					"event": "chat_message",
					"data": map[string]any{
						"player_id": c.PlayerID,
						"text":      text,
						"timestamp": time.Now().Unix(),
					},
				}
				if data, err := json.Marshal(chatMsg); err == nil {
					c.Hub.broadcast(c.RoomID, data)
				}
			}
		default:
			c.Hub.logger.Debug("收到未知消息類型",
				"type", msgType,
				"room_id", c.RoomID,
				"player_id", c.PlayerID)
		}
	}
}

// DisconnectPlayer 斷開玩家連接
func (hub *WebSocketHub) DisconnectPlayer(roomID, playerID string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	if roomConns, exists := hub.connections[roomID]; exists {
		if conn, exists := roomConns[playerID]; exists {
			// 先關閉 Send channel，再關閉連接
			conn.closeOnce.Do(func() {
				close(conn.Send)
			})
			conn.Conn.Close()
			delete(roomConns, playerID)
			if len(roomConns) == 0 {
				delete(hub.connections, roomID)
			}
		}
	}
}

// GetConnectionCount 獲取連接數
func (hub *WebSocketHub) GetConnectionCount() map[string]int {
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	result := make(map[string]int)
	for roomID, conns := range hub.connections {
		result[roomID] = len(conns)
	}
	return result
}