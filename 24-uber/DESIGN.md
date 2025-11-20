# Chapter 24: Uber/Lyft - 叫車平台系統設計

## 系統概述

Uber 和 Lyft 是全球領先的叫車平台，需要處理即時定位追蹤、司機乘客配對、路徑規劃、動態定價等複雜挑戰。本章將深入探討如何設計一個高效能、高可用的叫車平台系統。

**核心挑戰**：
- 即時位置追蹤（每秒處理百萬級 GPS 更新）
- 地理空間索引（快速找到附近的司機）
- 智能配對算法（最佳司機乘客匹配）
- 精確的 ETA 計算
- 動態定價（尖峰時段加價）
- 高併發處理（同時處理數百萬次叫車請求）

---

## Act 1: 即時定位追蹤系統

**場景**：週五晚上 8 點，台北市中心有 10,000 名司機在線上，每位司機每 4 秒回報一次 GPS 位置...

### 1.1 對話：Emma 與 David 討論位置追蹤

**Emma**（產品經理）：我們的 App 需要即時顯示附近的司機，乘客打開 App 就能看到周圍有多少車。這個功能要怎麼實現？

**David**（後端工程師）：這涉及兩個關鍵問題：
1. **司機端**：如何高效地將 GPS 位置持續回報給伺服器？
2. **乘客端**：如何快速查詢附近的司機並即時更新？

讓我先處理第一個問題。

### 1.2 GPS 位置回報策略

**David**：司機 App 需要持續回報位置，但頻率不能太高（耗電、耗流量），也不能太低（位置不準確）。

```go
// internal/location/tracker.go
package location

import (
    "context"
    "time"
    "github.com/gorilla/websocket"
)

type LocationUpdate struct {
    DriverID  int64     `json:"driver_id"`
    Latitude  float64   `json:"latitude"`
    Longitude float64   `json:"longitude"`
    Bearing   float64   `json:"bearing"`    // 行駛方向（0-360度）
    Speed     float64   `json:"speed"`      // km/h
    Timestamp time.Time `json:"timestamp"`
}

// LocationTracker 追蹤司機位置
type LocationTracker struct {
    redisClient *redis.Client
    wsHub       *WebSocketHub
}

// UpdateLocation 更新司機位置（通過 WebSocket）
func (t *LocationTracker) UpdateLocation(ctx context.Context, update *LocationUpdate) error {
    // 1. 儲存到 Redis（使用 Geo 資料結構）
    key := "drivers:online"
    err := t.redisClient.GeoAdd(ctx, key, &redis.GeoLocation{
        Name:      fmt.Sprintf("driver:%d", update.DriverID),
        Longitude: update.Longitude,
        Latitude:  update.Latitude,
    }).Err()

    if err != nil {
        return fmt.Errorf("failed to update geo location: %w", err)
    }

    // 2. 儲存詳細資訊（包含速度、方向）
    detailKey := fmt.Sprintf("driver:%d:location", update.DriverID)
    err = t.redisClient.HSet(ctx, detailKey, map[string]interface{}{
        "latitude":  update.Latitude,
        "longitude": update.Longitude,
        "bearing":   update.Bearing,
        "speed":     update.Speed,
        "timestamp": update.Timestamp.Unix(),
    }).Err()

    if err != nil {
        return fmt.Errorf("failed to update location details: %w", err)
    }

    // 3. 設定過期時間（30秒未更新視為離線）
    t.redisClient.Expire(ctx, detailKey, 30*time.Second)

    // 4. 通知附近的乘客（透過 WebSocket 推送）
    go t.notifyNearbyRiders(update)

    return nil
}
```

**Sarah**（前端工程師）：為什麼用 WebSocket 而不是 HTTP 輪詢？

**David**：
- **HTTP 輪詢**：每 4 秒發一次請求 → 10,000 司機 = 每秒 2,500 個請求
- **WebSocket**：建立一次連線，持續雙向通訊 → 只需維護 10,000 個連線

WebSocket 大幅降低伺服器負載和網路流量。

### 1.3 WebSocket 連線管理

```go
// internal/location/websocket.go
package location

import (
    "sync"
    "github.com/gorilla/websocket"
)

type WebSocketHub struct {
    // 司機連線池
    driverConns map[int64]*websocket.Conn
    driverMu    sync.RWMutex

    // 乘客連線池
    riderConns  map[int64]*websocket.Conn
    riderMu     sync.RWMutex

    // 註冊/登出通道
    register   chan *Connection
    unregister chan *Connection
}

type Connection struct {
    UserID   int64
    UserType string // "driver" or "rider"
    Conn     *websocket.Conn
}

func (h *WebSocketHub) Run() {
    for {
        select {
        case conn := <-h.register:
            if conn.UserType == "driver" {
                h.driverMu.Lock()
                h.driverConns[conn.UserID] = conn.Conn
                h.driverMu.Unlock()
            } else {
                h.riderMu.Lock()
                h.riderConns[conn.UserID] = conn.Conn
                h.riderMu.Unlock()
            }

        case conn := <-h.unregister:
            if conn.UserType == "driver" {
                h.driverMu.Lock()
                delete(h.driverConns, conn.UserID)
                h.driverMu.Unlock()
            } else {
                h.riderMu.Lock()
                delete(h.riderConns, conn.UserID)
                h.riderMu.Unlock()
            }
            conn.Conn.Close()
        }
    }
}

// BroadcastToRider 推送司機位置給特定乘客
func (h *WebSocketHub) BroadcastToRider(riderID int64, message interface{}) error {
    h.riderMu.RLock()
    conn, exists := h.riderConns[riderID]
    h.riderMu.RUnlock()

    if !exists {
        return fmt.Errorf("rider %d not connected", riderID)
    }

    return conn.WriteJSON(message)
}
```

---

## Act 2: 地理空間索引 - Geohash vs QuadTree vs S2

**場景**：乘客在台北車站叫車，系統需要在 1 公里內找到所有可用的司機...

### 2.1 對話：Michael 加入討論

**Michael**（資深架構師）：現在我們有 10,000 名司機的 GPS 座標，當乘客叫車時，要如何快速找到「附近 1 公里內的司機」？

**Emma**：遍歷所有司機，計算距離？

**Michael**：那太慢了！10,000 次距離計算，每次計算涉及開根號... 這樣會拖垮系統。我們需要**地理空間索引**（Geospatial Index）。

### 2.2 方案一：Geohash

**Michael**：Geohash 將地球劃分成網格，每個網格用一個字串編碼。

```go
// internal/geo/geohash.go
package geo

import (
    "github.com/mmcloughlin/geohash"
)

// GeohashIndex 使用 Geohash 的索引
type GeohashIndex struct {
    precision int // Geohash 精度（字元數）
}

// EncodeLocation 將經緯度編碼為 Geohash
func (g *GeohashIndex) EncodeLocation(lat, lon float64) string {
    // precision=6 約 1.2 km × 0.6 km
    return geohash.EncodeWithPrecision(lat, lon, uint(g.precision))
}

// Example:
// 台北車站: (25.0478, 121.5170)
// Geohash(6): "wsqqkh"
//
// 附近的 Geohash:
// wsqqkh (中心)
// wsqqkj (東)
// wsqqk5 (西)
// wsqqku (北)
// wsqqks (南)
```

**David**：所以查詢流程是？

**Michael**：
1. 計算乘客位置的 Geohash：`wsqqkh`
2. 計算周圍 8 個網格的 Geohash
3. 在 Redis 中查詢這 9 個網格內的所有司機

```go
// FindNearbyDrivers 查詢附近司機（Geohash 方法）
func (g *GeohashIndex) FindNearbyDrivers(ctx context.Context, lat, lon float64, radiusKm float64) ([]int64, error) {
    // 1. 計算中心點的 Geohash
    centerHash := g.EncodeLocation(lat, lon)

    // 2. 計算周圍的 Geohash（9個網格）
    neighbors := geohash.Neighbors(centerHash)
    searchHashes := append(neighbors, centerHash)

    // 3. 從 Redis 查詢每個網格內的司機
    var driverIDs []int64
    for _, hash := range searchHashes {
        key := fmt.Sprintf("drivers:geohash:%s", hash)
        members, err := g.redisClient.SMembers(ctx, key).Result()
        if err != nil {
            continue
        }

        for _, member := range members {
            var driverID int64
            fmt.Sscanf(member, "driver:%d", &driverID)
            driverIDs = append(driverIDs, driverID)
        }
    }

    // 4. 精確過濾（計算實際距離）
    return g.filterByDistance(ctx, lat, lon, driverIDs, radiusKm)
}
```

### 2.3 方案二：QuadTree

**Sarah**：Geohash 有什麼缺點嗎？

**Michael**：有的！**邊界問題**。如果乘客在網格邊緣，最近的司機可能在隔壁網格，但那個網格的 Geohash 差異很大。

QuadTree 是另一個選擇：

```go
// internal/geo/quadtree.go
package geo

type QuadTree struct {
    boundary  Rectangle  // 當前節點覆蓋的範圍
    capacity  int        // 每個節點最多存幾個點
    points    []Point    // 當前節點的點
    divided   bool       // 是否已分裂

    // 四個子節點
    northwest *QuadTree
    northeast *QuadTree
    southwest *QuadTree
    southeast *QuadTree
}

type Point struct {
    DriverID  int64
    Latitude  float64
    Longitude float64
}

type Rectangle struct {
    CenterLat float64
    CenterLon float64
    HalfWidth float64  // 緯度範圍的一半
    HalfHeight float64 // 經度範圍的一半
}

// Insert 插入一個司機位置
func (qt *QuadTree) Insert(point Point) bool {
    // 1. 檢查點是否在當前範圍內
    if !qt.boundary.Contains(point) {
        return false
    }

    // 2. 如果未達容量上限，直接插入
    if len(qt.points) < qt.capacity {
        qt.points = append(qt.points, point)
        return true
    }

    // 3. 已達上限，分裂成四個子節點
    if !qt.divided {
        qt.subdivide()
    }

    // 4. 遞迴插入到子節點
    if qt.northwest.Insert(point) { return true }
    if qt.northeast.Insert(point) { return true }
    if qt.southwest.Insert(point) { return true }
    if qt.southeast.Insert(point) { return true }

    return false
}

// Query 查詢範圍內的所有點
func (qt *QuadTree) Query(searchRange Rectangle) []Point {
    var found []Point

    // 1. 如果搜尋範圍與當前範圍不相交，直接返回
    if !qt.boundary.Intersects(searchRange) {
        return found
    }

    // 2. 檢查當前節點的所有點
    for _, p := range qt.points {
        if searchRange.Contains(p) {
            found = append(found, p)
        }
    }

    // 3. 遞迴搜尋子節點
    if qt.divided {
        found = append(found, qt.northwest.Query(searchRange)...)
        found = append(found, qt.northeast.Query(searchRange)...)
        found = append(found, qt.southwest.Query(searchRange)...)
        found = append(found, qt.southeast.Query(searchRange)...)
    }

    return found
}
```

### 2.4 方案三：S2 Geometry（Google 方案）

**Michael**：Google 內部使用的是 **S2 Geometry**，它將地球投影到一個立方體上，再遞迴劃分成六邊形網格。

```go
// internal/geo/s2.go
package geo

import (
    "github.com/golang/geo/s2"
)

type S2Index struct {
    level int // S2 Cell 層級（0-30）
}

// GetCellID 將經緯度轉換為 S2 CellID
func (s *S2Index) GetCellID(lat, lon float64) s2.CellID {
    latLng := s2.LatLngFromDegrees(lat, lon)
    cellID := s2.CellIDFromLatLng(latLng)
    return cellID.Parent(s.level) // 使用指定層級
}

// FindNearbyDrivers 查詢附近司機（S2 方法）
func (s *S2Index) FindNearbyDrivers(ctx context.Context, lat, lon float64, radiusKm float64) ([]int64, error) {
    // 1. 建立搜尋圓形區域
    center := s2.PointFromLatLng(s2.LatLngFromDegrees(lat, lon))
    radiusAngle := s2.Angle(radiusKm / 6371.0) // 地球半徑 6371 km
    cap := s2.CapFromCenterAngle(center, radiusAngle)

    // 2. 找出覆蓋這個圓形的所有 S2 Cells
    rc := &s2.RegionCoverer{
        MaxLevel: s.level,
        MaxCells: 8,
    }
    covering := rc.Covering(cap)

    // 3. 查詢每個 Cell 內的司機
    var driverIDs []int64
    for _, cellID := range covering {
        key := fmt.Sprintf("drivers:s2:%d", uint64(cellID))
        members, err := s.redisClient.SMembers(ctx, key).Result()
        if err != nil {
            continue
        }

        for _, member := range members {
            var driverID int64
            fmt.Sscanf(member, "driver:%d", &driverID)
            driverIDs = append(driverIDs, driverID)
        }
    }

    return driverIDs, nil
}
```

### 2.5 三種方案比較

**Emma**：哪一種最好？

**Michael**：

| 方案 | 優點 | 缺點 | 適用場景 |
|------|------|------|----------|
| **Geohash** | 簡單易懂、Redis 原生支援 | 邊界問題、網格固定 | 中小規模、精度要求不高 |
| **QuadTree** | 動態分裂、無邊界問題 | 記憶體開銷大、需自己實作 | 記憶體充足、需動態調整 |
| **S2** | 精度高、球面幾何準確 | 複雜度高、學習曲線陡 | 全球化應用、精度要求高 |

**結論**：我們選擇 **Redis Geo + Geohash**（簡單高效），配合精確距離過濾解決邊界問題。

---

## Act 3: 司機乘客智能配對

**場景**：台北車站有 5 名乘客同時叫車，附近有 20 名司機，如何做最佳配對？

### 3.1 對話：配對算法的挑戰

**Emma**：找到附近的司機後，要怎麼決定派哪一台車？

**David**：這不是簡單的「最近距離」問題，我們需要考慮：
1. **距離**：司機到乘客的距離
2. **ETA**：預估到達時間（考慮路況）
3. **司機評分**：優先派高評分司機
4. **接單率**：避免派給常拒單的司機
5. **方向匹配**：司機當前行駛方向是否朝向乘客

### 3.2 配對演算法

```go
// internal/matching/matcher.go
package matching

import (
    "context"
    "math"
    "sort"
)

type MatchRequest struct {
    RiderID   int64
    Latitude  float64
    Longitude float64
    Timestamp time.Time
}

type DriverCandidate struct {
    DriverID       int64
    Latitude       float64
    Longitude      float64
    Rating         float64  // 0-5 星
    AcceptanceRate float64  // 接單率 0-1
    Bearing        float64  // 行駛方向
    Distance       float64  // 到乘客的距離（km）
    ETA            int      // 預估到達時間（秒）
    Score          float64  // 綜合評分
}

type Matcher struct {
    geoIndex  *geo.S2Index
    etaCalc   *routing.ETACalculator
    redisClient *redis.Client
}

// FindBestDriver 找出最佳司機
func (m *Matcher) FindBestDriver(ctx context.Context, req *MatchRequest) (*DriverCandidate, error) {
    // 1. 找出附近 3 公里內的司機
    nearbyDrivers, err := m.geoIndex.FindNearbyDrivers(ctx, req.Latitude, req.Longitude, 3.0)
    if err != nil {
        return nil, err
    }

    if len(nearbyDrivers) == 0 {
        return nil, fmt.Errorf("no drivers available")
    }

    // 2. 取得每個司機的詳細資訊
    candidates := make([]*DriverCandidate, 0, len(nearbyDrivers))
    for _, driverID := range nearbyDrivers {
        driver, err := m.getDriverInfo(ctx, driverID)
        if err != nil {
            continue
        }

        // 檢查司機狀態（必須是 online 且 available）
        if driver.Status != "available" {
            continue
        }

        // 計算距離
        driver.Distance = m.calculateDistance(
            req.Latitude, req.Longitude,
            driver.Latitude, driver.Longitude,
        )

        // 計算 ETA
        driver.ETA, _ = m.etaCalc.Calculate(ctx,
            driver.Latitude, driver.Longitude,
            req.Latitude, req.Longitude,
        )

        candidates = append(candidates, driver)
    }

    // 3. 計算每個司機的綜合評分
    for _, driver := range candidates {
        driver.Score = m.calculateScore(driver, req)
    }

    // 4. 排序並選擇最佳司機
    sort.Slice(candidates, func(i, j int) bool {
        return candidates[i].Score > candidates[j].Score
    })

    return candidates[0], nil
}

// calculateScore 計算司機評分（多因素加權）
func (m *Matcher) calculateScore(driver *DriverCandidate, req *MatchRequest) float64 {
    // 權重配置
    const (
        distanceWeight     = 0.35  // 距離權重
        etaWeight          = 0.25  // ETA 權重
        ratingWeight       = 0.20  // 評分權重
        acceptanceWeight   = 0.15  // 接單率權重
        directionWeight    = 0.05  // 方向權重
    )

    // 1. 距離分數（越近越好，3km 為基準）
    distanceScore := math.Max(0, 1 - driver.Distance/3.0)

    // 2. ETA 分數（越快越好，10分鐘為基準）
    etaScore := math.Max(0, 1 - float64(driver.ETA)/600.0)

    // 3. 評分分數（5星制，歸一化到 0-1）
    ratingScore := driver.Rating / 5.0

    // 4. 接單率分數
    acceptanceScore := driver.AcceptanceRate

    // 5. 方向分數（司機朝向乘客得高分）
    directionScore := m.calculateDirectionScore(driver, req)

    // 加權總分
    totalScore := distanceScore*distanceWeight +
                  etaScore*etaWeight +
                  ratingScore*ratingWeight +
                  acceptanceScore*acceptanceWeight +
                  directionScore*directionWeight

    return totalScore
}

// calculateDirectionScore 計算方向匹配分數
func (m *Matcher) calculateDirectionScore(driver *DriverCandidate, req *MatchRequest) float64 {
    // 計算司機到乘客的方位角
    targetBearing := m.calculateBearing(
        driver.Latitude, driver.Longitude,
        req.Latitude, req.Longitude,
    )

    // 計算角度差異（0-180度）
    diff := math.Abs(driver.Bearing - targetBearing)
    if diff > 180 {
        diff = 360 - diff
    }

    // 角度差異越小，分數越高
    // 0度（正對）= 1.0，180度（反向）= 0.0
    return 1.0 - (diff / 180.0)
}
```

### 3.3 防止重複配對

**Sarah**：如果同時有多個乘客叫車，會不會派同一個司機給兩個人？

**David**：好問題！我們需要用**分散式鎖**防止重複配對。

```go
// RequestRide 發送叫車請求（帶鎖機制）
func (m *Matcher) RequestRide(ctx context.Context, req *MatchRequest) (*Trip, error) {
    const maxRetries = 3

    for i := 0; i < maxRetries; i++ {
        // 1. 找出最佳司機
        bestDriver, err := m.FindBestDriver(ctx, req)
        if err != nil {
            return nil, err
        }

        // 2. 嘗試鎖定這個司機（Redis SETNX）
        lockKey := fmt.Sprintf("driver:%d:lock", bestDriver.DriverID)
        locked, err := m.redisClient.SetNX(ctx, lockKey, req.RiderID, 30*time.Second).Result()

        if err != nil {
            return nil, err
        }

        if !locked {
            // 司機已被其他乘客鎖定，重試
            time.Sleep(100 * time.Millisecond)
            continue
        }

        // 3. 成功鎖定，發送派車通知給司機
        err = m.sendDispatchNotification(ctx, bestDriver.DriverID, req)
        if err != nil {
            // 發送失敗，釋放鎖
            m.redisClient.Del(ctx, lockKey)
            return nil, err
        }

        // 4. 建立行程記錄
        trip := &Trip{
            RiderID:  req.RiderID,
            DriverID: bestDriver.DriverID,
            Status:   "dispatched",
            RequestTime: req.Timestamp,
            PickupLat: req.Latitude,
            PickupLon: req.Longitude,
        }

        err = m.saveTripToDB(ctx, trip)
        if err != nil {
            m.redisClient.Del(ctx, lockKey)
            return nil, err
        }

        return trip, nil
    }

    return nil, fmt.Errorf("failed to match driver after %d retries", maxRetries)
}
```

---

## Act 4: 路徑規劃與 ETA 計算

**場景**：司機接到訂單，系統需要計算「多久能到達乘客位置」...

### 4.1 對話：ETA 的重要性

**Emma**：App 上顯示「司機 3 分鐘後到達」，這個時間是怎麼算出來的？

**Michael**：這是**預估到達時間**（ETA, Estimated Time of Arrival）。計算 ETA 需要考慮：
- 實際路徑（不是直線距離）
- 即時路況（塞車會延長時間）
- 轉彎、紅綠燈等因素

### 4.2 使用第三方 API（Google Maps / Mapbox）

```go
// internal/routing/eta.go
package routing

import (
    "context"
    "googlemaps.github.io/maps"
)

type ETACalculator struct {
    mapsClient *maps.Client
    cache      *redis.Client
}

// Calculate 計算從起點到終點的 ETA
func (e *ETACalculator) Calculate(ctx context.Context, fromLat, fromLon, toLat, toLon float64) (int, error) {
    // 1. 檢查快取（相同起終點在 5 分鐘內快取）
    cacheKey := fmt.Sprintf("eta:%f,%f:%f,%f", fromLat, fromLon, toLat, toLon)
    cached, err := e.cache.Get(ctx, cacheKey).Int()
    if err == nil {
        return cached, nil
    }

    // 2. 呼叫 Google Maps Directions API
    req := &maps.DirectionsRequest{
        Origin:      fmt.Sprintf("%f,%f", fromLat, fromLon),
        Destination: fmt.Sprintf("%f,%f", toLat, toLon),
        Mode:        maps.TravelModeDriving,
        DepartureTime: "now", // 考慮即時路況
    }

    routes, _, err := e.mapsClient.Directions(ctx, req)
    if err != nil {
        return 0, fmt.Errorf("failed to get directions: %w", err)
    }

    if len(routes) == 0 {
        return 0, fmt.Errorf("no route found")
    }

    // 3. 取得第一條路線的時間（秒）
    duration := int(routes[0].Legs[0].DurationInTraffic.Seconds())

    // 4. 快取結果（5分鐘）
    e.cache.Set(ctx, cacheKey, duration, 5*time.Minute)

    return duration, nil
}
```

### 4.3 自建路徑規劃引擎（降低成本）

**David**：Google Maps API 很貴！每次查詢 ETA 都要錢，一天百萬次查詢成本很高。

**Michael**：對！成熟的公司會自建路徑引擎。我們可以用 **OSM（OpenStreetMap）數據 + A\* 演算法**。

```go
// internal/routing/astar.go
package routing

import (
    "container/heap"
    "math"
)

type Node struct {
    ID       int64
    Lat      float64
    Lon      float64
    Edges    []*Edge // 連接的道路
}

type Edge struct {
    To       *Node
    Distance float64  // 公尺
    SpeedLimit float64 // km/h
}

type AStarRouter struct {
    graph map[int64]*Node // 路網圖
}

// FindRoute 使用 A* 演算法找路徑
func (r *AStarRouter) FindRoute(startID, endID int64) ([]*Node, float64, error) {
    start := r.graph[startID]
    end := r.graph[endID]

    if start == nil || end == nil {
        return nil, 0, fmt.Errorf("invalid node")
    }

    // A* 需要的資料結構
    openSet := &PriorityQueue{}
    heap.Init(openSet)

    // 起點的 g 值（實際代價）為 0
    gScore := map[int64]float64{startID: 0}

    // 起點的 f 值（g + h）
    fScore := map[int64]float64{
        startID: r.heuristic(start, end),
    }

    // 起點加入 open set
    heap.Push(openSet, &Item{
        node:     start,
        priority: fScore[startID],
    })

    // 記錄路徑
    cameFrom := make(map[int64]*Node)

    for openSet.Len() > 0 {
        // 取出 f 值最小的節點
        current := heap.Pop(openSet).(*Item).node

        // 到達終點
        if current.ID == endID {
            path := r.reconstructPath(cameFrom, current)
            distance := gScore[endID]
            return path, distance, nil
        }

        // 檢查所有鄰居
        for _, edge := range current.Edges {
            neighbor := edge.To

            // 計算從起點到鄰居的代價
            tentativeG := gScore[current.ID] + edge.Distance

            // 如果找到更好的路徑
            if oldG, exists := gScore[neighbor.ID]; !exists || tentativeG < oldG {
                cameFrom[neighbor.ID] = current
                gScore[neighbor.ID] = tentativeG
                fScore[neighbor.ID] = tentativeG + r.heuristic(neighbor, end)

                heap.Push(openSet, &Item{
                    node:     neighbor,
                    priority: fScore[neighbor.ID],
                })
            }
        }
    }

    return nil, 0, fmt.Errorf("no route found")
}

// heuristic 啟發式函數（歐氏距離）
func (r *AStarRouter) heuristic(a, b *Node) float64 {
    return calculateDistance(a.Lat, a.Lon, b.Lat, b.Lon) * 1000 // 轉為公尺
}

// CalculateETA 根據路徑計算 ETA
func (r *AStarRouter) CalculateETA(route []*Node) int {
    totalTime := 0.0 // 秒

    for i := 0; i < len(route)-1; i++ {
        current := route[i]
        next := route[i+1]

        // 找到連接這兩個節點的邊
        var edge *Edge
        for _, e := range current.Edges {
            if e.To.ID == next.ID {
                edge = e
                break
            }
        }

        if edge == nil {
            continue
        }

        // 時間 = 距離 / 速度
        speed := edge.SpeedLimit * 1000 / 3600 // 轉為 m/s
        time := edge.Distance / speed

        totalTime += time
    }

    return int(totalTime)
}
```

### 4.4 即時路況整合

**Sarah**：自建引擎怎麼處理塞車？

**Michael**：我們需要**即時路況數據**：

```go
// internal/routing/traffic.go
package routing

type TrafficData struct {
    EdgeID   int64
    Speed    float64   // 當前實際速度 km/h
    Congestion string  // "free", "moderate", "heavy", "severe"
    UpdateTime time.Time
}

// UpdateTrafficData 更新路況（從司機 GPS 推算）
func (r *AStarRouter) UpdateTrafficData(ctx context.Context, updates []*TrafficData) error {
    for _, data := range updates {
        key := fmt.Sprintf("traffic:edge:%d", data.EdgeID)

        // 儲存到 Redis（5 分鐘過期）
        err := r.redisClient.HSet(ctx, key, map[string]interface{}{
            "speed":      data.Speed,
            "congestion": data.Congestion,
            "updated_at": data.UpdateTime.Unix(),
        }).Err()

        if err != nil {
            return err
        }

        r.redisClient.Expire(ctx, key, 5*time.Minute)
    }

    return nil
}

// GetEdgeSpeed 取得道路當前速度
func (r *AStarRouter) GetEdgeSpeed(ctx context.Context, edgeID int64, defaultSpeed float64) float64 {
    key := fmt.Sprintf("traffic:edge:%d", edgeID)

    speed, err := r.redisClient.HGet(ctx, key, "speed").Float64()
    if err != nil {
        // 沒有即時數據，使用預設速限
        return defaultSpeed
    }

    return speed
}
```

---

## Act 5: 動態定價（Surge Pricing）

**場景**：週五晚上 9 點，台北信義區有 100 個叫車請求，但只有 30 個司機...

### 5.1 對話：供需失衡

**Emma**：尖峰時段叫車很難，要等很久。能不能用「加價」吸引更多司機？

**Michael**：這就是 Uber 的**動態定價**（Surge Pricing）！當需求 > 供給時，價格自動上漲 1.5x、2x、甚至 3x。

### 5.2 Surge 計算邏輯

```go
// internal/pricing/surge.go
package pricing

import (
    "context"
    "math"
)

type SurgeCalculator struct {
    geoIndex *geo.S2Index
    redis    *redis.Client
}

type SurgeMultiplier struct {
    Region     string  // 區域（S2 CellID）
    Multiplier float64 // 加價倍數（1.0 = 原價）
    UpdateTime time.Time
}

// CalculateSurge 計算某區域的加價倍數
func (s *SurgeCalculator) CalculateSurge(ctx context.Context, lat, lon float64) (float64, error) {
    // 1. 確定區域（使用 S2 Cell level 13，約 1km²）
    cellID := s.geoIndex.GetCellID(lat, lon)
    regionKey := fmt.Sprintf("region:%d", uint64(cellID))

    // 2. 統計該區域的供需狀況（過去 5 分鐘）
    supply := s.countAvailableDrivers(ctx, cellID)   // 可用司機數
    demand := s.countPendingRequests(ctx, cellID)    // 待配對的叫車數

    // 3. 計算供需比
    supplyDemandRatio := float64(supply) / math.Max(float64(demand), 1.0)

    // 4. 根據供需比計算 Surge 倍數
    var multiplier float64

    switch {
    case supplyDemandRatio >= 1.0:
        // 供給充足，原價
        multiplier = 1.0

    case supplyDemandRatio >= 0.7:
        // 稍微緊張，加價 1.2x
        multiplier = 1.2

    case supplyDemandRatio >= 0.5:
        // 供不應求，加價 1.5x
        multiplier = 1.5

    case supplyDemandRatio >= 0.3:
        // 嚴重短缺，加價 2.0x
        multiplier = 2.0

    default:
        // 極度短缺，加價 3.0x（設上限）
        multiplier = 3.0
    }

    // 5. 平滑處理（避免劇烈波動）
    oldMultiplier := s.getOldMultiplier(ctx, regionKey)
    smoothed := s.smoothMultiplier(oldMultiplier, multiplier)

    // 6. 儲存到 Redis
    surge := &SurgeMultiplier{
        Region:     regionKey,
        Multiplier: smoothed,
        UpdateTime: time.Now(),
    }

    s.saveSurge(ctx, surge)

    return smoothed, nil
}

// smoothMultiplier 平滑 Surge 變化（避免突然跳動）
func (s *SurgeCalculator) smoothMultiplier(old, new float64) float64 {
    // 使用指數移動平均（EMA）
    alpha := 0.3 // 權重
    return alpha*new + (1-alpha)*old
}

// countPendingRequests 統計待配對的叫車數
func (s *SurgeCalculator) countPendingRequests(ctx context.Context, cellID s2.CellID) int {
    key := fmt.Sprintf("pending_requests:%d", uint64(cellID))

    // 從 Redis Sorted Set 取得過去 5 分鐘的請求
    now := time.Now().Unix()
    fiveMinutesAgo := now - 300

    count, err := s.redis.ZCount(ctx, key,
        fmt.Sprintf("%d", fiveMinutesAgo),
        fmt.Sprintf("%d", now),
    ).Result()

    if err != nil {
        return 0
    }

    return int(count)
}
```

### 5.3 價格計算

```go
// internal/pricing/calculator.go
package pricing

type PriceCalculator struct {
    surgeCalc *SurgeCalculator
}

type PriceEstimate struct {
    BasePrice    float64 `json:"base_price"`     // 基礎價格
    Distance     float64 `json:"distance"`       // 距離（km）
    Duration     int     `json:"duration"`       // 時間（分鐘）
    Surge        float64 `json:"surge"`          // Surge 倍數
    FinalPrice   float64 `json:"final_price"`    // 最終價格
    Currency     string  `json:"currency"`
}

// EstimatePrice 估算行程價格
func (p *PriceCalculator) EstimatePrice(ctx context.Context, fromLat, fromLon, toLat, toLon float64) (*PriceEstimate, error) {
    // 1. 計算距離和時間
    distance := calculateDistance(fromLat, fromLon, toLat, toLon)
    duration := p.estimateDuration(distance) // 簡化：假設平均 30 km/h

    // 2. 基礎價格計算
    const (
        baseFare       = 70.0  // 起跳價 NT$70
        perKm          = 15.0  // 每公里 NT$15
        perMinute      = 2.5   // 每分鐘 NT$2.5
        minimumFare    = 85.0  // 最低收費 NT$85
    )

    basePrice := baseFare + (distance * perKm) + (float64(duration) * perMinute)

    if basePrice < minimumFare {
        basePrice = minimumFare
    }

    // 3. 計算 Surge 倍數
    surge, err := p.surgeCalc.CalculateSurge(ctx, fromLat, fromLon)
    if err != nil {
        surge = 1.0 // 預設原價
    }

    // 4. 最終價格
    finalPrice := basePrice * surge

    return &PriceEstimate{
        BasePrice:  basePrice,
        Distance:   distance,
        Duration:   duration,
        Surge:      surge,
        FinalPrice: math.Round(finalPrice),
        Currency:   "TWD",
    }, nil
}
```

---

## Act 6: 行程追蹤與支付

**場景**：司機接到乘客，開始行程，系統需要即時追蹤位置並在結束時自動扣款...

### 6.1 行程狀態機

```go
// internal/trip/state.go
package trip

type TripStatus string

const (
    StatusRequested   TripStatus = "requested"    // 乘客發送請求
    StatusDispatched  TripStatus = "dispatched"   // 已派車
    StatusAccepted    TripStatus = "accepted"     // 司機接單
    StatusArriving    TripStatus = "arriving"     // 司機前往接客
    StatusArrived     TripStatus = "arrived"      // 司機到達
    StatusInProgress  TripStatus = "in_progress"  // 行程中
    StatusCompleted   TripStatus = "completed"    // 已完成
    StatusCancelled   TripStatus = "cancelled"    // 已取消
)

type Trip struct {
    ID          int64      `json:"id"`
    RiderID     int64      `json:"rider_id"`
    DriverID    int64      `json:"driver_id"`
    Status      TripStatus `json:"status"`

    // 起點
    PickupLat   float64    `json:"pickup_lat"`
    PickupLon   float64    `json:"pickup_lon"`
    PickupAddr  string     `json:"pickup_addr"`

    // 終點
    DropoffLat  float64    `json:"dropoff_lat"`
    DropoffLon  float64    `json:"dropoff_lon"`
    DropoffAddr string     `json:"dropoff_addr"`

    // 時間戳
    RequestTime  time.Time  `json:"request_time"`
    AcceptTime   *time.Time `json:"accept_time"`
    PickupTime   *time.Time `json:"pickup_time"`
    DropoffTime  *time.Time `json:"dropoff_time"`

    // 價格
    EstimatedPrice float64  `json:"estimated_price"`
    FinalPrice     float64  `json:"final_price"`

    // 路徑追蹤
    Route       []Location `json:"route"`
}

type Location struct {
    Lat       float64   `json:"lat"`
    Lon       float64   `json:"lon"`
    Timestamp time.Time `json:"timestamp"`
}
```

### 6.2 即時位置追蹤

```go
// UpdateTripLocation 更新行程中的司機位置
func (t *TripService) UpdateTripLocation(ctx context.Context, tripID int64, lat, lon float64) error {
    // 1. 檢查行程狀態
    trip, err := t.getTripByID(ctx, tripID)
    if err != nil {
        return err
    }

    if trip.Status != StatusArriving && trip.Status != StatusInProgress {
        return fmt.Errorf("trip not active")
    }

    // 2. 儲存位置到 Redis（供即時查詢）
    locationKey := fmt.Sprintf("trip:%d:location", tripID)
    err = t.redis.GeoAdd(ctx, locationKey, &redis.GeoLocation{
        Name:      "current",
        Longitude: lon,
        Latitude:  lat,
    }).Err()

    if err != nil {
        return err
    }

    // 3. 記錄到行程路徑（每 10 秒記錄一次，用於後續分析）
    location := Location{
        Lat:       lat,
        Lon:       lon,
        Timestamp: time.Now(),
    }

    routeKey := fmt.Sprintf("trip:%d:route", tripID)
    data, _ := json.Marshal(location)
    t.redis.RPush(ctx, routeKey, data)

    // 4. 推送給乘客（WebSocket）
    t.wsHub.BroadcastToRider(trip.RiderID, map[string]interface{}{
        "type":      "location_update",
        "trip_id":   tripID,
        "latitude":  lat,
        "longitude": lon,
        "timestamp": time.Now(),
    })

    // 5. 檢查是否接近目的地（觸發「即將到達」通知）
    if trip.Status == StatusArriving {
        distance := calculateDistance(lat, lon, trip.PickupLat, trip.PickupLon)
        if distance < 0.1 { // 100公尺內
            t.notifyRiderDriverNearby(ctx, trip)
        }
    }

    return nil
}
```

### 6.3 支付整合

```go
// internal/payment/processor.go
package payment

type PaymentProcessor struct {
    stripeClient *stripe.Client
    db           *sql.DB
}

type Payment struct {
    ID              int64   `json:"id"`
    TripID          int64   `json:"trip_id"`
    RiderID         int64   `json:"rider_id"`
    Amount          float64 `json:"amount"`
    Currency        string  `json:"currency"`
    Method          string  `json:"method"` // "card", "cash", "wallet"
    Status          string  `json:"status"` // "pending", "completed", "failed"
    StripeChargeID  string  `json:"stripe_charge_id"`
    CreatedAt       time.Time `json:"created_at"`
}

// ProcessPayment 處理行程結束後的支付
func (p *PaymentProcessor) ProcessPayment(ctx context.Context, tripID int64) (*Payment, error) {
    // 1. 取得行程資訊
    trip, err := p.getTripByID(ctx, tripID)
    if err != nil {
        return nil, err
    }

    // 2. 取得乘客的預設支付方式
    rider, err := p.getRiderByID(ctx, trip.RiderID)
    if err != nil {
        return nil, err
    }

    // 3. 建立支付記錄
    payment := &Payment{
        TripID:   tripID,
        RiderID:  trip.RiderID,
        Amount:   trip.FinalPrice,
        Currency: "TWD",
        Method:   rider.DefaultPaymentMethod,
        Status:   "pending",
        CreatedAt: time.Now(),
    }

    // 4. 根據支付方式處理
    switch payment.Method {
    case "card":
        // 使用 Stripe 扣款
        err = p.chargeCard(ctx, rider.StripeCustomerID, payment.Amount)
        if err != nil {
            payment.Status = "failed"
            p.savePayment(ctx, payment)
            return nil, err
        }
        payment.Status = "completed"

    case "wallet":
        // 從錢包扣款
        err = p.deductWallet(ctx, trip.RiderID, payment.Amount)
        if err != nil {
            payment.Status = "failed"
            p.savePayment(ctx, payment)
            return nil, err
        }
        payment.Status = "completed"

    case "cash":
        // 現金支付（司機收取，標記為已完成）
        payment.Status = "completed"
    }

    // 5. 儲存支付記錄
    err = p.savePayment(ctx, payment)
    if err != nil {
        return nil, err
    }

    // 6. 分帳給司機（扣除平台抽成）
    go p.settlementToDriver(ctx, trip.DriverID, payment.Amount)

    return payment, nil
}

// settlementToDriver 結算給司機（扣除平台抽成）
func (p *PaymentProcessor) settlementToDriver(ctx context.Context, driverID int64, tripAmount float64) error {
    // Uber 抽成約 25%
    platformFee := tripAmount * 0.25
    driverEarning := tripAmount - platformFee

    // 更新司機錢包
    _, err := p.db.ExecContext(ctx, `
        UPDATE driver_wallets
        SET balance = balance + ?,
            pending_balance = pending_balance + ?
        WHERE driver_id = ?
    `, driverEarning, driverEarning, driverID)

    return err
}
```

---

## Act 7: 評分與反饋系統

### 7.1 雙向評分機制

```go
// internal/rating/service.go
package rating

type RatingService struct {
    db *sql.DB
}

type Rating struct {
    ID       int64   `json:"id"`
    TripID   int64   `json:"trip_id"`
    FromID   int64   `json:"from_id"`   // 評分者
    ToID     int64   `json:"to_id"`     // 被評分者
    Score    float64 `json:"score"`     // 1-5 星
    Comment  string  `json:"comment"`
    Tags     []string `json:"tags"`     // ["friendly", "clean_car", "safe_driving"]
    CreatedAt time.Time `json:"created_at"`
}

// SubmitRating 提交評分
func (r *RatingService) SubmitRating(ctx context.Context, rating *Rating) error {
    // 1. 驗證評分範圍
    if rating.Score < 1.0 || rating.Score > 5.0 {
        return fmt.Errorf("invalid score: must be 1-5")
    }

    // 2. 檢查是否已評分
    exists, err := r.hasRated(ctx, rating.TripID, rating.FromID)
    if err != nil {
        return err
    }
    if exists {
        return fmt.Errorf("already rated this trip")
    }

    // 3. 儲存評分
    _, err = r.db.ExecContext(ctx, `
        INSERT INTO ratings (trip_id, from_id, to_id, score, comment, tags, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `, rating.TripID, rating.FromID, rating.ToID, rating.Score, rating.Comment,
       pq.Array(rating.Tags), rating.CreatedAt)

    if err != nil {
        return err
    }

    // 4. 更新被評分者的平均評分
    go r.updateAverageRating(ctx, rating.ToID)

    // 5. 檢查低分預警（司機 < 4.0 星可能被停權）
    if rating.Score < 3.0 {
        go r.flagLowRating(ctx, rating)
    }

    return nil
}

// updateAverageRating 更新平均評分（使用滑動視窗）
func (r *RatingService) updateAverageRating(ctx context.Context, userID int64) error {
    // 計算最近 500 次行程的平均評分
    var avgScore float64
    err := r.db.QueryRowContext(ctx, `
        SELECT COALESCE(AVG(score), 0)
        FROM (
            SELECT score
            FROM ratings
            WHERE to_id = ?
            ORDER BY created_at DESC
            LIMIT 500
        ) recent_ratings
    `, userID).Scan(&avgScore)

    if err != nil {
        return err
    }

    // 更新到 drivers 表
    _, err = r.db.ExecContext(ctx, `
        UPDATE drivers
        SET rating = ?, rating_count = rating_count + 1
        WHERE id = ?
    `, avgScore, userID)

    return err
}
```

---

## Act 8: 系統擴展與成本優化

**場景**：系統從 1,000 名司機成長到 100,000 名司機，如何確保效能不下降？

### 8.1 對話：擴展挑戰

**Michael**：當規模擴大 100 倍，會面臨什麼瓶頸？

**David**：
1. **位置更新**：100,000 司機 × 每 4 秒 = 每秒 25,000 次寫入
2. **查詢附近司機**：尖峰時每秒 10,000 次查詢
3. **WebSocket 連線**：需要維護 100,000+ 長連線
4. **資料庫負載**：大量的行程記錄、評分數據

### 8.2 架構優化

```
                          ┌─────────────────┐
                          │   CDN (靜態)    │
                          └─────────────────┘
                                   │
                          ┌─────────────────┐
                          │  Load Balancer  │
                          └─────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │                    │                    │
     ┌────────▼────────┐  ┌───────▼────────┐  ┌───────▼────────┐
     │  API Server 1   │  │  API Server 2  │  │  API Server N  │
     │  (位置追蹤)     │  │  (叫車配對)    │  │  (支付評分)    │
     └─────────────────┘  └────────────────┘  └────────────────┘
              │                    │                    │
              └────────────────────┼────────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │                    │                    │
     ┌────────▼────────┐  ┌───────▼────────┐  ┌───────▼────────┐
     │  Redis Cluster  │  │  PostgreSQL    │  │  Kafka (事件)  │
     │  (位置/快取)    │  │  (行程/用戶)   │  │  (異步處理)    │
     └─────────────────┘  └────────────────┘  └────────────────┘
```

### 8.3 分區策略（Geosharding）

```go
// internal/sharding/geo_shard.go
package sharding

type GeoShard struct {
    ShardID   int
    Region    s2.CellID
    RedisAddr string
}

type ShardManager struct {
    shards []*GeoShard
}

// GetShardForLocation 根據位置決定使用哪個 Redis 分片
func (m *ShardManager) GetShardForLocation(lat, lon float64) *GeoShard {
    // 使用 S2 Cell level 10（城市級別）
    cellID := s2.CellIDFromLatLng(s2.LatLngFromDegrees(lat, lon)).Parent(10)

    // 一致性雜湊決定分片
    hash := int(cellID) % len(m.shards)
    return m.shards[hash]
}

// Example:
// 台北市 → Redis Shard 1 (10.0.1.10)
// 台中市 → Redis Shard 2 (10.0.1.11)
// 高雄市 → Redis Shard 3 (10.0.1.12)
```

### 8.4 成本分析

**Emma**：運營一個叫車平台的成本有多高？

**Michael**：讓我們以**台灣市場**為例計算（假設 10,000 名活躍司機，每日 50,000 趟行程）：

| 項目 | 規格 | 月費用 |
|------|------|--------|
| **伺服器** | 20 台 API Server (8C16G) | NT$300,000 |
| **Redis Cluster** | 12 節點 (32GB) | NT$180,000 |
| **PostgreSQL** | RDS Multi-AZ (r5.2xlarge) | NT$120,000 |
| **Kafka** | 6 節點集群 | NT$90,000 |
| **CDN** | 100TB 流量 | NT$60,000 |
| **地圖 API** | Google Maps (50,000 趟 × 3 次查詢/趟) | NT$450,000 |
| **簡訊通知** | 100,000 則/月 | NT$30,000 |
| **支付手續費** | Stripe 2.9% + NT$9 (假設單趟 NT$200) | NT$580,000 |
| **頻寬** | 200TB/月 | NT$150,000 |
| **監控 & 日誌** | Datadog + ELK | NT$60,000 |
| **總計** | | **NT$2,020,000/月** |

**優化建議**：
- **自建地圖引擎**：從 NT$450,000 降至 NT$50,000（節省 NT$400,000）
- **WebSocket 連線池複用**：減少 50% 伺服器成本
- **Redis 使用 Geohash**：記憶體使用減少 30%

---

## 總結

### 核心技術要點

1. **即時定位追蹤**
   - WebSocket 長連線
   - Redis Geo 資料結構
   - 每 4 秒更新一次位置

2. **地理空間索引**
   - Geohash：簡單高效，適合中小規模
   - QuadTree：動態分裂，適合記憶體充足場景
   - S2 Geometry：高精度，適合全球化應用

3. **智能配對算法**
   - 多因素評分（距離、ETA、評分、接單率、方向）
   - 分散式鎖防止重複派單
   - 3 次重試機制

4. **路徑規劃**
   - 第三方 API（Google Maps）vs 自建引擎（OSM + A*）
   - 即時路況整合
   - ETA 準確度 > 90%

5. **動態定價**
   - 供需比計算
   - 平滑演算法（EMA）
   - Surge 上限 3.0x

6. **支付與分帳**
   - Stripe 信用卡支付
   - 電子錢包
   - 司機結算（扣除 25% 平台費）

### 延伸思考

**Emma**：如果要設計「共乘」功能（UberPool），要怎麼做？

**Michael**：這會涉及：
- **路徑合併演算法**：找出順路的多個乘客
- **動態路徑調整**：中途加入新乘客
- **公平價格分攤**：根據實際里程分攤費用

這是個更複雜的最佳化問題，值得單獨深入研究！

**David**：自動駕駛的 Uber 會怎麼設計？

**Michael**：核心差異在於：
- 無需司機配對，改為**車輛調度**
- 需要**車隊管理系統**（充電、維護排程）
- **安全監控**：遠端接管機制
- **感測器數據**：即時上傳行駛數據

未來可期！

---

**下一章預告**：Airbnb - 住宿預訂平台（如何處理房源搜尋、日曆可用性、動態定價、反欺詐系統）
