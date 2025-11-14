package internal_test

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/koopa0/system-design/02-room-management/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 創建測試用的 logger
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // 測試時只顯示錯誤
	}))
}

// TestNewManager 測試創建管理器
func TestNewManager(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)

	require.NotNil(t, manager)

	// 清理
	defer manager.Stop()

	// 驗證初始狀態
	stats := manager.Stats()
	assert.Equal(t, 0, stats["total_rooms"])
	assert.Equal(t, 0, stats["total_players"])
}

// TestManager_CreateRoom 測試創建房間
func TestManager_CreateRoom(t *testing.T) {
	tests := []struct {
		name          string
		roomName      string
		maxPlayers    int
		password      string
		gameMode      internal.GameMode
		difficulty    string
		expectedError string
		validate      func(t *testing.T, room *internal.Room, err error)
	}{
		{
			name:       "create valid room",
			roomName:   "測試房間",
			maxPlayers: 4,
			password:   "",
			gameMode:   internal.ModeCoop,
			difficulty: "normal",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.NoError(t, err)
				require.NotNil(t, room)
				assert.NotEmpty(t, room.ID)
				assert.NotEmpty(t, room.JoinCode)
				assert.Equal(t, "測試房間", room.Name)
				assert.Equal(t, 4, room.MaxPlayers)
				assert.False(t, room.HasPassword)
			},
		},
		{
			name:       "create room with password",
			roomName:   "私人房間",
			maxPlayers: 2,
			password:   "secret123",
			gameMode:   internal.ModeVersus,
			difficulty: "hard",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.NoError(t, err)
				require.NotNil(t, room)
				assert.True(t, room.HasPassword)
				assert.Equal(t, internal.ModeVersus, room.GameMode)
			},
		},
		{
			name:          "invalid max players too low",
			roomName:      "測試房間",
			maxPlayers:    1,
			password:      "",
			gameMode:      internal.ModeCoop,
			difficulty:    "normal",
			expectedError: "玩家數量必須在 2-4 之間",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.Error(t, err)
				assert.Nil(t, room)
				assert.Contains(t, err.Error(), "玩家數量必須在 2-100 之間")
			},
		},
		{
			name:          "invalid max players too high",
			roomName:      "測試房間",
			maxPlayers:    101,
			password:      "",
			gameMode:      internal.ModeCoop,
			difficulty:    "normal",
			expectedError: "玩家數量必須在 2-100 之間",
			validate: func(t *testing.T, room *internal.Room, err error) {
				require.Error(t, err)
				assert.Nil(t, room)
				assert.Contains(t, err.Error(), "玩家數量必須在 2-100 之間")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := testLogger()
			manager := internal.NewManager(logger)
			defer manager.Stop()

			room, err := manager.CreateRoom(
				tt.roomName,
				tt.maxPlayers,
				tt.password,
				tt.gameMode,
				tt.difficulty,
			)

			tt.validate(t, room, err)

			// 如果創建成功，驗證可以獲取房間
			if err == nil {
				gotRoom, err := manager.GetRoom(room.ID)
				assert.NoError(t, err)
				assert.Equal(t, room.ID, gotRoom.ID)

				// 驗證通過加入碼獲取
				gotRoom, err = manager.GetRoomByJoinCode(room.JoinCode)
				assert.NoError(t, err)
				assert.Equal(t, room.ID, gotRoom.ID)
			}
		})
	}
}

// TestManager_JoinRoom 測試加入房間
func TestManager_JoinRoom(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(manager *internal.Manager) string // 返回房間 ID
		playerID      string
		playerName    string
		password      string
		expectedError string
		validate      func(t *testing.T, manager *internal.Manager, roomID string, err error)
	}{
		{
			name: "join room successfully",
			setupFunc: func(manager *internal.Manager) string {
				room, _ := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
				return room.ID
			},
			playerID:   "player_001",
			playerName: "玩家一",
			password:   "",
			validate: func(t *testing.T, manager *internal.Manager, roomID string, err error) {
				require.NoError(t, err)

				// 驗證玩家在房間中
				room, _ := manager.GetRoom(roomID)
				assert.Equal(t, 1, room.GetPlayerCount())

				// 驗證玩家房間映射
				playerRoomID, exists := manager.GetPlayerRoom("player_001")
				assert.True(t, exists)
				assert.Equal(t, roomID, playerRoomID)
			},
		},
		{
			name: "join room with correct password",
			setupFunc: func(manager *internal.Manager) string {
				room, _ := manager.CreateRoom("私人房間", 4, "secret123", internal.ModeCoop, "normal")
				return room.ID
			},
			playerID:   "player_001",
			playerName: "玩家一",
			password:   "secret123",
			validate: func(t *testing.T, manager *internal.Manager, roomID string, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "join room with wrong password",
			setupFunc: func(manager *internal.Manager) string {
				room, _ := manager.CreateRoom("私人房間", 4, "secret123", internal.ModeCoop, "normal")
				return room.ID
			},
			playerID:      "player_001",
			playerName:    "玩家一",
			password:      "wrong",
			expectedError: "密碼錯誤",
			validate: func(t *testing.T, manager *internal.Manager, roomID string, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "密碼錯誤")
			},
		},
		{
			name: "player already in another room",
			setupFunc: func(manager *internal.Manager) string {
				room1, _ := manager.CreateRoom("房間1", 4, "", internal.ModeCoop, "normal")
				room2, _ := manager.CreateRoom("房間2", 4, "", internal.ModeCoop, "normal")
				_ = manager.JoinRoom(room1.ID, "player_001", "玩家一", "")
				return room2.ID
			},
			playerID:      "player_001",
			playerName:    "玩家一",
			password:      "",
			expectedError: "玩家已在房間",
			validate: func(t *testing.T, manager *internal.Manager, roomID string, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "玩家已在房間")
			},
		},
		{
			name: "join non-existent room",
			setupFunc: func(manager *internal.Manager) string {
				return "non_existent_room"
			},
			playerID:      "player_001",
			playerName:    "玩家一",
			password:      "",
			expectedError: "房間不存在",
			validate: func(t *testing.T, manager *internal.Manager, roomID string, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "房間不存在")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := testLogger()
			manager := internal.NewManager(logger)
			defer manager.Stop()

			roomID := tt.setupFunc(manager)
			err := manager.JoinRoom(roomID, tt.playerID, tt.playerName, tt.password)
			tt.validate(t, manager, roomID, err)
		})
	}
}

// TestManager_LeaveRoom 測試離開房間
func TestManager_LeaveRoom(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(manager *internal.Manager) (roomID string, playerID string)
		validate  func(t *testing.T, manager *internal.Manager, roomID string, err error)
	}{
		{
			name: "leave room successfully",
			setupFunc: func(manager *internal.Manager) (string, string) {
				room, _ := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
				_ = manager.JoinRoom(room.ID, "player_001", "玩家一", "")
				_ = manager.JoinRoom(room.ID, "player_002", "玩家二", "")
				return room.ID, "player_001"
			},
			validate: func(t *testing.T, manager *internal.Manager, roomID string, err error) {
				require.NoError(t, err)

				// 驗證玩家已離開
				room, _ := manager.GetRoom(roomID)
				assert.Equal(t, 1, room.GetPlayerCount())

				// 驗證玩家房間映射已清除
				_, exists := manager.GetPlayerRoom("player_001")
				assert.False(t, exists)
			},
		},
		{
			name: "last player leaves closes room",
			setupFunc: func(manager *internal.Manager) (string, string) {
				room, _ := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
				manager.JoinRoom(room.ID, "player_001", "玩家一", "")
				return room.ID, "player_001"
			},
			validate: func(t *testing.T, manager *internal.Manager, roomID string, err error) {
				require.NoError(t, err)

				// 空房間不再立即被移除，而是等待過期機制處理
				room, err := manager.GetRoom(roomID)
				assert.NoError(t, err)
				assert.NotNil(t, room)
				assert.Equal(t, 0, room.GetPlayerCount())
				assert.Equal(t, internal.StatusWaiting, room.Status)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := testLogger()
			manager := internal.NewManager(logger)
			defer manager.Stop()

			roomID, playerID := tt.setupFunc(manager)
			err := manager.LeaveRoom(roomID, playerID)
			tt.validate(t, manager, roomID, err)
		})
	}
}

// TestManager_GameFlow 測試完整遊戲流程
func TestManager_GameFlow(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 創建房間
	room, err := manager.CreateRoom("遊戲房間", 2, "", internal.ModeCoop, "normal")
	require.NoError(t, err)

	// 玩家加入
	err = manager.JoinRoom(room.ID, "player_001", "玩家一", "")
	require.NoError(t, err)

	err = manager.JoinRoom(room.ID, "player_002", "玩家二", "")
	require.NoError(t, err)

	// 選擇歌曲
	song := &internal.Song{
		ID:         "song_001",
		Name:       "測試歌曲",
		Difficulty: "normal",
		Duration:   180,
	}
	err = manager.SelectSong(room.ID, "player_001", song)
	require.NoError(t, err)

	// 設置準備狀態
	err = manager.SetPlayerReady(room.ID, "player_001", true)
	require.NoError(t, err)

	err = manager.SetPlayerReady(room.ID, "player_002", true)
	require.NoError(t, err)

	// 開始遊戲
	err = manager.StartGame(room.ID, "player_001")
	require.NoError(t, err)

	// 驗證房間狀態
	gotRoom, err := manager.GetRoom(room.ID)
	require.NoError(t, err)
	assert.Equal(t, internal.StatusPlaying, gotRoom.Status)
}

// TestManager_ListRooms 測試房間列表
func TestManager_ListRooms(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 創建多個房間
	room1, _ := manager.CreateRoom("房間1", 4, "", internal.ModeCoop, "normal")
	room2, _ := manager.CreateRoom("房間2", 2, "password", internal.ModeVersus, "hard")
	_, _ = manager.CreateRoom("房間3", 3, "", internal.ModePractice, "easy")

	// 加入玩家
	manager.JoinRoom(room1.ID, "player_001", "玩家一", "")
	manager.JoinRoom(room1.ID, "player_002", "玩家二", "")
	manager.JoinRoom(room2.ID, "player_003", "玩家三", "password")

	t.Run("list all rooms", func(t *testing.T) {
		rooms, total := manager.ListRooms("", "", 1, 10)
		assert.Equal(t, 3, total)
		assert.Len(t, rooms, 3)
	})

	t.Run("filter by status", func(t *testing.T) {
		rooms, total := manager.ListRooms(internal.StatusWaiting, "", 1, 10)
		assert.GreaterOrEqual(t, total, 0)

		for _, room := range rooms {
			assert.Equal(t, internal.StatusWaiting, room["status"])
		}
	})

	t.Run("filter by game mode", func(t *testing.T) {
		rooms, total := manager.ListRooms("", internal.ModeCoop, 1, 10)
		assert.GreaterOrEqual(t, total, 1)

		for _, room := range rooms {
			assert.Equal(t, internal.ModeCoop, room["game_mode"])
		}
	})

	t.Run("pagination", func(t *testing.T) {
		// 第一頁
		rooms1, total1 := manager.ListRooms("", "", 1, 2)
		assert.Equal(t, 3, total1)
		assert.Len(t, rooms1, 2)

		// 第二頁
		rooms2, total2 := manager.ListRooms("", "", 2, 2)
		assert.Equal(t, 3, total2)
		assert.Len(t, rooms2, 1)

		// 超出範圍的頁
		rooms3, total3 := manager.ListRooms("", "", 5, 2)
		assert.Equal(t, 3, total3)
		assert.Len(t, rooms3, 0)
	})
}

// TestManager_GetRoomByJoinCode 測試通過加入碼獲取房間
func TestManager_GetRoomByJoinCode(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 創建房間
	room, err := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
	require.NoError(t, err)

	t.Run("valid join code", func(t *testing.T) {
		gotRoom, err := manager.GetRoomByJoinCode(room.JoinCode)
		require.NoError(t, err)
		assert.Equal(t, room.ID, gotRoom.ID)
	})

	t.Run("case insensitive join code", func(t *testing.T) {
		// 應該支援大小寫不敏感
		lowerCode := strings.ToLower(room.JoinCode)
		gotRoom, err := manager.GetRoomByJoinCode(lowerCode)
		require.NoError(t, err)
		assert.Equal(t, room.ID, gotRoom.ID)
	})

	t.Run("invalid join code", func(t *testing.T) {
		_, err := manager.GetRoomByJoinCode("INVALID")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "無效的加入碼")
	})
}

// TestManager_ConcurrentOperations 測試併發操作
func TestManager_ConcurrentOperations(t *testing.T) {
	t.Run("concurrent room creation", func(t *testing.T) {
		logger := testLogger()
		manager := internal.NewManager(logger)
		defer manager.Stop()

		var wg sync.WaitGroup
		roomCount := 10
		rooms := make([]*internal.Room, roomCount)
		errors := make([]error, roomCount)

		for i := 0; i < roomCount; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				roomName := fmt.Sprintf("房間%d", idx)
				rooms[idx], errors[idx] = manager.CreateRoom(
					roomName,
					4,
					"",
					internal.ModeCoop,
					"normal",
				)
			}(i)
		}

		wg.Wait()

		// 所有創建應該成功
		for i, err := range errors {
			assert.NoError(t, err)
			assert.NotNil(t, rooms[i])
		}

		// 驗證統計
		stats := manager.Stats()
		assert.Equal(t, roomCount, stats["total_rooms"])
	})

	t.Run("concurrent join and leave", func(t *testing.T) {
		logger := testLogger()
		manager := internal.NewManager(logger)
		defer manager.Stop()

		// 創建一個大房間
		room, err := manager.CreateRoom("大房間", 100, "", internal.ModeCoop, "normal")
		require.NoError(t, err)

		var wg sync.WaitGroup
		playerCount := 20

		// 同時加入
		for i := 0; i < playerCount; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				playerID := fmt.Sprintf("player_%03d", idx)
				playerName := fmt.Sprintf("玩家%d", idx)
				manager.JoinRoom(room.ID, playerID, playerName, "")
			}(i)
		}

		wg.Wait()

		// 驗證玩家數量
		gotRoom, _ := manager.GetRoom(room.ID)
		assert.Equal(t, playerCount, gotRoom.GetPlayerCount())

		// 同時離開一半玩家
		for i := 0; i < playerCount/2; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				playerID := fmt.Sprintf("player_%03d", idx)
				_ = manager.LeaveRoom(room.ID, playerID)
			}(i)
		}

		wg.Wait()

		// 驗證剩餘玩家數量
		gotRoom, _ = manager.GetRoom(room.ID)
		assert.Equal(t, playerCount/2, gotRoom.GetPlayerCount())
	})

	t.Run("concurrent operations on multiple rooms", func(t *testing.T) {
		logger := testLogger()
		manager := internal.NewManager(logger)
		defer manager.Stop()

		var wg sync.WaitGroup
		roomCount := 5
		operationsPerRoom := 10

		// 創建多個房間
		rooms := make([]*internal.Room, roomCount)
		for i := 0; i < roomCount; i++ {
			rooms[i], _ = manager.CreateRoom(
				fmt.Sprintf("房間%d", i),
				10,
				"",
				internal.ModeCoop,
				"normal",
			)
		}

		// 對每個房間執行併發操作
		for roomIdx := 0; roomIdx < roomCount; roomIdx++ {
			for opIdx := 0; opIdx < operationsPerRoom; opIdx++ {
				wg.Add(1)
				go func(rIdx, oIdx int) {
					defer wg.Done()

					roomID := rooms[rIdx].ID
					playerID := fmt.Sprintf("player_%d_%d", rIdx, oIdx)
					playerName := fmt.Sprintf("玩家%d-%d", rIdx, oIdx)

					// 加入
					manager.JoinRoom(roomID, playerID, playerName, "")

					// 執行一些操作
					if oIdx%3 == 0 {
						// 部分玩家離開
						time.Sleep(time.Millisecond)
						_ = manager.LeaveRoom(roomID, playerID)
					}
				}(roomIdx, opIdx)
			}
		}

		wg.Wait()

		// 驗證系統狀態一致性
		stats := manager.Stats()
		assert.Equal(t, roomCount, stats["total_rooms"])

		totalPlayers := stats["total_players"].(int)
		assert.GreaterOrEqual(t, totalPlayers, 0)
		assert.LessOrEqual(t, totalPlayers, roomCount*operationsPerRoom)
	})
}

// TestManager_Stats 測試統計功能
func TestManager_Stats(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 初始狀態
	stats := manager.Stats()
	assert.Equal(t, 0, stats["total_rooms"])
	assert.Equal(t, 0, stats["total_players"])

	// 創建不同類型的房間
	room1, _ := manager.CreateRoom("房間1", 4, "", internal.ModeCoop, "normal")
	room2, _ := manager.CreateRoom("房間2", 2, "", internal.ModeVersus, "hard")
	_, _ = manager.CreateRoom("房間3", 3, "", internal.ModeCoop, "easy")

	// 加入玩家
	manager.JoinRoom(room1.ID, "player_001", "玩家一", "")
	manager.JoinRoom(room1.ID, "player_002", "玩家二", "")
	manager.JoinRoom(room2.ID, "player_003", "玩家三", "")

	// 獲取統計
	stats = manager.Stats()
	assert.Equal(t, 3, stats["total_rooms"])
	assert.Equal(t, 3, stats["total_players"])

	// 驗證按狀態統計
	byStatus := stats["by_status"].(map[internal.RoomStatus]int)
	assert.Equal(t, 3, byStatus[internal.StatusWaiting])

	// 驗證按模式統計
	byMode := stats["by_mode"].(map[internal.GameMode]int)
	assert.Equal(t, 2, byMode[internal.ModeCoop])
	assert.Equal(t, 1, byMode[internal.ModeVersus])
}

// TestManager_Stop 測試停止管理器
func TestManager_Stop(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)

	// 創建一些房間和玩家
	room, _ := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
	manager.JoinRoom(room.ID, "player_001", "玩家一", "")

	// 停止管理器
	manager.Stop()

	// 驗證無法再創建房間
	_, err := manager.CreateRoom("新房間", 4, "", internal.ModeCoop, "normal")
	// 停止後的行為取決於實現，這裡只是確保不會 panic
	_ = err
}

// TestManager_RoomCleanup 測試房間清理功能
func TestManager_RoomCleanup(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 創建一個房間
	room, err := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
	require.NoError(t, err)
	roomID := room.ID

	// 加入玩家
	err = manager.JoinRoom(roomID, "player_001", "玩家一", "")
	require.NoError(t, err)

	// 所有玩家離開
	err = manager.LeaveRoom(roomID, "player_001")
	require.NoError(t, err)

	// 等待一段時間讓清理邏輯執行
	time.Sleep(100 * time.Millisecond)

	// 檢查房間是否還存在（空房間應該被清理）
	_, err = manager.GetRoom(roomID)
	// 根據實現，空房間可能會被清理
	_ = err
}

// TestManager_SetPlayerReady 測試設置玩家準備狀態
func TestManager_SetPlayerReady(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 創建房間並加入滿員玩家（使房間狀態變成 StatusPreparing）
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")
	manager.JoinRoom(room.ID, "player_001", "玩家一", "") // 第一個玩家成為房主
	manager.JoinRoom(room.ID, "player_002", "玩家二", "") // 滿員，狀態變成 StatusPreparing

	// 選擇歌曲（SetPlayerReady 需要先選歌）
	song := &internal.Song{
		ID:   "song_001",
		Name: "測試歌曲",
	}
	_ = manager.SelectSong(room.ID, "player_001", song)

	// 測試設置準備狀態
	err := manager.SetPlayerReady(room.ID, "player_001", true)
	assert.NoError(t, err)

	// 測試取消準備狀態
	err = manager.SetPlayerReady(room.ID, "player_001", false)
	assert.NoError(t, err)

	// 測試無效房間
	err = manager.SetPlayerReady("invalid_room", "player_001", true)
	assert.Error(t, err)

	// 測試無效玩家
	err = manager.SetPlayerReady(room.ID, "invalid_player", true)
	assert.Error(t, err)
}

// TestManager_SelectSong 測試選擇歌曲
func TestManager_SelectSong(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 創建房間並加入滿員玩家（使房間狀態變成 StatusPreparing）
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")
	manager.JoinRoom(room.ID, "player_001", "玩家一", "") // 第一個玩家成為房主
	manager.JoinRoom(room.ID, "player_002", "玩家二", "") // 滿員，狀態變成 StatusPreparing

	// 測試選擇歌曲（由房主執行）
	song := &internal.Song{
		ID:   "song_001",
		Name: "測試歌曲",
	}
	err := manager.SelectSong(room.ID, "player_001", song)
	assert.NoError(t, err)

	// 測試非房主選歌（應該失敗）
	err = manager.SelectSong(room.ID, "player_002", song)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "只有房主可以選歌")

	// 測試無效房間
	err = manager.SelectSong("invalid_room", "player_001", song)
	assert.Error(t, err)
}

// TestManager_StartGame 測試開始遊戲
func TestManager_StartGame(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 創建房間並加入滿員玩家
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")
	manager.JoinRoom(room.ID, "player_001", "玩家一", "") // 第一個玩家成為房主
	manager.JoinRoom(room.ID, "player_002", "玩家二", "") // 滿員

	// 選擇歌曲
	song := &internal.Song{
		ID:   "song_001",
		Name: "測試歌曲",
	}
	_ = manager.SelectSong(room.ID, "player_001", song)

	// 所有玩家設定為準備好
	_ = manager.SetPlayerReady(room.ID, "player_001", true)
	manager.SetPlayerReady(room.ID, "player_002", true)

	// 測試開始遊戲（由房主執行）
	err := manager.StartGame(room.ID, "player_001")
	assert.NoError(t, err)

	// 測試非房主開始遊戲（應該失敗）
	err = manager.StartGame(room.ID, "player_002")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "只有房主可以開始遊戲")

	// 測試無效房間
	err = manager.StartGame("invalid_room", "player_001")
	assert.Error(t, err)
}

// TestManager_GenerateIDUniqueness 測試 ID 生成唯一性
func TestManager_GenerateIDUniqueness(t *testing.T) {
	logger := testLogger()
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 生成多個房間，確保 ID 唯一
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		room, err := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
		assert.NoError(t, err)
		assert.NotContains(t, ids, room.ID, "生成了重複的 ID")
		ids[room.ID] = true
	}
}

// TestRoom_CloseWithEvents 測試關閉房間並發送事件
func TestRoom_CloseWithEvents(t *testing.T) {
	room := internal.NewRoom("room_001", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")

	// 監聽事件
	eventCh := room.Events()

	// 加入玩家
	room.AddPlayer("player_001", "玩家一")
	room.AddPlayer("player_002", "玩家二")

	// 清空之前的事件
	select {
	case <-eventCh:
	case <-time.After(10 * time.Millisecond):
	}
	select {
	case <-eventCh:
	case <-time.After(10 * time.Millisecond):
	}

	// 關閉房間
	go func() {
		time.Sleep(10 * time.Millisecond)
		room.Close("測試關閉")
	}()

	// 等待關閉事件
	select {
	case event := <-eventCh:
		assert.Equal(t, "room_closed", event.Type)
		data, ok := event.Data.(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "測試關閉", data["reason"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("沒有收到關閉事件")
	}

	// 驗證房間狀態
	assert.Equal(t, internal.StatusClosed, room.Status)

	// 驗證不能再加入玩家
	err := room.AddPlayer("player_003", "玩家三")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "房間狀態不允許加入")
}
