# Room Management 系統設計文檔

## 問題定義

### 業務需求
構建多人遊戲房間管理系統，支援：
- **房間生命週期**：創建、加入、遊戲進行、解散
- **實時狀態同步**：玩家操作立即通知所有成員
- **並發控制**：多個玩家同時操作房間
- **自動清理**：空閒房間自動回收

### 技術指標
| 指標 | 目標值 | 挑戰 |
|------|--------|------|
| **並發房間數** | 1,000 | 如何高效管理大量房間？ |
| **WebSocket 連接** | 4,000 | 如何處理大量並發連接？ |
| **廣播延遲** | P99 < 50ms | 如何保證實時性？ |
| **狀態一致性** | 100% | 並發操作下如何保證狀態正確？ |
| **記憶體占用** | < 20 MB (1000 房) | 如何控制記憶體使用？ |

---

## 設計決策樹

### 決策 1：如何實現實時狀態同步？

```
需求：玩家操作（加入、準備）需要立即通知房間內所有人

方案 A：HTTP 輪詢（Polling）
   機制：客戶端每 N 秒請求一次狀態
   問題：
   - 延遲高：最壞情況 N 秒延遲
   - 浪費資源：大量無效請求（狀態未變化）
   - 伸縮性差：1000 客戶端 × 1 req/s = 1000 QPS

   計算：
   - 1000 個房間 × 4 玩家 = 4000 連接
   - 輪詢間隔 1 秒 = 4000 QPS
   - 90% 請求無狀態變化（浪費）

方案 B：Server-Sent Events (SSE)
   機制：服務器單向推送事件流
   優勢：實時推送、HTTP 兼容
   問題：
   - 單向通信：客戶端操作仍需 HTTP POST
   - 連接管理複雜：需要維護兩套協議
   - 代理不友好：某些代理會緩衝 SSE

選擇方案 C：WebSocket
   機制：全雙工持久連接
   優勢：
   - 實時雙向：服務器推送 + 客戶端操作
   - 低延遲：無需輪詢，事件立即推送
   - 高效：單連接複用（相比輪詢省 90% 請求）

   權衡：
   - 代理兼容性：需要 HTTP/1.1 Upgrade（現代環境已普及）
   - 連接保持成本：需要心跳檢測（keepalive）
```

**選擇：方案 C（WebSocket）**

**實現細節：**
```go
// WebSocket 升級
upgrader := websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
}
conn, _ := upgrader.Upgrade(w, r, nil)

// 事件推送
for event := range room.Events() {
    conn.WriteJSON(event)
}
```

---

### 決策 2：如何管理房間狀態？

```
問題：房間有複雜的狀態轉換，如何保證邏輯正確性？

方案 A：布林標誌組合（isWaiting, isPlaying, isFinished）
   問題：
   - 狀態矛盾：可能同時 isWaiting=true, isPlaying=true
   - 難以擴展：新增狀態需要修改多處邏輯
   - 易出錯：忘記重置某個標誌

   範例（錯誤案例）：
   room.isWaiting = false
   room.isPlaying = true
   // 忘記設置 isReady = false
   // 導致狀態不一致

方案 B：字符串狀態（status = "waiting"）
   問題：
   - 拼寫錯誤：if status == "wating" (編譯期無法檢測)
   - 無約束：可以設置任意字符串
   - 轉換規則不明確：任何狀態都能轉到任何狀態

選擇方案 C：有限狀態機（Finite State Machine, FSM）
   機制：
   - 枚舉所有狀態：waiting, preparing, ready, playing, finished, closed
   - 定義轉換規則：只允許特定轉換
   - 操作檢查狀態：每個操作前驗證當前狀態

   優勢：
   - 類型安全：編譯期檢查（使用 const）
   - 邏輯清晰：狀態轉換圖可視化
   - 易於測試：窮舉所有狀態和轉換

   狀態轉換圖：
   waiting → preparing → ready → playing → finished → closed
              ↑____________↓
```

**選擇：方案 C（有限狀態機）**

**實現細節：**
```go
type RoomStatus string

const (
    StatusWaiting   RoomStatus = "waiting"
    StatusPreparing RoomStatus = "preparing"
    StatusReady     RoomStatus = "ready"
    StatusPlaying   RoomStatus = "playing"
    StatusFinished  RoomStatus = "finished"
    StatusClosed    RoomStatus = "closed"
)

// 狀態檢查
func (r *Room) AddPlayer(playerID, name string) error {
    if r.Status != StatusWaiting && r.Status != StatusPreparing {
        return fmt.Errorf("房間狀態不允許加入: %s", r.Status)
    }
    // ... 加入邏輯
}

// 自動狀態轉換
if len(r.Players) == r.MaxPlayers && r.Status == StatusWaiting {
    r.Status = StatusPreparing  // 人滿 → 準備選歌
}
```

---

### 決策 3：如何處理並發操作？

```
問題：多個玩家同時操作同一房間（加入、準備、選歌）

方案 A：無鎖（樂觀並發）
   問題：
   - 競態條件：兩個玩家同時加入，都通過容量檢查
   - 結果：超過房間人數上限

   時序範例：
   T1: 玩家 A 檢查容量（3/4，通過） 
   T2: 玩家 B 檢查容量（3/4，通過）
   T3: 玩家 A 加入（4/4）
   T4: 玩家 B 加入（5/4）超限！

方案 B：sync.Mutex（互斥鎖）
   機制：所有操作加鎖
   問題：
   - 性能瓶頸：讀操作（查詢房間列表）也需要鎖
   - 讀寫不分：獲取房間狀態阻塞其他讀取者

   分析：
   - 操作比例：讀取 90%（查詢狀態）+ 寫入 10%（加入、準備）
   - Mutex 讀寫都互斥 → 浪費 90% 的並發潛力

選擇方案 C：sync.RWMutex（讀寫鎖）
   機制：
   - 讀鎖（RLock）：多個讀取者可以並發
   - 寫鎖（Lock）：寫入者排他訪問

   優勢：
   - 讀取並發：多個玩家可以同時查詢房間狀態
   - 寫入安全：操作（加入、準備）互斥
   - 性能：讀多寫少場景優化（10x+ 提升）

   權衡：
   - 複雜度增加：需要區分讀寫操作
   - 鎖升級問題：不能從讀鎖升級到寫鎖（會死鎖）
```

**選擇：方案 C（讀寫鎖 RWMutex）**

**實現細節：**
```go
type Room struct {
    Mu sync.RWMutex
    // ... fields
}

// 讀操作：查詢狀態（並發友好）
func (r *Room) GetState() map[string]any {
    r.Mu.RLock()         // 多個讀取者可以並發
    defer r.Mu.RUnlock()
    // ... 讀取狀態
}

// 寫操作：加入玩家（互斥）
func (r *Room) AddPlayer(id, name string) error {
    r.Mu.Lock()          // 排他訪問
    defer r.Mu.Unlock()
    // ... 修改狀態
}

// 錯誤範例：鎖升級死鎖
func (r *Room) BadMethod() {
    r.Mu.RLock()
    // ... 讀取
    r.Mu.Lock()   // 死鎖！無法從讀鎖升級到寫鎖
    // ...
}
```

---

### 決策 4：如何實現事件廣播？

```
問題：玩家操作後，需要通知房間內其他玩家

方案 A：同步廣播（直接在操作中發送 WebSocket）
   機制：
   for _, conn := range room.connections {
       conn.WriteJSON(event)
   }

   問題：
   - 阻塞操作：慢消費者阻塞整個房間（某個玩家網速慢）
   - 鎖持有時間長：發送期間持有房間鎖
   - 級聯失敗：一個連接錯誤影響所有玩家

   計算：
   - 4 個玩家，1 個玩家網絡延遲 100ms
   - 操作延遲 = 100ms × 4 = 400ms
   - 其他玩家被迫等待

方案 B：多 goroutine 廣播（每個連接一個 goroutine）
   機制：
   for _, conn := range room.connections {
       go conn.WriteJSON(event)
   }

   問題：
   - goroutine 洪水：每次廣播創建 N 個 goroutine
   - 資源消耗：1000 房間 × 4 玩家 × 每秒 10 事件 = 40,000 goroutine/s
   - 順序無保證：事件可能亂序到達

選擇方案 C：事件驅動架構（Channel + 異步消費）
   機制：
   - 操作完成 → 發送事件到 channel
   - 後台 goroutine 異步消費 channel
   - 廣播到所有 WebSocket 連接

   流程：
   1. 玩家操作（如加入） → 修改房間狀態
   2. 發送事件到 events channel（非阻塞）
   3. 釋放鎖，操作立即返回
   4. 後台 goroutine 從 channel 讀取事件
   5. 廣播到所有 WebSocket 連接

   優勢：
   - 解耦：業務邏輯與通知邏輯分離
   - 非阻塞：操作不等待廣播完成
   - 背壓控制：channel 緩衝（如 100）應對突發
   - 資源可控：每個房間 1 個消費 goroutine

   權衡：
   - 異步性：事件到達有微小延遲（通常 < 1ms）
   - 內存開銷：channel 緩衝占用內存
   - 事件丟失：如果 channel 滿，丟棄事件（需要監控）
```

**選擇：方案 C（事件驅動架構）**

**實現細節：**
```go
type Room struct {
    events chan Event  // 緩衝 100 個事件
    // ...
}

// 操作：發送事件（非阻塞）
func (r *Room) AddPlayer(id, name string) error {
    r.Mu.Lock()
    // ... 修改狀態
    r.Mu.Unlock()

    // 非阻塞發送事件
    r.sendEvent(Event{
        Type: "player_joined",
        Data: map[string]any{"player": player},
    })
    return nil
}

func (r *Room) sendEvent(event Event) {
    select {
    case r.events <- event:  // 嘗試發送
    default:                 // channel 滿，丟棄事件
        // 生產環境應該：記錄日誌、監控丟失率
    }
}

// 消費：後台廣播（每個房間一個 goroutine）
func broadcastLoop(room *Room) {
    for event := range room.Events() {
        for _, conn := range room.connections {
            go conn.WriteJSON(event)  // 異步發送給每個客戶端
        }
    }
}
```

---

### 決策 5：如何處理空閒房間清理？

```
問題：玩家全部離開後，房間佔用內存未釋放

方案 A：立即刪除（玩家離開時刪除空房間）
   問題：
   - 誤刪：玩家短暫斷線（網絡抖動）後重連，房間已消失
   - 用戶體驗差：需要重新創建房間

   場景：
   - 玩家 A、B、C、D 在房間內
   - 玩家 A 網絡抖動 5 秒
   - 系統認為 A 離開，房間變為 3 人
   - 最後一人離開，房間刪除
   - 玩家 A 重連後發現房間消失

方案 B：永不刪除（手動管理）
   問題：
   - 內存洩漏：遺棄房間持續佔用內存
   - 資源浪費：1000 個遺棄房間 × 2KB = 2 MB

   計算：
   - 每天創建 10,000 個房間
   - 10% 玩家不正常離開（直接關閉瀏覽器）
   - 一個月後：10,000 × 30 × 0.1 = 30,000 個遺棄房間
   - 內存占用：30,000 × 2KB = 60 MB

選擇方案 C：超時自動清理（延遲刪除 + 定期掃描）
   機制：
   - 追蹤最後活動時間（lastActive）
   - 定期掃描（如每分鐘）
   - 超過閾值（如空房間 5 分鐘、任何房間 30 分鐘）自動關閉

   策略：
   - 空房間（0 人）：5 分鐘後刪除
   - 有人房間：30 分鐘後刪除（防止異常）
   - 已關閉房間：立即標記為過期

   優勢：
   - 容錯：短暫斷線可以重連
   - 自動清理：避免內存洩漏
   - 可配置：根據業務調整閾值

   權衡：
   - 延遲回收：內存不會立即釋放
   - 掃描開銷：需要定期遍歷所有房間（可優化為優先隊列）
```

**選擇：方案 C（超時自動清理）**

**實現細節：**
```go
type Room struct {
    lastActive time.Time
    // ...
}

// 更新活動時間
func (r *Room) AddPlayer(id, name string) error {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    // ... 操作邏輯

    r.lastActive = time.Now()  // 記錄活動時間
}

// 過期檢查
func (r *Room) IsExpired() bool {
    r.Mu.RLock()
    defer r.Mu.RUnlock()

    now := time.Now()

    // 規則 1：房間最多存在 30 分鐘
    if now.Sub(r.CreatedAt) > 30*time.Minute {
        return true
    }

    // 規則 2：無人房間 5 分鐘後過期
    if len(r.Players) == 0 && now.Sub(r.lastActive) > 5*time.Minute {
        return true
    }

    return false
}

// 定期清理（管理器層面）
func cleanupLoop(manager *RoomManager) {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        for _, room := range manager.GetAllRooms() {
            if room.IsExpired() {
                room.Close("timeout")
                manager.DeleteRoom(room.ID)
            }
        }
    }
}
```

---

## 擴展性分析

### 當前架構容量

```
單機內存存儲：
- 每個房間：約 2 KB（結構體 + 4 個玩家）
- 每個 WebSocket 連接：約 4 KB（緩衝區）
- 1000 個房間 × 4 玩家 = 4000 連接
- 內存占用：1000 × 2KB + 4000 × 4KB = 18 MB

單機性能：
- WebSocket 連接：4,000 並發（Go 輕量 goroutine）
- 事件廣播：10,000 msg/s（內存 channel 高效）
- 房間操作：5,000 QPS（RWMutex 優化）

結論：單機可支撐 1000 房間（4000 玩家同時在線）
```

### 10x 擴展（10,000 房間）

```
瓶頸分析：
內存：10,000 × 2KB + 40,000 × 4KB = 180 MB（仍可接受）
單機限制：無法水平擴展（內存存儲）
廣播性能：100,000 msg/s（接近 goroutine 調度極限）

方案 1：垂直擴展（增強單機）
- 增加 CPU、內存
- 效果：可支撐 ~5,000 房間
- 成本：$200/月（AWS）
- 限制：仍然是單點故障

方案 2：引入 Redis 存儲
- 房間狀態：存儲到 Redis（持久化 + 多實例共享）
- Pub/Sub：使用 Redis Pub/Sub 廣播事件
- 負載均衡：Sticky Session（同一房間路由到同一實例）
- 效果：10 個實例 × 1000 房間 = 10,000 房間
- 複雜度：需要處理跨實例廣播

架構變化：
當前：
  Client → WebSocket → Room (memory)

優化後：
  Client → Load Balancer (Sticky Session)
           ↓
           ├─ Instance 1 ─┐
           ├─ Instance 2 ─┤→ Redis (state)
           └─ Instance 3 ─┘    ↓
                              Pub/Sub (broadcast)
```

### 100x 擴展（100,000 房間）

```
需要架構升級：

1. WebSocket Gateway
   - 專門處理 WebSocket 連接（與業務邏輯分離）
   - 連接層：40 個實例 × 10,000 連接 = 400,000 連接
   - 邏輯層：10 個實例處理房間邏輯
   - 通信：通過 Redis Pub/Sub 或消息隊列

2. Redis Cluster
   - 分片：16 個 shard
   - 每個 shard：約 6,000 房間
   - 持久化：RDB + AOF（防止重啟數據丟失）

3. 消息隊列（替代 Pub/Sub）
   - 使用 Kafka 或 RabbitMQ
   - 保證消息順序（同一房間的事件有序）
   - 容錯：消息持久化，實例崩潰不丟消息

4. 分片策略
   - 按 room_id hash 分片
   - 同一房間的所有操作路由到同一實例
   - 避免跨實例鎖（降低複雜度）

架構：
Client
  ↓
Load Balancer (L7, WebSocket aware)
  ↓
├─ WebSocket Gateway (40 instances)
│  ├─ 處理 WebSocket 連接
│  └─ 訂閱消息隊列
  ↓
├─ Room Logic Service (10 instances)
│  ├─ 房間狀態管理
│  └─ 發布事件到消息隊列
  ↓
├─ Redis Cluster (16 shards)
│  └─ 房間狀態存儲
  ↓
└─ Kafka (事件廣播)

成本估算：
- WebSocket Gateway：40 × $50 = $2,000/月
- Room Logic：10 × $100 = $1,000/月
- Redis Cluster：16 × $100 = $1,600/月
- Kafka：3 節點 × $200 = $600/月
- 總計：約 $5,200/月
```

---

## 實現範圍標註

### 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **狀態機** | `room.go:24-51` | FSM 設計、狀態轉換規則 |
| **讀寫鎖** | `room.go:79-135` | RWMutex、並發安全 |
| **事件驅動** | `room.go:137-142, 518-540` | Channel、異步通知 |
| **超時清理** | `room.go:461-484` | 資源管理、過期檢測 |
| **TOCTOU 修復** | `room.go:133-134, 455-458, 528-532` | atomic.Bool、競態條件 |

### 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **WebSocket 實現** | 聚焦核心狀態管理 | Gorilla WebSocket、心跳檢測 |
| **Redis 存儲** | 單機已足夠示範 | Redis + Pub/Sub 多實例廣播 |
| **認證授權** | 簡化示範流程 | JWT token、房間密碼驗證 |
| **監控指標** | 聚焦業務邏輯 | Prometheus、廣播延遲分位數 |
| **消息順序保證** | 增加複雜度 | 序列號、Kafka 分區 |

### 生產環境額外需要

```
1. 連接管理
   - 心跳檢測：60 秒無消息視為斷線
   - 重連機制：斷線後 30 秒內可重連
   - 連接限流：單 IP 最多 10 個連接（防止濫用）
   - 優雅關閉：通知客戶端後關閉連接

2. 消息可靠性
   - 消息確認：客戶端 ACK 機制
   - 失敗重試：3 次重試 + 指數退避
   - 死信隊列：重試失敗的消息歸檔
   - 順序保證：同一房間的事件有序到達

3. 安全性
   - 房間密碼：bcrypt 加密存儲
   - 操作權限：只有房主可以選歌、開始遊戲
   - 速率限制：單玩家每秒最多 10 次操作
   - 輸入驗證：防止注入攻擊（如惡意房間名）

4. 可觀測性
   - Metrics：房間數、連接數、廣播延遲、事件丟失率
   - Tracing：操作鏈路追蹤（從 API 到廣播）
   - Logging：結構化日誌（房間 ID、玩家 ID）
   - Alerting：廣播延遲 > 100ms 告警

5. 擴展性
   - Sticky Session：同一玩家路由到同一實例
   - 分片策略：按房間 ID hash 分配
   - 動態擴展：根據房間數自動擴容
```

---

## 關鍵設計原則總結

### 1. 有限狀態機（清晰的業務邏輯）
```
明確的狀態 + 嚴格的轉換規則 = 邏輯正確性

waiting → preparing → ready → playing → finished

每個操作前檢查狀態，防止非法操作
```

### 2. 讀寫鎖優化（並發性能）
```
讀多寫少場景：sync.RWMutex

讀操作（90%）：並發執行（10x 吞吐量）
寫操作（10%）：互斥執行（保證一致性）

性能提升：單 Mutex 1,000 QPS → RWMutex 10,000 QPS
```

### 3. 事件驅動架構（解耦 + 異步）
```
操作 → 修改狀態 → 發送事件（非阻塞）→ 後台廣播

優勢：
- 操作快速返回（不等待廣播）
- 慢消費者不影響操作
- 背壓控制（channel 緩衝）
```

### 4. 超時自動清理（資源管理）
```
lastActive + 定期掃描 → 自動刪除過期房間

規則：
- 空房間：5 分鐘
- 任何房間：30 分鐘（防止異常）

避免內存洩漏，同時允許短暫斷線重連
```

---

## 延伸閱讀

### 相關系統設計問題
- 如何設計一個**聊天室系統**？（類似問題）
- 如何設計一個**在線協作編輯器**？（實時同步）
- 如何設計一個**遊戲匹配系統**？（房間分配）

### 系統設計模式
- **Finite State Machine**：狀態機模式
- **Event-Driven Architecture**：事件驅動架構
- **Pub/Sub Pattern**：發布訂閱模式
- **Read-Write Lock**：讀寫鎖優化

### 實現技術
- **WebSocket**：全雙工實時通信
- **Gorilla WebSocket**：Go WebSocket 庫
- **Redis Pub/Sub**：分布式消息廣播
- **Sticky Session**：負載均衡會話保持

---

## 總結

Room Management 展示了**實時系統**的經典設計模式：

1. **狀態機**：清晰的業務邏輯，防止非法操作
2. **讀寫鎖**：並發優化，讀多寫少場景
3. **事件驅動**：解耦業務邏輯與通知邏輯
4. **異步廣播**：非阻塞操作，保證響應速度

**核心思想：** 用明確的狀態機規範業務邏輯，用事件驅動解耦模塊，用讀寫鎖優化並發性能。

**適用場景：** 多人遊戲、在線協作、聊天室、實時競價等需要狀態管理和實時通信的場景

**不適用：** 單人應用、離線應用、無狀態服務

**與 Counter Service 對比：**
| 維度 | Counter Service | Room Management |
|------|----------------|-----------------|
| **核心挑戰** | 高頻寫入 | 實時同步 |
| **一致性** | 最終一致性 | 強一致性（狀態機） |
| **通信模式** | 單向（客戶端寫） | 雙向（WebSocket） |
| **擴展瓶頸** | 資料庫寫入 | WebSocket 連接數 |
| **存儲** | Redis + PostgreSQL | 內存（可擴展到 Redis） |
