# Room Management 系統設計文檔

## 凌晨兩點的緊急上線

2024 年 3 月 15 日凌晨 2:00，「節奏大師」遊戲即將上線多人對戰模式。技術總監 David 盯著監控面板，心裡卻隱隱不安。

「房間管理系統測試通過了嗎？」他問道。

「通過了！單元測試全綠，壓測也沒問題。」後端工程師 Amy 自信滿滿。

但 David 心裡清楚：真實世界從來不會像測試環境那麼友好。

## 第一次災難：狀態混亂（2024/03/15 上線當天）

### 最初的實現：布林標誌組合

Amy 最初用多個布林標誌管理房間狀態：

```go
type Room struct {
    ID          string
    Players     map[string]*Player
    MaxPlayers  int

    // 狀態標誌
    isWaiting   bool
    isPreparing bool
    isReady     bool
    isPlaying   bool
    isFinished  bool
    isClosed    bool
}

func (r *Room) StartGame() error {
    if !r.isReady {
        return errors.New("房間未準備好")
    }

    r.isReady = false
    r.isPlaying = true
    // ... 開始遊戲
    return nil
}

func (r *Room) AddPlayer(id, name string) error {
    if r.isPlaying {
        return errors.New("遊戲進行中，無法加入")
    }

    // ... 加入玩家
    r.isWaiting = false
    r.isPreparing = true
    // 忘記重置 isReady！
    return nil
}
```

上線 30 分鐘後，客服電話被打爆。

**事故現場：**
```
玩家 A 創建房間 → isWaiting=true
玩家 B、C、D 加入 → isPreparing=true
所有人準備完畢 → isReady=true
開始遊戲 → isPlaying=true, isReady=false

玩家 A 中途退出
重新加入時執行 AddPlayer()
  → isPreparing=true
  → 忘記設置 isReady=false

結果：isPreparing=true, isPlaying=true, isReady=true (三個狀態同時為真！)

後續操作完全混亂：
- StartGame() 檢查 isReady → 通過 → 重複開始遊戲
- AddPlayer() 檢查 !isPlaying → 失敗 → 但 isPreparing=true 又允許加入
```

**災難數據：**
- 上線 1 小時內：327 個房間狀態異常
- 用戶投訴：「遊戲開始了還能加人」
- 數據庫記錄：23% 的房間出現狀態矛盾
- 崩潰率：比平時高 15 倍

Amy 緊急加班到凌晨 5 點，在每個操作前加了一堆 `reset` 邏輯：

```go
func (r *Room) AddPlayer(id, name string) error {
    // 重置所有其他狀態
    r.isReady = false
    r.isPlaying = false
    r.isFinished = false
    // ...

    r.isPreparing = true
    // ... 加入玩家
}
```

但這只是治標不治本，代碼變得又臭又長。

### 凌晨 5 點的頓悟

David 看著 Amy 疲憊的臉，嘆了口氣：

「問題的根源在哪裡？」

Amy 想了想：「狀態太多了，我都不確定該設置哪些標誌。」

「對，你需要的不是 6 個布林值，而是**有限狀態機**。」David 在白板上畫出：

```
房間只能處於一個狀態：
waiting → preparing → ready → playing → finished → closed
           ↑__________↓

而不是 2^6 = 64 種可能的組合
```

### 改進方案：有限狀態機 (FSM)

David 重寫了狀態管理：

```go
type RoomStatus string

const (
    StatusWaiting   RoomStatus = "waiting"    // 等待玩家加入
    StatusPreparing RoomStatus = "preparing"  // 準備中（選歌等）
    StatusReady     RoomStatus = "ready"      // 準備完畢
    StatusPlaying   RoomStatus = "playing"    // 遊戲進行中
    StatusFinished  RoomStatus = "finished"   // 遊戲結束
    StatusClosed    RoomStatus = "closed"     // 房間關閉
)

type Room struct {
    ID         string
    Status     RoomStatus  // 唯一狀態
    Players    map[string]*Player
    MaxPlayers int
    CreatedAt  time.Time
    mu         sync.RWMutex
}

// 狀態轉換規則
func (r *Room) AddPlayer(id, name string) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    // 明確的狀態檢查
    if r.Status != StatusWaiting && r.Status != StatusPreparing {
        return fmt.Errorf("房間狀態 %s 不允許加入", r.Status)
    }

    if len(r.Players) >= r.MaxPlayers {
        return errors.New("房間已滿")
    }

    r.Players[id] = &Player{ID: id, Name: name}

    // 自動狀態轉換
    if len(r.Players) == r.MaxPlayers {
        r.Status = StatusPreparing
    }

    return nil
}

func (r *Room) StartGame() error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if r.Status != StatusReady {
        return fmt.Errorf("房間狀態 %s，無法開始遊戲", r.Status)
    }

    r.Status = StatusPlaying
    // ... 開始遊戲邏輯
    return nil
}
```

**狀態轉換圖：**
```
waiting (等待玩家)
   ↓ (人滿)
preparing (選歌、準備)
   ↓ (所有人準備完畢)
ready (可以開始)
   ↓ (房主開始遊戲)
playing (進行中)
   ↓ (遊戲結束)
finished (結算)
   ↓ (5分鐘後或手動關閉)
closed (關閉)

特殊轉換：
preparing ←→ waiting (玩家退出導致人數不足)
```

**改進效果（2024/03/16 部署）：**
- 狀態異常：327 次 → 0 次
- 代碼行數：原本 50 行狀態檢查 → 15 行狀態機
- 單元測試：從「測不完的組合」到「6 個狀態 × 5 個操作 = 30 個測試」
- 用戶投訴：下降 92%

## 第二次災難：並發混亂（2024/03/20）

### 背景：熱門主播的對戰活動

3 月 20 日晚上 8 點，知名主播「電音小王子」發起粉絲對戰活動。

**8:00 PM** - 主播宣布：「房間號 STREAMER-001，來啊！」

**8:01 PM** - 500 名粉絲同時湧入

**8:02 PM** - 系統崩潰

### 問題：無鎖並發的競態條件

當時的代碼沒有任何鎖保護：

```go
func (r *Room) AddPlayer(id, name string) error {
    // 沒有鎖！

    if len(r.Players) >= r.MaxPlayers {
        return errors.New("房間已滿")
    }

    // 問題：檢查和修改之間有時間窗口
    r.Players[id] = &Player{ID: id, Name: name}
    return nil
}
```

**競態條件時序圖：**
```
時間軸：房間上限 4 人，當前 3 人

T1: 玩家 A 檢查人數 (3/4) → 通過 ✓
T2: 玩家 B 檢查人數 (3/4) → 通過 ✓
T3: 玩家 C 檢查人數 (3/4) → 通過 ✓
T4: 玩家 A 加入 → 人數變為 4
T5: 玩家 B 加入 → 人數變為 5 (超限！)
T6: 玩家 C 加入 → 人數變為 6 (超限！)

結果：4 人房間擠進了 6 個人
```

**災難數據（2024/03/20 20:00-21:00）：**
- 超限房間：1,247 個（應該最多 4 人的房間出現 5-8 人）
- 最嚴重案例：房間上限 4 人，實際加入 11 人
- 遊戲崩潰：因為邏輯預期最多 4 人，數組越界
- 主播直播事故：「這什麼破遊戲，11 個人搶 4 個位置！」

### 第一次嘗試：sync.Mutex 全局鎖

運維工程師 Kevin 緊急修復：

```go
type Room struct {
    mu sync.Mutex  // 互斥鎖
    // ...
}

func (r *Room) AddPlayer(id, name string) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if len(r.Players) >= r.MaxPlayers {
        return errors.New("房間已滿")
    }

    r.Players[id] = &Player{ID: id, Name: name}
    return nil
}

func (r *Room) GetState() map[string]any {
    r.mu.Lock()         // 讀取也要鎖！
    defer r.mu.Unlock()

    return map[string]any{
        "status":  r.Status,
        "players": r.Players,
        // ...
    }
}
```

問題解決了，但性能崩了。

**性能災難（2024/03/21）：**
```
壓力測試結果：
- 查詢房間狀態 QPS：25,000 → 2,500 (下降 90%)
- 平均響應時間：5ms → 120ms (增加 24 倍)

原因分析：
- 玩家操作比例：讀取 90% (查看房間狀態) + 寫入 10% (加入、準備)
- sync.Mutex 讀寫不分：查詢也要排隊
- 1000 個房間，每個房間 10 次/秒查詢 = 10,000 次鎖競爭

瓶頸：
大量玩家只是查看房間狀態（「這局幾個人？」「誰還沒準備？」）
卻因為 Mutex 被迫排隊等待
```

David 看著監控：「讀多寫少的場景，用錯鎖了。」

### 改進方案：sync.RWMutex 讀寫鎖

```go
type Room struct {
    Mu sync.RWMutex  // 讀寫鎖
    // ...
}

// 讀操作：多個讀取者可以並發
func (r *Room) GetState() map[string]any {
    r.Mu.RLock()         // 讀鎖：允許並發
    defer r.Mu.RUnlock()

    return map[string]any{
        "status":       r.Status,
        "players":      r.Players,
        "player_count": len(r.Players),
    }
}

func (r *Room) GetPlayers() []*Player {
    r.Mu.RLock()
    defer r.Mu.RUnlock()

    players := make([]*Player, 0, len(r.Players))
    for _, p := range r.Players {
        players = append(players, p)
    }
    return players
}

// 寫操作：互斥訪問
func (r *Room) AddPlayer(id, name string) error {
    r.Mu.Lock()          // 寫鎖：排他訪問
    defer r.Mu.Unlock()

    if r.Status != StatusWaiting && r.Status != StatusPreparing {
        return fmt.Errorf("房間狀態不允許加入")
    }

    if len(r.Players) >= r.MaxPlayers {
        return errors.New("房間已滿")
    }

    r.Players[id] = &Player{ID: id, Name: name}

    if len(r.Players) == r.MaxPlayers {
        r.Status = StatusPreparing
    }

    return nil
}

func (r *Room) RemovePlayer(id string) error {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    delete(r.Players, id)

    // 人數不足，回到等待狀態
    if len(r.Players) < r.MaxPlayers && r.Status == StatusPreparing {
        r.Status = StatusWaiting
    }

    return nil
}
```

**性能對比（2024/03/22 優化後）：**
```
場景：1000 個房間，每個房間 10 次/秒查詢，1 次/秒寫入

sync.Mutex：
- 查詢 QPS：2,500
- 平均延遲：120ms
- 原因：讀寫互斥，所有操作排隊

sync.RWMutex：
- 查詢 QPS：23,000 (提升 9.2 倍)
- 平均延遲：6ms (降低 95%)
- 原因：讀操作並發，寫操作互斥
```

**陷阱警告：鎖升級死鎖**

Kevin 曾經寫過這樣的代碼，導致程序永久卡死：

```go
// 錯誤範例：從讀鎖升級到寫鎖
func (r *Room) BadMethod() {
    r.Mu.RLock()
    // 讀取一些數據
    if needUpdate {
        r.Mu.Lock()   // 死鎖！無法從讀鎖升級到寫鎖
        // ...
        r.Mu.Unlock()
    }
    r.Mu.RUnlock()
}

// 正確做法：先釋放讀鎖
func (r *Room) GoodMethod() {
    r.Mu.RLock()
    // 讀取數據
    needUpdate := someCondition
    r.Mu.RUnlock()  // 先釋放讀鎖

    if needUpdate {
        r.Mu.Lock()   // 再獲取寫鎖
        // 重新檢查條件（可能已經變化）
        if someCondition {
            // 更新數據
        }
        r.Mu.Unlock()
    }
}
```

## 第三次災難：同步廣播阻塞（2024/03/25）

### 背景：玩家操作卡頓

用戶反饋：「我按了準備，但其他人 5 秒後才看到我準備了。」

### 問題：同步廣播阻塞操作

當時的廣播實現：

```go
func (r *Room) AddPlayer(id, name string) error {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    // 1. 修改狀態
    r.Players[id] = &Player{ID: id, Name: name}

    // 2. 同步廣播（在鎖內！）
    event := Event{Type: "player_joined", Data: ...}
    for _, conn := range r.connections {
        conn.WriteJSON(event)  // 阻塞寫入 WebSocket
    }

    return nil
}
```

**問題分析：**
```
時序：4 個玩家，其中玩家 C 網絡延遲 200ms

玩家 A 執行「準備」操作：
T0:   獲取寫鎖 (Lock)
T1:   修改狀態 (1ms)
T2:   開始廣播
T3:   發送給玩家 A (10ms)
T4:   發送給玩家 B (10ms)
T5:   發送給玩家 C (200ms) ← 卡在這裡！
T6:   發送給玩家 D (10ms)
T231: 釋放鎖 (Unlock)

結果：
- 操作延遲 231ms（原本應該 1ms）
- 鎖持有時間過長，其他操作被阻塞
- 玩家 B 想查看房間狀態 → 等待 200ms
```

**災難數據（2024/03/25）：**
- 操作延遲 P99：從 15ms 暴增到 850ms
- 用戶投訴：「點擊沒反應，以為遊戲卡死了」
- 監控數據：23% 的操作延遲超過 500ms

### 第一次嘗試：多 goroutine 廣播

Amy 嘗試用並發廣播：

```go
func (r *Room) AddPlayer(id, name string) error {
    r.Mu.Lock()
    // 修改狀態
    r.Players[id] = &Player{ID: id, Name: name}
    r.Mu.Unlock()

    // 併發廣播
    event := Event{Type: "player_joined", Data: ...}
    for _, conn := range r.connections {
        go conn.WriteJSON(event)  // 每個連接一個 goroutine
    }

    return nil
}
```

新問題出現了。

**Goroutine 洪水（2024/03/26）：**
```
計算：
- 1000 個房間
- 每個房間 4 個玩家
- 平均每秒 10 個事件（加入、準備、選歌等）

goroutine 創建速率：
1000 房間 × 4 玩家 × 10 事件/秒 = 40,000 goroutine/秒

問題：
- Goroutine 調度開銷：每秒創建 40,000 個，GC 壓力巨大
- 事件順序混亂：
  玩家 A 先「加入」後「準備」
  但因為 goroutine 調度，其他玩家可能先收到「準備」再收到「加入」

監控數據：
- GC 時間：從 5% 增加到 25%
- 內存分配：增加 3 倍
- 事件亂序率：12%（用戶看到「玩家 X 已準備」但找不到這個玩家）
```

### 靈感：Kafka 的設計

David 想起了 Kafka 的設計：「為什麼不用 channel？」

**核心思想：**
```
操作 → 修改狀態 → 發送事件到 channel (非阻塞) → 釋放鎖
                                        ↓
                            後台 goroutine 消費 channel
                                        ↓
                                  廣播到所有連接
```

### 改進方案：事件驅動架構 (Event-Driven)

```go
type Room struct {
    ID          string
    Status      RoomStatus
    Players     map[string]*Player
    Mu          sync.RWMutex

    events      chan Event       // 事件 channel
    connections map[string]*websocket.Conn
}

type Event struct {
    Type      string         `json:"type"`
    Data      map[string]any `json:"data"`
    Timestamp time.Time      `json:"timestamp"`
}

func NewRoom(id string, maxPlayers int) *Room {
    r := &Room{
        ID:          id,
        Status:      StatusWaiting,
        Players:     make(map[string]*Player),
        events:      make(chan Event, 100),  // 緩衝 100 個事件
        connections: make(map[string]*websocket.Conn),
    }

    // 啟動廣播 goroutine
    go r.broadcastLoop()

    return r
}

// 操作：發送事件（非阻塞）
func (r *Room) AddPlayer(id, name string) error {
    r.Mu.Lock()

    // 1. 狀態檢查
    if r.Status != StatusWaiting && r.Status != StatusPreparing {
        r.Mu.Unlock()
        return fmt.Errorf("房間狀態不允許加入")
    }

    if len(r.Players) >= r.MaxPlayers {
        r.Mu.Unlock()
        return errors.New("房間已滿")
    }

    // 2. 修改狀態
    player := &Player{ID: id, Name: name, Ready: false}
    r.Players[id] = player

    if len(r.Players) == r.MaxPlayers {
        r.Status = StatusPreparing
    }

    r.Mu.Unlock()  // 早釋放鎖！

    // 3. 發送事件（非阻塞）
    r.sendEvent(Event{
        Type: "player_joined",
        Data: map[string]any{
            "player": player,
            "count":  len(r.Players),
        },
        Timestamp: time.Now(),
    })

    return nil
}

// 非阻塞發送事件
func (r *Room) sendEvent(event Event) {
    select {
    case r.events <- event:  // 嘗試發送
        // 成功
    default:
        // channel 滿，丟棄事件
        // 生產環境應該：記錄日誌、監控丟失率、告警
        log.Printf("事件丟失 (room=%s, type=%s)", r.ID, event.Type)
    }
}

// 後台廣播（每個房間一個 goroutine）
func (r *Room) broadcastLoop() {
    for event := range r.events {
        r.Mu.RLock()
        conns := make([]*websocket.Conn, 0, len(r.connections))
        for _, conn := range r.connections {
            conns = append(conns, conn)
        }
        r.Mu.RUnlock()

        // 廣播到所有連接（併發）
        for _, conn := range conns {
            go func(c *websocket.Conn) {
                if err := c.WriteJSON(event); err != nil {
                    log.Printf("廣播失敗: %v", err)
                }
            }(conn)
        }
    }
}
```

**性能提升（2024/03/27 部署）：**
```
場景：4 人房間，1 人網絡延遲 200ms

同步廣播：
- 操作延遲：231ms（被慢連接阻塞）
- 鎖持有時間：231ms
- 併發能力：被阻塞

事件驅動：
- 操作延遲：2ms（修改狀態 1ms + 發送到 channel 1ms）
- 鎖持有時間：1ms
- 廣播延遲：在後台異步執行，不影響操作

性能對比：
- 操作吞吐量：500 QPS → 12,000 QPS (提升 24 倍)
- P99 延遲：850ms → 8ms (降低 99%)
- 慢連接影響：從阻塞所有操作 → 只影響單個連接
```

**背壓控制（Backpressure）：**

當事件產生速度 > 消費速度時，channel 緩衝會被填滿：

```go
// 配置事件 channel 大小
events: make(chan Event, 100)

// 場景：突發流量
情況 1：正常（每秒 50 個事件，channel 緩衝足夠）
  → 所有事件都能發送

情況 2：突發（每秒 200 個事件，channel 緩衝滿）
  → select default 分支執行，丟棄事件
  → 監控告警：事件丟失率超過 5%

生產環境優化：
1. 監控 channel 長度：
   if len(r.events) > 80 {  // 緩衝使用 80%
       // 告警：事件積壓
   }

2. 動態調整緩衝大小（根據房間活躍度）

3. 事件優先級：
   - 關鍵事件（遊戲開始）：阻塞發送
   - 一般事件（狀態更新）：非阻塞，可丟棄
```

## 第四次災難：內存洩漏（2024/04/01）

### 背景：服務器內存持續增長

運維監控告警：

```
2024-04-01 10:00 - 內存使用：2 GB
2024-04-01 14:00 - 內存使用：5 GB
2024-04-01 18:00 - 內存使用：9 GB
2024-04-01 20:00 - 內存使用：12 GB (觸發告警)
```

**問題定位：**
```bash
$ curl http://localhost:8080/debug/rooms
{
  "total_rooms": 12,453,
  "active_rooms": 127,
  "zombie_rooms": 12,326  // 殭屍房間！
}
```

### 根本原因：房間未清理

當時的代碼沒有清理機制：

```go
type RoomManager struct {
    rooms map[string]*Room
    mu    sync.RWMutex
}

func (m *RoomManager) CreateRoom(id string) *Room {
    m.mu.Lock()
    defer m.mu.Unlock()

    room := NewRoom(id, 4)
    m.rooms[id] = room  // 存入 map
    return room
}

// 問題：沒有刪除房間的邏輯！
```

**洩漏場景：**
```
每天 10,000 個玩家創建房間
平均每場遊戲 15 分鐘
10% 玩家異常離開（關閉瀏覽器、網絡斷開）

結果：
- 每天新增 1,000 個遺棄房間（10,000 × 10%）
- 每個房間約 10 KB（結構體 + channel + 連接）
- 7 天後：7,000 個殭屍房間 = 70 MB
- 30 天後：30,000 個殭屍房間 = 300 MB
- 持續運行 3 個月 → OOM 崩潰
```

### 第一次嘗試：立即刪除

Kevin 加了刪除邏輯：

```go
func (r *Room) RemovePlayer(id string) error {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    delete(r.Players, id)

    // 立即刪除空房間
    if len(r.Players) == 0 {
        r.Status = StatusClosed
        close(r.events)  // 關閉 channel
        // 通知 manager 刪除
    }

    return nil
}
```

新問題出現了。

**誤刪災難（2024/04/02）：**
```
場景：4 人正在遊戲

18:30:00 - 玩家 A 網絡抖動，WebSocket 斷開
18:30:01 - 系統執行 RemovePlayer(A)
18:30:01 - 房間剩 3 人，繼續遊戲
18:30:05 - 玩家 A 網絡恢復，嘗試重連
18:30:05 - 錯誤：房間不存在（已被刪除）

用戶投訴：
「網絡卡了 5 秒，遊戲直接把我踢了！」
「重連後房間消失了，進度全沒了！」

數據：
- 誤刪率：15%（網絡抖動導致）
- 用戶留存率：下降 8%
```

### 第二次嘗試：永不刪除

「那就不刪了！」Kevin 說。

結果又回到了內存洩漏的老問題。

### 靈感：Redis 的過期策略

David 想起 Redis 的設計：

「Redis 不是立即刪除，也不是永不刪除，而是**延遲刪除 + 定期掃描**。」

### 改進方案：超時自動清理

```go
type Room struct {
    ID         string
    Status     RoomStatus
    Players    map[string]*Player
    CreatedAt  time.Time
    lastActive time.Time  // 最後活動時間
    // ...
}

// 更新活動時間
func (r *Room) AddPlayer(id, name string) error {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    // ... 操作邏輯

    r.lastActive = time.Now()  // 記錄活動
    return nil
}

func (r *Room) RemovePlayer(id string) error {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    delete(r.Players, id)
    r.lastActive = time.Now()

    // 不立即刪除！允許重連
    return nil
}

// 過期檢查
func (r *Room) IsExpired() bool {
    r.Mu.RLock()
    defer r.Mu.RUnlock()

    now := time.Now()

    // 規則 1：任何房間最多存在 30 分鐘（防止永久佔用）
    if now.Sub(r.CreatedAt) > 30*time.Minute {
        return true
    }

    // 規則 2：已關閉的房間 1 分鐘後過期
    if r.Status == StatusClosed && now.Sub(r.lastActive) > 1*time.Minute {
        return true
    }

    // 規則 3：無人房間 5 分鐘後過期（允許重連）
    if len(r.Players) == 0 && now.Sub(r.lastActive) > 5*time.Minute {
        return true
    }

    return false
}
```

**管理器層面的定期清理：**

```go
type RoomManager struct {
    rooms   map[string]*Room
    mu      sync.RWMutex
    metrics *Metrics
}

// 定期清理 goroutine
func (m *RoomManager) StartCleanup(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Minute)  // 每分鐘掃描一次
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            m.cleanup()
        case <-ctx.Done():
            return
        }
    }
}

func (m *RoomManager) cleanup() {
    m.mu.RLock()
    // 收集過期房間（不阻塞讀取）
    expiredIDs := make([]string, 0)
    for id, room := range m.rooms {
        if room.IsExpired() {
            expiredIDs = append(expiredIDs, id)
        }
    }
    m.mu.RUnlock()

    // 刪除過期房間
    if len(expiredIDs) > 0 {
        m.mu.Lock()
        for _, id := range expiredIDs {
            room := m.rooms[id]
            room.Close("timeout")  // 優雅關閉
            delete(m.rooms, id)
        }
        m.mu.Unlock()

        log.Printf("清理了 %d 個過期房間", len(expiredIDs))
        m.metrics.RoomsCleaned.Add(float64(len(expiredIDs)))
    }
}

func (r *Room) Close(reason string) {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    if r.Status == StatusClosed {
        return
    }

    r.Status = StatusClosed
    r.lastActive = time.Now()

    // 關閉事件 channel
    close(r.events)

    // 通知所有連接
    for _, conn := range r.connections {
        conn.WriteJSON(Event{
            Type: "room_closed",
            Data: map[string]any{"reason": reason},
        })
        conn.Close()
    }
}
```

**清理策略對比（2024/04/05 部署）：**
```
場景：10,000 個房間/天，10% 異常退出

立即刪除：
- 內存洩漏：無
- 誤刪率：15%（網絡抖動）
- 用戶體驗：差（無法重連）

永不刪除：
- 內存洩漏：300 MB/月
- 誤刪率：0%
- 用戶體驗：好（可重連），但服務器會崩潰

超時清理（5分鐘）：
- 內存洩漏：~5 MB（5分鐘內的遺棄房間）
- 誤刪率：0.3%（斷線 > 5 分鐘的極端情況）
- 用戶體驗：優（允許短暫重連）

監控數據（部署 7 天後）：
- 內存穩定在 2.5 GB（不再增長）
- 每天清理 950 個遺棄房間
- 重連成功率：提升到 94%
```

**優化：優先隊列 vs 全量掃描**

當房間數量增長到 10,000 個時，每分鐘掃描全部房間的開銷變大：

```go
// 優化前：全量掃描
func (m *RoomManager) cleanup() {
    for _, room := range m.rooms {  // 10,000 次檢查
        if room.IsExpired() {
            // ...
        }
    }
}
// 開銷：10,000 次函數調用 + 鎖競爭

// 優化後：優先隊列（按過期時間排序）
type RoomManager struct {
    rooms      map[string]*Room
    expireHeap *ExpireHeap  // 最小堆，按過期時間排序
}

func (m *RoomManager) cleanup() {
    now := time.Now()
    for m.expireHeap.Peek().ExpireTime.Before(now) {
        room := m.expireHeap.Pop()
        if room.IsExpired() {  // 再次確認
            delete(m.rooms, room.ID)
            room.Close("timeout")
        }
    }
}
// 開銷：只檢查已過期的房間（平均每次 ~100 個）
```

## 第五次挑戰：實時通信的實現（2024/04/10）

### 業務需求：WebSocket 實時推送

產品經理：「玩家操作要立即同步給所有人，延遲要低於 50ms。」

### 決策：為什麼選擇 WebSocket？

**方案對比：**

```
方案 A：HTTP 短輪詢 (Polling)
機制：客戶端每 N 秒請求一次
問題：
- 延遲高：最壞 N 秒
- 浪費資源：90% 請求無狀態變化
- QPS 高：4000 連接 × 1 req/s = 4000 QPS

方案 B：HTTP 長輪詢 (Long Polling)
機制：客戶端請求，服務器有事件才響應
問題：
- 連接管理複雜：需要維護大量 pending 請求
- 仍需重複建立連接

方案 C：Server-Sent Events (SSE)
機制：服務器單向推送事件流
問題：
- 單向通信：客戶端操作仍需 HTTP POST
- 需要維護兩套協議

選擇：WebSocket
機制：全雙工持久連接
優勢：
- 實時雙向：服務器推送 + 客戶端操作
- 低延遲：無輪詢，事件立即推送
- 高效：單連接複用
```

**WebSocket 實現：**

```go
import "github.com/gorilla/websocket"

var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        return true  // 生產環境需要驗證 Origin
    },
}

// 升級 HTTP 連接到 WebSocket
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("WebSocket 升級失敗: %v", err)
        return
    }
    defer conn.Close()

    // 解析參數
    roomID := r.URL.Query().Get("room_id")
    playerID := r.URL.Query().Get("player_id")

    room := s.manager.GetRoom(roomID)
    if room == nil {
        conn.WriteJSON(map[string]string{"error": "房間不存在"})
        return
    }

    // 註冊連接
    room.AddConnection(playerID, conn)
    defer room.RemoveConnection(playerID)

    // 監聽客戶端消息
    for {
        var msg Message
        if err := conn.ReadJSON(&msg); err != nil {
            if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
                log.Printf("WebSocket 錯誤: %v", err)
            }
            break
        }

        // 處理消息
        s.handleMessage(room, playerID, &msg)
    }
}

type Message struct {
    Type string         `json:"type"`
    Data map[string]any `json:"data"`
}

func (s *Server) handleMessage(room *Room, playerID string, msg *Message) {
    switch msg.Type {
    case "ready":
        room.SetPlayerReady(playerID, true)
    case "select_song":
        songID := msg.Data["song_id"].(string)
        room.SelectSong(playerID, songID)
    case "start_game":
        room.StartGame()
    default:
        log.Printf("未知消息類型: %s", msg.Type)
    }
}

// 房間連接管理
func (r *Room) AddConnection(playerID string, conn *websocket.Conn) {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    r.connections[playerID] = conn
}

func (r *Room) RemoveConnection(playerID string) {
    r.Mu.Lock()
    defer r.Mu.Unlock()

    delete(r.connections, playerID)
}
```

**心跳檢測（避免殭屍連接）：**

```go
const (
    pongWait   = 60 * time.Second
    pingPeriod = 50 * time.Second  // < pongWait
)

func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, _ := upgrader.Upgrade(w, r, nil)
    defer conn.Close()

    // 設置讀超時
    conn.SetReadDeadline(time.Now().Add(pongWait))
    conn.SetPongHandler(func(string) error {
        conn.SetReadDeadline(time.Now().Add(pongWait))
        return nil
    })

    // 啟動 ping goroutine
    go func() {
        ticker := time.NewTicker(pingPeriod)
        defer ticker.Stop()

        for {
            select {
            case <-ticker.C:
                if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                    return
                }
            }
        }
    }()

    // 讀取消息...
}
```

**客戶端實現（JavaScript）：**

```javascript
class RoomClient {
    constructor(roomId, playerId) {
        this.roomId = roomId;
        this.playerId = playerId;
        this.ws = null;
        this.reconnectAttempts = 0;
    }

    connect() {
        this.ws = new WebSocket(
            `ws://localhost:8080/ws?room_id=${this.roomId}&player_id=${this.playerId}`
        );

        this.ws.onopen = () => {
            console.log('WebSocket 連接成功');
            this.reconnectAttempts = 0;
        };

        this.ws.onmessage = (event) => {
            const data = JSON.parse(event.data);
            this.handleEvent(data);
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket 錯誤:', error);
        };

        this.ws.onclose = () => {
            console.log('WebSocket 連接關閉');
            this.reconnect();
        };
    }

    reconnect() {
        if (this.reconnectAttempts < 5) {
            this.reconnectAttempts++;
            const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), 10000);
            console.log(`${delay}ms 後重連...`);
            setTimeout(() => this.connect(), delay);
        }
    }

    handleEvent(event) {
        switch (event.type) {
            case 'player_joined':
                this.onPlayerJoined(event.data);
                break;
            case 'player_ready':
                this.onPlayerReady(event.data);
                break;
            case 'game_started':
                this.onGameStarted(event.data);
                break;
            case 'room_closed':
                this.onRoomClosed(event.data);
                break;
        }
    }

    // 發送操作
    ready() {
        this.send({ type: 'ready', data: {} });
    }

    selectSong(songId) {
        this.send({ type: 'select_song', data: { song_id: songId } });
    }

    send(message) {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(message));
        }
    }
}

// 使用
const client = new RoomClient('room-123', 'player-456');
client.connect();
client.ready();
```

## 新的挑戰：擴展性

### 當前架構容量（單機內存存儲）

```
計算：
- 每個房間：約 10 KB
  - 結構體：2 KB
  - 4 個玩家：4 × 0.5 KB = 2 KB
  - Event channel (100 cap)：100 × 0.05 KB = 5 KB
  - WebSocket 連接：4 × 4 KB = 16 KB（內核緩衝）

- 1000 個房間：
  - 房間數據：1000 × 10 KB = 10 MB
  - WebSocket 連接：4000 × 16 KB = 64 MB
  - 總計：約 75 MB

單機瓶頸：
- WebSocket 連接數：約 10,000（受文件描述符限制）
- 內存：16 GB → 最多支撐約 2,000 個房間
- CPU：廣播事件調度（goroutine 調度器瓶頸）
```

### 10x 擴展：Redis + Pub/Sub

```
架構變化：

當前（單機）：
Client → WebSocket → Room (memory)

優化後（多實例）：
Client → Load Balancer (Sticky Session)
         ↓
         ├─ Instance 1 ─┐
         ├─ Instance 2 ─┤→ Redis (房間狀態)
         └─ Instance 3 ─┘    ↓
                            Pub/Sub (事件廣播)

實現：
1. 房間狀態存 Redis：
   - Key: room:{room_id}
   - Value: JSON (status, players, ...)
   - TTL: 30 分鐘（自動過期）

2. 事件廣播用 Pub/Sub：
   - Channel: room:{room_id}:events
   - 訂閱者：所有持有該房間連接的實例

3. Sticky Session：
   - 同一房間的所有連接路由到同一實例
   - 避免跨實例鎖

代碼變化：
type Room struct {
    redis  *redis.Client
    pubsub *redis.PubSub
    // ...
}

func (r *Room) AddPlayer(id, name string) error {
    // 1. 操作 Redis（替代內存）
    r.redis.HSet(ctx, fmt.Sprintf("room:%s", r.ID), "players", ...)

    // 2. 發布事件
    r.redis.Publish(ctx, fmt.Sprintf("room:%s:events", r.ID), event)

    return nil
}

容量：
- 10 個實例 × 1,000 房間 = 10,000 房間
- 40,000 WebSocket 連接
- 成本：~$1,000/月
```

### 100x 擴展：分層架構

```
瓶頸：
- Redis Pub/Sub 不保證可靠性（訂閱者離線會丟消息）
- WebSocket 連接與業務邏輯耦合

架構：
Client (400,000 玩家)
  ↓
Load Balancer (L7, WebSocket aware)
  ↓
WebSocket Gateway (40 instances)
  ├─ 處理 WebSocket 連接（10,000/instance）
  ├─ 訂閱消息隊列
  └─ 推送事件到客戶端
  ↓
Message Queue (Kafka)
  ↓
Room Logic Service (10 instances)
  ├─ 房間狀態管理
  ├─ 業務邏輯
  └─ 發布事件到 Kafka
  ↓
Redis Cluster (16 shards)
  └─ 房間狀態存儲

分片策略：
- 按 room_id hash 分片
- 同一房間的操作路由到同一實例
- 避免分散式鎖

容量：
- 100,000 個房間
- 400,000 WebSocket 連接
- 成本：~$5,000/月
```

## 真實案例：Discord 的房間系統

Discord 處理百萬級語音/文字頻道（類似房間）的經驗：

### 架構演進

```
2015 年（單體應用）：
- MongoDB 存儲頻道狀態
- 單實例 WebSocket
- 容量：約 1,000 頻道

2017 年（Redis + Elixir）：
- 遷移到 Elixir (OTP)
- Redis 存儲狀態
- GenServer 管理每個頻道（類似 Room）
- 容量：10,000+ 頻道

2020 年（分散式架構）：
- ScyllaDB 替代 Redis（更高寫入吞吐）
- Rust 重寫部分服務（降低延遲）
- 自研消息路由（替代 Pub/Sub）
- 容量：1,000,000+ 頻道

關鍵優化：
1. 狀態分片：按 guild_id (類似 room_id) hash
2. 連接分離：WebSocket Gateway 與業務邏輯分離
3. 事件壓縮：批量發送事件（減少消息數）
4. 本地緩存：熱數據緩存在內存（減少 Redis 訪問）
```

參考資料：
- [How Discord Stores Billions of Messages](https://discord.com/blog/how-discord-stores-billions-of-messages)
- [How Discord Scaled Elixir to 5,000,000 Concurrent Users](https://discord.com/blog/using-rust-to-scale-elixir-for-11-million-concurrent-users)

## 總結與對比

### 核心設計原則

```
1. 有限狀態機（FSM）
   問題：多個布林標誌導致狀態矛盾
   方案：單一狀態 + 明確轉換規則
   效果：狀態異常從 23% → 0%

2. 讀寫鎖優化
   問題：sync.Mutex 讀寫互斥
   方案：sync.RWMutex 讀並發、寫互斥
   效果：讀多寫少場景 QPS 提升 9 倍

3. 事件驅動架構
   問題：同步廣播阻塞操作
   方案：操作發事件到 channel，後台異步廣播
   效果：操作延遲從 850ms → 8ms

4. 超時自動清理
   問題：立即刪除誤刪、永不刪除洩漏
   方案：延遲刪除（5分鐘）+ 定期掃描
   效果：內存穩定、重連成功率 94%
```

### 與其他系統的對比

| 維度 | Counter Service | URL Shortener | Room Management |
|------|----------------|---------------|-----------------|
| **核心挑戰** | 高頻寫入 | 讀多寫少 | 實時同步 + 狀態管理 |
| **一致性** | 最終一致性 | 強一致性（短網址唯一） | 強一致性（狀態機） |
| **通信模式** | 單向（客戶端寫） | 請求-響應 | 雙向（WebSocket） |
| **並發控制** | Redis 原子操作 | 資料庫唯一索引 | RWMutex + 事件驅動 |
| **存儲** | Redis + PostgreSQL | PostgreSQL | 內存 → Redis |
| **擴展瓶頸** | 資料庫寫入 | 資料庫查詢 | WebSocket 連接數 |

### 適用場景

**適合使用 Room Management 模式的場景：**
- 多人遊戲房間（如本案例）
- 在線協作編輯器（如 Google Docs）
- 聊天室系統
- 實時競價系統（拍賣）
- 視頻會議系統

**不適合的場景：**
- 單人應用（無需狀態同步）
- 離線應用（無實時性要求）
- 無狀態服務（如 CDN）

### 關鍵指標

```
最終性能（單機）：
- 支持房間數：2,000 個
- 並發連接數：8,000 WebSocket
- 操作吞吐量：12,000 QPS
- 事件廣播延遲：P99 < 8ms
- 內存占用：穩定在 2.5 GB
- 重連成功率：94%

代碼質量：
- 狀態管理：從 6 個布林值 → 單一狀態機
- 並發安全：RWMutex + Channel（無鎖升級死鎖）
- 資源管理：自動清理（無內存洩漏）
- 可測試性：狀態轉換可窮舉
```

### 延伸閱讀

**系統設計模式：**
- Finite State Machine（有限狀態機）
- Event-Driven Architecture（事件驅動）
- Pub/Sub Pattern（發布訂閱）
- CQRS（命令查詢職責分離）

**技術實現：**
- WebSocket 協議（RFC 6455）
- Gorilla WebSocket 庫
- Redis Pub/Sub
- Kafka 消息隊列

**真實案例：**
- Discord：百萬級頻道管理
- Slack：企業級即時通訊
- Among Us：多人遊戲房間
- Figma：實時協作編輯

---

從「凌晨兩點的緊急上線」到「穩定支撐 8,000 並發連接」，Room Management 系統經歷了 5 次重大災難和改進：

1. **狀態混亂** → 有限狀態機
2. **並發混亂** → 讀寫鎖
3. **廣播阻塞** → 事件驅動
4. **內存洩漏** → 超時清理
5. **實時通信** → WebSocket

每一次災難都是一次學習的機會。當你的系統需要管理複雜狀態、處理高並發、保證實時性時，這些經驗會成為你的寶貴財富。

**記住：** 好的架構不是一次設計出來的，而是在真實世界的壓力下不斷演進的。
