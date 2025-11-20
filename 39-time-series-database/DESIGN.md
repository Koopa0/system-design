# Chapter 39: 時序資料庫 (Time-Series Database)

> 使用蘇格拉底方法教學：透過四位工程師的對話，深入理解時序資料庫的設計與實作

## 角色介紹

- **Emma**: 資深資料庫架構師，專精於時序資料庫
- **David**: 後端工程師，負責監控系統開發
- **Sarah**: DevOps 工程師，管理 Prometheus 和 Grafana
- **Michael**: 資料工程師，處理 IoT 裝置資料

---

## Act 1: 為什麼需要時序資料庫？

**場景**：監控系統會議室，螢幕上顯示著 Grafana 儀表板

**David**: Emma，我們的監控系統遇到效能問題。我們用 MySQL 儲存所有伺服器的 CPU、記憶體、磁碟 metrics，每秒寫入 10 萬筆資料，查詢變得很慢。

**Emma**: 這是典型的時序資料場景。讓我問你：這些 metrics 資料有什麼特性？

**David**: 嗯...每筆資料都有時間戳記，而且是連續產生的。我們通常查詢最近一小時或一天的資料。

**Emma**: 還有呢？你們會更新舊資料嗎？

**David**: 不會。一旦寫入就不會修改。我們只需要查詢和聚合。

**Emma**: 完美！這就是**時序資料**（Time-Series Data）的特性：

```
時序資料的特性：
1. 時間戳記：每筆資料都有時間標記
2. 只寫入（Append-only）：不會更新或刪除單筆資料
3. 連續性：資料按時間順序產生
4. 大量寫入：每秒數千到數百萬筆
5. 聚合查詢：通常查詢一段時間範圍的平均值、最大值等
6. 過期刪除：舊資料會批次刪除（如保留 30 天）
```

**Sarah**: 所以一般的關聯式資料庫不適合這種場景？

**Emma**: 讓我們比較一下：

```
MySQL 儲存 metrics：
CREATE TABLE metrics (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    metric_name VARCHAR(255),
    host VARCHAR(255),
    value DOUBLE,
    timestamp BIGINT,
    INDEX idx_time (timestamp),
    INDEX idx_metric (metric_name, host, timestamp)
);

問題：
1. 每筆資料都需要主鍵（浪費空間）
2. B-Tree 索引不適合時序資料（大量隨機寫入導致碎片化）
3. 無法有效壓縮（每行獨立儲存）
4. 範圍查詢需要掃描大量索引
5. 刪除舊資料很慢（逐行刪除）

假設資料量：
- 1,000 台伺服器
- 每台 100 個 metrics
- 每 10 秒採集一次
- 寫入速率：1,000 × 100 ÷ 10 = 10,000 筆/秒
- 一天資料量：10,000 × 86,400 = 8.64 億筆
- 儲存空間（假設每筆 100 bytes）：86.4 GB/天

一個月：2.59 TB！
```

**Michael**: 天啊，我們的 IoT 系統有 10 萬個感測器，每秒採集一次，那就是 10 萬筆/秒！

**Emma**: 這就是為什麼我們需要專門設計的時序資料庫。讓我介紹兩個著名的時序資料庫：

### InfluxDB vs Prometheus

```
┌─────────────────────────────────────────────────────────────┐
│                        InfluxDB                             │
├─────────────────────────────────────────────────────────────┤
│ 設計理念：通用型時序資料庫                                   │
│ 資料模型：Measurement + Tags + Fields + Timestamp          │
│ 儲存引擎：TSM (Time-Structured Merge Tree)                  │
│ 壓縮：Delta encoding + Gorilla (時間戳) + Run-length       │
│ 查詢語言：InfluxQL / Flux                                   │
│ 寫入模式：Push（客戶端推送資料）                            │
│ 適用場景：IoT、業務指標、監控                               │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                       Prometheus                            │
├─────────────────────────────────────────────────────────────┤
│ 設計理念：監控與告警系統                                     │
│ 資料模型：Metric Name + Labels + Value + Timestamp         │
│ 儲存引擎：自訂格式（類似 LevelDB）                           │
│ 壓縮：Gorilla + XOR 壓縮                                     │
│ 查詢語言：PromQL                                             │
│ 寫入模式：Pull（主動抓取 metrics）                          │
│ 適用場景：Kubernetes 監控、微服務監控                        │
└─────────────────────────────────────────────────────────────┘
```

**David**: InfluxDB 和 Prometheus 聽起來很像，主要差別是什麼？

**Emma**: 主要差別在於**資料採集模式**：

```
Push 模式（InfluxDB）：
Client → InfluxDB

優點：
- 客戶端控制採集頻率
- 適合短生命週期的任務（如 Lambda function）
- 可以批次發送

缺點：
- 需要客戶端配置
- 網路故障時資料可能丟失

───────────────────────────────────────────────

Pull 模式（Prometheus）：
Prometheus → Targets (每個 target 暴露 HTTP endpoint)

優點：
- 集中配置，容易管理
- 可以檢測 target 是否存活
- 自動服務發現（Kubernetes）

缺點：
- 不適合短生命週期任務
- 需要 target 暴露 HTTP endpoint
```

**Sarah**: 我們用 Prometheus 監控 Kubernetes，它是怎麼知道要抓取哪些 Pod 的？

**Emma**: Prometheus 有**服務發現**機制，可以自動發現 Kubernetes 中的 Pod：

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'kubernetes-pods'
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: true
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
        action: replace
        target_label: __metrics_path__
        regex: (.+)
      - source_labels: [__address__, __meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        regex: ([^:]+)(?::\d+)?;(\d+)
        replacement: $1:$2
        target_label: __address__
```

這個配置會：
1. 發現所有帶有 `prometheus.io/scrape: "true"` annotation 的 Pod
2. 從 annotation 讀取 metrics path 和 port
3. 每隔一段時間（預設 15 秒）抓取 metrics

---

## Act 2: 時序資料的儲存格式

**場景**：Emma 在白板上畫出時序資料的儲存結構

**Emma**: 讓我們深入理解時序資料庫如何儲存資料。先看看資料模型：

### InfluxDB 資料模型

```
資料範例：

cpu_usage,host=server01,region=us-east value=45.2 1609459200000000000
cpu_usage,host=server01,region=us-east value=48.1 1609459210000000000
cpu_usage,host=server02,region=us-west value=62.3 1609459200000000000

結構解析：
┌─────────────┬──────────────────────────┬─────────┬────────────────────┐
│ Measurement │ Tags                     │ Fields  │ Timestamp          │
├─────────────┼──────────────────────────┼─────────┼────────────────────┤
│ cpu_usage   │ host=server01,           │ value=  │ 1609459200000000000│
│             │ region=us-east           │ 45.2    │ (nanoseconds)      │
└─────────────┴──────────────────────────┴─────────┴────────────────────┘

- Measurement: 類似 SQL 的 table name（如 cpu_usage、memory_usage）
- Tags: 索引欄位，用於過濾和分組（如 host、region）
- Fields: 實際的測量值（如 value=45.2）
- Timestamp: 奈秒精度的時間戳記
```

**David**: Tags 和 Fields 有什麼差別？

**Emma**: 這是關鍵問題！

```
Tags（標籤）：
- 會建立索引
- 用於 WHERE 子句過濾
- 用於 GROUP BY 分組
- 通常是低基數（cardinality）的字串（如 host、region、env）
- 儲存在倒排索引中

Fields（欄位）：
- 不會建立索引
- 實際的測量值
- 通常是數值型態
- 儲存在時間序列資料中

範例查詢：
SELECT mean(value)          -- Field
FROM cpu_usage              -- Measurement
WHERE host = 'server01'     -- Tag
  AND time > now() - 1h
GROUP BY region             -- Tag
```

**Sarah**: 為什麼要區分 Tags 和 Fields？

**Emma**: 因為**基數爆炸**問題！讓我舉個例子：

```
❌ 錯誤設計：把高基數資料當作 Tag

request_count,user_id=12345,url=/api/users/12345 value=1

問題：
- 如果有 100 萬個使用者
- 每個使用者訪問 1000 個不同的 URL
- 會產生 100 萬 × 1000 = 10 億個 series！

每個 series 需要：
- 索引條目：~100 bytes
- 總索引大小：10 億 × 100 bytes = 100 GB

記憶體不足！

───────────────────────────────────────────────

✅ 正確設計：高基數資料放在 Field

request_count,endpoint=/api/users,method=GET user_id="12345",value=1

這樣只有：
- endpoint × method = 100 × 5 = 500 個 series
- 索引大小：500 × 100 bytes = 50 KB
```

**Michael**: 所以設計 schema 時，要仔細選擇哪些是 Tag、哪些是 Field？

**Emma**: 完全正確！這是時序資料庫效能的關鍵。

### Series 的概念

**Emma**: 在時序資料庫中，**Series**（序列）是核心概念：

```
Series = Measurement + Tags 的唯一組合

範例：
cpu_usage,host=server01,region=us-east  <- Series 1
cpu_usage,host=server01,region=us-west  <- Series 2
cpu_usage,host=server02,region=us-east  <- Series 3

每個 Series 包含一系列的時間點資料：
Series 1:
  1609459200 → 45.2
  1609459210 → 48.1
  1609459220 → 46.8
  1609459230 → 50.3
  ...

資料在磁碟上的組織：
┌─────────────────────────────────────────┐
│ Series Index (倒排索引)                  │
├─────────────────────────────────────────┤
│ host=server01 → [Series 1, Series 2]    │
│ host=server02 → [Series 3]              │
│ region=us-east → [Series 1, Series 3]   │
│ region=us-west → [Series 2]             │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│ Time-Series Data (時間序列資料)         │
├─────────────────────────────────────────┤
│ Series 1: [1609459200→45.2, 1609459210→48.1, ...]│
│ Series 2: [1609459200→62.3, 1609459210→65.1, ...]│
│ Series 3: [1609459200→38.7, 1609459210→40.2, ...]│
└─────────────────────────────────────────┘
```

---

## Act 3: 壓縮技術 - Gorilla 演算法

**場景**：討論如何減少儲存空間

**David**: 時序資料量這麼大，有什麼方法可以壓縮嗎？

**Emma**: 這是時序資料庫的核心技術！讓我介紹 Facebook 的 **Gorilla 壓縮演算法**。

### 時間戳壓縮：Delta-of-Delta 編碼

**Emma**: 首先，時間戳通常是等間隔的，我們可以利用這個特性：

```
原始時間戳（每 10 秒一筆）：
1609459200
1609459210
1609459220
1609459230
1609459240

未壓縮：5 × 64 bits = 320 bits

───────────────────────────────────────────────

Delta 編碼（儲存差值）：
1609459200        (base, 64 bits)
      +10         (delta, 7 bits)
      +10         (delta, 7 bits)
      +10         (delta, 7 bits)
      +10         (delta, 7 bits)

總大小：64 + 4 × 7 = 92 bits
壓縮率：92 / 320 = 28.75%

───────────────────────────────────────────────

Delta-of-Delta 編碼（儲存差值的差值）：
1609459200        (base, 64 bits)
      +10         (delta, 7 bits)
        0         (delta-of-delta, 1 bit: '0' 表示相同)
        0         (1 bit)
        0         (1 bit)

總大小：64 + 7 + 3 × 1 = 74 bits
壓縮率：74 / 320 = 23.13%
```

**Sarah**: 如果採集間隔不規則呢？

**Emma**: 好問題！Gorilla 使用可變長度編碼：

```go
// Delta-of-Delta 編碼
func encodeDeltaOfDelta(dod int64) []byte {
    if dod == 0 {
        // 0: 用 1 bit 表示
        return []byte{0} // '0'
    } else if dod >= -63 && dod <= 64 {
        // -63 到 64: 用 2 + 7 = 9 bits 表示
        // '10' + 7-bit value
        return encodeBits('10', dod, 7)
    } else if dod >= -255 && dod <= 256 {
        // -255 到 256: 用 3 + 9 = 12 bits 表示
        // '110' + 9-bit value
        return encodeBits('110', dod, 9)
    } else if dod >= -2047 && dod <= 2048 {
        // -2047 到 2048: 用 4 + 12 = 16 bits 表示
        // '1110' + 12-bit value
        return encodeBits('1110', dod, 12)
    } else {
        // 其他: 用 4 + 32 = 36 bits 表示
        // '1111' + 32-bit value
        return encodeBits('1111', dod, 32)
    }
}

範例：
時間戳序列：
T0: 1609459200
T1: 1609459210  (delta = 10, dod = 10)
T2: 1609459220  (delta = 10, dod = 0)
T3: 1609459230  (delta = 10, dod = 0)
T4: 1609459245  (delta = 15, dod = 5)

編碼結果：
T0: [64 bits] (base timestamp)
T1: [10] (9 bits: '10' + 7-bit value)
T2: [0] (1 bit)
T3: [0] (1 bit)
T4: [10] (9 bits: '10' + 7-bit value for dod=5)

總大小：64 + 9 + 1 + 1 + 9 = 84 bits
vs 原始：5 × 64 = 320 bits
壓縮率：26.25%
```

### 數值壓縮：XOR 編碼

**Emma**: 對於浮點數值，Gorilla 使用 XOR 壓縮：

```
觀察：連續的 metrics 值通常變化不大

範例（CPU 使用率）：
V0: 45.2  (binary: 0100 0000 0010 0110 1001 1001 1001 1010)
V1: 48.1  (binary: 0100 0000 0100 0001 1001 1001 1001 1010)
V2: 46.8  (binary: 0100 0000 0011 0110 1001 1001 1001 1010)

XOR 壓縮原理：
V0 XOR V1 = 0000 0000 0110 0111 0000 0000 0000 0000
            ^^^^^^^^         ^^^^             ^^^^
            前導 0           中間非 0          尾隨 0

編碼：
- 如果 XOR 結果為 0（值相同）：用 1 bit '0' 表示
- 否則用 '1' + 前導 0 個數 + 有效位元長度 + 有效位元

V0: [64 bits] (base value)
V1: [1] + [5 bits: 前導 0 個數=8] + [6 bits: 長度=10] + [10 bits: 有效位元]
    = 22 bits

V2: [1] + [5 bits: 8] + [6 bits: 10] + [10 bits]
    = 22 bits

總大小：64 + 22 + 22 = 108 bits
vs 原始：3 × 64 = 192 bits
壓縮率：56.25%
```

**實作程式碼**：

```go
// internal/compression/gorilla.go
package compression

import (
    "math"
)

type GorillaCompressor struct {
    prevTimestamp      int64
    prevDelta          int64
    prevValue          float64
    prevLeadingZeros   int
    prevTrailingZeros  int

    writer *BitWriter
}

func NewGorillaCompressor() *GorillaCompressor {
    return &GorillaCompressor{
        writer: NewBitWriter(),
    }
}

// 壓縮時間戳
func (gc *GorillaCompressor) CompressTimestamp(timestamp int64) {
    if gc.prevTimestamp == 0 {
        // 第一個時間戳：直接寫入 64 bits
        gc.writer.WriteBits(uint64(timestamp), 64)
        gc.prevTimestamp = timestamp
        return
    }

    // 計算 delta
    delta := timestamp - gc.prevTimestamp

    if gc.prevDelta == 0 {
        // 第二個時間戳：寫入 delta
        gc.writer.WriteBits(uint64(delta), 64)
        gc.prevDelta = delta
        gc.prevTimestamp = timestamp
        return
    }

    // 計算 delta-of-delta
    dod := delta - gc.prevDelta

    // 使用可變長度編碼
    if dod == 0 {
        gc.writer.WriteBits(0, 1) // '0'
    } else if dod >= -63 && dod <= 64 {
        gc.writer.WriteBits(2, 2) // '10'
        gc.writer.WriteBits(uint64(dod), 7)
    } else if dod >= -255 && dod <= 256 {
        gc.writer.WriteBits(6, 3) // '110'
        gc.writer.WriteBits(uint64(dod), 9)
    } else if dod >= -2047 && dod <= 2048 {
        gc.writer.WriteBits(14, 4) // '1110'
        gc.writer.WriteBits(uint64(dod), 12)
    } else {
        gc.writer.WriteBits(15, 4) // '1111'
        gc.writer.WriteBits(uint64(dod), 32)
    }

    gc.prevDelta = delta
    gc.prevTimestamp = timestamp
}

// 壓縮浮點數值
func (gc *GorillaCompressor) CompressValue(value float64) {
    if gc.prevValue == 0 {
        // 第一個值：直接寫入 64 bits
        bits := math.Float64bits(value)
        gc.writer.WriteBits(bits, 64)
        gc.prevValue = value
        return
    }

    // XOR 與前一個值
    prevBits := math.Float64bits(gc.prevValue)
    currBits := math.Float64bits(value)
    xor := prevBits ^ currBits

    if xor == 0 {
        // 值相同
        gc.writer.WriteBits(0, 1) // '0'
        return
    }

    // 計算前導 0 和尾隨 0
    leadingZeros := countLeadingZeros(xor)
    trailingZeros := countTrailingZeros(xor)
    significantBits := 64 - leadingZeros - trailingZeros

    gc.writer.WriteBits(1, 1) // '1' 表示值不同

    // 檢查是否可以使用前一個值的 leading/trailing zeros
    if leadingZeros >= gc.prevLeadingZeros &&
       trailingZeros >= gc.prevTrailingZeros {
        // 可以重用：只寫 '0' 和有效位元
        gc.writer.WriteBits(0, 1)
        gc.writer.WriteBits(xor>>uint(gc.prevTrailingZeros),
                           64-gc.prevLeadingZeros-gc.prevTrailingZeros)
    } else {
        // 不能重用：寫 '1' + leading zeros + 長度 + 有效位元
        gc.writer.WriteBits(1, 1)
        gc.writer.WriteBits(uint64(leadingZeros), 5)    // 5 bits: 0-31
        gc.writer.WriteBits(uint64(significantBits), 6) // 6 bits: 0-63
        gc.writer.WriteBits(xor>>uint(trailingZeros), significantBits)

        gc.prevLeadingZeros = leadingZeros
        gc.prevTrailingZeros = trailingZeros
    }

    gc.prevValue = value
}

func countLeadingZeros(x uint64) int {
    if x == 0 {
        return 64
    }
    n := 0
    if x <= 0x00000000FFFFFFFF {
        n += 32
        x <<= 32
    }
    // ... 繼續二分搜尋
    return n
}

func countTrailingZeros(x uint64) int {
    if x == 0 {
        return 64
    }
    n := 0
    if (x & 0xFFFFFFFF) == 0 {
        n += 32
        x >>= 32
    }
    // ... 繼續二分搜尋
    return n
}
```

**Michael**: Gorilla 壓縮效果有多好？

**Emma**: Facebook 的論文顯示：

```
壓縮效果（實際生產資料）：

時間戳：
- 原始大小：64 bits/point
- 壓縮後：~1.37 bits/point
- 壓縮率：2.1%

數值：
- 原始大小：64 bits/point
- 壓縮後：~1.07 bits/point（變化小的 metrics）
            ~10 bits/point（變化大的 metrics）
- 壓縮率：1.7% - 15.6%

總體：
- 原始大小：128 bits/point (16 bytes)
- 壓縮後：~2.44 bits/point (0.3 bytes)
- 壓縮比：12:1 到 40:1

實例：
- 每秒 10 萬筆資料
- 未壓縮：100,000 × 16 bytes = 1.6 MB/s = 138 GB/day
- 壓縮後：100,000 × 0.3 bytes = 30 KB/s = 2.6 GB/day
- 節省空間：135.4 GB/day (98.1%)
```

---

## Act 4: TSM (Time-Structured Merge Tree)

**場景**：深入 InfluxDB 的儲存引擎

**Sarah**: InfluxDB 使用 TSM 引擎，它是什麼？

**Emma**: TSM 是 InfluxDB 自己設計的儲存引擎，基於 LSM Tree（Log-Structured Merge Tree）但針對時序資料優化。

### LSM Tree 基礎

**Emma**: 先理解 LSM Tree 的核心思想：

```
LSM Tree 的核心：將隨機寫入轉換為順序寫入

寫入流程：
1. 寫入記憶體（MemTable）
2. MemTable 滿了，刷新到磁碟（SSTable）
3. 定期合併 SSTable（Compaction）

┌─────────────────────────────────────────┐
│           Write Path                     │
├─────────────────────────────────────────┤
│                                          │
│  Client Write                            │
│       ↓                                  │
│  1. WAL (Write-Ahead Log)  ← 持久化保證  │
│       ↓                                  │
│  2. MemTable (記憶體)        ← 快速寫入  │
│       ↓ (滿了)                           │
│  3. Flush to Disk                        │
│       ↓                                  │
│  4. SSTable (Sorted String Table)        │
│                                          │
└─────────────────────────────────────────┘

磁碟上的結構：
Level 0:  [SST-1] [SST-2] [SST-3]        ← 最新資料
Level 1:  [SST-4] [SST-5]                 ← 已合併
Level 2:  [SST-6]                         ← 更大、更舊
```

### TSM 的優化

**Emma**: TSM 針對時序資料做了幾個關鍵優化：

**優化 1：按 Series 組織資料**

```
傳統 LSM（如 LevelDB）：
Key: user:1001:name   Value: "Alice"
Key: user:1001:age    Value: 30
Key: user:1002:name   Value: "Bob"

時序資料更適合：
Series: cpu_usage,host=server01
  Time: 1609459200  Value: 45.2
  Time: 1609459210  Value: 48.1
  Time: 1609459220  Value: 46.8

TSM 格式：
┌────────────────────────────────────────┐
│ TSM File                                │
├────────────────────────────────────────┤
│ Series: cpu_usage,host=server01        │
│   [Compressed Block 1: T0-T99]         │
│   [Compressed Block 2: T100-T199]      │
│                                         │
│ Series: memory_usage,host=server01     │
│   [Compressed Block 1: T0-T99]         │
│   [Compressed Block 2: T100-T199]      │
└────────────────────────────────────────┘

每個 Block：
- 包含 ~1000 個時間點
- 使用 Gorilla 壓縮
- 可以獨立解壓縮
```

**優化 2：列式儲存**

```go
// 不是這樣儲存（行式）：
type Point struct {
    Timestamp int64
    Value     float64
}
points := []Point{
    {1609459200, 45.2},
    {1609459210, 48.1},
    {1609459220, 46.8},
}

// 而是這樣儲存（列式）：
type Block struct {
    Timestamps []int64   // [1609459200, 1609459210, 1609459220]
    Values     []float64 // [45.2, 48.1, 46.8]
}

好處：
1. 更好的壓縮率（相同類型的資料一起壓縮）
2. 查詢效能（只讀取需要的列）
3. SIMD 優化（向量化計算）
```

**優化 3：時間分區**

```
按時間範圍分區（Shard）：

2024-01-15 00:00 - 23:59 → Shard 20240115
2024-01-16 00:00 - 23:59 → Shard 20240116
2024-01-17 00:00 - 23:59 → Shard 20240117

每個 Shard 包含多個 TSM 檔案：
/data/20240115/
    000001.tsm
    000002.tsm
    000003.tsm
/data/20240116/
    000001.tsm
    000002.tsm

優點：
1. 查詢可以跳過不相關的 Shard
2. 刪除舊資料很簡單（直接刪除整個目錄）
3. 並行寫入（不同時間範圍寫入不同 Shard）
```

### TSM 檔案格式

**Emma**: 讓我展示 TSM 檔案的詳細結構：

```
TSM File 格式：

┌─────────────────────────────────────────────────────────┐
│                     Header (5 bytes)                     │
├─────────────────────────────────────────────────────────┤
│ Magic: 0x16D116D1 (4 bytes)                             │
│ Version: 1 (1 byte)                                     │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                     Data Blocks                          │
├─────────────────────────────────────────────────────────┤
│ Block 1: Series A, Time Range [T0, T999]               │
│   ┌──────────────────────────────────────┐             │
│   │ Timestamps (Gorilla compressed)      │             │
│   │ Values (Gorilla compressed)          │             │
│   │ CRC32 checksum                       │             │
│   └──────────────────────────────────────┘             │
│                                                          │
│ Block 2: Series A, Time Range [T1000, T1999]           │
│   ...                                                    │
│                                                          │
│ Block 3: Series B, Time Range [T0, T999]               │
│   ...                                                    │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                     Index (索引)                         │
├─────────────────────────────────────────────────────────┤
│ Series A:                                               │
│   Block 1 → Offset: 1024, Size: 512, MinTime: T0       │
│   Block 2 → Offset: 1536, Size: 480, MinTime: T1000    │
│                                                          │
│ Series B:                                               │
│   Block 3 → Offset: 2016, Size: 520, MinTime: T0       │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                     Footer                               │
├─────────────────────────────────────────────────────────┤
│ Index Offset: 指向索引的位置                             │
│ Index Size: 索引大小                                    │
└─────────────────────────────────────────────────────────┘
```

**讀取流程**：

```go
// internal/tsm/reader.go
package tsm

type TSMReader struct {
    file  *os.File
    index map[string][]IndexEntry // Series → Block 位置
}

type IndexEntry struct {
    MinTime int64
    MaxTime int64
    Offset  int64
    Size    uint32
}

func (r *TSMReader) Read(series string, minTime, maxTime int64) ([]Point, error) {
    // 1. 從索引找到相關的 Blocks
    entries := r.index[series]
    relevantBlocks := []IndexEntry{}

    for _, entry := range entries {
        if entry.MaxTime >= minTime && entry.MinTime <= maxTime {
            relevantBlocks = append(relevantBlocks, entry)
        }
    }

    // 2. 讀取並解壓縮每個 Block
    points := []Point{}
    for _, entry := range relevantBlocks {
        // Seek 到 Block 位置
        r.file.Seek(entry.Offset, 0)

        // 讀取壓縮資料
        compressedData := make([]byte, entry.Size)
        r.file.Read(compressedData)

        // 解壓縮
        block := r.decompressBlock(compressedData)

        // 過濾時間範圍
        for i, ts := range block.Timestamps {
            if ts >= minTime && ts <= maxTime {
                points = append(points, Point{
                    Timestamp: ts,
                    Value:     block.Values[i],
                })
            }
        }
    }

    return points, nil
}

func (r *TSMReader) decompressBlock(data []byte) *Block {
    // 使用 Gorilla 解壓縮器
    decompressor := NewGorillaDecompressor(data)

    timestamps := []int64{}
    values := []float64{}

    for decompressor.HasNext() {
        ts, val := decompressor.Next()
        timestamps = append(timestamps, ts)
        values = append(values, val)
    }

    return &Block{
        Timestamps: timestamps,
        Values:     values,
    }
}
```

---

## Act 5: Prometheus 儲存引擎

**場景**：對比 Prometheus 的設計

**David**: Prometheus 的儲存引擎跟 InfluxDB 有什麼不同？

**Emma**: Prometheus 的設計更簡化，針對監控場景優化。

### Prometheus 資料模型

```
Metric 範例：

http_requests_total{method="GET", endpoint="/api/users", status="200"} 1547 @1609459200

結構：
┌────────────────────┬──────────────────────────────────┬───────┬────────────┐
│ Metric Name        │ Labels                            │ Value │ Timestamp  │
├────────────────────┼──────────────────────────────────┼───────┼────────────┤
│ http_requests_total│ method=GET,                      │ 1547  │ 1609459200 │
│                    │ endpoint=/api/users,             │       │            │
│                    │ status=200                       │       │            │
└────────────────────┴──────────────────────────────────┴───────┴────────────┘

Series ID = Hash(Metric Name + Labels)
例如: Hash("http_requests_total{method='GET',endpoint='/api/users',status='200'}")
     = 0x7f8a9b3c...
```

### Block-based 儲存

**Emma**: Prometheus 使用 Block-based 儲存，每個 Block 覆蓋 2 小時的資料：

```
磁碟結構：

/data/
  01FCXYZ.../          ← Block (2024-01-15 00:00-02:00)
    chunks/
      000001
      000002
      ...
    index              ← 倒排索引
    meta.json          ← 後設資料
    tombstones         ← 刪除標記

  01FCXZA.../          ← Block (2024-01-15 02:00-04:00)
    chunks/
    index
    meta.json
    tombstones

  wal/                 ← Write-Ahead Log
    000001
    000002
    ...
```

**Block 內部結構**：

```
┌──────────────────────────────────────────────────┐
│                   meta.json                       │
├──────────────────────────────────────────────────┤
│ {                                                 │
│   "version": 1,                                  │
│   "ulid": "01FCXYZ...",                          │
│   "minTime": 1609459200000,                      │
│   "maxTime": 1609466400000,                      │
│   "stats": {                                     │
│     "numSamples": 10000000,                      │
│     "numSeries": 5000,                           │
│     "numChunks": 25000                           │
│   }                                              │
│ }                                                │
└──────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────┐
│                   index                           │
├──────────────────────────────────────────────────┤
│ Postings List (倒排索引):                        │
│   __name__="http_requests_total" → [S1, S5, S9] │
│   method="GET" → [S1, S2, S3]                    │
│   method="POST" → [S4, S5]                       │
│   status="200" → [S1, S2, S4]                    │
│                                                   │
│ Series Table:                                    │
│   S1 → Chunks: [C1, C2, C3]                      │
│   S2 → Chunks: [C4, C5]                          │
│   ...                                            │
│                                                   │
│ Chunk Refs:                                      │
│   C1 → File: 000001, Offset: 0, Length: 1024    │
│   C2 → File: 000001, Offset: 1024, Length: 896  │
│   ...                                            │
└──────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────┐
│                   chunks/                         │
├──────────────────────────────────────────────────┤
│ Chunk 格式:                                       │
│   [Encoding: 1 byte]  ← XOR, Delta, etc.        │
│   [Data: variable]    ← Gorilla 壓縮資料         │
│   [CRC32: 4 bytes]                               │
└──────────────────────────────────────────────────┘
```

### Prometheus 查詢流程

```go
// 查詢範例：過去 1 小時的平均 CPU 使用率
// PromQL: avg_over_time(cpu_usage{host="server01"}[1h])

func (db *TSDB) Query(query string, startTime, endTime int64) ([]Point, error) {
    // 1. 解析 PromQL
    matchers := []Matcher{
        {Label: "__name__", Value: "cpu_usage"},
        {Label: "host", Value: "server01"},
    }

    // 2. 找出相關的 Blocks
    blocks := db.findBlocks(startTime, endTime)
    // 例如: [Block1: 10:00-12:00, Block2: 12:00-14:00]

    results := []Point{}

    // 3. 在每個 Block 中查詢
    for _, block := range blocks {
        // 3.1 使用倒排索引找出匹配的 Series
        seriesIDs := block.index.Lookup(matchers)
        // 例如: [Series123, Series456]

        // 3.2 讀取每個 Series 的 Chunks
        for _, seriesID := range seriesIDs {
            chunks := block.index.GetChunks(seriesID)

            for _, chunkRef := range chunks {
                // 3.3 讀取並解壓縮 Chunk
                chunkData := block.readChunk(chunkRef.File, chunkRef.Offset, chunkRef.Length)
                points := decompressChunk(chunkData)

                // 3.4 過濾時間範圍
                for _, p := range points {
                    if p.Timestamp >= startTime && p.Timestamp <= endTime {
                        results = append(results, p)
                    }
                }
            }
        }
    }

    // 4. 應用聚合函數
    return avgOverTime(results), nil
}
```

**Michael**: Prometheus 為什麼選擇 2 小時一個 Block？

**Emma**: 這是權衡的結果：

```
Block 大小權衡：

太小（如 10 分鐘）：
- 優點：寫入延遲低
- 缺點：太多檔案，查詢需要打開很多 Block

太大（如 24 小時）：
- 優點：檔案數量少
- 缺點：記憶體占用高，Compaction 時間長

2 小時：
- 折衷方案
- 典型場景下，查詢範圍通常是數小時（如 Grafana 預設看過去 6 小時）
- 查詢只需打開 3-4 個 Blocks
- Compaction 時間可控（~1-2 秒）
```

### Compaction (壓縮合併)

**Emma**: Prometheus 定期合併小 Blocks 為大 Blocks：

```
Compaction 流程：

Level 0 (每 2 小時):
[00:00-02:00] [02:00-04:00] [04:00-06:00]
       ↓              ↓             ↓
       └──────────────┴─────────────┘
                     ↓
Level 1 (每 12 小時):
              [00:00-12:00]


Level 1:
[00:00-12:00] [12:00-24:00]
       ↓              ↓
       └──────────────┘
              ↓
Level 2 (每 24 小時):
        [00:00-24:00]


合併的好處：
1. 減少檔案數量（提升查詢效能）
2. 重新壓縮（可能有更好的壓縮率）
3. 應用刪除操作（tombstones）
```

---

## Act 6: 查詢優化

**場景**：討論如何加速查詢

**Sarah**: 時序資料庫的查詢通常很慢，有什麼優化技巧？

**Emma**: 查詢優化是關鍵！讓我分享幾個技巧：

### 1. 降採樣 (Downsampling)

```
問題：查詢一年的資料（每秒一筆 = 3150 萬筆）太慢

解決方案：預先計算不同粒度的聚合資料

原始資料（10 秒一筆）：
cpu_usage{host=server01} @1609459200 = 45.2
cpu_usage{host=server01} @1609459210 = 48.1
cpu_usage{host=server01} @1609459220 = 46.8
cpu_usage{host=server01} @1609459230 = 50.3

降採樣（1 分鐘）：
cpu_usage:1m{host=server01} @1609459200 = 47.6  (avg of 6 points)
cpu_usage:1m{host=server01} @1609459260 = 49.2

降採樣（1 小時）：
cpu_usage:1h{host=server01} @1609459200 = 48.5  (avg of 360 points)

查詢策略：
- 查詢 < 1 天：使用原始資料
- 查詢 1-7 天：使用 1 分鐘降採樣
- 查詢 > 7 天：使用 1 小時降採樣

效能提升：
- 查詢 1 年資料：3150 萬 → 8760 筆 (3600× 減少)
- 查詢時間：30 秒 → 0.05 秒
```

**InfluxDB 的 Continuous Query**：

```sql
-- 自動降採樣
CREATE CONTINUOUS QUERY "cq_cpu_1m" ON "mydb"
BEGIN
  SELECT mean(value) AS value
  INTO "cpu_usage_1m"
  FROM "cpu_usage"
  GROUP BY time(1m), *
END

CREATE CONTINUOUS QUERY "cq_cpu_1h" ON "mydb"
BEGIN
  SELECT mean(value) AS value
  INTO "cpu_usage_1h"
  FROM "cpu_usage"
  GROUP BY time(1h), *
END
```

**Prometheus 的 Recording Rules**：

```yaml
groups:
  - name: downsampling
    interval: 1m
    rules:
      - record: cpu_usage:1m
        expr: avg_over_time(cpu_usage[1m])

      - record: cpu_usage:1h
        expr: avg_over_time(cpu_usage[1h])
```

### 2. 倒排索引優化

```
查詢：找出所有 region=us-east 且 status=200 的 Series

倒排索引：
region=us-east → [S1, S2, S3, S5, S7, S9]
status=200     → [S1, S3, S4, S5, S8]

交集運算：
S1, S2, S3, S5, S7, S9
∩
S1, S3, S4, S5, S8
=
S1, S3, S5

優化：使用 Bitmap
region=us-east → 101010101 (9 bits)
status=200     → 101011001 (9 bits)
                 ─────────
AND            → 101010001
                 ↑ ↑ ↑
                 S1 S3 S5

效能：
- 列表交集：O(n + m)
- Bitmap AND：O(1) (硬體指令)
- 對於 100 萬個 Series：從 10ms → 0.01ms
```

### 3. 時間索引

```
問題：即使知道 Series ID，還要掃描所有 Chunks 找出時間範圍

解決方案：為每個 Chunk 儲存時間範圍

Chunk Index:
Series S1:
  Chunk C1: [MinTime: T0,    MaxTime: T999]
  Chunk C2: [MinTime: T1000, MaxTime: T1999]
  Chunk C3: [MinTime: T2000, MaxTime: T2999]

查詢 T500-T1500：
- C1: MaxTime(T999) >= T500 AND MinTime(T0) <= T1500 ✓ 讀取
- C2: MaxTime(T1999) >= T500 AND MinTime(T1000) <= T1500 ✓ 讀取
- C3: MinTime(T2000) > T1500 ✗ 跳過

減少 I/O：從讀取 3 個 Chunks → 2 個 Chunks
```

### 4. 並行查詢

```go
// 並行查詢多個 Series
func (db *TSDB) ParallelQuery(seriesIDs []string, startTime, endTime int64) map[string][]Point {
    results := make(map[string][]Point)
    var mu sync.Mutex
    var wg sync.WaitGroup

    // 使用 worker pool
    semaphore := make(chan struct{}, runtime.NumCPU())

    for _, seriesID := range seriesIDs {
        wg.Add(1)
        go func(id string) {
            defer wg.Done()

            semaphore <- struct{}{}        // 取得 worker
            defer func() { <-semaphore }() // 釋放 worker

            points := db.queryOneSeries(id, startTime, endTime)

            mu.Lock()
            results[id] = points
            mu.Unlock()
        }(seriesID)
    }

    wg.Wait()
    return results
}
```

---

## Act 7: 生產環境最佳實踐

**場景**：討論部署與維運

**Michael**: 如果我們要在生產環境部署時序資料庫，需要注意什麼？

**Emma**: 讓我分享一些最佳實踐。

### 1. 容量規劃

```
計算所需的儲存空間：

輸入參數：
- 時間序列數量（Cardinality）: N
- 採集間隔（Interval）: I 秒
- 資料保留期（Retention）: R 天
- 每個資料點大小: S bytes

未壓縮大小：
Total = N × (86400 / I) × R × S

範例：
- 10,000 個 Series
- 每 10 秒一筆
- 保留 30 天
- 每筆 16 bytes (8 bytes timestamp + 8 bytes value)

Total = 10,000 × (86400 / 10) × 30 × 16
      = 10,000 × 8640 × 30 × 16
      = 41,472,000,000 bytes
      = 38.6 GB

使用 Gorilla 壓縮（壓縮比 12:1）：
Compressed = 38.6 / 12 = 3.2 GB

加上索引（約 10%）：
Total = 3.2 × 1.1 = 3.5 GB
```

### 2. 硬體選擇

```
推薦配置：

小型部署（< 10 萬 Series）：
- CPU: 4 cores
- RAM: 16 GB
- Disk: 500 GB SSD
- 寫入: ~10,000 points/sec
- 查詢: ~100 queries/sec

中型部署（10 萬 - 100 萬 Series）：
- CPU: 16 cores
- RAM: 64 GB
- Disk: 2 TB NVMe SSD
- 寫入: ~100,000 points/sec
- 查詢: ~1,000 queries/sec

大型部署（> 100 萬 Series）：
- 考慮分片部署
- 使用 Prometheus Federation 或 Thanos
- InfluxDB Enterprise（商業版）
```

### 3. 資料保留策略

**InfluxDB Retention Policy**：

```sql
-- 建立保留策略
CREATE RETENTION POLICY "30_days" ON "mydb"
  DURATION 30d
  REPLICATION 1
  DEFAULT

CREATE RETENTION POLICY "1_year" ON "mydb"
  DURATION 365d
  REPLICATION 1

-- 自動降採樣到長期儲存
CREATE CONTINUOUS QUERY "downsample_to_1year" ON "mydb"
BEGIN
  SELECT mean(value) AS value
  INTO "1_year"."cpu_usage_1h"
  FROM "30_days"."cpu_usage"
  GROUP BY time(1h), *
END
```

**Prometheus Storage**：

```yaml
# prometheus.yml
storage:
  tsdb:
    retention.time: 30d
    retention.size: 500GB  # 或限制大小
```

### 4. 高可用部署

**Prometheus Federation**：

```
架構：

┌─────────────────────────────────────────┐
│        Global Prometheus                 │
│  (查詢所有資料，保留降採樣資料)           │
└────────────┬────────────────────────────┘
             │
    ┌────────┴────────┬────────────┐
    ↓                 ↓            ↓
┌─────────┐      ┌─────────┐  ┌─────────┐
│ Prom1   │      │ Prom2   │  │ Prom3   │
│ (集群 A)│      │ (集群 B)│  │ (集群 C)│
└─────────┘      └─────────┘  └─────────┘

Global Prometheus 配置：
scrape_configs:
  - job_name: 'federate'
    scrape_interval: 1m
    honor_labels: true
    metrics_path: '/federate'
    params:
      'match[]':
        - '{job="kubernetes-pods"}'
    static_configs:
      - targets:
        - 'prom1:9090'
        - 'prom2:9090'
        - 'prom3:9090'
```

**InfluxDB Clustering (Enterprise)**：

```
架構：

┌────────────────────────────────────┐
│      Load Balancer                  │
└─────────┬──────────────────────────┘
          │
    ┌─────┴─────┬──────────┐
    ↓           ↓          ↓
┌────────┐  ┌────────┐  ┌────────┐
│ Meta 1 │  │ Meta 2 │  │ Meta 3 │  ← Meta Nodes (管理叢集狀態)
└────────┘  └────────┘  └────────┘

┌────────┐  ┌────────┐  ┌────────┐
│ Data 1 │  │ Data 2 │  │ Data 3 │  ← Data Nodes (儲存資料)
└────────┘  └────────┘  └────────┘

資料分片：
- Series 透過一致性雜湊分配到不同的 Data Node
- 每個 Shard 複製到多個節點（Replication Factor）
```

### 5. 監控時序資料庫本身

```yaml
# 監控 Prometheus
- alert: PrometheusTSDBReloadsFailing
  expr: increase(prometheus_tsdb_reloads_failures_total[3m]) > 0
  labels:
    severity: critical
  annotations:
    summary: "Prometheus TSDB reloads are failing"

- alert: PrometheusNotIngestingSamples
  expr: rate(prometheus_tsdb_head_samples_appended_total[5m]) <= 0
  for: 10m
  labels:
    severity: critical
  annotations:
    summary: "Prometheus is not ingesting samples"

- alert: PrometheusTSDBCompactionsFailing
  expr: increase(prometheus_tsdb_compactions_failed_total[3m]) > 0
  labels:
    severity: warning
  annotations:
    summary: "Prometheus TSDB compactions are failing"
```

**Emma**: 時序資料庫是現代監控系統的基石。理解它的核心原理——資料模型、壓縮技術、儲存引擎、查詢優化——能讓你更好地設計和維運大規模監控系統。

**David**: 我現在明白為什麼 Prometheus 和 InfluxDB 是業界標準了。它們針對時序資料的特性做了大量優化。

**Sarah**: Gorilla 壓縮演算法太神奇了！40:1 的壓縮比意味著我們可以用更少的資源儲存更多的資料。

**Michael**: 我們的 IoT 系統確實需要遷移到專門的時序資料庫。用 MySQL 完全不適合。

**Emma**: 完全正確。選擇正確的工具對於正確的問題，是工程師的核心能力之一。

---

## 總結

### 時序資料庫核心概念

1. **資料模型**: Measurement/Metric + Tags/Labels + Fields/Values + Timestamp
2. **基數管理**: 控制 Tag cardinality，避免索引爆炸
3. **壓縮技術**: Gorilla (Delta-of-Delta + XOR)，壓縮比 12-40:1
4. **儲存引擎**: TSM (InfluxDB) / Block-based (Prometheus)
5. **查詢優化**: 降採樣、倒排索引、並行查詢

### InfluxDB vs Prometheus

| 特性 | InfluxDB | Prometheus |
|------|----------|------------|
| 資料模型 | Measurement + Tags + Fields | Metric + Labels |
| 寫入模式 | Push | Pull |
| 查詢語言 | InfluxQL / Flux | PromQL |
| 適用場景 | 通用時序資料 | 監控與告警 |
| 高可用 | Enterprise | Federation / Thanos |

### 最佳實踐

1. 正確設計 Tags（低基數）和 Fields（高基數）
2. 使用降採樣減少查詢資料量
3. 設定合理的資料保留期
4. 監控時序資料庫本身的健康狀態
5. 根據規模選擇合適的硬體配置

下一章我們將學習 **Graph Database**（圖資料庫），探索社交網路、推薦系統等圖結構資料的儲存與查詢！
