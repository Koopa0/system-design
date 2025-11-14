package snowflake

import (
	"sync"
	"testing"
	"time"
)

func TestNewGenerator(t *testing.T) {
	tests := []struct {
		name      string
		machineID int64
		expectErr bool
	}{
		{"valid min", 0, false},
		{"valid mid", 512, false},
		{"valid max", 1023, false},
		{"invalid negative", -1, true},
		{"invalid too large", 1024, true},
		{"invalid very large", 9999, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewGenerator(tt.machineID)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error for machineID=%d, got nil", tt.machineID)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for machineID=%d: %v", tt.machineID, err)
				return
			}
			if gen.machineID != tt.machineID {
				t.Errorf("generator machineID = %d, want %d", gen.machineID, tt.machineID)
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	gen, err := NewGenerator(1)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	// 生成多個 ID 並驗證唯一性
	ids := make(map[int64]bool)
	count := 1000

	for i := 0; i < count; i++ {
		id, err := gen.Generate()
		if err != nil {
			t.Fatalf("Generate() error: %v", err)
		}

		// 檢查唯一性
		if ids[id] {
			t.Fatalf("duplicate ID generated: %d", id)
		}
		ids[id] = true

		// 檢查 ID 為正數
		if id <= 0 {
			t.Fatalf("generated non-positive ID: %d", id)
		}
	}

	t.Logf("successfully generated %d unique IDs", count)
}

func TestGenerateConcurrency(t *testing.T) {
	gen, err := NewGenerator(1)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	// 並發生成 ID
	goroutines := 10
	idsPerGoroutine := 100

	var wg sync.WaitGroup
	idsChan := make(chan int64, goroutines*idsPerGoroutine)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				id, err := gen.Generate()
				if err != nil {
					t.Errorf("Generate() error: %v", err)
					return
				}
				idsChan <- id
			}
		}()
	}

	wg.Wait()
	close(idsChan)

	// 驗證唯一性
	ids := make(map[int64]bool)
	for id := range idsChan {
		if ids[id] {
			t.Fatalf("duplicate ID in concurrent generation: %d", id)
		}
		ids[id] = true
	}

	expectedCount := goroutines * idsPerGoroutine
	if len(ids) != expectedCount {
		t.Fatalf("expected %d unique IDs, got %d", expectedCount, len(ids))
	}

	t.Logf("successfully generated %d unique IDs concurrently", len(ids))
}

func TestParseID(t *testing.T) {
	gen, err := NewGenerator(123)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	id, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	timestamp, machineID, sequence := ParseID(id)

	// 驗證機器 ID
	if machineID != 123 {
		t.Errorf("parsed machineID = %d, want 123", machineID)
	}

	// 驗證序列號（第一個 ID 應該是 0）
	if sequence < 0 || sequence > maxSequence {
		t.Errorf("parsed sequence = %d, out of range [0, %d]", sequence, maxSequence)
	}

	// 驗證時間戳（應該接近當前時間）
	now := time.Now().UnixMilli()
	if timestamp < now-1000 || timestamp > now+1000 {
		t.Errorf("parsed timestamp = %d, too far from now=%d", timestamp, now)
	}
}

func TestParseIDToTime(t *testing.T) {
	gen, err := NewGenerator(1)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	before := time.Now()
	id, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	after := time.Now()

	parsedTime := ParseIDToTime(id)

	if parsedTime.Before(before) || parsedTime.After(after) {
		t.Errorf("parsed time %s is not between %s and %s",
			parsedTime, before, after)
	}
}

func TestGetInfo(t *testing.T) {
	gen, err := NewGenerator(456)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	id, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	info := GetInfo(id)

	if info.ID != id {
		t.Errorf("info.ID = %d, want %d", info.ID, id)
	}

	if info.MachineID != 456 {
		t.Errorf("info.MachineID = %d, want 456", info.MachineID)
	}

	t.Logf("ID Info: %s", info.String())
}

func TestSequenceOverflow(t *testing.T) {
	gen, err := NewGenerator(1)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	// 在同一毫秒內生成大量 ID，測試序列號溢出處理
	// 每毫秒最多 4096 個 ID，我們嘗試生成 5000 個
	startTime := time.Now()
	count := 5000
	successCount := 0

	for i := 0; i < count; i++ {
		_, err := gen.Generate()
		if err == nil {
			successCount++
		}
	}

	duration := time.Since(startTime)
	t.Logf("generated %d IDs in %v", successCount, duration)

	// 應該全部成功（會等待下一毫秒）
	if successCount != count {
		t.Errorf("expected all %d IDs to be generated, got %d", count, successCount)
	}
}

func TestConstants(t *testing.T) {
	t.Logf("Max IDs per millisecond: %d", MaxIDsPerMillisecond())
	t.Logf("Max machines: %d", MaxMachines())
	t.Logf("Lifetime: %d years", LifeTime())

	if MaxIDsPerMillisecond() != 4096 {
		t.Errorf("MaxIDsPerMillisecond() = %d, want 4096", MaxIDsPerMillisecond())
	}

	if MaxMachines() != 1024 {
		t.Errorf("MaxMachines() = %d, want 1024", MaxMachines())
	}

	// Lifetime 應該約 69 年
	lifetime := LifeTime()
	if lifetime < 60 || lifetime > 75 {
		t.Errorf("LifeTime() = %d, expected around 69 years", lifetime)
	}
}

func TestMonotonicity(t *testing.T) {
	// 測試 ID 的單調遞增性（趨勢遞增）
	gen, err := NewGenerator(1)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	var lastID int64
	for i := 0; i < 1000; i++ {
		id, err := gen.Generate()
		if err != nil {
			t.Fatalf("Generate() error: %v", err)
		}

		if id <= lastID {
			t.Fatalf("ID not monotonically increasing: last=%d, current=%d", lastID, id)
		}

		lastID = id
	}

	t.Log("IDs are monotonically increasing ✓")
}

// 基準測試
func BenchmarkGenerate(b *testing.B) {
	gen, _ := NewGenerator(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen.Generate()
	}
}

func BenchmarkGenerateConcurrent(b *testing.B) {
	gen, _ := NewGenerator(1)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			gen.Generate()
		}
	})
}

func BenchmarkParseID(b *testing.B) {
	gen, _ := NewGenerator(1)
	id, _ := gen.Generate()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseID(id)
	}
}

// 示例：展示如何使用 Snowflake
func ExampleGenerator_Generate() {
	// 創建生成器（機器 ID = 1）
	gen, err := NewGenerator(1)
	if err != nil {
		panic(err)
	}

	// 生成 ID
	id, err := gen.Generate()
	if err != nil {
		panic(err)
	}

	// 解析 ID
	info := GetInfo(id)
	println(info.String())
}
