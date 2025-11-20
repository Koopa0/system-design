# Chapter 25: Google Maps - 地圖導航系統設計

## 系統概述

Google Maps 是全球最大的地圖導航服務，每月活躍用戶超過 10 億，需要處理海量的地圖資料渲染、路徑規劃、實時導航、路況預測等複雜功能。本章將深入探討如何設計一個高效能、高可用的地圖導航系統。

**核心挑戰**：
- 地圖渲染（處理 PB 級別的地圖資料）
- 路徑規劃（在百萬級路網中找最佳路徑）
- 實時導航（語音提示、路線重規劃）
- 路況預測（機器學習預測塞車）
- 全球化服務（支援 220+ 國家/地區）
- 離線地圖（在無網路時仍可使用）

---

## Act 1: 地圖瓦片系統（Tile System）

**場景**：使用者打開 Google Maps，看到台北市的地圖，可以縮放、平移...

### 1.1 對話：Emma 與 David 討論地圖渲染

**Emma**（產品經理）：Google Maps 的地圖是怎麼顯示出來的？整個地球的地圖資料這麼大，不可能一次全部下載吧？

**David**（後端工程師）：對！地球表面積約 5.1 億平方公里，如果要存儲高解析度的衛星圖，數據量會達到 **PB 級別**（1 PB = 1000 TB）。

我們使用的是 **地圖瓦片系統**（Tile System）。

### 1.2 地圖瓦片原理

**Michael**（資深架構師）：地圖瓦片的核心概念是：
1. **將地球劃分成網格**：每個網格是一張 256×256 像素的圖片
2. **多層級縮放**：Zoom Level 0（全球）到 Zoom Level 21（建築物細節）
3. **按需載入**：只下載使用者看到的區域

```
Zoom Level 0:  1 張瓦片（整個地球）
Zoom Level 1:  4 張瓦片（2×2）
Zoom Level 2:  16 張瓦片（4×4）
Zoom Level 3:  64 張瓦片（8×8）
...
Zoom Level 21: 4,398,046,511,104 張瓦片（2^21 × 2^21）
```

### 1.3 瓦片座標系統

```go
// internal/tiles/coordinate.go
package tiles

import (
    "math"
)

// TileCoordinate 瓦片座標
type TileCoordinate struct {
    X     int // 橫座標
    Y     int // 縱座標
    Zoom  int // 縮放層級 (0-21)
}

// LatLngToTile 將經緯度轉換為瓦片座標
func LatLngToTile(lat, lng float64, zoom int) *TileCoordinate {
    // 計算瓦片數量
    n := math.Pow(2, float64(zoom))

    // 經度轉 X（-180° ~ 180° → 0 ~ 2^zoom）
    x := int(math.Floor((lng + 180.0) / 360.0 * n))

    // 緯度轉 Y（使用 Web Mercator 投影）
    latRad := lat * math.Pi / 180.0
    y := int(math.Floor((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n))

    return &TileCoordinate{
        X:    x,
        Y:    y,
        Zoom: zoom,
    }
}

// Example:
// 台北車站 (25.0478, 121.5170) at Zoom 15
// → Tile (27441, 13563, 15)
```

**Sarah**（前端工程師）：所以當我在地圖上移動時，前端會計算需要哪些瓦片，然後向伺服器請求？

**David**：沒錯！

```javascript
// 前端計算需要的瓦片
function getTilesInView(bounds, zoom) {
  const topLeft = latLngToTile(bounds.north, bounds.west, zoom);
  const bottomRight = latLngToTile(bounds.south, bounds.east, zoom);

  const tiles = [];
  for (let x = topLeft.x; x <= bottomRight.x; x++) {
    for (let y = topLeft.y; y <= bottomRight.y; y++) {
      tiles.push({ x, y, zoom });
    }
  }
  return tiles;
}

// 請求瓦片圖片
function loadTile(x, y, zoom) {
  const url = `https://mt1.google.com/vt?x=${x}&y=${y}&z=${zoom}`;
  return fetch(url);
}
```

### 1.4 瓦片渲染服務

```go
// internal/tiles/renderer.go
package tiles

import (
    "context"
    "fmt"
    "image"
    "image/png"
)

type TileRenderer struct {
    storage    *S3Storage     // 存儲在 S3
    cache      *RedisCache    // Redis 快取
    mapData    *MapDataStore  // 原始地圖資料（道路、建築物等）
}

// RenderTile 渲染瓦片（首次生成或資料更新時）
func (r *TileRenderer) RenderTile(ctx context.Context, coord *TileCoordinate) (*image.Image, error) {
    // 1. 檢查快取
    cacheKey := fmt.Sprintf("tile:%d:%d:%d", coord.Zoom, coord.X, coord.Y)
    cached, err := r.cache.Get(ctx, cacheKey)
    if err == nil {
        return cached, nil
    }

    // 2. 從 S3 載入預渲染的瓦片
    s3Key := fmt.Sprintf("tiles/%d/%d/%d.png", coord.Zoom, coord.X, coord.Y)
    tile, err := r.storage.Get(ctx, s3Key)
    if err == nil {
        r.cache.Set(ctx, cacheKey, tile, 24*time.Hour)
        return tile, nil
    }

    // 3. 即時渲染（當瓦片不存在時）
    tile, err = r.renderFromMapData(ctx, coord)
    if err != nil {
        return nil, err
    }

    // 4. 儲存到 S3 和快取
    r.storage.Put(ctx, s3Key, tile)
    r.cache.Set(ctx, cacheKey, tile, 24*time.Hour)

    return tile, nil
}

// renderFromMapData 從原始地圖資料渲染瓦片
func (r *TileRenderer) renderFromMapData(ctx context.Context, coord *TileCoordinate) (*image.Image, error) {
    // 建立畫布
    canvas := image.NewRGBA(image.Rect(0, 0, 256, 256))

    // 取得這個瓦片範圍內的地圖要素
    bounds := r.getTileBounds(coord)

    // 1. 繪製底色（水域、陸地）
    r.drawBackground(canvas, bounds)

    // 2. 繪製道路（高速公路、主要道路、小路）
    roads := r.mapData.GetRoads(bounds)
    for _, road := range roads {
        r.drawRoad(canvas, road, coord)
    }

    // 3. 繪製建築物
    buildings := r.mapData.GetBuildings(bounds)
    for _, building := range buildings {
        r.drawBuilding(canvas, building, coord)
    }

    // 4. 繪製文字標籤（道路名稱、地點名稱）
    labels := r.mapData.GetLabels(bounds)
    for _, label := range labels {
        r.drawLabel(canvas, label, coord)
    }

    return canvas, nil
}
```

### 1.5 CDN 分發策略

**Michael**：地圖瓦片是靜態資源，非常適合用 CDN 加速。

```
使用者請求流程：

1. 使用者: 請求 tile (27441, 13563, 15)
   ↓
2. CDN 邊緣節點（台北）: 檢查快取
   ↓
3. [快取命中] → 直接返回（延遲 < 10ms）
   [快取未命中] → 回源到 Origin Server
   ↓
4. Origin Server: 從 S3 載入
   ↓
5. CDN: 快取 30 天
```

**成本優化**：
- **命中率**：熱門區域（市中心）快取命中率 > 95%
- **流量成本**：CDN 每 GB NT$1.5，Origin 每 GB NT$3（節省 50%）

---

## Act 2: 路徑規劃算法（Routing）

**場景**：使用者要從台北車站到台北 101，系統需要計算最佳路線...

### 2.1 對話：路徑規劃的挑戰

**Emma**：當使用者輸入起點和終點，系統怎麼找出最佳路線？

**Michael**：這是一個**圖論問題**！
- **節點**：路口、交叉路口
- **邊**：道路、街道
- **權重**：距離、時間、路況

台北市約有 10 萬個路口，全台灣約 200 萬個節點。要在這麼大的圖中找最短路徑，需要高效的演算法。

### 2.2 路網資料結構

```go
// internal/routing/graph.go
package routing

type RoadNetwork struct {
    nodes map[int64]*Node
    edges map[int64][]*Edge
}

type Node struct {
    ID       int64   // 路口 ID
    Lat      float64
    Lng      float64
    Type     string  // "intersection", "junction", "endpoint"
}

type Edge struct {
    ID         int64
    From       int64  // 起點節點
    To         int64  // 終點節點
    Distance   float64 // 距離（公尺）
    TimeEstimate int   // 預估時間（秒）
    RoadType   string  // "highway", "main_road", "street", "alley"
    SpeedLimit int     // 速限 (km/h)
    OneWay     bool    // 單行道
    TollRoad   bool    // 收費道路
}
```

### 2.3 Dijkstra 演算法

**David**：最基本的最短路徑演算法是 **Dijkstra**。

```go
// internal/routing/dijkstra.go
package routing

import (
    "container/heap"
    "math"
)

type DijkstraRouter struct {
    graph *RoadNetwork
}

type NodeDistance struct {
    NodeID   int64
    Distance float64
    index    int // heap 索引
}

type PriorityQueue []*NodeDistance

// 實作 heap.Interface
func (pq PriorityQueue) Len() int { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool {
    return pq[i].Distance < pq[j].Distance
}
func (pq PriorityQueue) Swap(i, j int) {
    pq[i], pq[j] = pq[j], pq[i]
    pq[i].index = i
    pq[j].index = j
}
func (pq *PriorityQueue) Push(x interface{}) {
    item := x.(*NodeDistance)
    item.index = len(*pq)
    *pq = append(*pq, item)
}
func (pq *PriorityQueue) Pop() interface{} {
    old := *pq
    n := len(old)
    item := old[n-1]
    *pq = old[0 : n-1]
    return item
}

// FindShortestPath 使用 Dijkstra 找最短路徑
func (d *DijkstraRouter) FindShortestPath(startID, endID int64) ([]*Node, float64, error) {
    // 初始化距離
    dist := make(map[int64]float64)
    prev := make(map[int64]int64)

    for nodeID := range d.graph.nodes {
        dist[nodeID] = math.Inf(1)
    }
    dist[startID] = 0

    // 優先佇列
    pq := &PriorityQueue{}
    heap.Init(pq)
    heap.Push(pq, &NodeDistance{NodeID: startID, Distance: 0})

    visited := make(map[int64]bool)

    for pq.Len() > 0 {
        current := heap.Pop(pq).(*NodeDistance)
        currentID := current.NodeID

        // 到達終點
        if currentID == endID {
            break
        }

        if visited[currentID] {
            continue
        }
        visited[currentID] = true

        // 檢查所有鄰居
        for _, edge := range d.graph.edges[currentID] {
            neighborID := edge.To

            // 計算新距離
            newDist := dist[currentID] + edge.Distance

            if newDist < dist[neighborID] {
                dist[neighborID] = newDist
                prev[neighborID] = currentID
                heap.Push(pq, &NodeDistance{
                    NodeID:   neighborID,
                    Distance: newDist,
                })
            }
        }
    }

    // 重建路徑
    path := d.reconstructPath(prev, startID, endID)
    return path, dist[endID], nil
}

func (d *DijkstraRouter) reconstructPath(prev map[int64]int64, start, end int64) []*Node {
    var path []*Node
    current := end

    for current != start {
        path = append([]*Node{d.graph.nodes[current]}, path...)
        current = prev[current]
    }
    path = append([]*Node{d.graph.nodes[start]}, path...)

    return path
}
```

### 2.4 A* 演算法（啟發式優化）

**Michael**：Dijkstra 的問題是會往四面八方搜索。**A\*** 演算法使用**啟發式函數**（Heuristic）導向終點，大幅提升效率！

```go
// internal/routing/astar.go
package routing

type AStarRouter struct {
    graph *RoadNetwork
}

type AStarNode struct {
    NodeID   int64
    GScore   float64 // 從起點到當前節點的實際代價
    FScore   float64 // GScore + Heuristic（預估總代價）
    index    int
}

type AStarPriorityQueue []*AStarNode

func (pq AStarPriorityQueue) Len() int { return len(pq) }
func (pq AStarPriorityQueue) Less(i, j int) bool {
    return pq[i].FScore < pq[j].FScore // 依 F 值排序
}
func (pq AStarPriorityQueue) Swap(i, j int) {
    pq[i], pq[j] = pq[j], pq[i]
    pq[i].index = i
    pq[j].index = j
}
func (pq *AStarPriorityQueue) Push(x interface{}) {
    item := x.(*AStarNode)
    item.index = len(*pq)
    *pq = append(*pq, item)
}
func (pq *AStarPriorityQueue) Pop() interface{} {
    old := *pq
    n := len(old)
    item := old[n-1]
    *pq = old[0 : n-1]
    return item
}

// FindShortestPath 使用 A* 找最短路徑
func (a *AStarRouter) FindShortestPath(startID, endID int64) ([]*Node, float64, error) {
    startNode := a.graph.nodes[startID]
    endNode := a.graph.nodes[endID]

    // G Score: 從起點到各節點的實際距離
    gScore := make(map[int64]float64)
    for nodeID := range a.graph.nodes {
        gScore[nodeID] = math.Inf(1)
    }
    gScore[startID] = 0

    // F Score: G + Heuristic
    fScore := make(map[int64]float64)
    fScore[startID] = a.heuristic(startNode, endNode)

    // 優先佇列
    pq := &AStarPriorityQueue{}
    heap.Init(pq)
    heap.Push(pq, &AStarNode{
        NodeID: startID,
        GScore: 0,
        FScore: fScore[startID],
    })

    prev := make(map[int64]int64)
    visited := make(map[int64]bool)

    for pq.Len() > 0 {
        current := heap.Pop(pq).(*AStarNode)
        currentID := current.NodeID

        // 到達終點
        if currentID == endID {
            break
        }

        if visited[currentID] {
            continue
        }
        visited[currentID] = true

        // 檢查鄰居
        for _, edge := range a.graph.edges[currentID] {
            neighborID := edge.To

            // 計算 G Score
            tentativeG := gScore[currentID] + edge.Distance

            if tentativeG < gScore[neighborID] {
                prev[neighborID] = currentID
                gScore[neighborID] = tentativeG

                // 計算 F Score = G + H
                neighborNode := a.graph.nodes[neighborID]
                fScore[neighborID] = tentativeG + a.heuristic(neighborNode, endNode)

                heap.Push(pq, &AStarNode{
                    NodeID: neighborID,
                    GScore: tentativeG,
                    FScore: fScore[neighborID],
                })
            }
        }
    }

    path := a.reconstructPath(prev, startID, endID)
    return path, gScore[endID], nil
}

// heuristic 啟發式函數（直線距離）
func (a *AStarRouter) heuristic(from, to *Node) float64 {
    // 使用 Haversine 公式計算球面距離
    return haversineDistance(from.Lat, from.Lng, to.Lat, to.Lng)
}
```

### 2.5 演算法效能比較

**Emma**：A* 快多少？

**Michael**：

| 演算法 | 時間複雜度 | 實際表現（台北 → 高雄）| 優缺點 |
|--------|-----------|----------------------|--------|
| **Dijkstra** | O(E + V log V) | 訪問 50 萬節點，耗時 800ms | 保證最短路徑，但慢 |
| **A*** | O(E + V log V) | 訪問 3 萬節點，耗時 120ms | **快 6 倍**，仍保證最短路徑 |
| **Bidirectional A*** | 更快 | 訪問 1.5 萬節點，耗時 60ms | **快 13 倍** |

**Bidirectional A***：從起點和終點同時搜索，兩邊相遇時結束！

---

## Act 3: 路況數據收集與預測

**場景**：週五晚上 6 點，台北市區塞車嚴重，Google Maps 建議改走其他路線...

### 3.1 對話：路況資料來源

**Emma**：Google Maps 怎麼知道哪裡塞車？

**David**：主要有三個資料來源：

1. **眾包資料**（Crowdsourcing）：從使用 Google Maps 的車輛收集 GPS 數據
2. **政府開放資料**：交通局的即時路況感測器
3. **歷史資料**：分析過去的塞車模式

### 3.2 GPS 資料收集

```go
// internal/traffic/collector.go
package traffic

type TrafficCollector struct {
    kafka      *KafkaProducer
    redis      *RedisClient
    aggregator *TrafficAggregator
}

type GPSUpdate struct {
    UserID    int64     `json:"user_id"`
    Lat       float64   `json:"lat"`
    Lng       float64   `json:"lng"`
    Speed     float64   `json:"speed"`      // km/h
    Bearing   float64   `json:"bearing"`    // 行駛方向
    Accuracy  float64   `json:"accuracy"`   // GPS 精度（公尺）
    Timestamp time.Time `json:"timestamp"`
}

// CollectGPSData 收集 GPS 資料（從行駛中的車輛）
func (c *TrafficCollector) CollectGPSData(ctx context.Context, update *GPSUpdate) error {
    // 1. 驗證資料品質
    if update.Accuracy > 50 {
        // GPS 精度太差，忽略
        return nil
    }

    // 2. 地圖匹配（Map Matching）：將 GPS 點對應到道路上
    edge := c.mapMatchGPSToRoad(update)
    if edge == nil {
        return nil
    }

    // 3. 發送到 Kafka（異步處理）
    trafficData := &TrafficData{
        EdgeID:    edge.ID,
        Speed:     update.Speed,
        Timestamp: update.Timestamp,
    }

    return c.kafka.Produce(ctx, "traffic-data", trafficData)
}

// mapMatchGPSToRoad 將 GPS 座標對應到最近的道路
func (c *TrafficCollector) mapMatchGPSToRoad(update *GPSUpdate) *Edge {
    // 使用 Hidden Markov Model (HMM) 做地圖匹配
    // 簡化版：找最近的道路

    // 1. 找附近 100 公尺內的道路
    nearbyEdges := c.findNearbyEdges(update.Lat, update.Lng, 100)

    if len(nearbyEdges) == 0 {
        return nil
    }

    // 2. 計算每條道路的匹配分數
    var bestEdge *Edge
    bestScore := 0.0

    for _, edge := range nearbyEdges {
        score := c.calculateMatchScore(update, edge)
        if score > bestScore {
            bestScore = score
            bestEdge = edge
        }
    }

    return bestEdge
}

func (c *TrafficCollector) calculateMatchScore(update *GPSUpdate, edge *Edge) float64 {
    // 1. 距離分數（越近越高）
    distance := c.distanceToEdge(update.Lat, update.Lng, edge)
    distanceScore := math.Max(0, 1 - distance/50) // 50 公尺內滿分

    // 2. 方向分數（行駛方向與道路方向一致）
    bearingDiff := math.Abs(update.Bearing - edge.Bearing)
    if bearingDiff > 180 {
        bearingDiff = 360 - bearingDiff
    }
    bearingScore := 1 - bearingDiff/180

    // 加權總分
    return distanceScore*0.7 + bearingScore*0.3
}
```

### 3.3 即時路況聚合

```go
// internal/traffic/aggregator.go
package traffic

type TrafficAggregator struct {
    redis *RedisClient
}

type TrafficData struct {
    EdgeID    int64
    Speed     float64
    Timestamp time.Time
}

// AggregateTrafficData 聚合路況資料（每分鐘執行一次）
func (a *TrafficAggregator) AggregateTrafficData(ctx context.Context) error {
    // 從 Kafka 批量讀取資料
    messages := a.consumeKafkaMessages(ctx, "traffic-data", 1000)

    // 按道路 ID 分組
    edgeData := make(map[int64][]float64)

    for _, msg := range messages {
        var data TrafficData
        json.Unmarshal(msg.Value, &data)

        edgeData[data.EdgeID] = append(edgeData[data.EdgeID], data.Speed)
    }

    // 計算每條道路的平均速度
    for edgeID, speeds := range edgeData {
        avgSpeed := calculateAverage(speeds)

        // 儲存到 Redis（5 分鐘過期）
        key := fmt.Sprintf("traffic:edge:%d", edgeID)
        err := a.redis.Set(ctx, key, avgSpeed, 5*time.Minute).Err()
        if err != nil {
            return err
        }

        // 判斷路況等級
        congestion := a.calculateCongestionLevel(edgeID, avgSpeed)

        // 儲存路況等級
        congestionKey := fmt.Sprintf("traffic:congestion:%d", edgeID)
        a.redis.Set(ctx, congestionKey, congestion, 5*time.Minute)
    }

    return nil
}

// calculateCongestionLevel 計算路況等級
func (a *TrafficAggregator) calculateCongestionLevel(edgeID int64, currentSpeed float64) string {
    // 取得該道路的速限
    edge := a.getEdge(edgeID)
    speedLimit := float64(edge.SpeedLimit)

    // 計算速度比
    ratio := currentSpeed / speedLimit

    switch {
    case ratio >= 0.8:
        return "free"      // 順暢
    case ratio >= 0.5:
        return "moderate"  // 緩慢
    case ratio >= 0.3:
        return "heavy"     // 擁塞
    default:
        return "severe"    // 嚴重擁塞
    }
}
```

### 3.4 機器學習預測路況

**Michael**：除了即時路況，我們還能**預測未來的路況**！

```go
// internal/traffic/predictor.go
package traffic

type TrafficPredictor struct {
    model *MLModel // 機器學習模型
    db    *PostgreSQL
}

// PredictTraffic 預測未來 30 分鐘的路況
func (p *TrafficPredictor) PredictTraffic(ctx context.Context, edgeID int64, futureTime time.Time) (float64, error) {
    // 1. 提取特徵
    features := p.extractFeatures(edgeID, futureTime)

    // 2. 使用模型預測
    predictedSpeed := p.model.Predict(features)

    return predictedSpeed, nil
}

// extractFeatures 提取特徵向量
func (p *TrafficPredictor) extractFeatures(edgeID int64, futureTime time.Time) []float64 {
    features := make([]float64, 0)

    // 1. 時間特徵
    features = append(features, float64(futureTime.Hour()))        // 小時 (0-23)
    features = append(features, float64(futureTime.Weekday()))     // 星期 (0-6)
    features = append(features, float64(futureTime.Day()))         // 日期 (1-31)

    // 2. 歷史速度（過去 1 小時的平均速度）
    historicalSpeed := p.getHistoricalSpeed(edgeID, futureTime.Add(-1*time.Hour), futureTime)
    features = append(features, historicalSpeed)

    // 3. 相鄰道路的路況
    neighborEdges := p.getNeighborEdges(edgeID)
    for _, neighborID := range neighborEdges {
        neighborSpeed := p.getCurrentSpeed(neighborID)
        features = append(features, neighborSpeed)
    }

    // 4. 特殊事件（演唱會、體育賽事等）
    hasEvent := p.hasEventNearby(edgeID, futureTime)
    if hasEvent {
        features = append(features, 1.0)
    } else {
        features = append(features, 0.0)
    }

    return features
}
```

**訓練資料**：
- 歷史路況資料（過去 2 年）
- 天氣資料
- 節日/活動資料
- 交通事故記錄

**模型**：
- **XGBoost** 或 **LSTM**（時序預測）
- **準確度**：P80 誤差 < 5 km/h

---

## Act 4: 實時導航系統

**場景**：使用者開車從台北到台中，系統提供語音導航「400 公尺後右轉」...

### 4.1 對話：導航的技術挑戰

**Emma**：導航功能看起來簡單，但實際上很複雜吧？

**Michael**：對！導航需要：
1. **即時定位**：持續追蹤使用者 GPS
2. **路線追蹤**：判斷使用者是否偏離路線
3. **語音提示**：在適當時機播報（「200 公尺後左轉」）
4. **動態重規劃**：遇到塞車時重新規劃路線

### 4.2 導航引擎

```go
// internal/navigation/engine.go
package navigation

type NavigationEngine struct {
    router         *AStarRouter
    trafficService *TrafficService
    tts            *TextToSpeechService
}

type NavigationSession struct {
    SessionID   string
    UserID      int64
    Route       []*Node      // 規劃的路線
    CurrentStep int          // 當前在第幾個轉彎點
    StartTime   time.Time
    ETA         time.Time
}

// StartNavigation 開始導航
func (n *NavigationEngine) StartNavigation(ctx context.Context, startLat, startLng, endLat, endLng float64) (*NavigationSession, error) {
    // 1. 規劃路線（考慮即時路況）
    route, distance, err := n.router.FindShortestPath(
        n.findNearestNode(startLat, startLng),
        n.findNearestNode(endLat, endLng),
    )
    if err != nil {
        return nil, err
    }

    // 2. 計算 ETA
    duration := n.calculateRouteDuration(route)
    eta := time.Now().Add(time.Duration(duration) * time.Second)

    // 3. 生成導航指令
    instructions := n.generateInstructions(route)

    // 4. 建立導航 Session
    session := &NavigationSession{
        SessionID:   uuid.New().String(),
        Route:       route,
        CurrentStep: 0,
        StartTime:   time.Now(),
        ETA:         eta,
    }

    return session, nil
}

// UpdateLocation 更新使用者位置（每秒呼叫一次）
func (n *NavigationEngine) UpdateLocation(ctx context.Context, session *NavigationSession, lat, lng float64) (*NavigationUpdate, error) {
    // 1. 檢查是否偏離路線
    onRoute, deviationDistance := n.checkOnRoute(session, lat, lng)

    if !onRoute && deviationDistance > 50 {
        // 偏離路線超過 50 公尺，重新規劃
        return n.Reroute(ctx, session, lat, lng)
    }

    // 2. 計算到下一個轉彎點的距離
    nextTurn := session.Route[session.CurrentStep+1]
    distanceToTurn := haversineDistance(lat, lng, nextTurn.Lat, nextTurn.Lng) * 1000 // 轉為公尺

    // 3. 生成語音提示
    var voiceInstruction string

    switch {
    case distanceToTurn <= 50:
        voiceInstruction = "現在右轉"
        session.CurrentStep++
    case distanceToTurn <= 200:
        voiceInstruction = "200 公尺後右轉"
    case distanceToTurn <= 500:
        voiceInstruction = "500 公尺後右轉"
    }

    // 4. 更新 ETA
    remainingDuration := n.calculateRemainingDuration(session, lat, lng)
    newETA := time.Now().Add(time.Duration(remainingDuration) * time.Second)

    return &NavigationUpdate{
        OnRoute:          onRoute,
        DistanceToTurn:   distanceToTurn,
        VoiceInstruction: voiceInstruction,
        ETA:              newETA,
    }, nil
}

// Reroute 重新規劃路線
func (n *NavigationEngine) Reroute(ctx context.Context, session *NavigationSession, currentLat, currentLng float64) (*NavigationUpdate, error) {
    // 1. 從當前位置重新規劃到終點
    endNode := session.Route[len(session.Route)-1]

    newRoute, _, err := n.router.FindShortestPath(
        n.findNearestNode(currentLat, currentLng),
        endNode.ID,
    )
    if err != nil {
        return nil, err
    }

    // 2. 更新 Session
    session.Route = newRoute
    session.CurrentStep = 0

    // 3. 語音提示
    return &NavigationUpdate{
        OnRoute:          true,
        VoiceInstruction: "正在重新規劃路線",
        Rerouted:         true,
    }, nil
}
```

### 4.3 語音提示生成

```go
// internal/navigation/instructions.go
package navigation

type Instruction struct {
    Type        string  // "turn_left", "turn_right", "continue", "arrive"
    Distance    float64 // 距離上一個指令的距離（公尺）
    RoadName    string  // 道路名稱
    Description string  // 文字描述
}

// generateInstructions 生成導航指令
func (n *NavigationEngine) generateInstructions(route []*Node) []*Instruction {
    instructions := make([]*Instruction, 0)

    for i := 0; i < len(route)-1; i++ {
        current := route[i]
        next := route[i+1]

        // 計算轉彎角度
        var turnType string
        if i < len(route)-2 {
            bearing1 := calculateBearing(current, next)
            bearing2 := calculateBearing(next, route[i+2])

            angle := bearing2 - bearing1
            if angle < 0 {
                angle += 360
            }

            switch {
            case angle < 30 || angle > 330:
                turnType = "continue"
            case angle >= 30 && angle < 150:
                turnType = "turn_right"
            case angle >= 150 && angle < 210:
                turnType = "u_turn"
            case angle >= 210 && angle < 330:
                turnType = "turn_left"
            }
        }

        // 取得道路名稱
        edge := n.getEdge(current.ID, next.ID)
        roadName := edge.Name

        // 計算距離
        distance := haversineDistance(current.Lat, current.Lng, next.Lat, next.Lng) * 1000

        instruction := &Instruction{
            Type:     turnType,
            Distance: distance,
            RoadName: roadName,
            Description: n.generateDescription(turnType, distance, roadName),
        }

        instructions = append(instructions, instruction)
    }

    return instructions
}

func (n *NavigationEngine) generateDescription(turnType string, distance float64, roadName string) string {
    switch turnType {
    case "turn_left":
        return fmt.Sprintf("%.0f 公尺後左轉進入%s", distance, roadName)
    case "turn_right":
        return fmt.Sprintf("%.0f 公尺後右轉進入%s", distance, roadName)
    case "continue":
        return fmt.Sprintf("繼續直行 %.0f 公尺", distance)
    case "arrive":
        return "您已到達目的地"
    default:
        return ""
    }
}
```

---

## Act 5: 地點搜尋與地理編碼

**場景**：使用者搜尋「台北車站」，系統返回精確座標...

### 5.1 地理編碼（Geocoding）

**Emma**：使用者輸入「台北車站」，系統怎麼知道座標？

**David**：這叫做 **地理編碼**（Geocoding）：地址 → 座標。

```go
// internal/geocoding/service.go
package geocoding

type GeocodingService struct {
    db          *PostgreSQL
    searchIndex *ElasticsearchClient
}

type Place struct {
    ID          int64    `json:"id"`
    Name        string   `json:"name"`
    Address     string   `json:"address"`
    Latitude    float64  `json:"latitude"`
    Longitude   float64  `json:"longitude"`
    PlaceType   string   `json:"place_type"` // "train_station", "restaurant", "landmark"
    City        string   `json:"city"`
    Country     string   `json:"country"`
}

// Geocode 地理編碼（地址 → 座標）
func (g *GeocodingService) Geocode(ctx context.Context, address string) ([]*Place, error) {
    // 使用 Elasticsearch 模糊搜尋
    query := map[string]interface{}{
        "query": map[string]interface{}{
            "multi_match": map[string]interface{}{
                "query": address,
                "fields": []string{"name^3", "address^2", "city"},
                "type": "best_fields",
                "fuzziness": "AUTO",
            },
        },
    }

    results, err := g.searchIndex.Search(ctx, "places", query)
    if err != nil {
        return nil, err
    }

    var places []*Place
    for _, hit := range results.Hits {
        var place Place
        json.Unmarshal(hit.Source, &place)
        places = append(places, &place)
    }

    return places, nil
}

// ReverseGeocode 反地理編碼（座標 → 地址）
func (g *GeocodingService) ReverseGeocode(ctx context.Context, lat, lng float64) (*Place, error) {
    // 使用地理空間查詢找最近的地點
    query := map[string]interface{}{
        "query": map[string]interface{}{
            "geo_distance": map[string]interface{}{
                "distance": "100m",
                "location": map[string]float64{
                    "lat": lat,
                    "lon": lng,
                },
            },
        },
        "sort": []map[string]interface{}{
            {
                "_geo_distance": map[string]interface{}{
                    "location": map[string]float64{
                        "lat": lat,
                        "lon": lng,
                    },
                    "order": "asc",
                },
            },
        },
    }

    results, err := g.searchIndex.Search(ctx, "places", query)
    if err != nil {
        return nil, err
    }

    if len(results.Hits) == 0 {
        return nil, fmt.Errorf("no place found")
    }

    var place Place
    json.Unmarshal(results.Hits[0].Source, &place)

    return &place, nil
}
```

### 5.2 搜尋自動補全

```go
// internal/search/autocomplete.go
package search

type AutocompleteService struct {
    trie  *Trie
    redis *RedisClient
}

// Autocomplete 搜尋自動補全
func (a *AutocompleteService) Autocomplete(ctx context.Context, prefix string, limit int) ([]string, error) {
    // 從 Trie 樹查詢
    suggestions := a.trie.SearchPrefix(prefix, limit)

    // 按熱門度排序
    ranked := a.rankByPopularity(suggestions)

    return ranked[:min(limit, len(ranked))], nil
}

// 熱門搜尋詞追蹤
func (a *AutocompleteService) TrackSearch(ctx context.Context, query string) error {
    // 增加搜尋計數（使用 Redis Sorted Set）
    key := "search:popular"
    return a.redis.ZIncrBy(ctx, key, 1, query).Err()
}
```

---

## Act 6: 離線地圖

**場景**：使用者出國旅行，下載日本地圖，在無網路時也能導航...

### 6.1 對話：離線地圖設計

**Emma**：離線地圖要下載什麼資料？

**Michael**：
1. **地圖瓦片**：特定區域的所有縮放層級
2. **路網資料**：道路節點、邊
3. **地點資料**：餐廳、景點、加油站
4. **搜尋索引**：能搜尋地點

### 6.2 離線地圖打包

```go
// internal/offline/packager.go
package offline

type OfflineMapPackager struct {
    tileStorage *S3Storage
    graphDB     *GraphDatabase
    placesDB    *PostgreSQL
}

type OfflineMapPackage struct {
    Region     string   `json:"region"`      // "taipei", "tokyo"
    Version    string   `json:"version"`     // "2024-01-15"
    TileCount  int      `json:"tile_count"`
    Size       int64    `json:"size"`        // bytes
    DownloadURL string  `json:"download_url"`
}

// PackageOfflineMap 打包離線地圖
func (o *OfflineMapPackager) PackageOfflineMap(ctx context.Context, bounds *BoundingBox) (*OfflineMapPackage, error) {
    // 1. 計算需要的瓦片
    tiles := o.calculateRequiredTiles(bounds, 10, 18) // Zoom 10-18

    // 2. 打包瓦片（壓縮成 .mbtiles 格式）
    tileArchive := o.packTiles(tiles)

    // 3. 匯出路網資料（SQLite）
    graphData := o.exportGraphData(bounds)

    // 4. 匯出地點資料
    placesData := o.exportPlacesData(bounds)

    // 5. 建立搜尋索引（使用 SQLite FTS5）
    searchIndex := o.buildOfflineSearchIndex(placesData)

    // 6. 壓縮打包
    packageFile := o.createPackage(tileArchive, graphData, placesData, searchIndex)

    // 7. 上傳到 CDN
    downloadURL := o.uploadToCDN(packageFile)

    return &OfflineMapPackage{
        Region:      "taipei",
        Version:     time.Now().Format("2006-01-02"),
        TileCount:   len(tiles),
        Size:        packageFile.Size,
        DownloadURL: downloadURL,
    }, nil
}
```

**檔案大小估算**（台北市）：
- 瓦片（Zoom 10-18）：約 500 MB
- 路網資料：約 50 MB
- 地點資料：約 20 MB
- 搜尋索引：約 10 MB
- **總計**：約 580 MB

---

## Act 7: 成本分析與優化

### 7.1 Google Maps 營運成本估算

**Emma**：Google Maps 營運成本有多高？

**Michael**：以**全球 10 億月活躍用戶**估算：

| 項目 | 規格 | 月費用 (USD) |
|------|------|-------------|
| **CDN** | 100 PB 流量（地圖瓦片） | $5,000,000 |
| **運算** | 10,000 台伺服器 (路徑規劃) | $2,000,000 |
| **儲存** | 5 PB 地圖資料（S3） | $100,000 |
| **資料庫** | PostgreSQL + Redis 集群 | $500,000 |
| **地圖資料授權** | 衛星圖、街景車 | $1,000,000 |
| **Elasticsearch** | 地點搜尋 | $300,000 |
| **Kafka** | 路況資料流 | $200,000 |
| **監控** | Datadog + Prometheus | $100,000 |
| **總計** | | **$9,200,000/月** |

**年度成本**：約 **$110,000,000（約 NT$ 35 億）**

### 7.2 成本優化策略

**優化方案**：

1. **瓦片快取優化**
   - 熱門區域快取命中率 > 95%
   - 冷門區域使用 On-Demand 渲染
   - 節省 CDN 成本 30%

2. **路徑規劃快取**
   - 熱門路線（如台北→台中）快取 1 小時
   - 快取命中率 40%
   - 節省運算成本 40%

3. **多級快取架構**
   ```
   瀏覽器快取 (7 天)
     ↓ Miss
   CDN 快取 (30 天)
     ↓ Miss
   Redis 快取 (24 小時)
     ↓ Miss
   S3 Origin
   ```

---

## 總結

### 核心技術要點

1. **地圖瓦片系統**
   - Web Mercator 投影
   - 多層級縮放（Zoom 0-21）
   - CDN 加速（命中率 > 95%）

2. **路徑規劃**
   - Dijkstra vs A* 演算法
   - Bidirectional A*（快 13 倍）
   - 考慮即時路況

3. **路況系統**
   - 眾包 GPS 資料
   - 地圖匹配（Map Matching）
   - 機器學習預測（XGBoost/LSTM）

4. **實時導航**
   - 路線追蹤與偏離偵測
   - 語音提示生成
   - 動態重規劃

5. **地理編碼**
   - Elasticsearch 模糊搜尋
   - 地理空間查詢
   - 搜尋自動補全（Trie）

6. **離線地圖**
   - .mbtiles 格式
   - SQLite 路網資料
   - FTS5 全文搜尋

### 延伸思考

**Emma**：如果要加入「AR 導航」功能（像 Google Live View），要怎麼設計？

**Michael**：需要：
- **電腦視覺**：辨識街景特徵點
- **AR 渲染**：在鏡頭畫面疊加箭頭
- **精確定位**：結合 GPS + 視覺定位（VPS）
- **3D 建築模型**：渲染周圍建築物

這是一個更複雜的系統，值得單獨研究！

**David**：無人駕駛的地圖需求有何不同？

**Michael**：
- **高精度地圖**（HD Map）：精度達公分級
- **3D 道路模型**：包含車道線、紅綠燈、標誌
- **即時更新**：施工、事故需立即更新
- **感測器融合**：結合 LiDAR、相機、雷達

---

**下一章預告**：Yelp - 附近的餐廳（地理空間索引、QuadTree、評分排序）
