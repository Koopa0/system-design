# Hotel Reservation（酒店預訂系統）

> **專案類型**：預訂平台
> **技術難度**：★★★☆☆
> **核心技術**：分散式鎖、庫存管理、超售防範、動態定價

## 目錄

- [系統概述](#系統概述)
- [技術架構](#技術架構)
- [資料庫設計](#資料庫設計)
- [核心功能實作](#核心功能實作)
- [API 文件](#api-文件)
- [效能優化](#效能優化)
- [監控與告警](#監控與告警)
- [部署架構](#部署架構)
- [成本估算](#成本估算)

---

## 系統概述

### 功能需求

| 功能模組 | 描述 | 優先級 |
|---------|------|--------|
| 房間搜尋 | 按日期、地點、房型搜尋 | P0 |
| 預訂管理 | 建立、取消、修改預訂 | P0 |
| 庫存管理 | 房間庫存、超售防範 | P0 |
| 支付處理 | 線上支付、退款 | P0 |
| 訂單管理 | 訂單狀態、歷史記錄 | P0 |
| 動態定價 | 根據需求調整價格 | P1 |
| 評價系統 | 用戶評價、星級評分 | P1 |
| 會員系統 | 積分、等級、專屬優惠 | P2 |

### 非功能需求

| 指標 | 目標值 | 說明 |
|-----|--------|------|
| 可用性 | 99.9% | 年停機時間 < 8.76 小時 |
| 預訂成功率 | > 99.5% | 避免超售 |
| 響應延遲 | P95 < 500ms | 搜尋和預訂 |
| 並發處理 | 10,000 QPS | 峰值訂單量 |
| 資料一致性 | 強一致 | 庫存不允許錯誤 |

---

## 技術架構

### 系統架構圖

```
┌──────────────────────────────────────────────────┐
│                   Client Layer                   │
│         (Web / Mobile App / Third Party)         │
└───────────────┬──────────────────────────────────┘
                │ HTTPS
                ↓
┌──────────────────────────────────────────────────┐
│            API Gateway (Nginx)                   │
│  - Rate Limiting (1000 req/min per user)        │
│  - SSL Termination                               │
│  - Load Balancing                                │
└───────────────┬──────────────────────────────────┘
                │
       ┌────────┴────────┐
       │                 │
       ↓                 ↓
┌─────────────┐   ┌──────────────┐
│  Search     │   │  Booking     │
│  Service    │   │  Service     │
└──────┬──────┘   └──────┬───────┘
       │                 │
       ↓                 ↓
┌──────────────────────────────────┐
│     Inventory Service            │
│  - Room Availability Check       │
│  - Distributed Lock (Redis)      │
│  - Optimistic/Pessimistic Lock   │
└──────┬──────────┬────────────────┘
       │          │
       ↓          ↓
┌──────────┐  ┌────────┐
│ MySQL    │  │ Redis  │
│(Sharding)│  │(Cache) │
└──────────┘  └────────┘
       │
       ↓
┌──────────────────────┐
│   Kafka (Events)     │
│ - Booking Created    │
│ - Booking Cancelled  │
│ - Payment Completed  │
└──────────────────────┘
       │
       ↓
┌──────────────────────────┐
│  Background Workers      │
│ - Expiration Worker      │
│ - Pricing Worker         │
│ - Notification Worker    │
└──────────────────────────┘
```

### 技術棧

| 層級 | 技術選型 | 說明 |
|-----|---------|------|
| **API 層** | Go + Gin | 高效能 HTTP 服務 |
| **快取層** | Redis Cluster | 分散式快取、分散式鎖 |
| **資料庫** | MySQL 8.0 (分片) | 主資料儲存 |
| **搜尋引擎** | Elasticsearch | 酒店、房間搜尋 |
| **訊息佇列** | Kafka | 事件驅動架構 |
| **定時任務** | Cron + Workers | 過期處理、定價更新 |
| **監控** | Prometheus + Grafana | 指標監控 |
| **日誌** | ELK Stack | 集中式日誌 |

---

## 資料庫設計

### 1. Hotels（酒店表）

```sql
CREATE TABLE hotels (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,

    -- 地理位置
    address VARCHAR(500) NOT NULL,
    city VARCHAR(100) NOT NULL,
    country VARCHAR(100) NOT NULL,
    latitude DECIMAL(10, 8),
    longitude DECIMAL(11, 8),

    -- 基本資訊
    star_rating DECIMAL(2, 1),    -- 星級：1.0 - 5.0
    description TEXT,
    amenities JSON,               -- 設施：["WiFi", "游泳池", "健身房"]

    -- 狀態
    status VARCHAR(20) NOT NULL DEFAULT 'active',  -- active, inactive

    -- 時間戳
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    -- 索引
    INDEX idx_city (city),
    INDEX idx_location (latitude, longitude),
    INDEX idx_star_rating (star_rating)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 2. Room Types（房型表）

```sql
CREATE TABLE room_types (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    hotel_id BIGINT NOT NULL,

    -- 房型資訊
    name VARCHAR(255) NOT NULL,          -- "標準雙人房"
    description TEXT,
    max_occupancy INT NOT NULL,          -- 最大入住人數
    bed_type VARCHAR(50),                -- "雙人床", "兩張單人床"
    size_sqm INT,                        -- 房間大小（平方米）

    -- 庫存
    total_rooms INT NOT NULL,            -- 總房間數

    -- 定價
    base_price INT NOT NULL,             -- 基礎價格（分）
    currency VARCHAR(3) NOT NULL DEFAULT 'TWD',

    -- 設施
    amenities JSON,                      -- ["陽台", "浴缸", "海景"]

    -- 取消政策
    cancellation_policy_id BIGINT,

    -- 圖片
    images JSON,                         -- ["url1", "url2", ...]

    -- 時間戳
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    -- 索引
    INDEX idx_hotel_id (hotel_id),
    FOREIGN KEY (hotel_id) REFERENCES hotels(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 3. Room Inventory（房間庫存表）

```sql
CREATE TABLE room_inventory (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    room_type_id BIGINT NOT NULL,
    date DATE NOT NULL,

    -- 庫存數量
    total_rooms INT NOT NULL,            -- 總房間數
    booked_rooms INT NOT NULL DEFAULT 0, -- 已預訂房間數
    available_rooms INT NOT NULL,        -- 可用房間數

    -- 動態定價
    price INT NOT NULL,                  -- 當日價格（分）

    -- 樂觀鎖
    version INT NOT NULL DEFAULT 0,

    -- 時間戳
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    -- 唯一索引
    UNIQUE KEY uk_room_date (room_type_id, date),

    -- 索引
    INDEX idx_date (date),
    INDEX idx_available (room_type_id, date, available_rooms)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 按月分表
-- 表名格式：room_inventory_YYYYMM
-- 例如：room_inventory_202505
```

### 4. Reservations（預訂表）

```sql
CREATE TABLE reservations (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,

    -- 用戶資訊
    user_id BIGINT NOT NULL,
    guest_name VARCHAR(255) NOT NULL,
    guest_email VARCHAR(255) NOT NULL,
    guest_phone VARCHAR(50) NOT NULL,

    -- 酒店與房型
    hotel_id BIGINT NOT NULL,
    room_type_id BIGINT NOT NULL,

    -- 入住資訊
    check_in DATE NOT NULL,
    check_out DATE NOT NULL,
    nights INT NOT NULL,                 -- 入住天數
    guests INT NOT NULL,                 -- 入住人數

    -- 價格
    total_price INT NOT NULL,            -- 總價（分）
    currency VARCHAR(3) NOT NULL DEFAULT 'TWD',

    -- 狀態
    status VARCHAR(20) NOT NULL,         -- pending, confirmed, checked_in, completed, cancelled, expired, no_show

    -- 支付
    payment_id BIGINT,                   -- 關聯支付記錄
    payment_status VARCHAR(20),          -- pending, paid, refunded

    -- 取消相關
    cancelled_at DATETIME,
    cancellation_reason TEXT,
    refund_amount INT DEFAULT 0,

    -- 特殊要求
    special_requests TEXT,

    -- 時間戳
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    confirmed_at DATETIME,
    checked_in_at DATETIME,
    completed_at DATETIME,

    -- 索引
    INDEX idx_user_id (user_id),
    INDEX idx_hotel_id (hotel_id),
    INDEX idx_check_in (check_in),
    INDEX idx_status (status),
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 按時間分表（月表）
-- 表名格式：reservations_YYYYMM
```

### 5. Cancellation Policies（取消政策表）

```sql
CREATE TABLE cancellation_policies (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,          -- "標準政策", "靈活政策", "不可取消"
    description TEXT,

    -- 規則（JSON 格式）
    -- 例如：[{"days_before": 7, "refund_percent": 1.0}, {"days_before": 3, "refund_percent": 0.5}]
    rules JSON NOT NULL,

    -- 時間戳
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 預設政策資料
INSERT INTO cancellation_policies (name, description, rules) VALUES
('標準政策', '7天前全額退款，3-7天前50%退款',
 '[{"days_before": 7, "refund_percent": 1.0}, {"days_before": 3, "refund_percent": 0.5}, {"days_before": 0, "refund_percent": 0.0}]'),

('靈活政策', '入住前1天可全額退款',
 '[{"days_before": 1, "refund_percent": 1.0}, {"days_before": 0, "refund_percent": 0.5}]'),

('不可取消', '任何時候取消均不退款',
 '[{"days_before": 0, "refund_percent": 0.0}]');
```

### 6. Reviews（評價表）

```sql
CREATE TABLE reviews (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,

    -- 關聯
    reservation_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    hotel_id BIGINT NOT NULL,
    room_type_id BIGINT NOT NULL,

    -- 評分（1-5）
    overall_rating DECIMAL(2, 1) NOT NULL,
    cleanliness_rating DECIMAL(2, 1),
    service_rating DECIMAL(2, 1),
    location_rating DECIMAL(2, 1),
    value_rating DECIMAL(2, 1),

    -- 評價內容
    title VARCHAR(255),
    content TEXT,

    -- 回覆
    hotel_response TEXT,
    responded_at DATETIME,

    -- 狀態
    status VARCHAR(20) NOT NULL DEFAULT 'active',  -- active, hidden, deleted

    -- 時間戳
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    -- 索引
    INDEX idx_hotel_id (hotel_id),
    INDEX idx_user_id (user_id),
    INDEX idx_reservation_id (reservation_id),
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

---

## 核心功能實作

### 1. 分散式鎖實作

```go
package lock

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/go-redis/redis/v8"
    "github.com/google/uuid"
)

// DistributedLock Redis 分散式鎖
type DistributedLock struct {
    client *redis.Client
    key    string
    value  string
    ttl    time.Duration
}

// NewDistributedLock 建立分散式鎖
func NewDistributedLock(client *redis.Client, key string, ttl time.Duration) *DistributedLock {
    return &DistributedLock{
        client: client,
        key:    key,
        value:  uuid.New().String(), // 唯一值，防止誤釋放
        ttl:    ttl,
    }
}

// Lock 獲取鎖
func (l *DistributedLock) Lock(ctx context.Context) (bool, error) {
    // SET key value NX EX ttl
    // NX: 只在鍵不存在時設定
    // EX: 設定過期時間（秒）
    success, err := l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
    if err != nil {
        return false, fmt.Errorf("獲取鎖失敗: %w", err)
    }

    return success, nil
}

// Unlock 釋放鎖（使用 Lua 腳本確保原子性）
func (l *DistributedLock) Unlock(ctx context.Context) error {
    // Lua 腳本：只有持鎖者才能釋放
    script := `
        if redis.call("get", KEYS[1]) == ARGV[1] then
            return redis.call("del", KEYS[1])
        else
            return 0
        end
    `

    result, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
    if err != nil {
        return fmt.Errorf("釋放鎖失敗: %w", err)
    }

    if result.(int64) == 0 {
        return errors.New("鎖已被其他人持有或已過期")
    }

    return nil
}

// TryLock 嘗試獲取鎖（帶重試）
func (l *DistributedLock) TryLock(ctx context.Context, retries int, retryDelay time.Duration) (bool, error) {
    for i := 0; i < retries; i++ {
        success, err := l.Lock(ctx)
        if err != nil {
            return false, err
        }

        if success {
            return true, nil
        }

        // 等待後重試
        select {
        case <-ctx.Done():
            return false, ctx.Err()
        case <-time.After(retryDelay):
            continue
        }
    }

    return false, nil
}

// Extend 延長鎖的過期時間
func (l *DistributedLock) Extend(ctx context.Context, additionalTTL time.Duration) error {
    script := `
        if redis.call("get", KEYS[1]) == ARGV[1] then
            return redis.call("expire", KEYS[1], ARGV[2])
        else
            return 0
        end
    `

    result, err := l.client.Eval(ctx, script, []string{l.key}, l.value, int(additionalTTL.Seconds())).Result()
    if err != nil {
        return fmt.Errorf("延長鎖失敗: %w", err)
    }

    if result.(int64) == 0 {
        return errors.New("鎖已失效")
    }

    return nil
}
```

### 2. 預訂服務（使用分散式鎖）

```go
package service

import (
    "context"
    "errors"
    "fmt"
    "time"
)

// ReservationService 預訂服務
type ReservationService struct {
    reservationRepo ReservationRepository
    inventoryRepo   InventoryRepository
    redisClient     *redis.Client
    paymentService  *PaymentService
}

// CreateReservationRequest 建立預訂請求
type CreateReservationRequest struct {
    UserID      int64
    RoomTypeID  int64
    CheckIn     time.Time
    CheckOut    time.Time
    Guests      int
    GuestName   string
    GuestEmail  string
    GuestPhone  string
    SpecialRequests string
}

// CreateReservation 建立預訂（使用分散式鎖）
func (s *ReservationService) CreateReservation(ctx context.Context, req *CreateReservationRequest) (*Reservation, error) {
    // 1. 驗證日期
    if req.CheckIn.Before(time.Now()) {
        return nil, errors.New("入住日期不能早於今天")
    }

    if req.CheckOut.Before(req.CheckIn) || req.CheckOut.Equal(req.CheckIn) {
        return nil, errors.New("退房日期必須晚於入住日期")
    }

    nights := int(req.CheckOut.Sub(req.CheckIn).Hours() / 24)

    // 2. 建立分散式鎖
    lockKey := fmt.Sprintf("reservation:lock:%d:%s:%s",
        req.RoomTypeID,
        req.CheckIn.Format("2006-01-02"),
        req.CheckOut.Format("2006-01-02"),
    )

    lock := NewDistributedLock(s.redisClient, lockKey, 10*time.Second)

    // 3. 嘗試獲取鎖
    acquired, err := lock.TryLock(ctx, 5, 100*time.Millisecond)
    if err != nil {
        return nil, fmt.Errorf("獲取鎖失敗: %w", err)
    }

    if !acquired {
        return nil, errors.New("系統繁忙，請稍後再試")
    }

    defer lock.Unlock(ctx)

    // 4. 檢查房間可用性
    available, err := s.checkAvailability(ctx, req.RoomTypeID, req.CheckIn, req.CheckOut)
    if err != nil {
        return nil, fmt.Errorf("檢查可用性失敗: %w", err)
    }

    if !available {
        return nil, errors.New("該日期房間已滿")
    }

    // 5. 計算總價
    totalPrice, err := s.calculateTotalPrice(ctx, req.RoomTypeID, req.CheckIn, req.CheckOut)
    if err != nil {
        return nil, fmt.Errorf("計算價格失敗: %w", err)
    }

    // 6. 建立預訂記錄
    reservation := &Reservation{
        UserID:          req.UserID,
        RoomTypeID:      req.RoomTypeID,
        CheckIn:         req.CheckIn,
        CheckOut:        req.CheckOut,
        Nights:          nights,
        Guests:          req.Guests,
        GuestName:       req.GuestName,
        GuestEmail:      req.GuestEmail,
        GuestPhone:      req.GuestPhone,
        TotalPrice:      totalPrice,
        Status:          StatusPending,
        SpecialRequests: req.SpecialRequests,
        CreatedAt:       time.Now(),
    }

    if err := s.reservationRepo.Create(ctx, reservation); err != nil {
        return nil, fmt.Errorf("建立預訂失敗: %w", err)
    }

    // 7. 減少庫存
    if err := s.decrementInventory(ctx, req.RoomTypeID, req.CheckIn, req.CheckOut); err != nil {
        // 回滾預訂
        s.reservationRepo.Delete(ctx, reservation.ID)
        return nil, fmt.Errorf("減少庫存失敗: %w", err)
    }

    // 8. 發送事件
    s.publishReservationCreatedEvent(reservation)

    return reservation, nil
}

// checkAvailability 檢查房間可用性
func (s *ReservationService) checkAvailability(ctx context.Context, roomTypeID int64, checkIn, checkOut time.Time) (bool, error) {
    currentDate := checkIn

    for currentDate.Before(checkOut) {
        inventory, err := s.inventoryRepo.GetByDate(ctx, roomTypeID, currentDate)
        if err != nil {
            return false, err
        }

        if inventory.AvailableRooms <= 0 {
            return false, nil
        }

        currentDate = currentDate.AddDate(0, 0, 1)
    }

    return true, nil
}

// decrementInventory 減少庫存
func (s *ReservationService) decrementInventory(ctx context.Context, roomTypeID int64, checkIn, checkOut time.Time) error {
    currentDate := checkIn

    for currentDate.Before(checkOut) {
        // 使用樂觀鎖更新
        maxRetries := 3
        success := false

        for i := 0; i < maxRetries; i++ {
            inventory, err := s.inventoryRepo.GetByDate(ctx, roomTypeID, currentDate)
            if err != nil {
                return err
            }

            // 使用版本號更新
            affected, err := s.inventoryRepo.DecrementWithVersion(ctx, inventory.ID, inventory.Version)
            if err != nil {
                return err
            }

            if affected > 0 {
                success = true
                break
            }

            // 版本號衝突，重試
            time.Sleep(10 * time.Millisecond)
        }

        if !success {
            // 回滾之前的減少
            s.rollbackInventory(ctx, roomTypeID, checkIn, currentDate)
            return errors.New("更新庫存失敗")
        }

        currentDate = currentDate.AddDate(0, 0, 1)
    }

    return nil
}

// rollbackInventory 回滾庫存
func (s *ReservationService) rollbackInventory(ctx context.Context, roomTypeID int64, start, end time.Time) {
    currentDate := start

    for currentDate.Before(end) {
        s.inventoryRepo.Increment(ctx, roomTypeID, currentDate)
        currentDate = currentDate.AddDate(0, 0, 1)
    }
}
```

### 3. 庫存倉庫（樂觀鎖實作）

```go
package repository

import (
    "context"
    "database/sql"
    "time"
)

// InventoryRepository 庫存倉庫
type InventoryRepository struct {
    db *sql.DB
}

// RoomInventory 房間庫存
type RoomInventory struct {
    ID             int64
    RoomTypeID     int64
    Date           time.Time
    TotalRooms     int
    BookedRooms    int
    AvailableRooms int
    Price          int64
    Version        int
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

// GetByDate 獲取指定日期的庫存
func (r *InventoryRepository) GetByDate(ctx context.Context, roomTypeID int64, date time.Time) (*RoomInventory, error) {
    var inventory RoomInventory

    err := r.db.QueryRowContext(ctx, `
        SELECT id, room_type_id, date, total_rooms, booked_rooms, available_rooms, price, version, created_at, updated_at
        FROM room_inventory
        WHERE room_type_id = ? AND date = ?
    `, roomTypeID, date).Scan(
        &inventory.ID,
        &inventory.RoomTypeID,
        &inventory.Date,
        &inventory.TotalRooms,
        &inventory.BookedRooms,
        &inventory.AvailableRooms,
        &inventory.Price,
        &inventory.Version,
        &inventory.CreatedAt,
        &inventory.UpdatedAt,
    )

    if err != nil {
        return nil, err
    }

    return &inventory, nil
}

// DecrementWithVersion 使用樂觀鎖減少庫存
func (r *InventoryRepository) DecrementWithVersion(ctx context.Context, id int64, expectedVersion int) (int64, error) {
    result, err := r.db.ExecContext(ctx, `
        UPDATE room_inventory
        SET booked_rooms = booked_rooms + 1,
            available_rooms = available_rooms - 1,
            version = version + 1,
            updated_at = NOW()
        WHERE id = ?
          AND version = ?
          AND available_rooms > 0
    `, id, expectedVersion)

    if err != nil {
        return 0, err
    }

    rowsAffected, err := result.RowsAffected()
    if err != nil {
        return 0, err
    }

    return rowsAffected, nil
}

// Increment 增加庫存（取消預訂時）
func (r *InventoryRepository) Increment(ctx context.Context, roomTypeID int64, date time.Time) error {
    _, err := r.db.ExecContext(ctx, `
        UPDATE room_inventory
        SET booked_rooms = booked_rooms - 1,
            available_rooms = available_rooms + 1,
            version = version + 1,
            updated_at = NOW()
        WHERE room_type_id = ? AND date = ?
    `, roomTypeID, date)

    return err
}

// InitializeInventory 初始化庫存（批次）
func (r *InventoryRepository) InitializeInventory(ctx context.Context, roomTypeID int64, startDate, endDate time.Time, totalRooms int, basePrice int64) error {
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO room_inventory (room_type_id, date, total_rooms, booked_rooms, available_rooms, price)
        VALUES (?, ?, ?, 0, ?, ?)
        ON DUPLICATE KEY UPDATE
            total_rooms = VALUES(total_rooms),
            available_rooms = available_rooms + (VALUES(total_rooms) - total_rooms)
    `)
    if err != nil {
        return err
    }
    defer stmt.Close()

    currentDate := startDate
    for currentDate.Before(endDate) || currentDate.Equal(endDate) {
        _, err := stmt.ExecContext(ctx, roomTypeID, currentDate, totalRooms, totalRooms, basePrice)
        if err != nil {
            return err
        }

        currentDate = currentDate.AddDate(0, 0, 1)
    }

    return tx.Commit()
}
```

### 4. 定時任務：自動過期預訂

```go
package worker

import (
    "context"
    "time"
)

// ExpirationWorker 過期處理工作者
type ExpirationWorker struct {
    reservationRepo  ReservationRepository
    inventoryService *InventoryService
}

// Run 運行過期檢查
func (w *ExpirationWorker) Run(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.expirePendingReservations(ctx)
        }
    }
}

// expirePendingReservations 過期待支付預訂
func (w *ExpirationWorker) expirePendingReservations(ctx context.Context) {
    // 查詢超過 15 分鐘未支付的預訂
    cutoffTime := time.Now().Add(-15 * time.Minute)

    reservations, err := w.reservationRepo.FindPendingBefore(ctx, cutoffTime)
    if err != nil {
        log.Error("查詢待過期預訂失敗", "error", err)
        return
    }

    log.Info("找到待過期預訂", "count", len(reservations))

    for _, reservation := range reservations {
        if err := w.expireReservation(ctx, reservation); err != nil {
            log.Error("過期預訂失敗",
                "reservation_id", reservation.ID,
                "error", err,
            )
        }
    }
}

// expireReservation 過期單個預訂
func (w *ExpirationWorker) expireReservation(ctx context.Context, reservation *Reservation) error {
    // 1. 更新狀態為已過期
    reservation.Status = StatusExpired
    reservation.CancelledAt = time.Now()

    if err := w.reservationRepo.Update(ctx, reservation); err != nil {
        return err
    }

    // 2. 釋放庫存
    if err := w.inventoryService.ReleaseInventory(ctx, reservation); err != nil {
        log.Error("釋放庫存失敗",
            "reservation_id", reservation.ID,
            "error", err,
        )
        // 不返回錯誤，繼續處理
    }

    log.Info("預訂已過期",
        "reservation_id", reservation.ID,
        "user_id", reservation.UserID,
    )

    return nil
}
```

---

## API 文件

### 1. 搜尋酒店

**端點**: `GET /api/v1/hotels/search`

**請求參數**:

```
city=台北&check_in=2025-06-01&check_out=2025-06-03&guests=2
```

**回應**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "hotels": [
      {
        "hotel_id": 123,
        "name": "台北君悅酒店",
        "star_rating": 5.0,
        "address": "台北市信義區松壽路2號",
        "latitude": 25.0375,
        "longitude": 121.5647,
        "min_price": 550000,
        "available_rooms": 15
      }
    ],
    "total": 45,
    "page": 1,
    "per_page": 20
  }
}
```

### 2. 查詢房型可用性

**端點**: `GET /api/v1/hotels/:hotel_id/availability`

**請求參數**:

```
check_in=2025-06-01&check_out=2025-06-03
```

**回應**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "room_types": [
      {
        "room_type_id": 456,
        "name": "標準雙人房",
        "max_occupancy": 2,
        "base_price": 350000,
        "available_rooms": 5,
        "total_price": 700000,
        "images": ["url1", "url2"]
      },
      {
        "room_type_id": 457,
        "name": "豪華套房",
        "max_occupancy": 4,
        "base_price": 800000,
        "available_rooms": 2,
        "total_price": 1600000,
        "images": ["url1", "url2"]
      }
    ]
  }
}
```

### 3. 建立預訂

**端點**: `POST /api/v1/reservations`

**請求**:

```json
{
  "room_type_id": 456,
  "check_in": "2025-06-01",
  "check_out": "2025-06-03",
  "guests": 2,
  "guest_name": "王小明",
  "guest_email": "ming@example.com",
  "guest_phone": "+886912345678",
  "special_requests": "需要高樓層房間"
}
```

**回應**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "reservation_id": 789012,
    "status": "pending",
    "total_price": 700000,
    "payment_deadline": "2025-05-18T10:45:00Z",
    "created_at": "2025-05-18T10:30:00Z"
  }
}
```

### 4. 取消預訂

**端點**: `POST /api/v1/reservations/:id/cancel`

**請求**:

```json
{
  "reason": "行程變更"
}
```

**回應**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "reservation_id": 789012,
    "status": "cancelled",
    "refund_amount": 700000,
    "refund_percent": 1.0,
    "cancelled_at": "2025-05-19T14:30:00Z"
  }
}
```

---

## 效能優化

### 1. 資料庫優化

#### 按日期範圍查詢優化

```sql
-- 優化前：逐日查詢
SELECT * FROM room_inventory
WHERE room_type_id = 123 AND date = '2025-06-01';

SELECT * FROM room_inventory
WHERE room_type_id = 123 AND date = '2025-06-02';
-- ... N 次查詢

-- 優化後：一次查詢
SELECT * FROM room_inventory
WHERE room_type_id = 123
  AND date BETWEEN '2025-06-01' AND '2025-06-03'
ORDER BY date;
```

#### 庫存檢查優化

```sql
-- 優化前：COUNT(*)
SELECT COUNT(*) FROM room_inventory
WHERE room_type_id = 123
  AND date BETWEEN '2025-06-01' AND '2025-06-03'
  AND available_rooms <= 0;

-- 優化後：MIN()
SELECT MIN(available_rooms) FROM room_inventory
WHERE room_type_id = 123
  AND date BETWEEN '2025-06-01' AND '2025-06-03';

-- 如果 MIN() > 0，表示所有日期都有房
```

### 2. Redis 快取

```go
// CachedInventoryService 帶快取的庫存服務
type CachedInventoryService struct {
    inventoryRepo InventoryRepository
    redisClient   *redis.Client
    cacheTTL      time.Duration
}

// CheckAvailability 檢查可用性（優先從快取）
func (s *CachedInventoryService) CheckAvailability(ctx context.Context, roomTypeID int64, checkIn, checkOut time.Time) (bool, error) {
    // 1. 生成快取鍵
    cacheKey := fmt.Sprintf("availability:%d:%s:%s",
        roomTypeID,
        checkIn.Format("2006-01-02"),
        checkOut.Format("2006-01-02"),
    )

    // 2. 查詢快取
    cached, err := s.redisClient.Get(ctx, cacheKey).Result()
    if err == nil {
        // 快取命中
        return cached == "1", nil
    }

    // 3. 快取未命中，查詢資料庫
    available, err := s.checkAvailabilityFromDB(ctx, roomTypeID, checkIn, checkOut)
    if err != nil {
        return false, err
    }

    // 4. 寫入快取（TTL: 30 秒）
    value := "0"
    if available {
        value = "1"
    }

    s.redisClient.Set(ctx, cacheKey, value, 30*time.Second)

    return available, nil
}

// InvalidateCache 使快取失效（預訂/取消時）
func (s *CachedInventoryService) InvalidateCache(ctx context.Context, roomTypeID int64, checkIn, checkOut time.Time) {
    currentDate := checkIn

    for currentDate.Before(checkOut) || currentDate.Equal(checkOut) {
        // 刪除該日期的所有快取
        pattern := fmt.Sprintf("availability:%d:*:%s*", roomTypeID, currentDate.Format("2006-01-02"))

        keys, _ := s.redisClient.Keys(ctx, pattern).Result()
        if len(keys) > 0 {
            s.redisClient.Del(ctx, keys...)
        }

        currentDate = currentDate.AddDate(0, 0, 1)
    }
}
```

### 3. 批次操作

```go
// BatchCheckAvailability 批次檢查多個房型的可用性
func (s *InventoryService) BatchCheckAvailability(ctx context.Context, roomTypeIDs []int64, checkIn, checkOut time.Time) (map[int64]bool, error) {
    result := make(map[int64]bool)

    // 批次查詢
    query := `
        SELECT room_type_id, MIN(available_rooms) as min_available
        FROM room_inventory
        WHERE room_type_id IN (?)
          AND date BETWEEN ? AND ?
        GROUP BY room_type_id
    `

    rows, err := s.db.QueryContext(ctx, query, roomTypeIDs, checkIn, checkOut.AddDate(0, 0, -1))
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    for rows.Next() {
        var roomTypeID int64
        var minAvailable int

        if err := rows.Scan(&roomTypeID, &minAvailable); err != nil {
            return nil, err
        }

        result[roomTypeID] = minAvailable > 0
    }

    // 填充沒有查到的（無庫存資料）
    for _, id := range roomTypeIDs {
        if _, exists := result[id]; !exists {
            result[id] = false
        }
    }

    return result, nil
}
```

---

## 監控與告警

### 核心監控指標

```go
// Metrics 監控指標
type Metrics struct {
    // 預訂指標
    ReservationsCreated  prometheus.Counter
    ReservationsCancelled prometheus.Counter
    ReservationsExpired  prometheus.Counter

    // 庫存指標
    InventoryChecks      prometheus.Counter
    InventoryChecksFailed prometheus.Counter
    OverbookingAttempts  prometheus.Counter

    // 效能指標
    ReservationDuration  prometheus.Histogram
    LockAcquisitionTime  prometheus.Histogram

    // 業務指標
    TotalRevenue         prometheus.Counter
    OccupancyRate        *prometheus.GaugeVec
}
```

### Grafana Dashboard

```json
{
  "dashboard": {
    "title": "Hotel Reservation System Dashboard",
    "panels": [
      {
        "title": "預訂成功率",
        "targets": [
          {
            "expr": "rate(reservations_created_total[5m]) / (rate(reservations_created_total[5m]) + rate(inventory_checks_failed_total[5m]))"
          }
        ]
      },
      {
        "title": "超售攔截次數",
        "targets": [
          {
            "expr": "rate(overbooking_attempts_total[1m])"
          }
        ]
      },
      {
        "title": "入住率",
        "targets": [
          {
            "expr": "occupancy_rate{hotel=\"taipei_grand\"}"
          }
        ]
      }
    ]
  }
}
```

---

## 成本估算

### 台灣地區成本（中型平台）

**假設**:
- 合作酒店：1,000 家
- 日訂單量：10,000 筆
- 註冊用戶：500,000 人

| 類別 | 月成本 | 說明 |
|-----|--------|------|
| 運算資源 | NT$ 45,000 | 6 台 API 服務器（4C8G） |
| 資料庫 | NT$ 80,000 | MySQL 主從（8C16G x 2） |
| Redis | NT$ 15,000 | 3 節點叢集 |
| Elasticsearch | NT$ 25,000 | 搜尋引擎 |
| 頻寬與 CDN | NT$ 8,000 | 圖片、靜態資源 |
| 監控與備份 | NT$ 7,000 | Prometheus + 備份 |
| **總計** | **NT$ 180,000** | **NT$ 2,160,000/年** |

**營收**（佣金 15%）：
- 平均訂單金額：NT$ 3,000
- 日訂單：10,000 筆
- 月佣金：NT$ 13,500,000
- 淨利：NT$ 13,320,000/月（74% 利潤率）

---

## 延伸閱讀

- [Booking.com Architecture](https://www.booking.com/content/about.html)
- [Airbnb Engineering Blog](https://medium.com/airbnb-engineering)
- [Distributed Locking with Redis](https://redis.io/docs/manual/patterns/distributed-locks/)
- [Hotel Revenue Management](https://en.wikipedia.org/wiki/Revenue_management)

---

**版本**: v1.0.0
**最後更新**: 2025-05-18
**維護者**: Reservation Team
