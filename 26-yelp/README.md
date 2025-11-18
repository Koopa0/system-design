# Yelp - 附近的餐廳搜尋系統技術文件

## 目錄
- [系統架構](#系統架構)
- [資料庫設計](#資料庫設計)
- [API 文件](#api-文件)
- [地理空間索引](#地理空間索引)
- [排序演算法](#排序演算法)
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
│  │ Web App  │  │Mobile App│  │   API    │                 │
│  └─────┬────┘  └─────┬────┘  └─────┬────┘                 │
└────────┼─────────────┼─────────────┼───────────────────────┘
         │             │             │
         └─────────────┴─────────────┘
                       │
              ┌────────▼─────────┐
              │   CDN (圖片)     │
              └────────┬─────────┘
                       │
              ┌────────▼─────────┐
              │  Load Balancer   │
              └────────┬─────────┘
                       │
       ┌───────────────┼───────────────┐
       │               │               │
┌──────▼──────┐ ┌─────▼──────┐ ┌─────▼──────┐
│Search Service│ │Review Svc  │ │Photo Svc   │
│ (地點搜尋)  │ │ (評論)     │ │ (圖片)     │
└──────┬──────┘ └─────┬──────┘ └─────┬──────┘
       │              │               │
       │      ┌───────▼───────┐       │
       │      │ Restaurant Svc│       │
       │      │  (餐廳管理)   │       │
       │      └───────┬───────┘       │
       │              │               │
       └──────────────┼───────────────┘
                      │
       ┌──────────────┼──────────────┐
       │              │              │
┌──────▼─────┐ ┌─────▼────┐ ┌──────▼──────┐
│Elasticsearch│ │PostgreSQL│ │     S3      │
│ (地點索引)  │ │(餐廳/評論)│ │  (圖片)     │
└────────────┘ └──────────┘ └─────────────┘
       │              │              │
┌──────▼─────┐ ┌─────▼────┐ ┌──────▼──────┐
│   Redis    │ │  Kafka   │ │ CloudFront  │
│  (快取)    │ │(事件流)  │ │   (CDN)     │
└────────────┘ └──────────┘ └─────────────┘
```

### 微服務架構

| 服務 | 職責 | 技術棧 | QPS |
|------|------|--------|-----|
| **Search Service** | 地點搜尋、過濾、排序 | Go, Elasticsearch | 50K |
| **Restaurant Service** | 餐廳資訊管理 | Go, PostgreSQL | 20K |
| **Review Service** | 評論管理、聚合 | Go, PostgreSQL, Kafka | 10K |
| **Photo Service** | 圖片上傳、處理、CDN | Go, S3, CloudFront | 5K |
| **User Service** | 使用者管理、認證 | Go, PostgreSQL | 15K |
| **Notification Service** | 通知推送 | Go, Firebase | 5K |

---

## 資料庫設計

### PostgreSQL Schema

#### 1. 餐廳表（Restaurants）

```sql
CREATE TABLE restaurants (
    id BIGSERIAL PRIMARY KEY,

    -- 基本資訊
    name VARCHAR(255) NOT NULL,
    name_en VARCHAR(255),
    description TEXT,

    -- 地理位置
    latitude DECIMAL(10,8) NOT NULL,
    longitude DECIMAL(11,8) NOT NULL,
    location GEOGRAPHY(POINT, 4326),  -- PostGIS

    address TEXT,
    city VARCHAR(100),
    state VARCHAR(100),
    country VARCHAR(100),
    postal_code VARCHAR(20),

    -- 分類
    category VARCHAR(100),            -- 'restaurant', 'cafe', 'bar'
    cuisine_type VARCHAR(100),        -- 'chinese', 'japanese', 'italian'
    tags TEXT[],                      -- ['vegetarian', 'outdoor_seating']

    -- 營業資訊
    phone VARCHAR(50),
    website VARCHAR(255),
    price_level INT,                  -- 1-4 ($ to $$$$)
    opening_hours JSONB,              -- 每日營業時間

    -- 評分統計
    rating DECIMAL(3,2) DEFAULT 0.00,
    review_count INT DEFAULT 0,

    -- 屬性
    verified BOOLEAN DEFAULT FALSE,
    claimed BOOLEAN DEFAULT FALSE,    -- 店家是否認領
    permanently_closed BOOLEAN DEFAULT FALSE,

    -- 社交媒體
    facebook_url VARCHAR(255),
    instagram_url VARCHAR(255),

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 索引
CREATE INDEX idx_restaurants_location ON restaurants USING GIST (location);
CREATE INDEX idx_restaurants_category ON restaurants(category);
CREATE INDEX idx_restaurants_cuisine ON restaurants(cuisine_type);
CREATE INDEX idx_restaurants_rating ON restaurants(rating DESC);
CREATE INDEX idx_restaurants_price ON restaurants(price_level);
CREATE INDEX idx_restaurants_name ON restaurants USING GIN (to_tsvector('english', name));

-- 複合索引（常用查詢組合）
CREATE INDEX idx_restaurants_location_rating ON restaurants
    USING GIST (location) INCLUDE (rating, review_count);
```

**營業時間 JSON 格式**：

```json
{
  "monday": {"open": "11:00", "close": "22:00"},
  "tuesday": {"open": "11:00", "close": "22:00"},
  "wednesday": {"open": "11:00", "close": "22:00"},
  "thursday": {"open": "11:00", "close": "22:00"},
  "friday": {"open": "11:00", "close": "23:00"},
  "saturday": {"open": "10:00", "close": "23:00"},
  "sunday": {"open": "10:00", "close": "22:00"}
}
```

#### 2. 評論表（Reviews）

```sql
CREATE TABLE reviews (
    id BIGSERIAL PRIMARY KEY,
    restaurant_id BIGINT NOT NULL REFERENCES restaurants(id),
    user_id BIGINT NOT NULL REFERENCES users(id),

    -- 評分
    rating DECIMAL(2,1) NOT NULL CHECK (rating >= 1.0 AND rating <= 5.0),

    -- 內容
    text TEXT,
    photos TEXT[],                    -- 照片 URLs

    -- 互動計數
    useful_count INT DEFAULT 0,
    funny_count INT DEFAULT 0,
    cool_count INT DEFAULT 0,

    -- 狀態
    status VARCHAR(20) DEFAULT 'active',  -- 'active', 'flagged', 'removed'

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(restaurant_id, user_id)   -- 每個使用者對每家餐廳只能評論一次
);

CREATE INDEX idx_reviews_restaurant ON reviews(restaurant_id, created_at DESC);
CREATE INDEX idx_reviews_user ON reviews(user_id, created_at DESC);
CREATE INDEX idx_reviews_rating ON reviews(rating);
CREATE INDEX idx_reviews_useful ON reviews(useful_count DESC);

-- 分區表（按月分區）
CREATE TABLE reviews_2024_01 PARTITION OF reviews
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
```

#### 3. 評論聚合表（Review Summaries）

```sql
CREATE TABLE review_summaries (
    restaurant_id BIGINT PRIMARY KEY REFERENCES restaurants(id),

    -- 統計資料
    average_rating DECIMAL(3,2),
    total_reviews INT DEFAULT 0,

    -- 評分分佈
    rating_5_star INT DEFAULT 0,
    rating_4_star INT DEFAULT 0,
    rating_3_star INT DEFAULT 0,
    rating_2_star INT DEFAULT 0,
    rating_1_star INT DEFAULT 0,

    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_review_summaries_rating ON review_summaries(average_rating DESC);
```

#### 4. 使用者表（Users）

```sql
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,

    -- 基本資訊
    email VARCHAR(255) UNIQUE NOT NULL,
    username VARCHAR(100) UNIQUE NOT NULL,
    password_hash VARCHAR(255),
    full_name VARCHAR(100),

    -- 個人資料
    avatar_url VARCHAR(255),
    bio TEXT,
    location VARCHAR(100),

    -- 統計
    review_count INT DEFAULT 0,
    photo_count INT DEFAULT 0,
    friend_count INT DEFAULT 0,

    -- Elite 會員
    elite_years INT[],                -- [2022, 2023, 2024]

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);
```

#### 5. 照片表（Photos）

```sql
CREATE TABLE photos (
    id VARCHAR(36) PRIMARY KEY,       -- UUID
    restaurant_id BIGINT NOT NULL REFERENCES restaurants(id),
    user_id BIGINT NOT NULL REFERENCES users(id),

    -- 圖片資訊
    caption TEXT,
    urls JSONB,                       -- {"original": "...", "large": "...", "medium": "...", "small": "...", "thumb": "..."}

    -- 統計
    view_count INT DEFAULT 0,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_photos_restaurant ON photos(restaurant_id, created_at DESC);
CREATE INDEX idx_photos_user ON photos(user_id, created_at DESC);
```

#### 6. 評論互動表（Review Votes）

```sql
CREATE TABLE review_votes (
    id BIGSERIAL PRIMARY KEY,
    review_id BIGINT NOT NULL REFERENCES reviews(id),
    user_id BIGINT NOT NULL REFERENCES users(id),

    vote_type VARCHAR(20),            -- 'useful', 'funny', 'cool'

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(review_id, user_id, vote_type)
);

CREATE INDEX idx_review_votes_review ON review_votes(review_id);
CREATE INDEX idx_review_votes_user ON review_votes(user_id);
```

### Elasticsearch 索引

```json
{
  "settings": {
    "number_of_shards": 5,
    "number_of_replicas": 2,
    "analysis": {
      "analyzer": {
        "autocomplete": {
          "tokenizer": "autocomplete_tokenizer",
          "filter": ["lowercase"]
        }
      },
      "tokenizer": {
        "autocomplete_tokenizer": {
          "type": "edge_ngram",
          "min_gram": 2,
          "max_gram": 10,
          "token_chars": ["letter", "digit"]
        }
      }
    }
  },
  "mappings": {
    "properties": {
      "id": {
        "type": "long"
      },
      "name": {
        "type": "text",
        "analyzer": "standard",
        "fields": {
          "keyword": {
            "type": "keyword"
          },
          "autocomplete": {
            "type": "text",
            "analyzer": "autocomplete"
          }
        }
      },
      "location": {
        "type": "geo_point"
      },
      "category": {
        "type": "keyword"
      },
      "cuisine_type": {
        "type": "keyword"
      },
      "tags": {
        "type": "keyword"
      },
      "rating": {
        "type": "float"
      },
      "review_count": {
        "type": "integer"
      },
      "price_level": {
        "type": "integer"
      },
      "opening_hours": {
        "type": "object",
        "enabled": false
      },
      "verified": {
        "type": "boolean"
      },
      "permanently_closed": {
        "type": "boolean"
      }
    }
  }
}
```

### Redis 資料結構

#### 1. 地理空間索引

```redis
# 餐廳地理位置
GEOADD restaurants:geo 121.5170 25.0478 "restaurant:1001"
GEOADD restaurants:geo 121.5200 25.0500 "restaurant:1002"

# 查詢附近 1 km 的餐廳
GEORADIUS restaurants:geo 121.5170 25.0478 1 km WITHDIST WITHCOORD
```

#### 2. 快取

```redis
# 餐廳資訊快取
SET restaurant:1001 '{"id":1001,"name":"鼎泰豐","rating":4.5}' EX 3600

# 搜尋結果快取（使用 hash 做 key）
SET search:hash:abc123 '[1001,1002,1003]' EX 300

# 評論統計快取
HSET restaurant:1001:stats rating "4.5"
HSET restaurant:1001:stats review_count "1234"
EXPIRE restaurant:1001:stats 3600
```

#### 3. 排行榜

```redis
# 熱門餐廳（按評論數）
ZADD restaurants:popular 1234 "restaurant:1001"
ZADD restaurants:popular 987 "restaurant:1002"

# 取得 Top 10
ZREVRANGE restaurants:popular 0 9 WITHSCORES
```

---

## API 文件

### 1. 搜尋餐廳

**Endpoint**: `GET /api/v1/search`

**Parameters**:
- `lat`: 緯度
- `lng`: 經度
- `radius`: 搜尋半徑（公尺），預設 5000
- `category`: 類別過濾（可選）
- `cuisine`: 菜系過濾（可選）
- `price`: 價格過濾（1-4）
- `open_now`: 是否只顯示營業中
- `sort_by`: 排序方式（distance, rating, best_match）
- `limit`: 結果數量，預設 20

**Example**:
```
GET /api/v1/search?lat=25.0478&lng=121.5170&radius=1000&category=restaurant&price=2&open_now=true&sort_by=best_match&limit=20
```

**Response**:
```json
{
  "total": 156,
  "results": [
    {
      "id": 1001,
      "name": "鼎泰豐（信義店）",
      "category": "restaurant",
      "cuisine_type": "chinese",
      "rating": 4.5,
      "review_count": 1234,
      "price_level": 3,
      "location": {
        "latitude": 25.0475,
        "longitude": 121.5168
      },
      "distance": 45.2,
      "address": "台北市信義區信義路五段7號",
      "phone": "+886-2-2345-6789",
      "opening_hours": {
        "open_now": true,
        "periods": [...]
      },
      "photos": [
        "https://cdn.yelp.com/photos/1001/large.webp"
      ]
    }
  ]
}
```

### 2. 取得餐廳詳情

**Endpoint**: `GET /api/v1/restaurants/{id}`

**Response**:
```json
{
  "id": 1001,
  "name": "鼎泰豐（信義店）",
  "description": "台灣知名小籠包餐廳，米其林一星推薦...",
  "category": "restaurant",
  "cuisine_type": "chinese",
  "tags": ["michelin", "dim_sum", "xiaolongbao"],
  "rating": 4.5,
  "review_count": 1234,
  "price_level": 3,
  "location": {
    "latitude": 25.0475,
    "longitude": 121.5168,
    "address": "台北市信義區信義路五段7號",
    "city": "台北市",
    "country": "台灣"
  },
  "contact": {
    "phone": "+886-2-2345-6789",
    "website": "https://www.dintaifung.com.tw"
  },
  "opening_hours": {
    "monday": {"open": "11:00", "close": "21:30"},
    "tuesday": {"open": "11:00", "close": "21:30"},
    "wednesday": {"open": "11:00", "close": "21:30"},
    "thursday": {"open": "11:00", "close": "21:30"},
    "friday": {"open": "11:00", "close": "21:30"},
    "saturday": {"open": "10:00", "close": "21:30"},
    "sunday": {"open": "10:00", "close": "21:30"}
  },
  "photos": [
    {
      "id": "photo-123",
      "url": "https://cdn.yelp.com/photos/1001/large.webp",
      "caption": "招牌小籠包"
    }
  ],
  "verified": true,
  "claimed": true
}
```

### 3. 提交評論

**Endpoint**: `POST /api/v1/reviews`

**Request**:
```json
{
  "restaurant_id": 1001,
  "rating": 5.0,
  "text": "小籠包非常好吃，皮薄餡多，湯汁鮮美！服務態度也很好。",
  "photos": [
    "https://cdn.yelp.com/photos/upload/abc123.jpg"
  ]
}
```

**Response**:
```json
{
  "id": 5001,
  "restaurant_id": 1001,
  "user_id": 2001,
  "rating": 5.0,
  "text": "小籠包非常好吃...",
  "photos": [
    "https://cdn.yelp.com/photos/reviews/5001/large.webp"
  ],
  "useful_count": 0,
  "funny_count": 0,
  "cool_count": 0,
  "created_at": "2024-10-18T14:30:00Z"
}
```

### 4. 取得餐廳評論

**Endpoint**: `GET /api/v1/restaurants/{id}/reviews`

**Parameters**:
- `sort`: 排序方式（newest, highest_rated, lowest_rated, most_useful）
- `page`: 頁碼，預設 1
- `limit`: 每頁數量，預設 20

**Response**:
```json
{
  "total": 1234,
  "page": 1,
  "limit": 20,
  "reviews": [
    {
      "id": 5001,
      "user": {
        "id": 2001,
        "username": "foodlover123",
        "avatar_url": "https://cdn.yelp.com/users/2001/avatar.jpg",
        "review_count": 56,
        "elite_years": [2023, 2024]
      },
      "rating": 5.0,
      "text": "小籠包非常好吃...",
      "photos": [...],
      "useful_count": 15,
      "funny_count": 2,
      "cool_count": 8,
      "created_at": "2024-10-15T10:30:00Z"
    }
  ]
}
```

### 5. 上傳照片

**Endpoint**: `POST /api/v1/photos`

**Content-Type**: `multipart/form-data`

**Parameters**:
- `restaurant_id`: 餐廳 ID
- `file`: 圖片檔案（JPG/PNG，最大 10MB）
- `caption`: 圖片說明（可選）

**Response**:
```json
{
  "id": "photo-abc123",
  "restaurant_id": 1001,
  "urls": {
    "original": "https://cdn.yelp.com/photos/abc123/original.webp",
    "large": "https://cdn.yelp.com/photos/abc123/large.webp",
    "medium": "https://cdn.yelp.com/photos/abc123/medium.webp",
    "small": "https://cdn.yelp.com/photos/abc123/small.webp",
    "thumb": "https://cdn.yelp.com/photos/abc123/thumb.webp"
  },
  "caption": "美味的小籠包",
  "created_at": "2024-10-18T14:35:00Z"
}
```

### 6. 自動補全

**Endpoint**: `GET /api/v1/autocomplete`

**Parameters**:
- `query`: 搜尋文字
- `lat`, `lng`: 使用者位置（用於排序）
- `limit`: 結果數量，預設 10

**Example**:
```
GET /api/v1/autocomplete?query=鼎泰&lat=25.0478&lng=121.5170&limit=10
```

**Response**:
```json
{
  "suggestions": [
    {
      "id": 1001,
      "name": "鼎泰豐（信義店）",
      "category": "restaurant",
      "rating": 4.5,
      "distance": 45.2
    },
    {
      "id": 1002,
      "name": "鼎泰豐（南西店）",
      "category": "restaurant",
      "rating": 4.4,
      "distance": 2300
    }
  ]
}
```

---

## 地理空間索引

### PostGIS 查詢優化

#### 1. 基本範圍查詢

```sql
-- 查詢附近 1 km 的餐廳
SELECT
    id,
    name,
    rating,
    ST_Distance(
        location,
        ST_SetSRID(ST_MakePoint(121.5170, 25.0478), 4326)::geography
    ) AS distance
FROM restaurants
WHERE ST_DWithin(
    location,
    ST_SetSRID(ST_MakePoint(121.5170, 25.0478), 4326)::geography,
    1000
)
AND permanently_closed = false
ORDER BY distance
LIMIT 20;
```

**執行計畫**：
```
Limit  (cost=0.42..123.45 rows=20 width=...)
  ->  Index Scan using idx_restaurants_location on restaurants
        Order By: (location <-> '...'::geography)
        Filter: (ST_DWithin(...) AND NOT permanently_closed)
```

#### 2. 複合條件查詢

```sql
-- 附近 1 km + 評分 > 4.0 + 價格 <= 2 + 營業中
SELECT
    r.id,
    r.name,
    r.rating,
    r.review_count,
    r.price_level,
    ST_Distance(
        r.location,
        ST_SetSRID(ST_MakePoint(121.5170, 25.0478), 4326)::geography
    ) AS distance,
    CASE
        WHEN EXTRACT(HOUR FROM NOW()) >= CAST((r.opening_hours->current_day->>'open')::TIME AS NUMERIC)
         AND EXTRACT(HOUR FROM NOW()) < CAST((r.opening_hours->current_day->>'close')::TIME AS NUMERIC)
        THEN true
        ELSE false
    END AS is_open
FROM restaurants r
WHERE ST_DWithin(
        r.location,
        ST_SetSRID(ST_MakePoint(121.5170, 25.0478), 4326)::geography,
        1000
    )
    AND r.rating >= 4.0
    AND r.price_level <= 2
    AND r.permanently_closed = false
ORDER BY distance
LIMIT 20;
```

### Elasticsearch 地理查詢

#### 1. Geo Distance 查詢

```json
{
  "query": {
    "bool": {
      "filter": {
        "geo_distance": {
          "distance": "1km",
          "location": {
            "lat": 25.0478,
            "lon": 121.5170
          }
        }
      }
    }
  },
  "sort": [
    {
      "_geo_distance": {
        "location": {
          "lat": 25.0478,
          "lon": 121.5170
        },
        "order": "asc",
        "unit": "km"
      }
    }
  ]
}
```

#### 2. Geo Bounding Box 查詢

```json
{
  "query": {
    "geo_bounding_box": {
      "location": {
        "top_left": {
          "lat": 25.0600,
          "lon": 121.5000
        },
        "bottom_right": {
          "lat": 25.0300,
          "lon": 121.5400
        }
      }
    }
  }
}
```

### QuadTree vs Geohash vs PostGIS 比較

| 方案 | 查詢時間 | 記憶體 | 優點 | 缺點 |
|------|----------|--------|------|------|
| **PostGIS** | 10-30ms | 低 | 準確、支援複雜查詢 | 資料庫壓力大 |
| **Redis Geo** | < 5ms | 中 | 快速、簡單 | 功能有限 |
| **Elasticsearch** | 10-20ms | 高 | 功能強大、可擴展 | 複雜度高 |
| **QuadTree** | 5-15ms | 中 | 自適應、動態 | 需自行實作 |

---

## 排序演算法

### 多因素評分公式

```
總分 = W1 × 距離分數 + W2 × 評分分數 + W3 × 評論數分數 + W4 × 價格匹配分數 + W5 × 營業中加分

其中：
W1 = 0.35 (距離權重)
W2 = 0.30 (評分權重)
W3 = 0.15 (評論數權重)
W4 = 0.10 (價格匹配權重)
W5 = 0.10 (營業中加分)

距離分數 = max(0, 1 - 距離/3km)
評分分數 = 評分 / 5.0
評論數分數 = log10(評論數 + 1) / log10(1001)
價格匹配分數 = 1 - |餐廳價格 - 使用者偏好| / 3
營業中加分 = 1.0 (營業中) 或 0.0 (休息中)
```

### 範例計算

**餐廳 A**：
- 距離：0.5 km
- 評分：4.5 星
- 評論數：500
- 價格：$$ (2)
- 使用者偏好價格：$$ (2)
- 目前營業中：是

```
距離分數 = 1 - 0.5/3 = 0.833
評分分數 = 4.5/5 = 0.900
評論數分數 = log10(501) / log10(1001) = 0.897
價格匹配分數 = 1 - 0/3 = 1.000
營業中加分 = 1.000

總分 = 0.35×0.833 + 0.30×0.900 + 0.15×0.897 + 0.10×1.000 + 0.10×1.000
     = 0.291 + 0.270 + 0.135 + 0.100 + 0.100
     = 0.896
```

**餐廳 B**：
- 距離：0.3 km
- 評分：3.5 星
- 評論數：50
- 價格：$$$$ (4)
- 使用者偏好價格：$$ (2)
- 目前營業中：否

```
距離分數 = 1 - 0.3/3 = 0.900
評分分數 = 3.5/5 = 0.700
評論數分數 = log10(51) / log10(1001) = 0.567
價格匹配分數 = 1 - 2/3 = 0.333
營業中加分 = 0.000

總分 = 0.35×0.900 + 0.30×0.700 + 0.15×0.567 + 0.10×0.333 + 0.10×0.000
     = 0.315 + 0.210 + 0.085 + 0.033 + 0.000
     = 0.643
```

**結果**：餐廳 A (0.896) > 餐廳 B (0.643)

---

## 效能指標

### 系統容量

| 指標 | 數值 | 備註 |
|------|------|------|
| **餐廳總數** | 500 萬 | 全球 |
| **月活躍用戶** | 1.5 億 | |
| **每日搜尋次數** | 1 億次 | 平均每秒 1,157 次 |
| **每日評論數** | 50 萬則 | 平均每秒 5.8 則 |
| **每日照片上傳** | 100 萬張 | 平均每秒 11.6 張 |

### API 延遲

| API | P50 | P95 | P99 |
|-----|-----|-----|-----|
| **搜尋餐廳** | <50ms | <150ms | <300ms |
| **取得餐廳詳情** | <20ms | <50ms | <100ms |
| **提交評論** | <100ms | <300ms | <500ms |
| **上傳照片** | <500ms | <1.5s | <3s |
| **自動補全** | <30ms | <80ms | <150ms |

### 快取命中率

| 快取類型 | 命中率 | TTL |
|----------|--------|-----|
| **餐廳資訊** | 95% | 1 小時 |
| **搜尋結果** | 60% | 5 分鐘 |
| **評論列表** | 80% | 30 分鐘 |
| **照片 URLs** | 99% | 7 天 |

---

## 成本分析

### 全球規模成本估算

**假設條件**：
- 餐廳總數：500 萬
- 月活躍用戶：1.5 億
- 每日搜尋：1 億次

| 項目 | 規格 | 月費用 (USD) |
|------|------|-------------|
| **Elasticsearch** | 50 節點（r5.2xlarge） | $150,000 |
| **PostgreSQL** | Aurora (20 實例) | $80,000 |
| **Redis** | 30 節點（r5.xlarge） | $60,000 |
| **EC2（API Server）** | 100 台 c5.xlarge | $120,000 |
| **S3（圖片儲存）** | 2 PB | $40,000 |
| **CloudFront（CDN）** | 50 TB 流量 | $25,000 |
| **Kafka** | 10 節點 | $20,000 |
| **頻寬** | 100 TB/月 | $50,000 |
| **監控** | Datadog + ELK | $20,000 |
| **總計** | | **$565,000/月** |

**年度成本**：約 **$6,780,000（約 NT$ 2.17 億）**

### 台灣區域成本估算

**假設條件**：
- 餐廳總數：10 萬
- 月活躍用戶：200 萬
- 每日搜尋：100 萬次

| 項目 | 規格 | 月費用 (NT$) |
|------|------|-------------|
| Elasticsearch | 5 節點 | 150,000 |
| RDS PostgreSQL | r5.large Multi-AZ | 60,000 |
| ElastiCache Redis | 5 節點 | 40,000 |
| EC2（API Server）| 10 台 c5.large | 100,000 |
| S3 | 20 TB | 12,000 |
| CloudFront | 5 TB | 7,500 |
| 頻寬 | 10 TB | 18,000 |
| 監控 | | 15,000 |
| **總計** | | **402,500/月** |

---

## 部署架構

### Kubernetes Deployment

```yaml
# search-service.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: search-service
spec:
  replicas: 10
  selector:
    matchLabels:
      app: search-service
  template:
    metadata:
      labels:
        app: search-service
    spec:
      containers:
      - name: search-service
        image: yelp/search-service:v1.5.0
        ports:
        - containerPort: 8080
        resources:
          requests:
            cpu: "2"
            memory: "4Gi"
          limits:
            cpu: "4"
            memory: "8Gi"
        env:
        - name: ES_HOST
          valueFrom:
            configMapKeyRef:
              name: elasticsearch-config
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
```

---

## 延伸閱讀

### 開源專案

- [Elasticsearch](https://github.com/elastic/elasticsearch) - 搜尋引擎
- [PostGIS](https://postgis.net/) - PostgreSQL 地理空間擴展
- [libvips](https://github.com/libvips/libvips) - 圖片處理庫

### 相關論文

- [Spatial Index Structures](https://en.wikipedia.org/wiki/Spatial_database) - 空間索引
- [Ranking Algorithms](https://www.elastic.co/guide/en/elasticsearch/reference/current/sort-search-results.html)

---

**版本**: v1.0.0
**最後更新**: 2024-10-18
**維護者**: Yelp Platform Team
