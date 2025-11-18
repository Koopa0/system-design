# Chapter 30: Stock Exchange（股票交易系統）

> **難度**：★★★★★
> **預估時間**：6-8 週
> **核心概念**：訂單撮合引擎、低延遲優化、訂單簿、高頻交易

---

## Act 1: 訂單撮合的藝術

週一早晨，Emma 走進會議室，白板上寫著一個令人興奮的新專案：**Stock Exchange（股票交易系統）**。

**Emma**：「各位早安！我們今天要設計一個股票交易系統。這可能是我們做過最複雜的系統了。」

**David**：「股票交易系統的核心是什麼？」

**Michael**：「是 **訂單撮合引擎（Order Matching Engine）**。它負責將買單和賣單配對成交。」

**Sarah**：「聽起來很簡單啊，不就是找到價格匹配的買賣單嗎？」

**David**（微笑）：「理論上是這樣。但實際上，這是全世界最複雜、要求最高的系統之一。讓我告訴你為什麼。」

### 訂單撮合的挑戰

**David** 在白板上寫下幾個數字：

- **延遲要求**：< 1ms（微秒級）
- **吞吐量**：100,000+ 訂單/秒
- **可用性**：99.999%（每年停機時間 < 5.26 分鐘）
- **正確性**：100%（絕對不能出錯）

**Emma**：「1 毫秒？！我們之前做的支付系統，P99 延遲是 300ms。」

**Michael**：「沒錯。股票交易系統的延遲要求是 **微秒級（µs）**，不是毫秒級（ms）。我們需要重新思考所有設計。」

### 訂單類型

**Sarah**：「首先，有哪些類型的訂單？」

**David**：「主要有三種：」

```go
// OrderType 訂單類型
type OrderType string

const (
    // MarketOrder 市價單：立即以當前市場最優價格成交
    // 例如：「買 100 股台積電，不管價格多少」
    OrderTypeMarket OrderType = "market"

    // LimitOrder 限價單：只在指定價格或更好的價格成交
    // 例如：「買 100 股台積電，價格不超過 NT$600」
    OrderTypLimit OrderType = "limit"

    // StopOrder 停損單：當價格達到觸發價時，變成市價單
    // 例如：「當台積電跌到 NT$580 時，賣出 100 股」
    OrderTypeStop OrderType = "stop"

    // StopLimitOrder 停損限價單：當價格達到觸發價時，變成限價單
    // 例如：「當台積電跌到 NT$580 時，以不低於 NT$575 的價格賣出」
    OrderTypeStopLimit OrderType = "stop_limit"
)

// OrderSide 買賣方向
type OrderSide string

const (
    OrderSideBuy  OrderSide = "buy"  // 買單
    OrderSideSell OrderSide = "sell" // 賣單
)

// Order 訂單
type Order struct {
    ID            int64     // 訂單 ID
    UserID        string    // 用戶 ID
    Symbol        string    // 股票代碼（例如：2330.TW = 台積電）
    Side          OrderSide // 買賣方向
    Type          OrderType // 訂單類型
    Quantity      int64     // 數量（股）
    Price         int64     // 價格（分，例如 60000 = NT$600.00）
    StopPrice     int64     // 停損價（僅用於停損單）
    FilledQty     int64     // 已成交數量
    Status        string    // 狀態：new, partial_filled, filled, cancelled
    CreatedAt     time.Time // 建立時間
}
```

**Emma**：「市價單和限價單有什麼區別？」

**Michael**：「讓我舉個例子。假設台積電目前的訂單簿是這樣：」

```
賣單（Ask）                      買單（Bid）
價格      數量                    價格      數量
----------------------------------------
NT$601    500 股                NT$599    300 股
NT$600    200 股                NT$598    400 股
NT$599.5  100 股                NT$597    600 股
```

**David**：「如果你下一個 **市價買單 100 股**：」
- 會立即以 NT$599.5 成交 100 股（吃掉賣單簿中最便宜的）

**David**：「如果你下一個 **限價買單 100 股，價格 NT$598**：」
- 不會立即成交，因為最便宜的賣單是 NT$599.5
- 訂單會進入買單簿，等待有人願意以 NT$598 賣出

**Sarah**：「所以市價單保證成交，但價格不確定；限價單價格確定，但不保證成交。」

**Michael**：「完全正確！」

---

## Act 2: 訂單簿的數據結構

**Emma**：「我們該如何儲存這些訂單？用資料庫嗎？」

**David**：「不行！資料庫太慢了。即使是 Redis，延遲也有幾毫秒。我們需要 **純內存（In-Memory）** 的數據結構。」

**Sarah**：「那要用什麼數據結構？」

### 訂單簿設計

**Michael**：「訂單簿需要支援這些操作：」

| 操作 | 時間複雜度要求 |
|-----|--------------|
| 新增訂單 | O(log n) |
| 取消訂單 | O(log n) |
| 找到最優買價 | O(1) |
| 找到最優賣價 | O(1) |
| 撮合訂單 | O(1) 均攤 |

**David**：「我們使用 **雙向有序映射（Sorted Map）** + **雙向鏈表（Doubly Linked List）**：」

```go
// OrderBook 訂單簿
type OrderBook struct {
    Symbol string // 股票代碼

    // 買單簿（價格從高到低排序）
    Bids *PriceLevelTree

    // 賣單簿（價格從低到高排序）
    Asks *PriceLevelTree

    // 訂單索引（用於快速查找和取消）
    OrderIndex map[int64]*Order

    // 鎖（保護並發訪問）
    mu sync.RWMutex
}

// PriceLevelTree 價格層級樹
type PriceLevelTree struct {
    // 使用紅黑樹儲存價格層級（價格 -> PriceLevel）
    Tree *rbtree.Tree

    // 是否為買單簿（影響排序順序）
    IsBid bool
}

// PriceLevel 價格層級
// 同一價格的所有訂單
type PriceLevel struct {
    Price  int64   // 價格
    Volume int64   // 總數量
    Orders *list.List // 訂單鏈表（按時間順序）
}
```

**Sarah**：「為什麼要用紅黑樹？」

**Michael**：「紅黑樹提供 O(log n) 的插入、刪除和查找，同時保持有序。這對於找到最優價格很重要。」

**Emma**：「為什麼每個價格層級用鏈表？」

**David**：「因為撮合遵循 **價格優先、時間優先（Price-Time Priority）** 原則：」
1. **價格優先**：買單價格高的優先，賣單價格低的優先
2. **時間優先**：同價格的訂單，先到先成交

**Michael**：「鏈表保證了時間順序，而且在頭部插入和刪除都是 O(1)。」

### 訂單簿操作

**Sarah**：「來看看具體的實作：」

```go
// NewOrderBook 建立訂單簿
func NewOrderBook(symbol string) *OrderBook {
    return &OrderBook{
        Symbol: symbol,
        Bids: &PriceLevelTree{
            Tree:  rbtree.NewWith(descendingComparator), // 買單：價格從高到低
            IsBid: true,
        },
        Asks: &PriceLevelTree{
            Tree:  rbtree.NewWith(ascendingComparator), // 賣單：價格從低到高
            IsBid: false,
        },
        OrderIndex: make(map[int64]*Order),
    }
}

// AddOrder 新增訂單
func (ob *OrderBook) AddOrder(order *Order) {
    ob.mu.Lock()
    defer ob.mu.Unlock()

    // 1. 加入訂單索引
    ob.OrderIndex[order.ID] = order

    // 2. 選擇買單簿或賣單簿
    var tree *PriceLevelTree
    if order.Side == OrderSideBuy {
        tree = ob.Bids
    } else {
        tree = ob.Asks
    }

    // 3. 獲取或建立價格層級
    priceLevel := tree.GetOrCreatePriceLevel(order.Price)

    // 4. 將訂單加到價格層級的尾部（時間優先）
    priceLevel.Orders.PushBack(order)
    priceLevel.Volume += order.Quantity
}

// RemoveOrder 取消訂單
func (ob *OrderBook) RemoveOrder(orderID int64) error {
    ob.mu.Lock()
    defer ob.mu.Unlock()

    // 1. 從索引中查找
    order, exists := ob.OrderIndex[orderID]
    if !exists {
        return errors.New("訂單不存在")
    }

    // 2. 選擇買單簿或賣單簿
    var tree *PriceLevelTree
    if order.Side == OrderSideBuy {
        tree = ob.Bids
    } else {
        tree = ob.Asks
    }

    // 3. 從價格層級中移除
    priceLevel := tree.GetPriceLevel(order.Price)
    if priceLevel != nil {
        priceLevel.RemoveOrder(order)
        priceLevel.Volume -= (order.Quantity - order.FilledQty)

        // 如果價格層級已空，移除它
        if priceLevel.Volume == 0 {
            tree.RemovePriceLevel(order.Price)
        }
    }

    // 4. 從索引中移除
    delete(ob.OrderIndex, orderID)

    return nil
}

// GetBestBid 獲取最優買價
func (ob *OrderBook) GetBestBid() (int64, int64, bool) {
    ob.mu.RLock()
    defer ob.mu.RUnlock()

    // 紅黑樹的最左節點（最大值）
    node := ob.Bids.Tree.Left()
    if node == nil {
        return 0, 0, false
    }

    priceLevel := node.Value.(*PriceLevel)
    return priceLevel.Price, priceLevel.Volume, true
}

// GetBestAsk 獲取最優賣價
func (ob *OrderBook) GetBestAsk() (int64, int64, bool) {
    ob.mu.RLock()
    defer ob.mu.RUnlock()

    // 紅黑樹的最左節點（最小值）
    node := ob.Asks.Tree.Left()
    if node == nil {
        return 0, 0, false
    }

    priceLevel := node.Value.(*PriceLevel)
    return priceLevel.Price, priceLevel.Volume, true
}
```

**Emma**：「這個設計很優雅！讀取最優價格是 O(1)，新增和刪除是 O(log n)。」

---

## Act 3: 撮合引擎

**David**：「現在來實作最核心的部分：**撮合引擎（Matching Engine）**。」

**Michael**：「撮合引擎的職責是：」
1. 接收新訂單
2. 檢查能否與現有訂單成交
3. 如果能成交，產生交易記錄
4. 如果不能完全成交，將剩餘部分加入訂單簿

### 撮合算法

**Sarah**：「讓我們看看撮合算法：」

```go
// MatchingEngine 撮合引擎
type MatchingEngine struct {
    // 訂單簿（每個股票一個）
    OrderBooks map[string]*OrderBook

    // 撮合結果通道
    TradeChannel chan *Trade

    // 訂單序列號生成器
    OrderIDGen *atomic.Int64

    mu sync.RWMutex
}

// Trade 交易記錄
type Trade struct {
    ID          int64     // 交易 ID
    Symbol      string    // 股票代碼
    BuyOrderID  int64     // 買單 ID
    SellOrderID int64     // 賣單 ID
    Price       int64     // 成交價格
    Quantity    int64     // 成交數量
    Timestamp   time.Time // 成交時間
}

// ProcessOrder 處理訂單（核心方法）
func (me *MatchingEngine) ProcessOrder(order *Order) []*Trade {
    me.mu.Lock()
    defer me.mu.Unlock()

    // 1. 獲取或建立訂單簿
    orderBook := me.getOrCreateOrderBook(order.Symbol)

    var trades []*Trade

    // 2. 如果是市價單，直接撮合
    if order.Type == OrderTypeMarket {
        trades = me.matchMarketOrder(orderBook, order)
    } else if order.Type == OrderTypeLimit {
        // 3. 限價單：先嘗試撮合，剩餘部分加入訂單簿
        trades = me.matchLimitOrder(orderBook, order)
    }

    return trades
}

// matchLimitOrder 撮合限價單
func (me *MatchingEngine) matchLimitOrder(ob *OrderBook, order *Order) []*Trade {
    var trades []*Trade

    if order.Side == OrderSideBuy {
        // 買單：與賣單簿撮合
        trades = me.matchBuyOrder(ob, order)
    } else {
        // 賣單：與買單簿撮合
        trades = me.matchSellOrder(ob, order)
    }

    // 如果訂單還有剩餘，加入訂單簿
    if order.FilledQty < order.Quantity {
        ob.AddOrder(order)
    } else {
        order.Status = "filled"
    }

    return trades
}

// matchBuyOrder 撮合買單
func (me *MatchingEngine) matchBuyOrder(ob *OrderBook, buyOrder *Order) []*Trade {
    var trades []*Trade

    // 持續從賣單簿中取出最優價格
    for buyOrder.FilledQty < buyOrder.Quantity {
        // 1. 獲取最優賣價
        bestAskPrice, _, exists := ob.GetBestAsk()
        if !exists {
            // 沒有賣單了
            break
        }

        // 2. 檢查價格是否匹配
        // 買單價格 >= 賣單價格 才能成交
        if buyOrder.Price < bestAskPrice {
            break
        }

        // 3. 獲取該價格層級的第一筆訂單（時間最早）
        priceLevel := ob.Asks.GetPriceLevel(bestAskPrice)
        if priceLevel == nil || priceLevel.Orders.Len() == 0 {
            break
        }

        sellOrder := priceLevel.Orders.Front().Value.(*Order)

        // 4. 計算成交數量
        remainingBuy := buyOrder.Quantity - buyOrder.FilledQty
        remainingSell := sellOrder.Quantity - sellOrder.FilledQty
        tradeQty := min(remainingBuy, remainingSell)

        // 5. 產生交易記錄
        trade := &Trade{
            ID:          me.generateTradeID(),
            Symbol:      buyOrder.Symbol,
            BuyOrderID:  buyOrder.ID,
            SellOrderID: sellOrder.ID,
            Price:       sellOrder.Price, // 成交價以賣單價格為準（價格優先）
            Quantity:    tradeQty,
            Timestamp:   time.Now(),
        }

        trades = append(trades, trade)

        // 6. 更新訂單狀態
        buyOrder.FilledQty += tradeQty
        sellOrder.FilledQty += tradeQty

        // 7. 發送交易到通道（非同步處理）
        select {
        case me.TradeChannel <- trade:
        default:
            log.Warn("交易通道已滿")
        }

        // 8. 如果賣單完全成交，從訂單簿移除
        if sellOrder.FilledQty == sellOrder.Quantity {
            sellOrder.Status = "filled"
            priceLevel.Orders.Remove(priceLevel.Orders.Front())
            priceLevel.Volume -= sellOrder.Quantity

            // 如果價格層級已空，移除
            if priceLevel.Volume == 0 {
                ob.Asks.RemovePriceLevel(bestAskPrice)
            }
        } else {
            sellOrder.Status = "partial_filled"
        }
    }

    // 更新買單狀態
    if buyOrder.FilledQty > 0 {
        if buyOrder.FilledQty == buyOrder.Quantity {
            buyOrder.Status = "filled"
        } else {
            buyOrder.Status = "partial_filled"
        }
    }

    return trades
}

// matchSellOrder 撮合賣單（邏輯類似，方向相反）
func (me *MatchingEngine) matchSellOrder(ob *OrderBook, sellOrder *Order) []*Trade {
    var trades []*Trade

    for sellOrder.FilledQty < sellOrder.Quantity {
        // 1. 獲取最優買價
        bestBidPrice, _, exists := ob.GetBestBid()
        if !exists {
            break
        }

        // 2. 檢查價格是否匹配
        // 賣單價格 <= 買單價格 才能成交
        if sellOrder.Price > bestBidPrice {
            break
        }

        // 3. 獲取該價格層級的第一筆訂單
        priceLevel := ob.Bids.GetPriceLevel(bestBidPrice)
        if priceLevel == nil || priceLevel.Orders.Len() == 0 {
            break
        }

        buyOrder := priceLevel.Orders.Front().Value.(*Order)

        // 4. 計算成交數量
        remainingSell := sellOrder.Quantity - sellOrder.FilledQty
        remainingBuy := buyOrder.Quantity - buyOrder.FilledQty
        tradeQty := min(remainingSell, remainingBuy)

        // 5. 產生交易記錄
        trade := &Trade{
            ID:          me.generateTradeID(),
            Symbol:      sellOrder.Symbol,
            BuyOrderID:  buyOrder.ID,
            SellOrderID: sellOrder.ID,
            Price:       buyOrder.Price, // 成交價以買單價格為準
            Quantity:    tradeQty,
            Timestamp:   time.Now(),
        }

        trades = append(trades, trade)

        // 6-8. 更新狀態（同上）
        // ...
    }

    return trades
}
```

**Emma**：「這個算法保證了價格優先、時間優先的原則！」

**David**：「沒錯。而且因為使用紅黑樹和鏈表，撮合的平均時間複雜度接近 O(1)。」

---

## Act 4: 低延遲優化

**Michael**：「我們現在有了基本的撮合引擎。但要達到微秒級延遲，還需要大量優化。」

**Sarah**：「有哪些優化技巧？」

### 1. 無鎖設計（Lock-Free）

**David**：「鎖是效能殺手。我們使用 **單執行緒 + 無鎖佇列** 的架構：」

```go
// MatchingEngineV2 低延遲撮合引擎
type MatchingEngineV2 struct {
    // 訂單輸入佇列（無鎖佇列）
    OrderQueue *lockfree.Queue

    // 訂單簿（單執行緒訪問，不需要鎖）
    OrderBooks map[string]*OrderBook

    // 撮合執行緒
    workerRunning atomic.Bool
}

// Start 啟動撮合引擎
func (me *MatchingEngineV2) Start() {
    me.workerRunning.Store(true)

    // 單一執行緒處理所有訂單（避免鎖競爭）
    go me.matchingWorker()
}

// matchingWorker 撮合工作執行緒
func (me *MatchingEngineV2) matchingWorker() {
    for me.workerRunning.Load() {
        // 從無鎖佇列中取出訂單
        item := me.OrderQueue.Dequeue()
        if item == nil {
            // 佇列為空，短暫休眠
            runtime.Gosched()
            continue
        }

        order := item.(*Order)

        // 處理訂單（單執行緒，無鎖）
        orderBook := me.OrderBooks[order.Symbol]
        if orderBook == nil {
            orderBook = NewOrderBook(order.Symbol)
            me.OrderBooks[order.Symbol] = orderBook
        }

        // 撮合（不需要鎖！）
        trades := me.match(orderBook, order)

        // 發送交易記錄
        for _, trade := range trades {
            me.publishTrade(trade)
        }
    }
}

// SubmitOrder 提交訂單（外部呼叫，多執行緒安全）
func (me *MatchingEngineV2) SubmitOrder(order *Order) {
    // 加入無鎖佇列
    me.OrderQueue.Enqueue(order)
}
```

**Emma**：「單執行緒？這不會成為瓶頸嗎？」

**Michael**：「不會！因為：」
1. **CPU 不用在鎖上浪費時間**：無鎖設計消除了鎖競爭
2. **CPU 快取友好**：單執行緒避免了快取失效（Cache Invalidation）
3. **指令流水線優化**：CPU 可以更好地預測分支

**David**：「納斯達克（NASDAQ）的撮合引擎就是單執行緒的，每秒可以處理 100,000+ 訂單。」

### 2. 內存池（Memory Pool）

**Sarah**：「每筆交易都會建立大量物件。Go 的 GC 會帶來延遲抖動（Latency Jitter）。」

**Michael**：「我們使用 **物件池（Object Pool）** 來重用物件：」

```go
// OrderPool 訂單物件池
var OrderPool = sync.Pool{
    New: func() interface{} {
        return &Order{}
    },
}

// AcquireOrder 從池中獲取訂單
func AcquireOrder() *Order {
    return OrderPool.Get().(*Order)
}

// ReleaseOrder 歸還訂單到池
func ReleaseOrder(order *Order) {
    // 重置欄位
    order.ID = 0
    order.UserID = ""
    order.Symbol = ""
    order.Quantity = 0
    order.FilledQty = 0

    // 放回池
    OrderPool.Put(order)
}

// 使用範例
func (me *MatchingEngine) ProcessOrderOptimized(orderData *OrderData) {
    // 1. 從池中獲取訂單物件
    order := AcquireOrder()
    defer ReleaseOrder(order) // 函式結束後歸還

    // 2. 填充資料
    order.ID = orderData.ID
    order.UserID = orderData.UserID
    order.Symbol = orderData.Symbol
    // ...

    // 3. 處理訂單
    trades := me.match(order)
    // ...
}
```

### 3. 預分配（Pre-allocation）

**David**：「避免動態分配記憶體：」

```go
// OrderBook 預分配版本
type OrderBookOptimized struct {
    Symbol string

    // 預分配價格層級（假設價格範圍：0-100000 分）
    BidLevels [100000]*PriceLevel
    AskLevels [100000]*PriceLevel

    // 最優買價和賣價索引
    BestBidIndex int
    BestAskIndex int
}

// GetBestBidOptimized O(1) 獲取最優買價
func (ob *OrderBookOptimized) GetBestBidOptimized() (int64, int64, bool) {
    if ob.BestBidIndex == -1 {
        return 0, 0, false
    }

    level := ob.BidLevels[ob.BestBidIndex]
    return int64(ob.BestBidIndex), level.Volume, true
}
```

### 4. CPU 親和性（CPU Affinity）

**Michael**：「將撮合執行緒綁定到特定 CPU 核心：」

```go
import "runtime"
import "syscall"

// PinThreadToCPU 將執行緒綁定到 CPU 核心
func PinThreadToCPU(cpuID int) error {
    // 設定 CPU 親和性（Linux）
    var cpuSet syscall.CPUSet
    cpuSet.Set(cpuID)

    _, _, errno := syscall.RawSyscall(
        syscall.SYS_SCHED_SETAFFINITY,
        0,
        uintptr(unsafe.Sizeof(cpuSet)),
        uintptr(unsafe.Pointer(&cpuSet)),
    )

    if errno != 0 {
        return errno
    }

    return nil
}

// 在撮合執行緒中使用
func (me *MatchingEngine) matchingWorker() {
    // 綁定到 CPU 0
    if err := PinThreadToCPU(0); err != nil {
        log.Error("設定 CPU 親和性失敗", err)
    }

    // 設定為實時優先級
    runtime.LockOSThread()

    // 撮合迴圈
    for {
        // ...
    }
}
```

**Emma**：「這些優化能帶來多少效能提升？」

**David**：「讓我們看看基準測試結果：」

| 優化階段 | 延遲（P99） | 吞吐量 | 提升 |
|---------|-----------|--------|------|
| 基礎版本（有鎖） | 800µs | 5,000 ops/s | - |
| 無鎖設計 | 150µs | 25,000 ops/s | 5x |
| + 物件池 | 80µs | 50,000 ops/s | 10x |
| + 預分配 | 45µs | 80,000 ops/s | 16x |
| + CPU 親和性 | 25µs | 120,000 ops/s | 24x |

**Sarah**：「24 倍的提升！」

---

## Act 5: 行情推送與市場深度

**Emma**：「撮合引擎產生交易後，我們需要即時推送行情給用戶。」

**David**：「沒錯。我們需要推送兩種資料：」
1. **成交行情（Trades）**：最新成交價、成交量
2. **市場深度（Market Depth）**：訂單簿的買賣盤資訊

### WebSocket 推送

**Michael**：「我們使用 WebSocket 來推送即時資料：」

```go
// MarketDataPublisher 行情發佈器
type MarketDataPublisher struct {
    // WebSocket 連線管理器
    connManager *websocket.ConnectionManager

    // 訂閱管理（symbol -> 訂閱者列表）
    subscriptions map[string]map[*websocket.Conn]bool

    mu sync.RWMutex
}

// Subscribe 訂閱股票行情
func (p *MarketDataPublisher) Subscribe(conn *websocket.Conn, symbol string) {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.subscriptions[symbol] == nil {
        p.subscriptions[symbol] = make(map[*websocket.Conn]bool)
    }

    p.subscriptions[symbol][conn] = true

    log.Info("用戶訂閱行情", "symbol", symbol, "conn", conn.RemoteAddr())
}

// PublishTrade 發佈成交資訊
func (p *MarketDataPublisher) PublishTrade(trade *Trade) {
    p.mu.RLock()
    subscribers := p.subscriptions[trade.Symbol]
    p.mu.RUnlock()

    if len(subscribers) == 0 {
        return
    }

    // 序列化交易資料
    message := &TradeMessage{
        Type:      "trade",
        Symbol:    trade.Symbol,
        Price:     trade.Price,
        Quantity:  trade.Quantity,
        Timestamp: trade.Timestamp.UnixMilli(),
    }

    data, _ := json.Marshal(message)

    // 廣播給所有訂閱者
    for conn := range subscribers {
        go func(c *websocket.Conn) {
            if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
                log.Error("發送行情失敗", err)
                p.Unsubscribe(c, trade.Symbol)
            }
        }(conn)
    }
}

// PublishOrderBook 發佈訂單簿深度
func (p *MarketDataPublisher) PublishOrderBook(symbol string, ob *OrderBook) {
    p.mu.RLock()
    subscribers := p.subscriptions[symbol]
    p.mu.RUnlock()

    if len(subscribers) == 0 {
        return
    }

    // 獲取 Level 2 資料（前 10 檔買賣價）
    depth := ob.GetDepth(10)

    message := &OrderBookMessage{
        Type:   "depth",
        Symbol: symbol,
        Bids:   depth.Bids,
        Asks:   depth.Asks,
        Timestamp: time.Now().UnixMilli(),
    }

    data, _ := json.Marshal(message)

    // 廣播
    for conn := range subscribers {
        go func(c *websocket.Conn) {
            c.WriteMessage(websocket.TextMessage, data)
        }(conn)
    }
}

// Depth 市場深度
type Depth struct {
    Bids []PriceQuantity // 買盤（價格從高到低）
    Asks []PriceQuantity // 賣盤（價格從低到高）
}

type PriceQuantity struct {
    Price    int64 `json:"price"`
    Quantity int64 `json:"quantity"`
}

// GetDepth 獲取訂單簿深度
func (ob *OrderBook) GetDepth(levels int) *Depth {
    depth := &Depth{
        Bids: make([]PriceQuantity, 0, levels),
        Asks: make([]PriceQuantity, 0, levels),
    }

    // 獲取買盤前 N 檔
    count := 0
    ob.Bids.Tree.Iterator(func(price interface{}, level interface{}) bool {
        if count >= levels {
            return false
        }

        pl := level.(*PriceLevel)
        depth.Bids = append(depth.Bids, PriceQuantity{
            Price:    pl.Price,
            Quantity: pl.Volume,
        })

        count++
        return true
    })

    // 獲取賣盤前 N 檔
    count = 0
    ob.Asks.Tree.Iterator(func(price interface{}, level interface{}) bool {
        if count >= levels {
            return false
        }

        pl := level.(*PriceLevel)
        depth.Asks = append(depth.Asks, PriceQuantity{
            Price:    pl.Price,
            Quantity: pl.Volume,
        })

        count++
        return true
    })

    return depth
}
```

### 增量更新（Incremental Updates）

**Sarah**：「每次都發送完整的訂單簿太浪費頻寬了。能否只發送變化的部分？」

**David**：「可以！我們使用 **增量更新（Incremental Updates）**：」

```go
// OrderBookDelta 訂單簿增量更新
type OrderBookDelta struct {
    Symbol    string
    Side      string // "bid" or "ask"
    Price     int64
    Quantity  int64  // 0 表示該價格層級已移除
    Timestamp int64
}

// PublishOrderBookDelta 發佈訂單簿增量
func (p *MarketDataPublisher) PublishOrderBookDelta(delta *OrderBookDelta) {
    subscribers := p.subscriptions[delta.Symbol]

    message := &OrderBookDeltaMessage{
        Type:      "depth_delta",
        Symbol:    delta.Symbol,
        Side:      delta.Side,
        Price:     delta.Price,
        Quantity:  delta.Quantity,
        Timestamp: delta.Timestamp,
    }

    data, _ := json.Marshal(message)

    for conn := range subscribers {
        conn.WriteMessage(websocket.TextMessage, data)
    }
}
```

**Emma**：「增量更新節省了多少頻寬？」

**Michael**：「對於活躍股票，節省 **90% 以上** 的頻寬！」

| 更新方式 | 訊息大小 | 頻率 | 頻寬（每秒） |
|---------|---------|------|-------------|
| 完整訂單簿 | 5 KB | 10 次/秒 | 50 KB/s |
| 增量更新 | 100 B | 100 次/秒 | 10 KB/s |

---

## Act 6: 風控系統

**David**：「交易系統還需要完善的 **風控系統（Risk Control System）** 來防止異常交易。」

**Emma**：「有哪些風險需要防範？」

### 1. 熔斷機制（Circuit Breaker）

**Michael**：「當價格波動過大時，暫停交易：」

```go
// CircuitBreaker 熔斷器
type CircuitBreaker struct {
    Symbol string

    // 基準價格（通常是前一日收盤價）
    ReferencePrice int64

    // 熔斷閾值（例如：±10%）
    UpperLimit int64 // 漲停價
    LowerLimit int64 // 跌停價

    // 熔斷狀態
    IsHalted bool
    HaltReason string
    HaltedAt time.Time
}

// NewCircuitBreaker 建立熔斷器
func NewCircuitBreaker(symbol string, referencePrice int64, limitPercent float64) *CircuitBreaker {
    upperLimit := int64(float64(referencePrice) * (1 + limitPercent))
    lowerLimit := int64(float64(referencePrice) * (1 - limitPercent))

    return &CircuitBreaker{
        Symbol:         symbol,
        ReferencePrice: referencePrice,
        UpperLimit:     upperLimit,
        LowerLimit:     lowerLimit,
        IsHalted:       false,
    }
}

// CheckPrice 檢查價格是否觸發熔斷
func (cb *CircuitBreaker) CheckPrice(price int64) error {
    if cb.IsHalted {
        return fmt.Errorf("交易已暫停: %s", cb.HaltReason)
    }

    if price > cb.UpperLimit {
        cb.IsHalted = true
        cb.HaltReason = fmt.Sprintf("價格 %d 超過漲停價 %d", price, cb.UpperLimit)
        cb.HaltedAt = time.Now()
        return errors.New(cb.HaltReason)
    }

    if price < cb.LowerLimit {
        cb.IsHalted = true
        cb.HaltReason = fmt.Sprintf("價格 %d 低於跌停價 %d", price, cb.LowerLimit)
        cb.HaltedAt = time.Now()
        return errors.New(cb.HaltReason)
    }

    return nil
}

// Resume 恢復交易
func (cb *CircuitBreaker) Resume() {
    cb.IsHalted = false
    cb.HaltReason = ""
    log.Info("恢復交易", "symbol", cb.Symbol)
}
```

### 2. 訂單頻率限制（Rate Limiting）

**Sarah**：「防止用戶過度頻繁下單（可能是 Bug 或惡意攻擊）：」

```go
// OrderRateLimiter 訂單頻率限制器
type OrderRateLimiter struct {
    // 用戶 -> Token Bucket
    limiters map[string]*rate.Limiter

    // 限制：每秒 10 筆訂單，突發 20 筆
    rate  rate.Limit
    burst int

    mu sync.RWMutex
}

// NewOrderRateLimiter 建立頻率限制器
func NewOrderRateLimiter(ordersPerSecond int, burst int) *OrderRateLimiter {
    return &OrderRateLimiter{
        limiters: make(map[string]*rate.Limiter),
        rate:     rate.Limit(ordersPerSecond),
        burst:    burst,
    }
}

// Allow 檢查用戶是否可以下單
func (l *OrderRateLimiter) Allow(userID string) bool {
    l.mu.Lock()
    defer l.mu.Unlock()

    limiter, exists := l.limiters[userID]
    if !exists {
        limiter = rate.NewLimiter(l.rate, l.burst)
        l.limiters[userID] = limiter
    }

    return limiter.Allow()
}

// 在撮合引擎中使用
func (me *MatchingEngine) ProcessOrderWithRateLimit(order *Order) error {
    // 檢查頻率限制
    if !me.rateLimiter.Allow(order.UserID) {
        return errors.New("下單過於頻繁，請稍後再試")
    }

    // 處理訂單
    trades := me.ProcessOrder(order)
    // ...

    return nil
}
```

### 3. 異常檢測（Anomaly Detection）

**David**：「使用機器學習檢測異常交易模式：」

```go
// AnomalyDetector 異常檢測器
type AnomalyDetector struct {
    // 用戶歷史交易統計
    userStats map[string]*UserTradingStats

    mu sync.RWMutex
}

// UserTradingStats 用戶交易統計
type UserTradingStats struct {
    UserID string

    // 統計指標
    AvgOrderSize      float64 // 平均訂單大小
    StdDevOrderSize   float64 // 標準差
    AvgOrderFrequency float64 // 平均下單頻率

    // 最近訂單
    RecentOrders []*Order
}

// DetectAnomaly 檢測訂單是否異常
func (d *AnomalyDetector) DetectAnomaly(order *Order) (bool, string) {
    d.mu.RLock()
    stats := d.userStats[order.UserID]
    d.mu.RUnlock()

    if stats == nil {
        // 新用戶，暫不檢測
        return false, ""
    }

    // 檢測 1：訂單大小異常
    // 如果訂單大小超過平均值的 5 倍標準差
    if float64(order.Quantity) > stats.AvgOrderSize+5*stats.StdDevOrderSize {
        return true, fmt.Sprintf("訂單大小異常：%d 股（平均：%.0f 股）",
            order.Quantity, stats.AvgOrderSize)
    }

    // 檢測 2：短時間內大量下單
    recentCount := 0
    cutoff := time.Now().Add(-1 * time.Minute)
    for _, o := range stats.RecentOrders {
        if o.CreatedAt.After(cutoff) {
            recentCount++
        }
    }

    if recentCount > 50 {
        return true, fmt.Sprintf("1 分鐘內下單 %d 次", recentCount)
    }

    // 檢測 3：價格異常（限價單價格遠離市價）
    if order.Type == OrderTypeLimit {
        marketPrice := d.getMarketPrice(order.Symbol)
        deviation := math.Abs(float64(order.Price-marketPrice)) / float64(marketPrice)

        if deviation > 0.2 { // 偏離市價超過 20%
            return true, fmt.Sprintf("限價 %d 偏離市價 %d 超過 20%%",
                order.Price, marketPrice)
        }
    }

    return false, ""
}
```

**Emma**：「這樣就能防止大部分的異常交易了！」

---

## Act 7: 持久化與災難恢復

**Michael**：「最後一個關鍵問題：如果系統崩潰，訂單簿中的所有訂單都會丟失！」

**Sarah**：「我們需要持久化嗎？但寫資料庫會嚴重影響效能。」

**David**：「我們使用 **WAL（Write-Ahead Log）** + **快照（Snapshot）** 的方式。」

### WAL（寫前日誌）

**Michael**：「每個操作先寫入日誌，再更新內存：」

```go
// WAL Write-Ahead Log
type WAL struct {
    file   *os.File
    writer *bufio.Writer
    mu     sync.Mutex
}

// LogEntry 日誌條目
type LogEntry struct {
    Timestamp int64
    Type      string // "add_order", "cancel_order", "trade"
    Data      []byte // JSON
}

// Append 追加日誌
func (w *WAL) Append(entry *LogEntry) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    // 序列化
    data, err := json.Marshal(entry)
    if err != nil {
        return err
    }

    // 寫入長度
    length := uint32(len(data))
    binary.Write(w.writer, binary.LittleEndian, length)

    // 寫入資料
    w.writer.Write(data)

    // 強制刷新到磁碟（保證持久性）
    w.writer.Flush()
    w.file.Sync()

    return nil
}

// 在撮合引擎中使用
func (me *MatchingEngine) ProcessOrderWithWAL(order *Order) {
    // 1. 先寫 WAL
    entry := &LogEntry{
        Timestamp: time.Now().UnixNano(),
        Type:      "add_order",
        Data:      toJSON(order),
    }

    if err := me.wal.Append(entry); err != nil {
        log.Error("寫入 WAL 失敗", err)
        return
    }

    // 2. 再處理訂單
    trades := me.ProcessOrder(order)

    // 3. 記錄交易到 WAL
    for _, trade := range trades {
        tradeEntry := &LogEntry{
            Timestamp: time.Now().UnixNano(),
            Type:      "trade",
            Data:      toJSON(trade),
        }
        me.wal.Append(tradeEntry)
    }
}
```

### 快照（Snapshot）

**David**：「定期保存訂單簿的完整快照：」

```go
// Snapshot 快照
type Snapshot struct {
    Timestamp  int64
    OrderBooks map[string]*OrderBookSnapshot
}

// OrderBookSnapshot 訂單簿快照
type OrderBookSnapshot struct {
    Symbol string
    Bids   []OrderSnapshot
    Asks   []OrderSnapshot
}

// OrderSnapshot 訂單快照
type OrderSnapshot struct {
    ID        int64
    UserID    string
    Side      string
    Price     int64
    Quantity  int64
    FilledQty int64
    CreatedAt int64
}

// CreateSnapshot 建立快照
func (me *MatchingEngine) CreateSnapshot() *Snapshot {
    snapshot := &Snapshot{
        Timestamp:  time.Now().UnixNano(),
        OrderBooks: make(map[string]*OrderBookSnapshot),
    }

    for symbol, ob := range me.OrderBooks {
        obSnapshot := &OrderBookSnapshot{
            Symbol: symbol,
            Bids:   make([]OrderSnapshot, 0),
            Asks:   make([]OrderSnapshot, 0),
        }

        // 快照所有買單
        ob.Bids.Tree.Iterator(func(price, level interface{}) bool {
            pl := level.(*PriceLevel)
            for e := pl.Orders.Front(); e != nil; e = e.Next() {
                order := e.Value.(*Order)
                obSnapshot.Bids = append(obSnapshot.Bids, toOrderSnapshot(order))
            }
            return true
        })

        // 快照所有賣單
        ob.Asks.Tree.Iterator(func(price, level interface{}) bool {
            pl := level.(*PriceLevel)
            for e := pl.Orders.Front(); e != nil; e = e.Next() {
                order := e.Value.(*Order)
                obSnapshot.Asks = append(obSnapshot.Asks, toOrderSnapshot(order))
            }
            return true
        })

        snapshot.OrderBooks[symbol] = obSnapshot
    }

    return snapshot
}

// SaveSnapshot 保存快照到磁碟
func (me *MatchingEngine) SaveSnapshot(snapshot *Snapshot) error {
    filename := fmt.Sprintf("snapshot_%d.json", snapshot.Timestamp)

    data, err := json.Marshal(snapshot)
    if err != nil {
        return err
    }

    // 壓縮
    compressed := gzip.Compress(data)

    return os.WriteFile(filename, compressed, 0644)
}

// 定期快照
func (me *MatchingEngine) snapshotWorker() {
    ticker := time.NewTicker(1 * time.Minute) // 每分鐘一次快照
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            snapshot := me.CreateSnapshot()
            if err := me.SaveSnapshot(snapshot); err != nil {
                log.Error("保存快照失敗", err)
            } else {
                log.Info("快照已保存", "timestamp", snapshot.Timestamp)
            }
        }
    }
}
```

### 災難恢復

**Sarah**：「系統重啟後如何恢復？」

**Michael**：「載入最新快照 + 重放 WAL：」

```go
// Recover 災難恢復
func (me *MatchingEngine) Recover() error {
    log.Info("開始災難恢復")

    // 1. 找到最新的快照
    snapshot, err := me.loadLatestSnapshot()
    if err != nil {
        return fmt.Errorf("載入快照失敗: %w", err)
    }

    // 2. 恢復訂單簿
    for symbol, obSnapshot := range snapshot.OrderBooks {
        ob := NewOrderBook(symbol)

        // 恢復買單
        for _, orderSnap := range obSnapshot.Bids {
            order := fromOrderSnapshot(&orderSnap)
            ob.AddOrder(order)
        }

        // 恢復賣單
        for _, orderSnap := range obSnapshot.Asks {
            order := fromOrderSnapshot(&orderSnap)
            ob.AddOrder(order)
        }

        me.OrderBooks[symbol] = ob
    }

    log.Info("快照已恢復", "timestamp", snapshot.Timestamp)

    // 3. 重放快照之後的 WAL
    entries, err := me.wal.ReadFrom(snapshot.Timestamp)
    if err != nil {
        return fmt.Errorf("讀取 WAL 失敗: %w", err)
    }

    log.Info("開始重放 WAL", "entries", len(entries))

    for _, entry := range entries {
        switch entry.Type {
        case "add_order":
            var order Order
            json.Unmarshal(entry.Data, &order)
            me.ProcessOrder(&order)

        case "cancel_order":
            var cancelData struct {
                OrderID int64 `json:"order_id"`
            }
            json.Unmarshal(entry.Data, &cancelData)
            me.CancelOrder(cancelData.OrderID)

        case "trade":
            // 交易記錄僅用於審計，不影響訂單簿狀態
        }
    }

    log.Info("災難恢復完成")
    return nil
}
```

**Emma**：「這樣即使系統崩潰，我們也能完整恢復所有訂單！」

**David**：「沒錯。WAL + 快照 是資料庫系統普遍使用的可靠方案。」

---

## 總結

本章我們深入學習了 **Stock Exchange（股票交易系統）** 的設計，涵蓋：

### 核心技術點

1. **訂單撮合引擎**
   - 訂單類型：市價單、限價單、停損單
   - 撮合原則：價格優先、時間優先
   - 時間複雜度：O(1) 均攤

2. **訂單簿數據結構**
   - 紅黑樹（Sorted Map）儲存價格層級
   - 雙向鏈表（Linked List）維護時間順序
   - O(1) 查詢最優價格，O(log n) 插入刪除

3. **低延遲優化**
   - 無鎖設計（Lock-Free Queue + 單執行緒）
   - 物件池（避免 GC）
   - 預分配（避免動態記憶體分配）
   - CPU 親和性（Pin to CPU Core）
   - **效能提升：24 倍**（從 800µs 降至 25µs）

4. **行情推送**
   - WebSocket 即時推送
   - 增量更新（節省 90% 頻寬）
   - Level 2 市場深度

5. **風控系統**
   - 熔斷機制（Circuit Breaker）
   - 訂單頻率限制（Rate Limiting）
   - 異常檢測（Anomaly Detection）

6. **持久化與災難恢復**
   - WAL（Write-Ahead Log）
   - 定期快照（Snapshot）
   - 災難恢復（Snapshot + WAL Replay）

### 架構特點

- **極致效能**：微秒級延遲，10 萬+ QPS
- **強一致性**：100% 正確的撮合結果
- **高可用性**：99.999% 可用性
- **可觀測性**：完整的行情推送和審計日誌

股票交易系統是全世界最複雜的金融系統之一。通過本章學習，你已經掌握了構建世界級交易所的核心技術！📈✨
