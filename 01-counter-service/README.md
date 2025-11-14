## 練習一：遊戲活躍度計數服務

您正在為一款音樂節奏遊戲開發後端服務。產品經理希望在遊戲主介面顯示「當前在線人數」和「今日活躍玩家數」，讓玩家感受到遊戲的熱度。這個看似簡單的功能，實際上需要處理高併發更新和查詢

### 功能需求

#### 1. 計數器管理

- 系統需要管理多個計數器，每個計數器有唯一的名稱
- 支援的計數器類型：
  - `online_players`：當前在線人數（玩家登入+1，登出-1）
  - `daily_active_users`：今日活躍用戶數（每個用戶每天只計算一次）
  - `total_games_played`：今日遊戲局數（每完成一局+1）
  - 自定義計數器：運營活動可能需要臨時計數器

#### 2. API

```go
// 增加計數
POST /api/v1/counter/{name}/increment
Request Body: {
    "value": 1,           // 增加的值，預設為1
    "user_id": "u123456", // 可選，用於去重計數
    "metadata": {}        // 可選，附加資訊
}
Response: {
    "success": true,
    "current_value": 12345
}

// 減少計數
POST /api/v1/counter/{name}/decrement
Request Body: {
    "value": 1,
    "user_id": "u123456"
}
Response: {
    "success": true,
    "current_value": 12344
}

// 獲取當前值
GET /api/v1/counter/{name}
Response: {
    "name": "online_players",
    "value": 12344,
    "last_updated": "2024-01-15T10:30:00Z"
}

// 批量獲取
GET /api/v1/counters?names=online_players,daily_active_users
Response: {
    "counters": [
        {"name": "online_players", "value": 12344},
        {"name": "daily_active_users", "value": 45678}
    ]
}

// 重置計數器
POST /api/v1/counter/{name}/reset
Request Body: {
    "admin_token": "secret_token"  // 需要管理員權限
}
```

#### 3. 去重計數邏輯

對於 `daily_active_users` 這類計數器，同一個 user_id 在同一天內多次呼叫 increment，只應該計數一次。系統需要記住哪些用戶已經被計數過。

#### 4. 自動重置機制

- `daily_active_users` 和 `total_games_played` 應該在每天凌晨 0 點自動重置
- 重置前需要將當天的資料歸檔（至少保留 7 天的歷史資料）

### 非功能需求

#### 效能要求

- 支援至少 10,000 QPS 的增減操作
- 查詢延遲 P99 < 10ms
- 批量查詢最多 10 個計數器，延遲 P99 < 20ms

#### 可靠性要求

- 計數器的值必須準確，不能因為併發導致計數錯誤
- 系統重啟後，計數器的值必須恢復
- 即使 Redis 崩潰，也要有降級方案

### 驗收標準

#### 基本功能測試（必須通過）

1. **併發正確性測試**：啟動 1000 個 goroutine 同時對同一個計數器 increment，最終值必須正確
2. **去重測試**：同一個 user_id 多次 increment `daily_active_users`，值只增加 1
3. **自動重置測試**：模擬時間到達凌晨 0 點，驗證計數器重置和資料歸檔
4. **持久化測試**：重啟服務後，計數器值保持不變

#### 效能測試

```bash
# 使用 wrk 或 ab 進行壓力測試
wrk -t12 -c400 -d30s --latency http://localhost:8080/api/v1/counter/online_players/increment
```

要求：QPS > 10,000，P99 延遲 < 10ms

#### 可靠性測試

1. **Redis 故障模擬**：停止 Redis，服務應該降級到只讀模式，從 PostgreSQL 讀取
2. **記憶體洩漏測試**：運行 24 小時，記憶體使用應該穩定
3. **優雅關閉測試**：收到 SIGTERM 信號時，應該完成當前請求再關閉
