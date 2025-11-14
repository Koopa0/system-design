## 練習二：遊戲房間管理系統

您的音樂遊戲新增了多人合作模式，2-4 名玩家可以一起演奏同一首歌曲。玩家可以創建房間、加入房間、準備開始、選擇歌曲等。這個系統需要管理房間的生命週期，處理玩家的各種操作，並確保狀態同步

### 功能需求

#### 1. 房間生命週期

房間有以下狀態：

- `waiting`：等待玩家加入
- `preparing`：所有玩家到齊，選擇歌曲中
- `ready`：所有玩家已準備，即將開始
- `playing`：遊戲進行中
- `finished`：遊戲結束，顯示結果
- `closed`：房間關閉

#### 2. 核心功能

```go
// 創建房間
POST /api/v1/rooms/create
Request: {
    "room_name": "大師挑戰",
    "max_players": 4,
    "password": "1234",  // 可選，私人房間
    "game_mode": "coop", // coop, versus, practice
    "difficulty": "hard"
}
Response: {
    "room_id": "room_abc123",
    "join_code": "ABC123",  // 簡短的加入碼
    "status": "waiting"
}

// 加入房間
POST /api/v1/rooms/{room_id}/join
Request: {
    "player_id": "player_123",
    "player_name": "小明",
    "password": "1234"  // 如果需要
}
Response: {
    "success": true,
    "room_state": {
        "room_id": "room_abc123",
        "players": [...],
        "status": "waiting"
    }
}

// 離開房間
POST /api/v1/rooms/{room_id}/leave
Request: {
    "player_id": "player_123"
}

// 玩家準備/取消準備
POST /api/v1/rooms/{room_id}/ready
Request: {
    "player_id": "player_123",
    "is_ready": true
}

// 選擇歌曲（房主才能操作）
POST /api/v1/rooms/{room_id}/select_song
Request: {
    "player_id": "player_123",
    "song_id": "song_456"
}

// 開始遊戲（所有人準備後，房主可以開始）
POST /api/v1/rooms/{room_id}/start
Request: {
    "player_id": "player_123"
}

// 獲取房間列表
GET /api/v1/rooms?status=waiting&mode=coop&page=1&limit=20
Response: {
    "rooms": [
        {
            "room_id": "room_abc123",
            "room_name": "大師挑戰",
            "current_players": 2,
            "max_players": 4,
            "status": "waiting",
            "has_password": true,
            "game_mode": "coop",
            "host_name": "小明"
        }
    ],
    "total": 45,
    "page": 1
}

// 獲取房間詳情
GET /api/v1/rooms/{room_id}
Response: {
    "room_id": "room_abc123",
    "players": [
        {
            "player_id": "player_123",
            "player_name": "小明",
            "is_host": true,
            "is_ready": false,
            "joined_at": "2024-01-15T10:30:00Z"
        }
    ],
    "selected_song": {
        "song_id": "song_456",
        "song_name": "Butterfly",
        "difficulty": "hard",
        "duration": 180
    },
    "status": "preparing"
}
```

#### 3. 實時通知（WebSocket）

```javascript
// WebSocket 連接
ws://localhost:8080/ws/rooms/{room_id}?player_id=player_123

// 服務端推送的事件類型
{
    "event": "player_joined",
    "data": {
        "player": {...},
        "current_players": 3
    }
}

{
    "event": "player_left",
    "data": {
        "player_id": "player_456",
        "new_host": "player_789"  // 如果房主離開
    }
}

{
    "event": "player_ready_changed",
    "data": {
        "player_id": "player_123",
        "is_ready": true
    }
}

{
    "event": "song_selected",
    "data": {
        "song": {...}
    }
}

{
    "event": "game_starting",
    "data": {
        "countdown": 3  // 3秒後開始
    }
}

{
    "event": "room_closed",
    "data": {
        "reason": "host_left"  // 或 "inactive", "game_ended"
    }
}
```

#### 4. 房間管理規則

- 房間最多存在 30 分鐘，超時自動關閉
- 如果房間內無人，5 分鐘後自動關閉
- 房主離開時，自動轉移房主給加入時間最早的玩家
- 遊戲開始後，不允許新玩家加入
- 每個玩家同時只能在一個房間內

### 非功能需求

#### 效能要求

- 支援同時 1000 個活躍房間
- 每個房間最多 4 個玩家
- WebSocket 訊息延遲 < 100ms
- 房間列表查詢 < 200ms

#### 一致性要求

- 房間狀態必須一致，避免出現「幽靈玩家」
- 玩家斷線重連時，能夠恢復房間狀態
- 防止重複加入、重複準備等異常操作

#### 高可用要求

- 服務重啟時，保留所有房間狀態
- 支援優雅關閉，通知所有連接的客戶端
- WebSocket 斷線自動重連機制

### 技術規範

#### 建議架構

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Client    │────▶│  API Server │────▶│    Redis    │
│  (Flutter)  │     │    (Gin)    │     │ (Room State)│
└─────────────┘     └─────────────┘     └─────────────┘
       │                    │                    │
       │                    ▼                    ▼
       │            ┌─────────────┐     ┌─────────────┐
       └───────────▶│  WebSocket  │────▶│ PostgreSQL  │
                    │   Server    │     │  (History)  │
                    │  (Gorilla)  │     └─────────────┘
                    └─────────────┘
```

### 驗收標準

#### 功能測試

1. **完整流程測試**：創建房間 → 玩家加入 → 選歌 → 準備 → 開始遊戲
2. **併發加入測試**：多個玩家同時加入同一房間，不能超過上限
3. **房主轉移測試**：房主離開後，驗證房主正確轉移
4. **過期清理測試**：驗證房間按規則自動清理

#### WebSocket 測試

1. **廣播測試**：一個玩家的操作，其他玩家都能收到通知
2. **斷線重連測試**：客戶端斷線重連後，能恢復狀態
3. **壓力測試**：100 個房間，每個 4 個玩家，共 400 個 WebSocket 連接

#### 異常處理測試

1. **重複操作**：重複加入、重複準備等
2. **權限驗證**：非房主不能選歌、不能開始遊戲
3. **狀態機測試**：在錯誤的狀態下執行操作應該被拒絕
