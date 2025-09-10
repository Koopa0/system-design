package testutils

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/koopa0/system-design/exercise-1/internal/sqlc"
)

// MockQuerier 實作 sqlc.Querier 介面的 mock
type MockQuerier struct {
	mu       sync.RWMutex
	counters map[string]int64

	// 記錄呼叫次數
	IncrementCalls atomic.Int32
	DecrementCalls atomic.Int32
	GetCalls       atomic.Int32
	SetCalls       atomic.Int32
	ResetCalls     atomic.Int32

	// 錯誤注入
	ShouldFailNext bool
	FailError      error
}

// NewMockQuerier 創建新的 MockQuerier
func NewMockQuerier() *MockQuerier {
	return &MockQuerier{
		counters: make(map[string]int64),
	}
}

// IncrementCounter 實作 sqlc 的 IncrementCounter 方法
func (m *MockQuerier) IncrementCounter(ctx context.Context, params sqlc.IncrementCounterParams) (pgtype.Int8, error) {
	m.IncrementCalls.Add(1)

	if m.ShouldFailNext {
		m.ShouldFailNext = false
		return pgtype.Int8{}, m.FailError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.counters[params.Name]
	newValue := current + params.CurrentValue.Int64
	m.counters[params.Name] = newValue

	return pgtype.Int8{Int64: newValue, Valid: true}, nil
}

// DecrementCounter 實作 sqlc 的 DecrementCounter 方法
func (m *MockQuerier) DecrementCounter(ctx context.Context, params sqlc.DecrementCounterParams) (pgtype.Int8, error) {
	m.DecrementCalls.Add(1)

	if m.ShouldFailNext {
		m.ShouldFailNext = false
		return pgtype.Int8{}, m.FailError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.counters[params.Name]
	newValue := current - params.CurrentValue.Int64
	if newValue < 0 {
		newValue = 0
	}
	m.counters[params.Name] = newValue

	return pgtype.Int8{Int64: newValue, Valid: true}, nil
}

// GetCounter 實作 sqlc 的 GetCounter 方法
func (m *MockQuerier) GetCounter(ctx context.Context, name string) (sqlc.Counter, error) {
	m.GetCalls.Add(1)

	if m.ShouldFailNext {
		m.ShouldFailNext = false
		return sqlc.Counter{}, m.FailError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	value, exists := m.counters[name]
	if !exists {
		return sqlc.Counter{}, pgx.ErrNoRows
	}

	return sqlc.Counter{
		Name:         name,
		CurrentValue: pgtype.Int8{Int64: value, Valid: true},
	}, nil
}

// GetCounters 實作 sqlc 的 GetCounters 方法
func (m *MockQuerier) GetCounters(ctx context.Context, names []string) ([]sqlc.Counter, error) {
	m.GetCalls.Add(1)

	if m.ShouldFailNext {
		m.ShouldFailNext = false
		return nil, m.FailError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []sqlc.Counter
	for _, name := range names {
		if value, exists := m.counters[name]; exists {
			result = append(result, sqlc.Counter{
				Name:         name,
				CurrentValue: pgtype.Int8{Int64: value, Valid: true},
			})
		}
	}

	return result, nil
}

// SetCounter 實作 sqlc 的 SetCounter 方法
func (m *MockQuerier) SetCounter(ctx context.Context, params sqlc.SetCounterParams) error {
	m.SetCalls.Add(1)

	if m.ShouldFailNext {
		m.ShouldFailNext = false
		return m.FailError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.counters[params.Name] = params.CurrentValue.Int64
	return nil
}

// ResetCounter 實作 sqlc 的 ResetCounter 方法
func (m *MockQuerier) ResetCounter(ctx context.Context, name string) error {
	m.ResetCalls.Add(1)

	if m.ShouldFailNext {
		m.ShouldFailNext = false
		return m.FailError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.counters[name] = 0
	return nil
}

// CreateCounter 實作 sqlc 的 CreateCounter 方法
func (m *MockQuerier) CreateCounter(ctx context.Context, params sqlc.CreateCounterParams) (sqlc.Counter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.counters[params.Name]; exists {
		// 計數器已存在
		return sqlc.Counter{}, &pgconn.PgError{Code: "23505"} // unique violation
	}

	m.counters[params.Name] = 0
	return sqlc.Counter{
		Name:         params.Name,
		CurrentValue: pgtype.Int8{Int64: 0, Valid: true},
	}, nil
}

// EnqueueWrite 實作 sqlc 的 EnqueueWrite 方法
func (m *MockQuerier) EnqueueWrite(ctx context.Context, params sqlc.EnqueueWriteParams) (sqlc.WriteQueue, error) {
	// 簡單的 mock 實作
	return sqlc.WriteQueue{
		ID:          1,
		CounterName: params.CounterName,
		Operation:   params.Operation,
		Value:       params.Value,
	}, nil
}

// DequeueWrites 實作 sqlc 的 DequeueWrites 方法
func (m *MockQuerier) DequeueWrites(ctx context.Context, limit int32) ([]sqlc.WriteQueue, error) {
	// 返回空列表
	return []sqlc.WriteQueue{}, nil
}

// MarkWriteProcessed 實作 sqlc 的 MarkWriteProcessed 方法
func (m *MockQuerier) MarkWriteProcessed(ctx context.Context, id int32) error {
	return nil
}

// CleanOldQueue 實作 sqlc 的 CleanOldQueue 方法
func (m *MockQuerier) CleanOldQueue(ctx context.Context) error {
	return nil
}

// ListCounters 實作 sqlc 的 ListCounters 方法
func (m *MockQuerier) ListCounters(ctx context.Context, params sqlc.ListCountersParams) ([]sqlc.Counter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []sqlc.Counter
	count := int32(0)

	for name, value := range m.counters {
		if count >= params.Offset && count < params.Offset+params.Limit {
			result = append(result, sqlc.Counter{
				Name:         name,
				CurrentValue: pgtype.Int8{Int64: value, Valid: true},
			})
		}
		count++
		if count >= params.Offset+params.Limit {
			break
		}
	}

	return result, nil
}

// GetCounterStats 獲取計數器統計資訊（測試用）
func (m *MockQuerier) GetCounterStats() map[string]interface{} {
	return map[string]interface{}{
		"increment_calls": m.IncrementCalls.Load(),
		"decrement_calls": m.DecrementCalls.Load(),
		"get_calls":       m.GetCalls.Load(),
		"set_calls":       m.SetCalls.Load(),
		"reset_calls":     m.ResetCalls.Load(),
	}
}

// SetCounterValue 直接設置計數器值（測試用）
func (m *MockQuerier) SetCounterValue(name string, value int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[name] = value
}

// GetCounterValue 直接獲取計數器值（測試用）
func (m *MockQuerier) GetCounterValue(name string) (int64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, exists := m.counters[name]
	return value, exists
}