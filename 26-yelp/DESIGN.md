# Chapter 26: Yelp - 附近的餐廳搜尋系統

## 系統概述

Yelp 是全球最大的本地商家評論平台，幫助使用者找到「附近的餐廳」、「最好的咖啡廳」等。本章將深入探討如何設計一個高效能的地點搜尋與評論系統。

**核心挑戰**：
- 地理空間搜尋（快速找到附近的餐廳）
- 複合排序（距離 + 評分 + 價格 + 營業時間）
- 評論系統（百萬級評論、反垃圾）
- 圖片上傳與展示
- 即時更新（新增餐廳、營業時間變更）
- 高併發讀取（尖峰時段大量查詢）

---

## Act 1: 地理空間索引 - 找到附近的餐廳

**場景**：使用者在台北車站打開 Yelp，搜尋「附近 1 公里內的餐廳」...

### 1.1 對話：Emma 與 David 討論搜尋策略

**Emma**（產品經理）：使用者最常用的功能是「附近的餐廳」，這個要怎麼實現？

**David**（後端工程師）：這是一個典型的**地理空間範圍查詢**（Geo Range Query）問題。假設台北市有 10,000 家餐廳，我們要在 1 公里內找到所有餐廳。

**Sarah**（前端工程師）：直接遍歷所有餐廳，計算距離不行嗎？

**David**：那太慢了！10,000 次距離計算，每次都要用 Haversine 公式... 這會拖垮系統。

**Michael**（資深架構師）：我們需要**空間索引**（Spatial Index），讓查詢複雜度從 O(N) 降到 O(log N)。

### 1.2 方案一：PostgreSQL + PostGIS

**Michael**：最簡單的方案是用 **PostgreSQL + PostGIS** 擴展。

```sql
-- 建立餐廳表
CREATE TABLE restaurants (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    latitude DECIMAL(10,8) NOT NULL,
    longitude DECIMAL(11,8) NOT NULL,
    location GEOGRAPHY(POINT, 4326),  -- PostGIS 地理類型
    category VARCHAR(100),
    rating DECIMAL(3,2),
    price_level INT,                  -- 1-4 ($ to $$$$)
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 建立地理空間索引（GIST）
CREATE INDEX idx_restaurants_location ON restaurants USING GIST (location);
```

**查詢附近 1 公里的餐廳**：

```sql
SELECT
    id,
    name,
    rating,
    ST_Distance(
        location,
        ST_SetSRID(ST_MakePoint(121.5170, 25.0478), 4326)::geography
    ) AS distance_meters
FROM restaurants
WHERE ST_DWithin(
    location,
    ST_SetSRID(ST_MakePoint(121.5170, 25.0478), 4326)::geography,
    1000  -- 1000 公尺
)
ORDER BY distance_meters
LIMIT 20;
```

**Emma**：這個方案的效能如何？

**Michael**：
- **查詢時間**：10-30ms（10,000 家餐廳）
- **優點**：簡單、準確、支援複雜查詢
- **缺點**：資料庫壓力大、擴展性有限

### 1.3 方案二：Redis + Geohash

**David**：如果要降低資料庫負載，可以用 **Redis Geo**。

```go
// internal/search/redis_geo.go
package search

import (
    "context"
    "github.com/go-redis/redis/v8"
)

type RedisGeoSearch struct {
    redis *redis.Client
}

// AddRestaurant 新增餐廳到 Redis Geo
func (r *RedisGeoSearch) AddRestaurant(ctx context.Context, id int64, lat, lng float64) error {
    key := "restaurants:geo"

    return r.redis.GeoAdd(ctx, key, &redis.GeoLocation{
        Name:      fmt.Sprintf("restaurant:%d", id),
        Longitude: lng,
        Latitude:  lat,
    }).Err()
}

// SearchNearby 查詢附近的餐廳
func (r *RedisGeoSearch) SearchNearby(ctx context.Context, lat, lng float64, radiusKm float64) ([]int64, error) {
    key := "restaurants:geo"

    // GEORADIUS 查詢
    results, err := r.redis.GeoRadius(ctx, key, lng, lat, &redis.GeoRadiusQuery{
        Radius:      radiusKm,
        Unit:        "km",
        WithCoord:   true,
        WithDist:    true,
        Count:       100,
        Sort:        "ASC", // 按距離排序
    }).Result()

    if err != nil {
        return nil, err
    }

    // 提取餐廳 ID
    var restaurantIDs []int64
    for _, result := range results {
        var id int64
        fmt.Sscanf(result.Name, "restaurant:%d", &id)
        restaurantIDs = append(restaurantIDs, id)
    }

    return restaurantIDs, nil
}
```

**效能**：
- **查詢時間**：< 5ms（100 萬家餐廳）
- **記憶體使用**：約 50 bytes/餐廳
- **優點**：快速、可擴展
- **缺點**：只能做簡單的範圍查詢，無法結合其他條件（評分、價格）

### 1.4 方案三：Elasticsearch + Geo Point

**Michael**：如果需要**複合查詢**（距離 + 評分 + 類別），推薦用 **Elasticsearch**。

```go
// internal/search/elasticsearch.go
package search

import (
    "context"
    "encoding/json"
    "github.com/elastic/go-elasticsearch/v8"
)

type ElasticsearchSearch struct {
    client *elasticsearch.Client
}

// SearchNearby 複合查詢：附近 + 評分 > 4.0 + 開放中
func (e *ElasticsearchSearch) SearchNearby(ctx context.Context, lat, lng float64, radiusKm float64, minRating float64) ([]*Restaurant, error) {
    // 建立查詢
    query := map[string]interface{}{
        "query": map[string]interface{}{
            "bool": map[string]interface{}{
                "must": []map[string]interface{}{
                    // 1. 地理範圍
                    {
                        "geo_distance": map[string]interface{}{
                            "distance": fmt.Sprintf("%fkm", radiusKm),
                            "location": map[string]float64{
                                "lat": lat,
                                "lon": lng,
                            },
                        },
                    },
                    // 2. 評分過濾
                    {
                        "range": map[string]interface{}{
                            "rating": map[string]interface{}{
                                "gte": minRating,
                            },
                        },
                    },
                },
                "filter": []map[string]interface{}{
                    // 3. 目前營業中
                    {
                        "script": map[string]interface{}{
                            "script": `
                                def now = new Date();
                                def day = now.getDay();
                                def hour = now.getHours();

                                if (doc['opening_hours'].size() == 0) return false;

                                def hours = doc['opening_hours'][day];
                                return hour >= hours.open && hour < hours.close;
                            `,
                        },
                    },
                },
            },
        },
        "sort": [
            // 先按距離排序
            {
                "_geo_distance": map[string]interface{}{
                    "location": map[string]float64{
                        "lat": lat,
                        "lon": lng,
                    },
                    "order":         "asc",
                    "unit":          "km",
                    "distance_type": "arc",
                },
            },
            // 再按評分排序
            {
                "rating": map[string]string{
                    "order": "desc",
                },
            },
        ],
        "size": 20,
    }

    // 執行搜尋
    var buf bytes.Buffer
    if err := json.NewEncoder(&buf).Encode(query); err != nil {
        return nil, err
    }

    res, err := e.client.Search(
        e.client.Search.WithContext(ctx),
        e.client.Search.WithIndex("restaurants"),
        e.client.Search.WithBody(&buf),
    )
    if err != nil {
        return nil, err
    }
    defer res.Body.Close()

    // 解析結果
    var results struct {
        Hits struct {
            Hits []struct {
                Source Restaurant `json:"_source"`
                Sort   []float64  `json:"sort"`
            } `json:"hits"`
        } `json:"hits"`
    }

    if err := json.NewDecoder(res.Body).Decode(&results); err != nil {
        return nil, err
    }

    var restaurants []*Restaurant
    for _, hit := range results.Hits.Hits {
        restaurant := hit.Source
        restaurant.Distance = hit.Sort[0] // 第一個 sort 值是距離
        restaurants = append(restaurants, &restaurant)
    }

    return restaurants, nil
}
```

**Emma**：Elasticsearch 的優勢是什麼？

**Michael**：
- **複合查詢**：可以同時過濾距離、評分、價格、類別、營業時間等
- **全文搜尋**：可以搜尋餐廳名稱、菜單內容
- **聚合分析**：可以統計每個類別的餐廳數量
- **效能**：查詢時間 < 20ms（百萬級資料）

---

## Act 2: QuadTree - 動態空間分割

**場景**：台北市中心餐廳密集，郊區稀疏，如何優化空間索引？

### 2.1 對話：QuadTree 的優勢

**Sarah**：如果某些區域餐廳很密集（如東區），某些區域很稀疏（如郊區），用固定網格（Geohash）會不會浪費？

**Michael**：好問題！這就是 **QuadTree** 的優勢 - **自適應分割**。

### 2.2 QuadTree 實作

```go
// internal/spatial/quadtree.go
package spatial

import (
    "sync"
)

type QuadTree struct {
    boundary  *Rectangle
    capacity  int              // 每個節點最多存幾個點
    points    []*Restaurant
    divided   bool

    // 四個子節點
    northwest *QuadTree
    northeast *QuadTree
    southwest *QuadTree
    southeast *QuadTree

    mu sync.RWMutex
}

type Rectangle struct {
    CenterLat  float64
    CenterLng  float64
    HalfWidth  float64 // 緯度範圍的一半
    HalfHeight float64 // 經度範圍的一半
}

type Restaurant struct {
    ID       int64
    Name     string
    Lat      float64
    Lng      float64
    Rating   float64
    Distance float64 // 查詢時計算
}

// Contains 檢查點是否在矩形內
func (r *Rectangle) Contains(lat, lng float64) bool {
    return lat >= r.CenterLat-r.HalfWidth &&
           lat <= r.CenterLat+r.HalfWidth &&
           lng >= r.CenterLng-r.HalfHeight &&
           lng <= r.CenterLng+r.HalfHeight
}

// Intersects 檢查兩個矩形是否相交
func (r *Rectangle) Intersects(other *Rectangle) bool {
    return !(other.CenterLat-other.HalfWidth > r.CenterLat+r.HalfWidth ||
             other.CenterLat+other.HalfWidth < r.CenterLat-r.HalfWidth ||
             other.CenterLng-other.HalfHeight > r.CenterLng+r.HalfHeight ||
             other.CenterLng+other.HalfHeight < r.CenterLng-r.HalfHeight)
}

// NewQuadTree 建立 QuadTree
func NewQuadTree(boundary *Rectangle, capacity int) *QuadTree {
    return &QuadTree{
        boundary: boundary,
        capacity: capacity,
        points:   make([]*Restaurant, 0, capacity),
        divided:  false,
    }
}

// Insert 插入餐廳
func (qt *QuadTree) Insert(restaurant *Restaurant) bool {
    qt.mu.Lock()
    defer qt.mu.Unlock()

    // 1. 檢查是否在範圍內
    if !qt.boundary.Contains(restaurant.Lat, restaurant.Lng) {
        return false
    }

    // 2. 如果未達容量上限，直接插入
    if len(qt.points) < qt.capacity {
        qt.points = append(qt.points, restaurant)
        return true
    }

    // 3. 已達上限，分割成四個子節點
    if !qt.divided {
        qt.subdivide()
    }

    // 4. 遞迴插入到子節點
    if qt.northwest.Insert(restaurant) { return true }
    if qt.northeast.Insert(restaurant) { return true }
    if qt.southwest.Insert(restaurant) { return true }
    if qt.southeast.Insert(restaurant) { return true }

    return false
}

// subdivide 分割成四個子節點
func (qt *QuadTree) subdivide() {
    x := qt.boundary.CenterLat
    y := qt.boundary.CenterLng
    w := qt.boundary.HalfWidth / 2
    h := qt.boundary.HalfHeight / 2

    nw := &Rectangle{CenterLat: x + w, CenterLng: y - h, HalfWidth: w, HalfHeight: h}
    ne := &Rectangle{CenterLat: x + w, CenterLng: y + h, HalfWidth: w, HalfHeight: h}
    sw := &Rectangle{CenterLat: x - w, CenterLng: y - h, HalfWidth: w, HalfHeight: h}
    se := &Rectangle{CenterLat: x - w, CenterLng: y + h, HalfWidth: w, HalfHeight: h}

    qt.northwest = NewQuadTree(nw, qt.capacity)
    qt.northeast = NewQuadTree(ne, qt.capacity)
    qt.southwest = NewQuadTree(sw, qt.capacity)
    qt.southeast = NewQuadTree(se, qt.capacity)

    qt.divided = true

    // 將現有的點重新插入到子節點
    for _, point := range qt.points {
        qt.northwest.Insert(point)
        qt.northeast.Insert(point)
        qt.southwest.Insert(point)
        qt.southeast.Insert(point)
    }

    // 清空當前節點的點
    qt.points = nil
}

// QueryRange 範圍查詢
func (qt *QuadTree) QueryRange(searchArea *Rectangle) []*Restaurant {
    qt.mu.RLock()
    defer qt.mu.RUnlock()

    found := make([]*Restaurant, 0)

    // 1. 如果搜尋範圍與當前範圍不相交，直接返回
    if !qt.boundary.Intersects(searchArea) {
        return found
    }

    // 2. 檢查當前節點的所有點
    for _, p := range qt.points {
        if searchArea.Contains(p.Lat, p.Lng) {
            found = append(found, p)
        }
    }

    // 3. 遞迴搜尋子節點
    if qt.divided {
        found = append(found, qt.northwest.QueryRange(searchArea)...)
        found = append(found, qt.northeast.QueryRange(searchArea)...)
        found = append(found, qt.southwest.QueryRange(searchArea)...)
        found = append(found, qt.southeast.QueryRange(searchArea)...)
    }

    return found
}

// QueryCircle 圓形範圍查詢（更常用）
func (qt *QuadTree) QueryCircle(centerLat, centerLng, radiusKm float64) []*Restaurant {
    // 1. 先用矩形範圍查詢（QuadTree 優化）
    searchArea := &Rectangle{
        CenterLat:  centerLat,
        CenterLng:  centerLng,
        HalfWidth:  radiusKm / 111.0, // 約 111 km/度
        HalfHeight: radiusKm / 111.0,
    }

    candidates := qt.QueryRange(searchArea)

    // 2. 精確過濾（計算實際距離）
    results := make([]*Restaurant, 0)
    for _, restaurant := range candidates {
        distance := haversineDistance(centerLat, centerLng, restaurant.Lat, restaurant.Lng)
        if distance <= radiusKm {
            restaurant.Distance = distance
            results = append(results, restaurant)
        }
    }

    // 3. 按距離排序
    sort.Slice(results, func(i, j int) bool {
        return results[i].Distance < results[j].Distance
    })

    return results
}
```

### 2.3 QuadTree 視覺化

```
台北市 QuadTree 範圍劃分示意：

Level 0 (整個台北市):
┌─────────────────────────────┐
│                             │
│         台北市              │
│      (100 家餐廳)           │
│                             │
└─────────────────────────────┘

Level 1 (分割成 4 個區域):
┌──────────────┬──────────────┐
│   北區       │   東北區     │
│  (15 家)     │  (20 家)     │
├──────────────┼──────────────┤
│   西南區     │   東區       │
│  (10 家)     │  (55 家)     │ ← 超過容量，繼續分割
└──────────────┴──────────────┘

Level 2 (東區再分割):
┌──────────────┬──────────────┐
│   北區(15)   │   東北(20)   │
├──────────────┼───────┬──────┤
│   西南(10)   │ 東-NW │東-NE │
│              │ (12)  │ (15) │
│              ├───────┼──────┤
│              │ 東-SW │東-SE │
│              │ (10)  │ (18) │
└──────────────┴───────┴──────┘
```

**David**：QuadTree 的優勢在哪？

**Michael**：
- **自適應**：密集區域自動細分，稀疏區域保持粗粒度
- **動態**：新增/刪除餐廳時可以動態調整
- **查詢效率**：O(log N) 時間複雜度

---

## Act 3: 複合排序 - 不只是距離

**場景**：使用者搜尋「附近的餐廳」，但希望看到「評分高」且「價格合理」的選項...

### 3.1 對話：排序策略

**Emma**：使用者不只在乎距離，還在乎評分、價格、是否營業中。我們要怎麼排序？

**Michael**：這需要**多因素加權排序**（Multi-factor Ranking）。

### 3.2 評分公式設計

```go
// internal/ranking/scorer.go
package ranking

type RankingScorer struct {
    weights *RankingWeights
}

type RankingWeights struct {
    Distance    float64 // 距離權重
    Rating      float64 // 評分權重
    ReviewCount float64 // 評論數權重
    PriceMatch  float64 // 價格匹配權重
    OpenNow     float64 // 營業中加分
}

// 預設權重
var DefaultWeights = &RankingWeights{
    Distance:    0.35,
    Rating:      0.30,
    ReviewCount: 0.15,
    PriceMatch:  0.10,
    OpenNow:     0.10,
}

// CalculateScore 計算餐廳綜合評分
func (r *RankingScorer) CalculateScore(restaurant *Restaurant, userLat, userLng float64, userPriceLevel int) float64 {
    // 1. 距離分數（越近越高，3km 為基準）
    distanceScore := calculateDistanceScore(restaurant.Distance, 3.0)

    // 2. 評分分數（5 星制歸一化）
    ratingScore := restaurant.Rating / 5.0

    // 3. 評論數分數（使用對數縮放，避免評論數過大主導排名）
    reviewScore := math.Log10(float64(restaurant.ReviewCount+1)) / math.Log10(1001) // 1000+ 評論滿分

    // 4. 價格匹配分數
    priceScore := calculatePriceMatchScore(restaurant.PriceLevel, userPriceLevel)

    // 5. 營業中加分
    openScore := 0.0
    if restaurant.OpenNow {
        openScore = 1.0
    }

    // 加權總分
    totalScore := distanceScore*r.weights.Distance +
                  ratingScore*r.weights.Rating +
                  reviewScore*r.weights.ReviewCount +
                  priceScore*r.weights.PriceMatch +
                  openScore*r.weights.OpenNow

    return totalScore
}

// calculateDistanceScore 距離評分函數
func calculateDistanceScore(distanceKm, maxDistanceKm float64) float64 {
    if distanceKm >= maxDistanceKm {
        return 0
    }
    // 使用反比例函數（距離越近，分數越高）
    return 1 - (distanceKm / maxDistanceKm)
}

// calculatePriceMatchScore 價格匹配評分
func calculatePriceMatchScore(restaurantPrice, userPrice int) float64 {
    diff := math.Abs(float64(restaurantPrice - userPrice))

    switch {
    case diff == 0:
        return 1.0  // 完全匹配
    case diff == 1:
        return 0.7  // 差 1 級
    case diff == 2:
        return 0.4  // 差 2 級
    default:
        return 0.1  // 差 3 級以上
    }
}
```

### 3.3 排序實作

```go
// internal/search/service.go
package search

type SearchService struct {
    esClient *ElasticsearchSearch
    scorer   *RankingScorer
}

type SearchRequest struct {
    Lat        float64
    Lng        float64
    Radius     float64  // km
    MinRating  float64
    PriceLevel int      // 1-4
    Category   string
    OpenNow    bool
    SortBy     string   // "distance", "rating", "best_match"
}

// Search 搜尋餐廳
func (s *SearchService) Search(ctx context.Context, req *SearchRequest) ([]*Restaurant, error) {
    // 1. 從 Elasticsearch 查詢候選餐廳
    restaurants, err := s.esClient.SearchNearby(ctx, req.Lat, req.Lng, req.Radius, req.MinRating)
    if err != nil {
        return nil, err
    }

    // 2. 計算每個餐廳的綜合評分
    for _, restaurant := range restaurants {
        restaurant.Score = s.scorer.CalculateScore(restaurant, req.Lat, req.Lng, req.PriceLevel)
    }

    // 3. 根據使用者選擇的排序方式排序
    switch req.SortBy {
    case "distance":
        sort.Slice(restaurants, func(i, j int) bool {
            return restaurants[i].Distance < restaurants[j].Distance
        })
    case "rating":
        sort.Slice(restaurants, func(i, j int) bool {
            return restaurants[i].Rating > restaurants[j].Rating
        })
    case "best_match":
        fallthrough
    default:
        sort.Slice(restaurants, func(i, j int) bool {
            return restaurants[i].Score > restaurants[j].Score
        })
    }

    return restaurants, nil
}
```

---

## Act 4: 評論系統

**場景**：使用者在餐廳用餐後，留下評論和照片...

### 4.1 評論資料結構

```go
// internal/reviews/model.go
package reviews

type Review struct {
    ID           int64     `json:"id"`
    RestaurantID int64     `json:"restaurant_id"`
    UserID       int64     `json:"user_id"`
    Rating       float64   `json:"rating"`      // 1-5 星
    Text         string    `json:"text"`
    Photos       []string  `json:"photos"`      // 照片 URLs
    Useful       int       `json:"useful"`      // 有用計數
    Funny        int       `json:"funny"`       // 有趣計數
    Cool         int       `json:"cool"`        // 酷計數
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
}

type ReviewSummary struct {
    RestaurantID  int64   `json:"restaurant_id"`
    AverageRating float64 `json:"average_rating"`
    TotalReviews  int     `json:"total_reviews"`
    Rating5Star   int     `json:"rating_5_star"`
    Rating4Star   int     `json:"rating_4_star"`
    Rating3Star   int     `json:"rating_3_star"`
    Rating2Star   int     `json:"rating_2_star"`
    Rating1Star   int     `json:"rating_1_star"`
}
```

### 4.2 評論提交與聚合

```go
// internal/reviews/service.go
package reviews

type ReviewService struct {
    db    *PostgreSQL
    cache *RedisClient
    queue *KafkaProducer
}

// SubmitReview 提交評論
func (r *ReviewService) SubmitReview(ctx context.Context, review *Review) error {
    // 1. 反垃圾檢查
    if r.isSpam(review) {
        return fmt.Errorf("spam detected")
    }

    // 2. 儲存評論
    err := r.db.ExecContext(ctx, `
        INSERT INTO reviews (restaurant_id, user_id, rating, text, photos, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
    `, review.RestaurantID, review.UserID, review.Rating, review.Text, pq.Array(review.Photos), time.Now())

    if err != nil {
        return err
    }

    // 3. 發送到 Kafka（異步更新聚合資料）
    event := &ReviewCreatedEvent{
        RestaurantID: review.RestaurantID,
        Rating:       review.Rating,
    }
    r.queue.Produce(ctx, "review-created", event)

    // 4. 清除快取
    cacheKey := fmt.Sprintf("restaurant:%d:reviews", review.RestaurantID)
    r.cache.Del(ctx, cacheKey)

    return nil
}

// UpdateRestaurantRating 更新餐廳平均評分（Kafka Consumer）
func (r *ReviewService) UpdateRestaurantRating(ctx context.Context, restaurantID int64) error {
    // 計算新的平均評分
    var summary ReviewSummary
    err := r.db.QueryRowContext(ctx, `
        SELECT
            restaurant_id,
            AVG(rating) as average_rating,
            COUNT(*) as total_reviews,
            SUM(CASE WHEN rating = 5 THEN 1 ELSE 0 END) as rating_5_star,
            SUM(CASE WHEN rating = 4 THEN 1 ELSE 0 END) as rating_4_star,
            SUM(CASE WHEN rating = 3 THEN 1 ELSE 0 END) as rating_3_star,
            SUM(CASE WHEN rating = 2 THEN 1 ELSE 0 END) as rating_2_star,
            SUM(CASE WHEN rating = 1 THEN 1 ELSE 0 END) as rating_1_star
        FROM reviews
        WHERE restaurant_id = ?
        GROUP BY restaurant_id
    `, restaurantID).Scan(
        &summary.RestaurantID,
        &summary.AverageRating,
        &summary.TotalReviews,
        &summary.Rating5Star,
        &summary.Rating4Star,
        &summary.Rating3Star,
        &summary.Rating2Star,
        &summary.Rating1Star,
    )

    if err != nil {
        return err
    }

    // 更新餐廳表
    _, err = r.db.ExecContext(ctx, `
        UPDATE restaurants
        SET rating = ?, review_count = ?
        WHERE id = ?
    `, summary.AverageRating, summary.TotalReviews, restaurantID)

    return err
}

// isSpam 反垃圾檢查
func (r *ReviewService) isSpam(review *Review) bool {
    // 1. 檢查評論長度
    if len(review.Text) < 10 {
        return true
    }

    // 2. 檢查是否包含違禁詞
    if r.containsBannedWords(review.Text) {
        return true
    }

    // 3. 檢查使用者評論頻率（1 分鐘內不能超過 3 次）
    key := fmt.Sprintf("user:%d:review_rate", review.UserID)
    count, _ := r.cache.Incr(ctx, key).Result()
    r.cache.Expire(ctx, key, 1*time.Minute)

    if count > 3 {
        return true
    }

    return false
}
```

---

## Act 5: 圖片上傳與 CDN

**場景**：使用者上傳餐廳照片，系統需要處理、壓縮、分發...

### 5.1 圖片上傳流程

```go
// internal/photos/service.go
package photos

type PhotoService struct {
    s3      *S3Client
    cdn     *CloudFrontClient
    imageProcessor *ImageProcessor
}

// UploadPhoto 上傳照片
func (p *PhotoService) UploadPhoto(ctx context.Context, userID int64, restaurantID int64, file io.Reader) (*Photo, error) {
    // 1. 讀取圖片
    img, format, err := image.Decode(file)
    if err != nil {
        return nil, fmt.Errorf("invalid image: %w", err)
    }

    // 2. 生成多種尺寸
    sizes := map[string]int{
        "original": 0,    // 原圖
        "large":    1200, // 大圖
        "medium":   800,  // 中圖
        "small":    400,  // 小圖
        "thumb":    150,  // 縮圖
    }

    photoID := uuid.New().String()
    urls := make(map[string]string)

    for sizeName, width := range sizes {
        // 調整大小
        resized := img
        if width > 0 {
            resized = p.imageProcessor.Resize(img, width)
        }

        // 壓縮（WebP 格式）
        var buf bytes.Buffer
        err = webp.Encode(&buf, resized, &webp.Options{Quality: 85})
        if err != nil {
            return nil, err
        }

        // 上傳到 S3
        key := fmt.Sprintf("photos/%d/%s/%s.webp", restaurantID, photoID, sizeName)
        err = p.s3.Upload(ctx, key, &buf, "image/webp")
        if err != nil {
            return nil, err
        }

        // CDN URL
        urls[sizeName] = fmt.Sprintf("https://cdn.yelp.com/%s", key)
    }

    // 3. 儲存到資料庫
    photo := &Photo{
        ID:           photoID,
        RestaurantID: restaurantID,
        UserID:       userID,
        URLs:         urls,
        CreatedAt:    time.Now(),
    }

    err = p.savePhoto(ctx, photo)
    if err != nil {
        return nil, err
    }

    return photo, nil
}
```

---

## Act 6: 快取策略

**場景**：熱門餐廳（如鼎泰豐）被大量查詢，如何降低資料庫壓力？

### 6.1 多層快取

```go
// internal/cache/multi_layer.go
package cache

type MultiLayerCache struct {
    l1 *LocalCache    // 記憶體快取（單機）
    l2 *RedisCache    // Redis 快取（分散式）
    db *PostgreSQL    // 資料庫
}

// GetRestaurant 取得餐廳資訊
func (m *MultiLayerCache) GetRestaurant(ctx context.Context, id int64) (*Restaurant, error) {
    key := fmt.Sprintf("restaurant:%d", id)

    // 1. 檢查 L1 快取（記憶體）
    if restaurant, found := m.l1.Get(key); found {
        return restaurant.(*Restaurant), nil
    }

    // 2. 檢查 L2 快取（Redis）
    var restaurant Restaurant
    data, err := m.l2.Get(ctx, key).Bytes()
    if err == nil {
        json.Unmarshal(data, &restaurant)

        // 寫入 L1
        m.l1.Set(key, &restaurant, 5*time.Minute)

        return &restaurant, nil
    }

    // 3. 從資料庫查詢
    err = m.db.QueryRowContext(ctx, `
        SELECT id, name, latitude, longitude, rating, review_count, price_level
        FROM restaurants
        WHERE id = ?
    `, id).Scan(&restaurant.ID, &restaurant.Name, &restaurant.Lat, &restaurant.Lng,
        &restaurant.Rating, &restaurant.ReviewCount, &restaurant.PriceLevel)

    if err != nil {
        return nil, err
    }

    // 4. 寫入快取
    data, _ = json.Marshal(&restaurant)
    m.l2.Set(ctx, key, data, 1*time.Hour)
    m.l1.Set(key, &restaurant, 5*time.Minute)

    return &restaurant, nil
}
```

**快取層級**：
```
請求流程：
1. L1 (Local Cache) - 5 分鐘 TTL - 命中率 60%
   ↓ Miss
2. L2 (Redis) - 1 小時 TTL - 命中率 35%
   ↓ Miss
3. Database (PostgreSQL) - 5%

總快取命中率：95%
```

---

## 總結

### 核心技術要點

1. **地理空間索引**
   - PostgreSQL + PostGIS（簡單、準確）
   - Redis Geo（快速、可擴展）
   - Elasticsearch（複合查詢）
   - QuadTree（自適應分割）

2. **複合排序**
   - 距離 + 評分 + 評論數 + 價格 + 營業時間
   - 多因素加權評分
   - 個人化排序

3. **評論系統**
   - 異步聚合（Kafka）
   - 反垃圾機制
   - 評分分佈統計

4. **圖片處理**
   - 多尺寸生成
   - WebP 壓縮
   - CDN 分發

5. **快取策略**
   - L1 (記憶體) + L2 (Redis)
   - 快取命中率 > 95%

### 延伸思考

**Emma**：如果要加入「訂位」功能，要怎麼設計？

**Michael**：需要：
- **時段管理**：每個餐廳的可訂位時段
- **座位管理**：追蹤已訂位和剩餘座位
- **併發控制**：防止超訂（分散式鎖）
- **取消與候補**：取消訂位後釋放座位

這是另一個複雜的系統，值得深入研究！

---

**下一章預告**：Food Delivery (UberEats) - 外送平台（訂單匹配、調度優化、多點取送）
