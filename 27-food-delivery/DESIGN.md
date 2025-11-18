# Chapter 27: Food Delivery (UberEats) - 外送平台系統設計

## 系統概述

UberEats、美團外賣等外送平台需要處理餐廳、顧客、外送員三方的複雜協調。本章將深入探討如何設計一個高效能、高可用的外送平台系統。

**核心挑戰**：
- 訂單匹配（將訂單分配給最合適的外送員）
- 多訂單打包（一個外送員同時送多個訂單）
- 路線優化（解決 TSP 問題，最短路徑）
- ETA 預測（準確預測送達時間）
- 即時追蹤（顧客追蹤外送員位置）
- 高峰調度（中午、晚餐時段訂單激增）

---

## Act 1: 訂單流程與狀態機

**場景**：週五晚上 7 點，顧客在 App 上點了一份炸雞，系統開始協調餐廳、外送員、顧客...

### 1.1 對話：Emma 與 David 討論訂單流程

**Emma**（產品經理）：一個外送訂單從下單到送達，中間要經過哪些步驟？

**David**（後端工程師）：讓我畫出完整的訂單流程：

```
1. 顧客下單 → 2. 餐廳確認 → 3. 配對外送員 → 4. 外送員取餐 → 5. 配送中 → 6. 送達
```

但實際上比這複雜得多，因為每個步驟都可能失敗或取消。

### 1.2 訂單狀態機

**Michael**（資深架構師）：我們需要一個完善的**狀態機**（State Machine）。

```go
// internal/order/state.go
package order

type OrderStatus string

const (
    // 顧客階段
    StatusPending           OrderStatus = "pending"            // 待確認
    StatusRestaurantConfirmed OrderStatus = "restaurant_confirmed" // 餐廳已確認

    // 配對階段
    StatusSearchingDriver   OrderStatus = "searching_driver"   // 尋找外送員
    StatusDriverAssigned    OrderStatus = "driver_assigned"    // 已派單

    // 取餐階段
    StatusDriverArriving    OrderStatus = "driver_arriving"    // 外送員前往餐廳
    StatusDriverArrived     OrderStatus = "driver_arrived"     // 外送員到達餐廳
    StatusPreparing         OrderStatus = "preparing"          // 餐點準備中
    StatusPickedUp          OrderStatus = "picked_up"          // 已取餐

    // 配送階段
    StatusInTransit         OrderStatus = "in_transit"         // 配送中
    StatusNearby            OrderStatus = "nearby"             // 即將到達

    // 完成階段
    StatusDelivered         OrderStatus = "delivered"          // 已送達
    StatusCompleted         OrderStatus = "completed"          // 已完成（評價後）

    // 異常狀態
    StatusCancelled         OrderStatus = "cancelled"          // 已取消
    StatusFailed            OrderStatus = "failed"             // 失敗
)

type Order struct {
    ID              int64       `json:"id"`
    CustomerID      int64       `json:"customer_id"`
    RestaurantID    int64       `json:"restaurant_id"`
    DriverID        int64       `json:"driver_id"`
    Status          OrderStatus `json:"status"`

    // 地點
    RestaurantLat   float64     `json:"restaurant_lat"`
    RestaurantLng   float64     `json:"restaurant_lng"`
    DeliveryLat     float64     `json:"delivery_lat"`
    DeliveryLng     float64     `json:"delivery_lng"`
    DeliveryAddress string      `json:"delivery_address"`

    // 時間戳
    CreatedAt       time.Time   `json:"created_at"`
    ConfirmedAt     *time.Time  `json:"confirmed_at"`
    AssignedAt      *time.Time  `json:"assigned_at"`
    PickedUpAt      *time.Time  `json:"picked_up_at"`
    DeliveredAt     *time.Time  `json:"delivered_at"`

    // 金額
    FoodPrice       float64     `json:"food_price"`
    DeliveryFee     float64     `json:"delivery_fee"`
    TotalPrice      float64     `json:"total_price"`

    // 預估時間
    EstimatedPickupTime  time.Time `json:"estimated_pickup_time"`
    EstimatedDeliveryTime time.Time `json:"estimated_delivery_time"`
}
```

### 1.3 狀態轉換邏輯

```go
// internal/order/state_machine.go
package order

type StateMachine struct {
    db    *PostgreSQL
    cache *RedisClient
}

// TransitionTo 狀態轉換
func (sm *StateMachine) TransitionTo(ctx context.Context, orderID int64, newStatus OrderStatus) error {
    // 1. 取得當前訂單
    order, err := sm.getOrder(ctx, orderID)
    if err != nil {
        return err
    }

    // 2. 驗證狀態轉換是否合法
    if !sm.isValidTransition(order.Status, newStatus) {
        return fmt.Errorf("invalid transition from %s to %s", order.Status, newStatus)
    }

    // 3. 執行狀態轉換
    err = sm.db.ExecContext(ctx, `
        UPDATE orders
        SET status = ?, updated_at = ?
        WHERE id = ?
    `, newStatus, time.Now(), orderID)

    if err != nil {
        return err
    }

    // 4. 觸發相應的業務邏輯
    sm.handleStateChange(ctx, order, newStatus)

    return nil
}

// isValidTransition 檢查狀態轉換是否合法
func (sm *StateMachine) isValidTransition(from, to OrderStatus) bool {
    validTransitions := map[OrderStatus][]OrderStatus{
        StatusPending: {
            StatusRestaurantConfirmed,
            StatusCancelled,
        },
        StatusRestaurantConfirmed: {
            StatusSearchingDriver,
            StatusCancelled,
        },
        StatusSearchingDriver: {
            StatusDriverAssigned,
            StatusCancelled,
        },
        StatusDriverAssigned: {
            StatusDriverArriving,
            StatusCancelled,
        },
        StatusDriverArriving: {
            StatusDriverArrived,
            StatusCancelled,
        },
        StatusDriverArrived: {
            StatusPreparing,
            StatusPickedUp,
        },
        StatusPreparing: {
            StatusPickedUp,
        },
        StatusPickedUp: {
            StatusInTransit,
        },
        StatusInTransit: {
            StatusNearby,
            StatusDelivered,
        },
        StatusNearby: {
            StatusDelivered,
        },
        StatusDelivered: {
            StatusCompleted,
        },
    }

    allowedStates, exists := validTransitions[from]
    if !exists {
        return false
    }

    for _, allowed := range allowedStates {
        if allowed == to {
            return true
        }
    }

    return false
}

// handleStateChange 處理狀態變化的副作用
func (sm *StateMachine) handleStateChange(ctx context.Context, order *Order, newStatus OrderStatus) {
    switch newStatus {
    case StatusRestaurantConfirmed:
        // 開始搜尋外送員
        sm.startSearchingDriver(ctx, order)

    case StatusDriverAssigned:
        // 通知外送員、顧客
        sm.notifyDriverAndCustomer(ctx, order)

    case StatusPickedUp:
        // 更新 ETA、通知顧客
        sm.updateETAAndNotify(ctx, order)

    case StatusDelivered:
        // 扣款、結算、請求評價
        sm.processPaymentAndSettle(ctx, order)
    }
}
```

---

## Act 2: 外送員匹配算法

**場景**：台北市中心有 100 個外送員在線，新訂單進來，要派給誰？

### 2.1 對話：匹配算法的考量因素

**Emma**：怎麼決定派哪個外送員？

**Michael**：這不是簡單的「最近距離」問題。我們要考慮：
1. **距離**：外送員到餐廳的距離
2. **方向**：外送員當前行駛方向是否朝向餐廳
3. **訂單狀態**：外送員是否已有其他訂單（多單配送）
4. **評分**：外送員的評分
5. **接單率**：外送員的接單率（避免派給常拒單的人）
6. **配送時效**：能否在預期時間內送達

### 2.2 匹配演算法實作

```go
// internal/matching/driver_matcher.go
package matching

type DriverMatcher struct {
    geoIndex *geo.S2Index
    scorer   *MatchingScorer
}

type DriverCandidate struct {
    DriverID       int64
    Latitude       float64
    Longitude      float64
    CurrentOrders  int       // 當前訂單數
    Rating         float64
    AcceptanceRate float64
    Bearing        float64   // 行駛方向
    Distance       float64   // 到餐廳的距離
    ETA            int       // 到餐廳的 ETA（秒）
    Score          float64   // 綜合評分
}

// FindBestDriver 找出最佳外送員
func (m *DriverMatcher) FindBestDriver(ctx context.Context, order *Order) (*DriverCandidate, error) {
    // 1. 找出餐廳附近 3 公里內的外送員
    nearbyDrivers, err := m.geoIndex.FindNearbyDrivers(
        ctx,
        order.RestaurantLat,
        order.RestaurantLng,
        3.0,
    )

    if err != nil {
        return nil, err
    }

    if len(nearbyDrivers) == 0 {
        return nil, fmt.Errorf("no drivers available")
    }

    // 2. 過濾可用的外送員
    candidates := make([]*DriverCandidate, 0)
    for _, driverID := range nearbyDrivers {
        driver, err := m.getDriverInfo(ctx, driverID)
        if err != nil {
            continue
        }

        // 檢查外送員狀態
        if driver.Status != "available" && driver.Status != "on_delivery" {
            continue
        }

        // 檢查是否已達最大訂單數（最多同時 3 單）
        if driver.CurrentOrders >= 3 {
            continue
        }

        // 計算距離
        driver.Distance = calculateDistance(
            order.RestaurantLat, order.RestaurantLng,
            driver.Latitude, driver.Longitude,
        )

        // 計算 ETA
        driver.ETA, _ = m.calculateETA(ctx, driver, order)

        candidates = append(candidates, driver)
    }

    if len(candidates) == 0 {
        return nil, fmt.Errorf("no suitable drivers")
    }

    // 3. 計算每個外送員的匹配分數
    for _, driver := range candidates {
        driver.Score = m.scorer.CalculateScore(driver, order)
    }

    // 4. 排序並選擇最佳外送員
    sort.Slice(candidates, func(i, j int) bool {
        return candidates[i].Score > candidates[j].Score
    })

    // 5. 嘗試派單（使用分散式鎖防止重複派單）
    bestDriver := candidates[0]
    locked, err := m.tryLockDriver(ctx, bestDriver.DriverID, order.ID)
    if !locked {
        // 第一選擇被鎖定，嘗試第二選擇
        if len(candidates) > 1 {
            return m.tryAssignToDriver(ctx, candidates[1], order)
        }
        return nil, fmt.Errorf("all drivers busy")
    }

    return bestDriver, nil
}

// CalculateScore 計算外送員匹配分數
func (s *MatchingScorer) CalculateScore(driver *DriverCandidate, order *Order) float64 {
    const (
        distanceWeight     = 0.35  // 距離權重
        etaWeight          = 0.25  // ETA 權重
        ratingWeight       = 0.15  // 評分權重
        acceptanceWeight   = 0.10  // 接單率權重
        directionWeight    = 0.10  // 方向權重
        orderLoadWeight    = 0.05  // 訂單負載權重
    )

    // 1. 距離分數（3 km 為基準）
    distanceScore := math.Max(0, 1 - driver.Distance/3.0)

    // 2. ETA 分數（10 分鐘為基準）
    etaScore := math.Max(0, 1 - float64(driver.ETA)/600.0)

    // 3. 評分分數
    ratingScore := driver.Rating / 5.0

    // 4. 接單率分數
    acceptanceScore := driver.AcceptanceRate

    // 5. 方向分數（朝向餐廳得高分）
    directionScore := s.calculateDirectionScore(driver, order)

    // 6. 訂單負載分數（已有訂單越少越好）
    orderLoadScore := 1.0 - (float64(driver.CurrentOrders) / 3.0)

    // 加權總分
    totalScore := distanceScore*distanceWeight +
                  etaScore*etaWeight +
                  ratingScore*ratingWeight +
                  acceptanceScore*acceptanceWeight +
                  directionScore*directionWeight +
                  orderLoadScore*orderLoadWeight

    return totalScore
}
```

---

## Act 3: 多訂單打包（Batching）

**場景**：外送員已經在送一個訂單，新的訂單可以順路一起送，提升效率...

### 3.1 對話：多單配送的挑戰

**Emma**：為什麼要讓一個外送員同時送多個訂單？

**David**：
1. **提升效率**：外送員單趟可以送 2-3 個訂單
2. **降低成本**：減少空跑的時間
3. **增加收入**：外送員收入提高

但也有挑戰：
- **路線規劃**：要找出最佳取餐、送達順序
- **時效保證**：不能讓第一個訂單等太久
- **顧客體驗**：顧客可能不滿意「順路送」

### 3.2 多單配送規則

**Michael**：我們需要一些規則：

```go
// internal/batching/rules.go
package batching

type BatchingRules struct {
    MaxOrdersPerDriver      int       // 最多同時 3 單
    MaxDetourDistance       float64   // 最大繞路距離 1 km
    MaxAdditionalTime       int       // 最多額外延遲 10 分鐘
    MaxPickupStops          int       // 最多取餐點數 2 個
    MinBatchingScore        float64   // 最低打包分數 0.7
}

var DefaultRules = &BatchingRules{
    MaxOrdersPerDriver:  3,
    MaxDetourDistance:   1.0,  // 1 km
    MaxAdditionalTime:   600,  // 10 分鐘
    MaxPickupStops:      2,
    MinBatchingScore:    0.7,
}

// CanBatch 判斷是否可以打包
func (b *Batcher) CanBatch(ctx context.Context, driverID int64, newOrder *Order) (bool, error) {
    // 1. 取得外送員當前的訂單
    currentOrders, err := b.getDriverCurrentOrders(ctx, driverID)
    if err != nil {
        return false, err
    }

    // 2. 檢查訂單數量
    if len(currentOrders) >= b.rules.MaxOrdersPerDriver {
        return false, nil
    }

    // 3. 檢查取餐點數量
    pickupStops := b.countUniqueRestaurants(currentOrders)
    if pickupStops >= b.rules.MaxPickupStops {
        return false, nil
    }

    // 4. 計算打包分數
    score := b.calculateBatchingScore(currentOrders, newOrder)
    if score < b.rules.MinBatchingScore {
        return false, nil
    }

    // 5. 模擬新路線，檢查是否超時
    newRoute := b.calculateOptimalRoute(append(currentOrders, newOrder))

    for i, order := range currentOrders {
        oldDeliveryTime := order.EstimatedDeliveryTime
        newDeliveryTime := newRoute.DeliveryTimes[i]

        additionalTime := newDeliveryTime.Sub(oldDeliveryTime).Seconds()
        if additionalTime > float64(b.rules.MaxAdditionalTime) {
            return false, nil
        }
    }

    return true, nil
}

// calculateBatchingScore 計算打包分數
func (b *Batcher) calculateBatchingScore(currentOrders []*Order, newOrder *Order) float64 {
    // 1. 檢查是否同一家餐廳（同餐廳打包最優）
    sameRestaurant := false
    for _, order := range currentOrders {
        if order.RestaurantID == newOrder.RestaurantID {
            sameRestaurant = true
            break
        }
    }

    if sameRestaurant {
        return 1.0 // 最高分
    }

    // 2. 計算送達地點的距離
    avgDistance := 0.0
    for _, order := range currentOrders {
        distance := calculateDistance(
            order.DeliveryLat, order.DeliveryLng,
            newOrder.DeliveryLat, newOrder.DeliveryLng,
        )
        avgDistance += distance
    }
    avgDistance /= float64(len(currentOrders))

    // 3. 距離越近，分數越高（1 km 為基準）
    distanceScore := math.Max(0, 1 - avgDistance/1.0)

    // 4. 考慮方向一致性
    directionScore := b.calculateDirectionConsistency(currentOrders, newOrder)

    // 加權總分
    return distanceScore*0.6 + directionScore*0.4
}
```

---

## Act 4: 路線優化（TSP 問題）

**場景**：外送員有 3 個訂單，要先取哪一個餐、先送哪一個？

### 4.1 對話：TSP 問題

**Michael**：這是經典的 **TSP（Traveling Salesman Problem，旅行商問題）**！

假設外送員有 3 個訂單：
- 訂單 A：餐廳 R1 → 地點 D1
- 訂單 B：餐廳 R2 → 地點 D2
- 訂單 C：餐廳 R1 → 地點 D3（同餐廳）

可能的路線有很多種：
1. R1(A,C) → D1 → D2 → R2(B) → D3
2. R1(A,C) → D1 → D3 → R2(B) → D2
3. R1(A,C) → R2(B) → D1 → D2 → D3
...

要找出**總時間最短**的路線。

### 4.2 路線優化演算法

```go
// internal/routing/optimizer.go
package routing

type RouteOptimizer struct {
    router *AStarRouter
}

type Stop struct {
    Type     string  // "pickup" or "delivery"
    OrderID  int64
    Location Location
    TimeWindow TimeWindow  // 時間窗口限制
}

type Location struct {
    Lat float64
    Lng float64
}

type TimeWindow struct {
    Earliest time.Time  // 最早時間
    Latest   time.Time  // 最晚時間
}

type Route struct {
    Stops         []*Stop
    TotalDistance float64
    TotalTime     int
    DeliveryTimes []time.Time
}

// OptimizeRoute 優化路線（使用啟發式算法）
func (o *RouteOptimizer) OptimizeRoute(ctx context.Context, driverLocation Location, orders []*Order) (*Route, error) {
    // 1. 建立所有停靠點
    stops := o.buildStops(orders)

    // 2. 使用貪婪算法找出初始路線
    initialRoute := o.greedyRoute(driverLocation, stops)

    // 3. 使用 2-opt 算法優化
    optimizedRoute := o.twoOptOptimization(initialRoute)

    // 4. 驗證時間窗口限制
    if !o.validateTimeWindows(optimizedRoute) {
        // 調整路線順序以滿足時間限制
        optimizedRoute = o.adjustForTimeWindows(optimizedRoute)
    }

    return optimizedRoute, nil
}

// greedyRoute 貪婪算法：每次選擇最近的未訪問點
func (o *RouteOptimizer) greedyRoute(start Location, stops []*Stop) *Route {
    route := &Route{
        Stops: make([]*Stop, 0, len(stops)),
    }

    visited := make(map[int]bool)
    current := start

    // 確保先取餐，再送達
    pickupStops := o.filterByType(stops, "pickup")
    deliveryStops := o.filterByType(stops, "delivery")

    // 第一階段：取所有餐
    for len(pickupStops) > 0 {
        nearestIdx := o.findNearest(current, pickupStops, visited)
        stop := pickupStops[nearestIdx]

        route.Stops = append(route.Stops, stop)
        visited[nearestIdx] = true
        current = stop.Location

        pickupStops = o.removeVisited(pickupStops, visited)
    }

    // 第二階段：送所有餐
    visited = make(map[int]bool)  // 重置
    for len(deliveryStops) > 0 {
        nearestIdx := o.findNearest(current, deliveryStops, visited)
        stop := deliveryStops[nearestIdx]

        route.Stops = append(route.Stops, stop)
        visited[nearestIdx] = true
        current = stop.Location

        deliveryStops = o.removeVisited(deliveryStops, visited)
    }

    // 計算總距離和時間
    route.TotalDistance, route.TotalTime = o.calculateRouteMetrics(route)

    return route
}

// twoOptOptimization 2-opt 算法優化
func (o *RouteOptimizer) twoOptOptimization(route *Route) *Route {
    improved := true
    bestRoute := route

    for improved {
        improved = false

        for i := 1; i < len(bestRoute.Stops)-1; i++ {
            for j := i + 1; j < len(bestRoute.Stops); j++ {
                // 嘗試反轉 i 到 j 之間的順序
                newRoute := o.reverseSegment(bestRoute, i, j)

                // 如果新路線更短，採用新路線
                if newRoute.TotalDistance < bestRoute.TotalDistance {
                    // 但要確保不違反取餐-送達順序
                    if o.isValidRoute(newRoute) {
                        bestRoute = newRoute
                        improved = true
                    }
                }
            }
        }
    }

    return bestRoute
}

// isValidRoute 驗證路線是否合法（取餐必須在送達之前）
func (o *RouteOptimizer) isValidRoute(route *Route) bool {
    pickupTime := make(map[int64]int)  // OrderID -> 取餐時間索引

    for i, stop := range route.Stops {
        if stop.Type == "pickup" {
            pickupTime[stop.OrderID] = i
        } else if stop.Type == "delivery" {
            pickupIdx, exists := pickupTime[stop.OrderID]
            if !exists || pickupIdx >= i {
                // 送達在取餐之前，不合法
                return false
            }
        }
    }

    return true
}
```

### 4.3 考慮時間窗口的約束

```go
// validateTimeWindows 驗證時間窗口
func (o *RouteOptimizer) validateTimeWindows(route *Route) bool {
    currentTime := time.Now()

    for _, stop := range route.Stops {
        // 計算到達此停靠點的時間
        arrivalTime := currentTime.Add(time.Duration(stop.TravelTime) * time.Second)

        // 檢查是否在時間窗口內
        if arrivalTime.Before(stop.TimeWindow.Earliest) || arrivalTime.After(stop.TimeWindow.Latest) {
            return false
        }

        // 加上停留時間（取餐 3 分鐘，送達 2 分鐘）
        if stop.Type == "pickup" {
            currentTime = arrivalTime.Add(3 * time.Minute)
        } else {
            currentTime = arrivalTime.Add(2 * time.Minute)
        }
    }

    return true
}
```

---

## Act 5: ETA 預測

**場景**：顧客在 App 上看到「預計 35 分鐘送達」，這個時間是怎麼算出來的？

### 5.1 對話：ETA 的組成

**Emma**：外送的 ETA 要考慮哪些因素？

**Michael**：ETA 由三個部分組成：

```
總 ETA = 餐廳準備時間 + 外送員到餐廳時間 + 配送到顧客時間
```

每個部分都需要精確預測。

### 5.2 ETA 預測實作

```go
// internal/eta/predictor.go
package eta

type ETAPredictor struct {
    router         *AStarRouter
    trafficService *TrafficService
    mlModel        *MLModel
}

// PredictOrderETA 預測訂單 ETA
func (p *ETAPredictor) PredictOrderETA(ctx context.Context, order *Order, driver *Driver) (*ETAPrediction, error) {
    // 1. 預測餐廳準備時間（使用機器學習模型）
    prepTime := p.predictPreparationTime(ctx, order)

    // 2. 預測外送員到餐廳的時間
    pickupTime := p.predictPickupTime(ctx, driver, order)

    // 3. 預測配送到顧客的時間
    deliveryTime := p.predictDeliveryTime(ctx, order)

    // 4. 計算總 ETA
    totalETA := prepTime + pickupTime + deliveryTime

    // 5. 加入緩衝時間（避免承諾過早）
    bufferTime := int(float64(totalETA) * 0.15)  // 15% 緩衝
    finalETA := totalETA + bufferTime

    return &ETAPrediction{
        PreparationTime: prepTime,
        PickupTime:      pickupTime,
        DeliveryTime:    deliveryTime,
        BufferTime:      bufferTime,
        TotalETA:        finalETA,
        EstimatedDeliveryAt: time.Now().Add(time.Duration(finalETA) * time.Second),
    }, nil
}

// predictPreparationTime 預測餐廳準備時間（機器學習模型）
func (p *ETAPredictor) predictPreparationTime(ctx context.Context, order *Order) int {
    // 提取特徵
    features := []float64{
        float64(order.ItemCount),                    // 餐點數量
        float64(time.Now().Hour()),                  // 當前小時
        float64(time.Now().Weekday()),               // 星期幾
        p.getRestaurantHistoricalAvg(order.RestaurantID), // 餐廳歷史平均
    }

    // 使用模型預測（單位：秒）
    predicted := p.mlModel.Predict(features)

    // 限制範圍（5-30 分鐘）
    predicted = math.Max(300, math.Min(predicted, 1800))

    return int(predicted)
}

// predictPickupTime 預測外送員到餐廳時間
func (p *ETAPredictor) predictPickupTime(ctx context.Context, driver *Driver, order *Order) int {
    // 使用路徑規劃服務計算（考慮即時路況）
    duration, err := p.router.CalculateETA(ctx,
        driver.Latitude, driver.Longitude,
        order.RestaurantLat, order.RestaurantLng,
    )

    if err != nil {
        // 失敗時使用直線距離估算
        distance := calculateDistance(
            driver.Latitude, driver.Longitude,
            order.RestaurantLat, order.RestaurantLng,
        )
        // 假設平均速度 20 km/h
        duration = int(distance / 20.0 * 3600)
    }

    return duration
}

// predictDeliveryTime 預測配送時間
func (p *ETAPredictor) predictDeliveryTime(ctx context.Context, order *Order) int {
    duration, err := p.router.CalculateETA(ctx,
        order.RestaurantLat, order.RestaurantLng,
        order.DeliveryLat, order.DeliveryLng,
    )

    if err != nil {
        distance := calculateDistance(
            order.RestaurantLat, order.RestaurantLng,
            order.DeliveryLat, order.DeliveryLng,
        )
        duration = int(distance / 15.0 * 3600)  // 假設 15 km/h（市區較慢）
    }

    // 加上停車、找地址、電梯等時間（2-5 分鐘）
    duration += 180

    return duration
}
```

### 5.3 動態 ETA 更新

```go
// UpdateETAInRealtime 即時更新 ETA
func (p *ETAPredictor) UpdateETAInRealtime(ctx context.Context, orderID int64) error {
    // 1. 取得訂單和外送員當前位置
    order, err := p.getOrder(ctx, orderID)
    if err != nil {
        return err
    }

    driver, err := p.getDriver(ctx, order.DriverID)
    if err != nil {
        return err
    }

    // 2. 根據當前狀態重新計算 ETA
    var newETA int

    switch order.Status {
    case StatusDriverArriving:
        // 外送員前往餐廳中，只需計算到餐廳的時間
        pickupTime := p.predictPickupTime(ctx, driver, order)
        prepTime := p.predictPreparationTime(ctx, order)
        deliveryTime := p.predictDeliveryTime(ctx, order)
        newETA = pickupTime + prepTime + deliveryTime

    case StatusPreparing:
        // 已到達餐廳，等待取餐
        prepTime := p.getRemainingPrepTime(ctx, order)
        deliveryTime := p.predictDeliveryTime(ctx, order)
        newETA = prepTime + deliveryTime

    case StatusInTransit:
        // 配送中，只剩配送時間
        newETA = p.predictDeliveryTime(ctx, order)
    }

    // 3. 更新訂單的預估送達時間
    newDeliveryTime := time.Now().Add(time.Duration(newETA) * time.Second)

    err = p.db.ExecContext(ctx, `
        UPDATE orders
        SET estimated_delivery_time = ?
        WHERE id = ?
    `, newDeliveryTime, orderID)

    // 4. 如果 ETA 變化超過 5 分鐘，通知顧客
    oldETA := order.EstimatedDeliveryTime.Sub(time.Now()).Seconds()
    diff := math.Abs(float64(newETA) - oldETA)

    if diff > 300 {  // 5 分鐘
        p.notifyCustomerETAChange(ctx, order, newDeliveryTime)
    }

    return nil
}
```

---

## Act 6: 即時追蹤與通知

**場景**：顧客在 App 上看到外送員即時位置，並收到「外送員已到達餐廳」的推播...

### 6.1 WebSocket 即時追蹤

```go
// internal/tracking/service.go
package tracking

type TrackingService struct {
    wsHub *WebSocketHub
    redis *RedisClient
}

// TrackOrder 追蹤訂單（WebSocket 連線）
func (t *TrackingService) TrackOrder(ctx context.Context, customerID, orderID int64, conn *websocket.Conn) {
    // 1. 註冊連線
    t.wsHub.Register(&Connection{
        UserID:   customerID,
        UserType: "customer",
        OrderID:  orderID,
        Conn:     conn,
    })

    // 2. 定期推送外送員位置（每 5 秒）
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            // 取得外送員當前位置
            location, err := t.getDriverLocation(ctx, orderID)
            if err != nil {
                continue
            }

            // 推送給顧客
            update := &LocationUpdate{
                Type:      "driver_location",
                Latitude:  location.Lat,
                Longitude: location.Lng,
                Timestamp: time.Now(),
            }

            conn.WriteJSON(update)

        case <-ctx.Done():
            return
        }
    }
}

// NotifyOrderStatusChange 通知訂單狀態變化
func (t *TrackingService) NotifyOrderStatusChange(ctx context.Context, order *Order) {
    // 根據狀態發送不同的通知
    var message string

    switch order.Status {
    case StatusRestaurantConfirmed:
        message = "餐廳已確認您的訂單，正在準備中"

    case StatusDriverAssigned:
        message = "外送員已接單，正在前往餐廳取餐"

    case StatusPickedUp:
        message = "外送員已取餐，正在配送中"

    case StatusNearby:
        message = "外送員即將到達，請準備取餐"

    case StatusDelivered:
        message = "訂單已送達，請享用美食！"
    }

    // 發送推播通知
    t.sendPushNotification(ctx, order.CustomerID, message)

    // 發送 WebSocket 通知
    t.wsHub.BroadcastToCustomer(order.CustomerID, map[string]interface{}{
        "type":    "status_change",
        "order_id": order.ID,
        "status":  order.Status,
        "message": message,
    })
}
```

---

## Act 7: 動態定價與成本優化

**場景**：中午 12 點，訂單量激增，外送員供不應求...

### 7.1 動態外送費

```go
// internal/pricing/surge.go
package pricing

type DeliveryPricing struct {
    BaseFee      float64  // 基礎外送費
    DistanceFee  float64  // 距離費（每公里）
    SurgeMultiplier float64  // 尖峰加價倍數
    MinFee       float64  // 最低外送費
    MaxFee       float64  // 最高外送費
}

// CalculateDeliveryFee 計算外送費
func (p *PricingService) CalculateDeliveryFee(ctx context.Context, order *Order) float64 {
    // 1. 基礎費用
    baseFee := 30.0  // NT$30

    // 2. 距離費用
    distance := calculateDistance(
        order.RestaurantLat, order.RestaurantLng,
        order.DeliveryLat, order.DeliveryLng,
    )
    distanceFee := distance * 10.0  // 每公里 NT$10

    // 3. 計算 Surge 倍數
    surge := p.calculateSurge(ctx, order)

    // 4. 總費用
    totalFee := (baseFee + distanceFee) * surge

    // 5. 限制範圍
    totalFee = math.Max(30, math.Min(totalFee, 150))

    return math.Round(totalFee)
}

// calculateSurge 計算尖峰倍數
func (p *PricingService) calculateSurge(ctx context.Context, order *Order) float64 {
    // 統計該區域的供需
    supply := p.countAvailableDrivers(ctx, order.RestaurantLat, order.RestaurantLng, 2.0)
    demand := p.countPendingOrders(ctx, order.RestaurantLat, order.RestaurantLng, 2.0)

    supplyDemandRatio := float64(supply) / math.Max(float64(demand), 1.0)

    var surge float64
    switch {
    case supplyDemandRatio >= 1.0:
        surge = 1.0  // 供給充足
    case supplyDemandRatio >= 0.7:
        surge = 1.2
    case supplyDemandRatio >= 0.5:
        surge = 1.5
    case supplyDemandRatio >= 0.3:
        surge = 1.8
    default:
        surge = 2.0  // 最高 2 倍
    }

    return surge
}
```

### 7.2 外送員收入計算

```go
// CalculateDriverEarning 計算外送員收入
func (p *PricingService) CalculateDriverEarning(ctx context.Context, order *Order) float64 {
    // 平台抽成 20%
    platformFee := order.DeliveryFee * 0.20
    driverEarning := order.DeliveryFee - platformFee

    // 加上小費（如果有）
    if order.Tip > 0 {
        driverEarning += order.Tip
    }

    return driverEarning
}
```

---

## 總結

### 核心技術要點

1. **訂單狀態機**
   - 12 種狀態
   - 嚴格的狀態轉換驗證
   - 狀態變化觸發業務邏輯

2. **外送員匹配**
   - 多因素評分（6 個維度）
   - 分散式鎖防重複派單
   - 接單率、評分考量

3. **多訂單打包**
   - 最多同時 3 單
   - 繞路距離 < 1 km
   - 額外延遲 < 10 分鐘

4. **路線優化**
   - TSP 問題
   - 貪婪 + 2-opt 算法
   - 時間窗口約束

5. **ETA 預測**
   - 準備時間（ML 模型）
   - 取餐時間（A* 算法）
   - 配送時間（考慮路況）
   - 即時動態更新

6. **動態定價**
   - 供需比計算
   - Surge 最高 2 倍
   - 平台抽成 20%

### 延伸思考

**Emma**：如果要支援「預約外送」（指定送達時間），要怎麼設計？

**Michael**：需要：
- **時間槽管理**：預先分配外送員時間
- **提前調度**：在預約時間前安排取餐
- **優先級排序**：預約訂單優先於即時訂單

這是另一個有趣的挑戰！

---

**Phase 5: Location-Based Services 完成！** 🎉
**下一個 Phase 預告**：Phase 6: E-Commerce（電商交易）- Flash Sale、Payment System、Stock Exchange
