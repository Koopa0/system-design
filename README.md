# System Design with Go

使用 Go 實現經典系統設計問題的教學專案。

## 專案簡介

本專案提供五個經過深入設計分析的系統實作案例，每個案例包含：
- 詳細的設計決策樹（DESIGN.md）
- 可運行的 Go 實現代碼
- 擴展性分析與權衡討論

**目標：** 展示系統設計思維過程，而非生產級完整實現。

## 已完成案例

| 服務 | 核心設計問題 | 關鍵技術 |
|------|-------------|---------|
| [Counter Service](./01-counter-service) | 高頻寫入、批量優化 | Redis + PostgreSQL, Batch Write |
| [Room Management](./02-room-management) | 實時狀態同步、並發控制 | WebSocket, Finite State Machine |
| [URL Shortener](./03-url-shortener) | 分布式唯一 ID、Base62 編碼 | Snowflake, Cache-Aside |
| [Rate Limiter](./04-rate-limiter) | 流量控制、防止突發 | Token Bucket, Sliding Window |
| [Distributed Cache](./05-distributed-cache) | 淘汰算法、水平擴展 | LRU/LFU, Consistent Hashing |

每個服務目錄包含：
- `DESIGN.md` - 系統設計文檔（決策樹、權衡分析、擴展性討論）
- `README.md` - 使用說明與 API 文檔
- 代碼實現（帶詳細註解）

## 快速開始

```bash
# 以 Counter Service 為例
cd 01-counter-service

# 啟動依賴服務（Redis + PostgreSQL）
docker-compose up -d

# 運行服務
go run cmd/server/main.go

# 測試 API
curl http://localhost:8080/api/v1/counter/online_players/increment
```

詳細使用說明請參考各服務目錄的 README.md。

## 環境需求

- Go 1.24+
- Docker & Docker Compose（用於運行依賴服務）

## 技術

**核心：**
- Go 標準庫（`net/http`、`context`、`sync`）
- PostgreSQL（持久化存儲）
- Redis（快取、原子操作）

**重點：** 使用 Go 標準庫展示慣用模式，最小化第三方依賴。

## 學習路線

建議按照以下順序學習：

1. **Counter Service** - 理解批量寫入優化
2. **Room Management** - 學習狀態機與事件驅動
3. **URL Shortener** - 掌握分布式 ID 生成
4. **Rate Limiter** - 了解不同限流算法特性
5. **Distributed Cache** - 深入淘汰算法與一致性哈希

每個案例的 DESIGN.md 包含：
- 問題定義與容量估算
- 4-5 個核心設計決策（為什麼選這個方案？）
- 從當前到 10x、100x 的擴展分析
- 實現範圍標註（已實現 vs 教學簡化 vs 生產環境需要）

## 專案特色

- **決策導向**：每個設計決策都說明為什麼選擇（❌ 為何不選 A → ✅ 為何選 B）
- **權衡分析**：明確標註每個選擇的優勢與代價
- **擴展性思考**：從 1K 到 100K 的容量規劃與成本估算
- **教學透明**：清楚標註哪些是教學簡化、哪些是生產環境必需

## 未來計劃

更多系統設計案例請參考 [ROADMAP.md](./ROADMAP.md)。

## License

MIT License
