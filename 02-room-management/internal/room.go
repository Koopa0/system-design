package internal

import (
	"fmt"
	"sync"
	"time"
)

// RoomStatus 房間狀態
type RoomStatus string

const (
	StatusWaiting   RoomStatus = "waiting"   // 等待玩家加入
	StatusPreparing RoomStatus = "preparing" // 所有玩家到齊，選擇歌曲中
	StatusReady     RoomStatus = "ready"     // 所有玩家已準備
	StatusPlaying   RoomStatus = "playing"   // 遊戲進行中
	StatusFinished  RoomStatus = "finished"  // 遊戲結束
	StatusClosed    RoomStatus = "closed"    // 房間關閉
)

// GameMode 遊戲模式
type GameMode string

const (
	ModeCoop     GameMode = "coop"     // 合作模式
	ModeVersus   GameMode = "versus"   // 對戰模式
	ModePractice GameMode = "practice" // 練習模式
)

// Player 玩家資訊
type Player struct {
	ID       string    `json:"player_id"`
	Name     string    `json:"player_name"`
	IsHost   bool      `json:"is_host"`
	IsReady  bool      `json:"is_ready"`
	JoinedAt time.Time `json:"joined_at"`
}

// Song 歌曲資訊
type Song struct {
	ID         string `json:"song_id"`
	Name       string `json:"song_name"`
	Difficulty string `json:"difficulty"`
	Duration   int    `json:"duration"` // 秒數
}

// Room 遊戲房間
type Room struct {
	ID          string     `json:"room_id"`
	Name        string     `json:"room_name"`
	JoinCode    string     `json:"join_code"`
	MaxPlayers  int        `json:"max_players"`
	Password    string     `json:"-"` // 不序列化密碼
	HasPassword bool       `json:"has_password"`
	GameMode    GameMode   `json:"game_mode"`
	Difficulty  string     `json:"difficulty"`
	Status      RoomStatus `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	Players      map[string]*Player `json:"players"`
	SelectedSong *Song              `json:"selected_song,omitempty"`
	HostID       string             `json:"host_id"`

	Mu         sync.RWMutex `json:"-"`
	events     chan Event   // 事件通道
	lastActive time.Time    // 最後活動時間
}

// Event 房間事件
type Event struct {
	Type string `json:"event"`
	Data any    `json:"data"`
}

// NewRoom 創建新房間
func NewRoom(id, name, joinCode string, maxPlayers int, password string, mode GameMode, difficulty string) *Room {
	now := time.Now()
	return &Room{
		ID:          id,
		Name:        name,
		JoinCode:    joinCode,
		MaxPlayers:  maxPlayers,
		Password:    password,
		HasPassword: password != "",
		GameMode:    mode,
		Difficulty:  difficulty,
		Status:      StatusWaiting,
		CreatedAt:   now,
		UpdatedAt:   now,
		Players:     make(map[string]*Player),
		events:      make(chan Event, 100),
		lastActive:  now,
	}
}

// AddPlayer 加入玩家
func (r *Room) AddPlayer(playerID, playerName string) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	// 檢查房間狀態
	if r.Status != StatusWaiting && r.Status != StatusPreparing {
		return fmt.Errorf("房間狀態不允許加入: %s", r.Status)
	}

	// 檢查人數上限
	if len(r.Players) >= r.MaxPlayers {
		return fmt.Errorf("房間已滿")
	}

	// 檢查是否已在房間內
	if _, exists := r.Players[playerID]; exists {
		return fmt.Errorf("玩家已在房間內")
	}

	// 創建玩家
	player := &Player{
		ID:       playerID,
		Name:     playerName,
		IsHost:   len(r.Players) == 0, // 第一個玩家成為房主
		IsReady:  false,
		JoinedAt: time.Now(),
	}

	r.Players[playerID] = player
	if player.IsHost {
		r.HostID = playerID
	}

	// 更新房間狀態
	if len(r.Players) == r.MaxPlayers && r.Status == StatusWaiting {
		r.Status = StatusPreparing
	}

	r.lastActive = time.Now()
	r.UpdatedAt = time.Now()

	// 發送事件
	r.sendEvent(Event{
		Type: "player_joined",
		Data: map[string]any{
			"player":          player,
			"current_players": len(r.Players),
		},
	})

	return nil
}

// RemovePlayer 移除玩家
func (r *Room) RemovePlayer(playerID string) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	player, exists := r.Players[playerID]
	if !exists {
		return fmt.Errorf("玩家不在房間內")
	}

	delete(r.Players, playerID)
	r.lastActive = time.Now()
	r.UpdatedAt = time.Now()

	// 如果是房主離開，轉移房主
	newHostID := ""
	if player.IsHost && len(r.Players) > 0 {
		// 找到加入時間最早的玩家
		var earliestPlayer *Player
		for _, p := range r.Players {
			if earliestPlayer == nil || p.JoinedAt.Before(earliestPlayer.JoinedAt) {
				earliestPlayer = p
			}
		}
		if earliestPlayer != nil {
			earliestPlayer.IsHost = true
			r.HostID = earliestPlayer.ID
			newHostID = earliestPlayer.ID
		}
	}

	// 更新房間狀態
	// 空房間不立即關閉，而是等待過期機制處理
	if len(r.Players) > 0 && r.Status == StatusPreparing && len(r.Players) < r.MaxPlayers {
		r.Status = StatusWaiting
	}

	// 發送事件
	eventData := map[string]any{
		"player_id": playerID,
	}
	if newHostID != "" {
		eventData["new_host"] = newHostID
	}

	r.sendEvent(Event{
		Type: "player_left",
		Data: eventData,
	})

	return nil
}

// SetPlayerReady 設置玩家準備狀態
func (r *Room) SetPlayerReady(playerID string, isReady bool) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	player, exists := r.Players[playerID]
	if !exists {
		return fmt.Errorf("玩家不在房間內")
	}

	// 檢查房間狀態
	if r.Status != StatusPreparing {
		return fmt.Errorf("當前狀態不能準備: %s", r.Status)
	}

	// 必須先選歌
	if r.SelectedSong == nil {
		return fmt.Errorf("尚未選擇歌曲")
	}

	player.IsReady = isReady
	r.lastActive = time.Now()
	r.UpdatedAt = time.Now()

	// 檢查是否所有玩家都準備好了
	allReady := true
	for _, p := range r.Players {
		if !p.IsReady {
			allReady = false
			break
		}
	}

	if allReady && len(r.Players) == r.MaxPlayers {
		r.Status = StatusReady
	}

	// 發送事件
	r.sendEvent(Event{
		Type: "player_ready_changed",
		Data: map[string]any{
			"player_id": playerID,
			"is_ready":  isReady,
		},
	})

	return nil
}

// SelectSong 選擇歌曲（只有房主可以）
func (r *Room) SelectSong(playerID string, song *Song) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	// 檢查是否是房主
	if r.HostID != playerID {
		return fmt.Errorf("只有房主可以選歌")
	}

	// 檢查房間狀態
	if r.Status != StatusPreparing {
		return fmt.Errorf("當前狀態不能選歌: %s", r.Status)
	}

	r.SelectedSong = song
	r.lastActive = time.Now()
	r.UpdatedAt = time.Now()

	// 重置所有玩家的準備狀態
	for _, p := range r.Players {
		p.IsReady = false
	}

	// 發送事件
	r.sendEvent(Event{
		Type: "song_selected",
		Data: map[string]any{
			"song": song,
		},
	})

	return nil
}

// StartGame 開始遊戲（只有房主可以）
func (r *Room) StartGame(playerID string) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	// 檢查是否是房主
	if r.HostID != playerID {
		return fmt.Errorf("只有房主可以開始遊戲")
	}

	// 檢查房間狀態
	if r.Status != StatusReady {
		return fmt.Errorf("房間尚未準備好")
	}

	r.Status = StatusPlaying
	r.lastActive = time.Now()
	r.UpdatedAt = time.Now()

	// 發送事件
	r.sendEvent(Event{
		Type: "game_starting",
		Data: map[string]any{
			"countdown": 3,
		},
	})

	return nil
}

// EndGame 結束遊戲
func (r *Room) EndGame() {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	r.Status = StatusFinished
	r.lastActive = time.Now()
	r.UpdatedAt = time.Now()

	r.sendEvent(Event{
		Type: "game_ended",
		Data: map[string]any{},
	})
}

// Close 關閉房間
func (r *Room) Close(reason string) {
	r.Mu.Lock()
	
	if r.Status == StatusClosed {
		r.Mu.Unlock()
		return
	}
	
	r.Status = StatusClosed
	r.UpdatedAt = time.Now()
	
	// 發送關閉事件前先釋放鎖，避免死鎖
	r.Mu.Unlock()
	
	// 使用非阻塞發送
	event := Event{
		Type: "room_closed",
		Data: map[string]any{
			"reason": reason,
		},
	}
	
	select {
	case r.events <- event:
		// 成功發送
	case <-time.After(100 * time.Millisecond):
		// 超時
	}
	
	// 給接收者一點時間處理事件
	time.Sleep(10 * time.Millisecond)
	close(r.events)
}

// IsExpired 檢查房間是否過期
func (r *Room) IsExpired() bool {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	// 已關閉的房間視為過期
	if r.Status == StatusClosed {
		return true
	}

	now := time.Now()
	
	// 房間最多存在 30 分鐘
	if now.Sub(r.CreatedAt) > 30*time.Minute {
		return true
	}

	// 無人房間 5 分鐘後過期
	if len(r.Players) == 0 && now.Sub(r.lastActive) > 5*time.Minute {
		return true
	}

	return false
}

// GetState 獲取房間狀態（用於序列化）
func (r *Room) GetState() map[string]any {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	players := make([]*Player, 0, len(r.Players))
	for _, p := range r.Players {
		players = append(players, p)
	}

	return map[string]any{
		"room_id":       r.ID,
		"room_name":     r.Name,
		"join_code":     r.JoinCode,
		"max_players":   r.MaxPlayers,
		"has_password":  r.HasPassword,
		"game_mode":     r.GameMode,
		"difficulty":    r.Difficulty,
		"status":        r.Status,
		"players":       players,
		"selected_song": r.SelectedSong,
		"host_id":       r.HostID,
		"created_at":    r.CreatedAt,
		"updated_at":    r.UpdatedAt,
	}
}

// Events 獲取事件通道
func (r *Room) Events() <-chan Event {
	return r.events
}

// sendEvent 發送事件（內部使用，需要持有鎖）
func (r *Room) sendEvent(event Event) {
	select {
	case r.events <- event:
	default:
		// 通道滿了，丟棄事件（簡單處理）
	}
}

// ValidatePassword 驗證密碼
func (r *Room) ValidatePassword(password string) bool {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	return r.Password == "" || r.Password == password
}

// GetPlayerCount 獲取玩家數量
func (r *Room) GetPlayerCount() int {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	return len(r.Players)
}

// GetHostName 獲取房主名稱
func (r *Room) GetHostName() string {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	
	if host, exists := r.Players[r.HostID]; exists {
		return host.Name
	}
	return ""
}