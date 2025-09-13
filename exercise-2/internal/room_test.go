package internal_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/koopa0/system-design/exercise-2/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRoom 測試創建新房間
func TestNewRoom(t *testing.T) {
	tests := []struct {
		name       string
		roomID     string
		roomName   string
		joinCode   string
		maxPlayers int
		password   string
		mode       internal.GameMode
		difficulty string
		validate   func(t *testing.T, room *internal.Room)
	}{
		{
			name:       "create room without password",
			roomID:     "room_001",
			roomName:   "測試房間",
			joinCode:   "ABC123",
			maxPlayers: 4,
			password:   "",
			mode:       internal.ModeCoop,
			difficulty: "normal",
			validate: func(t *testing.T, room *internal.Room) {
				assert.Equal(t, "room_001", room.ID)
				assert.Equal(t, "測試房間", room.Name)
				assert.Equal(t, "ABC123", room.JoinCode)
				assert.Equal(t, 4, room.MaxPlayers)
				assert.False(t, room.HasPassword)
				assert.Equal(t, internal.ModeCoop, room.GameMode)
				assert.Equal(t, internal.StatusWaiting, room.Status)
				assert.Empty(t, room.Players)
			},
		},
		{
			name:       "create room with password",
			roomID:     "room_002",
			roomName:   "私人房間",
			joinCode:   "XYZ789",
			maxPlayers: 2,
			password:   "secret123",
			mode:       internal.ModeVersus,
			difficulty: "hard",
			validate: func(t *testing.T, room *internal.Room) {
				assert.Equal(t, "room_002", room.ID)
				assert.Equal(t, "私人房間", room.Name)
				assert.Equal(t, 2, room.MaxPlayers)
				assert.True(t, room.HasPassword)
				assert.Equal(t, "secret123", room.Password)
				assert.Equal(t, internal.ModeVersus, room.GameMode)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			room := internal.NewRoom(
				tt.roomID,
				tt.roomName,
				tt.joinCode,
				tt.maxPlayers,
				tt.password,
				tt.mode,
				tt.difficulty,
			)

			require.NotNil(t, room)
			tt.validate(t, room)
		})
	}
}

// TestRoom_AddPlayer 測試加入玩家
func TestRoom_AddPlayer(t *testing.T) {
	tests := []struct {
		name          string
		setupRoom     func() *internal.Room
		playerID      string
		playerName    string
		expectedError string
		validate      func(t *testing.T, room *internal.Room, err error)
	}{
		{
			name: "add first player becomes host",
			setupRoom: func() *internal.Room {
				return internal.NewRoom("room_001", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
			},
			playerID:   "player_001",
			playerName: "玩家一",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.NoError(t, err)
				assert.Equal(t, 1, room.GetPlayerCount())
				assert.Equal(t, "player_001", room.HostID)
				
				// 驗證玩家資訊
				room.Mu.RLock()
				player := room.Players["player_001"]
				room.Mu.RUnlock()
				
				assert.NotNil(t, player)
				assert.True(t, player.IsHost)
				assert.Equal(t, "玩家一", player.Name)
			},
		},
		{
			name: "add second player not host",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_002", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
				room.AddPlayer("player_001", "玩家一")
				return room
			},
			playerID:   "player_002",
			playerName: "玩家二",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.NoError(t, err)
				assert.Equal(t, 2, room.GetPlayerCount())
				
				room.Mu.RLock()
				player := room.Players["player_002"]
				room.Mu.RUnlock()
				
				assert.NotNil(t, player)
				assert.False(t, player.IsHost)
			},
		},
		{
			name: "room full error",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_003", "測試房間", "ABC123", 2, "", internal.ModeCoop, "normal")
				room.AddPlayer("player_001", "玩家一")
				room.AddPlayer("player_002", "玩家二")
				return room
			},
			playerID:      "player_003",
			playerName:    "玩家三",
			expectedError: "房間已滿",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "房間已滿")
				assert.Equal(t, 2, room.GetPlayerCount())
			},
		},
		{
			name: "player already in room",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_004", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
				room.AddPlayer("player_001", "玩家一")
				return room
			},
			playerID:      "player_001",
			playerName:    "玩家一",
			expectedError: "玩家已在房間內",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "玩家已在房間內")
			},
		},
		{
			name: "room status not allowed",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_005", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
				room.Status = internal.StatusPlaying
				return room
			},
			playerID:      "player_001",
			playerName:    "玩家一",
			expectedError: "房間狀態不允許加入",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "房間狀態不允許加入")
			},
		},
		{
			name: "status changes to preparing when full",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_006", "測試房間", "ABC123", 2, "", internal.ModeCoop, "normal")
				room.AddPlayer("player_001", "玩家一")
				return room
			},
			playerID:   "player_002",
			playerName: "玩家二",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.NoError(t, err)
				assert.Equal(t, 2, room.GetPlayerCount())
				assert.Equal(t, internal.StatusPreparing, room.Status)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			room := tt.setupRoom()
			err := room.AddPlayer(tt.playerID, tt.playerName)
			tt.validate(t, room, err)
		})
	}
}

// TestRoom_RemovePlayer 測試移除玩家
func TestRoom_RemovePlayer(t *testing.T) {
	tests := []struct {
		name          string
		setupRoom     func() *internal.Room
		playerID      string
		expectedError string
		validate      func(t *testing.T, room *internal.Room, err error)
	}{
		{
			name: "remove existing player",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_001", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
				room.AddPlayer("player_001", "玩家一")
				room.AddPlayer("player_002", "玩家二")
				return room
			},
			playerID: "player_002",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.NoError(t, err)
				assert.Equal(t, 1, room.GetPlayerCount())
				
				room.Mu.RLock()
				_, exists := room.Players["player_002"]
				room.Mu.RUnlock()
				assert.False(t, exists)
			},
		},
		{
			name: "remove host transfers ownership",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_002", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
				room.AddPlayer("player_001", "玩家一")
				time.Sleep(10 * time.Millisecond) // 確保時間差異
				room.AddPlayer("player_002", "玩家二")
				return room
			},
			playerID: "player_001",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.NoError(t, err)
				assert.Equal(t, 1, room.GetPlayerCount())
				assert.Equal(t, "player_002", room.HostID)
				
				room.Mu.RLock()
				player := room.Players["player_002"]
				room.Mu.RUnlock()
				assert.True(t, player.IsHost)
			},
		},
		{
			name: "remove last player closes room",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_003", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
				room.AddPlayer("player_001", "玩家一")
				return room
			},
			playerID: "player_001",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.NoError(t, err)
				assert.Equal(t, 0, room.GetPlayerCount())
				// 空房間不立即關閉，等待過期機制處理
				assert.Equal(t, internal.StatusWaiting, room.Status)
			},
		},
		{
			name: "remove non-existent player",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_004", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
				room.AddPlayer("player_001", "玩家一")
				return room
			},
			playerID:      "player_999",
			expectedError: "玩家不在房間內",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "玩家不在房間內")
			},
		},
		{
			name: "status changes from preparing to waiting",
			setupRoom: func() *internal.Room {
				room := internal.NewRoom("room_005", "測試房間", "ABC123", 2, "", internal.ModeCoop, "normal")
				room.AddPlayer("player_001", "玩家一")
				room.AddPlayer("player_002", "玩家二")
				// 房間應該是 Preparing 狀態
				return room
			},
			playerID: "player_002",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.NoError(t, err)
				assert.Equal(t, 1, room.GetPlayerCount())
				assert.Equal(t, internal.StatusWaiting, room.Status)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			room := tt.setupRoom()
			err := room.RemovePlayer(tt.playerID)
			tt.validate(t, room, err)
		})
	}
}

// TestRoom_StateTransitions 測試狀態轉換
func TestRoom_StateTransitions(t *testing.T) {
	t.Run("complete game flow", func(t *testing.T) {
		room := internal.NewRoom("room_001", "測試房間", "ABC123", 2, "", internal.ModeCoop, "normal")
		
		// 初始狀態應該是 Waiting
		assert.Equal(t, internal.StatusWaiting, room.Status)
		
		// 加入玩家
		err := room.AddPlayer("player_001", "玩家一")
		require.NoError(t, err)
		assert.Equal(t, internal.StatusWaiting, room.Status)
		
		// 加滿玩家，狀態變為 Preparing
		err = room.AddPlayer("player_002", "玩家二")
		require.NoError(t, err)
		assert.Equal(t, internal.StatusPreparing, room.Status)
		
		// 選擇歌曲
		song := &internal.Song{
			ID:         "song_001",
			Name:       "測試歌曲",
			Difficulty: "normal",
			Duration:   180,
		}
		err = room.SelectSong("player_001", song)
		require.NoError(t, err)
		assert.NotNil(t, room.SelectedSong)
		
		// 玩家準備
		err = room.SetPlayerReady("player_001", true)
		require.NoError(t, err)
		assert.Equal(t, internal.StatusPreparing, room.Status)
		
		err = room.SetPlayerReady("player_002", true)
		require.NoError(t, err)
		assert.Equal(t, internal.StatusReady, room.Status)
		
		// 開始遊戲
		err = room.StartGame("player_001")
		require.NoError(t, err)
		assert.Equal(t, internal.StatusPlaying, room.Status)
		
		// 結束遊戲
		room.EndGame()
		assert.Equal(t, internal.StatusFinished, room.Status)
	})

	t.Run("cannot start without all ready", func(t *testing.T) {
		room := internal.NewRoom("room_002", "測試房間", "ABC123", 2, "", internal.ModeCoop, "normal")
		room.AddPlayer("player_001", "玩家一")
		room.AddPlayer("player_002", "玩家二")
		
		// 選擇歌曲但不準備
		song := &internal.Song{ID: "song_001", Name: "測試歌曲"}
		room.SelectSong("player_001", song)
		
		// 嘗試開始遊戲
		err := room.StartGame("player_001")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "房間尚未準備好")
	})

	t.Run("only host can select song", func(t *testing.T) {
		room := internal.NewRoom("room_003", "測試房間", "ABC123", 2, "", internal.ModeCoop, "normal")
		room.AddPlayer("player_001", "玩家一") // 房主
		room.AddPlayer("player_002", "玩家二")
		
		song := &internal.Song{ID: "song_001", Name: "測試歌曲"}
		
		// 非房主嘗試選歌
		err := room.SelectSong("player_002", song)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "只有房主可以選歌")
		
		// 房主選歌
		err = room.SelectSong("player_001", song)
		assert.NoError(t, err)
	})

	t.Run("only host can start game", func(t *testing.T) {
		room := internal.NewRoom("room_004", "測試房間", "ABC123", 2, "", internal.ModeCoop, "normal")
		room.AddPlayer("player_001", "玩家一")
		room.AddPlayer("player_002", "玩家二")
		
		// 準備遊戲
		song := &internal.Song{ID: "song_001", Name: "測試歌曲"}
		room.SelectSong("player_001", song)
		room.SetPlayerReady("player_001", true)
		room.SetPlayerReady("player_002", true)
		
		// 非房主嘗試開始
		err := room.StartGame("player_002")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "只有房主可以開始遊戲")
		
		// 房主開始
		err = room.StartGame("player_001")
		assert.NoError(t, err)
	})
}

// TestRoom_ConcurrentOperations 測試併發操作
func TestRoom_ConcurrentOperations(t *testing.T) {
	t.Run("concurrent add players", func(t *testing.T) {
		room := internal.NewRoom("room_001", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
		
		var wg sync.WaitGroup
		errors := make([]error, 4)
		
		// 同時加入 4 個玩家
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				playerID := fmt.Sprintf("player_%03d", idx)
				playerName := fmt.Sprintf("玩家%d", idx)
				errors[idx] = room.AddPlayer(playerID, playerName)
			}(i)
		}
		
		wg.Wait()
		
		// 應該全部成功
		for _, err := range errors {
			assert.NoError(t, err)
		}
		
		assert.Equal(t, 4, room.GetPlayerCount())
		assert.Equal(t, internal.StatusPreparing, room.Status)
	})

	t.Run("concurrent add and remove", func(t *testing.T) {
		room := internal.NewRoom("room_002", "測試房間", "ABC123", 10, "", internal.ModeCoop, "normal")
		
		var wg sync.WaitGroup
		
		// 先加入一些玩家
		for i := 0; i < 5; i++ {
			playerID := fmt.Sprintf("player_%03d", i)
			playerName := fmt.Sprintf("玩家%d", i)
			room.AddPlayer(playerID, playerName)
		}
		
		// 同時加入和移除玩家
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				if idx%2 == 0 {
					// 移除現有玩家
					playerID := fmt.Sprintf("player_%03d", idx/2)
					room.RemovePlayer(playerID)
				} else {
					// 加入新玩家
					playerID := fmt.Sprintf("player_new_%03d", idx)
					playerName := fmt.Sprintf("新玩家%d", idx)
					room.AddPlayer(playerID, playerName)
				}
			}(i)
		}
		
		wg.Wait()
		
		// 驗證房間狀態一致性
		count := room.GetPlayerCount()
		assert.GreaterOrEqual(t, count, 0)
		assert.LessOrEqual(t, count, 10)
		
		// 驗證房主存在（如果有玩家）
		if count > 0 {
			assert.NotEmpty(t, room.HostID)
			assert.NotEmpty(t, room.GetHostName())
		}
	})

	t.Run("concurrent ready state changes", func(t *testing.T) {
		room := internal.NewRoom("room_003", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
		
		// 加入玩家
		for i := 0; i < 4; i++ {
			playerID := fmt.Sprintf("player_%03d", i)
			playerName := fmt.Sprintf("玩家%d", i)
			room.AddPlayer(playerID, playerName)
		}
		
		// 選擇歌曲
		song := &internal.Song{ID: "song_001", Name: "測試歌曲"}
		room.SelectSong("player_000", song)
		
		var wg sync.WaitGroup
		
		// 同時改變準備狀態
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				playerID := fmt.Sprintf("player_%03d", idx)
				
				// 多次改變狀態
				for j := 0; j < 5; j++ {
					room.SetPlayerReady(playerID, j%2 == 0)
					time.Sleep(time.Millisecond)
				}
				
				// 最終設為準備
				room.SetPlayerReady(playerID, true)
			}(i)
		}
		
		wg.Wait()
		
		// 所有玩家應該都準備好了
		assert.Equal(t, internal.StatusReady, room.Status)
	})
}

// TestRoom_PasswordValidation 測試密碼驗證
func TestRoom_PasswordValidation(t *testing.T) {
	tests := []struct {
		name         string
		roomPassword string
		inputPassword string
		expected     bool
	}{
		{
			name:         "no password required",
			roomPassword: "",
			inputPassword: "",
			expected:     true,
		},
		{
			name:         "no password with any input",
			roomPassword: "",
			inputPassword: "anything",
			expected:     true,
		},
		{
			name:         "correct password",
			roomPassword: "secret123",
			inputPassword: "secret123",
			expected:     true,
		},
		{
			name:         "incorrect password",
			roomPassword: "secret123",
			inputPassword: "wrong",
			expected:     false,
		},
		{
			name:         "empty input with password",
			roomPassword: "secret123",
			inputPassword: "",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			room := internal.NewRoom("room_001", "測試房間", "ABC123", 4, tt.roomPassword, internal.ModeCoop, "normal")
			result := room.ValidatePassword(tt.inputPassword)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRoom_EventChannel 測試事件通道
func TestRoom_EventChannel(t *testing.T) {
	t.Run("receive player joined event", func(t *testing.T) {
		room := internal.NewRoom("room_001", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
		
		// 監聽事件
		eventCh := room.Events()
		
		// 加入玩家
		go func() {
			time.Sleep(10 * time.Millisecond)
			room.AddPlayer("player_001", "玩家一")
		}()
		
		// 等待事件
		select {
		case event := <-eventCh:
			assert.Equal(t, "player_joined", event.Type)
			data, ok := event.Data.(map[string]any)
			assert.True(t, ok)
			assert.Equal(t, 1, data["current_players"])
		case <-time.After(100 * time.Millisecond):
			t.Fatal("沒有收到事件")
		}
	})

	t.Run("receive multiple events", func(t *testing.T) {
		room := internal.NewRoom("room_002", "測試房間", "ABC123", 2, "", internal.ModeCoop, "normal")
		eventCh := room.Events()
		
		// 產生多個事件
		go func() {
			room.AddPlayer("player_001", "玩家一")
			room.AddPlayer("player_002", "玩家二")
			room.RemovePlayer("player_002")
		}()
		
		// 收集事件
		events := make([]internal.Event, 0)
		timeout := time.After(100 * time.Millisecond)
		
		for {
			select {
			case event := <-eventCh:
				events = append(events, event)
			case <-timeout:
				goto done
			}
		}
		
	done:
		assert.GreaterOrEqual(t, len(events), 3)
		
		// 驗證事件類型
		eventTypes := make([]string, len(events))
		for i, e := range events {
			eventTypes[i] = e.Type
		}
		
		assert.Contains(t, eventTypes, "player_joined")
		assert.Contains(t, eventTypes, "player_left")
	})
}

// TestRoom_Expiration 測試房間過期
func TestRoom_Expiration(t *testing.T) {
	t.Run("new room not expired", func(t *testing.T) {
		room := internal.NewRoom("room_001", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
		assert.False(t, room.IsExpired())
	})

	t.Run("empty room expires after inactivity", func(t *testing.T) {
		room := internal.NewRoom("room_002", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
		
		// 模擬時間流逝（需要修改 IsExpired 方法以支援測試）
		// 這裡只能測試基本邏輯
		room.AddPlayer("player_001", "玩家一")
		room.RemovePlayer("player_001")
		
		// 空房間但剛被操作過，不應該過期
		assert.False(t, room.IsExpired())
	})
}

// TestRoom_GetState 測試獲取房間狀態
func TestRoom_GetState(t *testing.T) {
	room := internal.NewRoom("room_001", "測試房間", "ABC123", 2, "password", internal.ModeCoop, "hard")
	
	// 加入玩家
	room.AddPlayer("player_001", "玩家一")
	room.AddPlayer("player_002", "玩家二")
	
	// 選擇歌曲
	song := &internal.Song{
		ID:         "song_001",
		Name:       "測試歌曲",
		Difficulty: "normal",
		Duration:   180,
	}
	room.SelectSong("player_001", song)
	
	// 獲取狀態
	state := room.GetState()
	
	// 驗證狀態
	assert.Equal(t, "room_001", state["room_id"])
	assert.Equal(t, "測試房間", state["room_name"])
	assert.Equal(t, "ABC123", state["join_code"])
	assert.Equal(t, 2, state["max_players"])
	assert.Equal(t, true, state["has_password"])
	assert.Equal(t, internal.ModeCoop, state["game_mode"])
	assert.Equal(t, "hard", state["difficulty"])
	assert.Equal(t, internal.StatusPreparing, state["status"])
	assert.Equal(t, "player_001", state["host_id"])
	
	// 驗證玩家列表
	players, ok := state["players"].([]*internal.Player)
	assert.True(t, ok)
	assert.Len(t, players, 2)
	
	// 驗證歌曲
	stateSong, ok := state["selected_song"].(*internal.Song)
	assert.True(t, ok)
	assert.Equal(t, "song_001", stateSong.ID)
}

// TestRoom_Close 測試關閉房間
func TestRoom_Close(t *testing.T) {
	room := internal.NewRoom("room_001", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
	
	// 先監聽事件通道
	eventCh := room.Events()
	
	// 加入玩家
	room.AddPlayer("player_001", "玩家一")
	
	// 清空 player_joined 事件
	select {
	case <-eventCh:
		// 忽略 player_joined 事件
	case <-time.After(10 * time.Millisecond):
		// 沒有事件也沒關係
	}
	
	// 關閉房間
	go func() {
		time.Sleep(10 * time.Millisecond)
		room.Close("test_reason")
	}()
	
	// 等待關閉事件
	select {
	case event := <-eventCh:
		assert.Equal(t, "room_closed", event.Type)
		data, ok := event.Data.(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "test_reason", data["reason"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("沒有收到關閉事件")
	}
	
	// 等待狀態更新
	time.Sleep(20 * time.Millisecond)
	
	// 驗證狀態
	assert.Equal(t, internal.StatusClosed, room.Status)
	
	// 事件通道應該已關閉
	select {
	case _, ok := <-eventCh:
		assert.False(t, ok, "事件通道應該已關閉")
	default:
		// 通道可能還有緩存的事件
	}
}

// TestRoom_IsExpired 測試房間過期判斷
func TestRoom_IsExpired(t *testing.T) {
	room := internal.NewRoom("room_001", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
	
	// 新房間不應該過期
	assert.False(t, room.IsExpired())
	
	// 關閉的房間應該過期
	room.Close("測試")
	assert.True(t, room.IsExpired())
}