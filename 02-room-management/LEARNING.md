# 學習要點：Room Management System

> 房間管理系統展示了狀態機設計、WebSocket 實時通訊、並發控制等核心概念

## 🎯 學習目標

完成本案例後，你將掌握：

1. ✅ **狀態機設計**：有限狀態自動機（FSM）的實踐
2. ✅ **WebSocket 通訊**：長連接管理、心跳檢測
3. ✅ **並發安全**：sync.RWMutex、原子操作
4. ✅ **事件驅動架構**：事件廣播、訂閱模式
5. ✅ **資源管理**：房間生命週期、自動清理

---

## 📊 問題場景

### 真實案例

**多人音樂遊戲**需要實現遊戲大廳：
- 🎮 玩家創建房間，等待其他人加入
- 👥 房主選擇歌曲，所有人看到實時更新
- ✅ 所有玩家準備好後，開始遊戲
- 💬 實時通訊（玩家進出、狀態變化）

### 挑戰

1. **狀態一致性**：多個玩家看到的房間狀態必須一致
2. **實時性**：狀態變化必須立即推送給所有人
3. **併發控制**：多個玩家同時操作不能出錯
4. **資源清理**：空房間、過期房間自動清理
5. **斷線重連**：玩家網絡抖動後能恢復狀態

---

## 💡 核心概念詳解

### 1. 狀態機設計（Finite State Machine）

#### 為什麼需要狀態機？

房間有明確的生命週期和狀態轉換規則：

```
waiting → preparing → ready → playing → finished → closed
   ↓         ↓                    ↓
 closed    closed              finished
```

**狀態機的好處**：
- 明確哪些操作在哪些狀態下允許
- 防止非法狀態轉換
- 代碼更清晰、可維護

#### 狀態定義

參考 `room.go:9-19`：

```go
type RoomStatus string

const (
    StatusWaiting   RoomStatus = "waiting"   // 等待玩家加入
    StatusPreparing RoomStatus = "preparing" // 選擇歌曲中
    StatusReady     RoomStatus = "ready"     // 所有人準備好
    StatusPlaying   RoomStatus = "playing"   // 遊戲進行中
    StatusFinished  RoomStatus = "finished"  // 遊戲結束
    StatusClosed    RoomStatus = "closed"    // 房間關閉
)
```

#### 狀態轉換規則

| 當前狀態 | 允許的操作 | 轉換到 |
|---------|-----------|--------|
| **waiting** | 加入房間 | waiting（未滿）/ preparing（滿了） |
| **preparing** | 選歌、準備 | ready（全部準備）|
| **ready** | 開始遊戲 | playing |
| **playing** | 結束遊戲 | finished |
| **任何狀態** | 關閉房間 | closed |

**關鍵代碼** `room.go:98-149`：

```go
func (r *Room) AddPlayer(playerID, playerName string) error {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    // ✅ 狀態檢查：只有 waiting/preparing 才能加入
    if r.Status != StatusWaiting && r.Status != StatusPreparing {
        return fmt.Errorf("房間狀態不允許加入: %s", r.Status)
    }

    // ... 加入邏輯 ...

    // 自動狀態轉換：滿人 → preparing
    if len(r.Players) == r.MaxPlayers && r.Status == StatusWaiting {
        r.Status = StatusPreparing
    }

    return nil
}
```

---

### 2. WebSocket 長連接管理

#### 為什麼用 WebSocket？

| 方案 | 優點 | 缺點 | 延遲 |
|------|------|------|------|
| **輪詢（Polling）** | 簡單 | 浪費帶寬、服務器壓力大 | 1-5 秒 |
| **長輪詢（Long Polling）** | 實時性較好 | 複雜、連接數多 | 1 秒 |
| **WebSocket** ✅ | 全雙工、低延遲、省資源 | 需要支持 | <100ms |

**決策**：房間管理需要實時推送（玩家進出、狀態變化），WebSocket 最適合。

#### Hub 模式

參考 `websocket.go:13-58`：

```go
type WebSocketHub struct {
    connections map[string]map[string]*Connection
    // roomID -> playerID -> Connection
}

// 廣播消息到房間內所有玩家
func (hub *WebSocketHub) broadcast(roomID string, message []byte) {
    for _, conn := range hub.connections[roomID] {
        conn.Send <- message  // 非阻塞發送
    }
}
```

**設計要點**：
- 每個房間一個連接池
- 每個玩家一個 WebSocket 連接
- 集中管理，方便廣播

---

### 3. 心跳檢測（Heartbeat）

#### 為什麼需要心跳？

**問題場景**：
- 用戶關閉瀏覽器（沒有正常斷開）
- 網絡中斷（防火牆、路由器）
- WebSocket 連接"假活"（TCP 連接還在，但應用層已死）

**解決方案**：定期 Ping/Pong

參考 `websocket.go:296-343`：

```go
func (c *Connection) writePump() {
    ticker := time.NewTicker(54 * time.Second)  // 每 54 秒 Ping 一次
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            // 發送 Ping
            if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                return  // 連接斷開
            }
        }
    }
}

func (c *Connection) readPump() {
    // 設置 60 秒超時
    c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))

    // Pong Handler
    c.Conn.SetPongHandler(func(string) error {
        // 收到 Pong，重置超時
        c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
        return nil
    })
}
```

**為什麼是 54 秒？**
- 許多代理服務器 60 秒無數據會斷開連接
- 54 秒 < 60 秒，確保連接保活

---

### 4. 並發安全：讀寫鎖

#### 問題

多個 goroutine 同時操作房間：
- HTTP 請求 goroutine：加入房間、離開、準備
- WebSocket goroutine：發送事件
- 清理 goroutine：過期檢查

**如果沒有鎖**：
```go
// ❌ 競爭條件（Race Condition）
player := room.Players[playerID]  // goroutine 1 讀取
delete(room.Players, playerID)    // goroutine 2 刪除
player.IsReady = true              // goroutine 1 寫入 → PANIC!
```

#### 解決方案：sync.RWMutex

參考 `room.go:65`：

```go
type Room struct {
    // ...
    Mu sync.RWMutex  // 讀寫鎖
}

// 讀操作（多個 goroutine 可以同時讀）
func (r *Room) GetState() map[string]any {
    r.Mu.RLock()         // 讀鎖
    defer r.Mu.RUnlock()

    // 讀取數據...
    return state
}

// 寫操作（同時只能一個 goroutine 寫）
func (r *Room) AddPlayer(...) error {
    r.Mu.Lock()          // 寫鎖
    defer r.Mu.Unlock()

    // 修改數據...
    return nil
}
```

**RWMutex vs Mutex**：
- **Mutex**：讀寫都互斥（同時只能一個操作）
- **RWMutex**：讀讀不互斥，讀寫/寫寫互斥（讀多寫少的場景更優）

**房間管理特點**：
- 讀取頻繁（查詢房間狀態、列表）
- 寫入較少（加入、離開、準備）
- **RWMutex 更合適** ✅

---

### 5. 事件驅動架構

#### 為什麼用事件？

**傳統方式**：
```go
// ❌ 緊耦合
func (r *Room) AddPlayer(...) {
    // 業務邏輯
    r.Players[playerID] = player

    // 直接調用通知
    hub.broadcast(roomID, "player joined")  // 耦合！
}
```

**事件驅動**：
```go
// ✅ 解耦
func (r *Room) AddPlayer(...) {
    // 業務邏輯
    r.Players[playerID] = player

    // 發送事件（不關心誰處理）
    r.sendEvent(Event{Type: "player_joined", Data: player})
}

// 另一個地方監聽事件
for event := range room.Events() {
    hub.broadcast(roomID, event)  // 解耦！
}
```

**好處**：
- Room 不依賴 WebSocketHub
- 可以輕易添加新的事件處理（如日誌、分析）
- 單元測試更容易

參考 `websocket.go:181-231`：

```go
func (hub *WebSocketHub) roomEventLoop() {
    for {
        // 檢查所有房間的事件
        for _, roomID := range roomIDs {
            room, _ := hub.manager.GetRoom(roomID)

            // 非阻塞讀取事件
            select {
            case event := <-room.Events():
                message, _ := json.Marshal(event)
                hub.broadcast(roomID, message)
            default:
                // 沒有事件，繼續下一個房間
            }
        }
    }
}
```

---

### 6. 房間生命週期管理

#### 自動清理機制

**問題場景**：
- 玩家創建房間後斷線離開
- 房間一直存在，浪費內存
- 長時間運行後內存洩漏

**解決方案**：定時清理

參考 `manager.go:251-296`：

```go
func (m *Manager) cleanupLoop() {
    ticker := time.NewTicker(1 * time.Minute)  // 每分鐘檢查一次

    for {
        select {
        case <-ticker.C:
            // 查找過期房間
            for roomID, room := range m.rooms {
                if room.IsExpired() {
                    room.Close("timeout")
                    m.removeRoom(roomID)
                }
            }
        }
    }
}

// 過期規則
func (r *Room) IsExpired() bool {
    // 1. 房間存在超過 30 分鐘
    if time.Since(r.CreatedAt) > 30*time.Minute {
        return true
    }

    // 2. 空房間 5 分鐘無活動
    if len(r.Players) == 0 && time.Since(r.lastActive) > 5*time.Minute {
        return true
    }

    return false
}
```

**設計考量**：
- **30 分鐘總時長限制**：防止房間無限存在
- **5 分鐘空房間清理**：快速釋放資源
- **lastActive 更新**：任何操作都更新活動時間

---

## 🔍 深入分析：架構演進

### 階段 1：內存存儲（0 - 1,000 在線房間）

```
API Server (內存存儲 Map)
    ↓
WebSocket 連接
```

**優點**：簡單、快速
**缺點**：
- 服務重啟數據丟失 ❌
- 無法水平擴展（多實例）

**當前實現** ✅

---

### 階段 2：Redis 持久化（1,000 - 10,000 房間）

```
API Server → Redis (房間狀態)
           → WebSocket Hub
```

**改進**：
- 服務重啟可恢復房間
- 多實例共享狀態

**實現要點**：
```go
// 房間狀態存 Redis
redis.HSet("room:123", "status", "waiting")
redis.HSet("room:123", "players", json.Marshal(players))

// 事件用 Redis Pub/Sub
redis.Publish("room:123:events", event)
```

---

### 階段 3：多實例 + 粘性會話（10,000+ 房間）

```
                   ┌─> API Server 1 (WebSocket Hub 1)
Load Balancer ────┼─> API Server 2 (WebSocket Hub 2)
(Sticky Session)  └─> API Server 3 (WebSocket Hub 3)
                         ↓
                      Redis
```

**關鍵**：
- **粘性會話（Sticky Session）**：同一玩家的請求路由到同一實例
- **Redis Pub/Sub**：跨實例廣播事件

**為什麼需要粘性？**
- WebSocket 是有狀態的（長連接）
- 同一玩家的 HTTP 和 WebSocket 必須在同一實例

---

### 階段 4：消息隊列解耦（大規模）

```
API Server → Kafka (房間事件)
WebSocket Hub ← Kafka Consumer
           ↓
         Redis
```

**好處**：
- API Server 和 WebSocket Hub 完全解耦
- 可以獨立擴展
- 事件持久化（重放）

---

## 📈 效能分析

### 容量估算

假設：
- 同時 1,000 個活躍房間
- 每個房間 4 個玩家 = 4,000 個 WebSocket 連接
- 每個連接內存：約 10KB
- 總內存：4,000 × 10KB = 40MB ✅ 可接受

### 瓶頸分析

| 瓶頸 | 原因 | 解決方案 |
|------|------|---------|
| **單實例連接數** | 操作系統文件描述符限制（默認 1024） | 調整 `ulimit -n 65535` |
| **廣播性能** | 遍歷所有連接發送消息 | 使用 Goroutine Pool |
| **內存使用** | 大量長連接 | 實現連接池、超時斷開 |

---

## 🛠️ 實踐建議

### 運行測試

```bash
# 完整流程測試
go test -v -run TestRoomFullFlow

# 併發加入測試
go test -v -run TestConcurrentJoin

# 房主轉移測試
go test -v -run TestHostTransfer

# WebSocket 壓力測試
go test -v -run TestWebSocketStress
```

### WebSocket 客戶端測試

```javascript
// JavaScript 客戶端
const ws = new WebSocket('ws://localhost:8080/ws/rooms/room_123?player_id=player_1');

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log('收到事件:', data.event, data.data);
};

ws.send(JSON.stringify({
    type: 'chat',
    text: '大家好！'
}));
```

### 監控指標

- **房間數量**：當前活躍房間
- **WebSocket 連接數**：總連接數
- **事件延遲**：事件發送到接收的時間
- **斷線率**：每分鐘斷開連接數

---

## 💭 擴展思考

### 1. 如何實現斷線重連？

**客戶端策略**：
```javascript
let reconnectAttempts = 0;
const maxReconnectDelay = 30000;

function connect() {
    const ws = new WebSocket(url);

    ws.onclose = () => {
        const delay = Math.min(1000 * Math.pow(2, reconnectAttempts), maxReconnectDelay);
        reconnectAttempts++;

        setTimeout(connect, delay);  // 指數退避重連
    };

    ws.onopen = () => {
        reconnectAttempts = 0;  // 重置計數
    };
}
```

**服務端**：
- 保留玩家在房間的狀態（不立即移除）
- 重連後驗證 playerID，恢復 WebSocket 連接

---

### 2. 如何處理"幽靈玩家"？

**問題**：
- 玩家斷線，但服務端不知道
- 房間顯示"在線"，但實際已離開

**解決方案**：
1. **心跳超時**：60 秒無 Pong → 斷開連接 → 從房間移除
2. **定期檢查**：每 5 分鐘清理長時間無活動的玩家
3. **客戶端主動報告**：頁面關閉時發送離開請求（beforeunload）

---

### 3. 如何實現跨房間聊天（全局聊天）？

**方案 1：特殊房間**
```go
// 全局聊天室，所有人自動加入
globalRoom := manager.GetRoom("global")
```

**方案 2：獨立頻道**
```go
// Redis Pub/Sub
redis.Publish("global_chat", message)

// 所有實例訂閱
redis.Subscribe("global_chat")
```

---

## 📚 延伸閱讀

### 相關系統設計案例

- **21-chat-system**: 完整的聊天系統（1對1、群聊）
- **20-news-feed**: 事件廣播的另一個應用
- **30-youtube**: 直播聊天室（類似場景）

### 經典資料

- **WebSocket RFC 6455**
  - [官方規範](https://tools.ietf.org/html/rfc6455)

- **Gorilla WebSocket**
  - [GitHub](https://github.com/gorilla/websocket)
  - [範例代碼](https://github.com/gorilla/websocket/tree/master/examples/chat)

### 相關技術

- **CRDT（無衝突複製數據類型）**
  - 用於分布式狀態同步
  - Google Docs 使用的技術

- **Operational Transformation (OT)**
  - 另一種實時協作算法
  - 衝突解決

---

## ✅ 自我檢測

學完本案例後，你應該能夠回答：

- [ ] 狀態機有哪些狀態？允許的轉換是什麼？
- [ ] 為什麼用 WebSocket 而不是輪詢？
- [ ] 心跳檢測的作用是什麼？為什麼是 54 秒？
- [ ] RWMutex 和 Mutex 的區別？何時用 RWMutex？
- [ ] 事件驅動架構的好處是什麼？
- [ ] 房間過期的兩個條件是什麼？
- [ ] 如何從單實例擴展到多實例？

**如果你能清晰回答以上問題，恭喜你已經掌握了房間管理系統的核心概念！🎉**

---

## 🎯 面試技巧

當面試官問：**"設計一個遊戲房間系統"**

### 第 1 步：明確需求（5 分鐘）

**功能需求**：
- 創建、加入、離開房間
- 房主權限（選歌、開始遊戲）
- 實時狀態同步

**非功能需求**：
- 支持 1,000 個活躍房間
- WebSocket 延遲 < 100ms
- 高可用性（99.9%）

### 第 2 步：容量估算（3 分鐘）

```
活躍房間: 1,000
每房間玩家: 4
WebSocket 連接: 4,000

內存：
- 每個連接 10KB
- 總計: 4,000 × 10KB = 40MB

帶寬（心跳）：
- 每連接 54 秒一次 Ping（~100 bytes）
- QPS: 4,000 / 54 ≈ 74 QPS
- 帶寬: 74 × 100 bytes ≈ 7.4 KB/s ✅ 可忽略
```

### 第 3 步：高層設計（10 分鐘）

畫出架構圖：
```
Client (WebSocket) → Load Balancer → API Server
                                     → Manager (內存)
                                     → WebSocket Hub
```

說明關鍵組件：
- **Manager**：房間管理（創建、查詢、清理）
- **Room**：狀態機、玩家管理
- **WebSocket Hub**：連接管理、事件廣播

### 第 4 步：深入設計（20 分鐘）

- **狀態機**：畫出狀態轉換圖
- **並發安全**：RWMutex 使用
- **事件系統**：如何廣播
- **清理機制**：過期房間

### 第 5 步：擴展與優化（7 分鐘）

**瓶頸**：
- 單實例 WebSocket 連接數限制

**優化**：
- Redis 持久化（服務重啟）
- 多實例 + 粘性會話
- Redis Pub/Sub 跨實例廣播

**記住**：先設計簡單可行的方案，再討論擴展！
