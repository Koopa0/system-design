# Uber/Lyft - 叫車平台技術文件

## 目錄
- [系統架構](#系統架構)
- [資料庫設計](#資料庫設計)
- [API 文件](#api-文件)
- [地理空間索引](#地理空間索引)
- [效能指標](#效能指標)
- [成本分析](#成本分析)
- [部署架構](#部署架構)

---

## 系統架構

### 高階架構圖

```
┌─────────────┐         ┌─────────────┐
│  乘客 App   │         │  司機 App   │
│  (React)    │         │  (React)    │
└──────┬──────┘         └──────┬──────┘
       │                       │
       │    HTTPS/WebSocket    │
       └───────────┬───────────┘
                   │
          ┌────────▼─────────┐
          │  Load Balancer   │
          │   (Nginx/ALB)    │
          └────────┬─────────┘
                   │
       ┌───────────┼───────────┐
       │           │           │
┌──────▼──────┐ ┌─▼────────┐ ┌▼──────────┐
│ Location    │ │ Matching │ │ Payment   │
│ Service     │ │ Service  │ │ Service   │
│ (Go)        │ │ (Go)     │ │ (Go)      │
└─────┬───────┘ └─┬────────┘ └┬──────────┘
      │           │            │
      └───────────┼────────────┘
                  │
      ┌───────────┼────────────┐
      │           │            │
┌─────▼─────┐ ┌──▼────────┐ ┌─▼──────────┐
│  Redis    │ │PostgreSQL │ │   Kafka    │
│  Cluster  │ │  Cluster  │ │  Cluster   │
│           │ │           │ │            │
│ · Geo索引 │ │ · 用戶    │ │ · 事件流   │
│ · 快取    │ │ · 行程    │ │ · 日誌     │
│ · Surge   │ │ · 支付    │ │ · 分析     │
└───────────┘ └───────────┘ └────────────┘
```

### 微服務架構

| 服務 | 職責 | 技術棧 | 實例數 |
|------|------|--------|--------|
| **Location Service** | GPS 追蹤、地理索引 | Go, Redis Geo | 10+ |
| **Matching Service** | 司機配對、派單 | Go, Redis Lock | 8+ |
| **Routing Service** | 路徑規劃、ETA | Go, OSM/Google Maps | 6+ |
| **Pricing Service** | 價格計算、Surge | Go, Redis | 4+ |
| **Trip Service** | 行程管理、狀態機 | Go, PostgreSQL | 8+ |
| **Payment Service** | 支付、結算 | Go, Stripe API | 4+ |
| **Notification Service** | 推播、簡訊 | Go, Firebase/Twilio | 4+ |
| **Rating Service** | 評分、反饋 | Go, PostgreSQL | 2+ |

---

## 資料庫設計

### PostgreSQL Schema

#### 1. 用戶表（Users）

```sql
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    phone VARCHAR(20) UNIQUE NOT NULL,
    email VARCHAR(255),
    name VARCHAR(100),
    password_hash VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_phone ON users(phone);
CREATE INDEX idx_users_email ON users(email);
```

#### 2. 司機表（Drivers）

```sql
CREATE TABLE drivers (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id),

    -- 車輛資訊
    vehicle_type VARCHAR(50),      -- 'economy', 'premium', 'xl'
    vehicle_brand VARCHAR(50),
    vehicle_model VARCHAR(50),
    vehicle_year INT,
    license_plate VARCHAR(20),

    -- 司機資訊
    license_number VARCHAR(50),
    status VARCHAR(20) DEFAULT 'offline',  -- 'offline', 'online', 'busy', 'in_trip'
    rating DECIMAL(3,2) DEFAULT 5.00,
    rating_count INT DEFAULT 0,
    acceptance_rate DECIMAL(3,2) DEFAULT 1.00,
    cancellation_rate DECIMAL(3,2) DEFAULT 0.00,

    -- 位置（最後已知位置，快取用）
    last_latitude DECIMAL(10,8),
    last_longitude DECIMAL(11,8),
    last_location_update TIMESTAMP,

    -- 統計
    total_trips INT DEFAULT 0,
    total_earnings DECIMAL(12,2) DEFAULT 0,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_drivers_user_id ON drivers(user_id);
CREATE INDEX idx_drivers_status ON drivers(status);
CREATE INDEX idx_drivers_rating ON drivers(rating);
```

#### 3. 乘客表（Riders）

```sql
CREATE TABLE riders (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id),

    -- 支付資訊
    default_payment_method VARCHAR(20),  -- 'card', 'cash', 'wallet'
    stripe_customer_id VARCHAR(100),

    -- 統計
    total_trips INT DEFAULT 0,
    rating DECIMAL(3,2) DEFAULT 5.00,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_riders_user_id ON riders(user_id);
```

#### 4. 行程表（Trips）

```sql
CREATE TABLE trips (
    id BIGSERIAL PRIMARY KEY,
    rider_id BIGINT NOT NULL REFERENCES riders(id),
    driver_id BIGINT REFERENCES drivers(id),

    -- 狀態
    status VARCHAR(20) NOT NULL,  -- 'requested', 'dispatched', 'accepted', 'arriving', 'arrived', 'in_progress', 'completed', 'cancelled'

    -- 起點
    pickup_latitude DECIMAL(10,8) NOT NULL,
    pickup_longitude DECIMAL(11,8) NOT NULL,
    pickup_address TEXT,

    -- 終點
    dropoff_latitude DECIMAL(10,8),
    dropoff_longitude DECIMAL(11,8),
    dropoff_address TEXT,

    -- 時間戳
    request_time TIMESTAMP NOT NULL,
    dispatch_time TIMESTAMP,
    accept_time TIMESTAMP,
    pickup_time TIMESTAMP,
    dropoff_time TIMESTAMP,

    -- 價格
    estimated_price DECIMAL(10,2),
    final_price DECIMAL(10,2),
    surge_multiplier DECIMAL(3,2) DEFAULT 1.00,

    -- 距離與時間
    estimated_distance DECIMAL(8,2),  -- km
    actual_distance DECIMAL(8,2),
    estimated_duration INT,           -- 秒
    actual_duration INT,

    -- 取消資訊
    cancelled_by VARCHAR(20),         -- 'rider', 'driver', 'system'
    cancel_reason TEXT,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_trips_rider_id ON trips(rider_id);
CREATE INDEX idx_trips_driver_id ON trips(driver_id);
CREATE INDEX idx_trips_status ON trips(status);
CREATE INDEX idx_trips_request_time ON trips(request_time);
CREATE INDEX idx_trips_pickup_location ON trips(pickup_latitude, pickup_longitude);

-- 分區表（按月分區，提升查詢效能）
CREATE TABLE trips_2024_01 PARTITION OF trips
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
CREATE TABLE trips_2024_02 PARTITION OF trips
    FOR VALUES FROM ('2024-02-01') TO ('2024-03-01');
-- ... 依此類推
```

#### 5. 評分表（Ratings）

```sql
CREATE TABLE ratings (
    id BIGSERIAL PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id),
    from_id BIGINT NOT NULL,           -- 評分者 (rider_id 或 driver_id)
    to_id BIGINT NOT NULL,             -- 被評分者
    from_type VARCHAR(10) NOT NULL,    -- 'rider' or 'driver'

    score DECIMAL(2,1) NOT NULL CHECK (score >= 1.0 AND score <= 5.0),
    comment TEXT,
    tags TEXT[],                       -- ['friendly', 'clean_car', 'safe_driving']

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_ratings_trip_id ON ratings(trip_id);
CREATE INDEX idx_ratings_to_id ON ratings(to_id);
CREATE INDEX idx_ratings_created_at ON ratings(created_at);
```

#### 6. 支付表（Payments）

```sql
CREATE TABLE payments (
    id BIGSERIAL PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id),
    rider_id BIGINT NOT NULL REFERENCES riders(id),

    amount DECIMAL(10,2) NOT NULL,
    currency VARCHAR(3) DEFAULT 'TWD',

    payment_method VARCHAR(20),        -- 'card', 'cash', 'wallet'
    status VARCHAR(20),                -- 'pending', 'completed', 'failed', 'refunded'

    -- 第三方支付資訊
    stripe_charge_id VARCHAR(100),
    stripe_payment_intent_id VARCHAR(100),

    -- 分帳
    driver_earning DECIMAL(10,2),      -- 司機收入
    platform_fee DECIMAL(10,2),        -- 平台抽成

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_payments_trip_id ON payments(trip_id);
CREATE INDEX idx_payments_rider_id ON payments(rider_id);
CREATE INDEX idx_payments_status ON payments(status);
```

#### 7. 司機錢包（Driver Wallets）

```sql
CREATE TABLE driver_wallets (
    id BIGSERIAL PRIMARY KEY,
    driver_id BIGINT UNIQUE NOT NULL REFERENCES drivers(id),

    balance DECIMAL(12,2) DEFAULT 0.00,              -- 可提領餘額
    pending_balance DECIMAL(12,2) DEFAULT 0.00,      -- 處理中的收入（行程剛結束）
    total_earned DECIMAL(12,2) DEFAULT 0.00,         -- 歷史總收入
    total_withdrawn DECIMAL(12,2) DEFAULT 0.00,      -- 歷史總提領

    bank_account VARCHAR(50),
    bank_code VARCHAR(10),

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_driver_wallets_driver_id ON driver_wallets(driver_id);
```

### Redis 資料結構

#### 1. 司機位置索引（Geo）

```redis
# 儲存所有在線司機的位置
GEOADD drivers:online 121.5170 25.0478 driver:1001
GEOADD drivers:online 121.5200 25.0500 driver:1002

# 查詢附近 3 公里內的司機
GEORADIUS drivers:online 121.5170 25.0478 3 km WITHDIST

# 查詢結果：
# 1) "driver:1001"
# 2) "0.0000"
# 3) "driver:1002"
# 4) "0.2847"
```

#### 2. 司機詳細資訊（Hash）

```redis
# 儲存司機即時狀態
HSET driver:1001:info status "available"
HSET driver:1001:info latitude "25.0478"
HSET driver:1001:info longitude "121.5170"
HSET driver:1001:info bearing "45.5"
HSET driver:1001:info speed "30"
HSET driver:1001:info updated_at "1634567890"

# 設定過期時間（30秒未更新視為離線）
EXPIRE driver:1001:info 30
```

#### 3. Surge 定價（Hash）

```redis
# 儲存各區域的 Surge 倍數
HSET surge:regions region:wsqqkh "1.5"    # 台北車站
HSET surge:regions region:wsqque "2.0"    # 信義區
HSET surge:regions region:wsqqt5 "1.2"    # 松山區

# 過期時間 5 分鐘
EXPIRE surge:regions 300
```

#### 4. 待配對請求（Sorted Set）

```redis
# 儲存待配對的叫車請求（按時間排序）
ZADD pending_requests:wsqqkh 1634567890 "request:5001"
ZADD pending_requests:wsqqkh 1634567895 "request:5002"

# 查詢過去 5 分鐘的請求數量（計算 Surge）
ZCOUNT pending_requests:wsqqkh 1634567590 1634567890
```

#### 5. 分散式鎖（String）

```redis
# 鎖定司機（防止重複派單）
SETNX driver:1001:lock rider:2001 EX 30

# 回傳 1 = 成功鎖定
# 回傳 0 = 已被鎖定
```

---

## API 文件

### 1. 乘客叫車

**Endpoint**: `POST /api/v1/trips/request`

**Request**:
```json
{
  "rider_id": 2001,
  "pickup": {
    "latitude": 25.0478,
    "longitude": 121.5170,
    "address": "台北車站"
  },
  "dropoff": {
    "latitude": 25.0330,
    "longitude": 121.5654,
    "address": "台北 101"
  },
  "vehicle_type": "economy",
  "payment_method": "card"
}
```

**Response**:
```json
{
  "trip_id": 8001,
  "status": "dispatched",
  "driver": {
    "id": 1001,
    "name": "王大明",
    "vehicle": "Toyota Camry",
    "license_plate": "ABC-1234",
    "rating": 4.85,
    "photo_url": "https://cdn.uber.com/drivers/1001.jpg"
  },
  "eta": 180,
  "estimated_price": 185.0,
  "surge_multiplier": 1.5,
  "driver_location": {
    "latitude": 25.0500,
    "longitude": 121.5200
  }
}
```

### 2. 查詢附近司機

**Endpoint**: `GET /api/v1/drivers/nearby`

**Parameters**:
- `latitude`: 25.0478
- `longitude`: 121.5170
- `radius`: 3 (公里)
- `vehicle_type`: economy

**Response**:
```json
{
  "drivers": [
    {
      "id": 1001,
      "latitude": 25.0500,
      "longitude": 121.5200,
      "distance": 0.28,
      "eta": 120,
      "bearing": 45.5,
      "rating": 4.85
    },
    {
      "id": 1002,
      "latitude": 25.0450,
      "longitude": 121.5180,
      "distance": 0.15,
      "eta": 90,
      "bearing": 180.0,
      "rating": 4.92
    }
  ],
  "count": 2
}
```

### 3. 更新司機位置

**Endpoint**: `POST /api/v1/drivers/location` (WebSocket)

**Request**:
```json
{
  "driver_id": 1001,
  "latitude": 25.0478,
  "longitude": 121.5170,
  "bearing": 45.5,
  "speed": 30,
  "timestamp": "2024-10-18T14:30:00Z"
}
```

**Response**:
```json
{
  "status": "ok",
  "nearby_requests": 3
}
```

### 4. 價格估算

**Endpoint**: `POST /api/v1/pricing/estimate`

**Request**:
```json
{
  "pickup": {
    "latitude": 25.0478,
    "longitude": 121.5170
  },
  "dropoff": {
    "latitude": 25.0330,
    "longitude": 121.5654
  },
  "vehicle_type": "economy"
}
```

**Response**:
```json
{
  "base_price": 123.50,
  "distance": 5.2,
  "duration": 15,
  "surge": 1.5,
  "final_price": 185.0,
  "currency": "TWD",
  "breakdown": {
    "base_fare": 70.0,
    "distance_fare": 78.0,
    "time_fare": 37.5,
    "surge_amount": 61.5
  }
}
```

### 5. 司機接單

**Endpoint**: `POST /api/v1/trips/{trip_id}/accept`

**Request**:
```json
{
  "driver_id": 1001
}
```

**Response**:
```json
{
  "trip_id": 8001,
  "status": "accepted",
  "rider": {
    "id": 2001,
    "name": "李小華",
    "phone": "+886912345678",
    "rating": 4.75
  },
  "pickup": {
    "latitude": 25.0478,
    "longitude": 121.5170,
    "address": "台北車站"
  },
  "estimated_earnings": 147.5
}
```

### 6. 提交評分

**Endpoint**: `POST /api/v1/ratings`

**Request**:
```json
{
  "trip_id": 8001,
  "from_id": 2001,
  "to_id": 1001,
  "from_type": "rider",
  "score": 5.0,
  "comment": "司機很準時，車子很乾淨！",
  "tags": ["friendly", "clean_car", "safe_driving"]
}
```

**Response**:
```json
{
  "rating_id": 9001,
  "status": "success",
  "driver_new_rating": 4.86
}
```

---

## 地理空間索引

### Geohash 精度對照表

| Precision | Cell Size | Example Use Case |
|-----------|-----------|------------------|
| 1 | ~5,000 km | 洲級別 |
| 2 | ~1,250 km | 國家級別 |
| 3 | ~156 km | 省/州級別 |
| 4 | ~39 km | 城市級別 |
| 5 | ~4.9 km | 區域級別 |
| **6** | **~1.2 km** | **叫車服務（推薦）** |
| 7 | ~152 m | 街區級別 |
| 8 | ~38 m | 建築物級別 |

### S2 Cell Level 對照表

| Level | Cell Size | Use Case |
|-------|-----------|----------|
| 10 | ~100 km | 城市級別（Geosharding） |
| 13 | ~1 km² | 司機搜尋範圍 |
| 15 | ~100 m² | 精確定位 |
| 18 | ~10 m² | 建築物入口 |

### 距離計算公式（Haversine）

```go
func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
    const R = 6371 // 地球半徑（公里）

    φ1 := lat1 * math.Pi / 180
    φ2 := lat2 * math.Pi / 180
    Δφ := (lat2 - lat1) * math.Pi / 180
    Δλ := (lon2 - lon1) * math.Pi / 180

    a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
         math.Cos(φ1)*math.Cos(φ2)*
         math.Sin(Δλ/2)*math.Sin(Δλ/2)

    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

    return R * c
}
```

---

## 效能指標

### 系統容量

| 指標 | 數值 | 備註 |
|------|------|------|
| **同時在線司機** | 100,000 | 尖峰時段 |
| **同時在線乘客** | 500,000 | 尖峰時段 |
| **每秒叫車請求** | 10,000 QPS | 尖峰 |
| **位置更新頻率** | 每 4 秒 | 司機 App |
| **每秒位置更新** | 25,000 寫入/秒 | 100,000 司機 ÷ 4 |
| **WebSocket 連線** | 600,000 | 司機 + 乘客 |

### 延遲要求

| 操作 | P50 | P95 | P99 |
|------|-----|-----|-----|
| **查詢附近司機** | <50ms | <100ms | <200ms |
| **司機配對** | <200ms | <500ms | <1s |
| **位置更新** | <20ms | <50ms | <100ms |
| **價格估算** | <100ms | <300ms | <500ms |
| **支付處理** | <1s | <2s | <5s |

### Redis 效能

```bash
# 每秒位置查詢 (GEORADIUS)
Benchmark: 50,000 queries/sec
P50 latency: 1.2ms
P99 latency: 5.8ms

# 每秒位置更新 (GEOADD)
Benchmark: 100,000 writes/sec
P50 latency: 0.8ms
P99 latency: 3.2ms
```

### PostgreSQL 優化

```sql
-- 使用 BRIN 索引（Block Range Index）加速時間範圍查詢
CREATE INDEX idx_trips_request_time_brin ON trips
USING BRIN (request_time) WITH (pages_per_range = 128);

-- 使用部分索引（Partial Index）加速活躍行程查詢
CREATE INDEX idx_trips_active ON trips(status)
WHERE status IN ('dispatched', 'accepted', 'arriving', 'in_progress');

-- 使用 GiST 索引（Generalized Search Tree）加速地理查詢
CREATE EXTENSION postgis;
ALTER TABLE trips ADD COLUMN pickup_location GEOGRAPHY(POINT, 4326);
CREATE INDEX idx_trips_pickup_location ON trips USING GIST (pickup_location);
```

---

## 成本分析

### 台灣市場成本估算

**假設條件**：
- 活躍司機：10,000 名
- 每日行程：50,000 趟
- 平均單價：NT$200
- 平台抽成：25%
- 每日 GMV：NT$10,000,000

#### 月度成本明細

| 類別 | 項目 | 規格 | 月費用 (NT$) |
|------|------|------|--------------|
| **運算資源** | API Server (EC2) | 20 × c5.2xlarge (8C16G) | 300,000 |
| | Location Service | 10 × c5.xlarge (4C8G) | 120,000 |
| **資料庫** | PostgreSQL RDS | r5.2xlarge Multi-AZ | 120,000 |
| | Redis Cluster | 12 × r5.large (32GB) | 180,000 |
| **訊息佇列** | Kafka Cluster | 6 × m5.large | 90,000 |
| **儲存** | S3 (路徑、圖片) | 10TB | 15,000 |
| **CDN** | CloudFront | 100TB 流量 | 60,000 |
| **第三方 API** | Google Maps API | 150,000 次/日 × 30 天 | 450,000 |
| | Twilio SMS | 100,000 則 | 30,000 |
| **支付** | Stripe 手續費 | 2.9% + NT$9 per transaction | 580,000 |
| **網路** | Data Transfer | 200TB/月 | 150,000 |
| **監控** | Datadog + ELK | Full Stack | 60,000 |
| **備份** | 自動備份 | 每日快照 | 20,000 |
| **其他** | 域名、SSL 憑證等 | - | 10,000 |
| **總計** | | | **2,185,000** |

#### 成本優化方案

| 優化項目 | 原成本 | 優化後 | 節省金額 | 方法 |
|----------|--------|--------|----------|------|
| **地圖 API** | 450,000 | 50,000 | 400,000 | 自建路徑引擎（OSM + A*） |
| **Redis** | 180,000 | 126,000 | 54,000 | 使用 Geosharding 減少 30% 記憶體 |
| **API Server** | 300,000 | 180,000 | 120,000 | WebSocket 連線池複用 |
| **CDN** | 60,000 | 36,000 | 24,000 | 壓縮地圖資源 |
| **總計** | 2,185,000 | **1,587,000** | **598,000** | **節省 27%** |

#### 單位經濟效益

```
每趟行程成本 = NT$1,587,000 ÷ (50,000 × 30) = NT$1.06

收入結構（單趟 NT$200）：
- 司機收入：NT$150 (75%)
- 平台抽成：NT$50 (25%)
- 平台成本：NT$1.06 (0.53%)
- 平台淨利：NT$48.94 (24.47%)

毛利率：24.47%
```

---

## 部署架構

### AWS 部署方案

```yaml
# 區域配置（台灣）
region: ap-northeast-1 (Tokyo, 最近台灣的區域)

# VPC 配置
vpc:
  cidr: 10.0.0.0/16
  availability_zones: 3

  subnets:
    public:
      - 10.0.1.0/24  (AZ-1)
      - 10.0.2.0/24  (AZ-2)
      - 10.0.3.0/24  (AZ-3)

    private:
      - 10.0.11.0/24 (AZ-1)
      - 10.0.12.0/24 (AZ-2)
      - 10.0.13.0/24 (AZ-3)

# 負載均衡
load_balancer:
  type: Application Load Balancer (ALB)
  scheme: internet-facing
  health_check:
    path: /health
    interval: 10s
    timeout: 5s

# Auto Scaling
auto_scaling:
  api_server:
    min: 10
    max: 50
    target_cpu: 70%

  location_service:
    min: 5
    max: 30
    target_cpu: 70%

# 資料庫
database:
  postgresql:
    instance_class: db.r5.2xlarge
    engine_version: 14.7
    multi_az: true
    backup_retention: 7 days
    read_replicas: 2

  redis:
    node_type: cache.r5.large
    num_cache_nodes: 12
    cluster_mode: enabled
    shards: 3
    replicas: 3

# Kubernetes (EKS)
kubernetes:
  version: 1.27
  node_groups:
    - name: api-nodes
      instance_type: c5.2xlarge
      min_size: 10
      max_size: 50

    - name: location-nodes
      instance_type: c5.xlarge
      min_size: 5
      max_size: 30
```

### Docker Compose（本地開發）

```yaml
version: '3.8'

services:
  api-server:
    build: ./cmd/api
    ports:
      - "8080:8080"
    environment:
      - DB_HOST=postgres
      - REDIS_HOST=redis
      - KAFKA_BROKERS=kafka:9092
    depends_on:
      - postgres
      - redis
      - kafka

  location-service:
    build: ./cmd/location
    ports:
      - "8081:8081"
    environment:
      - REDIS_HOST=redis
    depends_on:
      - redis

  postgres:
    image: postgres:14
    ports:
      - "5432:5432"
    environment:
      - POSTGRES_DB=uber
      - POSTGRES_USER=uber
      - POSTGRES_PASSWORD=uber123
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7
    ports:
      - "6379:6379"
    command: redis-server --appendonly yes
    volumes:
      - redis_data:/data

  kafka:
    image: confluentinc/cp-kafka:7.4.0
    ports:
      - "9092:9092"
    environment:
      - KAFKA_ZOOKEEPER_CONNECT=zookeeper:2181
      - KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://kafka:9092

  zookeeper:
    image: confluentinc/cp-zookeeper:7.4.0
    environment:
      - ZOOKEEPER_CLIENT_PORT=2181

volumes:
  postgres_data:
  redis_data:
```

### Kubernetes Deployment（生產環境）

```yaml
# location-service.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: location-service
spec:
  replicas: 10
  selector:
    matchLabels:
      app: location-service
  template:
    metadata:
      labels:
        app: location-service
    spec:
      containers:
      - name: location-service
        image: uber/location-service:v1.0.0
        ports:
        - containerPort: 8081
        resources:
          requests:
            cpu: "2"
            memory: "4Gi"
          limits:
            cpu: "4"
            memory: "8Gi"
        env:
        - name: REDIS_HOST
          valueFrom:
            secretKeyRef:
              name: redis-secret
              key: host
        livenessProbe:
          httpGet:
            path: /health
            port: 8081
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8081
          initialDelaySeconds: 10
          periodSeconds: 5

---
apiVersion: v1
kind: Service
metadata:
  name: location-service
spec:
  selector:
    app: location-service
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8081
  type: LoadBalancer

---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: location-service-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: location-service
  minReplicas: 10
  maxReplicas: 50
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

---

## 監控與告警

### Prometheus Metrics

```go
// internal/metrics/metrics.go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // 位置更新計數
    locationUpdatesTotal = promauto.NewCounter(prometheus.CounterOpts{
        Name: "location_updates_total",
        Help: "Total number of location updates",
    })

    // 叫車請求計數
    tripRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "trip_requests_total",
        Help: "Total number of trip requests",
    }, []string{"status"})

    // 配對延遲
    matchingLatency = promauto.NewHistogram(prometheus.HistogramOpts{
        Name:    "matching_latency_seconds",
        Help:    "Latency of driver matching",
        Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
    })

    // 在線司機數
    onlineDrivers = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "online_drivers",
        Help: "Number of online drivers",
    })

    // Surge 倍數
    surgeMultiplier = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Name: "surge_multiplier",
        Help: "Current surge multiplier by region",
    }, []string{"region"})
)
```

### Grafana Dashboard

```json
{
  "dashboard": {
    "title": "Uber Platform Overview",
    "panels": [
      {
        "title": "Online Drivers",
        "targets": [
          {
            "expr": "online_drivers"
          }
        ]
      },
      {
        "title": "Trip Requests (QPS)",
        "targets": [
          {
            "expr": "rate(trip_requests_total[1m])"
          }
        ]
      },
      {
        "title": "Matching Latency P95",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, matching_latency_seconds)"
          }
        ]
      },
      {
        "title": "Surge Heatmap",
        "targets": [
          {
            "expr": "surge_multiplier"
          }
        ]
      }
    ]
  }
}
```

---

## 安全性

### 1. 認證與授權

```go
// internal/auth/middleware.go
package auth

import (
    "github.com/gin-gonic/gin"
    "github.com/golang-jwt/jwt/v5"
)

func AuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        tokenString := c.GetHeader("Authorization")

        if tokenString == "" {
            c.JSON(401, gin.H{"error": "missing token"})
            c.Abort()
            return
        }

        token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
            return []byte("secret"), nil
        })

        if err != nil || !token.Valid {
            c.JSON(401, gin.H{"error": "invalid token"})
            c.Abort()
            return
        }

        claims := token.Claims.(jwt.MapClaims)
        c.Set("user_id", claims["user_id"])
        c.Set("role", claims["role"])
        c.Next()
    }
}
```

### 2. 資料加密

- **傳輸加密**：HTTPS/TLS 1.3
- **靜態加密**：RDS/S3 啟用加密
- **敏感資料**：使用 AES-256 加密（信用卡號、身分證字號）

### 3. 反欺詐系統

```go
// internal/fraud/detector.go
package fraud

type FraudDetector struct {
    db    *sql.DB
    redis *redis.Client
}

// DetectFraudulentTrip 檢測可疑行程
func (f *FraudDetector) DetectFraudulentTrip(ctx context.Context, trip *Trip) (bool, string) {
    // 1. 檢查重複叫車（1分鐘內同一乘客 > 3次）
    count := f.redis.ZCount(ctx,
        fmt.Sprintf("rider:%d:requests", trip.RiderID),
        fmt.Sprintf("%d", time.Now().Add(-1*time.Minute).Unix()),
        fmt.Sprintf("%d", time.Now().Unix()),
    ).Val()

    if count > 3 {
        return true, "too_many_requests"
    }

    // 2. 檢查異常距離（起終點相同）
    if trip.PickupLat == trip.DropoffLat && trip.PickupLon == trip.DropoffLon {
        return true, "same_pickup_dropoff"
    }

    // 3. 檢查乘客信用評分
    var creditScore int
    f.db.QueryRowContext(ctx, "SELECT credit_score FROM riders WHERE id = ?", trip.RiderID).Scan(&creditScore)

    if creditScore < 20 {
        return true, "low_credit_score"
    }

    return false, ""
}
```

---

## 延伸閱讀

### 相關技術文章

1. [Uber 的地理空間索引演進](https://eng.uber.com/go-geofence/)
2. [動態定價算法詳解](https://eng.uber.com/ubers-dynamic-pricing-model/)
3. [大規模 WebSocket 架構](https://medium.com/@nerdijoe/scaling-websockets-9a31497af051)
4. [Redis Geo 性能優化](https://redis.io/docs/data-types/geospatial/)

### 開源專案

- [H3 - Uber's Hexagonal Hierarchical Spatial Index](https://github.com/uber/h3)
- [OSRM - Open Source Routing Machine](https://github.com/Project-OSRM/osrm-backend)
- [Centrifugo - Real-time messaging server](https://github.com/centrifugal/centrifugo)

---

**版本**: v1.0.0
**最後更新**: 2024-10-18
**維護者**: System Design Team
