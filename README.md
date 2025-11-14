# System Design with Go

生產級系統設計實作案例庫，使用 Go 標準庫實作經典系統設計問題。

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

## 專案簡介

本專案提供經過生產驗證的系統設計實作範例，著重於：

- 系統設計思維與權衡分析
- Go 標準庫最佳實踐
- 分散式系統核心概念
- 詳細的程式碼註解與設計說明

### 參考資料

- [ByteByteGo](https://bytebytego.com/) - 系統設計圖解
- [Grokking the System Design Interview](https://www.educative.io/courses/grokking-the-system-design-interview) - 面試準備
- [Designing Data-Intensive Applications](https://dataintensive.net/) - 分散式系統理論

## 學習路線圖

### Phase 1: 基礎組件

所有大型系統的基石。

| 編號 | 系統 | 核心概念 | 狀態 |
|------|------|----------|------|
| [01](./01-counter-service) | Counter Service | Redis、降級機制、批量優化 | 已完成 |
| [02](./02-room-management) | Room Management | WebSocket、狀態機、並發控制 | 已完成 |
| [03](./03-url-shortener) | URL Shortener | ID 生成、Base62、快取策略 | 已完成 |
| [04](./04-rate-limiter) | Rate Limiter | 限流演算法、分散式限流 | 進行中 |
| 05 | Distributed Cache | LRU、一致性雜湊 | 規劃中 |

### Phase 2: 數據密集型

處理大量數據的系統。

| 編號 | 系統 | 核心概念 | 狀態 |
|------|------|----------|------|
| 10 | Search Autocomplete | Trie、前綴匹配、Elasticsearch | 規劃中 |
| 11 | Web Crawler | 分散式爬蟲、去重、URL Frontier | 規劃中 |
| 12 | Metrics Monitoring | 時序數據、聚合、Prometheus | 規劃中 |

### Phase 3: 社交媒體

高並發、實時性要求。

| 編號 | 系統 | 核心概念 | 狀態 |
|------|------|----------|------|
| 20 | News Feed | Feed 生成、推拉模型、Fanout | 規劃中 |
| 21 | Chat System | 1對1、群聊、離線訊息 | 規劃中 |
| 22 | Notification Service | 多通道推送、訂閱、去重 | 規劃中 |

### Phase 4: 媒體平台

處理大檔案和串流。

| 編號 | 系統 | 核心概念 | 狀態 |
|------|------|----------|------|
| 30 | YouTube | 影片上傳、轉碼、CDN | 規劃中 |
| 31 | Netflix | 串流、自適應碼率 | 規劃中 |

### Phase 5: 電商交易

高並發交易、庫存管理。

| 編號 | 系統 | 核心概念 | 狀態 |
|------|------|----------|------|
| 50 | Flash Sale | 庫存扣減、超賣問題 | 規劃中 |
| 51 | Payment System | 雙寫、冪等性、對帳 | 規劃中 |

### Phase 6: AI 平台

新興的 AI 系統設計。

| 編號 | 系統 | 核心概念 | 狀態 |
|------|------|----------|------|
| 60 | ChatGPT-like System | LLM API、流式輸出 | 規劃中 |
| 61 | AI Agent Platform | Agent 編排、Tool Calling | 規劃中 |
| 62 | RAG System | 向量資料庫、Embedding | 規劃中 |

完整路線圖請參考 [ROADMAP.md](./ROADMAP.md)

## 技術棧

### 核心技術

- **語言**: Go 1.24+
- **Web 框架**: 僅使用標準庫 `net/http`
  - 展示 Go 最佳實踐和設計模式
  - Go 1.22+ 的增強路由支援
  - 無第三方依賴，部署簡單
- **資料庫**: PostgreSQL, Redis
- **訊息佇列**: NATS, NSQ, Kafka
- **監控**: Prometheus, Grafana
- **容器化**: Docker, Docker Compose

### 設計原則

本專案遵循以下原則：

#### 1. Go 最佳實踐

- 嚴格遵循 [Effective Go](https://go.dev/doc/effective_go)
- 參考 [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- 學習標準庫的優雅風格（`net/http`、`context`、`errors`）
- 使用慣用的 Go 模式（`io.Reader`、函數選項模式）

#### 2. 詳細註解

每個設計決策都應解釋原因：

```go
// GetUser 從資料庫查詢使用者資訊
//
// 設計考量：
//   - 優先從 Redis 快取查詢（降低資料庫壓力）
//   - 快取未命中時查詢 PostgreSQL
//   - 使用 context.Context 支援逾時和取消
//
// Trade-offs：
//   - 快取可能不一致（最終一致性，可接受 1 秒延遲）
//   - 增加了系統複雜度（但提升了 80% 效能）
func GetUser(ctx context.Context, id string) (*User, error)
```

#### 3. 系統設計優先

- 重點講解：為什麼用 Redis？為什麼用這個演算法？
- 詳細說明：CAP 權衡、一致性選擇、擴展性考量
- 簡化非核心邏輯：錯誤處理可簡化（但要正確）

#### 4. 程式碼優雅且可讀

```go
type UserService struct {
    db    *sql.DB
    cache *redis.Client
}

func (s *UserService) GetUser(ctx context.Context, id string) (*User, error) {
    // 先查快取
    if user, err := s.getFromCache(ctx, id); err == nil {
        return user, nil
    }

    // 快取未命中，查資料庫
    return s.getFromDB(ctx, id)
}
```

### 技術棧映射

| 技術 | 使用案例 | 說明 |
|------|---------|------|
| **net/http** | 所有專案 | 標準庫，展示 HTTP 最佳實踐 |
| **Redis** | 01-Counter, 03-URL-Shortener, 04-Rate-Limiter | 快取、原子操作 |
| **PostgreSQL** | 01-Counter, 02-Room, 03-URL-Shortener | 持久化儲存 |
| **WebSocket** | 02-Room, 21-Chat, 60-ChatGPT | 實時通訊 |
| **NATS** | 22-Notification | 輕量級訊息佇列 |
| **Kafka** | 30-YouTube, 50-Flash-Sale | 大規模事件流 |
| **Elasticsearch** | 10-Search-Autocomplete, 11-Web-Crawler | 全文搜尋 |
| **gRPC** | 微服務通訊 | 服務間通訊 |

## 快速開始

### 環境需求

```bash
# Go 版本
go version  # 需要 1.23+

# Docker（用於執行依賴服務）
docker --version
docker-compose --version
```

### 執行某個專案

以 `01-counter-service` 為例：

```bash
# 1. 進入專案目錄
cd 01-counter-service

# 2. 啟動依賴服務（Redis + PostgreSQL）
docker-compose up -d

# 3. 執行資料庫遷移
make migrate-up

# 4. 啟動服務
go run cmd/server/main.go

# 5. 測試 API
curl http://localhost:8080/api/v1/counter/online_players/increment
```

每個專案都有獨立的 README，包含詳細的執行說明。

## 如何使用本專案

### 學習方式建議

1. **閱讀 README.md** - 了解問題背景、需求分析、架構設計
2. **執行程式碼** - 本地啟動服務，實際體驗系統行為
3. **閱讀原始碼** - 每個檔案都有詳細註解，理解實作細節
4. **執行測試** - 單元測試、整合測試、壓力測試
5. **擴展練習** - 嘗試實作文件中的延伸問題

### 面試準備

如果你正在準備系統設計面試：

1. **按順序學習** - 從 Phase 1 的基礎組件開始
2. **理解 Trade-offs** - 每個設計決策背後的原因
3. **練習畫圖** - 能夠快速畫出架構圖
4. **估算容量** - 練習 Back-of-the-envelope 計算
5. **討論擴展性** - 從小規模到大規模的演進

## 專案結構

每個系統設計案例的標準結構：

```
XX-system-name/
├── README.md           # 專案說明
├── cmd/
│   └── server/
│       └── main.go     # 服務入口
├── internal/           # 業務邏輯（詳細註解）
├── pkg/                # 可復用的套件
├── docker-compose.yml  # 依賴服務
├── Makefile            # 常用指令
└── go.mod
```

## 貢獻指南

這是一個開源學習專案，歡迎貢獻：

- 提交新的系統設計案例
- 修復 Bug 或改進程式碼
- 改進文件或翻譯

## 推薦資源

### 書籍

- [Designing Data-Intensive Applications](https://dataintensive.net/) - Martin Kleppmann
- [System Design Interview Vol 1 & 2](https://bytebytego.com/) - Alex Xu

### 線上課程

- [Grokking the System Design Interview](https://www.educative.io/courses/grokking-the-system-design-interview)
- [Grokking the Advanced System Design Interview](https://www.educative.io/courses/grokking-adv-system-design-intvw)

### 網站和部落格

- [ByteByteGo Newsletter](https://blog.bytebytego.com/)
- [High Scalability](http://highscalability.com/)
- [System Design Primer](https://github.com/donnemartin/system-design-primer)

## 進度追蹤

- 已完成：3 個案例
- 進行中：1 個案例
- 規劃中：20+ 個案例

查看 [ROADMAP.md](./ROADMAP.md) 了解完整計畫。

## License

MIT License - 詳見 [LICENSE](./LICENSE) 檔案

---

如果這個專案對你有幫助，請給個 Star。

有問題或建議？歡迎開 Issue 討論。
