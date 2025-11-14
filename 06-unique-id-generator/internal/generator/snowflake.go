// Package generator 實現多種分布式 ID 生成算法
//
// 本包提供：
//   - Snowflake: Twitter 的 64-bit ID 生成算法
//   - 時鐘回撥處理
//   - ID 解析工具
//
// 教學重點：
//   - 位運算技巧
//   - 時鐘依賴問題
//   - 並發安全
package generator

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// epoch 是 Snowflake 的起始時間（2024-01-01 00:00:00 UTC）
	epoch int64 = 1704067200000 // 毫秒時間戳

	// 位數分配
	timestampBits = 41 // 時間戳佔 41 bit（可用約 69 年）
	machineBits   = 10 // 機器 ID 佔 10 bit（支持 1024 台機器）
	sequenceBits  = 12 // 序列號佔 12 bit（每毫秒 4096 個 ID）

	// 最大值
	maxMachineID = (1 << machineBits) - 1  // 1023
	maxSequence  = (1 << sequenceBits) - 1 // 4095

	// 位移量
	machineShift   = sequenceBits               // 12
	timestampShift = sequenceBits + machineBits // 22

	// 時鐘回撥容忍度（毫秒）
	defaultMaxBackwardMS = 5000 // 默認容忍 5 秒回撥
)

var (
	// ErrInvalidMachineID 機器 ID 超出範圍
	ErrInvalidMachineID = errors.New("machine ID must be between 0 and 1023")

	// ErrClockMovedBackwards 時鐘回撥過多
	ErrClockMovedBackwards = errors.New("clock moved backwards too much")
)

// Snowflake 實現 Twitter Snowflake 算法
//
// 結構：
//   64-bit = [1-bit 符號][41-bit 時間戳][10-bit 機器ID][12-bit 序列號]
//
// 系統設計考量：
//   1. 為何 64-bit？
//      - 資料庫友好：BIGINT 標準類型
//      - 緊湊高效：比 UUID 小 50%
//      - 有序性：時間戳遞增，B-Tree 索引友好
//
//   2. 為何時間戳在高位？
//      - 保證 ID 趨勢遞增
//      - 範圍查詢友好：按時間範圍查詢等價於按 ID 範圍
//
//   3. 為何需要序列號？
//      - 同一毫秒內可能生成多個 ID
//      - 12-bit 序列號 = 每毫秒 4096 個 ID
//      - 吞吐量：4096 × 1000 = 400 萬 ID/秒
type Snowflake struct {
	mu               sync.Mutex // 保護並發訪問
	machineID        int64      // 機器 ID（0-1023）
	sequence         int64      // 當前序列號（0-4095）
	lastTimestamp    int64      // 上次生成 ID 的時間戳（毫秒）
	maxBackwardMS    int64      // 最大容忍回撥（毫秒）
	clockBackCounter int64      // 時鐘回撥計數器（監控用）
}

// Config Snowflake 配置
type Config struct {
	MachineID     int64 // 機器 ID（0-1023）
	MaxBackwardMS int64 // 最大容忍時鐘回撥（毫秒），0 表示使用默認值
}

// NewSnowflake 創建 Snowflake ID 生成器
//
// 參數：
//   machineID: 機器 ID，範圍 0-1023
//
// 教學重點：
//   - 機器 ID 分配策略（手動配置 vs ZooKeeper 自動分配）
//   - 如何保證不同機器的 ID 不衝突
func NewSnowflake(machineID int64) (*Snowflake, error) {
	return NewSnowflakeWithConfig(&Config{
		MachineID:     machineID,
		MaxBackwardMS: defaultMaxBackwardMS,
	})
}

// NewSnowflakeWithConfig 使用配置創建生成器
func NewSnowflakeWithConfig(config *Config) (*Snowflake, error) {
	if config.MachineID < 0 || config.MachineID > maxMachineID {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidMachineID, config.MachineID)
	}

	maxBackward := config.MaxBackwardMS
	if maxBackward == 0 {
		maxBackward = defaultMaxBackwardMS
	}

	return &Snowflake{
		machineID:     config.MachineID,
		sequence:      0,
		lastTimestamp: 0,
		maxBackwardMS: maxBackward,
	}, nil
}

// Generate 生成下一個 Snowflake ID
//
// 算法流程：
//   1. 獲取當前時間戳（毫秒）
//   2. 處理時鐘回撥（關鍵設計點）
//   3. 處理同一毫秒內的序列號
//   4. 組裝 64-bit ID
//
// 時鐘回撥處理（系統設計重點）：
//   問題：NTP 校正可能導致時鐘往回調
//   影響：可能產生重複 ID 或破壞有序性
//
//   處理策略：
//   - 小回撥（< maxBackwardMS）：
//     → 使用上次時間戳，記錄告警
//     → 序列號繼續遞增（可能更快耗盡）
//     → 優勢：服務不中斷
//
//   - 大回撥（>= maxBackwardMS）：
//     → 拒絕生成，返回錯誤
//     → 優勢：避免 ID 重複
//     → 權衡：服務短暫不可用
//
// 並發安全：使用互斥鎖保護
func (s *Snowflake) Generate() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	timestamp := currentMillis()

	// 時鐘回撥檢測
	if timestamp < s.lastTimestamp {
		offset := s.lastTimestamp - timestamp

		// 小回撥：容忍，記錄監控
		if offset <= s.maxBackwardMS {
			// 記錄時鐘回撥事件（生產環境應發送到監控系統）
			s.clockBackCounter++

			// 使用上次時間戳，繼續生成
			timestamp = s.lastTimestamp

			// 注意：序列號會更快耗盡，但通常不會有問題
			// 最壞情況：同一毫秒內生成超過 4096 個 ID
			// 解決：等待下一毫秒（在下面的邏輯中處理）
		} else {
			// 大回撥：拒絕生成
			// 生產環境應：
			//   1. 記錄嚴重告警
			//   2. 通知運維人員
			//   3. 檢查 NTP 配置
			return 0, fmt.Errorf("%w: offset=%dms, max=%dms",
				ErrClockMovedBackwards, offset, s.maxBackwardMS)
		}
	}

	// 同一毫秒內
	if timestamp == s.lastTimestamp {
		// 序列號自增
		s.sequence = (s.sequence + 1) & maxSequence

		// 序列號溢出（同一毫秒內生成了 4096 個 ID）
		if s.sequence == 0 {
			// 等待下一毫秒
			timestamp = s.waitNextMillis(s.lastTimestamp)
		}
	} else {
		// 新的毫秒，重置序列號
		s.sequence = 0
	}

	// 更新最後時間戳
	s.lastTimestamp = timestamp

	// 組裝 Snowflake ID
	//
	// 結構：
	//   [0|時間戳(41bit)|機器ID(10bit)|序列號(12bit)]
	//
	// 位運算：
	//   時間戳部分：(timestamp - epoch) << 22
	//   機器ID部分：machineID << 12
	//   序列號部分：sequence（已在低 12 位）
	//   最終ID：三者按位或（|）
	//
	// 範例：
	//   時間戳 = 100（從 epoch 開始）
	//   機器ID = 5
	//   序列號 = 7
	//   ID = (100 << 22) | (5 << 12) | 7
	//      = 419430400 | 20480 | 7
	//      = 419450887
	id := ((timestamp - epoch) << timestampShift) |
		(s.machineID << machineShift) |
		s.sequence

	return id, nil
}

// waitNextMillis 等待直到下一毫秒
//
// 為何需要等待？
//   - 同一毫秒內序列號溢出（生成了 4096 個 ID）
//   - 必須等到下一毫秒才能繼續
//
// 性能優化：
//   - 短暫 sleep 避免 CPU 空轉
//   - 10 微秒平衡延遲與 CPU 使用
func (s *Snowflake) waitNextMillis(lastTimestamp int64) int64 {
	timestamp := currentMillis()
	for timestamp <= lastTimestamp {
		time.Sleep(10 * time.Microsecond)
		timestamp = currentMillis()
	}
	return timestamp
}

// GetClockBackCounter 返回時鐘回撥計數器（監控用）
func (s *Snowflake) GetClockBackCounter() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clockBackCounter
}

// currentMillis 獲取當前毫秒時間戳
func currentMillis() int64 {
	return time.Now().UnixMilli()
}

// SnowflakeInfo Snowflake ID 解析信息
type SnowflakeInfo struct {
	ID        int64     `json:"id"`
	Timestamp int64     `json:"timestamp"` // 毫秒時間戳
	Time      time.Time `json:"time"`
	MachineID int64     `json:"machine_id"`
	Sequence  int64     `json:"sequence"`
}

// ParseSnowflakeID 解析 Snowflake ID
//
// 教學重點：
//   - 位運算的逆操作
//   - 如何從 64-bit 整數中提取各個部分
//
// 應用場景：
//   - 調試：查看 ID 的生成時間和機器
//   - 監控：分析 ID 分布
//   - 排序：按時間戳排序
func ParseSnowflakeID(id int64) SnowflakeInfo {
	// 提取序列號（低 12 bit）
	sequence := id & maxSequence

	// 提取機器 ID（中間 10 bit）
	machineID := (id >> machineShift) & maxMachineID

	// 提取時間戳（高 41 bit）
	timestamp := (id >> timestampShift) + epoch

	return SnowflakeInfo{
		ID:        id,
		Timestamp: timestamp,
		Time:      time.UnixMilli(timestamp),
		MachineID: machineID,
		Sequence:  sequence,
	}
}

// String 返回 Snowflake ID 信息的字符串表示
func (info SnowflakeInfo) String() string {
	return fmt.Sprintf("ID=%d, Time=%s, Machine=%d, Seq=%d",
		info.ID,
		info.Time.Format(time.RFC3339),
		info.MachineID,
		info.Sequence)
}

// Capacity 返回 Snowflake 的容量信息（教學用）
type Capacity struct {
	MaxIDsPerMillis int   `json:"max_ids_per_millis"` // 每毫秒最大 ID 數
	MaxMachines     int   `json:"max_machines"`       // 最大機器數
	LifeTimeYears   int   `json:"lifetime_years"`     // 可用年數
	TotalCapacity   int64 `json:"total_capacity"`     // 總 ID 容量
}

// GetCapacity 返回容量信息
func GetCapacity() Capacity {
	maxTimestamp := (int64(1) << timestampBits) - 1
	years := maxTimestamp / 1000 / 60 / 60 / 24 / 365

	return Capacity{
		MaxIDsPerMillis: maxSequence + 1,
		MaxMachines:     maxMachineID + 1,
		LifeTimeYears:   int(years),
		TotalCapacity:   (1 << 63) - 1, // 2^63 - 1（int64 最大值）
	}
}
