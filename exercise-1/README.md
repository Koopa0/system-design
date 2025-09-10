練習一：遊戲活躍度計數服務（入門級）
業務背景
您正在為一款音樂節奏遊戲開發後端服務。產品經理希望在遊戲主介面顯示「當前在線人數」和「今日活躍玩家數」，讓玩家感受到遊戲的熱度。這個看似簡單的功能，實際上需要處理高併發更新和查詢。
功能需求

1. 計數器管理

系統需要管理多個計數器，每個計數器有唯一的名稱
支援的計數器類型：

online_players：當前在線人數（玩家登入+1，登出-1）
daily_active_users：今日活躍用戶數（每個用戶每天只計算一次）
total_games_played：今日遊戲局數（每完成一局+1）
自定義計數器：運營活動可能需要臨時計數器

2. API 介面規範
   go// 增加計數
   POST /api/v1/counter/{name}/increment
   Request Body: {
   "value": 1, // 增加的值，預設為1
   "user_id": "u123456", // 可選，用於去重計數
   "metadata": {} // 可選，附加資訊
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
"admin_token": "secret_token" // 需要管理員權限
} 3. 去重計數邏輯
對於 daily_active_users 這類計數器，同一個 user_id 在同一天內多次呼叫 increment，只應該計數一次。系統需要記住哪些用戶已經被計數過。4. 自動重置機制

daily_active_users 和 total_games_played 應該在每天凌晨 0 點自動重置
重置前需要將當天的資料歸檔（至少保留 7 天的歷史資料）

非功能需求
效能要求

支援至少 10,000 QPS 的增減操作
查詢延遲 P99 < 10ms
批量查詢最多 10 個計數器，延遲 P99 < 20ms

可靠性要求

計數器的值必須準確，不能因為併發導致計數錯誤
系統重啟後，計數器的值必須恢復
即使 Redis 崩潰，也要有降級方案

可觀測性要求

記錄每個 API 的請求日誌
監控 QPS、延遲、錯誤率
當計數器值異常（如突然歸零）時發出告警

技術規範
建議的技術棧

Web 框架: net/http
儲存：Redis/ ristretto 你要權衡並給出解釋（主存儲）+ PostgreSQL（備份和歷史資料）
配置管理：Viper
日誌： slog
部署: docker-compose

資料模型設計
sql-- PostgreSQL 表結構
CREATE TABLE counters (
id SERIAL PRIMARY KEY,
name VARCHAR(100) UNIQUE NOT NULL,
current_value BIGINT DEFAULT 0,
created_at TIMESTAMP DEFAULT NOW(),
updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE counter_history (
id SERIAL PRIMARY KEY,
counter_name VARCHAR(100) NOT NULL,
date DATE NOT NULL,
final_value BIGINT NOT NULL,
unique_users TEXT[], -- 陣列儲存去重的用戶ID
metadata JSONB,
created_at TIMESTAMP DEFAULT NOW(),
UNIQUE(counter_name, date)
);

-- Redis 資料結構
-- 計數器當前值：STRING
-- Key: counter:{name}
-- Value: 12345

-- 去重集合：SET
-- Key: counter:{name}:users:{date}
-- Members: user_id1, user_id2, ...

-- 計數器元資料：HASH
-- Key: counter:{name}:meta
-- Fields: last_updated, created_at, type
驗收標準
基本功能測試（必須通過）

併發正確性測試：啟動 1000 個 goroutine 同時對同一個計數器 increment，最終值必須正確
去重測試：同一個 user_id 多次 increment daily_active_users，值只增加 1
自動重置測試：模擬時間到達凌晨 0 點，驗證計數器重置和資料歸檔
持久化測試：重啟服務後，計數器值保持不變

效能測試
bash# 使用 wrk 或 ab 進行壓力測試
wrk -t12 -c400 -d30s --latency http://localhost:8080/api/v1/counter/online_players/increment
要求：QPS > 10,000，P99 延遲 < 10ms
可靠性測試

Cache 故障模擬：停止 Redis，服務應該降級到只讀模式，從 PostgreSQL 讀取
記憶體洩漏測試：運行 24 小時，記憶體使用應該穩定
優雅關閉測試：收到 SIGTERM 信號時，應該完成當前請求再關閉

程式碼品質要求

所有公開函數都有文檔註釋
使用 golangci-lint 檢查，無 critical 問題

進階挑戰（選做）

滑動視窗計數：實現「過去 1 小時活躍用戶數」這種滑動視窗計數器
分散式部署：支援多實例部署，使用 Redis Cluster 或分片
實時推送：當計數器值變化時，透過 WebSocket 推送給訂閱的客戶端
限流保護：對每個 IP 或 user_id 實施限流，防止惡意刷量
