# Chapter 31: Hotel Reservation（酒店預訂系統）

> **難度**：★★★☆☆
> **預估時間**：3-4 週
> **核心概念**：分散式鎖、庫存管理、超售防範、預訂狀態機

---

## Act 1: 超售的噩夢

週一早晨，Emma 收到了一封來自客服部門的緊急郵件。

**Emma**：「各位，我們有大麻煩了！週末有 3 位客人同時預訂了同一間房，但我們只有 1 間房！」

**David**：「這是經典的 **超售（Overbooking）** 問題。當多個用戶同時預訂時，系統沒有正確處理並發。」

**Sarah**：「讓我看看程式碼...」

```go
// ❌ 錯誤示範：沒有並發控制
func (s *ReservationService) CreateReservation(req *CreateReservationRequest) (*Reservation, error) {
    // 1. 檢查房間是否可用
    room, err := s.roomRepo.GetRoom(req.RoomID)
    if err != nil {
        return nil, err
    }

    if room.AvailableCount <= 0 {
        return nil, errors.New("房間已滿")
    }

    // 2. 建立預訂
    reservation := &Reservation{
        RoomID:    req.RoomID,
        UserID:    req.UserID,
        CheckIn:   req.CheckIn,
        CheckOut:  req.CheckOut,
        Status:    "pending",
        CreatedAt: time.Now(),
    }

    if err := s.reservationRepo.Create(reservation); err != nil {
        return nil, err
    }

    // 3. 減少可用房間數
    room.AvailableCount--
    s.roomRepo.Update(room)

    return reservation, nil
}
```

**Michael**：「我看到問題了！在步驟 1 和步驟 3 之間，有一個 **競爭條件（Race Condition）**。」

**Emma**：「什麼是競爭條件？」

**David**：「假設有 2 個用戶同時預訂：」

```
時間線：
─────────────────────────────────────────────────
用戶 A                    用戶 B
─────────────────────────────────────────────────
檢查房間：1 間可用
                          檢查房間：1 間可用
建立預訂
                          建立預訂
減少房間數：1 -> 0
                          減少房間數：0 -> -1
```

**Sarah**：「兩個用戶都通過了檢查，結果同一間房被預訂了兩次！」

**Michael**：「這就是為什麼我們需要 **並發控制機制**。」

---

## Act 2: 樂觀鎖與悲觀鎖

**David**：「處理並發有兩種主要策略：樂觀鎖和悲觀鎖。」

**Emma**：「有什麼區別？」

### 樂觀鎖（Optimistic Locking）

**Michael**：「樂觀鎖假設衝突很少發生，所以不加鎖。而是在更新時檢查資料是否被修改過。」

```go
// Room 資料模型（使用版本號）
type Room struct {
    ID             int64
    HotelID        int64
    RoomType       string
    AvailableCount int
    Version        int64  // 版本號
    UpdatedAt      time.Time
}

// ✅ 使用樂觀鎖
func (s *ReservationService) CreateReservationOptimistic(req *CreateReservationRequest) (*Reservation, error) {
    maxRetries := 3

    for i := 0; i < maxRetries; i++ {
        // 1. 讀取房間資訊（包含版本號）
        room, err := s.roomRepo.GetRoom(req.RoomID)
        if err != nil {
            return nil, err
        }

        if room.AvailableCount <= 0 {
            return nil, errors.New("房間已滿")
        }

        // 2. 建立預訂
        reservation := &Reservation{
            RoomID:    req.RoomID,
            UserID:    req.UserID,
            CheckIn:   req.CheckIn,
            CheckOut:  req.CheckOut,
            Status:    "pending",
            CreatedAt: time.Now(),
        }

        if err := s.reservationRepo.Create(reservation); err != nil {
            return nil, err
        }

        // 3. 使用 CAS（Compare-And-Swap）更新房間數
        // SQL: UPDATE rooms SET available_count = available_count - 1, version = version + 1
        //      WHERE id = ? AND version = ?
        updated, err := s.roomRepo.DecrementWithVersion(
            room.ID,
            room.Version,
        )

        if err != nil {
            return nil, err
        }

        if updated {
            // 成功！
            return reservation, nil
        }

        // 版本號不匹配，說明有其他人修改了，重試
        log.Warn("樂觀鎖衝突，重試", "attempt", i+1)
        time.Sleep(time.Millisecond * 10)
    }

    return nil, errors.New("預訂失敗，請重試")
}

// DecrementWithVersion 使用版本號減少庫存
func (r *RoomRepository) DecrementWithVersion(roomID int64, expectedVersion int64) (bool, error) {
    result, err := r.db.Exec(`
        UPDATE rooms
        SET available_count = available_count - 1,
            version = version + 1,
            updated_at = NOW()
        WHERE id = ? AND version = ? AND available_count > 0
    `, roomID, expectedVersion)

    if err != nil {
        return false, err
    }

    rowsAffected, _ := result.RowsAffected()
    return rowsAffected > 0, nil
}
```

**Sarah**：「如果版本號不匹配，說明有其他交易修改了資料，我們就重試。」

**Emma**：「這很聰明！不需要加鎖，只在最後更新時檢查。」

### 悲觀鎖（Pessimistic Locking）

**David**：「悲觀鎖則假設衝突經常發生，所以在讀取時就加鎖。」

```go
// ✅ 使用悲觀鎖（資料庫行鎖）
func (s *ReservationService) CreateReservationPessimistic(req *CreateReservationRequest) (*Reservation, error) {
    // 開始資料庫交易
    tx, err := s.db.BeginTx(context.Background(), nil)
    if err != nil {
        return nil, err
    }
    defer tx.Rollback()

    // 1. 使用 FOR UPDATE 鎖定房間記錄
    room, err := s.roomRepo.GetRoomForUpdate(tx, req.RoomID)
    if err != nil {
        return nil, err
    }

    if room.AvailableCount <= 0 {
        return nil, errors.New("房間已滿")
    }

    // 2. 建立預訂
    reservation := &Reservation{
        RoomID:    req.RoomID,
        UserID:    req.UserID,
        CheckIn:   req.CheckIn,
        CheckOut:  req.CheckOut,
        Status:    "pending",
        CreatedAt: time.Now(),
    }

    if err := s.reservationRepo.CreateWithTx(tx, reservation); err != nil {
        return nil, err
    }

    // 3. 減少房間數
    room.AvailableCount--
    if err := s.roomRepo.UpdateWithTx(tx, room); err != nil {
        return nil, err
    }

    // 4. 提交交易（釋放鎖）
    if err := tx.Commit(); err != nil {
        return nil, err
    }

    return reservation, nil
}

// GetRoomForUpdate 使用 FOR UPDATE 鎖定記錄
func (r *RoomRepository) GetRoomForUpdate(tx *sql.Tx, roomID int64) (*Room, error) {
    var room Room

    err := tx.QueryRow(`
        SELECT id, hotel_id, room_type, available_count, version, updated_at
        FROM rooms
        WHERE id = ?
        FOR UPDATE  -- 悲觀鎖：鎖定此行直到交易結束
    `, roomID).Scan(&room.ID, &room.HotelID, &room.RoomType, &room.AvailableCount, &room.Version, &room.UpdatedAt)

    if err != nil {
        return nil, err
    }

    return &room, nil
}
```

**Michael**：「`FOR UPDATE` 會鎖定這一行，其他交易必須等待，直到我們提交或回滾。」

**Sarah**：「那選擇哪種鎖？」

**David**：「各有優缺點：」

| 比較項目 | 樂觀鎖 | 悲觀鎖 |
|---------|--------|--------|
| **適用場景** | 衝突少、讀多寫少 | 衝突多、寫密集 |
| **效能** | 高（無鎖等待） | 低（有鎖等待） |
| **實作複雜度** | 中（需要重試邏輯） | 簡單 |
| **資料庫負載** | 低 | 高（鎖競爭） |
| **範例** | 文章編輯、商品瀏覽 | 搶票、秒殺、酒店預訂 |

**Emma**：「對於酒店預訂，房間數量有限，衝突可能較多，所以悲觀鎖更合適？」

**Michael**：「沒錯！但我們還有第三種選擇：**分散式鎖**。」

---

## Act 3: 分散式鎖

**David**：「當系統有多個伺服器時，資料庫鎖可能不夠用。我們需要 **分散式鎖**。」

**Sarah**：「什麼是分散式鎖？」

**Michael**：「分散式鎖是跨多個伺服器的鎖機制。最常用的是 **Redis 分散式鎖**。」

### Redis 分散式鎖

```go
// RedisLock Redis 分散式鎖
type RedisLock struct {
    client *redis.Client
    key    string
    value  string // UUID（確保只有持鎖者能釋放）
    ttl    time.Duration
}

// Lock 獲取鎖
func (l *RedisLock) Lock(ctx context.Context) (bool, error) {
    // 使用 SET NX EX 命令
    // NX: 只在鍵不存在時設定
    // EX: 設定過期時間（防止死鎖）
    success, err := l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
    return success, err
}

// Unlock 釋放鎖（使用 Lua 腳本確保原子性）
func (l *RedisLock) Unlock(ctx context.Context) error {
    script := `
        if redis.call("get", KEYS[1]) == ARGV[1] then
            return redis.call("del", KEYS[1])
        else
            return 0
        end
    `

    _, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
    return err
}

// TryLock 嘗試獲取鎖（帶重試）
func (l *RedisLock) TryLock(ctx context.Context, retries int, retryDelay time.Duration) (bool, error) {
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
```

### 使用分散式鎖預訂

```go
// ✅ 使用分散式鎖
func (s *ReservationService) CreateReservationWithDistributedLock(req *CreateReservationRequest) (*Reservation, error) {
    ctx := context.Background()

    // 1. 建立分散式鎖
    lockKey := fmt.Sprintf("room:lock:%d:%s:%s",
        req.RoomID,
        req.CheckIn.Format("2006-01-02"),
        req.CheckOut.Format("2006-01-02"),
    )

    lock := &RedisLock{
        client: s.redisClient,
        key:    lockKey,
        value:  uuid.New().String(),
        ttl:    10 * time.Second,
    }

    // 2. 嘗試獲取鎖
    acquired, err := lock.TryLock(ctx, 3, 100*time.Millisecond)
    if err != nil {
        return nil, err
    }

    if !acquired {
        return nil, errors.New("系統繁忙，請稍後再試")
    }

    defer lock.Unlock(ctx)

    // 3. 檢查房間可用性
    available, err := s.checkRoomAvailability(req.RoomID, req.CheckIn, req.CheckOut)
    if err != nil {
        return nil, err
    }

    if !available {
        return nil, errors.New("房間已滿")
    }

    // 4. 建立預訂
    reservation := &Reservation{
        RoomID:    req.RoomID,
        UserID:    req.UserID,
        CheckIn:   req.CheckIn,
        CheckOut:  req.CheckOut,
        Status:    "pending",
        CreatedAt: time.Now(),
    }

    if err := s.reservationRepo.Create(reservation); err != nil {
        return nil, err
    }

    // 5. 減少庫存
    if err := s.decrementInventory(req.RoomID, req.CheckIn, req.CheckOut); err != nil {
        // 回滾預訂
        s.reservationRepo.Delete(reservation.ID)
        return nil, err
    }

    return reservation, nil
}
```

**Emma**：「這樣即使有多個伺服器，同一時間也只有一個能預訂特定房間！」

**David**：「沒錯。而且我們使用了 **細粒度鎖**：不是鎖整個酒店，而是只鎖特定房間的特定日期。」

---

## Act 4: 庫存管理

**Sarah**：「我們一直在說『減少庫存』，但實際上酒店的庫存是怎麼管理的？」

**Michael**：「這是個好問題。酒店庫存有兩種模型。」

### 模型 1: 計數器模型（Counter Model）

**David**：「最簡單的方式是記錄每種房型的總數和已預訂數。」

```go
// RoomType 房型
type RoomType struct {
    ID           int64
    HotelID      int64
    Name         string  // "標準雙人房"
    TotalCount   int     // 總房間數：100
    Description  string
    BasePrice    int64   // 基礎價格（分）
    Amenities    string  // 設施（JSON）
}

// RoomInventory 房間庫存（按日期）
type RoomInventory struct {
    ID             int64
    RoomTypeID     int64
    Date           time.Time  // 2025-05-18
    TotalCount     int        // 總數：100
    BookedCount    int        // 已預訂：45
    AvailableCount int        // 可用：55
    Price          int64      // 當日價格（可能有動態定價）
}

// CheckAvailability 檢查可用性
func (s *InventoryService) CheckAvailability(roomTypeID int64, checkIn, checkOut time.Time) (bool, error) {
    // 需要檢查入住和退房之間的每一天
    currentDate := checkIn
    for currentDate.Before(checkOut) {
        inventory, err := s.inventoryRepo.GetByDate(roomTypeID, currentDate)
        if err != nil {
            return false, err
        }

        if inventory.AvailableCount <= 0 {
            return false, nil // 該日期無房
        }

        currentDate = currentDate.AddDate(0, 0, 1)
    }

    return true, nil
}

// DecrementInventory 減少庫存
func (s *InventoryService) DecrementInventory(roomTypeID int64, checkIn, checkOut time.Time) error {
    currentDate := checkIn
    for currentDate.Before(checkOut) {
        // 原子性減少
        affected, err := s.inventoryRepo.Decrement(roomTypeID, currentDate)
        if err != nil {
            // 需要回滾之前的減少
            s.rollbackDecrement(roomTypeID, checkIn, currentDate)
            return err
        }

        if affected == 0 {
            // 庫存不足
            s.rollbackDecrement(roomTypeID, checkIn, currentDate)
            return errors.New("庫存不足")
        }

        currentDate = currentDate.AddDate(0, 0, 1)
    }

    return nil
}

// Decrement SQL 實作
func (r *InventoryRepository) Decrement(roomTypeID int64, date time.Time) (int64, error) {
    result, err := r.db.Exec(`
        UPDATE room_inventory
        SET booked_count = booked_count + 1,
            available_count = available_count - 1
        WHERE room_type_id = ?
          AND date = ?
          AND available_count > 0
    `, roomTypeID, date)

    if err != nil {
        return 0, err
    }

    return result.RowsAffected()
}
```

**Emma**：「這個模型很直觀，但如果預訂跨多天，需要鎖定多個日期的庫存。」

**Sarah**：「對，而且如果中途某天失敗了，還要回滾之前的操作。」

### 模型 2: 預訂記錄模型（Reservation Record Model）

**Michael**：「另一種方式是不記錄總數，而是記錄每個預訂，動態計算可用數。」

```go
// 不需要 RoomInventory 表
// 直接從 Reservations 表計算

// CheckAvailabilityByCount 通過計數檢查可用性
func (s *InventoryService) CheckAvailabilityByCount(roomTypeID int64, checkIn, checkOut time.Time) (bool, error) {
    // 查詢房型總數
    roomType, err := s.roomTypeRepo.GetByID(roomTypeID)
    if err != nil {
        return false, err
    }

    // 計算每一天的已預訂數
    currentDate := checkIn
    for currentDate.Before(checkOut) {
        // 計算該日期有多少預訂（包括該日期在入住期間的所有預訂）
        count, err := s.reservationRepo.CountByDate(roomTypeID, currentDate)
        if err != nil {
            return false, err
        }

        if count >= roomType.TotalCount {
            return false, nil // 該日期已滿
        }

        currentDate = currentDate.AddDate(0, 0, 1)
    }

    return true, nil
}

// CountByDate 計算指定日期的預訂數
func (r *ReservationRepository) CountByDate(roomTypeID int64, date time.Time) (int, error) {
    var count int

    err := r.db.QueryRow(`
        SELECT COUNT(*)
        FROM reservations
        WHERE room_type_id = ?
          AND status NOT IN ('cancelled', 'expired')
          AND check_in <= ?
          AND check_out > ?
    `, roomTypeID, date, date).Scan(&count)

    return count, err
}
```

**David**：「這兩種模型各有優缺點：」

| 比較項目 | 計數器模型 | 預訂記錄模型 |
|---------|-----------|------------|
| **查詢效能** | 快（直接讀庫存表） | 慢（需要 COUNT） |
| **寫入複雜度** | 高（需要維護庫存） | 低（只寫預訂） |
| **資料一致性** | 難（庫存可能不準） | 易（單一數據源） |
| **歷史追溯** | 難 | 易（有完整記錄） |
| **適用規模** | 大型酒店（房間多） | 中小型酒店 |

**Sarah**：「實務上會選哪種？」

**Michael**：「大多數系統使用 **混合模型**：計數器用於快速查詢，預訂記錄用於最終驗證。」

---

## Act 5: 預訂狀態機

**Emma**：「預訂建立後，還有很多狀態需要管理：待支付、已確認、已入住、已完成、已取消...」

**David**：「沒錯。我們需要一個 **狀態機（State Machine）** 來管理預訂的生命週期。」

### 狀態定義

```go
// ReservationStatus 預訂狀態
type ReservationStatus string

const (
    // 待支付：用戶剛建立預訂，尚未支付
    StatusPending ReservationStatus = "pending"

    // 已確認：支付成功，預訂確認
    StatusConfirmed ReservationStatus = "confirmed"

    // 已入住：客人已 check-in
    StatusCheckedIn ReservationStatus = "checked_in"

    // 已完成：客人已 check-out
    StatusCompleted ReservationStatus = "completed"

    // 已取消：用戶主動取消或超時未支付
    StatusCancelled ReservationStatus = "cancelled"

    // 已過期：未支付超時自動取消
    StatusExpired ReservationStatus = "expired"

    // 未入住：No-show（預訂有效但客人沒來）
    StatusNoShow ReservationStatus = "no_show"
)

// Reservation 預訂
type Reservation struct {
    ID        int64
    UserID    string
    HotelID   int64
    RoomTypeID int64

    CheckIn   time.Time
    CheckOut  time.Time
    Nights    int

    Status    ReservationStatus
    TotalPrice int64

    // 時間戳
    CreatedAt   time.Time
    ConfirmedAt time.Time
    CheckedInAt time.Time
    CompletedAt time.Time
    CancelledAt time.Time
}
```

### 狀態轉換

**Michael**：「狀態機定義了允許的狀態轉換：」

```go
// StateTransition 狀態轉換規則
var StateTransitionRules = map[ReservationStatus][]ReservationStatus{
    StatusPending: {
        StatusConfirmed, // 支付成功
        StatusCancelled, // 用戶取消
        StatusExpired,   // 超時未支付
    },
    StatusConfirmed: {
        StatusCheckedIn, // 入住
        StatusCancelled, // 取消（可能有手續費）
        StatusNoShow,    // 未入住
    },
    StatusCheckedIn: {
        StatusCompleted, // 退房
    },
    // 終態（無法轉換）
    StatusCompleted: {},
    StatusCancelled: {},
    StatusExpired:   {},
    StatusNoShow:    {},
}

// CanTransition 檢查是否允許轉換
func CanTransition(from, to ReservationStatus) bool {
    allowedStates, exists := StateTransitionRules[from]
    if !exists {
        return false
    }

    for _, state := range allowedStates {
        if state == to {
            return true
        }
    }

    return false
}

// TransitionTo 轉換狀態
func (r *Reservation) TransitionTo(newStatus ReservationStatus) error {
    if !CanTransition(r.Status, newStatus) {
        return fmt.Errorf("不允許從 %s 轉換到 %s", r.Status, newStatus)
    }

    oldStatus := r.Status
    r.Status = newStatus

    // 更新相應的時間戳
    switch newStatus {
    case StatusConfirmed:
        r.ConfirmedAt = time.Now()
    case StatusCheckedIn:
        r.CheckedInAt = time.Now()
    case StatusCompleted:
        r.CompletedAt = time.Now()
    case StatusCancelled, StatusExpired:
        r.CancelledAt = time.Now()
    }

    log.Info("預訂狀態轉換",
        "reservation_id", r.ID,
        "from", oldStatus,
        "to", newStatus,
    )

    return nil
}
```

### 自動過期

**Sarah**：「如果用戶建立預訂後不支付，怎麼辦？」

**David**：「我們需要一個 **定時任務** 來自動過期未支付的預訂。」

```go
// ExpirationWorker 過期處理工作者
type ExpirationWorker struct {
    reservationRepo ReservationRepository
    inventoryService *InventoryService
}

// Run 運行過期檢查
func (w *ExpirationWorker) Run() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        w.expirePendingReservations()
    }
}

// expirePendingReservations 過期待支付預訂
func (w *ExpirationWorker) expirePendingReservations() {
    // 查詢超過 15 分鐘未支付的預訂
    cutoffTime := time.Now().Add(-15 * time.Minute)

    reservations, err := w.reservationRepo.FindPendingBefore(cutoffTime)
    if err != nil {
        log.Error("查詢待過期預訂失敗", err)
        return
    }

    for _, reservation := range reservations {
        // 轉換狀態為已過期
        if err := reservation.TransitionTo(StatusExpired); err != nil {
            log.Error("過期預訂失敗", "reservation_id", reservation.ID, "error", err)
            continue
        }

        // 更新資料庫
        if err := w.reservationRepo.Update(reservation); err != nil {
            log.Error("更新預訂狀態失敗", "reservation_id", reservation.ID, "error", err)
            continue
        }

        // 釋放庫存
        if err := w.inventoryService.IncrementInventory(
            reservation.RoomTypeID,
            reservation.CheckIn,
            reservation.CheckOut,
        ); err != nil {
            log.Error("釋放庫存失敗", "reservation_id", reservation.ID, "error", err)
        }

        log.Info("預訂已過期", "reservation_id", reservation.ID)
    }
}
```

**Emma**：「這樣就能自動回收未支付的預訂，釋放庫存給其他客人！」

---

## Act 6: 取消政策與退款

**Sarah**：「客人取消預訂時，退款規則是怎樣的？」

**Michael**：「這取決於 **取消政策（Cancellation Policy）**。」

### 取消政策類型

```go
// CancellationPolicy 取消政策
type CancellationPolicy struct {
    ID          int64
    Name        string
    Description string
    Rules       []CancellationRule // 取消規則
}

// CancellationRule 取消規則
type CancellationRule struct {
    DaysBefore    int     // 入住前幾天
    RefundPercent float64 // 退款比例（0-1）
}

// 範例：標準取消政策
var StandardPolicy = &CancellationPolicy{
    Name: "標準取消政策",
    Rules: []CancellationRule{
        {DaysBefore: 7, RefundPercent: 1.0},   // 7天前取消：全額退款
        {DaysBefore: 3, RefundPercent: 0.5},   // 3-7天前：50% 退款
        {DaysBefore: 1, RefundPercent: 0.0},   // 1-3天前：不退款
        {DaysBefore: 0, RefundPercent: 0.0},   // 入住當天：不退款
    },
}

// 範例：靈活取消政策
var FlexiblePolicy = &CancellationPolicy{
    Name: "靈活取消政策",
    Rules: []CancellationRule{
        {DaysBefore: 1, RefundPercent: 1.0},   // 1天前取消：全額退款
        {DaysBefore: 0, RefundPercent: 0.5},   // 入住當天：50% 退款
    },
}

// 範例：不可取消政策
var NonRefundablePolicy = &CancellationPolicy{
    Name: "不可取消政策",
    Rules: []CancellationRule{
        {DaysBefore: 0, RefundPercent: 0.0},   // 任何時候取消：不退款
    },
}
```

### 計算退款金額

```go
// CalculateRefund 計算退款金額
func (p *CancellationPolicy) CalculateRefund(reservation *Reservation, cancelTime time.Time) int64 {
    // 計算距離入住還有幾天
    daysUntilCheckIn := int(reservation.CheckIn.Sub(cancelTime).Hours() / 24)

    // 找到適用的規則
    var refundPercent float64 = 0.0

    for _, rule := range p.Rules {
        if daysUntilCheckIn >= rule.DaysBefore {
            refundPercent = rule.RefundPercent
            break
        }
    }

    // 計算退款金額
    refundAmount := int64(float64(reservation.TotalPrice) * refundPercent)

    log.Info("計算退款",
        "reservation_id", reservation.ID,
        "days_until_checkin", daysUntilCheckIn,
        "refund_percent", refundPercent,
        "total_price", reservation.TotalPrice,
        "refund_amount", refundAmount,
    )

    return refundAmount
}

// CancelReservation 取消預訂
func (s *ReservationService) CancelReservation(reservationID int64, reason string) error {
    // 1. 查詢預訂
    reservation, err := s.reservationRepo.GetByID(reservationID)
    if err != nil {
        return err
    }

    // 2. 檢查是否可以取消
    if !CanTransition(reservation.Status, StatusCancelled) {
        return errors.New("該預訂無法取消")
    }

    // 3. 獲取取消政策
    policy, err := s.policyRepo.GetByID(reservation.PolicyID)
    if err != nil {
        return err
    }

    // 4. 計算退款金額
    refundAmount := policy.CalculateRefund(reservation, time.Now())

    // 5. 轉換狀態
    if err := reservation.TransitionTo(StatusCancelled); err != nil {
        return err
    }

    reservation.RefundAmount = refundAmount
    reservation.CancellationReason = reason

    // 6. 更新預訂
    if err := s.reservationRepo.Update(reservation); err != nil {
        return err
    }

    // 7. 釋放庫存
    if err := s.inventoryService.IncrementInventory(
        reservation.RoomTypeID,
        reservation.CheckIn,
        reservation.CheckOut,
    ); err != nil {
        log.Error("釋放庫存失敗", err)
    }

    // 8. 處理退款（如果有）
    if refundAmount > 0 {
        if err := s.paymentService.Refund(reservation.PaymentID, refundAmount); err != nil {
            log.Error("退款失敗", err)
            // 告警：需要人工處理
        }
    }

    // 9. 發送通知
    s.notificationService.SendCancellationEmail(reservation)

    return nil
}
```

**Emma**：「政策越靈活，客人越喜歡，但酒店風險越大。」

**David**：「沒錯。這就是為什麼不可退款的房價通常更便宜——酒店確保了收入。」

---

## Act 7: 動態定價

**Sarah**：「我注意到同一間房，不同日期價格不同。這是怎麼實作的？」

**Michael**：「這叫 **動態定價（Dynamic Pricing）**，也叫收益管理（Revenue Management）。」

### 定價因素

**David**：「價格受多種因素影響：」

```go
// PricingEngine 定價引擎
type PricingEngine struct {
    basePriceRepo BasePriceRepository
    demandPredictor *DemandPredictor
}

// CalculatePrice 計算價格
func (e *PricingEngine) CalculatePrice(roomTypeID int64, date time.Time) int64 {
    // 1. 基礎價格
    basePrice := e.getBasePrice(roomTypeID)

    // 2. 季節性調整
    seasonalMultiplier := e.getSeasonalMultiplier(date)

    // 3. 需求調整（基於預訂率）
    occupancyRate := e.getOccupancyRate(roomTypeID, date)
    demandMultiplier := e.getDemandMultiplier(occupancyRate)

    // 4. 星期幾調整（週末通常較貴）
    weekdayMultiplier := e.getWeekdayMultiplier(date)

    // 5. 特殊事件調整（演唱會、展覽等）
    eventMultiplier := e.getEventMultiplier(date)

    // 6. 提前預訂折扣
    advanceBookingDiscount := e.getAdvanceBookingDiscount(date)

    // 綜合計算
    finalPrice := float64(basePrice) *
        seasonalMultiplier *
        demandMultiplier *
        weekdayMultiplier *
        eventMultiplier *
        advanceBookingDiscount

    return int64(finalPrice)
}

// getSeasonalMultiplier 季節性調整
func (e *PricingEngine) getSeasonalMultiplier(date time.Time) float64 {
    month := date.Month()

    switch {
    case month >= 7 && month <= 8:
        return 1.5 // 暑假旺季：+50%
    case month == 12 || month == 1 || month == 2:
        return 1.3 // 寒假、春節：+30%
    case month >= 4 && month <= 5:
        return 1.2 // 春季：+20%
    default:
        return 1.0 // 平季
    }
}

// getDemandMultiplier 需求調整
func (e *PricingEngine) getDemandMultiplier(occupancyRate float64) float64 {
    switch {
    case occupancyRate > 0.9:
        return 1.5 // 剩餘房間 < 10%：大幅漲價
    case occupancyRate > 0.8:
        return 1.3 // 剩餘房間 < 20%：中度漲價
    case occupancyRate > 0.6:
        return 1.1 // 剩餘房間 < 40%：小幅漲價
    case occupancyRate < 0.3:
        return 0.8 // 剩餘房間 > 70%：降價促銷
    default:
        return 1.0 // 正常價格
    }
}

// getWeekdayMultiplier 星期幾調整
func (e *PricingEngine) getWeekdayMultiplier(date time.Time) float64 {
    weekday := date.Weekday()

    if weekday == time.Friday || weekday == time.Saturday {
        return 1.2 // 週末：+20%
    }

    return 1.0
}

// getAdvanceBookingDiscount 提前預訂折扣
func (e *PricingEngine) getAdvanceBookingDiscount(date time.Time) float64 {
    daysInAdvance := int(date.Sub(time.Now()).Hours() / 24)

    switch {
    case daysInAdvance > 60:
        return 0.8 // 提前 2 個月：20% 折扣
    case daysInAdvance > 30:
        return 0.9 // 提前 1 個月：10% 折扣
    default:
        return 1.0 // 無折扣
    }
}
```

**Emma**：「所以一間房的價格可能每天都在變化！」

**Michael**：「沒錯。航空公司、酒店都使用這種策略來最大化收益。」

**Sarah**：「這需要機器學習來預測需求嗎？」

**David**：「可以！更進階的系統會使用 ML 模型：」

```go
// DemandPredictor 需求預測器（使用機器學習）
type DemandPredictor struct {
    model *MLModel
}

// PredictOccupancy 預測入住率
func (p *DemandPredictor) PredictOccupancy(hotelID int64, date time.Time) float64 {
    // 特徵工程
    features := map[string]float64{
        "day_of_week":      float64(date.Weekday()),
        "month":            float64(date.Month()),
        "days_until":       float64(date.Sub(time.Now()).Hours() / 24),
        "historical_rate":  p.getHistoricalRate(hotelID, date),
        "nearby_events":    p.getNearbyEvents(hotelID, date),
        "competitor_price": p.getCompetitorPrice(hotelID, date),
    }

    // 使用訓練好的模型預測
    prediction := p.model.Predict(features)

    return prediction
}
```

**Emma**：「酒店系統比我想像的複雜多了！」

---

## 總結

本章我們深入學習了 **Hotel Reservation（酒店預訂系統）** 的設計，涵蓋：

### 核心技術點

1. **並發控制**
   - 樂觀鎖（版本號）
   - 悲觀鎖（FOR UPDATE）
   - 分散式鎖（Redis）

2. **庫存管理**
   - 計數器模型（快速查詢）
   - 預訂記錄模型（資料一致性）
   - 混合模型（實務應用）

3. **預訂狀態機**
   - 狀態定義（7 種狀態）
   - 狀態轉換規則
   - 自動過期機制

4. **取消政策**
   - 多種政策類型（標準、靈活、不可取消）
   - 退款金額計算
   - 庫存釋放

5. **動態定價**
   - 多因素定價（季節、需求、星期）
   - 提前預訂折扣
   - 機器學習預測

### 架構特點

- **高並發**：分散式鎖 + 樂觀鎖
- **零超售**：嚴格的庫存控制
- **靈活定價**：動態定價引擎
- **自動化**：定時任務處理過期

酒店預訂系統需要精確的庫存管理和靈活的業務規則。通過本章學習，你已經掌握了構建生產級酒店系統的核心技術！🏨✨
