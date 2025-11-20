# Food Delivery (UberEats) - 外送平台技術文件

## 目錄
- [系統架構](#系統架構)
- [資料庫設計](#資料庫設計)
- [API 文件](#api-文件)
- [路線優化演算法](#路線優化演算法)
- [效能指標](#效能指標)
- [成本分析](#成本分析)
- [部署架構](#部署架構)

---

## 系統架構

### 高階架構圖

```
┌────────────────────────────────────────────────────────────┐
│                      三端應用程式                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                 │
│  │Customer  │  │ Driver   │  │Restaurant│                 │
│  │   App    │  │   App    │  │   App    │                 │
│  └─────┬────┘  └─────┬────┘  └─────┬────┘                 │
└────────┼─────────────┼─────────────┼───────────────────────┘
         │             │             │
         └─────────────┴─────────────┘
                       │
              ┌────────▼─────────┐
              │  Load Balancer   │
              └────────┬─────────┘
                       │
       ┌───────────────┼───────────────┐
       │               │               │
┌──────▼──────┐ ┌─────▼──────┐ ┌─────▼──────┐
│ Order Svc   │ │Matching Svc│ │Routing Svc │
│  (訂單)     │ │ (匹配)     │ │ (路線)     │
└──────┬──────┘ └─────┬──────┘ └─────┬──────┘
       │              │               │
       │      ┌───────▼───────┐       │
       │      │ Tracking Svc  │       │
       │      │  (追蹤)       │       │
       │      └───────┬───────┘       │
       │              │               │
       └──────────────┼───────────────┘
                      │
       ┌──────────────┼──────────────┐
       │              │              │
┌──────▼─────┐ ┌─────▼────┐ ┌──────▼──────┐
│PostgreSQL  │ │   Redis  │ │     Kafka   │
│(訂單/用戶) │ │(位置/鎖) │ │  (事件流)   │
└────────────┘ └──────────┘ └─────────────┘
```

### 微服務架構

| 服務 | 職責 | 技術棧 | QPS |
|------|------|--------|-----|
| **Order Service** | 訂單管理、狀態機 | Go, PostgreSQL | 20K |
| **Matching Service** | 外送員匹配 | Go, Redis | 10K |
| **Routing Service** | 路線規劃、TSP | Go, A* | 5K |
| **Tracking Service** | 即時追蹤 | Go, WebSocket | 50K |
| **Pricing Service** | 定價、Surge | Go, Redis | 15K |
| **ETA Service** | ETA 預測 | Go, ML Model | 10K |
| **Notification Service** | 推播通知 | Go, Firebase | 30K |

---

## 資料庫設計

### PostgreSQL Schema

#### 1. 訂單表（Orders）

```sql
CREATE TABLE orders (
    id BIGSERIAL PRIMARY KEY,

    -- 關聯
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    restaurant_id BIGINT NOT NULL REFERENCES restaurants(id),
    driver_id BIGINT REFERENCES drivers(id),

    -- 狀態
    status VARCHAR(30) NOT NULL,

    -- 地點
    restaurant_lat DECIMAL(10,8) NOT NULL,
    restaurant_lng DECIMAL(11,8) NOT NULL,
    delivery_lat DECIMAL(10,8) NOT NULL,
    delivery_lng DECIMAL(11,8) NOT NULL,
    delivery_address TEXT NOT NULL,
    delivery_instructions TEXT,

    -- 時間
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    confirmed_at TIMESTAMP,
    assigned_at TIMESTAMP,
    picked_up_at TIMESTAMP,
    delivered_at TIMESTAMP,

    -- 預估時間
    estimated_prep_time INT,          -- 準備時間（秒）
    estimated_pickup_time TIMESTAMP,
    estimated_delivery_time TIMESTAMP,

    -- 金額
    food_price DECIMAL(10,2) NOT NULL,
    delivery_fee DECIMAL(10,2) NOT NULL,
    tip DECIMAL(10,2) DEFAULT 0,
    total_price DECIMAL(10,2) NOT NULL,

    -- 其他
    special_requests TEXT,
    rating INT,
    review TEXT,

    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_orders_customer ON orders(customer_id, created_at DESC);
CREATE INDEX idx_orders_restaurant ON orders(restaurant_id, created_at DESC);
CREATE INDEX idx_orders_driver ON orders(driver_id, created_at DESC);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_created_at ON orders(created_at DESC);

-- 分區表（按月）
CREATE TABLE orders_2024_01 PARTITION OF orders
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
```

#### 2. 訂單項目表（Order Items）

```sql
CREATE TABLE order_items (
    id BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES orders(id),

    item_name VARCHAR(255) NOT NULL,
    quantity INT NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    customizations JSONB,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_order_items_order ON order_items(order_id);
```

#### 3. 外送員表（Drivers）

```sql
CREATE TABLE drivers (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id),

    -- 狀態
    status VARCHAR(20) DEFAULT 'offline',  -- 'offline', 'available', 'on_delivery'
    current_order_count INT DEFAULT 0,

    -- 位置（最後已知位置）
    last_latitude DECIMAL(10,8),
    last_longitude DECIMAL(11,8),
    last_bearing DECIMAL(5,2),
    last_location_update TIMESTAMP,

    -- 車輛資訊
    vehicle_type VARCHAR(50),         -- 'motorcycle', 'bicycle', 'car'
    vehicle_number VARCHAR(50),

    -- 統計
    total_deliveries INT DEFAULT 0,
    rating DECIMAL(3,2) DEFAULT 5.00,
    acceptance_rate DECIMAL(3,2) DEFAULT 1.00,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_drivers_status ON drivers(status);
CREATE INDEX idx_drivers_location_update ON drivers(last_location_update);
```

#### 4. 餐廳表（Restaurants）

```sql
CREATE TABLE restaurants (
    id BIGSERIAL PRIMARY KEY,

    name VARCHAR(255) NOT NULL,
    latitude DECIMAL(10,8) NOT NULL,
    longitude DECIMAL(11,8) NOT NULL,
    location GEOGRAPHY(POINT, 4326),
    address TEXT,

    -- 營業資訊
    opening_hours JSONB,
    average_prep_time INT,            -- 平均準備時間（秒）

    -- 統計
    rating DECIMAL(3,2),
    total_orders INT DEFAULT 0,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_restaurants_location ON restaurants USING GIST (location);
```

#### 5. 路線表（Routes）

```sql
CREATE TABLE routes (
    id BIGSERIAL PRIMARY KEY,
    driver_id BIGINT NOT NULL REFERENCES drivers(id),

    -- 路線資訊
    stops JSONB,                      -- 停靠點序列
    total_distance DECIMAL(10,2),    -- 總距離（km）
    total_time INT,                  -- 總時間（秒）

    status VARCHAR(20),              -- 'active', 'completed'

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_routes_driver ON routes(driver_id, created_at DESC);
```

### Redis 資料結構

#### 1. 外送員位置（Geo）

```redis
# 儲存所有在線外送員的位置
GEOADD drivers:online 121.5170 25.0478 "driver:1001"
GEOADD drivers:online 121.5200 25.0500 "driver:1002"

# 查詢附近 3 km 的外送員
GEORADIUS drivers:online 121.5170 25.0478 3 km WITHDIST WITHCOORD
```

#### 2. 外送員狀態

```redis
# 外送員詳細狀態
HSET driver:1001 status "on_delivery"
HSET driver:1001 current_orders "2"
HSET driver:1001 latitude "25.0478"
HSET driver:1001 longitude "121.5170"
EXPIRE driver:1001 300
```

#### 3. 訂單鎖（分散式鎖）

```redis
# 鎖定外送員（防止重複派單）
SETNX driver:1001:lock order:5001 EX 30
```

#### 4. 待配對訂單佇列

```redis
# 按區域分組的待配對訂單
ZADD pending_orders:taipei 1634567890 "order:5001"
ZADD pending_orders:taipei 1634567895 "order:5002"
```

---

## API 文件

### 1. 顧客下單

**Endpoint**: `POST /api/v1/orders`

**Request**:
```json
{
  "restaurant_id": 101,
  "delivery_address": {
    "latitude": 25.0478,
    "longitude": 121.5170,
    "address": "台北市中正區忠孝西路一段49號",
    "instructions": "請放在警衛室"
  },
  "items": [
    {
      "item_id": 1001,
      "name": "炸雞套餐",
      "quantity": 2,
      "price": 150.0,
      "customizations": {
        "spicy_level": "medium"
      }
    }
  ],
  "payment_method": "credit_card",
  "tip": 20.0
}
```

**Response**:
```json
{
  "order_id": 5001,
  "status": "pending",
  "total_price": 350.0,
  "delivery_fee": 30.0,
  "estimated_delivery_time": "2024-10-18T13:45:00Z",
  "created_at": "2024-10-18T13:10:00Z"
}
```

### 2. 外送員接單

**Endpoint**: `POST /api/v1/drivers/{driver_id}/accept-order`

**Request**:
```json
{
  "order_id": 5001
}
```

**Response**:
```json
{
  "order_id": 5001,
  "status": "driver_assigned",
  "restaurant": {
    "id": 101,
    "name": "KFC 台北車站店",
    "latitude": 25.0475,
    "longitude": 121.5168,
    "address": "台北市中正區忠孝西路一段47號"
  },
  "delivery": {
    "latitude": 25.0478,
    "longitude": 121.5170,
    "address": "台北市中正區忠孝西路一段49號"
  },
  "estimated_earnings": 24.0
}
```

### 3. 更新訂單狀態

**Endpoint**: `PUT /api/v1/orders/{order_id}/status`

**Request**:
```json
{
  "status": "picked_up"
}
```

**Response**:
```json
{
  "order_id": 5001,
  "status": "picked_up",
  "estimated_delivery_time": "2024-10-18T13:45:00Z"
}
```

### 4. 即時追蹤（WebSocket）

**Endpoint**: `ws://api.ubereats.com/api/v1/tracking/{order_id}`

**接收訊息**:
```json
{
  "type": "location_update",
  "driver_id": 1001,
  "latitude": 25.0476,
  "longitude": 121.5169,
  "bearing": 45.5,
  "timestamp": "2024-10-18T13:35:00Z"
}
```

```json
{
  "type": "status_change",
  "order_id": 5001,
  "status": "nearby",
  "message": "外送員即將到達"
}
```

### 5. 取得路線

**Endpoint**: `GET /api/v1/drivers/{driver_id}/route`

**Response**:
```json
{
  "driver_id": 1001,
  "current_orders": [5001, 5002],
  "route": {
    "stops": [
      {
        "type": "pickup",
        "order_id": 5001,
        "location": {"lat": 25.0475, "lng": 121.5168},
        "address": "KFC 台北車站店",
        "eta": 120
      },
      {
        "type": "pickup",
        "order_id": 5002,
        "location": {"lat": 25.0480, "lng": 121.5170},
        "address": "麥當勞",
        "eta": 300
      },
      {
        "type": "delivery",
        "order_id": 5001,
        "location": {"lat": 25.0478, "lng": 121.5170},
        "address": "忠孝西路一段49號",
        "eta": 600
      },
      {
        "type": "delivery",
        "order_id": 5002,
        "location": {"lat": 25.0490, "lng": 121.5180},
        "address": "忠孝西路一段55號",
        "eta": 900
      }
    ],
    "total_distance": 2.5,
    "total_time": 900
  }
}
```

---

## 路線優化演算法

### TSP 問題複雜度

| 停靠點數 | 可能路線數 | 計算複雜度 |
|---------|-----------|------------|
| 2 | 2 | O(N!) |
| 3 | 6 | |
| 4 | 24 | |
| 5 | 120 | |
| 6 | 720 | |
| 10 | 3,628,800 | 不可行 |

### 演算法選擇

| 演算法 | 時間複雜度 | 品質 | 適用場景 |
|--------|-----------|------|----------|
| **暴力法** | O(N!) | 最優 | N ≤ 4 |
| **貪婪法** | O(N²) | 70-80% | 快速初解 |
| **2-opt** | O(N²) | 90-95% | 局部優化 |
| **Genetic Algorithm** | O(G×N²) | 95-99% | 大規模 N > 20 |

### 2-opt 優化示例

```
初始路線：A → B → C → D → E → A
距離：10 + 8 + 12 + 9 + 11 = 50

2-opt 嘗試反轉：
1. A → C → B → D → E → A (距離 48) ✓ 更好
2. A → B → D → C → E → A (距離 52) ✗
3. A → B → C → E → D → A (距離 47) ✓ 更好

最終路線：A → B → C → E → D → A
距離：10 + 8 + 10 + 8 + 11 = 47 (節省 6%)
```

### 約束條件

1. **取餐-送達順序**：取餐必須在送達之前
2. **時間窗口**：必須在規定時間內送達
3. **容量限制**：外送員最多 3 單
4. **繞路限制**：繞路距離 < 1 km

---

## 效能指標

### 系統容量

| 指標 | 數值 | 備註 |
|------|------|------|
| **日均訂單** | 100 萬 | 平均每秒 11.6 單 |
| **尖峰 QPS** | 500 | 中午、晚餐時段 |
| **在線外送員** | 5 萬 | 尖峰時段 |
| **在線餐廳** | 20 萬 | |
| **活躍用戶** | 500 萬/月 | |

### API 延遲

| API | P50 | P95 | P99 |
|-----|-----|-----|-----|
| **下單** | <100ms | <300ms | <500ms |
| **匹配外送員** | <200ms | <500ms | <1s |
| **更新狀態** | <50ms | <150ms | <300ms |
| **路線優化** | <300ms | <800ms | <1.5s |
| **ETA 預測** | <100ms | <300ms | <500ms |

### 匹配成功率

| 時段 | 成功率 | 平均等待時間 |
|------|--------|-------------|
| **非尖峰** | 98% | 30 秒 |
| **尖峰** | 92% | 90 秒 |
| **深夜** | 85% | 180 秒 |

### ETA 準確度

```
準確度定義：實際送達時間與預估時間差異 ≤ 5 分鐘

準確度: 85%
平均誤差: ±3 分鐘
```

---

## 成本分析

### 台灣市場成本估算

**假設條件**：
- 日均訂單：10 萬
- 在線外送員：5,000
- 活躍餐廳：20,000

| 項目 | 規格 | 月費用 (NT$) |
|------|------|-------------|
| **運算（EC2）** | 50 台 c5.xlarge | 500,000 |
| **資料庫（RDS）** | r5.2xlarge Multi-AZ | 150,000 |
| **Redis** | 20 節點 | 120,000 |
| **Kafka** | 10 節點 | 80,000 |
| **WebSocket 伺服器** | 30 台 | 300,000 |
| **簡訊/推播** | 300 萬則 | 90,000 |
| **地圖 API** | 30 萬次/日 | 300,000 |
| **頻寬** | 50 TB | 90,000 |
| **監控** | Datadog | 30,000 |
| **總計** | | **1,660,000/月** |

### 單筆訂單成本

```
單筆訂單技術成本 = NT$1,660,000 ÷ (100,000 × 30)
                 = NT$0.55

收入結構（單筆 NT$300）：
- 餐點費用：NT$250 (83.3%) → 餐廳收入
- 外送費：NT$30 (10%) → 外送員收入 NT$24，平台收入 NT$6
- 平台服務費：NT$20 (6.7%) → 平台收入（從餐廳抽成 8%）

平台總收入：NT$6 + NT$20 = NT$26
平台成本：NT$0.55
平台淨利：NT$25.45 (單筆毛利率 8.5%)
```

### 成本優化策略

| 優化項目 | 原成本 | 優化後 | 節省 | 方法 |
|----------|--------|--------|------|------|
| 地圖 API | 300,000 | 50,000 | 250,000 | 自建路徑引擎 |
| WebSocket | 300,000 | 180,000 | 120,000 | 連線池複用 |
| 簡訊 | 90,000 | 45,000 | 45,000 | App 推播為主 |
| **總計** | 1,660,000 | **1,245,000** | **415,000** | **節省 25%** |

---

## 部署架構

### Kubernetes 部署

```yaml
# matching-service.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: matching-service
spec:
  replicas: 10
  selector:
    matchLabels:
      app: matching-service
  template:
    metadata:
      labels:
        app: matching-service
    spec:
      containers:
      - name: matching-service
        image: ubereats/matching-service:v2.0.0
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
        - name: REDIS_HOST
          valueFrom:
            configMapKeyRef:
              name: redis-config
              key: host
        - name: DB_HOST
          valueFrom:
            secretKeyRef:
              name: db-secret
              key: host
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10

---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: matching-service-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: matching-service
  minReplicas: 10
  maxReplicas: 50
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
        name: pending_orders
      target:
        type: AverageValue
        averageValue: "100"
```

---

## 監控與告警

### 關鍵指標

```go
// Prometheus Metrics
var (
    // 訂單指標
    ordersCreated = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "orders_created_total",
            Help: "Total number of orders created",
        },
    )

    // 匹配延遲
    matchingLatency = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name: "matching_latency_seconds",
            Help: "Latency of driver matching",
            Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
        },
    )

    // 配對成功率
    matchingSuccessRate = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "matching_success_rate",
            Help: "Success rate of driver matching",
        },
    )

    // 在線外送員
    onlineDrivers = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "online_drivers",
            Help: "Number of online drivers",
        },
    )
)
```

### 告警規則

```yaml
# alerts.yaml
groups:
  - name: delivery_alerts
    rules:
      - alert: HighMatchingLatency
        expr: histogram_quantile(0.95, matching_latency_seconds) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Matching latency is high"

      - alert: LowMatchingSuccessRate
        expr: matching_success_rate < 0.9
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: "Matching success rate below 90%"

      - alert: InsufficientDrivers
        expr: online_drivers / pending_orders < 0.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Driver supply is low"
```

---

## 延伸閱讀

### 開源專案

- [OSRM](https://github.com/Project-OSRM/osrm-backend) - 路徑規劃引擎
- [OR-Tools](https://github.com/google/or-tools) - Google 優化工具（TSP）

### 技術論文

- [Traveling Salesman Problem](https://en.wikipedia.org/wiki/Travelling_salesman_problem)
- [Vehicle Routing Problem](https://en.wikipedia.org/wiki/Vehicle_routing_problem)

---

**版本**: v1.0.0
**最後更新**: 2024-10-18
**維護者**: Food Delivery Platform Team
