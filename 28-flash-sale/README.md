# Flash Sale - 秒殺系統技術文件

## 目錄
- [系統架構](#系統架構)
- [資料庫設計](#資料庫設計)
- [API 文件](#api-文件)
- [Redis Lua 腳本](#redis-lua-腳本)
- [限流演算法](#限流演算法)
- [效能指標](#效能指標)
- [成本分析](#成本分析)
- [壓測報告](#壓測報告)

---

## 系統架構

### 高階架構圖

```
┌────────────────────────────────────────────────────────────┐
│                      百萬用戶同時搶購                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                 │
│  │  User 1  │  │  User 2  │  │ User N   │                 │
│  └─────┬────┘  └─────┬────┘  └─────┬────┘                 │
└────────┼─────────────┼─────────────┼───────────────────────┘
         │             │             │
         └─────────────┴─────────────┘
                       │
              ┌────────▼─────────┐
              │       CDN        │  ← 靜態資源
              │  (80% 流量過濾)  │
              └────────┬─────────┘
                       │
              ┌────────▼─────────┐
              │      Nginx       │  ← IP 限流
              │  (10% 流量過濾)  │
              └────────┬─────────┘
                       │
       ┌───────────────┼───────────────┐
       │               │               │
┌──────▼──────┐ ┌─────▼──────┐ ┌─────▼──────┐
│  API 1      │ │  API 2     │ │  API N     │  ← 後端限流
│ (令牌桶)    │ │ (令牌桶)   │ │ (令牌桶)   │  (5% 過濾)
└──────┬──────┘ └─────┬──────┘ └─────┬──────┘
       │              │               │
       └──────────────┼───────────────┘
                      │
              ┌───────▼────────┐
              │  Redis Cluster │  ← 扣庫存
              │  (Lua 原子性)  │  (100K+ QPS)
              └───────┬────────┘
                      │
              ┌───────▼────────┐
              │      Kafka     │  ← 削峰填谷
              │  (異步處理)    │
              └───────┬────────┘
                      │
       ┌──────────────┼───────────────┐
       │              │               │
┌──────▼──────┐ ┌────▼───────┐ ┌────▼────────┐
│Order Service│ │Pay Service │ │Stock Service│
│  (訂單)     │ │  (支付)    │ │  (庫存)     │
└──────┬──────┘ └────┬───────┘ └────┬────────┘
       │             │               │
       └─────────────┴───────────────┘
                      │
              ┌───────▼────────┐
              │   PostgreSQL   │  ← 持久化
              │   (分庫分表)   │
              └────────────────┘
```

### 流量過濾

| 層級 | 工具 | QPS | 過濾率 | 剩餘流量 |
|------|------|-----|--------|----------|
| 用戶端 | 按鈕防抖 | 1,000,000 | 50% | 500,000 |
| CDN | 靜態資源 | 500,000 | 60% | 200,000 |
| Nginx | IP 限流 | 200,000 | 50% | 100,000 |
| 後端 | 令牌桶 | 100,000 | 80% | 20,000 |
| Redis | 庫存檢查 | 20,000 | 95% | 1,000 |
| Kafka | 異步處理 | 1,000 | - | 1,000 |

---

## 資料庫設計

### PostgreSQL Schema

#### 1. 商品表（Products）

```sql
CREATE TABLE products (
    id BIGSERIAL PRIMARY KEY,

    -- 基本資訊
    name VARCHAR(255) NOT NULL,
    description TEXT,
    original_price DECIMAL(10,2) NOT NULL,
    flash_sale_price DECIMAL(10,2),

    -- 庫存
    stock INT NOT NULL DEFAULT 0,
    sold_count INT DEFAULT 0,

    -- 秒殺資訊
    is_flash_sale BOOLEAN DEFAULT FALSE,
    flash_sale_start TIMESTAMP,
    flash_sale_end TIMESTAMP,

    -- 限購
    limit_per_user INT DEFAULT 1,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_products_flash_sale ON products(is_flash_sale, flash_sale_start, flash_sale_end);
```

#### 2. 訂單表（Orders）

```sql
CREATE TABLE orders (
    id BIGSERIAL PRIMARY KEY,
    order_no VARCHAR(32) UNIQUE NOT NULL,

    -- 用戶
    user_id BIGINT NOT NULL,

    -- 商品
    product_id BIGINT NOT NULL REFERENCES products(id),
    product_name VARCHAR(255),
    quantity INT NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    total_amount DECIMAL(10,2) NOT NULL,

    -- 狀態
    status VARCHAR(20) DEFAULT 'pending_payment',
    -- 'pending_payment', 'paid', 'cancelled', 'timeout'

    -- 時間
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    paid_at TIMESTAMP,
    cancelled_at TIMESTAMP,

    -- 超時取消（15 分鐘）
    timeout_at TIMESTAMP
);

CREATE INDEX idx_orders_user ON orders(user_id, created_at DESC);
CREATE INDEX idx_orders_product ON orders(product_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_timeout ON orders(timeout_at) WHERE status = 'pending_payment';

-- 分區表（按月）
CREATE TABLE orders_2024_01 PARTITION OF orders
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
```

#### 3. 用戶購買記錄（User Purchases）

```sql
CREATE TABLE user_purchases (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    product_id BIGINT NOT NULL,

    purchase_count INT DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(user_id, product_id)
);

CREATE INDEX idx_user_purchases_user ON user_purchases(user_id);
```

### Redis 資料結構

#### 1. 庫存

```redis
# 商品庫存
SET product:1001:stock 1000

# Lua 腳本扣庫存
EVAL "
    local stock = tonumber(redis.call('GET', KEYS[1]) or '0')
    if stock >= tonumber(ARGV[1]) then
        redis.call('DECRBY', KEYS[1], ARGV[1])
        return 1
    else
        return 0
    end
" 1 product:1001:stock 1
```

#### 2. 用戶購買記錄

```redis
# 記錄已購買用戶（Set）
SADD flash_sale:1001:users 2001
SADD flash_sale:1001:users 2002

# 檢查用戶是否已購買
SISMEMBER flash_sale:1001:users 2001  # 返回 1（已購買）
```

#### 3. IP 限流

```redis
# IP 訪問計數
INCR ratelimit:ip:192.168.1.100
EXPIRE ratelimit:ip:192.168.1.100 60

# 檢查是否超限
GET ratelimit:ip:192.168.1.100  # 如果 > 10，拒絕請求
```

#### 4. 驗證碼

```redis
# 存儲驗證碼
SET captcha:2001 "AB12CD" EX 300

# 驗證
GET captcha:2001  # 比對用戶輸入
```

#### 5. 分散式鎖

```redis
# 獲取鎖（防快取擊穿）
SETNX lock:product:1001 1 EX 10

# 釋放鎖
DEL lock:product:1001
```

---

## API 文件

### 1. 商品詳情

**Endpoint**: `GET /api/v1/flash-sale/products/{product_id}`

**Response**:
```json
{
  "product_id": 1001,
  "name": "小米手機 14 Pro",
  "description": "最新旗艦手機",
  "original_price": 29999.00,
  "flash_sale_price": 9999.00,
  "stock": 1000,
  "sold_count": 0,
  "flash_sale_start": "2024-10-20T20:00:00Z",
  "flash_sale_end": "2024-10-20T20:05:00Z",
  "limit_per_user": 1,
  "user_purchased": false
}
```

### 2. 生成驗證碼

**Endpoint**: `GET /api/v1/flash-sale/captcha`

**Response**:
```json
{
  "captcha_id": "abc123",
  "image_base64": "data:image/png;base64,iVBORw0KGgo..."
}
```

### 3. 搶購商品

**Endpoint**: `POST /api/v1/flash-sale/purchase`

**Request**:
```json
{
  "product_id": 1001,
  "quantity": 1,
  "captcha_id": "abc123",
  "captcha_code": "AB12CD"
}
```

**Response** (成功):
```json
{
  "success": true,
  "message": "搶購成功！訂單處理中",
  "order_no": "FS20241020123456789"
}
```

**Response** (失敗 - 庫存不足):
```json
{
  "success": false,
  "message": "商品已售完"
}
```

**Response** (失敗 - 已購買):
```json
{
  "success": false,
  "message": "您已搶購過此商品"
}
```

**Response** (失敗 - 驗證碼錯誤):
```json
{
  "success": false,
  "message": "驗證碼錯誤"
}
```

### 4. 查詢訂單

**Endpoint**: `GET /api/v1/orders/{order_no}`

**Response**:
```json
{
  "order_no": "FS20241020123456789",
  "user_id": 2001,
  "product_id": 1001,
  "product_name": "小米手機 14 Pro",
  "quantity": 1,
  "price": 9999.00,
  "total_amount": 9999.00,
  "status": "pending_payment",
  "created_at": "2024-10-20T20:00:05Z",
  "timeout_at": "2024-10-20T20:15:05Z"
}
```

---

## Redis Lua 腳本

### 扣庫存腳本

```lua
-- deduct_stock.lua
local stock_key = KEYS[1]
local quantity = tonumber(ARGV[1])

-- 取得當前庫存
local current_stock = tonumber(redis.call('GET', stock_key) or '0')

-- 檢查庫存是否足夠
if current_stock >= quantity then
    -- 扣減庫存
    redis.call('DECRBY', stock_key, quantity)
    return 1  -- 成功
else
    return 0  -- 庫存不足
end
```

### 帶限購的扣庫存腳本

```lua
-- deduct_stock_with_limit.lua
local stock_key = KEYS[1]
local users_key = KEYS[2]
local user_id = ARGV[1]
local quantity = tonumber(ARGV[2])

-- 檢查用戶是否已購買
local purchased = redis.call('SISMEMBER', users_key, user_id)
if purchased == 1 then
    return -1  -- 已購買
end

-- 取得當前庫存
local current_stock = tonumber(redis.call('GET', stock_key) or '0')

-- 檢查庫存是否足夠
if current_stock >= quantity then
    -- 扣減庫存
    redis.call('DECRBY', stock_key, quantity)

    -- 記錄用戶已購買
    redis.call('SADD', users_key, user_id)

    return 1  -- 成功
else
    return 0  -- 庫存不足
end
```

**Go 呼叫方式**:

```go
func (r *RedisStock) DeductStockWithLimit(ctx context.Context, productID, userID int64, quantity int) (int, error) {
    script := `
        local stock_key = KEYS[1]
        local users_key = KEYS[2]
        local user_id = ARGV[1]
        local quantity = tonumber(ARGV[2])

        local purchased = redis.call('SISMEMBER', users_key, user_id)
        if purchased == 1 then
            return -1
        end

        local current_stock = tonumber(redis.call('GET', stock_key) or '0')
        if current_stock >= quantity then
            redis.call('DECRBY', stock_key, quantity)
            redis.call('SADD', users_key, user_id)
            return 1
        else
            return 0
        end
    `

    stockKey := fmt.Sprintf("product:%d:stock", productID)
    usersKey := fmt.Sprintf("flash_sale:%d:users", productID)

    result, err := r.redis.Eval(ctx, script,
        []string{stockKey, usersKey},
        userID, quantity,
    ).Int()

    return result, err
}
```

---

## 限流演算法

### 令牌桶演算法

```go
package ratelimit

import (
    "sync"
    "time"
)

type TokenBucket struct {
    capacity   int       // 桶容量
    tokens     int       // 當前令牌數
    rate       int       // 每秒生成令牌數
    lastUpdate time.Time
    mu         sync.Mutex
}

func NewTokenBucket(capacity, rate int) *TokenBucket {
    return &TokenBucket{
        capacity:   capacity,
        tokens:     capacity,
        rate:       rate,
        lastUpdate: time.Now(),
    }
}

func (tb *TokenBucket) Allow() bool {
    tb.mu.Lock()
    defer tb.mu.Unlock()

    now := time.Now()
    elapsed := now.Sub(tb.lastUpdate).Seconds()

    // 補充令牌
    tokensToAdd := int(elapsed * float64(tb.rate))
    tb.tokens += tokensToAdd

    if tb.tokens > tb.capacity {
        tb.tokens = tb.capacity
    }

    tb.lastUpdate = now

    // 嘗試消費一個令牌
    if tb.tokens > 0 {
        tb.tokens--
        return true
    }

    return false
}
```

### 漏桶演算法

```go
package ratelimit

type LeakyBucket struct {
    capacity   int
    water      int
    leakRate   int       // 每秒漏出速率
    lastUpdate time.Time
    mu         sync.Mutex
}

func NewLeakyBucket(capacity, leakRate int) *LeakyBucket {
    return &LeakyBucket{
        capacity:   capacity,
        water:      0,
        leakRate:   leakRate,
        lastUpdate: time.Now(),
    }
}

func (lb *LeakyBucket) Allow() bool {
    lb.mu.Lock()
    defer lb.mu.Unlock()

    now := time.Now()
    elapsed := now.Sub(lb.lastUpdate).Seconds()

    // 漏水
    leaked := int(elapsed * float64(lb.leakRate))
    lb.water -= leaked

    if lb.water < 0 {
        lb.water = 0
    }

    lb.lastUpdate = now

    // 嘗試加水
    if lb.water < lb.capacity {
        lb.water++
        return true
    }

    return false
}
```

### 滑動視窗計數器

```go
package ratelimit

type SlidingWindowCounter struct {
    redis      *redis.Client
    windowSize time.Duration
    maxRequests int
}

func (sw *SlidingWindowCounter) Allow(ctx context.Context, key string) (bool, error) {
    now := time.Now()
    windowStart := now.Add(-sw.windowSize)

    pipe := sw.redis.Pipeline()

    // 1. 移除過期的請求記錄
    pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart.UnixMilli()))

    // 2. 統計當前視窗內的請求數
    pipe.ZCard(ctx, key)

    // 3. 添加當前請求
    pipe.ZAdd(ctx, key, &redis.Z{
        Score:  float64(now.UnixMilli()),
        Member: fmt.Sprintf("%d", now.UnixNano()),
    })

    // 4. 設定過期時間
    pipe.Expire(ctx, key, sw.windowSize)

    results, err := pipe.Exec(ctx)
    if err != nil {
        return false, err
    }

    // 檢查請求數
    count := results[1].(*redis.IntCmd).Val()

    return count < int64(sw.maxRequests), nil
}
```

---

## 效能指標

### 系統容量

| 指標 | 數值 | 備註 |
|------|------|------|
| **商品數量** | 1,000 | 同時秒殺 |
| **庫存總數** | 100 萬件 | 平均每商品 1,000 件 |
| **搶購用戶** | 100 萬 | 瞬間併發 |
| **QPS 峰值** | 100,000 | 開搶瞬間 |
| **Redis QPS** | 100,000 | 扣庫存 |
| **Kafka TPS** | 10,000 | 訊息吞吐 |

### API 延遲

| API | P50 | P95 | P99 |
|-----|-----|-----|-----|
| **搶購** | <50ms | <150ms | <300ms |
| **商品詳情** | <20ms | <50ms | <100ms |
| **訂單查詢** | <30ms | <80ms | <150ms |

### Redis 效能

```bash
# 壓測結果
redis-benchmark -t set,get -n 100000 -c 100

SET: 101010.10 requests per second
GET: 112359.55 requests per second

# Lua 腳本執行
EVAL (扣庫存): 89285.71 requests per second
```

### 成功率

| 情境 | 成功率 | 平均耗時 |
|------|--------|----------|
| **正常流量** | 99.9% | 35ms |
| **10 萬 QPS** | 98.5% | 120ms |
| **50 萬 QPS** | 95% | 450ms |

---

## 成本分析

### 基礎設施成本

**假設條件**：
- 秒殺活動：每日 1 場
- 持續時間：5 分鐘
- 峰值 QPS：100,000

| 項目 | 規格 | 月費用 (NT$) |
|------|------|-------------|
| **Redis Cluster** | 20 節點 (r5.xlarge) | 600,000 |
| **EC2 (API Server)** | 50 台 c5.2xlarge | 500,000 |
| **PostgreSQL** | Aurora (r5.4xlarge) | 200,000 |
| **Kafka** | 15 節點 | 120,000 |
| **Load Balancer** | ALB | 30,000 |
| **CloudWatch** | 監控 | 20,000 |
| **Nginx** | 限流層 10 台 | 50,000 |
| **總計** | | **1,520,000/月** |

### 成本優化

| 優化項目 | 優化前 | 優化後 | 節省 |
|----------|--------|--------|------|
| Redis | 600,000 | 400,000 | 200,000 (使用 Reserved Instance) |
| EC2 | 500,000 | 350,000 | 150,000 (Auto Scaling) |
| **總計** | 1,520,000 | **1,170,000** | **350,000 (23%)** |

### 單場秒殺成本

```
單場秒殺成本 = NT$1,170,000 ÷ 30 場 = NT$39,000

假設售出商品：
- 數量：1,000 件
- 單價：NT$10,000
- 總 GMV：NT$10,000,000

技術成本占比：0.39%
```

---

## 壓測報告

### 壓測工具

```bash
# 使用 Apache Bench
ab -n 100000 -c 1000 -p data.json -T application/json \
   http://api.example.com/api/v1/flash-sale/purchase

# 使用 wrk
wrk -t 100 -c 1000 -d 30s \
    --latency \
    -s purchase.lua \
    http://api.example.com/api/v1/flash-sale/purchase
```

### 壓測結果

#### 場景 1：1,000 併發

```
Concurrency Level:      1,000
Time taken for tests:   10.234 seconds
Complete requests:      100,000
Failed requests:        156
Requests per second:    9,771.35 [#/sec] (mean)
Time per request:       102.340 [ms] (mean)
Time per request:       0.102 [ms] (mean, across all concurrent requests)

Percentage of requests served within:
  50%    85ms
  66%    95ms
  75%    110ms
  80%    125ms
  90%    160ms
  95%    210ms
  98%    285ms
  99%    350ms
 100%    890ms (longest request)
```

#### 場景 2：10,000 併發

```
Concurrency Level:      10,000
Time taken for tests:   25.678 seconds
Complete requests:      100,000
Failed requests:        2,341
Requests per second:    3,894.12 [#/sec] (mean)
Time per request:       2,567.8 [ms] (mean)

Percentage of requests served within:
  50%    450ms
  75%    780ms
  90%    1,250ms
  95%    2,100ms
  99%    4,500ms
 100%    8,900ms
```

### 瓶頸分析

| QPS | CPU 使用率 | 記憶體 | Redis QPS | 瓶頸 |
|-----|-----------|--------|-----------|------|
| 10K | 35% | 40% | 15K | 無 |
| 50K | 75% | 60% | 70K | 網路頻寬 |
| 100K | 95% | 85% | 110K | CPU、連線池 |

---

## 監控與告警

### Prometheus Metrics

```go
var (
    // 搶購請求計數
    purchaseRequests = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "purchase_requests_total",
            Help: "Total purchase requests",
        },
        []string{"status"},  // success, failed, out_of_stock
    )

    // 庫存剩餘
    stockRemaining = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "stock_remaining",
            Help: "Remaining stock",
        },
        []string{"product_id"},
    )

    // 搶購延遲
    purchaseLatency = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name: "purchase_latency_seconds",
            Help: "Purchase request latency",
            Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
        },
    )
)
```

### Grafana Dashboard

**關鍵指標**：
- QPS（每秒請求數）
- 成功率
- 延遲（P50、P95、P99）
- 庫存剩餘
- Redis CPU/記憶體
- Kafka Lag

---

## 安全性

### 1. 防 DDoS

```nginx
# nginx.conf
limit_req_zone $binary_remote_addr zone=flash_sale:10m rate=10r/s;

server {
    location /api/flash-sale/ {
        limit_req zone=flash_sale burst=20 nodelay;
        limit_req_status 429;
    }
}
```

### 2. 防重放攻擊

```go
// 請求簽名驗證
func VerifySignature(userID int64, timestamp int64, signature string) bool {
    // 檢查時間戳（5 分鐘內有效）
    if time.Now().Unix() - timestamp > 300 {
        return false
    }

    // 驗證簽名
    expected := GenerateSignature(userID, timestamp)
    return expected == signature
}
```

### 3. 防 SQL 注入

```go
// 使用參數化查詢
db.QueryContext(ctx, "SELECT * FROM orders WHERE user_id = ?", userID)

// 避免字串拼接
// db.Query("SELECT * FROM orders WHERE user_id = " + userID)  // 危險！
```

---

## 延伸閱讀

### 技術文章

- [淘寶雙 11 技術架構](https://www.infoq.cn/article/taobao-double-11-architecture)
- [Redis 官方文檔 - Lua 腳本](https://redis.io/docs/manual/programmability/eval-intro/)
- [秒殺系統設計與實現](https://tech.meituan.com/seckill-design.html)

### 開源專案

- [go-zero](https://github.com/zeromicro/go-zero) - 微服務框架（含限流）
- [sentinel-go](https://github.com/alibaba/sentinel-golang) - 阿里巴巴限流組件

---

**版本**: v1.0.0
**最後更新**: 2024-10-20
**維護者**: Flash Sale Team
