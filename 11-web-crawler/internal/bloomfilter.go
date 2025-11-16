package internal

import (
	"hash/fnv"
	"math"
)

// BloomFilter 布隆過濾器（概率型去重）
type BloomFilter struct {
	bitArray  []bool
	size      uint
	hashCount int
}

// NewBloomFilter 創建布隆過濾器
// expectedElements: 預期元素數量
// falsePositiveRate: 可接受的誤判率（如 0.01 = 1%）
func NewBloomFilter(expectedElements int, falsePositiveRate float64) *BloomFilter {
	size := optimalSize(expectedElements, falsePositiveRate)
	hashCount := optimalHashCount(size, expectedElements)

	return &BloomFilter{
		bitArray:  make([]bool, size),
		size:      uint(size),
		hashCount: hashCount,
	}
}

// Add 添加元素
func (bf *BloomFilter) Add(item string) {
	for i := 0; i < bf.hashCount; i++ {
		hash := bf.hash(item, uint(i))
		bf.bitArray[hash] = true
	}
}

// Contains 檢查元素是否可能存在
// 返回 true: 可能存在（有誤判）
// 返回 false: 一定不存在
func (bf *BloomFilter) Contains(item string) bool {
	for i := 0; i < bf.hashCount; i++ {
		hash := bf.hash(item, uint(i))
		if !bf.bitArray[hash] {
			return false // 一定不存在
		}
	}
	return true // 可能存在
}

// hash 哈希函數（使用 FNV-1a + 種子）
func (bf *BloomFilter) hash(item string, seed uint) uint {
	h := fnv.New64a()
	h.Write([]byte(item))
	hash := h.Sum64() + uint64(seed)*0x9e3779b97f4a7c15 // 魔數（黃金比例）
	return uint(hash % uint64(bf.size))
}

// optimalSize 計算最佳 bit 數組大小
// m = -(n * ln(p)) / (ln(2)^2)
func optimalSize(n int, p float64) int {
	m := -(float64(n) * math.Log(p)) / math.Pow(math.Log(2), 2)
	return int(math.Ceil(m))
}

// optimalHashCount 計算最佳哈希函數數量
// k = (m / n) * ln(2)
func optimalHashCount(m, n int) int {
	k := (float64(m) / float64(n)) * math.Log(2)
	return int(math.Ceil(k))
}

// Size 返回 bit 數組大小
func (bf *BloomFilter) Size() uint {
	return bf.size
}

// HashCount 返回哈希函數數量
func (bf *BloomFilter) HashCount() int {
	return bf.hashCount
}

// EstimatedFalsePositiveRate 估算當前誤判率
func (bf *BloomFilter) EstimatedFalsePositiveRate(insertedElements int) float64 {
	// p ≈ (1 - e^(-kn/m))^k
	exponent := -float64(bf.hashCount*insertedElements) / float64(bf.size)
	return math.Pow(1-math.Exp(exponent), float64(bf.hashCount))
}
