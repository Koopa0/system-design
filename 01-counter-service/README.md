# Counter Service

分散式計數器服務，支援高並發計數操作與降級機制。

## 設計目標

實作生產級計數器系統，展示 Redis + PostgreSQL 雙寫架構與降級策略。

## 核心功能

- 多計數器管理（在線人數、活躍使用者、遊戲局數）
- 原子操作（INCR/DECR）
- 去重計數（同一使用者每日只計算一次）
- 自動重置（每日凌晨重置特定計數器）
- 資料歸檔（保留歷史資料）

## 系統設計

### 架構

```
Client → API Server → Redis (cache) ↘
                                      → PostgreSQL (persistent)
```

### 雙寫策略

1. **寫入路徑**：Redis（即時）→ 批量寫入 PostgreSQL（定期）
2. **讀取路徑**：優先讀 Redis，失敗則降級到 PostgreSQL
3. **降級機制**：Redis 故障時，只讀模式從 PostgreSQL 查詢

### 關鍵設計決策

**為何使用 Redis？**
- 原子操作：INCR/DECR 保證並發安全
- 高效能：記憶體操作，支援 10K+ QPS
- 過期機制：TTL 自動清理去重記錄

**為何需要 PostgreSQL？**
- 資料持久化：Redis 重啟不遺失資料
- 歷史查詢：支援時間範圍查詢
- 資料分析：支援複雜聚合查詢

**Trade-offs**：
- 最終一致性：批量寫入導致短暫延遲（可接受）
- 複雜度增加：需維護雙寫同步邏輯
- 效能提升：減少 80% 資料庫查詢

## API

### 增加計數

```http
POST /api/v1/counter/{name}/increment
Content-Type: application/json

{
  "value": 1,
  "user_id": "u123456",
  "metadata": {}
}
```

回應：
```json
{
  "success": true,
  "current_value": 12345
}
```

### 減少計數

```http
POST /api/v1/counter/{name}/decrement
Content-Type: application/json

{
  "value": 1,
  "user_id": "u123456"
}
```

### 查詢計數

```http
GET /api/v1/counter/{name}
```

回應：
```json
{
  "name": "online_players",
  "value": 12344,
  "last_updated": "2024-01-15T10:30:00Z"
}
```

### 批量查詢

```http
GET /api/v1/counters?names=online_players,daily_active_users
```

## 使用方式

### 啟動服務

```bash
# 1. 啟動依賴服務
docker-compose up -d

# 2. 執行資料庫遷移
make migrate-up

# 3. 啟動服務
go run cmd/server/main.go

# 4. 測試 API
curl -X POST http://localhost:8080/api/v1/counter/online_players/increment
```

### 設定檔

編輯 `config.yaml`：

```yaml
server:
  port: 8080

redis:
  addr: localhost:6379
  password: ""
  db: 0

postgres:
  host: localhost
  port: 5432
  user: postgres
  password: postgres
  dbname: counter_db
```

## 測試

### 單元測試

```bash
go test -v ./...
```

### 並發測試

```bash
go test -v -race ./internal/counter
```

測試場景：
- 1000 個 goroutine 同時 increment
- 驗證最終計數正確性
- 檢查資料競爭

### 整合測試

```bash
make test-integration
```

測試場景：
- Redis 故障降級
- PostgreSQL 批量寫入
- 去重邏輯驗證

## 效能基準

### 測試環境

- CPU: 4 cores
- Memory: 8 GB
- Redis: 單實例
- PostgreSQL: 單實例

### 效能指標

| 操作 | QPS | P50 延遲 | P99 延遲 |
|------|-----|---------|---------|
| Increment | 12,000 | 2ms | 8ms |
| Decrement | 12,000 | 2ms | 8ms |
| Get | 25,000 | 1ms | 5ms |
| Batch Get (10) | 15,000 | 3ms | 12ms |

### 壓力測試

```bash
# 使用 wrk 測試
wrk -t12 -c400 -d30s --latency \
  http://localhost:8080/api/v1/counter/test/increment
```

預期結果：
- QPS > 10,000
- P99 延遲 < 10ms
- 無錯誤回應

## 擴展性

### 從 1K 到 100K QPS

**單機版本（1K-10K QPS）**：
- 當前架構已足夠
- Redis 單實例可處理 10K+ QPS

**垂直擴展（10K-50K QPS）**：
- 升級伺服器規格（更多 CPU、記憶體）
- Redis 使用持久化機制（AOF）

**水平擴展（50K-100K QPS）**：
- API Server：無狀態，可任意擴展
- Redis：使用 Redis Cluster 分片
- PostgreSQL：讀寫分離 + 分片

**分片策略**：
```
counter_name → hash → shard_id
例如：online_players → shard_0
     daily_active_users → shard_1
```

## 監控指標

建議監控：
- Redis 命中率
- PostgreSQL 寫入延遲
- API 錯誤率
- 計數器異常波動

## 已知限制

1. **計數器不支援事務**：多個計數器操作無法保證原子性
2. **批量寫入可能遺失**：服務崩潰時，未寫入 PostgreSQL 的資料會遺失
3. **去重記錄佔用記憶體**：大量使用者會佔用 Redis 記憶體

## 實作細節

詳見程式碼註解：
- `internal/counter/service.go` - 核心業務邏輯
- `internal/counter/redis.go` - Redis 操作
- `internal/counter/postgres.go` - PostgreSQL 操作
- `internal/counter/batch.go` - 批量寫入邏輯
