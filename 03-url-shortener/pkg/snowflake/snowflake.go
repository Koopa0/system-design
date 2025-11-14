// Package snowflake 實現了 Twitter Snowflake 分布式 ID 生成算法
//
// Snowflake ID 是一個 64 位的整數，結構如下：
//
//	1 bit    | 41 bit           | 10 bit     | 12 bit
//	符號位   | 時間戳(毫秒)      | 機器ID      | 序列號
//	0        | timestamp        | machine    | sequence
//
// 特點：
//   - 趨勢遞增：基於時間戳，大致有序（有利於資料庫索引）
//   - 分布式：每台機器獨立生成，無需協調
//   - 高效能：本地生成，無網絡開銷
//   - 唯一性：時間戳 + 機器 ID + 序列號保證全局唯一
//
// 容量分析：
//   - 時間戳 41 bit：可用約 69 年（從 epoch 開始）
//   - 機器 ID 10 bit：支持 1024 台機器
//   - 序列號 12 bit：每毫秒可生成 4096 個 ID
//
// 參考：
//   - https://github.com/twitter-archive/snowflake
//   - https://blog.twitter.com/engineering/en_us/a/2010/announcing-snowflake
package snowflake

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// epoch 是 Snowflake 的起始時間（2024-01-01 00:00:00 UTC）
	// 可以根據業務需要調整，epoch 越晚，可用時間越長
	epoch int64 = 1704067200000 // 2024-01-01 00:00:00 UTC 的毫秒時間戳

	// 位數分配
	timestampBits = 41 // 時間戳佔 41 bit
	machineBits   = 10 // 機器 ID 佔 10 bit
	sequenceBits  = 12 // 序列號佔 12 bit

	// 最大值計算（2^n - 1）
	maxMachineID = (1 << machineBits) - 1  // 1023
	maxSequence  = (1 << sequenceBits) - 1 // 4095

	// 位移量
	machineShift   = sequenceBits               // 12
	timestampShift = sequenceBits + machineBits // 22
)

var (
	// ErrInvalidMachineID 當機器 ID 超出範圍時返回
	ErrInvalidMachineID = errors.New("machine ID must be between 0 and 1023")

	// ErrClockMovedBackwards 當時鐘回撥時返回
	ErrClockMovedBackwards = errors.New("clock moved backwards, refusing to generate ID")

	// ErrSequenceOverflow 當同一毫秒內序列號用盡時返回（極少發生）
	ErrSequenceOverflow = errors.New("sequence overflow in current millisecond")
)

// Generator Snowflake ID 生成器
type Generator struct {
	mu            sync.Mutex // 保護並發訪問
	machineID     int64      // 機器 ID（0-1023）
	sequence      int64      // 當前序列號（0-4095）
	lastTimestamp int64      // 上次生成 ID 的時間戳（毫秒）
}

// NewGenerator 創建一個新的 Snowflake ID 生成器
//
// 參數：
//   - machineID: 機器 ID，範圍 0-1023
//
// 返回：
//   - Generator 實例
//   - 錯誤（如果 machineID 無效）
func NewGenerator(machineID int64) (*Generator, error) {
	if machineID < 0 || machineID > maxMachineID {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidMachineID, machineID)
	}

	return &Generator{
		machineID:     machineID,
		sequence:      0,
		lastTimestamp: 0,
	}, nil
}

// Generate 生成下一個 Snowflake ID
//
// 算法流程：
//  1. 獲取當前時間戳（毫秒）
//  2. 如果時間戳 < 上次時間戳 → 時鐘回撥，報錯
//  3. 如果時間戳 == 上次時間戳 → 序列號+1
//  4. 如果序列號溢出 → 等待下一毫秒
//  5. 如果時間戳 > 上次時間戳 → 重置序列號為 0
//  6. 組裝 ID：時間戳 << 22 | 機器ID << 12 | 序列號
//
// 併發安全：使用互斥鎖保護
func (g *Generator) Generate() (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 獲取當前時間戳（毫秒）
	timestamp := currentMilliseconds()

	// 時鐘回撥檢測
	if timestamp < g.lastTimestamp {
		// 時鐘回撥了，這是一個嚴重問題
		// 生產環境應該：
		// 1. 記錄日誌告警
		// 2. 拒絕生成 ID（避免重複）
		// 3. 等待時鐘恢復（可選）
		return 0, fmt.Errorf("%w: last=%d, current=%d",
			ErrClockMovedBackwards, g.lastTimestamp, timestamp)
	}

	// 同一毫秒內
	if timestamp == g.lastTimestamp {
		// 序列號自增
		g.sequence = (g.sequence + 1) & maxSequence

		// 序列號溢出（同一毫秒內生成了 4096 個 ID）
		if g.sequence == 0 {
			// 等待下一毫秒
			timestamp = g.waitNextMillisecond(g.lastTimestamp)
		}
	} else {
		// 新的毫秒，重置序列號
		g.sequence = 0
	}

	// 更新最後時間戳
	g.lastTimestamp = timestamp

	// 組裝 Snowflake ID
	//
	// 結構：
	//   [0|時間戳(41bit)|機器ID(10bit)|序列號(12bit)]
	//
	// 示例（假設 timestamp=100, machineID=5, sequence=7）：
	//   時間戳部分：100 << 22 = 419430400
	//   機器ID部分：5 << 12 = 20480
	//   序列號部分：7
	//   最終ID：419430400 | 20480 | 7 = 419450887
	id := ((timestamp - epoch) << timestampShift) |
		(g.machineID << machineShift) |
		g.sequence

	return id, nil
}

// MustGenerate 類似 Generate，但遇到錯誤會 panic
// 僅在測試或確保不會出錯的場景使用
func (g *Generator) MustGenerate() int64 {
	id, err := g.Generate()
	if err != nil {
		panic(err)
	}
	return id
}

// waitNextMillisecond 等待直到下一毫秒
// waitNextMillisecond 等待下一毫秒
//
// 系統設計考量：
//   - 為什麼需要等待？
//     → 同一毫秒內序列號溢出（生成了 4096 個 ID）
//     → 必須等到下一毫秒才能繼續生成
//   - 性能優化：
//     → 添加短暫 sleep（避免 CPU 空轉）
//     → 10 微秒足夠（1 毫秒 = 1000 微秒）
func (g *Generator) waitNextMillisecond(lastTimestamp int64) int64 {
	timestamp := currentMilliseconds()
	for timestamp <= lastTimestamp {
		// 短暫休眠，避免 CPU 空轉（busy-wait）
		//
		// 為什麼休眠 10 微秒？
		//   - 太短：仍然浪費 CPU
		//   - 太長：增加延遲
		//   - 10μs：平衡 CPU 使用與響應時間
		time.Sleep(10 * time.Microsecond)
		timestamp = currentMilliseconds()
	}
	return timestamp
}

// currentMilliseconds 獲取當前毫秒時間戳
func currentMilliseconds() int64 {
	return time.Now().UnixMilli()
}

// ParseID 解析 Snowflake ID，提取時間戳、機器 ID、序列號
//
// 返回值：
//   - timestamp: 生成 ID 時的毫秒時間戳
//   - machineID: 機器 ID
//   - sequence: 序列號
func ParseID(id int64) (timestamp int64, machineID int64, sequence int64) {
	// 提取序列號（低 12 bit）
	sequence = id & maxSequence

	// 提取機器 ID（中間 10 bit）
	machineID = (id >> machineShift) & maxMachineID

	// 提取時間戳（高 41 bit）
	timestamp = (id >> timestampShift) + epoch

	return
}

// ParseIDToTime 將 Snowflake ID 轉換為生成時間
func ParseIDToTime(id int64) time.Time {
	timestamp, _, _ := ParseID(id)
	return time.UnixMilli(timestamp)
}

// Info 返回 Snowflake ID 的詳細信息（用於調試）
type Info struct {
	ID        int64     `json:"id"`
	Timestamp int64     `json:"timestamp"`
	Time      time.Time `json:"time"`
	MachineID int64     `json:"machine_id"`
	Sequence  int64     `json:"sequence"`
}

// GetInfo 獲取 Snowflake ID 的詳細信息
func GetInfo(id int64) Info {
	timestamp, machineID, sequence := ParseID(id)
	return Info{
		ID:        id,
		Timestamp: timestamp,
		Time:      time.UnixMilli(timestamp),
		MachineID: machineID,
		Sequence:  sequence,
	}
}

// String 返回 Snowflake ID 信息的字符串表示
func (info Info) String() string {
	return fmt.Sprintf("ID=%d, Time=%s, Machine=%d, Seq=%d",
		info.ID, info.Time.Format(time.RFC3339), info.MachineID, info.Sequence)
}

// MaxIDsPerMillisecond 返回每毫秒最多可生成的 ID 數量
func MaxIDsPerMillisecond() int {
	return maxSequence + 1 // 4096
}

// MaxMachines 返回最多支持的機器數量
func MaxMachines() int {
	return maxMachineID + 1 // 1024
}

// LifeTime 返回從 epoch 開始可用的最大時間（年）
func LifeTime() int {
	maxTimestamp := (int64(1) << timestampBits) - 1
	milliseconds := maxTimestamp
	years := milliseconds / 1000 / 60 / 60 / 24 / 365
	return int(years) // 約 69 年
}
