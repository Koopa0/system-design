# Google Maps - 地圖導航系統技術文件

## 目錄
- [系統架構](#系統架構)
- [資料庫設計](#資料庫設計)
- [API 文件](#api-文件)
- [地圖瓦片系統](#地圖瓦片系統)
- [路徑規劃演算法](#路徑規劃演算法)
- [效能指標](#效能指標)
- [成本分析](#成本分析)
- [部署架構](#部署架構)

---

## 系統架構

### 高階架構圖

```
┌────────────────────────────────────────────────────────────┐
│                        用戶端                               │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                 │
│  │ Web App  │  │Mobile App│  │ Car Nav  │                 │
│  └─────┬────┘  └─────┬────┘  └─────┬────┘                 │
└────────┼─────────────┼─────────────┼───────────────────────┘
         │             │             │
         └─────────────┴─────────────┘
                       │
              ┌────────▼─────────┐
              │   CDN (CloudFront│
              │   地圖瓦片快取)   │
              └────────┬─────────┘
                       │
              ┌────────▼─────────┐
              │  Load Balancer   │
              │   (ALB/Nginx)    │
              └────────┬─────────┘
                       │
       ┌───────────────┼───────────────┐
       │               │               │
┌──────▼──────┐ ┌─────▼──────┐ ┌─────▼──────┐
│ Tile Service│ │Route Service│ │Search Svc  │
│  (瓦片)     │ │ (路徑規劃)  │ │ (地點搜尋) │
└──────┬──────┘ └─────┬──────┘ └─────┬──────┘
       │              │               │
       │      ┌───────▼───────┐       │
       │      │Traffic Service│       │
       │      │  (路況分析)   │       │
       │      └───────┬───────┘       │
       │              │               │
       └──────────────┼───────────────┘
                      │
       ┌──────────────┼──────────────┐
       │              │              │
┌──────▼─────┐ ┌─────▼────┐ ┌──────▼──────┐
│     S3     │ │PostgreSQL│ │Elasticsearch│
│  (瓦片儲存)│ │(路網/地點)│ │ (地點索引)  │
└────────────┘ └──────────┘ └─────────────┘
       │              │              │
┌──────▼─────┐ ┌─────▼────┐ ┌──────▼──────┐
│   Redis    │ │  Kafka   │ │  InfluxDB   │
│  (快取)    │ │(GPS流)   │ │ (路況時序)  │
└────────────┘ └──────────┘ └─────────────┘
```

### 微服務架構

| 服務 | 職責 | 技術棧 | QPS |
|------|------|--------|-----|
| **Tile Service** | 地圖瓦片渲染與分發 | Go, S3, CloudFront | 500K |
| **Routing Service** | 路徑規劃（A*） | Go, PostgreSQL | 50K |
| **Traffic Service** | 路況收集與預測 | Go, Kafka, InfluxDB | 100K |
| **Navigation Service** | 實時導航 | Go, WebSocket | 10K |
| **Geocoding Service** | 地理編碼 | Go, Elasticsearch | 20K |
| **Search Service** | 地點搜尋 | Go, Elasticsearch | 30K |
| **Places Service** | 地點資訊管理 | Go, PostgreSQL | 10K |

---

## 資料庫設計

### PostgreSQL Schema

#### 1. 道路節點表（Road Nodes）

```sql
CREATE TABLE road_nodes (
    id BIGSERIAL PRIMARY KEY,
    latitude DECIMAL(10,8) NOT NULL,
    longitude DECIMAL(11,8) NOT NULL,
    node_type VARCHAR(50),     -- 'intersection', 'junction', 'endpoint'

    -- 地理空間索引
    location GEOGRAPHY(POINT, 4326),

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 地理空間索引（PostGIS）
CREATE INDEX idx_road_nodes_location ON road_nodes USING GIST (location);
CREATE INDEX idx_road_nodes_lat_lng ON road_nodes(latitude, longitude);
```

#### 2. 道路邊表（Road Edges）

```sql
CREATE TABLE road_edges (
    id BIGSERIAL PRIMARY KEY,
    from_node_id BIGINT NOT NULL REFERENCES road_nodes(id),
    to_node_id BIGINT NOT NULL REFERENCES road_nodes(id),

    -- 道路屬性
    road_name VARCHAR(255),
    road_type VARCHAR(50),        -- 'highway', 'main_road', 'street', 'alley'
    one_way BOOLEAN DEFAULT FALSE,
    toll_road BOOLEAN DEFAULT FALSE,

    -- 距離與時間
    distance DECIMAL(10,2),       -- 公尺
    speed_limit INT,              -- km/h
    time_estimate INT,            -- 秒

    -- 幾何形狀（LineString）
    geometry GEOGRAPHY(LINESTRING, 4326),

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_road_edges_from_node ON road_edges(from_node_id);
CREATE INDEX idx_road_edges_to_node ON road_edges(to_node_id);
CREATE INDEX idx_road_edges_geometry ON road_edges USING GIST (geometry);
CREATE INDEX idx_road_edges_road_type ON road_edges(road_type);
```

#### 3. 地點表（Places）

```sql
CREATE TABLE places (
    id BIGSERIAL PRIMARY KEY,

    -- 基本資訊
    name VARCHAR(255) NOT NULL,
    name_en VARCHAR(255),
    address TEXT,
    city VARCHAR(100),
    country VARCHAR(100),
    postal_code VARCHAR(20),

    -- 座標
    latitude DECIMAL(10,8) NOT NULL,
    longitude DECIMAL(11,8) NOT NULL,
    location GEOGRAPHY(POINT, 4326),

    -- 分類
    place_type VARCHAR(50),       -- 'restaurant', 'hotel', 'gas_station', 'landmark'
    category VARCHAR(100),        -- 'chinese_food', 'cafe', 'convenience_store'

    -- 營業資訊
    phone VARCHAR(50),
    website VARCHAR(255),
    opening_hours JSONB,

    -- 評分
    rating DECIMAL(3,2),
    review_count INT DEFAULT 0,
    price_level INT,              -- 1-4 ($ to $$$$)

    -- 屬性
    verified BOOLEAN DEFAULT FALSE,
    permanently_closed BOOLEAN DEFAULT FALSE,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_places_location ON places USING GIST (location);
CREATE INDEX idx_places_place_type ON places(place_type);
CREATE INDEX idx_places_rating ON places(rating DESC);
CREATE INDEX idx_places_name ON places USING GIN (to_tsvector('english', name));

-- 全文搜尋索引
CREATE INDEX idx_places_fulltext ON places
    USING GIN (to_tsvector('english', name || ' ' || COALESCE(address, '')));
```

#### 4. 路況資料表（Traffic Data）

```sql
-- 使用 TimescaleDB 擴展處理時序資料
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE traffic_data (
    time TIMESTAMPTZ NOT NULL,
    edge_id BIGINT NOT NULL REFERENCES road_edges(id),

    -- 速度資料
    average_speed DECIMAL(5,2),   -- km/h
    speed_limit DECIMAL(5,2),
    speed_ratio DECIMAL(3,2),     -- average_speed / speed_limit

    -- 路況等級
    congestion_level VARCHAR(20), -- 'free', 'moderate', 'heavy', 'severe'

    -- 統計
    sample_count INT,             -- 樣本數量

    PRIMARY KEY (time, edge_id)
);

-- 轉換為時序表（按天分區）
SELECT create_hypertable('traffic_data', 'time', chunk_time_interval => INTERVAL '1 day');

-- 建立索引
CREATE INDEX idx_traffic_data_edge_time ON traffic_data(edge_id, time DESC);
CREATE INDEX idx_traffic_data_congestion ON traffic_data(congestion_level);

-- 自動保留策略（保留 90 天）
SELECT add_retention_policy('traffic_data', INTERVAL '90 days');
```

#### 5. GPS 追蹤資料（GPS Traces）

```sql
CREATE TABLE gps_traces (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT,

    latitude DECIMAL(10,8) NOT NULL,
    longitude DECIMAL(11,8) NOT NULL,
    location GEOGRAPHY(POINT, 4326),

    speed DECIMAL(5,2),           -- km/h
    bearing DECIMAL(5,2),         -- 0-360 度
    accuracy DECIMAL(6,2),        -- 公尺

    timestamp TIMESTAMPTZ NOT NULL,

    -- 地圖匹配後的道路
    matched_edge_id BIGINT REFERENCES road_edges(id),

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 分區表（按月）
CREATE TABLE gps_traces_2024_01 PARTITION OF gps_traces
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');

CREATE INDEX idx_gps_traces_user_time ON gps_traces(user_id, timestamp DESC);
CREATE INDEX idx_gps_traces_timestamp ON gps_traces(timestamp);
```

### Redis 資料結構

#### 1. 瓦片快取（Tile Cache）

```redis
# 儲存瓦片圖片（Binary）
SET tile:15:27441:13563 <binary_data> EX 86400

# 瓦片存在性檢查（Bloom Filter）
BF.ADD tiles:exists "15:27441:13563"
BF.EXISTS tiles:exists "15:27441:13563"
```

#### 2. 路徑規劃快取

```redis
# 快取熱門路線（起點-終點 hash）
SET route:hash:abc123def456 '{
  "distance": 125000,
  "duration": 5400,
  "waypoints": [...],
  "cached_at": 1634567890
}' EX 3600

# 使用 Geo 索引快速查詢起點/終點
GEOADD routes:origins 121.5170 25.0478 "route:abc123"
```

#### 3. 即時路況

```redis
# 道路當前速度
HSET traffic:edge:12345 speed "45.5"
HSET traffic:edge:12345 congestion "moderate"
HSET traffic:edge:12345 updated_at "1634567890"
EXPIRE traffic:edge:12345 300

# 路況事件（事故、施工）
ZADD traffic:events 1634567890 "accident:location:25.0478,121.5170"
```

### Elasticsearch 索引

```json
{
  "mappings": {
    "properties": {
      "name": {
        "type": "text",
        "analyzer": "standard",
        "fields": {
          "keyword": { "type": "keyword" },
          "autocomplete": {
            "type": "text",
            "analyzer": "autocomplete"
          }
        }
      },
      "address": {
        "type": "text"
      },
      "location": {
        "type": "geo_point"
      },
      "place_type": {
        "type": "keyword"
      },
      "category": {
        "type": "keyword"
      },
      "rating": {
        "type": "float"
      },
      "review_count": {
        "type": "integer"
      },
      "opening_hours": {
        "type": "object",
        "enabled": false
      }
    }
  }
}
```

---

## API 文件

### 1. 取得地圖瓦片

**Endpoint**: `GET /api/v1/tiles/{z}/{x}/{y}.png`

**Parameters**:
- `z`: 縮放層級 (0-21)
- `x`: X 座標
- `y`: Y 座標

**Example**:
```
GET /api/v1/tiles/15/27441/13563.png
```

**Response**: PNG 圖片（256×256 像素）

**Headers**:
```
Content-Type: image/png
Cache-Control: public, max-age=2592000  # 30 days
ETag: "abc123def456"
```

### 2. 路徑規劃

**Endpoint**: `POST /api/v1/directions`

**Request**:
```json
{
  "origin": {
    "latitude": 25.0478,
    "longitude": 121.5170
  },
  "destination": {
    "latitude": 25.0330,
    "longitude": 121.5654
  },
  "mode": "driving",              // "driving", "walking", "bicycling", "transit"
  "avoid": ["tolls", "highways"], // 可選
  "departure_time": "now",        // 或 Unix timestamp
  "alternatives": true            // 是否返回多條路線
}
```

**Response**:
```json
{
  "status": "OK",
  "routes": [
    {
      "summary": "建國高架道路",
      "distance": 5200,           // 公尺
      "duration": 780,            // 秒
      "duration_in_traffic": 1080,// 考慮路況
      "polyline": "encoded_polyline_string",
      "bounds": {
        "northeast": {"lat": 25.0478, "lng": 121.5654},
        "southwest": {"lat": 25.0330, "lng": 121.5170}
      },
      "legs": [
        {
          "distance": 5200,
          "duration": 780,
          "start_location": {"lat": 25.0478, "lng": 121.5170},
          "end_location": {"lat": 25.0330, "lng": 121.5654},
          "steps": [
            {
              "distance": 200,
              "duration": 30,
              "html_instructions": "向<b>東</b>朝<b>市民大道</b>前進",
              "polyline": "...",
              "maneuver": "turn-right"
            }
          ]
        }
      ]
    }
  ]
}
```

### 3. 地理編碼

**Endpoint**: `GET /api/v1/geocode`

**Parameters**:
- `address`: 地址字串（URL encoded）

**Example**:
```
GET /api/v1/geocode?address=%E5%8F%B0%E5%8C%97%E8%BB%8A%E7%AB%99
```

**Response**:
```json
{
  "status": "OK",
  "results": [
    {
      "place_id": "ChIJFx8gKkypQjQRHBwhTMGcDhE",
      "formatted_address": "台北市中正區北平西路3號",
      "geometry": {
        "location": {"lat": 25.0478, "lng": 121.5170},
        "location_type": "ROOFTOP",
        "viewport": {
          "northeast": {"lat": 25.0491, "lng": 121.5183},
          "southwest": {"lat": 25.0465, "lng": 121.5157}
        }
      },
      "place_type": "train_station",
      "address_components": [
        {"long_name": "台北車站", "short_name": "台北車站", "types": ["point_of_interest"]},
        {"long_name": "中正區", "short_name": "中正區", "types": ["sublocality"]},
        {"long_name": "台北市", "short_name": "台北市", "types": ["locality"]}
      ]
    }
  ]
}
```

### 4. 反地理編碼

**Endpoint**: `GET /api/v1/geocode/reverse`

**Parameters**:
- `lat`: 緯度
- `lng`: 經度

**Example**:
```
GET /api/v1/geocode/reverse?lat=25.0478&lng=121.5170
```

**Response**: 同地理編碼

### 5. 地點搜尋

**Endpoint**: `GET /api/v1/places/search`

**Parameters**:
- `query`: 搜尋關鍵字
- `lat`, `lng`: 中心點座標
- `radius`: 搜尋半徑（公尺），預設 5000
- `type`: 地點類型過濾

**Example**:
```
GET /api/v1/places/search?query=咖啡廳&lat=25.0478&lng=121.5170&radius=1000
```

**Response**:
```json
{
  "status": "OK",
  "results": [
    {
      "place_id": "ChIJ...",
      "name": "星巴克 台北車站店",
      "vicinity": "台北市中正區忠孝西路一段49號",
      "geometry": {
        "location": {"lat": 25.0475, "lng": 121.5168}
      },
      "rating": 4.2,
      "user_ratings_total": 523,
      "price_level": 2,
      "opening_hours": {
        "open_now": true
      },
      "photos": [
        {"photo_reference": "abc123..."}
      ]
    }
  ]
}
```

### 6. 搜尋自動補全

**Endpoint**: `GET /api/v1/places/autocomplete`

**Parameters**:
- `input`: 輸入文字
- `lat`, `lng`: 使用者位置（用於排序）

**Example**:
```
GET /api/v1/places/autocomplete?input=台北&lat=25.0478&lng=121.5170
```

**Response**:
```json
{
  "status": "OK",
  "predictions": [
    {
      "description": "台北車站",
      "place_id": "ChIJ...",
      "structured_formatting": {
        "main_text": "台北車站",
        "secondary_text": "台北市中正區"
      },
      "distance_meters": 50
    },
    {
      "description": "台北101",
      "place_id": "ChIJ...",
      "structured_formatting": {
        "main_text": "台北101",
        "secondary_text": "台北市信義區"
      },
      "distance_meters": 5200
    }
  ]
}
```

---

## 地圖瓦片系統

### Web Mercator 投影

Google Maps 使用 **Web Mercator 投影**（EPSG:3857），將球面座標投影到平面。

**公式**：

```
給定經緯度 (lat, lng)，縮放層級 z

瓦片數量 n = 2^z

X座標 = floor((lng + 180) / 360 * n)

Y座標 = floor((1 - ln(tan(lat) + sec(lat)) / π) / 2 * n)
```

### 縮放層級對照表

| Zoom Level | 地球寬度（像素）| 瓦片數量 | 1像素 ≈ | 適用場景 |
|------------|---------------|---------|---------|----------|
| 0 | 256 | 1 | 156 km | 世界地圖 |
| 5 | 8,192 | 1,024 | 4.9 km | 國家 |
| 10 | 262,144 | 1,048,576 | 152 m | 城市 |
| 15 | 8,388,608 | 1,073,741,824 | 4.8 m | 街區 |
| 18 | 67,108,864 | 68,719,476,736 | 60 cm | 建築物 |
| 21 | 536,870,912 | 4.4 兆 | 7.5 cm | 超高精度 |

### 瓦片命名規範

Google Maps 使用 `{z}/{x}/{y}` 命名規範：

```
https://mt1.google.com/vt?x=27441&y=13563&z=15

其中：
- mt1：地圖瓦片伺服器（mt0, mt1, mt2, mt3 輪流）
- x, y：瓦片座標
- z：縮放層級
```

### 瓦片生成流程

```
1. 原始資料（OSM XML/PBF）
   ↓
2. 資料處理（osm2pgsql）
   ↓
3. 匯入 PostgreSQL + PostGIS
   ↓
4. 瓦片渲染（Mapnik / Tilestache）
   ├─ 繪製背景（水域、陸地）
   ├─ 繪製道路（高速公路 → 小巷）
   ├─ 繪製建築物
   └─ 繪製文字標籤
   ↓
5. 輸出 PNG/WebP
   ↓
6. 上傳到 S3
   ↓
7. CDN 分發（CloudFront）
```

### 瓦片壓縮

```bash
# WebP 格式（比 PNG 小 25-35%）
cwebp -q 80 tile.png -o tile.webp

# 檔案大小比較
tile.png    → 45 KB
tile.webp   → 30 KB (節省 33%)
```

---

## 路徑規劃演算法

### Dijkstra vs A* 性能比較

**測試場景**：台北 → 高雄（約 350 km）

| 指標 | Dijkstra | A* | Bidirectional A* |
|------|----------|-----|------------------|
| **訪問節點數** | 500,000 | 30,000 | 15,000 |
| **執行時間** | 800 ms | 120 ms | 60 ms |
| **記憶體使用** | 50 MB | 8 MB | 10 MB |
| **路徑品質** | 最優 | 最優 | 最優 |

### A* 啟發式函數選擇

```go
// 1. 歐氏距離（不準確，因為地球是球面）
func euclideanDistance(lat1, lng1, lat2, lng2 float64) float64 {
    dlat := lat2 - lat1
    dlng := lng2 - lng1
    return math.Sqrt(dlat*dlat + dlng*dlng) * 111000 // 約 111 km/度
}

// 2. 曼哈頓距離
func manhattanDistance(lat1, lng1, lat2, lng2 float64) float64 {
    dlat := math.Abs(lat2 - lat1)
    dlng := math.Abs(lng2 - lng1)
    return (dlat + dlng) * 111000
}

// 3. Haversine 公式（最準確）✓
func haversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
    const R = 6371000 // 地球半徑（公尺）

    φ1 := lat1 * math.Pi / 180
    φ2 := lat2 * math.Pi / 180
    Δφ := (lat2 - lat1) * math.Pi / 180
    Δλ := (lng2 - lng1) * math.Pi / 180

    a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
         math.Cos(φ1)*math.Cos(φ2)*
         math.Sin(Δλ/2)*math.Sin(Δλ/2)

    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

    return R * c
}
```

### Contraction Hierarchies（進階優化）

**Google Maps 實際使用的演算法**：

```
預處理階段（離線）：
1. 計算節點重要性（Importance）
2. 按重要性排序（Highway > Main Road > Street）
3. 建立 Shortcut（捷徑邊）
4. 建立層級結構

查詢階段（線上）：
1. 從起點向上搜尋（只走更重要的節點）
2. 從終點向上搜尋
3. 兩邊相遇時停止
4. 重建完整路徑

效能提升：
- 查詢時間：120ms → 5ms（快 24 倍）
- 訪問節點：30,000 → 500（減少 98%）
```

---

## 效能指標

### 系統容量

| 指標 | 數值 | 備註 |
|------|------|------|
| **月活躍用戶** | 10 億 | 全球 |
| **每日路徑查詢** | 5 億次 | 平均每秒 5,787 次 |
| **瓦片請求** | 100 億次/日 | 平均每秒 115,740 次 |
| **GPS 資料點** | 500 億/日 | 路況眾包資料 |
| **儲存空間** | 5 PB | 地圖資料 + 街景 |

### API 延遲要求

| API | P50 | P95 | P99 |
|-----|-----|-----|-----|
| **瓦片載入** | <20ms | <50ms | <100ms |
| **路徑規劃** | <100ms | <300ms | <500ms |
| **地點搜尋** | <50ms | <150ms | <300ms |
| **地理編碼** | <80ms | <200ms | <400ms |
| **即時路況** | <30ms | <80ms | <150ms |

### 資料庫效能

```sql
-- 路徑查詢優化（使用 PostGIS）
EXPLAIN ANALYZE
SELECT e.id, e.from_node_id, e.to_node_id, e.distance
FROM road_edges e
WHERE ST_DWithin(
    e.geometry,
    ST_SetSRID(ST_MakePoint(121.5170, 25.0478), 4326)::geography,
    1000
);

-- 執行時間：12ms
-- 使用索引：idx_road_edges_geometry
```

---

## 成本分析

### 全球規模成本估算

**假設條件**：
- 月活躍用戶：10 億
- 每用戶每月瓦片請求：100 次
- 每用戶每月路徑查詢：10 次

| 類別 | 項目 | 月費用 (USD) |
|------|------|-------------|
| **CDN** | 100 PB 流量 | $5,000,000 |
| **運算** | 10,000 台 c5.2xlarge | $2,000,000 |
| **儲存（S3）** | 5 PB 地圖瓦片 | $100,000 |
| **資料庫** | PostgreSQL (100 實例) | $500,000 |
| **Redis** | 200 節點集群 | $300,000 |
| **Elasticsearch** | 100 節點 | $200,000 |
| **Kafka** | 50 節點（GPS 流） | $150,000 |
| **InfluxDB** | 時序資料庫 | $100,000 |
| **頻寬** | 50 PB/月 | $1,000,000 |
| **地圖資料** | 授權、街景車 | $1,000,000 |
| **監控** | Datadog + Prometheus | $100,000 |
| **總計** | | **$10,450,000/月** |

**年度成本**：約 **$125,000,000（約 NT$ 40 億）**

### 台灣區域成本估算

**假設條件**：
- 月活躍用戶：500 萬
- 每日路徑查詢：200 萬次

| 項目 | 規格 | 月費用 (NT$) |
|------|------|--------------|
| CDN（CloudFront）| 500 TB 流量 | 750,000 |
| EC2（路徑規劃）| 20 × c5.xlarge | 200,000 |
| RDS PostgreSQL | r5.2xlarge Multi-AZ | 150,000 |
| ElastiCache Redis | 10 × r5.large | 100,000 |
| S3 儲存 | 50 TB 瓦片 | 30,000 |
| Elasticsearch | 5 節點 | 80,000 |
| 頻寬 | 300 TB | 180,000 |
| 監控 | CloudWatch + Datadog | 40,000 |
| **總計** | | **1,530,000/月** |

### 成本優化策略

| 優化項目 | 原成本 | 優化後 | 節省 | 方法 |
|----------|--------|--------|------|------|
| CDN 流量 | 750,000 | 525,000 | 225,000 | WebP 格式、壓縮 |
| 路徑快取 | 200,000 | 120,000 | 80,000 | 熱門路線快取 |
| 瓦片儲存 | 30,000 | 24,000 | 6,000 | 冷門區域 On-Demand |
| **總計** | 1,530,000 | **1,219,000** | **311,000** | **節省 20%** |

---

## 部署架構

### AWS 全球部署

```yaml
# 多區域部署
regions:
  - us-east-1 (N. Virginia)      # 美國東岸
  - us-west-1 (N. California)    # 美國西岸
  - eu-west-1 (Ireland)          # 歐洲
  - ap-northeast-1 (Tokyo)       # 亞太（最近台灣）
  - ap-southeast-1 (Singapore)   # 東南亞

# 每個區域的架構
per_region:
  vpc:
    cidr: 10.0.0.0/16
    availability_zones: 3

  services:
    tile_service:
      type: ECS Fargate
      instances: 50+
      auto_scaling: true

    routing_service:
      type: ECS Fargate
      instances: 20+

    traffic_service:
      type: ECS Fargate
      instances: 30+

  databases:
    postgresql:
      engine: Aurora PostgreSQL
      instance_class: r5.4xlarge
      multi_az: true
      read_replicas: 3

    redis:
      type: ElastiCache
      node_type: r5.2xlarge
      cluster_mode: enabled
      shards: 6
      replicas: 2

  storage:
    s3_tiles:
      storage_class: Standard
      lifecycle: Glacier after 90 days

  cdn:
    type: CloudFront
    price_class: All
    cache_behavior:
      ttl: 2592000  # 30 days
```

### Kubernetes 部署

```yaml
# routing-service.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: routing-service
spec:
  replicas: 20
  selector:
    matchLabels:
      app: routing-service
  template:
    metadata:
      labels:
        app: routing-service
    spec:
      containers:
      - name: routing-service
        image: maps/routing-service:v2.5.0
        ports:
        - containerPort: 8080
        resources:
          requests:
            cpu: "4"
            memory: "8Gi"
          limits:
            cpu: "8"
            memory: "16Gi"
        env:
        - name: DB_HOST
          valueFrom:
            secretKeyRef:
              name: db-secret
              key: host
        - name: REDIS_HOST
          valueFrom:
            configMapKeyRef:
              name: redis-config
              key: host
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5

---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: routing-service-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: routing-service
  minReplicas: 20
  maxReplicas: 100
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Pods
    pods:
      metric:
        name: http_requests_per_second
      target:
        type: AverageValue
        averageValue: "1000"
```

---

## 監控與告警

### Prometheus Metrics

```go
var (
    // 瓦片請求
    tileRequests = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "tile_requests_total",
            Help: "Total number of tile requests",
        },
        []string{"zoom", "cache_hit"},
    )

    // 路徑規劃延遲
    routingLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "routing_latency_seconds",
            Help: "Latency of route planning",
            Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
        },
        []string{"algorithm"},
    )

    // 路況資料點
    trafficDataPoints = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "traffic_data_points_total",
            Help: "Total GPS data points collected",
        },
    )
)
```

### Grafana Dashboard

**關鍵指標**：
- QPS（每秒請求數）
- P95/P99 延遲
- 瓦片快取命中率
- 路徑規劃成功率
- GPS 資料流量
- 資料庫連線數

---

## 安全性

### 1. API 限流

```go
// 每個 IP 每分鐘 60 次請求
rateLimiter := rate.NewLimiter(rate.Every(time.Second), 1)
```

### 2. API Key 驗證

```http
GET /api/v1/directions?key=YOUR_API_KEY&...
```

### 3. HTTPS/TLS 加密

所有 API 強制使用 HTTPS。

---

## 延伸閱讀

### 開源專案

- [OSRM](https://github.com/Project-OSRM/osrm-backend) - 開源路徑規劃引擎
- [Valhalla](https://github.com/valhalla/valhalla) - Mapbox 開源路徑引擎
- [Mapnik](https://github.com/mapnik/mapnik) - 地圖瓦片渲染
- [OpenStreetMap](https://www.openstreetmap.org/) - 開源地圖資料

### 技術論文

- [Contraction Hierarchies](https://arxiv.org/abs/0803.0319) - 快速路徑查詢
- [Hidden Markov Model for Map Matching](https://www.microsoft.com/en-us/research/publication/hidden-markov-map-matching-noise-sparseness/)

---

**版本**: v1.0.0
**最後更新**: 2024-10-18
**維護者**: Maps Platform Team
