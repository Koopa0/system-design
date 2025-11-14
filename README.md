# System Design with Go

使用 Go 實現經典系統設計問題的教學專案。

## 專案簡介

系統設計教學專案，展示如何分析和實現經典系統設計問題。

每個案例包含：
- **DESIGN.md** - 設計決策樹、權衡分析、擴展性討論
- **README.md** - 使用說明與 API 文檔
- **程式碼實現** - 帶詳細註解的 Go 程式碼

**目標：** 展示系統設計思維過程，而非生產級完整實現。

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
- PostgreSQL（持久化儲存）
- Redis（快取、原子操作）

**重點：** 使用 Go 標準庫展示慣用模式，最小化第三方依賴。

## 文檔結構

每個案例的 DESIGN.md 包含：
- 問題定義與容量估算
- 4-5 個核心設計決策（❌ 為何不選 → ✅ 為何選）
- 從當前到 10x、100x 的擴展分析
- 實現範圍標註（✅ 已實現 / ⚠️ 教學簡化 / 🚀 生產環境需要）

## 未來計劃

更多系統設計案例請參考 [ROADMAP.md](./ROADMAP.md)。

## License

MIT License
