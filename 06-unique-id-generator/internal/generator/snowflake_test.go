package generator

import (
	"sync"
	"testing"
	"time"
)

// TestSnowflake_Generate_Basic 測試基本 ID 生成
func TestSnowflake_Generate_Basic(t *testing.T) {
	sf, err := NewSnowflake(1)
	if err != nil {
		t.Fatalf("NewSnowflake failed: %v", err)
	}

	id, err := sf.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if id <= 0 {
		t.Errorf("Expected positive ID, got %d", id)
	}
}

// TestSnowflake_Generate_Uniqueness 測試 ID 唯一性
//
// 教學重點：
//   - 並發安全測試
//   - 大量 ID 唯一性驗證
func TestSnowflake_Generate_Uniqueness(t *testing.T) {
	sf, err := NewSnowflake(1)
	if err != nil {
		t.Fatalf("NewSnowflake failed: %v", err)
	}

	const count = 100000
	ids := make(map[int64]bool, count)
	mu := sync.Mutex{}

	// 並發生成 ID
	var wg sync.WaitGroup
	workers := 10
	perWorker := count / workers

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				id, err := sf.Generate()
				if err != nil {
					t.Errorf("Generate failed: %v", err)
					return
				}

				mu.Lock()
				if ids[id] {
					t.Errorf("Duplicate ID found: %d", id)
				}
				ids[id] = true
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(ids) != count {
		t.Errorf("Expected %d unique IDs, got %d", count, len(ids))
	}
}

// TestSnowflake_Generate_Monotonic 測試 ID 趨勢遞增
//
// 教學重點：
//   - Snowflake ID 的有序性
//   - 為什麼時間戳在高位
func TestSnowflake_Generate_Monotonic(t *testing.T) {
	sf, err := NewSnowflake(1)
	if err != nil {
		t.Fatalf("NewSnowflake failed: %v", err)
	}

	const count = 1000
	var lastID int64

	for i := 0; i < count; i++ {
		id, err := sf.Generate()
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}

		if id <= lastID {
			t.Errorf("ID not monotonic: last=%d, current=%d", lastID, id)
		}
		lastID = id

		// 輕微延遲避免序列號溢出
		if i%100 == 0 {
			time.Sleep(1 * time.Millisecond)
		}
	}
}

// TestSnowflake_InvalidMachineID 測試無效的機器 ID
func TestSnowflake_InvalidMachineID(t *testing.T) {
	tests := []struct {
		name      string
		machineID int64
		wantErr   bool
	}{
		{"Negative", -1, true},
		{"Zero", 0, false},
		{"Valid", 512, false},
		{"Max", 1023, false},
		{"TooLarge", 1024, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSnowflake(tt.machineID)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSnowflake() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSnowflake_ClockBackward_Small 測試小幅度時鐘回撥
//
// 教學重點：
//   - 小回撥容忍策略
//   - 監控計數器的使用
func TestSnowflake_ClockBackward_Small(t *testing.T) {
	sf, err := NewSnowflakeWithConfig(&Config{
		MachineID:     1,
		MaxBackwardMS: 5000,
	})
	if err != nil {
		t.Fatalf("NewSnowflakeWithConfig failed: %v", err)
	}

	// 生成一個 ID，記錄時間戳
	id1, err := sf.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// 模擬小幅度回撥（< 5s）
	// 注意：這個測試無法直接模擬時鐘回撥，只是驗證邏輯
	// 實際生產環境中，時鐘回撥由 NTP 校正引起

	// 快速生成多個 ID（同一毫秒內）
	for i := 0; i < 100; i++ {
		id2, err := sf.Generate()
		if err != nil {
			t.Fatalf("Generate failed after sequence: %v", err)
		}
		if id2 <= id1 {
			t.Errorf("ID not monotonic: %d <= %d", id2, id1)
		}
		id1 = id2
	}
}

// TestParseSnowflakeID 測試 ID 解析
//
// 教學重點：
//   - 位運算的逆操作
//   - 如何從 ID 提取信息
func TestParseSnowflakeID(t *testing.T) {
	machineID := int64(5)
	sf, err := NewSnowflake(machineID)
	if err != nil {
		t.Fatalf("NewSnowflake failed: %v", err)
	}

	// 生成 ID
	id, err := sf.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// 解析 ID
	info := ParseSnowflakeID(id)

	// 驗證機器 ID
	if info.MachineID != machineID {
		t.Errorf("Expected machine ID %d, got %d", machineID, info.MachineID)
	}

	// 驗證時間戳在合理範圍內（最近 10 秒）
	now := time.Now()
	diff := now.Sub(info.Time)
	if diff < 0 || diff > 10*time.Second {
		t.Errorf("Timestamp diff too large: %v", diff)
	}

	// 驗證序列號在有效範圍內
	if info.Sequence < 0 || info.Sequence > maxSequence {
		t.Errorf("Invalid sequence: %d", info.Sequence)
	}
}

// TestSnowflake_SequenceOverflow 測試序列號溢出
//
// 教學重點：
//   - 同一毫秒內生成大量 ID
//   - 序列號溢出時等待下一毫秒
func TestSnowflake_SequenceOverflow(t *testing.T) {
	sf, err := NewSnowflake(1)
	if err != nil {
		t.Fatalf("NewSnowflake failed: %v", err)
	}

	// 快速生成 ID，直到跨越毫秒
	var lastTimestamp int64
	foundNextMillis := false

	for i := 0; i < 5000; i++ {
		id, err := sf.Generate()
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}

		info := ParseSnowflakeID(id)
		if lastTimestamp > 0 && info.Timestamp > lastTimestamp {
			foundNextMillis = true
			break
		}
		lastTimestamp = info.Timestamp
	}

	if !foundNextMillis {
		t.Log("Warning: Did not trigger sequence overflow (may be normal)")
	}
}

// TestGetCapacity 測試容量計算
func TestGetCapacity(t *testing.T) {
	cap := GetCapacity()

	if cap.MaxIDsPerMillis != 4096 {
		t.Errorf("Expected 4096 IDs/ms, got %d", cap.MaxIDsPerMillis)
	}

	if cap.MaxMachines != 1024 {
		t.Errorf("Expected 1024 machines, got %d", cap.MaxMachines)
	}

	if cap.LifeTimeYears < 69 {
		t.Errorf("Expected at least 69 years, got %d", cap.LifeTimeYears)
	}
}

// BenchmarkSnowflake_Generate 性能測試
//
// 教學重點：
//   - 測量 ID 生成性能
//   - 並發鎖的開銷
func BenchmarkSnowflake_Generate(b *testing.B) {
	sf, err := NewSnowflake(1)
	if err != nil {
		b.Fatalf("NewSnowflake failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sf.Generate()
		if err != nil {
			b.Fatalf("Generate failed: %v", err)
		}
	}
}

// BenchmarkSnowflake_Generate_Parallel 並發性能測試
func BenchmarkSnowflake_Generate_Parallel(b *testing.B) {
	sf, err := NewSnowflake(1)
	if err != nil {
		b.Fatalf("NewSnowflake failed: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := sf.Generate()
			if err != nil {
				b.Fatalf("Generate failed: %v", err)
			}
		}
	})
}

// BenchmarkParseSnowflakeID 解析性能測試
func BenchmarkParseSnowflakeID(b *testing.B) {
	sf, _ := NewSnowflake(1)
	id, _ := sf.Generate()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ParseSnowflakeID(id)
	}
}
