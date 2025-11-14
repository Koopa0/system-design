# Rate Limiter

分散式限流器，支援多種演算法與多維度限流。

## 設計目標

實作生產級限流系統，展示從單機到分散式架構的演進過程。

## 支援的演算法

- Token Bucket - 支援突發流量，最常用
- Leaky Bucket - 平滑流量輸出
- Sliding Window - 精確控制，避免邊界問題

## 限流維度

- IP 位址限流
- 使用者限流
- API 端點限流

## 架構演進

### 單機版
使用本地記憶體實作，適合單一服務實例。

### 分散式版
使用 Redis + Lua 腳本保證原子性，支援多服務實例。

## 使用方式

```go
// Token Bucket 範例
limiter := tokenbucket.New(100, 10) // 容量100，每秒填充10個token
if limiter.Allow() {
    // 處理請求
}

// 分散式限流範例
limiter := distributed.New(redisClient, "api:/users", 1000, time.Minute)
if limiter.Allow(ctx, userID) {
    // 處理請求
}
```

## 執行

```bash
# 啟動 Redis
docker-compose up -d

# 執行服務
go run cmd/server/main.go

# 測試
curl http://localhost:8080/api/test
```

## 效能指標

- 單機版：100K+ RPS
- 分散式版：50K+ RPS（受限於 Redis 網路延遲）

## 實作細節

詳見程式碼註解。
