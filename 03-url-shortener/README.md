# URL Shortener

分散式短網址服務，展示 Snowflake ID 生成、Base62 編碼、快取策略等核心概念。

## 設計目標

實作類似 bit.ly 或 TinyURL 的短網址服務，支援高並發與快速重定向。

## 核心功能

- 長網址轉短網址（Snowflake ID + Base62 編碼）
- 短網址重定向（Cache-Aside 策略）
- 自訂短網址（可選）
- 點擊統計
- SSRF 防護

## 使用方式

### 縮短 URL

```bash
curl -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{
    "long_url": "https://example.com/very/long/url",
    "custom_code": "my-link",
    "expire_at": "2025-12-31T23:59:59Z"
  }'
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

```bash
curl -L http://localhost:8080/aB3xD9
```

回應：301 重定向到原始 URL

### 統計查詢

```bash
curl http://localhost:8080/api/v1/stats/aB3xD9
```

## 執行

```bash
# 1. 啟動依賴服務（Redis + PostgreSQL）
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

## 效能指標

| 操作 | QPS | P50 延遲 | P99 延遲 |
|------|-----|---------|---------|
| 縮短 URL | 500 | 10ms | 30ms |
| 重定向（快取命中） | 50,000 | 1ms | 3ms |
| 重定向（快取未命中） | 1,000 | 5ms | 15ms |

**快取命中率**：80-85%

## 資料模型

```sql
CREATE TABLE urls (
    id BIGINT PRIMARY KEY,
    short_code VARCHAR(10) UNIQUE NOT NULL,
    long_url TEXT NOT NULL,
    clicks BIGINT DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    expire_at TIMESTAMP,
    INDEX idx_short_code (short_code)
);
```

## 實作細節

詳細的系統設計分析請參考 [DESIGN.md](./DESIGN.md)，包含：
- Snowflake vs UUID vs Auto-increment ID 比較
- Base62 vs Base64 編碼權衡
- Cache-Aside 模式實現
- SSRF 防護策略
- 從 1K 到 100K QPS 的擴展分析
