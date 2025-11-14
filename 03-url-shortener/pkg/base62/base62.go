// Package base62 提供 Base62 編碼和解碼功能
//
// Base62 使用字符集：0-9, A-Z, a-z（共 62 個字符）
// 相比 Base64，Base62 不包含 URL 中的特殊字符（+ 和 /），更適合用於 URL 短碼
//
// 使用場景：
//   - URL 短網址服務（將數字 ID 編碼為短碼）
//   - 生成緊湊的唯一標識符
//   - 任何需要將大數字轉換為短字符串的場景
package base62

import (
	"errors"
	"math"
	"strings"
)

// 字符集：0-9（10個）+ A-Z（26個）+ a-z（26個）= 62個字符
// 順序很重要：保持 0-9, A-Z, a-z 的順序，方便調試
const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

const base = 62

var (
	// ErrInvalidCharacter 當輸入字符串包含非 Base62 字符時返回
	ErrInvalidCharacter = errors.New("invalid character in base62 string")

	// ErrOverflow 當解碼結果超過 uint64 範圍時返回
	ErrOverflow = errors.New("decoded value exceeds uint64 range")
)

// charToValue 將 Base62 字符轉換為對應的數值（0-61）
// 使用 map 查找，時間複雜度 O(1)
var charToValue map[byte]uint64

func init() {
	// 初始化字符到數值的映射表
	charToValue = make(map[byte]uint64, base)
	for i, char := range base62Chars {
		charToValue[byte(char)] = uint64(i)
	}
}

// Encode 將 uint64 數字編碼為 Base62 字符串
//
// 算法原理：
//   - 類似於十進制轉二進制，不斷除以 62 取餘數
//   - 餘數對應的字符就是編碼結果的一位
//   - 從低位到高位構建，最後反轉
//
// 範例：
//   Encode(0)          → "0"
//   Encode(61)         → "z"
//   Encode(62)         → "10"
//   Encode(123456789)  → "8M0kX"
//
// 時間複雜度：O(log62(num))
func Encode(num uint64) string {
	// 特殊情況：0
	if num == 0 {
		return "0"
	}

	// 預估結果長度（避免多次內存分配）
	// log62(num) ≈ log10(num) / log10(62) ≈ log10(num) / 1.79
	estimatedLen := int(math.Log10(float64(num))/math.Log10(base)) + 1
	result := make([]byte, 0, estimatedLen)

	// 不斷除以 62，取餘數對應的字符
	for num > 0 {
		remainder := num % base
		result = append(result, base62Chars[remainder])
		num /= base
	}

	// 反轉字符串（因為我們是從低位到高位構建的）
	reverse(result)

	return string(result)
}

// Decode 將 Base62 字符串解碼為 uint64 數字
//
// 算法原理：
//   - 從高位到低位遍歷字符串
//   - 每個字符的值 × 62^(位置) 累加
//   - 類似於字符串 "123" → 1×100 + 2×10 + 3×1 = 123
//
// 範例：
//   Decode("0")      → 0, nil
//   Decode("z")      → 61, nil
//   Decode("10")     → 62, nil
//   Decode("8M0kX")  → 123456789, nil
//
// 時間複雜度：O(len(str))
func Decode(str string) (uint64, error) {
	if str == "" {
		return 0, nil
	}

	var result uint64
	strLen := len(str)

	// 從左到右遍歷每個字符
	for i, char := range str {
		// 查找字符對應的數值
		value, ok := charToValue[byte(char)]
		if !ok {
			return 0, ErrInvalidCharacter
		}

		// 計算該位的權重：62^(剩餘位數)
		power := strLen - i - 1
		weightedValue := value * pow62(uint64(power))

		// 檢查溢出
		if result > math.MaxUint64-weightedValue {
			return 0, ErrOverflow
		}

		result += weightedValue
	}

	return result, nil
}

// EncodeBytes 將字節數組編碼為 Base62（先轉換為大整數）
// 注意：這種方式會損失前導零信息，不適合加密場景
func EncodeBytes(data []byte) string {
	// 將字節數組視為大端序的大整數
	var num uint64
	for _, b := range data {
		num = num<<8 | uint64(b)
	}
	return Encode(num)
}

// MustDecode 類似 Decode，但遇到錯誤會 panic
// 僅在確保輸入正確的場景下使用（如測試）
func MustDecode(str string) uint64 {
	result, err := Decode(str)
	if err != nil {
		panic(err)
	}
	return result
}

// reverse 原地反轉字節切片
func reverse(s []byte) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// pow62 計算 62 的 n 次方
// 使用快速冪算法，時間複雜度 O(log n)
func pow62(n uint64) uint64 {
	if n == 0 {
		return 1
	}

	result := uint64(1)
	base := uint64(62)

	for n > 0 {
		if n&1 == 1 {
			result *= base
		}
		base *= base
		n >>= 1
	}

	return result
}

// IsValid 檢查字符串是否為有效的 Base62 編碼
func IsValid(str string) bool {
	for _, char := range str {
		if _, ok := charToValue[byte(char)]; !ok {
			return false
		}
	}
	return true
}

// Length 計算編碼給定數字所需的字符數
// 公式：ceil(log62(num + 1))
func Length(num uint64) int {
	if num == 0 {
		return 1
	}
	return int(math.Ceil(math.Log(float64(num+1)) / math.Log(base)))
}

// MaxValue 返回給定長度的 Base62 字符串能表示的最大值
// 公式：62^length - 1
// 例如：MaxValue(7) = 62^7 - 1 = 3,521,614,606,207
func MaxValue(length int) uint64 {
	if length <= 0 {
		return 0
	}
	return pow62(uint64(length)) - 1
}

// Pad 將編碼結果填充到指定長度（左填充 '0'）
// 用於生成固定長度的短碼
//
// 範例：
//   Pad(Encode(123), 7) → "000001Z"
func Pad(encoded string, targetLen int) string {
	if len(encoded) >= targetLen {
		return encoded
	}
	return strings.Repeat("0", targetLen-len(encoded)) + encoded
}
