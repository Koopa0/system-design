# Unique ID Generator

分布式唯一 ID 生成器，展示 Snowflake、UUID、ULID 等不同方案的對比與權衡。

## 設計目標

實作高性能、全局唯一、趨勢遞增的 ID 生成服務，適用於訂單號、用戶 ID、分布式追蹤等場景。

## 核心功能

- **Snowflake 算法**：64-bit 整數 = 時間戳 + 機器 ID + 序列號
- **UUID 生成**：128-bit 全局唯一識別碼
- **ULID 生成**：時間有序的 UUID 替代品
- **時鐘回撥處理**：容忍小回撥，拒絕大回撥
- **機器 ID 管理**：支持手動配置或自動分配

## 使用方式

### Snowflake ID

```go
import "github.com/yourusername/06-unique-id-generator/internal/generator"

// 初始化生成器（machineID: 0-1023）
gen, err := generator.NewSnowflake(1)
if err != nil {
    log.Fatal(err)
}

// 生成 ID
id := gen.Generate()
fmt.Printf("Generated ID: %d\n", id)

// 解析 ID
info := generator.ParseSnowflakeID(id)
fmt.Printf("Timestamp: %v\n", info.Timestamp)
fmt.Printf("MachineID: %d\n", info.MachineID)
fmt.Printf("Sequence: %d\n", info.Sequence)
```

### UUID

```go
import "github.com/google/uuid"

// UUID v4（隨機）
id := uuid.New()
fmt.Printf("UUID: %s\n", id.String())
// 550e8400-e29b-41d4-a716-446655440000

// UUID v1（時間戳 + MAC 地址）
id := uuid.NewUUID()
```

### ULID

```go
import "github.com/oklog/ulid/v2"

// 生成 ULID
id := ulid.Make()
fmt.Printf("ULID: %s\n", id.String())
// 01ARZ3NDEKTSV4RRFFQ69G5FAV
```

### HTTP API

```bash
# 生成 Snowflake ID
curl http://localhost:8080/api/v1/snowflake

# 生成批量 ID（100 個）
curl http://localhost:8080/api/v1/snowflake/batch?count=100

# 解析 ID
curl http://localhost:8080/api/v1/snowflake/123456789/parse

# 生成 UUID
curl http://localhost:8080/api/v1/uuid

# 生成 ULID
curl http://localhost:8080/api/v1/ulid
```

## 執行

```bash
# 1. 啟動服務（machineID=1）
go run cmd/server/main.go --machine-id=1

# 2. 測試生成
curl http://localhost:8080/api/v1/snowflake

# 回應範例
{
  "id": 123456789012345,
  "id_str": "123456789012345",
  "timestamp": "2024-03-15T10:30:45.123Z",
  "machine_id": 1,
  "sequence": 789
}
```

## 效能指標

### Snowflake

| 指標 | 數值 |
|------|------|
| 單機 QPS | 10,000+ |
| 延遲 | < 0.1ms |
| 每毫秒容量 | 4,096 個 ID |
| 支持機器數 | 1,024 台 |
| 可用時間 | 69 年 |

### UUID/ULID

| 指標 | 數值 |
|------|------|
| 單機 QPS | 100,000+ |
| 延遲 | < 0.01ms |
| ID 長度 | 128-bit / 26 字符 |
| 碰撞概率 | < 10^-15 |

## ID 方案對比

| 方案 | 長度 | 有序性 | 性能 | 適用場景 |
|------|------|--------|------|---------|
| **Snowflake** | 64-bit | 趨勢遞增 | 高 | 訂單號、用戶 ID、資料庫主鍵 |
| **UUID** | 128-bit | 無序 | 極高 | 無序場景、跨系統唯一標識 |
| **ULID** | 128-bit | 時間有序 | 極高 | 需要排序的無序場景 |
| **自增 ID** | 64-bit | 嚴格遞增 | 中 | 單機資料庫、小規模系統 |

## 時鐘回撥處理

```go
// 配置容忍範圍
config := &generator.Config{
    MachineID:        1,
    MaxBackwardMS:    5000,  // 最多容忍 5 秒回撥
    EnableMonitoring: true,  // 啟用監控
}

gen := generator.NewSnowflakeWithConfig(config)

// 生成 ID（自動處理小回撥）
id, err := gen.Generate()
if err != nil {
    // 大回撥時返回錯誤
    log.Error("Clock moved backwards too much:", err)
}
```

監控指標：
- 時鐘回撥次數
- 回撥偏移量
- 生成失敗率

## 機器 ID 分配

### 方案 A：手動配置

```yaml
# config.yaml
machine_id: 1
datacenter_id: 0
```

### 方案 B：IP 地址哈希

```go
ip := getLocalIP()
machineID := hash(ip) % 1024
```

### 方案 C：ZooKeeper 自動分配（推薦）

```go
allocator := NewZKAllocator("localhost:2181")
machineID, err := allocator.Allocate()
// 自動分配唯一 ID，節點下線自動回收
```

## 測試

```bash
# 單元測試
go test -v ./...

# 性能測試
go test -bench=. -benchmem ./internal/generator

# 並發測試
go test -race -v ./internal/generator
```

測試場景：
- Snowflake 唯一性（100 萬 ID）
- 時鐘回撥處理
- 並發安全性
- 性能基準

## 實作細節

詳細的系統設計分析請參考 [DESIGN.md](./DESIGN.md)，包含：
- Snowflake vs UUID vs ULID vs 自增 ID 完整對比
- 時鐘回撥的 4 種處理方案
- 機器 ID 的 4 種分配策略
- 從 10K 到 100K 到 1000 萬 QPS 的擴展分析
- 兩級機器 ID（數據中心 + 機器）設計
