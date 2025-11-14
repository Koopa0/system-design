package internal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Manager 房間管理器
type Manager struct {
	rooms      map[string]*Room  // roomID -> Room
	joinCodes  map[string]string // joinCode -> roomID
	playerRoom map[string]string // playerID -> roomID
	mu         sync.RWMutex
	logger     *slog.Logger
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewManager 創建房間管理器
func NewManager(logger *slog.Logger) *Manager {
	m := &Manager{
		rooms:      make(map[string]*Room),
		joinCodes:  make(map[string]string),
		playerRoom: make(map[string]string),
		logger:     logger,
		stopCh:     make(chan struct{}),
	}

	// 啟動清理 goroutine
	m.wg.Add(1)
	go m.cleanupLoop()

	return m
}

// CreateRoom 創建房間
func (m *Manager) CreateRoom(name string, maxPlayers int, password string, gameMode GameMode, difficulty string) (*Room, error) {
	// 驗證參數
	if maxPlayers < 2 || maxPlayers > 100 {
		return nil, fmt.Errorf("玩家數量必須在 2-100 之間")
	}

	// 生成 ID 和加入碼
	roomID := m.generateID("room")
	joinCode := m.generateJoinCode()

	// 創建房間
	room := NewRoom(roomID, name, joinCode, maxPlayers, password, gameMode, difficulty)

	m.mu.Lock()
	m.rooms[roomID] = room
	m.joinCodes[joinCode] = roomID
	m.mu.Unlock()

	m.logger.Info("房間已創建",
		"room_id", roomID,
		"join_code", joinCode,
		"name", name,
		"max_players", maxPlayers,
		"mode", gameMode)

	return room, nil
}

// GetRoom 獲取房間
func (m *Manager) GetRoom(roomID string) (*Room, error) {
	m.mu.RLock()
	room, exists := m.rooms[roomID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("房間不存在: %s", roomID)
	}

	return room, nil
}

// GetRoomByJoinCode 通過加入碼獲取房間
func (m *Manager) GetRoomByJoinCode(joinCode string) (*Room, error) {
	m.mu.RLock()
	roomID, exists := m.joinCodes[strings.ToUpper(joinCode)]
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("無效的加入碼: %s", joinCode)
	}
	room := m.rooms[roomID]
	m.mu.RUnlock()

	return room, nil
}

// JoinRoom 加入房間
func (m *Manager) JoinRoom(roomID, playerID, playerName, password string) error {
	// 檢查玩家是否已在其他房間
	m.mu.RLock()
	if existingRoomID, exists := m.playerRoom[playerID]; exists {
		m.mu.RUnlock()
		return fmt.Errorf("玩家已在房間 %s 中", existingRoomID)
	}
	m.mu.RUnlock()

	// 獲取房間
	room, err := m.GetRoom(roomID)
	if err != nil {
		return fmt.Errorf("房間不存在: %s", roomID)
	}

	// 驗證密碼
	if !room.ValidatePassword(password) {
		return fmt.Errorf("密碼錯誤")
	}

	// 加入房間
	if err := room.AddPlayer(playerID, playerName); err != nil {
		return err
	}

	// 記錄玩家所在房間
	m.mu.Lock()
	m.playerRoom[playerID] = roomID
	m.mu.Unlock()

	m.logger.Info("玩家加入房間",
		"room_id", roomID,
		"player_id", playerID,
		"player_name", playerName)

	return nil
}

// LeaveRoom 離開房間
func (m *Manager) LeaveRoom(roomID, playerID string) error {
	room, err := m.GetRoom(roomID)
	if err != nil {
		return fmt.Errorf("房間不存在: %s", roomID)
	}

	// 從房間移除玩家
	if err := room.RemovePlayer(playerID); err != nil {
		return err
	}

	// 清除玩家房間記錄
	m.mu.Lock()
	delete(m.playerRoom, playerID)
	m.mu.Unlock()

	m.logger.Info("玩家離開房間",
		"room_id", roomID,
		"player_id", playerID)

	// 不要在這裡移除房間，讓清理機制處理
	// 房間會在過期後自動清理（空房間5分鐘，總時長30分鐘）

	return nil
}

// SetPlayerReady 設置玩家準備狀態
func (m *Manager) SetPlayerReady(roomID, playerID string, isReady bool) error {
	room, err := m.GetRoom(roomID)
	if err != nil {
		return fmt.Errorf("房間不存在: %s", roomID)
	}

	return room.SetPlayerReady(playerID, isReady)
}

// SelectSong 選擇歌曲
func (m *Manager) SelectSong(roomID, playerID string, song *Song) error {
	room, err := m.GetRoom(roomID)
	if err != nil {
		return fmt.Errorf("房間不存在: %s", roomID)
	}

	return room.SelectSong(playerID, song)
}

// StartGame 開始遊戲
func (m *Manager) StartGame(roomID, playerID string) error {
	room, err := m.GetRoom(roomID)
	if err != nil {
		return fmt.Errorf("房間不存在: %s", roomID)
	}

	return room.StartGame(playerID)
}

// ListRooms 列出房間
func (m *Manager) ListRooms(status RoomStatus, mode GameMode, page, limit int) ([]map[string]any, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 收集符合條件的房間
	var filtered []*Room
	for _, room := range m.rooms {
		// 過濾狀態
		if status != "" && room.Status != status {
			continue
		}
		// 過濾模式
		if mode != "" && room.GameMode != mode {
			continue
		}
		filtered = append(filtered, room)
	}

	total := len(filtered)

	// 分頁
	start := (page - 1) * limit
	end := start + limit
	if start >= total {
		return []map[string]any{}, total
	}
	if end > total {
		end = total
	}

	// 構建結果
	result := make([]map[string]any, 0, end-start)
	for i := start; i < end; i++ {
		room := filtered[i]
		result = append(result, map[string]any{
			"room_id":         room.ID,
			"room_name":       room.Name,
			"current_players": room.GetPlayerCount(),
			"max_players":     room.MaxPlayers,
			"status":          room.Status,
			"has_password":    room.HasPassword,
			"game_mode":       room.GameMode,
			"host_name":       room.GetHostName(),
		})
	}

	return result, total
}

// GetPlayerRoom 獲取玩家所在房間
func (m *Manager) GetPlayerRoom(playerID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	roomID, exists := m.playerRoom[playerID]
	return roomID, exists
}

// cleanupLoop 清理過期房間
func (m *Manager) cleanupLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanup()
		case <-m.stopCh:
			return
		}
	}
}

// Cleanup 執行清理（公開方法供測試使用）
func (m *Manager) Cleanup() {
	m.cleanup()
}

// cleanup 執行清理
func (m *Manager) cleanup() {
	m.mu.RLock()
	var toRemove []string
	for roomID, room := range m.rooms {
		if room.IsExpired() {
			toRemove = append(toRemove, roomID)
		}
	}
	m.mu.RUnlock()

	// 移除過期房間
	for _, roomID := range toRemove {
		m.mu.RLock()
		room := m.rooms[roomID]
		m.mu.RUnlock()

		if room != nil {
			room.Close("timeout")
			m.removeRoom(roomID)
			m.logger.Info("房間已過期清理", "room_id", roomID)
		}
	}
}

// removeRoom 移除房間（內部使用）
func (m *Manager) removeRoom(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	room, exists := m.rooms[roomID]
	if !exists {
		return
	}

	// 清理加入碼
	delete(m.joinCodes, room.JoinCode)

	// 清理玩家記錄
	for playerID := range room.Players {
		delete(m.playerRoom, playerID)
	}

	// 移除房間
	delete(m.rooms, roomID)

	m.logger.Info("房間已移除", "room_id", roomID)
}

// Stop 停止管理器
func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()

	// 關閉所有房間
	m.mu.Lock()
	for _, room := range m.rooms {
		room.Close("server_shutdown")
	}
	m.mu.Unlock()

	m.logger.Info("房間管理器已停止")
}

// generateID 生成唯一 ID
func (m *Manager) generateID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// 如果隨機讀取失敗，使用時間戳作為備用
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}

// generateJoinCode 生成簡短的加入碼
func (m *Manager) generateJoinCode() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = chars[randInt(len(chars))]
	}
	return string(b)
}

// randInt 生成隨機數
func randInt(max int) int {
	b := make([]byte, 1)
	if _, err := rand.Read(b); err != nil {
		// 如果隨機讀取失敗，使用時間作為隨機源
		return int(time.Now().UnixNano()) % max
	}
	return int(b[0]) % max
}

// Stats 獲取統計資訊
func (m *Manager) Stats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statusCount := make(map[RoomStatus]int)
	modeCount := make(map[GameMode]int)
	totalPlayers := 0

	for _, room := range m.rooms {
		statusCount[room.Status]++
		modeCount[room.GameMode]++
		totalPlayers += room.GetPlayerCount()
	}

	return map[string]any{
		"total_rooms":   len(m.rooms),
		"total_players": totalPlayers,
		"by_status":     statusCount,
		"by_mode":       modeCount,
	}
}
