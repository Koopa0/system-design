# 系統設計專案重構總結

> **時間**: 2025年1月
> **範圍**: 01-counter-service, 02-room-management, 03-url-shortener
> **目標**: 將代碼註解從實作導向轉變為系統設計導向

---

## 📋 重構內容

### 1. URL Shortener (03-url-shortener) - 結構重構

**移除的 DDD 模式**:
- ❌ `internal/shortener/service.go` (Service 層)
- ❌ 基於方法的 API (`s.CreateShortURL()`)
- ❌ DDD 術語 (Service, Repository, Entity)

**新的 Go 標準庫風格組織**:
```
internal/shortener/
├── types.go       # 數據類型與錯誤定義
├── store.go       # 存儲接口
├── shorten.go     # 短網址生成邏輯
├── resolve.go     # 短網址解析邏輯
└── stats.go       # 統計功能

internal/storage/
├── memory.go      # V1: 內存實現（開發用）
└── postgres.go    # V2: PostgreSQL 實現（生產用）
```

**設計模式轉變**:
- ✅ 函數式 API: `shortener.Shorten(ctx, store, idgen, ...)`
- ✅ 明確的依賴注入（作為參數傳遞）
- ✅ 按功能劃分文件，而非架構層

### 2. Counter Service (01-counter-service) - 註解重構

**轉變重點**: 實作細節 → 系統設計思維

| 重構前 | 重構後 |
|--------|--------|
| "使用 Redis INCR 命令" | "為什麼用 Redis？原子性保證 + 微秒級延遲" |
| "批量寫入 PostgreSQL" | "批量優化：10,000 QPS → 100 次 DB 寫入" |
| "降級到 PostgreSQL" | "高可用設計：Redis 故障 → PostgreSQL 接管" |

**新增系統設計註解**:
- 容量規劃 (10,000 QPS 處理)
- 權衡取捨 (最終一致性 vs 強一致性)
- 高可用架構 (Redis → PostgreSQL 降級)
- 批量優化原理 (緩衝 + 合併)

### 3. Room Management (02-room-management) - FSM 與並發

**新增核心註解**:

**有限狀態機 (FSM)**:
```
waiting → preparing → ready → playing → finished
             ↑____________↓
```
- 狀態轉換規則
- 並發安全保證
- 事件驅動架構

**WebSocket 心跳機制** (最關鍵的設計細節):
- **為什麼 54 秒 Ping?** 避開代理服務器的 60 秒超時
- **為什麼 60 秒超時?** 配合 54 秒 Ping + 6 秒余量
- **為什麼用 Ping/Pong?** WebSocket 原生支持，更高效

---

## 🎯 設計原則確立

### 代碼組織原則

✅ **DO - 遵循 Go 標準庫風格**:
- 按功能劃分文件 (`shorten.go`, `resolve.go`)
- 函數式 API，明確依賴
- 使用純 Go 動詞 (`Load`, `Save`, `Shorten`)

❌ **DON'T - 避免的模式**:
- DDD 分層 (Service, Repository, Entity)
- OOP 術語 (GetUser, SetUser)
- 框架依賴 (Gin, Echo) - 僅用 `net/http`

### 註解撰寫原則

**應該寫** (System Design):
- 為什麼這樣設計？
- 有哪些替代方案？
- 權衡取捨是什麼？
- 容量如何規劃？
- 如何擴展？

**不應該寫** (Implementation Details):
- Go 語言特性說明
- 框架選擇理由
- 純粹的實作細節

---

## 📊 完成狀態

| 案例 | 結構重構 | 註解重構 | 狀態 |
|------|----------|----------|------|
| 01-counter-service | N/A | ✅ | ✅ 已完成 |
| 02-room-management | N/A | ✅ | ✅ 已完成 |
| 03-url-shortener | ✅ | ✅ | ✅ 已完成 |

**提交記錄**:
- `c4dc6b3` - URL Shortener 結構重構 (Go stdlib 風格)
- `73c5abc` - URL Shortener 學習指南 (LEARNING.md)
- `7b2ff2f` - Counter Service 系統設計註解
- `2e43d57` - Room Management 系統設計註解
- `7f10bcc` - WebSocket 心跳機制註解

---

## 🚀 下一步建議

### Phase 1: 完善現有案例 (短期)

**測試補充**:
- [ ] 01-counter-service: 併發測試 (1000 goroutines)
- [ ] 02-room-management: 狀態機轉換測試
- [ ] 03-url-shortener: Base62 編碼/解碼測試

**功能增強**:
- [ ] 03-url-shortener: 實作 Redis 快取層 (V3 架構)
- [ ] 01-counter-service: 自動重置機制 (每日凌晨)
- [ ] 02-room-management: 房間超時自動關閉

**文檔完善**:
- [ ] 每個案例補充 API 使用範例
- [ ] 添加性能測試結果 (wrk/ab)
- [ ] 補充架構演進圖 (V1 → V2 → V3)

### Phase 2: 新增基礎案例 (中期)

**建議順序**:
1. **Rate Limiter** (限流器)
   - 核心概念: 令牌桶、漏桶、固定窗口、滑動窗口
   - 系統設計: 分布式限流、Redis + Lua
   - 難度: ⭐️⭐️

2. **Distributed Cache** (分布式快取)
   - 核心概念: LRU/LFU、一致性哈希
   - 系統設計: 快取策略、快取穿透/擊穿/雪崩
   - 難度: ⭐️⭐️

3. **Message Queue** (消息隊列)
   - 核心概念: Pub/Sub、消費者組、死信隊列
   - 系統設計: 消息可靠性、順序保證、冪等性
   - 難度: ⭐️⭐️⭐️

### Phase 3: 進階案例 (長期)

**數據密集型**:
- Search Autocomplete (搜尋自動完成)
- Web Crawler (網路爬蟲)
- Metrics Monitoring (指標監控)

**社交媒體**:
- News Feed (動態消息流)
- Chat System (聊天系統)
- Notification Service (通知服務)

---

## 📚 學習資源整合

**已建立的學習路徑**:
1. 閱讀 `README.md` - 理解問題背景
2. 閱讀 `LEARNING.md` - 系統設計深度分析 (僅 03-url-shortener 有)
3. 閱讀源碼 - 實作細節與註解
4. 運行測試 - 驗證功能
5. 擴展練習 - 動手實踐

**參考資源** (已整合到 `.claude/design-guidelines.md`):
- [ByteByteGo](https://bytebytego.com/) - 視覺化系統設計
- [Grokking System Design](https://www.designgurus.io/) - 面試準備
- [DDIA](https://dataintensive.net/) - 深度理論
- [Effective Go](https://go.dev/doc/effective_go) - Go 最佳實踐

---

## 🎓 教學成果

完成重構後，學習者應該能夠:

✅ **理解系統設計思維**:
- 從需求推導設計
- 容量估算 (Back-of-the-envelope)
- 權衡不同方案 (Trade-offs)

✅ **掌握 Go 最佳實踐**:
- 標準庫風格的代碼組織
- 函數式 API 設計
- 並發安全模式

✅ **準備系統設計面試**:
- 經典案例的設計流程
- 如何深入討論細節
- 常見面試問題應對

✅ **實踐能力**:
- 套用到其他系統設計問題
- 理解通用設計模式
- 知道何時使用何種技術

---

## 💡 關鍵洞察

### 1. 註解的價值在於「為什麼」

❌ **低價值**: "使用 sync.RWMutex"
✅ **高價值**: "讀多寫少場景 → RWMutex 允許並發讀"

### 2. 容量規劃是系統設計的基礎

每個功能都應該回答:
- 預期 QPS 是多少？
- 延遲要求是什麼？
- 存儲需求如何計算？

### 3. 權衡取捨比完美方案更重要

展示思考過程:
- 方案 A: xxx (優點/缺點)
- 方案 B: xxx (優點/缺點)
- 選擇: xxx (理由)

### 4. 架構演進比一步到位更實際

V1 (MVP) → V2 (持久化) → V3 (快取) → V4 (高可用) → V5 (水平擴展)

---

**總結**: 本次重構成功將專案從「可運行的代碼範例」提升為「系統設計教學專案」，為後續案例建立了清晰的設計原則和註解標準。
