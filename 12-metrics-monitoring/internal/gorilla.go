package internal

import (
	"bytes"
	"encoding/binary"
	"math"
)

// GorillaCompressor Gorilla 壓縮器
type GorillaCompressor struct {
	// Gorilla 壓縮使用 Delta-of-Delta 和 XOR 編碼
}

// CompressedData 壓縮後的數據
type CompressedData struct {
	TimestampsCompressed []byte
	ValuesCompressed     []byte
	OriginalCount        int
	CompressionRatio     float64
}

// NewGorillaCompressor 創建 Gorilla 壓縮器
func NewGorillaCompressor() *GorillaCompressor {
	return &GorillaCompressor{}
}

// Compress 壓縮時序數據
func (gc *GorillaCompressor) Compress(timestamps []int64, values []float64) *CompressedData {
	if len(timestamps) == 0 {
		return &CompressedData{}
	}

	timestampsCompressed := gc.compressTimestamps(timestamps)
	valuesCompressed := gc.compressValues(values)

	originalSize := len(timestamps)*8 + len(values)*8 // 8 bytes per int64/float64
	compressedSize := len(timestampsCompressed) + len(valuesCompressed)

	compressionRatio := float64(originalSize) / float64(compressedSize)

	return &CompressedData{
		TimestampsCompressed: timestampsCompressed,
		ValuesCompressed:     valuesCompressed,
		OriginalCount:        len(timestamps),
		CompressionRatio:     compressionRatio,
	}
}

// Decompress 解壓縮時序數據
func (gc *GorillaCompressor) Decompress(data *CompressedData) ([]int64, []float64) {
	timestamps := gc.decompressTimestamps(data.TimestampsCompressed, data.OriginalCount)
	values := gc.decompressValues(data.ValuesCompressed, data.OriginalCount)
	return timestamps, values
}

// compressTimestamps 壓縮時間戳（Delta-of-Delta 編碼）
func (gc *GorillaCompressor) compressTimestamps(timestamps []int64) []byte {
	if len(timestamps) == 0 {
		return []byte{}
	}

	buf := new(bytes.Buffer)

	// 寫入第一個時間戳（完整 64 位）
	binary.Write(buf, binary.BigEndian, timestamps[0])

	if len(timestamps) == 1 {
		return buf.Bytes()
	}

	// 寫入第一個 delta（32 位足夠）
	delta := timestamps[1] - timestamps[0]
	binary.Write(buf, binary.BigEndian, int32(delta))

	// Delta-of-Delta 編碼
	prevDelta := delta
	for i := 2; i < len(timestamps); i++ {
		currentDelta := timestamps[i] - timestamps[i-1]
		deltaOfDelta := currentDelta - prevDelta

		// 根據 delta-of-delta 的大小使用不同的編碼
		if deltaOfDelta == 0 {
			// 0: 寫入 1 位標誌 '0'
			buf.WriteByte(0)
		} else if deltaOfDelta >= -63 && deltaOfDelta <= 64 {
			// [-63, 64]: 寫入 '10' + 7 位數據
			buf.WriteByte(1)
			buf.WriteByte(byte(deltaOfDelta & 0x7F))
		} else if deltaOfDelta >= -255 && deltaOfDelta <= 256 {
			// [-255, 256]: 寫入 '110' + 9 位數據
			buf.WriteByte(2)
			binary.Write(buf, binary.BigEndian, int16(deltaOfDelta))
		} else if deltaOfDelta >= -2047 && deltaOfDelta <= 2048 {
			// [-2047, 2048]: 寫入 '1110' + 12 位數據
			buf.WriteByte(3)
			binary.Write(buf, binary.BigEndian, int16(deltaOfDelta))
		} else {
			// 其他: 寫入 '1111' + 32 位數據
			buf.WriteByte(4)
			binary.Write(buf, binary.BigEndian, int32(deltaOfDelta))
		}

		prevDelta = currentDelta
	}

	return buf.Bytes()
}

// decompressTimestamps 解壓縮時間戳
func (gc *GorillaCompressor) decompressTimestamps(data []byte, count int) []int64 {
	if len(data) == 0 {
		return []int64{}
	}

	buf := bytes.NewReader(data)
	timestamps := make([]int64, count)

	// 讀取第一個時間戳
	binary.Read(buf, binary.BigEndian, &timestamps[0])

	if count == 1 {
		return timestamps
	}

	// 讀取第一個 delta
	var firstDelta int32
	binary.Read(buf, binary.BigEndian, &firstDelta)
	timestamps[1] = timestamps[0] + int64(firstDelta)

	// 解壓 Delta-of-Delta
	prevDelta := int64(firstDelta)
	for i := 2; i < count && buf.Len() > 0; i++ {
		flag, _ := buf.ReadByte()

		var deltaOfDelta int64
		switch flag {
		case 0:
			deltaOfDelta = 0
		case 1:
			b, _ := buf.ReadByte()
			deltaOfDelta = int64(int8(b))
		case 2:
			var val int16
			binary.Read(buf, binary.BigEndian, &val)
			deltaOfDelta = int64(val)
		case 3:
			var val int16
			binary.Read(buf, binary.BigEndian, &val)
			deltaOfDelta = int64(val)
		case 4:
			var val int32
			binary.Read(buf, binary.BigEndian, &val)
			deltaOfDelta = int64(val)
		}

		currentDelta := prevDelta + deltaOfDelta
		timestamps[i] = timestamps[i-1] + currentDelta
		prevDelta = currentDelta
	}

	return timestamps
}

// compressValues 壓縮數值（XOR 編碼）
func (gc *GorillaCompressor) compressValues(values []float64) []byte {
	if len(values) == 0 {
		return []byte{}
	}

	buf := new(bytes.Buffer)

	// 寫入第一個值（完整 64 位浮點數）
	binary.Write(buf, binary.BigEndian, values[0])

	if len(values) == 1 {
		return buf.Bytes()
	}

	// XOR 編碼
	prevValue := math.Float64bits(values[0])
	prevLeadingZeros := 0
	prevTrailingZeros := 0

	for i := 1; i < len(values); i++ {
		currentValue := math.Float64bits(values[i])
		xor := prevValue ^ currentValue

		if xor == 0 {
			// 值相同，寫入 1 位標誌 '0'
			buf.WriteByte(0)
		} else {
			// 值不同，寫入 '1' + XOR 編碼
			leadingZeros := countLeadingZeros(xor)
			trailingZeros := countTrailingZeros(xor)

			if leadingZeros >= prevLeadingZeros && trailingZeros >= prevTrailingZeros {
				// 使用之前的 leading/trailing zeros
				// 寫入 '10' + 有效位
				buf.WriteByte(1)
				meaningfulBits := 64 - prevLeadingZeros - prevTrailingZeros
				binary.Write(buf, binary.BigEndian, uint64(xor>>uint(prevTrailingZeros))<<uint(64-meaningfulBits))
			} else {
				// 寫入新的 leading/trailing zeros
				// 寫入 '11' + 5 位 leading zeros + 6 位有效位長度 + 有效位
				buf.WriteByte(2)
				buf.WriteByte(byte(leadingZeros))
				meaningfulBits := 64 - leadingZeros - trailingZeros
				buf.WriteByte(byte(meaningfulBits))
				binary.Write(buf, binary.BigEndian, uint64(xor>>uint(trailingZeros))<<uint(64-meaningfulBits))

				prevLeadingZeros = leadingZeros
				prevTrailingZeros = trailingZeros
			}
		}

		prevValue = currentValue
	}

	return buf.Bytes()
}

// decompressValues 解壓縮數值
func (gc *GorillaCompressor) decompressValues(data []byte, count int) []float64 {
	if len(data) == 0 {
		return []float64{}
	}

	buf := bytes.NewReader(data)
	values := make([]float64, count)

	// 讀取第一個值
	var firstValue uint64
	binary.Read(buf, binary.BigEndian, &firstValue)
	values[0] = math.Float64frombits(firstValue)

	if count == 1 {
		return values
	}

	// 解壓 XOR 編碼
	prevValue := firstValue
	prevLeadingZeros := 0
	prevTrailingZeros := 0

	for i := 1; i < count && buf.Len() > 0; i++ {
		flag, _ := buf.ReadByte()

		if flag == 0 {
			// 值相同
			values[i] = math.Float64frombits(prevValue)
		} else if flag == 1 {
			// 使用之前的 leading/trailing zeros
			meaningfulBits := 64 - prevLeadingZeros - prevTrailingZeros
			var xorBits uint64
			binary.Read(buf, binary.BigEndian, &xorBits)
			xor := (xorBits >> uint(64-meaningfulBits)) << uint(prevTrailingZeros)
			currentValue := prevValue ^ xor
			values[i] = math.Float64frombits(currentValue)
			prevValue = currentValue
		} else {
			// 讀取新的 leading/trailing zeros
			leadingZeros, _ := buf.ReadByte()
			meaningfulBits, _ := buf.ReadByte()
			var xorBits uint64
			binary.Read(buf, binary.BigEndian, &xorBits)

			trailingZeros := 64 - int(leadingZeros) - int(meaningfulBits)
			xor := (xorBits >> uint(64-meaningfulBits)) << uint(trailingZeros)
			currentValue := prevValue ^ xor
			values[i] = math.Float64frombits(currentValue)

			prevValue = currentValue
			prevLeadingZeros = int(leadingZeros)
			prevTrailingZeros = trailingZeros
		}
	}

	return values
}

// countLeadingZeros 計算前導零數量
func countLeadingZeros(n uint64) int {
	if n == 0 {
		return 64
	}
	count := 0
	for i := 63; i >= 0; i-- {
		if (n & (1 << uint(i))) != 0 {
			break
		}
		count++
	}
	return count
}

// countTrailingZeros 計算後綴零數量
func countTrailingZeros(n uint64) int {
	if n == 0 {
		return 64
	}
	count := 0
	for i := 0; i < 64; i++ {
		if (n & (1 << uint(i))) != 0 {
			break
		}
		count++
	}
	return count
}

// GetCompressionStats 獲取壓縮統計
func (gc *GorillaCompressor) GetCompressionStats(timestamps []int64, values []float64) map[string]interface{} {
	compressed := gc.Compress(timestamps, values)

	originalSize := len(timestamps)*8 + len(values)*8
	compressedSize := len(compressed.TimestampsCompressed) + len(compressed.ValuesCompressed)

	return map[string]interface{}{
		"original_size_bytes":   originalSize,
		"compressed_size_bytes": compressedSize,
		"compression_ratio":     compressed.CompressionRatio,
		"space_saved_percent":   (1.0 - float64(compressedSize)/float64(originalSize)) * 100.0,
	}
}
