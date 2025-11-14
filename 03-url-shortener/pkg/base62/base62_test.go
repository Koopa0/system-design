package base62

import (
	"math"
	"testing"
)

func TestEncode(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected string
	}{
		{"zero", 0, "0"},
		{"one", 1, "1"},
		{"nine", 9, "9"},
		{"ten", 10, "A"},
		{"base minus one", 61, "z"},
		{"base", 62, "10"},
		{"large number", 123456789, "8M0kX"},
		{"very large", 3521614606207, "zzzzzz"},
		{"max uint64", math.MaxUint64, "LygHa16AHYF"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Encode(tt.input)
			if result != tt.expected {
				t.Errorf("Encode(%d) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  uint64
		expectErr bool
	}{
		{"zero", "0", 0, false},
		{"one", "1", 1, false},
		{"ten", "A", 10, false},
		{"base", "10", 62, false},
		{"large number", "8M0kX", 123456789, false},
		{"empty string", "", 0, false},
		{"invalid character", "8M0kX!", 0, true},
		{"invalid character space", "8M 0kX", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Decode(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Errorf("Decode(%s) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("Decode(%s) unexpected error: %v", tt.input, err)
				return
			}
			if result != tt.expected {
				t.Errorf("Decode(%s) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	testCases := []uint64{
		0, 1, 62, 123, 456789,
		uint64(1) << 32, // 4GB
		uint64(1) << 40, // 1TB
		math.MaxUint64 / 2,
		math.MaxUint64 - 1,
		math.MaxUint64,
	}

	for _, original := range testCases {
		encoded := Encode(original)
		decoded, err := Decode(encoded)
		if err != nil {
			t.Errorf("Round trip failed for %d: encode=%s, decode error=%v",
				original, encoded, err)
			continue
		}
		if decoded != original {
			t.Errorf("Round trip failed for %d: encoded=%s, decoded=%d",
				original, encoded, decoded)
		}
	}
}

func TestIsValid(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"0123456789", true},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZ", true},
		{"abcdefghijklmnopqrstuvwxyz", true},
		{"8M0kX", true},
		{"", true}, // 空字符串視為有效
		{"8M0kX!", false},
		{"hello world", false},
		{"test+test", false},
		{"test/test", false},
	}

	for _, tt := range tests {
		result := IsValid(tt.input)
		if result != tt.valid {
			t.Errorf("IsValid(%s) = %v, want %v", tt.input, result, tt.valid)
		}
	}
}

func TestLength(t *testing.T) {
	tests := []struct {
		input    uint64
		expected int
	}{
		{0, 1},
		{1, 1},
		{61, 1},
		{62, 2},
		{123456789, 5},
		{3521614606207, 6}, // 62^6 - 1
	}

	for _, tt := range tests {
		result := Length(tt.input)
		if result != tt.expected {
			t.Errorf("Length(%d) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestMaxValue(t *testing.T) {
	tests := []struct {
		length   int
		expected uint64
	}{
		{0, 0},
		{1, 61},            // 62^1 - 1
		{2, 3843},          // 62^2 - 1
		{6, 56800235583},   // 62^6 - 1
		{7, 3521614606207}, // 62^7 - 1
	}

	for _, tt := range tests {
		result := MaxValue(tt.length)
		if result != tt.expected {
			t.Errorf("MaxValue(%d) = %d, want %d", tt.length, result, tt.expected)
		}
	}
}

func TestPad(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		targetLen int
		expected  string
	}{
		{"no padding needed", "8M0kX", 3, "8M0kX"},
		{"pad to 7", "8M0kX", 7, "008M0kX"},
		{"pad to 10", "1", 10, "0000000001"},
		{"exact length", "abc", 3, "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Pad(tt.input, tt.targetLen)
			if result != tt.expected {
				t.Errorf("Pad(%s, %d) = %s, want %s",
					tt.input, tt.targetLen, result, tt.expected)
			}
		})
	}
}

// 基準測試
func BenchmarkEncode(b *testing.B) {
	nums := []uint64{0, 123, 456789, math.MaxUint64}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Encode(nums[i%len(nums)])
	}
}

func BenchmarkDecode(b *testing.B) {
	strs := []string{"0", "1Z", "8M0kX", "LygHa16AHYF"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Decode(strs[i%len(strs)])
	}
}

// 驗證容量計算
func TestCapacityCalculation(t *testing.T) {
	lengths := []int{6, 7, 8}
	for _, length := range lengths {
		capacity := MaxValue(length) + 1 // +1 因為包含 0
		t.Logf("Base62 長度 %d 可表示 %d 個唯一值", length, capacity)

		// 驗證：62^length
		expected := pow62(uint64(length))
		if capacity != expected {
			t.Errorf("容量計算錯誤：got %d, want %d", capacity, expected)
		}
	}

	// 輸出一些有用的信息
	t.Log("\nBase62 容量分析：")
	t.Logf("6 位: %d (約 568 億)", MaxValue(6)+1)
	t.Logf("7 位: %d (約 3.5 兆)", MaxValue(7)+1)
	t.Logf("8 位: %d (約 218 兆)", MaxValue(8)+1)
}
