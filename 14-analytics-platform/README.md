# Analytics Platform - 分析平台

> 從 OLTP 到 OLAP：構建秒級響應的實時分析平台

## 概述

本章節展示如何設計一個高性能的分析平台，支持：
- **實時分析**：秒級數據延遲
- **複雜查詢**：多維度聚合、JOIN
- **大規模數據**：TB - PB 級數據
- **高性能**：P99 查詢延遲 < 1 秒

## 學習目標

- 理解 **OLTP vs OLAP** 的本質區別
- 掌握**列式存儲**的原理和優勢
- 學習 **Lambda 架構** vs **Kappa 架構**
- 實踐**物化視圖**優化查詢性能
- 了解 **ClickHouse、Flink、Kafka** 的使用場景

## 核心概念

### 1. OLTP vs OLAP

| 特性 | OLTP (交易處理) | OLAP (分析處理) |
|------|----------------|----------------|
| 查詢類型 | 點查詢 (WHERE id = 123) | 聚合查詢 (GROUP BY, SUM) |
| 讀寫比例 | 50/50 | 95/5 (讀多寫少) |
| 掃描行數 | 少量 (1-1000) | 大量 (百萬-億) |
| 存儲格式 | 行式存儲 | 列式存儲 |
| 代表產品 | PostgreSQL, MySQL | ClickHouse, Druid |

### 2. 列式存儲 vs 行式存儲

**行式存儲**（OLTP）：
```
Row 1: [id:1, user:1001, amount:299, province:"台北", ...]
Row 2: [id:2, user:1002, amount:599, province:"台中", ...]
```

**列式存儲**（OLAP）：
```
id:       [1, 2, 3, ...]
user:     [1001, 1002, 1003, ...]
amount:   [299, 599, 199, ...]
province: ["台北", "台中", "台北", ...]
```

**優勢**：
- 只讀取需要的列（減少 I/O）
- 列數據相似度高（壓縮率高 10:1）
- 向量化執行（SIMD 加速）

### 3. Lambda vs Kappa 架構

**Lambda 架構**：
```
批處理層 (Spark) + 速度層 (Flink) + 查詢層
優勢：批處理性能高
劣勢：代碼重複，維護成本高
```

**Kappa 架構**：
```
流處理層 (Flink) + 查詢層
優勢：架構簡單，一套代碼
劣勢：依賴 Kafka 持久化
```

### 4. 物化視圖

預聚合常用查詢，查詢速度提升 100-1000 倍：

```sql
-- 創建物化視圖
CREATE MATERIALIZED VIEW mv_daily_sales AS
SELECT category, order_date, SUM(amount) as daily_sales
FROM fact_orders
GROUP BY category, order_date;

-- 查詢（0.01 秒 vs 原本 5 秒）
SELECT * FROM mv_daily_sales WHERE order_date = today();
```

## 技術棧

- **OLAP 數據庫**: ClickHouse (列式存儲)
- **流處理引擎**: Apache Flink
- **消息隊列**: Apache Kafka
- **快取**: Redis
- **BI 工具**: Metabase, Grafana

## 架構演進

### 階段 1：PostgreSQL (失敗)
- ❌ 查詢超時 (60+ 秒)
- ❌ 無法支持複雜聚合

### 階段 2：ClickHouse + ETL
- ✅ 查詢速度提升 174 倍
- ❌ 數據延遲 1 小時

### 階段 3：Lambda 架構
- ✅ 實時性：秒級延遲
- ❌ 維護兩套代碼

### 階段 4：Kappa 架構 (最終)
- ✅ 架構簡單，一套代碼
- ✅ 實時性：< 5 秒延遲
- ✅ 查詢性能：P99 < 1 秒

## 性能指標

```
數據規模：
- 5,000 萬訂單/月
- 500 GB/月 (壓縮後)

查詢性能：
- 簡單聚合：0.05 秒
- 複雜 JOIN：0.5 秒
- 物化視圖：0.01 秒

實時性：
- 數據延遲：< 5 秒 (P99)
```

## 項目結構

```
14-analytics-platform/
├── DESIGN.md           # 詳細設計文檔（蘇格拉底式教學）
├── README.md           # 本文件
├── etl/                # ETL 腳本
│   ├── extract.py      # 從 OLTP 提取數據
│   ├── transform.py    # 數據轉換清洗
│   └── load.py         # 載入到 ClickHouse
├── flink/              # Flink 流處理
│   └── KappaAnalytics.java
├── serving/            # 查詢服務
│   └── query.py
└── docs/               # 補充文檔
    ├── clickhouse-setup.md
    ├── flink-deployment.md
    └── performance-tuning.md
```

## 快速開始

### 1. 啟動 ClickHouse

```bash
docker run -d \
  --name clickhouse \
  -p 8123:8123 \
  -p 9000:9000 \
  clickhouse/clickhouse-server
```

### 2. 創建表

```sql
-- 連接 ClickHouse
clickhouse-client

-- 創建事實表
CREATE TABLE fact_orders (
    order_id UInt64,
    user_id UInt32,
    amount Decimal(10, 2),
    province String,
    category String,
    created_at DateTime
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(created_at)
ORDER BY (created_at, province);

-- 創建物化視圖
CREATE MATERIALIZED VIEW mv_daily_sales
ENGINE = SummingMergeTree()
ORDER BY (category, order_date)
AS
SELECT
    category,
    toDate(created_at) as order_date,
    SUM(amount) as daily_sales,
    COUNT(*) as order_count
FROM fact_orders
GROUP BY category, order_date;
```

### 3. 測試查詢

```sql
-- 查詢今日各類目銷售額
SELECT
    category,
    SUM(daily_sales) as total_sales,
    SUM(order_count) as total_orders
FROM mv_daily_sales
WHERE order_date = today()
GROUP BY category
ORDER BY total_sales DESC;
```

## 關鍵設計決策

### 為什麼選擇 ClickHouse？

| 對比項 | ClickHouse | Druid | Pinot |
|-------|-----------|-------|-------|
| 查詢速度 | 極快 | 快 | 快 |
| SQL 支持 | 完善 | 有限 | 有限 |
| 運維難度 | 中等 | 高 | 高 |
| 壓縮率 | 10:1 | 8:1 | 8:1 |
| 生態 | 豐富 | 中等 | 中等 |

**結論**：ClickHouse SQL 支持完善，查詢速度快，適合大多數分析場景。

### 為什麼選擇 Kappa 而非 Lambda？

- ✅ 架構簡單，維護成本低
- ✅ 一套代碼，避免邏輯不一致
- ✅ Kafka 可保留 90 天數據，支持重新計算
- ✅ 數據規模 < 10 PB，無需單獨批處理層

## 常見問題

### Q1: ClickHouse vs PostgreSQL 有多大差距？

**測試**：查詢過去 1 小時的平均 CPU

- PostgreSQL: 8.7 秒 (全表掃描 72 萬行)
- ClickHouse: 0.05 秒 (列式存儲 + 壓縮)

**提升**：174 倍

### Q2: 物化視圖會佔用多少存儲？

**示例**：
- 原始表：5,000 萬行，500 GB
- 物化視圖：300 行 (30 天 × 10 類目)，30 KB

**增加存儲**：可忽略不計

### Q3: 數據延遲有多低？

**Kappa 架構**：
- Kafka → Flink → ClickHouse
- 延遲：< 5 秒 (P99)

### Q4: 成本如何？

**AWS 月成本**：約 $3,074
- ClickHouse: $1,614
- Kafka: $438
- Flink: $730
- Redis: $292

**ROI**：16 倍以上（業務價值 > $50,000/月）

## 延伸閱讀

### 開源項目

- [ClickHouse](https://github.com/ClickHouse/ClickHouse) - 高性能列式數據庫
- [Apache Flink](https://github.com/apache/flink) - 流處理引擎
- [Apache Kafka](https://github.com/apache/kafka) - 分布式消息隊列
- [Apache Druid](https://github.com/apache/druid) - 實時 OLAP 數據庫
- [Apache Pinot](https://github.com/apache/pinot) - 實時分析平台

### 論文與文章

- **Lambda Architecture** (Nathan Marz, 2011)
- **Kappa Architecture** (Jay Kreps, 2014)
- **Dremel: Interactive Analysis of Web-Scale Datasets** (Google, 2010)
- **Gorilla: A Fast, Scalable, In-Memory Time Series Database** (Facebook, 2015)

### 相關章節

- **12-metrics-monitoring**: 時序數據庫（Prometheus）
- **13-distributed-kv-store**: 分布式存儲（Dynamo）
- **07-message-queue**: 消息隊列（NATS）
- **09-event-driven**: 事件驅動架構

## 總結

從 CEO 的「5 分鐘靈魂拷問」到秒級實時分析平台，我們學到了：

1. **OLTP ≠ OLAP**：交易處理和分析處理需要不同的數據庫
2. **列式存儲**：針對分析查詢優化，性能提升 100+ 倍
3. **Kappa 架構**：簡化 Lambda，一套代碼統一批流處理
4. **物化視圖**：空間換時間，查詢加速 100-1000 倍
5. **選對工具**：Right tool for the right job

**記住：不要用跑車拉貨，也不要用卡車飆速！** 🏎️🚚
