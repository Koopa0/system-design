# System Design 專案註解與設計指南

> **核心理念**：這是一個**系統設計教學專案**，不是生產級實作範例
>
> **適用範圍**：所有系統設計案例（Counter Service、Room Management、URL Shortener 等）

## 📋 註解撰寫原則

### ✅ 應該寫的註解

**1. 系統設計決策**
```go
// 系統設計考量：
//   - 為什麼用 302 而非 301？
//     → 302 每次都經過服務器，可以統計點擊
//     → 301 被瀏覽器快取，無法追蹤
```

**2. 容量規劃與計算**
```go
// 容量分析：
//   - 7 位 Base62：62^7 = 3.5 兆（3.5 trillion）
//   - 假設每秒 1000 個新 URL，可用 111 年
//   - QPS 目標：10,000（讀） / 1,000（寫）
```

**3. 權衡取捨（Trade-offs）**
```go
// 一致性 vs 性能：
//   - 選擇最終一致性（Eventual Consistency）
//   - 點擊統計允許延遲更新（異步）
//   - 換取更低的重定向延遲（< 10ms）
```

**4. 遇到的問題與解法**
```go
// 問題：同一毫秒內生成大量 ID 會序列號溢出
// 解法：等待下一毫秒（waitNextMillisecond）
// 影響：極端情況下會有短暫阻塞
```

**5. 擴展性考量**
```go
// 水平擴展策略：
//   - 資料庫分片：按 short_code 前綴（一致性哈希）
//   - 快取分層：本地快取 + Redis
//   - CDN：靜態內容加速
```

**6. 面試提示**
```go
// 面試常問：
//   Q: 如何保證短碼唯一性？
//   A: 1) Snowflake ID 本身唯一
//      2) 資料庫 UNIQUE 約束作為最後防線
//      3) 自定義短碼需要檢查衝突
```

### ❌ 不應該寫的註解

**1. Go 語言實作細節**
```go
// ❌ 不要寫：
// Go 慣用法：使用 sync.RWMutex 保護併發訪問

// ✅ 改寫為（如果真的需要）：
// 併發安全：讀寫鎖允許多個讀操作並行
```

**2. 框架/工具選擇說明**
```go
// ❌ 不要寫：
// 使用 net/http 標準庫（不依賴框架）

// ✅ 改寫為：
// HTTP 層設計：標準 REST API，無狀態
```

**3. 代碼結構組織**
```go
// ❌ 不要寫：
// 設計理念：
//   - 遵循 Go 標準庫風格：按功能劃分文件
//   - 不使用 DDD 分層
//   - 簡單直接的函數式 API
```

**4. 純粹的實現說明**
```go
// ❌ 不要寫：
// 使用指針類型表示可選字段（*time.Time）
```

## 📐 系統設計重點清單

### 每個功能都應思考

1. **需求分析**
   - 功能性需求：必須做什麼？
   - 非功能性需求：QPS、延遲、可用性？

2. **容量估算**
   - DAU（日活用戶）
   - QPS（讀/寫比例）
   - 存儲需求（數據量、增長率）
   - 帶寬需求

3. **高層設計**
   - 核心組件有哪些？
   - 數據流向？
   - API 設計？

4. **深入設計**
   - 資料庫 Schema
   - 快取策略（Cache-Aside、Write-Through？）
   - 分片策略（Sharding Key？）

5. **權衡取捨**
   - CAP 定理：選擇 CP 還是 AP？
   - 一致性模型：強一致 vs 最終一致？
   - 讀寫優化：優化讀還是寫？

6. **擴展性**
   - 如何水平擴展？
   - 單點故障（SPOF）？
   - 瓶頸在哪裡？

7. **可靠性**
   - 故障處理
   - 降級策略
   - 監控告警

## 🎯 常見系統設計問題

### URL Shortener - 核心設計問題

1. **如何生成短碼？**
   - 方案 A：哈希（MD5 + Base62）
   - 方案 B：自增 ID + Base62
   - 方案 C：分布式 ID（Snowflake）✅
   - 權衡：唯一性、性能、可擴展性

2. **如何處理高併發讀？**
   - 快取（Redis）
   - CDN
   - 讀寫分離
   - 副本（Replicas）

3. **如何處理寫入？**
   - 異步寫入（消息隊列）
   - 批量寫入
   - 分片寫入

4. **如何統計點擊？**
   - 同步 vs 異步？
   - 精確 vs 近似？
   - 實時 vs 批處理？

5. **如何處理過期？**
   - 主動刪除 vs 惰性刪除？
   - TTL 機制
   - 定期清理任務

6. **如何防止濫用？**
   - 限流（Rate Limiting）
   - 驗證碼
   - 黑名單

### URL Shortener - 擴展問題

7. **如何支援自定義短碼？**
   - 衝突檢測
   - 保留字過濾

8. **如何支援分析功能？**
   - 時序資料庫（InfluxDB、TimescaleDB）
   - 數據倉庫（OLAP）
   - 日誌系統（ELK）

9. **如何處理熱點數據？**
   - 本地快取
   - 多層快取
   - 快取預熱

10. **如何保證高可用？**
    - 多區域部署
    - 故障轉移
    - 健康檢查

## 📊 容量估算模板

```
假設：
- DAU：1000 萬
- 每用戶每天創建：0.1 個短鏈
- 每用戶每天點擊：5 個短鏈

計算：
- 寫 QPS：1000萬 × 0.1 / 86400 ≈ 12 QPS
- 讀 QPS：1000萬 × 5 / 86400 ≈ 580 QPS
- 讀寫比：約 50:1

存儲（5 年）：
- 每天新增：1000萬 × 0.1 = 100 萬條
- 5 年總量：100萬 × 365 × 5 ≈ 18 億條
- 每條記錄：約 200 bytes
- 總存儲：18億 × 200B ≈ 360GB

帶寬：
- 寫入：12 QPS × 200B ≈ 2.4 KB/s
- 讀取：580 QPS × 500B ≈ 290 KB/s（假設返回包更大）
```

## 🔄 迭代設計流程

1. **V1 - MVP（最小可行產品）**
   - 單機版本
   - 內存存儲
   - 基本功能

2. **V2 - 持久化**
   - PostgreSQL
   - 基本索引
   - 簡單監控

3. **V3 - 性能優化**
   - Redis 快取
   - 讀寫分離
   - 連接池

4. **V4 - 高可用**
   - 主從複製
   - 故障轉移
   - 負載均衡

5. **V5 - 水平擴展**
   - 資料庫分片
   - 無狀態服務
   - CDN

## 📝 註解模板

```go
// === 功能名稱 ===
//
// 系統設計問題：
//   為什麼需要這個功能？解決什麼問題？
//
// 設計方案：
//   方案 A：xxx（優點 / 缺點）
//   方案 B：xxx（優點 / 缺點）
//   ✅ 選擇：xxx（理由）
//
// 容量考量：
//   - QPS：xxx
//   - 延遲：xxx
//   - 存儲：xxx
//
// 權衡取捨：
//   選擇 X 而非 Y，因為...
//
// 潛在問題：
//   1. xxx → 解法：xxx
//   2. xxx → 解法：xxx
//
// 擴展方向：
//   - 短期：xxx
//   - 長期：xxx
```

## 🎓 教學目標

讀者閱讀代碼後應該能夠：

1. **理解問題本質**
   - 為什麼短網址服務需要這樣設計？
   - 核心挑戰是什麼？

2. **掌握設計方法論**
   - 如何從需求推導設計？
   - 如何做容量估算？
   - 如何權衡不同方案？

3. **回答面試問題**
   - 系統設計面試的常見問題
   - 如何一步步展開設計
   - 如何深入討論細節

4. **實踐能力**
   - 能夠套用到其他系統設計問題
   - 理解通用的設計模式
   - 知道何時使用何種技術

## 🚫 避免的陷阱

1. ❌ 過度關注代碼實現細節
2. ❌ 糾結於語言特性和慣用法
3. ❌ 展示"最佳實踐"而不解釋為什麼
4. ❌ 只給答案，不說明推導過程
5. ❌ 忽略容量估算和性能指標
6. ❌ 不討論權衡取捨（Trade-offs）
7. ❌ 缺少擴展性討論

## ✅ 應該強調的

1. ✅ 為什麼這樣設計？
2. ✅ 還有哪些其他方案？
3. ✅ 各方案的優缺點？
4. ✅ 在什麼規模下需要改變設計？
5. ✅ 會遇到什麼瓶頸？
6. ✅ 如何監控和調優？
7. ✅ 面試中如何展開討論？

---

## 📚 參考來源

本專案的系統設計方法論和最佳實踐參考以下優秀資源：

### 核心參考

**1. [ByteByteGo](https://bytebytego.com/)**
   - 系統設計視覺化教學
   - Alex Xu 的《System Design Interview》系列
   - 涵蓋：URL Shortener、Rate Limiter、Distributed Cache 等經典案例
   - 本專案採用：視覺化設計流程、容量估算方法

**2. [Grokking the System Design Interview](https://www.designgurus.io/course/grokking-the-system-design-interview)**
   - 系統設計面試準備
   - 涵蓋：15+ 真實系統設計案例
   - 本專案採用：問題分析框架、設計模式

**3. [Designing Data-Intensive Applications (DDIA)](https://dataintensive.net/)**
   - Martin Kleppmann 的經典著作
   - 深入探討：資料系統的基礎原理
   - 本專案採用：CAP 理論、一致性模型、複製與分片策略

### 補充資源

**4. [System Design Primer](https://github.com/donnemartin/system-design-primer)**
   - GitHub 開源學習資源
   - 涵蓋：系統設計基礎概念、演算法、資料結構
   - 適合：初學者建立系統化知識

**5. [Awesome System Design](https://github.com/madd86/awesome-system-design)**
   - 精選系統設計資源集合
   - 包含：文章、影片、工具、案例研究

**6. [High Scalability](http://highscalability.com/)**
   - 真實公司的架構案例
   - 如：Netflix、Uber、Instagram 的系統設計
   - 學習：實戰經驗、架構演進

**7. [Martin Fowler's Blog](https://martinfowler.com/)**
   - 軟體架構大師的見解
   - 涵蓋：微服務、事件驅動、API 設計
   - 經典文章：《Patterns of Enterprise Application Architecture》

**8. [AWS Architecture Blog](https://aws.amazon.com/blogs/architecture/)**
   - 雲端架構最佳實踐
   - 學習：Well-Architected Framework、參考架構

**9. [Google SRE Book](https://sre.google/books/)**
   - Google 的 SRE 實踐
   - 涵蓋：可靠性、監控、容量規劃
   - 免費在線閱讀

**10. [Uber Engineering Blog](https://eng.uber.com/)**
   - Uber 的技術實踐
   - 案例：分布式追蹤、微服務、數據基礎設施

### 學術論文

**11. 經典分布式系統論文**
   - [The Google File System](https://research.google/pubs/pub51/)
   - [MapReduce](https://research.google/pubs/pub62/)
   - [Bigtable](https://research.google/pubs/pub27898/)
   - [Dynamo: Amazon's Highly Available Key-value Store](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf)
   - [Kafka: a Distributed Messaging System](https://notes.stephenholiday.com/Kafka.pdf)

### Go 語言特定

**12. [Effective Go](https://go.dev/doc/effective_go)**
   - Go 官方最佳實踐
   - 本專案遵循的代碼風格基礎

**13. [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)**
   - Go 團隊的代碼審查指南

**14. [Standard Library](https://pkg.go.dev/std)**
   - Go 標準庫源碼
   - 學習：優雅的 API 設計、錯誤處理、並發模式

### 實踐建議

閱讀這些資源時，建議：

1. **先理解概念，再看實作**
   - ByteByteGo：快速建立系統設計思維
   - DDIA：深入理解底層原理
   - 本專案：動手實踐，鞏固知識

2. **對比不同來源的觀點**
   - 同一問題可能有多種解法
   - 理解每種方案的適用場景
   - 培養權衡取捨的能力

3. **關注真實案例**
   - 公司技術博客（Uber、Netflix、Airbnb）
   - 學習架構演進過程
   - 理解為什麼改變設計

4. **動手實踐**
   - 純看不夠，要寫代碼
   - 估算容量、設計 API、畫架構圖
   - 本專案提供可運行的參考實作

### 如何使用本專案

1. **配合 ByteByteGo 學習**
   - 先看 ByteByteGo 的 URL Shortener 章節
   - 理解高層設計（High-Level Design）
   - 再看本專案的代碼實作

2. **對照 Grokking 的問題框架**
   - 需求澄清（Requirements Clarification）
   - 容量估算（Capacity Estimation）
   - API 設計（API Design）
   - 數據模型（Data Model）
   - 高層設計（High-Level Design）
   - 深入設計（Detailed Design）

3. **結合 DDIA 深入理解**
   - 第 5 章：複製（Replication）→ 主從複製
   - 第 6 章：分片（Partitioning）→ 資料庫分片
   - 第 7 章：事務（Transactions）→ 一致性保證
   - 第 8 章：分布式系統的麻煩（Trouble with Distributed Systems）

### 持續學習

系統設計是一個持續演進的領域，建議：

- 訂閱技術博客（如 Hacker News、Medium）
- 關注開源項目（學習實際架構）
- 參與技術討論（理解不同觀點）
- 動手實踐（驗證理論）

**記住**：系統設計沒有完美答案，只有適合場景的權衡取捨。
