package internal

import (
	"fmt"
	"sync"
	"time"
)

// 系統設計問題：
//   如何管理多人遊戲房間的生命週期，處理並發操作，並實時同步狀態？
//
// 核心挑戰：
//   1. 狀態管理：房間有複雜的狀態轉換（waiting → preparing → ready → playing）
//   2. 並發控制：多個玩家同時操作（加入、準備、選歌）
//   3. 實時通信：狀態變更需要立即通知所有玩家
//   4. 資源回收：空閒房間自動關閉（避免內存洩漏）
//
// 設計方案：
//   ✅ 有限狀態機（FSM）- 規範狀態轉換
//   ✅ RWMutex - 讀多寫少優化
//   ✅ 事件驅動 - 狀態變更異步通知
//   ✅ 超時機制 - 自動清理空閒房間

// RoomStatus 房間狀態
//
// 有限狀態機設計：
//
//	waiting → preparing → ready → playing → finished → closed
//	           ↑____________↓
//
// 狀態轉換規則：
//   - waiting → preparing：所有玩家到齊
//   - preparing → ready：房主選歌 + 所有玩家準備
//   - ready → playing：房主開始遊戲
//   - playing → finished：遊戲結束
//   - 任何狀態 → closed：房主解散 / 超時
//
// 為什麼需要狀態機？
//   - 防止非法操作（如遊戲中途加人）
//   - 清晰的生命週期管理
//   - 簡化錯誤處理
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
//
// 系統設計考量：
//
//  1. 並發控制（RWMutex）：
//     問題：多個玩家同時操作同一房間（加入、準備、選歌）
//     方案：sync.RWMutex（讀寫鎖）
//     優勢：
//     - 讀操作並發（查詢房間狀態、獲取玩家列表）
//     - 寫操作互斥（加入玩家、改變狀態）
//     - 性能：讀多寫少場景優化（相比 Mutex）
//
//  2. 事件驅動架構（events chan）：
//     問題：房間狀態改變需要通知所有連接的客戶端
//     方案：事件通道 + WebSocket 廣播
//     流程：
//     操作（如玩家加入）→ 修改狀態 → 發送事件 → WebSocket 廣播
//     優勢：
//     - 解耦：業務邏輯與通知邏輯分離
//     - 異步：不阻塞主流程
//     - 緩衝：channel 緩衝 100 個事件（應對突發）
//
//  3. 資源管理（lastActive）：
//     問題：空閒房間佔用內存（玩家全部離開但房間未關閉）
//     方案：超時自動清理
//     策略：
//     - 追蹤最後活動時間
//     - 定期掃描（如每分鐘）
//     - 超過閾值（如 30 分鐘）自動關閉
//
// 4. 容量規劃：
//   - 單房間最大玩家：4 人（根據遊戲設計）
//   - 事件緩衝：100 個（應對快速操作）
//   - 超時閾值：30 分鐘（平衡體驗與資源）
type Room struct {
	ID          string     `json:"room_id"`
	Name        string     `json:"room_name"`
	JoinCode    string     `json:"join_code"` // 簡短加入碼（如 "ABC123"）
	MaxPlayers  int        `json:"max_players"`
	Password    string     `json:"-"` // 不序列化（安全）
	HasPassword bool       `json:"has_password"`
	GameMode    GameMode   `json:"game_mode"`
	Difficulty  string     `json:"difficulty"`
	Status      RoomStatus `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	Players      map[string]*Player `json:"players"`
	SelectedSong *Song              `json:"selected_song,omitempty"`
	HostID       string             `json:"host_id"` // 房主有特殊權限

	Mu         sync.RWMutex `json:"-"` // 讀寫鎖（並發控制）
	events     chan Event   // 事件通道（異步通知）
	lastActive time.Time    // 最後活動時間（資源回收）
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
//
// 系統設計重點：
//
// 1. 並發安全（寫鎖）：
//   - 使用 Lock（寫鎖）而非 RLock（讀鎖）
//   - 修改房間狀態需要排他訪問
//   - defer Unlock 確保異常時也釋放鎖
//
// 2. 狀態機驗證：
//   - 只允許在 waiting/preparing 狀態加入
//   - playing/finished 狀態不允許（遊戲已開始/結束）
//   - 防止非法操作（系統設計核心）
//
// 3. 狀態自動轉換：
//   - waiting → preparing（人滿自動轉換）
//   - 體現狀態機的自動化
//
// 4. 事件通知：
//   - 操作完成後發送事件
//   - 異步通知所有客戶端（WebSocket 廣播）
//   - 不阻塞主流程（channel 緩衝）
func (r *Room) AddPlayer(playerID, playerName string) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	// 狀態檢查（狀態機驗證）
	if r.Status != StatusWaiting && r.Status != StatusPreparing {
		return fmt.Errorf("房間狀態不允許加入: %s", r.Status)
	}

	// 容量檢查
	if len(r.Players) >= r.MaxPlayers {
		return fmt.Errorf("房間已滿")
	}

	// 重複檢查（冪等性）
	if _, exists := r.Players[playerID]; exists {
		return fmt.Errorf("玩家已在房間內")
	}

	// 創建玩家
	player := &Player{
		ID:       playerID,
		Name:     playerName,
		IsHost:   len(r.Players) == 0, // 第一個玩家自動成為房主
		IsReady:  false,
		JoinedAt: time.Now(),
	}

	r.Players[playerID] = player
	if player.IsHost {
		r.HostID = playerID
	}

	// 狀態自動轉換（狀態機）
	if len(r.Players) == r.MaxPlayers && r.Status == StatusWaiting {
		r.Status = StatusPreparing // 人滿 → 準備選歌
	}

	r.lastActive = time.Now()
	r.UpdatedAt = time.Now()

	// 異步事件通知（事件驅動）
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
//
// 系統設計考量：
//   - 檢查房間是否已關閉，避免 send on closed channel panic
//   - 非阻塞發送（使用 select default），避免慢消費者阻塞操作
//   - 如果通道滿或已關閉，丟棄事件（優先保證操作成功）
func (r *Room) sendEvent(event Event) {
	// 檢查房間是否已關閉（防止 panic）
	if r.Status == StatusClosed {
		return
	}

	select {
	case r.events <- event:
	default:
		// 通道滿了或已關閉，丟棄事件（簡單處理）
		// 生產環境應該：記錄日誌、監控丟失率
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
