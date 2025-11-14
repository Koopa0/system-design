# URL Shortener

分散式短網址服務，展示 ID 生成、Base62 編碼、快取策略等核心概念。

## 設計目標

實作類似 bit.ly 或 TinyURL 的短網址服務，支援高並發與快速重定向。

## 核心功能

- 長網址轉短網址
- 短網址重定向
- 自訂短網址（可選）
- 點擊統計
- 過期機制

## 問題定義

給定一個長網址，產生一個短網址：

```
輸入: https://www.example.com/very/long/url/with/many/parameters?id=12345&ref=abc
輸出: https://short.url/aB3xD9
```

點擊短網址時，重定向到原始長網址。

### 使用場景

1. **社交媒體**：Twitter 字數限制（280 字）
2. **行銷追蹤**：追蹤點擊來源、轉換率
3. **美觀易記**：短網址更容易分享和記憶
4. **防止釣魚**：顯示短網址來源，使用者更信任

## 需求分析

### 功能需求

1. **縮短網址**
   - 使用者提交長網址
   - 系統返回唯一的短網址
   - 短網址格式：`https://short.url/{shortCode}`

2. **重定向**
   - 使用者訪問短網址
   - 系統返回 301/302 重定向
   - 記錄點擊統計

3. **自訂短網址**（可選）
   - 使用者指定 shortCode
   - 檢查是否已被占用

4. **過期機制**（可選）
   - 設定過期時間
   - 過期後自動刪除或返回 404

### 非功能需求

1. **高可用性**：99.9% uptime
2. **低延遲**：重定向 < 10ms (P99)
3. **可擴展**：支援每日 1 億次請求
4. **URL 長度**：6-7 個字元

## 容量估算

### 假設

- 寫入：100 萬 URL/天
- 讀取：100:1 讀寫比（1 億次重定向/天）
- 儲存時間：10 年
- URL 平均長度：100 bytes

### 計算

**總 URL 數量**：
```
100 萬/天 × 365 天 × 10 年 = 36.5 億個 URL
```

**QPS**：
```
寫入：100 萬 / 86400 秒 ≈ 12 QPS
讀取：1 億 / 86400 秒 ≈ 1160 QPS
```

**儲存空間**：
```
36.5 億 × 100 bytes ≈ 365 GB
```

**快取需求**（80/20 法則）：
```
20% 熱門 URL = 73 GB
```

## 系統設計

### 架構

```
Client → Load Balancer → API Servers → Redis (cache) → PostgreSQL (storage)
                                      → ID Generator
```

### 核心組件

1. **ID Generator**：產生唯一 ID
2. **Base62 Encoder**：將 ID 編碼為短碼
3. **Cache Layer**：Redis 快取熱門 URL
4. **Database**：PostgreSQL 持久化儲存

### ID 生成策略

#### 方案一：Auto-increment ID

優點：
- 簡單
- 順序遞增

缺點：
- 單點故障（單一資料庫）
- 可預測（安全問題）
- 擴展困難

#### 方案二：UUID

優點：
- 全域唯一
- 無需協調

缺點：
- 過長（32 字元）
- 無序（索引效能差）

#### 方案三：Snowflake（採用）

優點：
- 全域唯一
- 時間有序
- 高效能（本地產生）

缺點：
- 需要機器 ID 分配
- 時鐘回撥問題

**Snowflake ID 結構**（64-bit）：
```
1 bit (unused) | 41 bits (timestamp) | 10 bits (machine ID) | 12 bits (sequence)
```

### Base62 編碼

**為何使用 Base62？**
- URL 友善：只包含 [a-zA-Z0-9]
- 長度短：64-bit 數字編碼為 7 個字元
- 可逆：可解碼回原始 ID

**編碼表**：
```
0-9: 0-9
10-35: a-z
36-61: A-Z
```

**範例**：
```
ID: 123456789
Base62: 8M0kX
```

### 快取策略

#### Cache-Aside 模式

```go
func GetURL(shortCode string) (string, error) {
    // 1. 先查快取
    if url, err := cache.Get(shortCode); err == nil {
        return url, nil
    }

    // 2. 快取未命中，查資料庫
    url, err := db.Query(shortCode)
    if err != nil {
        return "", err
    }

    // 3. 寫入快取
    cache.Set(shortCode, url, 24*time.Hour)
    return url, nil
}
```

#### 快取問題與解決

**1. 快取穿透**（查詢不存在的 URL）
- 解決：快取空值或使用 Bloom Filter

**2. 快取雪崩**（大量快取同時過期）
- 解決：隨機 TTL（24h ± 1h）

**3. 快取擊穿**（熱門 URL 過期瞬間）
- 解決：分散式鎖或永不過期

## API 設計

### 縮短 URL

```http
POST /api/v1/shorten
Content-Type: application/json

{
  "long_url": "https://example.com/very/long/url",
  "custom_code": "my-link",
  "expire_at": "2025-12-31T23:59:59Z"
}
```

回應：
```json
{
  "short_url": "https://short.url/aB3xD9",
  "short_code": "aB3xD9",
  "long_url": "https://example.com/very/long/url",
  "created_at": "2024-01-15T10:30:00Z"
}
```

### 重定向

```http
GET /{shortCode}
```

回應：
```http
HTTP/1.1 301 Moved Permanently
Location: https://example.com/very/long/url
```

### 統計查詢

```http
GET /api/v1/stats/{shortCode}
```

回應：
```json
{
  "short_code": "aB3xD9",
  "clicks": 12345,
  "created_at": "2024-01-15T10:30:00Z",
  "last_accessed": "2024-01-16T08:20:00Z"
}
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
curl -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"long_url":"https://example.com/test"}'
```

## 資料模型

### PostgreSQL Schema

```sql
CREATE TABLE urls (
    id BIGINT PRIMARY KEY,
    short_code VARCHAR(10) UNIQUE NOT NULL,
    long_url TEXT NOT NULL,
    custom_code BOOLEAN DEFAULT FALSE,
    clicks BIGINT DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    expire_at TIMESTAMP,
    INDEX idx_short_code (short_code),
    INDEX idx_created_at (created_at)
);
```

## 測試

### 單元測試

```bash
go test -v ./...
```

### 整合測試

```bash
make test-integration
```

測試場景：
- ID 生成唯一性
- Base62 編碼正確性
- 快取命中率
- 重定向正確性

## 效能基準

### 測試環境

- CPU: 4 cores
- Memory: 8 GB
- Redis: 單實例
- PostgreSQL: 單實例

### 效能指標

| 操作 | QPS | P50 延遲 | P99 延遲 |
|------|-----|---------|---------|
| Shorten URL | 500 | 10ms | 30ms |
| Redirect (cache hit) | 50,000 | 1ms | 3ms |
| Redirect (cache miss) | 1,000 | 5ms | 15ms |

### 快取命中率

- 熱門 URL：95%+
- 長尾 URL：60-70%
- 整體：80-85%

## 擴展性

### 從 1K 到 100K QPS

**單機版本（<5K QPS）**：
- 當前架構已足夠

**垂直擴展（5K-20K QPS）**：
- 升級伺服器規格
- Redis 主從複製

**水平擴展（20K-100K QPS）**：
- API Server：無狀態，可任意擴展
- Redis：Redis Cluster
- PostgreSQL：分片 + 讀寫分離

**分片策略**：
```
short_code → hash → shard_id
例如：aB3xD9 → shard_0
     xY7zK2 → shard_1
```

## 監控指標

建議監控：
- 重定向成功率
- 快取命中率
- 重定向延遲（P50, P95, P99）
- 404 錯誤率

## 已知限制

1. **URL 不可修改**：短碼產生後無法更改對應的長網址
2. **無防濫用機制**：未實作 Rate Limiting
3. **統計資料簡單**：只記錄點擊次數，無來源分析

## 實作細節

詳見程式碼註解：
- `internal/shortener/service.go` - 核心業務邏輯
- `internal/shortener/idgen.go` - Snowflake ID 產生器
- `internal/shortener/base62.go` - Base62 編碼器
- `pkg/cache/redis.go` - Redis 快取層
