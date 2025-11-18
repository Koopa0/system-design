# Chapter 28: Flash Sale - 秒殺系統設計

## 系統概述

秒殺系統是電商平台在特定時間以超低價格限量銷售商品的促銷活動，如淘寶雙 11、小米手機搶購、演唱會搶票等。本章將深入探討如何設計一個能承受百萬級併發的秒殺系統。

**核心挑戰**：
- 超高併發（開搶瞬間 100 萬人同時點擊）
- 防止超賣（1000 件商品不能賣出 1001 件）
- 削峰填谷（平滑流量高峰）
- 防止黃牛（限制機器人刷單）
- 快速響應（用戶體驗良好）
- 數據一致性（庫存、訂單一致）

---

## Act 1: 秒殺系統的挑戰

**場景**：週六晚上 8 點，小米最新手機開搶，1000 支手機，100 萬人搶購...

### 1.1 對話：Emma 與 David 討論秒殺的技術挑戰

**Emma**（產品經理）：我們要做一個秒殺活動，1000 支手機 NT$9,999，預計會有 100 萬人搶購。系統能承受嗎？

**David**（後端工程師）：讓我計算一下... 如果 100 萬人在開搶瞬間（假設 10 秒內）同時點擊「立即購買」：

```
QPS = 1,000,000 / 10 = 100,000 QPS
```

這是**平常流量的 1000 倍**！我們的系統撐不住。

**Michael**（資深架構師）：而且這還只是開始。真正的挑戰有三個：

### 1.2 三大核心挑戰

#### 挑戰 1：超高併發

```
正常流量: 100 QPS
秒殺瞬間: 100,000 QPS (1000 倍)

瞬間流量峰值：
- 網路頻寬: 100,000 請求 × 1KB = 100 MB/s
- 資料庫連線: 100,000 併發查詢 → 資料庫直接掛掉
- 庫存扣減: 需要保證原子性
```

#### 挑戰 2：超賣問題

```go
// 錯誤的庫存扣減方式（會超賣！）
func BuyProduct(productID int64) error {
    // 1. 查詢庫存
    stock := db.Query("SELECT stock FROM products WHERE id = ?", productID)

    // 2. 檢查庫存
    if stock > 0 {
        // 3. 扣減庫存
        db.Exec("UPDATE products SET stock = stock - 1 WHERE id = ?", productID)
        return nil
    }

    return errors.New("out of stock")
}

// 問題：
// 時間 T1: 用戶 A 查詢庫存 = 1
// 時間 T2: 用戶 B 查詢庫存 = 1（還沒扣減）
// 時間 T3: 用戶 A 扣減庫存 = 0
// 時間 T4: 用戶 B 扣減庫存 = -1（超賣！）
```

#### 挑戰 3：系統雪崩

```
秒殺開始 → 大量請求湧入 → 資料庫壓力激增 → 回應變慢
→ 更多請求堆積 → 連線池耗盡 → 系統崩潰 → 所有服務掛掉
```

---

## Act 2: Redis 扣庫存 - 解決超賣問題

**場景**：如何保證 1000 件商品不會賣出 1001 件？

### 2.1 對話：為什麼用 Redis？

**Emma**：為什麼不能直接用資料庫扣庫存？

**Michael**：
1. **效能問題**：資料庫 QPS 上限約 5,000，無法承受 100,000 QPS
2. **鎖競爭**：100,000 個請求同時更新同一行資料，鎖競爭激烈
3. **Redis 優勢**：
   - 單線程模型，天然原子性
   - QPS 可達 100,000+
   - 記憶體操作，速度快

### 2.2 Redis 扣庫存實作

```go
// internal/stock/redis_stock.go
package stock

import (
    "context"
    "github.com/go-redis/redis/v8"
)

type RedisStock struct {
    redis *redis.Client
}

// DeductStock 扣減庫存（使用 Lua 腳本保證原子性）
func (r *RedisStock) DeductStock(ctx context.Context, productID int64, quantity int) (bool, error) {
    // Lua 腳本（原子性執行）
    script := `
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
    `

    key := fmt.Sprintf("product:%d:stock", productID)

    result, err := r.redis.Eval(ctx, script, []string{key}, quantity).Int()
    if err != nil {
        return false, err
    }

    return result == 1, nil
}

// PreloadStock 預載庫存到 Redis（秒殺開始前）
func (r *RedisStock) PreloadStock(ctx context.Context, productID int64, stock int) error {
    key := fmt.Sprintf("product:%d:stock", productID)

    // 設定庫存
    err := r.redis.Set(ctx, key, stock, 0).Err()
    if err != nil {
        return err
    }

    return nil
}

// GetStock 查詢當前庫存
func (r *RedisStock) GetStock(ctx context.Context, productID int64) (int, error) {
    key := fmt.Sprintf("product:%d:stock", productID)

    stock, err := r.redis.Get(ctx, key).Int()
    if err == redis.Nil {
        return 0, nil
    }

    return stock, err
}
```

### 2.3 為什麼用 Lua 腳本？

**Sarah**（前端工程師）：為什麼不能用多個 Redis 命令？

**Michael**：

```go
// 錯誤方式（非原子性）
func DeductStockWrong(ctx context.Context, productID int64) error {
    key := fmt.Sprintf("product:%d:stock", productID)

    // 1. 取得庫存
    stock, _ := redis.Get(ctx, key).Int()

    // 2. 檢查庫存
    if stock > 0 {
        // 3. 扣減庫存
        redis.Decr(ctx, key)
        return nil
    }

    return errors.New("out of stock")
}

// 問題：步驟 1、2、3 之間有時間間隔，可能被其他請求插入！
```

**Lua 腳本的優勢**：
- **原子性**：整個腳本在 Redis 中原子性執行
- **無網路往返**：所有邏輯在 Redis 伺服器執行
- **效能**：減少網路延遲

---

## Act 3: 消息隊列削峰

**場景**：100,000 QPS 的流量直接打到資料庫會崩潰，如何削峰？

### 3.1 對話：削峰填谷的概念

**Emma**：就算 Redis 能扛住 100,000 QPS，但後續要建立訂單、扣款、發貨，這些還是要寫資料庫啊？

**Michael**：這就是**削峰填谷**的用途！我們用**消息隊列**（Message Queue）。

```
流程：
1. 用戶搶購 → Redis 扣庫存（瞬間完成）
2. 返回「搶購成功，訂單處理中」
3. 將訂單資訊寫入 Kafka
4. 後台慢慢消費 Kafka，建立訂單、扣款

效果：
- 前端：100,000 QPS（Redis 承受）
- 後端：1,000 QPS（資料庫可承受）
```

### 3.2 消息隊列實作

```go
// internal/flashsale/service.go
package flashsale

type FlashSaleService struct {
    redisStock *RedisStock
    kafka      *KafkaProducer
    db         *PostgreSQL
}

// Purchase 搶購商品
func (f *FlashSaleService) Purchase(ctx context.Context, userID, productID int64) (*PurchaseResult, error) {
    // 1. 檢查用戶是否已搶購（限購 1 件）
    purchased, err := f.checkUserPurchased(ctx, userID, productID)
    if err != nil {
        return nil, err
    }
    if purchased {
        return &PurchaseResult{
            Success: false,
            Message: "您已搶購過此商品",
        }, nil
    }

    // 2. Redis 扣庫存（原子性操作）
    success, err := f.redisStock.DeductStock(ctx, productID, 1)
    if err != nil {
        return nil, err
    }

    if !success {
        return &PurchaseResult{
            Success: false,
            Message: "商品已售完",
        }, nil
    }

    // 3. 記錄用戶已搶購（防止重複搶購）
    err = f.markUserPurchased(ctx, userID, productID)
    if err != nil {
        // 回滾庫存
        f.redisStock.IncrStock(ctx, productID, 1)
        return nil, err
    }

    // 4. 發送訂單訊息到 Kafka（異步處理）
    orderMsg := &OrderMessage{
        UserID:    userID,
        ProductID: productID,
        Quantity:  1,
        Timestamp: time.Now(),
    }

    err = f.kafka.Produce(ctx, "flash-sale-orders", orderMsg)
    if err != nil {
        // 回滾
        f.redisStock.IncrStock(ctx, productID, 1)
        f.unmarkUserPurchased(ctx, userID, productID)
        return nil, err
    }

    // 5. 立即返回（不等待訂單建立完成）
    return &PurchaseResult{
        Success: true,
        Message: "搶購成功！訂單處理中，請稍後查看訂單詳情",
    }, nil
}

// ProcessOrder Kafka 消費者：處理訂單
func (f *FlashSaleService) ProcessOrder(ctx context.Context, msg *OrderMessage) error {
    // 1. 建立訂單
    order := &Order{
        UserID:    msg.UserID,
        ProductID: msg.ProductID,
        Quantity:  msg.Quantity,
        Status:    "pending_payment",
        CreatedAt: msg.Timestamp,
    }

    err := f.db.Create(ctx, order)
    if err != nil {
        return err
    }

    // 2. 發送支付通知
    f.sendPaymentNotification(ctx, order)

    // 3. 設定訂單超時取消（15 分鐘內未支付自動取消）
    f.scheduleOrderTimeout(ctx, order.ID, 15*time.Minute)

    return nil
}
```

### 3.3 Kafka 消費者

```go
// internal/consumer/order_consumer.go
package consumer

type OrderConsumer struct {
    kafka   *KafkaConsumer
    service *FlashSaleService
}

func (c *OrderConsumer) Start(ctx context.Context) {
    // 訂閱主題
    c.kafka.Subscribe("flash-sale-orders")

    for {
        select {
        case <-ctx.Done():
            return

        case msg := <-c.kafka.Messages():
            var orderMsg OrderMessage
            json.Unmarshal(msg.Value, &orderMsg)

            // 處理訂單（可能失敗，需要重試）
            err := c.service.ProcessOrder(ctx, &orderMsg)
            if err != nil {
                // 記錄失敗，稍後重試
                log.Error("Failed to process order", "error", err)

                // 重試 3 次
                for i := 0; i < 3; i++ {
                    time.Sleep(time.Duration(i+1) * time.Second)
                    err = c.service.ProcessOrder(ctx, &orderMsg)
                    if err == nil {
                        break
                    }
                }

                if err != nil {
                    // 寫入死信隊列
                    c.kafka.ProduceToDLQ(ctx, msg)
                }
            }

            // 確認消費
            c.kafka.CommitMessage(msg)
        }
    }
}
```

---

## Act 4: 分層架構 - 多級防護

**場景**：100 萬人搶購，如何避免所有流量都打到後端？

### 4.1 對話：分層防護的概念

**Michael**：我們需要**分層攔截**，在不同層級過濾流量。

```
第 1 層：CDN（靜態資源）
↓ 過濾 80%
第 2 層：前端限流（按鈕防抖、倒數計時）
↓ 過濾 10%
第 3 層：Nginx 限流（rate limit）
↓ 過濾 5%
第 4 層：後端限流（令牌桶、漏桶）
↓ 過濾 3%
第 5 層：Redis（扣庫存）
↓ 只剩 2%
第 6 層：消息隊列（異步處理）
```

### 4.2 前端限流

```javascript
// 前端防抖
let isRequesting = false;

function purchase() {
  // 防止重複點擊
  if (isRequesting) {
    alert('請勿重複點擊');
    return;
  }

  isRequesting = true;

  fetch('/api/flash-sale/purchase', {
    method: 'POST',
    body: JSON.stringify({ product_id: 1001 })
  })
  .then(response => response.json())
  .then(data => {
    if (data.success) {
      alert('搶購成功！');
    } else {
      alert(data.message);
    }
  })
  .finally(() => {
    // 1 秒後才能再次點擊
    setTimeout(() => {
      isRequesting = false;
    }, 1000);
  });
}
```

### 4.3 Nginx 限流

```nginx
# nginx.conf

# 限流配置（每個 IP 每秒最多 10 個請求）
limit_req_zone $binary_remote_addr zone=flash_sale:10m rate=10r/s;

server {
    listen 80;

    location /api/flash-sale/ {
        # 應用限流規則
        limit_req zone=flash_sale burst=20 nodelay;

        # 反向代理到後端
        proxy_pass http://backend;
    }
}
```

### 4.4 後端限流（令牌桶）

```go
// internal/ratelimit/token_bucket.go
package ratelimit

import (
    "sync"
    "time"
    "golang.org/x/time/rate"
)

type TokenBucket struct {
    limiter *rate.Limiter
    mu      sync.Mutex
}

// NewTokenBucket 建立令牌桶
func NewTokenBucket(qps int, burst int) *TokenBucket {
    return &TokenBucket{
        limiter: rate.NewLimiter(rate.Limit(qps), burst),
    }
}

// Allow 檢查是否允許請求
func (tb *TokenBucket) Allow() bool {
    return tb.limiter.Allow()
}

// Middleware 限流中介軟體
func (tb *TokenBucket) Middleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        if !tb.Allow() {
            c.JSON(429, gin.H{
                "error": "請求過於頻繁，請稍後再試",
            })
            c.Abort()
            return
        }

        c.Next()
    }
}
```

---

## Act 5: 防止黃牛

**場景**：黃牛用腳本搶購，如何防止？

### 5.1 對話：黃牛的手段

**Emma**：聽說黃牛會用機器人搶購，我們要怎麼防範？

**David**：黃牛的手段包括：
1. **腳本自動化**：寫程式自動點擊
2. **分散式搶購**：用多台機器、多個帳號
3. **搶先搶購**：提前幾毫秒發送請求

### 5.2 防黃牛措施

#### 措施 1：驗證碼

```go
// internal/captcha/service.go
package captcha

type CaptchaService struct {
    redis *RedisClient
}

// GenerateCaptcha 生成驗證碼
func (c *CaptchaService) GenerateCaptcha(ctx context.Context, userID int64) (*Captcha, error) {
    // 生成隨機驗證碼
    code := generateRandomCode(6)

    // 生成驗證碼圖片
    image := generateCaptchaImage(code)

    // 存儲到 Redis（5 分鐘過期）
    key := fmt.Sprintf("captcha:%d", userID)
    c.redis.Set(ctx, key, code, 5*time.Minute)

    return &Captcha{
        Image: image,
    }, nil
}

// VerifyCaptcha 驗證驗證碼
func (c *CaptchaService) VerifyCaptcha(ctx context.Context, userID int64, code string) (bool, error) {
    key := fmt.Sprintf("captcha:%d", userID)

    stored, err := c.redis.Get(ctx, key).Result()
    if err == redis.Nil {
        return false, nil  // 驗證碼已過期
    }
    if err != nil {
        return false, err
    }

    // 比對驗證碼（不區分大小寫）
    match := strings.EqualFold(stored, code)

    if match {
        // 驗證成功後刪除
        c.redis.Del(ctx, key)
    }

    return match, nil
}
```

#### 措施 2：IP 限流

```go
// CheckIPRateLimit 檢查 IP 限流
func (f *FlashSaleService) CheckIPRateLimit(ctx context.Context, ip string) (bool, error) {
    key := fmt.Sprintf("ratelimit:ip:%s", ip)

    // 使用 Redis INCR（原子性）
    count, err := f.redis.Incr(ctx, key).Result()
    if err != nil {
        return false, err
    }

    // 設定過期時間（1 分鐘）
    if count == 1 {
        f.redis.Expire(ctx, key, 1*time.Minute)
    }

    // 限制：每分鐘最多 10 次請求
    if count > 10 {
        return false, nil
    }

    return true, nil
}
```

#### 措施 3：用戶限購

```go
// checkUserPurchased 檢查用戶是否已搶購
func (f *FlashSaleService) checkUserPurchased(ctx context.Context, userID, productID int64) (bool, error) {
    key := fmt.Sprintf("flash_sale:%d:users", productID)

    // 使用 Redis Set 記錄已搶購的用戶
    exists, err := f.redis.SIsMember(ctx, key, userID).Result()
    if err != nil {
        return false, err
    }

    return exists, nil
}

// markUserPurchased 標記用戶已搶購
func (f *FlashSaleService) markUserPurchased(ctx context.Context, userID, productID int64) error {
    key := fmt.Sprintf("flash_sale:%d:users", productID)

    return f.redis.SAdd(ctx, key, userID).Err()
}
```

#### 措施 4：請求簽名

```go
// GenerateRequestToken 生成請求 Token（防重放攻擊）
func (f *FlashSaleService) GenerateRequestToken(ctx context.Context, userID int64) (string, error) {
    // Token = MD5(userID + timestamp + secret)
    timestamp := time.Now().Unix()
    token := fmt.Sprintf("%d:%d:%s", userID, timestamp, "secret_key")
    hash := md5.Sum([]byte(token))
    tokenStr := hex.EncodeToString(hash[:])

    // 存儲到 Redis（5 分鐘過期）
    key := fmt.Sprintf("request_token:%d", userID)
    f.redis.Set(ctx, key, tokenStr, 5*time.Minute)

    return tokenStr, nil
}

// VerifyRequestToken 驗證請求 Token
func (f *FlashSaleService) VerifyRequestToken(ctx context.Context, userID int64, token string) (bool, error) {
    key := fmt.Sprintf("request_token:%d", userID)

    stored, err := f.redis.Get(ctx, key).Result()
    if err == redis.Nil {
        return false, nil
    }
    if err != nil {
        return false, err
    }

    // 驗證後刪除（防止重複使用）
    if stored == token {
        f.redis.Del(ctx, key)
        return true, nil
    }

    return false, nil
}
```

---

## Act 6: 快取策略

**場景**：商品詳情頁被百萬人同時訪問，如何優化？

### 6.1 快取預熱

```go
// PreWarmCache 快取預熱（秒殺開始前）
func (f *FlashSaleService) PreWarmCache(ctx context.Context, productID int64) error {
    // 1. 載入商品資訊到 Redis
    product, err := f.db.GetProduct(ctx, productID)
    if err != nil {
        return err
    }

    key := fmt.Sprintf("product:%d", productID)
    data, _ := json.Marshal(product)
    f.redis.Set(ctx, key, data, 30*time.Minute)

    // 2. 預載庫存
    f.redisStock.PreloadStock(ctx, productID, product.Stock)

    return nil
}
```

### 6.2 防止快取穿透

```go
// GetProduct 取得商品資訊（防快取穿透）
func (f *FlashSaleService) GetProduct(ctx context.Context, productID int64) (*Product, error) {
    key := fmt.Sprintf("product:%d", productID)

    // 1. 從快取取得
    data, err := f.redis.Get(ctx, key).Bytes()
    if err == nil {
        var product Product
        json.Unmarshal(data, &product)
        return &product, nil
    }

    // 2. 快取未命中，使用分散式鎖防止快取擊穿
    lockKey := fmt.Sprintf("lock:product:%d", productID)
    locked, err := f.redis.SetNX(ctx, lockKey, 1, 10*time.Second).Result()

    if !locked {
        // 其他請求正在載入，等待後重試
        time.Sleep(50 * time.Millisecond)
        return f.GetProduct(ctx, productID)
    }

    defer f.redis.Del(ctx, lockKey)

    // 3. 從資料庫載入
    product, err := f.db.GetProduct(ctx, productID)
    if err != nil {
        // 商品不存在，快取空值防止穿透
        if err == sql.ErrNoRows {
            f.redis.Set(ctx, key, "null", 5*time.Minute)
        }
        return nil, err
    }

    // 4. 寫入快取
    data, _ = json.Marshal(product)
    f.redis.Set(ctx, key, data, 30*time.Minute)

    return product, nil
}
```

### 6.3 防止快取雪崩

```go
// SetCacheWithRandomExpire 設定快取（隨機過期時間）
func (f *FlashSaleService) SetCacheWithRandomExpire(ctx context.Context, key string, value interface{}, baseTTL time.Duration) error {
    // 加入隨機時間（防止同時過期）
    randomTTL := baseTTL + time.Duration(rand.Intn(300))*time.Second

    data, _ := json.Marshal(value)
    return f.redis.Set(ctx, key, data, randomTTL).Err()
}
```

---

## Act 7: 數據一致性與對賬

**場景**：Redis 庫存扣減了，但訂單建立失敗，如何保證一致性？

### 7.1 對話：最終一致性

**Emma**：如果 Redis 扣了庫存，但 Kafka 訊息丟失導致訂單沒建立，怎麼辦？

**Michael**：這就需要**對賬機制**（Reconciliation）。

### 7.2 對賬系統

```go
// internal/reconciliation/service.go
package reconciliation

type ReconciliationService struct {
    redis *RedisClient
    db    *PostgreSQL
}

// ReconcileStock 對賬庫存（定時任務，每小時執行）
func (r *ReconciliationService) ReconcileStock(ctx context.Context, productID int64) error {
    // 1. 取得 Redis 庫存
    redisStock, err := r.redis.Get(ctx, fmt.Sprintf("product:%d:stock", productID)).Int()
    if err != nil {
        return err
    }

    // 2. 計算實際售出數量
    var soldCount int
    err = r.db.QueryRowContext(ctx, `
        SELECT COUNT(*)
        FROM orders
        WHERE product_id = ? AND status != 'cancelled'
    `, productID).Scan(&soldCount)
    if err != nil {
        return err
    }

    // 3. 計算預期庫存
    var originalStock int
    err = r.db.QueryRowContext(ctx, `
        SELECT stock FROM products WHERE id = ?
    `, productID).Scan(&originalStock)
    if err != nil {
        return err
    }

    expectedStock := originalStock - soldCount

    // 4. 比對差異
    diff := redisStock - expectedStock

    if diff != 0 {
        log.Warn("Stock mismatch detected",
            "product_id", productID,
            "redis_stock", redisStock,
            "expected_stock", expectedStock,
            "diff", diff,
        )

        // 記錄差異
        r.recordDiscrepancy(ctx, productID, diff)

        // 修正 Redis 庫存
        r.redis.Set(ctx, fmt.Sprintf("product:%d:stock", productID), expectedStock, 0)
    }

    return nil
}
```

---

## 總結

### 核心技術要點

1. **Redis 扣庫存**
   - Lua 腳本保證原子性
   - 單線程模型，無鎖競爭
   - QPS 可達 100,000+

2. **消息隊列削峰**
   - 前端：100,000 QPS
   - 後端：1,000 QPS
   - 異步處理，提升用戶體驗

3. **分層防護**
   - CDN → 前端限流 → Nginx → 後端限流 → Redis
   - 層層過濾，減少後端壓力

4. **防黃牛**
   - 驗證碼
   - IP 限流
   - 用戶限購
   - 請求簽名

5. **快取策略**
   - 快取預熱
   - 防穿透（空值快取）
   - 防雪崩（隨機過期）
   - 防擊穿（分散式鎖）

6. **數據一致性**
   - 最終一致性
   - 定時對賬
   - 訂單超時取消

### 延伸思考

**Emma**：如果要做「盲盒」秒殺（隨機抽取商品），要怎麼設計？

**Michael**：需要：
- **隨機算法**：公平抽獎算法
- **庫存池**：多種商品的庫存管理
- **中獎記錄**：防止重複中獎
- **公示機制**：透明化中獎名單

這是另一個有趣的挑戰！

---

**下一章預告**：Payment System - 支付系統（冪等性、雙寫一致性、對賬、分散式事務）
