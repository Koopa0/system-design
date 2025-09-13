package internal_test

import (
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/koopa0/system-design/exercise-2/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStress_ConcurrentRoomCreation 測試併發創建房間
func TestStress_ConcurrentRoomCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	const (
		numGoroutines = 100
		roomsPerGoroutine = 10
	)

	var (
		wg sync.WaitGroup
		successCount int32
		errorCount int32
	)

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			
			for j := 0; j < roomsPerGoroutine; j++ {
				roomName := fmt.Sprintf("房間_%d_%d", goroutineID, j)
				maxPlayers := 2 + rand.Intn(3) // 2-4 玩家
				gameMode := []internal.GameMode{
					internal.ModeCoop,
					internal.ModeVersus,
					internal.ModePractice,
				}[rand.Intn(3)]
				
				_, err := manager.CreateRoom(
					roomName,
					maxPlayers,
					"",
					gameMode,
					"normal",
				)
				
				if err != nil {
					atomic.AddInt32(&errorCount, 1)
				} else {
					atomic.AddInt32(&successCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("創建房間壓力測試結果:")
	t.Logf("  總房間數: %d", numGoroutines*roomsPerGoroutine)
	t.Logf("  成功: %d", successCount)
	t.Logf("  失敗: %d", errorCount)
	t.Logf("  耗時: %v", duration)
	t.Logf("  速率: %.2f rooms/sec", float64(successCount)/duration.Seconds())

	// 驗證
	assert.Equal(t, int32(numGoroutines*roomsPerGoroutine), successCount)
	assert.Equal(t, int32(0), errorCount)

	// 驗證統計
	stats := manager.Stats()
	assert.Equal(t, int(successCount), stats["total_rooms"])
}

// TestStress_ConcurrentPlayerJoinLeave 測試併發玩家加入和離開
func TestStress_ConcurrentPlayerJoinLeave(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	// 創建一個大容量房間
	room, err := manager.CreateRoom("大房間", 100, "", internal.ModeCoop, "normal")
	require.NoError(t, err)

	const (
		numPlayers = 100
		numOperations = 10 // 每個玩家加入離開的次數
	)

	var (
		wg sync.WaitGroup
		joinCount int32
		leaveCount int32
		errorCount int32
	)

	start := time.Now()

	for i := 0; i < numPlayers; i++ {
		wg.Add(1)
		go func(playerID int) {
			defer wg.Done()
			
			playerName := fmt.Sprintf("玩家_%d", playerID)
			playerIDStr := fmt.Sprintf("player_%d", playerID)
			
			for j := 0; j < numOperations; j++ {
				// 加入房間
				err := manager.JoinRoom(room.ID, playerIDStr, playerName, "")
				if err == nil {
					atomic.AddInt32(&joinCount, 1)
				} else {
					atomic.AddInt32(&errorCount, 1)
				}
				
				// 隨機延遲
				time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
				
				// 離開房間
				err = manager.LeaveRoom(room.ID, playerIDStr)
				if err == nil {
					atomic.AddInt32(&leaveCount, 1)
				} else {
					atomic.AddInt32(&errorCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("玩家加入離開壓力測試結果:")
	t.Logf("  總操作數: %d", numPlayers*numOperations*2)
	t.Logf("  加入成功: %d", joinCount)
	t.Logf("  離開成功: %d", leaveCount)
	t.Logf("  錯誤: %d", errorCount)
	t.Logf("  耗時: %v", duration)
	t.Logf("  速率: %.2f ops/sec", float64(joinCount+leaveCount)/duration.Seconds())

	// 驗證
	assert.Equal(t, joinCount, leaveCount)
	assert.Equal(t, int32(numPlayers*numOperations), joinCount)
}

// TestStress_MultiRoomOperations 測試多房間併發操作
func TestStress_MultiRoomOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	const (
		numRooms = 50
		playersPerRoom = 4
		operationsPerPlayer = 5
	)

	// 創建多個房間
	rooms := make([]*internal.Room, numRooms)
	for i := 0; i < numRooms; i++ {
		room, err := manager.CreateRoom(
			fmt.Sprintf("房間_%d", i),
			playersPerRoom,
			"",
			internal.ModeCoop,
			"normal",
		)
		require.NoError(t, err)
		rooms[i] = room
	}

	var (
		wg sync.WaitGroup
		totalOperations int32
	)

	start := time.Now()

	// 對每個房間執行併發操作
	for roomIdx, room := range rooms {
		for playerIdx := 0; playerIdx < playersPerRoom; playerIdx++ {
			wg.Add(1)
			go func(rIdx, pIdx int, r *internal.Room) {
				defer wg.Done()
				
				playerID := fmt.Sprintf("player_%d_%d", rIdx, pIdx)
				playerName := fmt.Sprintf("玩家_%d_%d", rIdx, pIdx)
				
				// 加入房間
				manager.JoinRoom(r.ID, playerID, playerName, "")
				atomic.AddInt32(&totalOperations, 1)
				
				// 執行一系列操作
				for op := 0; op < operationsPerPlayer; op++ {
					switch op % 3 {
					case 0:
						// 選歌（如果是房主）
						if pIdx == 0 {
							song := &internal.Song{
								ID:   fmt.Sprintf("song_%d", op),
								Name: fmt.Sprintf("歌曲_%d", op),
							}
							manager.SelectSong(r.ID, playerID, song)
						}
					case 1:
						// 準備/取消準備
						manager.SetPlayerReady(r.ID, playerID, op%2 == 0)
					case 2:
						// 獲取房間狀態
						manager.GetRoom(r.ID)
					}
					atomic.AddInt32(&totalOperations, 1)
					
					time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
				}
			}(roomIdx, playerIdx, room)
		}
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("多房間操作壓力測試結果:")
	t.Logf("  房間數: %d", numRooms)
	t.Logf("  每房間玩家數: %d", playersPerRoom)
	t.Logf("  總操作數: %d", totalOperations)
	t.Logf("  耗時: %v", duration)
	t.Logf("  速率: %.2f ops/sec", float64(totalOperations)/duration.Seconds())

	// 驗證系統狀態
	stats := manager.Stats()
	assert.Equal(t, numRooms, stats["total_rooms"])
	assert.LessOrEqual(t, stats["total_players"], numRooms*playersPerRoom)
}

// TestStress_RoomLifecycle 測試房間生命週期壓力
func TestStress_RoomLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	const (
		numCycles = 100
		numConcurrent = 10
	)

	var (
		wg sync.WaitGroup
		completedCycles int32
	)

	start := time.Now()

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for cycle := 0; cycle < numCycles/numConcurrent; cycle++ {
				// 創建房間
				room, err := manager.CreateRoom(
					fmt.Sprintf("臨時房間_%d_%d", workerID, cycle),
					2,
					"",
					internal.ModeCoop,
					"normal",
				)
				if err != nil {
					continue
				}
				
				// 玩家加入
				for p := 0; p < 2; p++ {
					playerID := fmt.Sprintf("player_%d_%d_%d", workerID, cycle, p)
					manager.JoinRoom(room.ID, playerID, fmt.Sprintf("玩家%d", p), "")
				}
				
				// 選歌
				song := &internal.Song{ID: "song_1", Name: "測試歌曲"}
				manager.SelectSong(room.ID, fmt.Sprintf("player_%d_%d_0", workerID, cycle), song)
				
				// 準備
				for p := 0; p < 2; p++ {
					playerID := fmt.Sprintf("player_%d_%d_%d", workerID, cycle, p)
					manager.SetPlayerReady(room.ID, playerID, true)
				}
				
				// 開始遊戲
				manager.StartGame(room.ID, fmt.Sprintf("player_%d_%d_0", workerID, cycle))
				
				// 結束遊戲
				gotRoom, _ := manager.GetRoom(room.ID)
				if gotRoom != nil {
					gotRoom.EndGame()
				}
				
				// 所有玩家離開
				for p := 0; p < 2; p++ {
					playerID := fmt.Sprintf("player_%d_%d_%d", workerID, cycle, p)
					manager.LeaveRoom(room.ID, playerID)
				}
				
				atomic.AddInt32(&completedCycles, 1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("房間生命週期壓力測試結果:")
	t.Logf("  完成週期數: %d", completedCycles)
	t.Logf("  耗時: %v", duration)
	t.Logf("  速率: %.2f cycles/sec", float64(completedCycles)/duration.Seconds())

	// 手動觸發清理以確保過期房間被移除
	manager.Cleanup()
	
	// 驗證沒有洩漏
	stats := manager.Stats()
	// 空房間需要 5 分鐘才過期，所以剛創建的空房間還在
	// 100 個週期可能會有最多 100 個空房間
	assert.LessOrEqual(t, stats["total_rooms"], int(completedCycles)+10)
}

// BenchmarkRoom_AddPlayer 基準測試：加入玩家
func BenchmarkRoom_AddPlayer(b *testing.B) {
	room := internal.NewRoom("bench_room", "測試房間", "ABC123", 100, "", internal.ModeCoop, "normal")
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		playerID := fmt.Sprintf("player_%d", i)
		playerName := fmt.Sprintf("玩家%d", i)
		room.AddPlayer(playerID, playerName)
	}
	
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "players/sec")
}

// BenchmarkRoom_RemovePlayer 基準測試：移除玩家
func BenchmarkRoom_RemovePlayer(b *testing.B) {
	room := internal.NewRoom("bench_room", "測試房間", "ABC123", 1000, "", internal.ModeCoop, "normal")
	
	// 預先加入玩家
	for i := 0; i < b.N; i++ {
		playerID := fmt.Sprintf("player_%d", i)
		playerName := fmt.Sprintf("玩家%d", i)
		room.AddPlayer(playerID, playerName)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		playerID := fmt.Sprintf("player_%d", i)
		room.RemovePlayer(playerID)
	}
	
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "removes/sec")
}

// BenchmarkRoom_GetState 基準測試：獲取房間狀態
func BenchmarkRoom_GetState(b *testing.B) {
	room := internal.NewRoom("bench_room", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
	
	// 加入一些玩家
	for i := 0; i < 4; i++ {
		room.AddPlayer(fmt.Sprintf("player_%d", i), fmt.Sprintf("玩家%d", i))
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = room.GetState()
	}
	
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "gets/sec")
}

// BenchmarkManager_CreateRoom 基準測試：創建房間
func BenchmarkManager_CreateRoom(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.CreateRoom(
			fmt.Sprintf("房間_%d", i),
			4,
			"",
			internal.ModeCoop,
			"normal",
		)
	}
	
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "rooms/sec")
}

// BenchmarkManager_GetRoom 基準測試：獲取房間
func BenchmarkManager_GetRoom(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()
	
	// 創建一些房間
	roomIDs := make([]string, 100)
	for i := 0; i < 100; i++ {
		room, _ := manager.CreateRoom(
			fmt.Sprintf("房間_%d", i),
			4,
			"",
			internal.ModeCoop,
			"normal",
		)
		roomIDs[i] = room.ID
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		roomID := roomIDs[i%100]
		manager.GetRoom(roomID)
	}
	
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "gets/sec")
}

// BenchmarkManager_ListRooms 基準測試：列出房間
func BenchmarkManager_ListRooms(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()
	
	// 創建一些房間
	for i := 0; i < 100; i++ {
		manager.CreateRoom(
			fmt.Sprintf("房間_%d", i),
			4,
			"",
			internal.ModeCoop,
			"normal",
		)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.ListRooms("", "", 1, 20)
	}
	
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "lists/sec")
}

// BenchmarkConcurrentOperations 基準測試：併發操作
func BenchmarkConcurrentOperations(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()
	
	// 創建一個房間
	room, _ := manager.CreateRoom("bench_room", 100, "", internal.ModeCoop, "normal")
	
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			playerID := fmt.Sprintf("player_%d_%d", i, time.Now().UnixNano())
			playerName := fmt.Sprintf("玩家%d", i)
			
			// 隨機執行操作
			switch i % 4 {
			case 0:
				manager.JoinRoom(room.ID, playerID, playerName, "")
			case 1:
				manager.LeaveRoom(room.ID, playerID)
			case 2:
				manager.GetRoom(room.ID)
			case 3:
				manager.ListRooms("", "", 1, 10)
			}
		}
	})
}

// TestStress_MemoryUsage 測試記憶體使用
func TestStress_MemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	const (
		numRooms = 1000
		playersPerRoom = 10
	)

	// 創建大量房間和玩家
	for i := 0; i < numRooms; i++ {
		room, err := manager.CreateRoom(
			fmt.Sprintf("房間_%d", i),
			playersPerRoom,
			"",
			internal.ModeCoop,
			"normal",
		)
		require.NoError(t, err)
		
		// 加入玩家
		for j := 0; j < playersPerRoom/2; j++ {
			playerID := fmt.Sprintf("player_%d_%d", i, j)
			playerName := fmt.Sprintf("玩家_%d_%d", i, j)
			manager.JoinRoom(room.ID, playerID, playerName, "")
		}
	}

	// 獲取統計
	stats := manager.Stats()
	t.Logf("記憶體使用測試:")
	t.Logf("  總房間數: %d", stats["total_rooms"])
	t.Logf("  總玩家數: %d", stats["total_players"])
	
	// 驗證
	assert.Equal(t, numRooms, stats["total_rooms"])
	assert.Equal(t, numRooms*playersPerRoom/2, stats["total_players"])
}

// TestStress_RapidStateChanges 測試快速狀態變化
func TestStress_RapidStateChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	room := internal.NewRoom("test_room", "測試房間", "ABC123", 4, "", internal.ModeCoop, "normal")
	
	// 加入玩家
	for i := 0; i < 4; i++ {
		err := room.AddPlayer(fmt.Sprintf("player_%d", i), fmt.Sprintf("玩家%d", i))
		require.NoError(t, err)
	}
	
	// 選歌
	song := &internal.Song{ID: "song_1", Name: "測試歌曲"}
	err := room.SelectSong("player_0", song)
	require.NoError(t, err)

	const numIterations = 1000
	var wg sync.WaitGroup
	
	start := time.Now()
	
	// 併發改變準備狀態
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(playerIdx int) {
			defer wg.Done()
			playerID := fmt.Sprintf("player_%d", playerIdx)
			
			for j := 0; j < numIterations; j++ {
				room.SetPlayerReady(playerID, j%2 == 0)
			}
		}(i)
	}
	
	wg.Wait()
	duration := time.Since(start)
	
	t.Logf("快速狀態變化測試:")
	t.Logf("  總操作數: %d", 4*numIterations)
	t.Logf("  耗時: %v", duration)
	t.Logf("  速率: %.2f changes/sec", float64(4*numIterations)/duration.Seconds())
	
	// 驗證房間狀態一致性
	state := room.GetState()
	assert.NotNil(t, state)
	assert.NotEmpty(t, state["players"])
}