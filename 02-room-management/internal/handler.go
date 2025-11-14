package internal

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Handler HTTP 請求處理器
type Handler struct {
	manager *Manager
	logger  *slog.Logger
}

// NewHandler 創建 HTTP 處理器
func NewHandler(manager *Manager, logger *slog.Logger) *Handler {
	return &Handler{
		manager: manager,
		logger:  logger,
	}
}

// Routes 設定路由
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// 中間件鏈
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		return h.recoverer(h.loggerMiddleware(handler))
	}

	// 房間管理 API
	mux.HandleFunc("POST /api/v1/rooms/create", wrap(h.createRoom))
	mux.HandleFunc("POST /api/v1/rooms/{room_id}/join", wrap(h.joinRoom))
	mux.HandleFunc("POST /api/v1/rooms/{room_id}/leave", wrap(h.leaveRoom))
	mux.HandleFunc("POST /api/v1/rooms/{room_id}/ready", wrap(h.setReady))
	mux.HandleFunc("POST /api/v1/rooms/{room_id}/select_song", wrap(h.selectSong))
	mux.HandleFunc("POST /api/v1/rooms/{room_id}/start", wrap(h.startGame))
	mux.HandleFunc("GET /api/v1/rooms", wrap(h.listRooms))
	mux.HandleFunc("GET /api/v1/rooms/{room_id}", wrap(h.getRoomDetail))

	// 健康檢查
	mux.HandleFunc("GET /health", wrap(h.health))
	mux.HandleFunc("GET /stats", wrap(h.stats))

	return mux
}

// 請求結構
type createRoomRequest struct {
	RoomName   string   `json:"room_name"`
	MaxPlayers int      `json:"max_players"`
	Password   string   `json:"password,omitempty"`
	GameMode   GameMode `json:"game_mode"`
	Difficulty string   `json:"difficulty"`
}

type joinRoomRequest struct {
	PlayerID   string `json:"player_id"`
	PlayerName string `json:"player_name"`
	Password   string `json:"password,omitempty"`
}

type leaveRoomRequest struct {
	PlayerID string `json:"player_id"`
}

type readyRequest struct {
	PlayerID string `json:"player_id"`
	IsReady  bool   `json:"is_ready"`
}

type selectSongRequest struct {
	PlayerID string `json:"player_id"`
	SongID   string `json:"song_id"`
}

type startGameRequest struct {
	PlayerID string `json:"player_id"`
}

// createRoom 創建房間
func (h *Handler) createRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, "無效的請求格式", http.StatusBadRequest)
		return
	}

	// 驗證參數
	if req.RoomName == "" {
		h.errorResponse(w, "房間名稱不能為空", http.StatusBadRequest)
		return
	}
	if req.MaxPlayers < 2 || req.MaxPlayers > 100 {
		h.errorResponse(w, "玩家數量必須在 2-100 之間", http.StatusBadRequest)
		return
	}
	if req.GameMode == "" {
		req.GameMode = ModeCoop
	}

	// 創建房間
	room, err := h.manager.CreateRoom(
		req.RoomName,
		req.MaxPlayers,
		req.Password,
		req.GameMode,
		req.Difficulty,
	)
	if err != nil {
		h.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回結果
	h.jsonResponse(w, map[string]any{
		"room_id":   room.ID,
		"join_code": room.JoinCode,
		"status":    room.Status,
	}, http.StatusCreated)
}

// joinRoom 加入房間
func (h *Handler) joinRoom(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("room_id")

	var req joinRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, "無效的請求格式", http.StatusBadRequest)
		return
	}

	// 驗證參數
	if req.PlayerID == "" || req.PlayerName == "" {
		h.errorResponse(w, "玩家資訊不完整", http.StatusBadRequest)
		return
	}

	// 加入房間
	if err := h.manager.JoinRoom(roomID, req.PlayerID, req.PlayerName, req.Password); err != nil {
		status := http.StatusBadRequest
		errMsg := err.Error()
		if strings.HasPrefix(errMsg, "房間不存在") {
			status = http.StatusNotFound
		} else if errMsg == "密碼錯誤" {
			status = http.StatusBadRequest // 改為 400 符合測試期望
		}
		h.errorResponse(w, errMsg, status)
		return
	}

	// 獲取房間狀態
	room, err := h.manager.GetRoom(roomID)
	if err != nil {
		h.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponse(w, map[string]any{
		"success":    true,
		"room_state": room.GetState(),
	}, http.StatusOK)
}

// leaveRoom 離開房間
func (h *Handler) leaveRoom(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("room_id")

	var req leaveRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, "無效的請求格式", http.StatusBadRequest)
		return
	}

	if req.PlayerID == "" {
		h.errorResponse(w, "玩家ID為必填", http.StatusBadRequest)
		return
	}

	if err := h.manager.LeaveRoom(roomID, req.PlayerID); err != nil {
		status := http.StatusBadRequest
		errMsg := err.Error()
		if strings.HasPrefix(errMsg, "房間不存在") {
			status = http.StatusNotFound
		}
		h.errorResponse(w, errMsg, status)
		return
	}

	h.jsonResponse(w, map[string]any{
		"success": true,
	}, http.StatusOK)
}

// setReady 設置準備狀態
func (h *Handler) setReady(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("room_id")

	var req readyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, "無效的請求格式", http.StatusBadRequest)
		return
	}

	if req.PlayerID == "" {
		h.errorResponse(w, "玩家ID為必填", http.StatusBadRequest)
		return
	}

	if err := h.manager.SetPlayerReady(roomID, req.PlayerID, req.IsReady); err != nil {
		status := http.StatusBadRequest
		errMsg := err.Error()
		if strings.HasPrefix(errMsg, "房間不存在") {
			status = http.StatusNotFound
		}
		h.errorResponse(w, errMsg, status)
		return
	}

	h.jsonResponse(w, map[string]any{
		"success": true,
	}, http.StatusOK)
}

// selectSong 選擇歌曲
func (h *Handler) selectSong(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("room_id")

	var req selectSongRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, "無效的請求格式", http.StatusBadRequest)
		return
	}

	if req.PlayerID == "" {
		h.errorResponse(w, "玩家ID為必填", http.StatusBadRequest)
		return
	}

	if req.SongID == "" {
		h.errorResponse(w, "歌曲資訊為必填", http.StatusBadRequest)
		return
	}

	// 這裡簡化處理，實際應該從歌曲庫查詢
	song := &Song{
		ID:         req.SongID,
		Name:       "示例歌曲",
		Difficulty: "normal",
		Duration:   180,
	}

	if err := h.manager.SelectSong(roomID, req.PlayerID, song); err != nil {
		status := http.StatusBadRequest
		errMsg := err.Error()
		if strings.HasPrefix(errMsg, "房間不存在") {
			status = http.StatusNotFound
		}
		h.errorResponse(w, errMsg, status)
		return
	}

	h.jsonResponse(w, map[string]any{
		"success": true,
	}, http.StatusOK)
}

// startGame 開始遊戲
func (h *Handler) startGame(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("room_id")

	var req startGameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, "無效的請求格式", http.StatusBadRequest)
		return
	}

	if req.PlayerID == "" {
		h.errorResponse(w, "玩家ID為必填", http.StatusBadRequest)
		return
	}

	if err := h.manager.StartGame(roomID, req.PlayerID); err != nil {
		status := http.StatusBadRequest
		errMsg := err.Error()
		if strings.HasPrefix(errMsg, "房間不存在") {
			status = http.StatusNotFound
		}
		h.errorResponse(w, errMsg, status)
		return
	}

	h.jsonResponse(w, map[string]any{
		"success": true,
	}, http.StatusOK)
}

// listRooms 列出房間
func (h *Handler) listRooms(w http.ResponseWriter, r *http.Request) {
	// 解析查詢參數
	query := r.URL.Query()

	status := RoomStatus(query.Get("status"))
	mode := GameMode(query.Get("mode"))

	page := 1
	if p := query.Get("page"); p != "" {
		if val, err := strconv.Atoi(p); err == nil && val > 0 {
			page = val
		}
	}

	limit := 20
	if l := query.Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 && val <= 100 {
			limit = val
		}
	}

	rooms, total := h.manager.ListRooms(status, mode, page, limit)

	h.jsonResponse(w, map[string]any{
		"rooms": rooms,
		"total": total,
		"page":  page,
	}, http.StatusOK)
}

// getRoomDetail 獲取房間詳情
func (h *Handler) getRoomDetail(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("room_id")

	room, err := h.manager.GetRoom(roomID)
	if err != nil {
		h.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	h.jsonResponse(w, room.GetState(), http.StatusOK)
}

// health 健康檢查
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	h.jsonResponse(w, map[string]any{
		"status": "healthy",
		"time":   time.Now().Unix(),
	}, http.StatusOK)
}

// stats 統計資訊
func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	stats := h.manager.Stats()
	h.jsonResponse(w, stats, http.StatusOK)
}

// jsonResponse 返回 JSON 響應
func (h *Handler) jsonResponse(w http.ResponseWriter, data any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("編碼 JSON 失敗", "error", err)
	}
}

// errorResponse 返回錯誤響應
func (h *Handler) errorResponse(w http.ResponseWriter, message string, status int) {
	h.jsonResponse(w, map[string]any{
		"error": message,
	}, status)
}

// loggerMiddleware 日誌中間件
func (h *Handler) loggerMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 包裝 ResponseWriter 以獲取狀態碼
		ww := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next(ww, r)

		h.logger.Info("HTTP 請求",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.statusCode,
			"duration", time.Since(start))
	}
}

// recoverer panic 恢復中間件
func (h *Handler) recoverer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				h.logger.Error("處理請求時發生 panic",
					"error", err,
					"method", r.Method,
					"path", r.URL.Path)

				h.errorResponse(w, "內部伺服器錯誤", http.StatusInternalServerError)
			}
		}()

		next(w, r)
	}
}

// responseWriter 包裝 ResponseWriter 以獲取狀態碼
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}
