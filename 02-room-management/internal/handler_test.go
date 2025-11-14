package internal_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/koopa0/system-design/02-room-management/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandler_CreateRoom 測試創建房間 API
func TestHandler_CreateRoom(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    any
		expectedStatus int
		validate       func(t *testing.T, resp map[string]any)
	}{
		{
			name: "create room successfully",
			requestBody: map[string]any{
				"room_name":   "測試房間",
				"max_players": 4,
				"game_mode":   "coop",
				"difficulty":  "normal",
			},
			expectedStatus: http.StatusCreated,
			validate: func(t *testing.T, resp map[string]any) {
				assert.NotEmpty(t, resp["room_id"])
				assert.NotEmpty(t, resp["join_code"])
				assert.Equal(t, "waiting", resp["status"])
			},
		},
		{
			name: "create room with password",
			requestBody: map[string]any{
				"room_name":   "私人房間",
				"max_players": 2,
				"password":    "secret123",
				"game_mode":   "versus",
				"difficulty":  "hard",
			},
			expectedStatus: http.StatusCreated,
			validate: func(t *testing.T, resp map[string]any) {
				assert.NotEmpty(t, resp["room_id"])
				assert.NotEmpty(t, resp["join_code"])
			},
		},
		{
			name: "missing room name",
			requestBody: map[string]any{
				"max_players": 4,
				"game_mode":   "coop",
			},
			expectedStatus: http.StatusBadRequest,
			validate: func(t *testing.T, resp map[string]any) {
				assert.Contains(t, resp["error"], "房間名稱不能為空")
			},
		},
		{
			name: "invalid max players",
			requestBody: map[string]any{
				"room_name":   "測試房間",
				"max_players": 101,
				"game_mode":   "coop",
			},
			expectedStatus: http.StatusBadRequest,
			validate: func(t *testing.T, resp map[string]any) {
				assert.Contains(t, resp["error"], "玩家數量必須在 2-100 之間")
			},
		},
		{
			name: "default game mode",
			requestBody: map[string]any{
				"room_name":   "測試房間",
				"max_players": 3,
				"difficulty":  "easy",
			},
			expectedStatus: http.StatusCreated,
			validate: func(t *testing.T, resp map[string]any) {
				assert.NotEmpty(t, resp["room_id"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 設置測試環境
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := internal.NewManager(logger)
			defer manager.Stop()

			handler := internal.NewHandler(manager, logger)
			router := handler.Routes()

			// 創建請求
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/create", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			// 執行請求
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// 驗證狀態碼
			assert.Equal(t, tt.expectedStatus, w.Code)

			// 解析響應
			var resp map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)

			// 驗證響應
			tt.validate(t, resp)
		})
	}
}

// TestHandler_JoinRoom 測試加入房間 API
func TestHandler_JoinRoom(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(manager *internal.Manager) string // 返回房間 ID
		requestBody    any
		expectedStatus int
		validate       func(t *testing.T, resp map[string]any)
	}{
		{
			name: "join room successfully",
			setupFunc: func(manager *internal.Manager) string {
				room, _ := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
				return room.ID
			},
			requestBody: map[string]any{
				"player_id":   "player_001",
				"player_name": "玩家一",
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, resp map[string]any) {
				assert.True(t, resp["success"].(bool))
				assert.NotNil(t, resp["room_state"])
			},
		},
		{
			name: "join room with password",
			setupFunc: func(manager *internal.Manager) string {
				room, _ := manager.CreateRoom("私人房間", 4, "secret123", internal.ModeCoop, "normal")
				return room.ID
			},
			requestBody: map[string]any{
				"player_id":   "player_001",
				"player_name": "玩家一",
				"password":    "secret123",
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, resp map[string]any) {
				assert.True(t, resp["success"].(bool))
			},
		},
		{
			name: "join room with wrong password",
			setupFunc: func(manager *internal.Manager) string {
				room, _ := manager.CreateRoom("私人房間", 4, "secret123", internal.ModeCoop, "normal")
				return room.ID
			},
			requestBody: map[string]any{
				"player_id":   "player_001",
				"player_name": "玩家一",
				"password":    "wrong",
			},
			expectedStatus: http.StatusBadRequest,
			validate: func(t *testing.T, resp map[string]any) {
				assert.Contains(t, resp["error"], "密碼錯誤")
			},
		},
		{
			name: "missing player info",
			setupFunc: func(manager *internal.Manager) string {
				room, _ := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
				return room.ID
			},
			requestBody: map[string]any{
				"player_name": "玩家一",
			},
			expectedStatus: http.StatusBadRequest,
			validate: func(t *testing.T, resp map[string]any) {
				assert.Contains(t, resp["error"], "玩家資訊不完整")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 設置測試環境
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := internal.NewManager(logger)
			defer manager.Stop()

			roomID := tt.setupFunc(manager)

			handler := internal.NewHandler(manager, logger)
			router := handler.Routes()

			// 創建請求
			body, _ := json.Marshal(tt.requestBody)
			url := fmt.Sprintf("/api/v1/rooms/%s/join", roomID)
			req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("room_id", roomID)

			// 執行請求
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// 驗證狀態碼
			assert.Equal(t, tt.expectedStatus, w.Code)

			// 解析響應
			var resp map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)

			// 驗證響應
			tt.validate(t, resp)
		})
	}
}

// TestHandler_LeaveRoom 測試離開房間 API
func TestHandler_LeaveRoom(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建房間並加入玩家
	room, _ := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
	_ = manager.JoinRoom(room.ID, "player_001", "玩家一", "")
	_ = manager.JoinRoom(room.ID, "player_002", "玩家二", "")

	// 測試離開房間
	reqBody := map[string]any{
		"player_id": "player_001",
	}
	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("/api/v1/rooms/%s/leave", room.ID)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("room_id", room.ID)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["success"].(bool))

	// 驗證玩家已離開
	gotRoom, _ := manager.GetRoom(room.ID)
	assert.Equal(t, 1, gotRoom.GetPlayerCount())
}

// TestHandler_SetReady 測試設置準備狀態 API
func TestHandler_SetReady(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建房間並準備
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")
	_ = manager.JoinRoom(room.ID, "player_001", "玩家一", "")
	_ = manager.JoinRoom(room.ID, "player_002", "玩家二", "")

	// 選擇歌曲
	song := &internal.Song{ID: "song_001", Name: "測試歌曲"}
	err := manager.SelectSong(room.ID, "player_001", song)
	require.NoError(t, err)

	// 測試設置準備狀態
	reqBody := map[string]any{
		"player_id": "player_001",
		"is_ready":  true,
	}
	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("/api/v1/rooms/%s/ready", room.ID)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("room_id", room.ID)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["success"].(bool))
}

// TestHandler_SelectSong 測試選擇歌曲 API
func TestHandler_SelectSong(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建房間
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")
	_ = manager.JoinRoom(room.ID, "player_001", "玩家一", "")
	_ = manager.JoinRoom(room.ID, "player_002", "玩家二", "")

	// 測試選擇歌曲
	reqBody := map[string]any{
		"player_id": "player_001",
		"song_id":   "song_001",
	}
	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("/api/v1/rooms/%s/select_song", room.ID)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("room_id", room.ID)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["success"].(bool))
}

// TestHandler_StartGame 測試開始遊戲 API
func TestHandler_StartGame(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 準備遊戲
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")
	_ = manager.JoinRoom(room.ID, "player_001", "玩家一", "")
	_ = manager.JoinRoom(room.ID, "player_002", "玩家二", "")

	song := &internal.Song{ID: "song_001", Name: "測試歌曲"}
	err := manager.SelectSong(room.ID, "player_001", song)
	require.NoError(t, err)
	err = manager.SetPlayerReady(room.ID, "player_001", true)
	require.NoError(t, err)
	err = manager.SetPlayerReady(room.ID, "player_002", true)
	require.NoError(t, err)

	// 測試開始遊戲
	reqBody := map[string]any{
		"player_id": "player_001",
	}
	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("/api/v1/rooms/%s/start", room.ID)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("room_id", room.ID)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["success"].(bool))

	// 驗證遊戲已開始
	gotRoom, _ := manager.GetRoom(room.ID)
	assert.Equal(t, internal.StatusPlaying, gotRoom.Status)
}

// TestHandler_ListRooms 測試房間列表 API
func TestHandler_ListRooms(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建多個房間
	room1, _ := manager.CreateRoom("房間1", 4, "", internal.ModeCoop, "normal")
	room2, _ := manager.CreateRoom("房間2", 2, "password", internal.ModeVersus, "hard")
	_, err := manager.CreateRoom("房間3", 3, "", internal.ModePractice, "easy")
	require.NoError(t, err)

	// 加入玩家
	err = manager.JoinRoom(room1.ID, "player_001", "玩家一", "")
	require.NoError(t, err)
	err = manager.JoinRoom(room2.ID, "player_002", "玩家二", "password")
	require.NoError(t, err)

	tests := []struct {
		name        string
		queryParams string
		validate    func(t *testing.T, resp map[string]any)
	}{
		{
			name:        "list all rooms",
			queryParams: "",
			validate: func(t *testing.T, resp map[string]any) {
				assert.Equal(t, float64(3), resp["total"])
				rooms := resp["rooms"].([]any)
				assert.Len(t, rooms, 3)
			},
		},
		{
			name:        "filter by status",
			queryParams: "?status=waiting",
			validate: func(t *testing.T, resp map[string]any) {
				rooms := resp["rooms"].([]any)
				for _, room := range rooms {
					roomMap := room.(map[string]any)
					assert.Equal(t, "waiting", roomMap["status"])
				}
			},
		},
		{
			name:        "filter by game mode",
			queryParams: "?mode=coop",
			validate: func(t *testing.T, resp map[string]any) {
				rooms := resp["rooms"].([]any)
				assert.GreaterOrEqual(t, len(rooms), 1)
				for _, room := range rooms {
					roomMap := room.(map[string]any)
					assert.Equal(t, "coop", roomMap["game_mode"])
				}
			},
		},
		{
			name:        "pagination",
			queryParams: "?page=1&limit=2",
			validate: func(t *testing.T, resp map[string]any) {
				assert.Equal(t, float64(3), resp["total"])
				assert.Equal(t, float64(1), resp["page"])
				rooms := resp["rooms"].([]any)
				assert.LessOrEqual(t, len(rooms), 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/rooms" + tt.queryParams
			req := httptest.NewRequest(http.MethodGet, url, nil)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var resp map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			tt.validate(t, resp)
		})
	}
}

// TestHandler_GetRoomDetail 測試獲取房間詳情 API
func TestHandler_GetRoomDetail(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建房間
	room, _ := manager.CreateRoom("測試房間", 4, "", internal.ModeCoop, "normal")
	err := manager.JoinRoom(room.ID, "player_001", "玩家一", "")
	require.NoError(t, err)

	// 測試獲取房間詳情
	url := fmt.Sprintf("/api/v1/rooms/%s", room.ID)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.SetPathValue("room_id", room.ID)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, room.ID, resp["room_id"])
	assert.Equal(t, "測試房間", resp["room_name"])
	assert.Equal(t, room.JoinCode, resp["join_code"])
	assert.Equal(t, float64(4), resp["max_players"])

	// 測試不存在的房間
	req = httptest.NewRequest(http.MethodGet, "/api/v1/rooms/non_existent", nil)
	req.SetPathValue("room_id", "non_existent")

	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestHandler_Health 測試健康檢查 API
func TestHandler_Health(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "healthy", resp["status"])
	assert.NotNil(t, resp["time"])
}

// TestHandler_Stats 測試統計 API
func TestHandler_Stats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建一些房間和玩家
	room1, _ := manager.CreateRoom("房間1", 4, "", internal.ModeCoop, "normal")
	room2, _ := manager.CreateRoom("房間2", 2, "", internal.ModeVersus, "hard")

	err := manager.JoinRoom(room1.ID, "player_001", "玩家一", "")
	require.NoError(t, err)
	err = manager.JoinRoom(room1.ID, "player_002", "玩家二", "")
	require.NoError(t, err)
	err = manager.JoinRoom(room2.ID, "player_003", "玩家三", "")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(2), resp["total_rooms"])
	assert.Equal(t, float64(3), resp["total_players"])
}

// TestHandler_CompleteGameFlow 測試完整遊戲流程
func TestHandler_CompleteGameFlow(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 步驟 1: 創建房間
	createBody := map[string]any{
		"room_name":   "遊戲房間",
		"max_players": 2,
		"game_mode":   "coop",
		"difficulty":  "normal",
	}
	body, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var createResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &createResp)
	roomID := createResp["room_id"].(string)

	// 步驟 2: 玩家加入
	for i := 1; i <= 2; i++ {
		joinBody := map[string]any{
			"player_id":   fmt.Sprintf("player_%03d", i),
			"player_name": fmt.Sprintf("玩家%d", i),
		}
		body, _ := json.Marshal(joinBody)
		url := fmt.Sprintf("/api/v1/rooms/%s/join", roomID)
		req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("room_id", roomID)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// 步驟 3: 選擇歌曲
	selectBody := map[string]any{
		"player_id": "player_001",
		"song_id":   "song_001",
	}
	body, _ = json.Marshal(selectBody)
	url := fmt.Sprintf("/api/v1/rooms/%s/select_song", roomID)
	req = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("room_id", roomID)

	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 步驟 4: 玩家準備
	for i := 1; i <= 2; i++ {
		readyBody := map[string]any{
			"player_id": fmt.Sprintf("player_%03d", i),
			"is_ready":  true,
		}
		body, _ := json.Marshal(readyBody)
		url := fmt.Sprintf("/api/v1/rooms/%s/ready", roomID)
		req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("room_id", roomID)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// 步驟 5: 開始遊戲
	startBody := map[string]any{
		"player_id": "player_001",
	}
	body, _ = json.Marshal(startBody)
	url = fmt.Sprintf("/api/v1/rooms/%s/start", roomID)
	req = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("room_id", roomID)

	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 驗證最終狀態
	room, _ := manager.GetRoom(roomID)
	assert.Equal(t, internal.StatusPlaying, room.Status)
}

// TestHandler_ConcurrentRequests 測試併發請求
func TestHandler_ConcurrentRequests(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建一個房間
	room, err := manager.CreateRoom("併發測試房間", 10, "", internal.ModeCoop, "normal")
	require.NoError(t, err)
	require.NotNil(t, room)

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// 併發加入房間
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			joinBody := map[string]any{
				"player_id":   fmt.Sprintf("player_%03d", idx),
				"player_name": fmt.Sprintf("玩家%d", idx),
			}
			body, _ := json.Marshal(joinBody)
			url := fmt.Sprintf("/api/v1/rooms/%s/join", room.ID)
			req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("room_id", room.ID)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// 所有請求都應該成功
	assert.Equal(t, 10, successCount)

	// 驗證房間狀態
	gotRoom, _ := manager.GetRoom(room.ID)
	assert.Equal(t, 10, gotRoom.GetPlayerCount())
}

// TestHandler_ErrorHandling 測試錯誤處理
func TestHandler_ErrorHandling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	tests := []struct {
		name           string
		method         string
		url            string
		body           string
		expectedStatus int
	}{
		{
			name:           "invalid JSON",
			method:         http.MethodPost,
			url:            "/api/v1/rooms/create",
			body:           "{invalid json}",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "non-existent room",
			method:         http.MethodGet,
			url:            "/api/v1/rooms/non_existent",
			body:           "",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "missing content type",
			method:         http.MethodPost,
			url:            "/api/v1/rooms/create",
			body:           `{"room_name": "test", "max_players": 4}`,
			expectedStatus: http.StatusCreated, // 應該仍然能處理
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.url, bytes.NewReader([]byte(tt.body)))
			if tt.url == "/api/v1/rooms/non_existent" {
				req.SetPathValue("room_id", "non_existent")
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

// TestHandler_PanicRecovery 測試 panic 恢復
func TestHandler_PanicRecovery(t *testing.T) {
	// 這個測試需要修改內部實現來觸發 panic
	// 在實際環境中，我們確保 panic 不會崩潰服務器

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 嘗試各種可能導致問題的請求
	testCases := []struct {
		name   string
		method string
		url    string
		body   any
	}{
		{
			name:   "null body",
			method: http.MethodPost,
			url:    "/api/v1/rooms/create",
			body:   nil,
		},
		{
			name:   "huge payload",
			method: http.MethodPost,
			url:    "/api/v1/rooms/create",
			body: map[string]any{
				"room_name":   string(make([]byte, 1000000)), // 1MB 字串
				"max_players": 4,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 確保不會 panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Handler panicked: %v", r)
				}
			}()

			var body []byte
			if tc.body != nil {
				body, _ = json.Marshal(tc.body)
			}

			req := httptest.NewRequest(tc.method, tc.url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// 只要不 panic 就算通過
			assert.NotNil(t, w)
		})
	}
}

// TestHandler_ResponseTime 測試響應時間
func TestHandler_ResponseTime(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping response time test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 測試各個端點的響應時間
	endpoints := []struct {
		name        string
		method      string
		url         string
		body        any
		maxDuration time.Duration
	}{
		{
			name:        "health check",
			method:      http.MethodGet,
			url:         "/health",
			maxDuration: 10 * time.Millisecond,
		},
		{
			name:        "stats",
			method:      http.MethodGet,
			url:         "/stats",
			maxDuration: 50 * time.Millisecond,
		},
		{
			name:        "list rooms",
			method:      http.MethodGet,
			url:         "/api/v1/rooms",
			maxDuration: 100 * time.Millisecond,
		},
		{
			name:   "create room",
			method: http.MethodPost,
			url:    "/api/v1/rooms/create",
			body: map[string]any{
				"room_name":   "測試房間",
				"max_players": 4,
				"game_mode":   "coop",
			},
			maxDuration: 100 * time.Millisecond,
		},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			var body []byte
			if ep.body != nil {
				body, _ = json.Marshal(ep.body)
			}

			req := httptest.NewRequest(ep.method, ep.url, bytes.NewReader(body))
			if ep.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}

			w := httptest.NewRecorder()

			start := time.Now()
			router.ServeHTTP(w, req)
			duration := time.Since(start)

			assert.Less(t, duration, ep.maxDuration,
				"Endpoint %s took %v, expected less than %v",
				ep.name, duration, ep.maxDuration)
		})
	}
}

// TestHandler_JoinRoomErrors 測試加入房間的錯誤情況
func TestHandler_JoinRoomErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建一個房間
	room, _ := manager.CreateRoom("測試房間", 2, "password", internal.ModeCoop, "normal")

	tests := []struct {
		name           string
		roomID         string
		requestBody    any
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "無效的請求體",
			roomID:         room.ID,
			requestBody:    "invalid json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "無效的請求格式",
		},
		{
			name:   "缺少玩家ID",
			roomID: room.ID,
			requestBody: map[string]any{
				"player_name": "玩家",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "玩家資訊不完整",
		},
		{
			name:   "錯誤的密碼",
			roomID: room.ID,
			requestBody: map[string]any{
				"player_id":   "player_001",
				"player_name": "玩家一",
				"password":    "wrong",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "密碼錯誤",
		},
		{
			name:   "房間不存在",
			roomID: "invalid_room",
			requestBody: map[string]any{
				"player_id":   "player_001",
				"player_name": "玩家一",
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "房間不存在",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+tt.roomID+"/join", bytes.NewReader(body))
			req.SetPathValue("room_id", tt.roomID)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			var resp map[string]any
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Contains(t, resp["error"], tt.expectedError)
		})
	}
}

// TestHandler_LeaveRoomErrors 測試離開房間的錯誤情況
func TestHandler_LeaveRoomErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建一個房間
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")

	tests := []struct {
		name           string
		roomID         string
		requestBody    any
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "無效的請求體",
			roomID:         room.ID,
			requestBody:    "invalid json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "無效的請求格式",
		},
		{
			name:           "缺少玩家ID",
			roomID:         room.ID,
			requestBody:    map[string]any{},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "玩家ID為必填",
		},
		{
			name:   "房間不存在",
			roomID: "invalid_room",
			requestBody: map[string]any{
				"player_id": "player_001",
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "房間不存在",
		},
		{
			name:   "玩家不在房間內",
			roomID: room.ID,
			requestBody: map[string]any{
				"player_id": "player_not_in_room",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "玩家不在房間內",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+tt.roomID+"/leave", bytes.NewReader(body))
			req.SetPathValue("room_id", tt.roomID)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			var resp map[string]any
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Contains(t, resp["error"], tt.expectedError)
		})
	}
}

// TestHandler_SetReadyErrors 測試設置準備狀態的錯誤情況
func TestHandler_SetReadyErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建一個房間
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")

	tests := []struct {
		name           string
		roomID         string
		requestBody    any
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "無效的請求體",
			roomID:         room.ID,
			requestBody:    "invalid json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "無效的請求格式",
		},
		{
			name:   "缺少玩家ID",
			roomID: room.ID,
			requestBody: map[string]any{
				"is_ready": true,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "玩家ID為必填",
		},
		{
			name:   "房間不存在",
			roomID: "invalid_room",
			requestBody: map[string]any{
				"player_id": "player_001",
				"is_ready":  true,
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "房間不存在",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+tt.roomID+"/ready", bytes.NewReader(body))
			req.SetPathValue("room_id", tt.roomID)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			var resp map[string]any
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Contains(t, resp["error"], tt.expectedError)
		})
	}
}

// TestHandler_SelectSongErrors 測試選擇歌曲的錯誤情況
func TestHandler_SelectSongErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建一個房間並加入玩家（成為房主）
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")
	_ = manager.JoinRoom(room.ID, "player_001", "玩家一", "") // 第一個玩家成為房主
	_ = manager.JoinRoom(room.ID, "player_002", "玩家二", "") // 滿員，狀態變成 StatusPreparing

	tests := []struct {
		name           string
		roomID         string
		requestBody    any
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "無效的請求體",
			roomID:         room.ID,
			requestBody:    "invalid json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "無效的請求格式",
		},
		{
			name:   "非房主選歌",
			roomID: room.ID,
			requestBody: map[string]any{
				"player_id": "player_002",
				"song_id":   "song_001",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "只有房主可以選歌",
		},
		{
			name:   "房間不存在",
			roomID: "invalid_room",
			requestBody: map[string]any{
				"player_id": "player_001",
				"song_id":   "song_001",
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "房間不存在",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+tt.roomID+"/select_song", bytes.NewReader(body))
			req.SetPathValue("room_id", tt.roomID)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			var resp map[string]any
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Contains(t, resp["error"], tt.expectedError)
		})
	}
}

// TestHandler_StartGameErrors 測試開始遊戲的錯誤情況
func TestHandler_StartGameErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := internal.NewManager(logger)
	defer manager.Stop()

	handler := internal.NewHandler(manager, logger)
	router := handler.Routes()

	// 創建一個房間並加入玩家（成為房主）
	room, _ := manager.CreateRoom("測試房間", 2, "", internal.ModeCoop, "normal")
	_ = manager.JoinRoom(room.ID, "player_001", "玩家一", "") // 第一個玩家成為房主

	tests := []struct {
		name           string
		roomID         string
		requestBody    any
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "無效的請求體",
			roomID:         room.ID,
			requestBody:    "invalid json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "無效的請求格式",
		},
		{
			name:           "缺少玩家ID",
			roomID:         room.ID,
			requestBody:    map[string]any{},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "玩家ID為必填",
		},
		{
			name:   "房間不存在",
			roomID: "invalid_room",
			requestBody: map[string]any{
				"player_id": "player_001",
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "房間不存在",
		},
		{
			name:   "非房主開始遊戲",
			roomID: room.ID,
			requestBody: map[string]any{
				"player_id": "player_002",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "只有房主可以開始遊戲",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+tt.roomID+"/start", bytes.NewReader(body))
			req.SetPathValue("room_id", tt.roomID)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			var resp map[string]any
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Contains(t, resp["error"], tt.expectedError)
		})
	}
}
