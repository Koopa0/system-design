# 系統設計學習路線圖

> 從基礎到高級，從簡單到複雜的系統設計學習路徑

## 總覽

本路線圖涵蓋 **40 個系統設計案例**，按難度和主題分為 8 個階段。建議按順序學習，每個階段都建立在前一階段的基礎上。

### 學習時間估算

- **初級（Phase 1-2）**: 2-3 個月
- **中級（Phase 3-4）**: 2-3 個月
- **高級（Phase 5-8）**: 3-4 個月

**總計**: 約 6-10 個月（每週投入 10-15 小時）

---

## Phase 1: 基礎組件 (Foundation)

> 所有大型系統的基石，必須熟練掌握

### 01. Counter Service
- **難度**: 1 星
- **時間**: 1-2 週
- **核心概念**: Redis、PostgreSQL、降級機制、批量優化
- **學習重點**:
  - Redis 原子操作（INCR, INCRBY）
  - 雙寫策略（Redis + DB）
  - 降級方案設計
  - 批量寫入優化
- **應用場景**:
  - 網站 PV/UV 統計
  - 遊戲在線人數
  - API 調用次數統計
  - 電商訂單數量
- **ByteByteGo**: Design a Metrics Monitoring System
- **DDIA**: Chapter 5 - Replication

### 02. Room Management
- **難度**: 2 星
- **時間**: 2-3 週
- **核心概念**: WebSocket、狀態機、並發控制、事件驅動
- **學習重點**:
  - WebSocket 長連接管理
  - 狀態機設計（有限狀態自動機）
  - 併發安全（sync.RWMutex）
  - 事件廣播機制
- **應用場景**:
  - 遊戲房間/大廳
  - 視訊會議室（Zoom）
  - 協作空間（Google Docs）
  - 聊天頻道（Discord）
- **ByteByteGo**: Design a Chat System (部分)
- **相關**: WebRTC、CRDT

### 03. URL Shortener
- **難度**: 1 星
- **時間**: 1-2 週
- **核心概念**: 分布式 ID、Base62 編碼、快取策略
- **學習重點**:
  - ID 生成策略對比（自增、UUID、Snowflake）
  - Base62 編碼原理
  - Cache-Aside 模式
  - 容量估算（Back-of-the-envelope）
- **應用場景**:
  - 短網址服務（bit.ly）
  - QR Code 生成
  - 追蹤鏈接（Marketing）
- **ByteByteGo**: Design a URL Shortener
- **Grokking**: Chapter 2
- **DDIA**: Chapter 6 - Partitioning

### 04. Rate Limiter
- **難度**: 2 星
- **時間**: 1-2 週
- **核心概念**: 限流算法、分布式限流、Redis Lua
- **學習重點**:
  - Token Bucket vs Leaky Bucket vs Sliding Window
  - 分布式限流（Redis + Lua 保證原子性）
  - 多維度限流（IP、用戶、API）
  - 限流響應策略（拒絕 vs 延遲）
- **應用場景**:
  - API Gateway 限流
  - 防止 DDoS 攻擊
  - 保護下游服務
  - 公平性保證
- **ByteByteGo**: Design a Rate Limiter
- **DDIA**: Chapter 11 - Stream Processing

### 05. Distributed Cache
- **難度**: 2 星
- **時間**: 2-3 週
- **核心概念**: LRU/LFU、一致性哈希、快取策略
- **學習重點**:
  - 快取淘汰算法（LRU、LFU、ARC）
  - 一致性哈希（解決節點增減問題）
  - 快取穿透、雪崩、擊穿
  - 快取更新策略（Cache-Aside、Write-Through、Write-Back）
- **應用場景**:
  - Redis Cluster
  - Memcached
  - CDN
- **ByteByteGo**: Design a Key-Value Store 
- **DDIA**: Chapter 5 - Replication

### 06. Unique ID Generator
- **難度**: 2 星
- **時間**: 1 週
- **核心概念**: Snowflake、時鐘同步、分布式協調
- **學習重點**:
  - Snowflake 算法詳解
  - 時鐘回撥問題
  - 機器 ID 分配
  - UUID vs ULID vs Snowflake
- **應用場景**:
  - 訂單號生成
  - 用戶 ID
  - 分布式追蹤（Trace ID）
- **ByteByteGo**: Design a Unique ID Generator

---

## Phase 1.5: 消息與事件 (Messaging & Events)

> Phase 1 到 Phase 2 的過渡，引入異步處理、事件驅動架構

### 07. Message Queue
- **難度**: 2 星
- **時間**: 2-3 週
- **核心概念**: NATS JetStream、At-least-once、Queue Groups
- **學習重點**:
  - 消息隊列選型（NATS vs Kafka vs RabbitMQ vs Redis）
  - At-least-once 語義保證
  - Queue Groups 負載均衡
  - 消息持久化與重試機制
- **應用場景**:
  - 微服務異步通訊
  - 任務隊列（郵件、報表）
  - 削峰填谷（秒殺系統）
  - 事件驅動架構
- **ByteByteGo**: Design a Message Queue
- **DDIA**: Chapter 11 - Stream Processing
- **技術棧**: NATS JetStream（輕量級、高性能）

### 08. Task Scheduler
- **難度**: 2 星
- **時間**: 2-3 週
- **核心概念**: 時間輪算法、延遲隊列、Cron 表達式
- **學習重點**:
  - 時間輪算法（Netty、Kafka 使用）
  - 延遲任務調度（訂單超時取消）
  - 定時任務（Cron 解析）
  - 分布式調度（避免重複執行）
- **應用場景**:
  - 訂單超時處理（30 分鐘未支付）
  - 定時報表生成（每日凌晨）
  - 會議室預訂釋放（2 小時）
  - 週期性任務（數據同步）
- **ByteByteGo**: Design a Task Scheduler
- **算法**: Timing Wheel（O(1) 插入與觸發）
- **技術棧**: 時間輪 + NATS JetStream（持久化）

### 09. Event-Driven Architecture
- **難度**: 3 星
- **時間**: 3-4 週
- **核心概念**: Event Sourcing、CQRS、Saga 模式
- **學習重點**:
  - Event Sourcing（事件溯源）
  - CQRS（讀寫分離）
  - Saga 模式（分布式事務協調）
  - 事件重播與狀態重建
- **應用場景**:
  - 微服務事件驅動
  - 訂單處理流程（下單 → 扣庫存 → 扣款）
  - 完整審計歷史
  - 複雜業務流程編排
- **ByteByteGo**: Design an Event-Driven System
- **DDIA**: Chapter 11 - Stream Processing
- **模式**: Event Sourcing、CQRS、Saga Choreography
- **技術棧**: NATS JetStream（Event Store）

---

## Phase 2: 數據密集型 (Data-Intensive)

> 處理海量數據、搜尋、分析

### 10. Search Autocomplete
- **難度**: 2 星
- **時間**: 2-3 週
- **核心概念**: Trie 樹、前綴匹配、Elasticsearch
- **學習重點**:
  - Trie（前綴樹）數據結構
  - Top K 熱門查詢
  - 前綴匹配優化
  - 拼寫糾正（Levenshtein Distance）
- **應用場景**:
  - Google 搜尋建議
  - IDE 代碼補全
  - 命令行 Autocomplete
- **ByteByteGo**: Design a Search Autocomplete
- **DDIA**: Chapter 3 - Storage and Retrieval

### 11. Web Crawler
- **難度**: 3 星
- **時間**: 3-4 週
- **核心概念**: 分布式爬蟲、URL Frontier、去重、禮貌性
- **學習重點**:
  - URL Frontier（BFS vs 優先級隊列）
  - 去重（Bloom Filter）
  - Robots.txt 和禮貌性（Politeness）
  - DNS 查詢優化
- **應用場景**:
  - Google 爬蟲
  - 價格監控
  - 輿情分析
- **ByteByteGo**: Design a Web Crawler
- **DDIA**: Chapter 10 - Batch Processing

### 12. Distributed KV Store
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: CAP、Dynamo、向量時鐘、Gossip 協議
- **學習重點**:
  - CAP 定理實踐
  - 一致性哈希 + 虛擬節點
  - 衝突解決（向量時鐘）
  - Quorum 讀寫（W + R > N）
- **應用場景**:
  - Amazon Dynamo
  - Cassandra
  - Riak
- **ByteByteGo**: Design a Key-Value Store
- **DDIA**: Chapter 5, 9

### 13. Metrics Monitoring
- **難度**: 3 星
- **時間**: 3-4 週
- **核心概念**: 時序數據、聚合、Prometheus、InfluxDB
- **學習重點**:
  - 時序數據存儲優化
  - 下採樣（Downsampling）
  - 聚合查詢（Sum、Avg、P99）
  - 告警規則引擎
- **應用場景**:
  - Prometheus + Grafana
  - DataDog
  - New Relic
- **ByteByteGo**: Design a Metrics Monitoring System
- **DDIA**: Chapter 3 - Storage

### 14. Analytics Platform
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: OLAP、數據倉庫、ClickHouse、Druid
- **學習重點**:
  - OLTP vs OLAP
  - 列式存儲優化
  - 物化視圖（Materialized View）
  - Lambda 架構 vs Kappa 架構
- **應用場景**:
  - Google Analytics
  - 商業智能（BI）
  - 用戶行為分析
- **DDIA**: Chapter 10 - Batch Processing

---

## Phase 3: 社交媒體 (Social Media)

> 高並發、實時性、社交關係

### 15. News Feed (Twitter/Facebook)
- **難度**: 3 星
- **時間**: 3-4 週
- **核心概念**: Feed 生成、推拉模型、Fanout
- **學習重點**:
  - Push（Fanout-on-Write）vs Pull（Fanout-on-Read）
  - 混合模式（明星賬號用 Pull）
  - Feed 排序算法
  - 分頁和游標
- **應用場景**:
  - Twitter 時間線
  - Facebook News Feed
  - Instagram Feed
- **ByteByteGo**: Design a News Feed System
- **DDIA**: Chapter 11 - Stream Processing

### 16. Chat System (WhatsApp/WeChat)
- **難度**: 3 星
- **時間**: 4-5 週
- **核心概念**: 1對1、群聊、離線訊息、已讀回執
- **學習重點**:
  - WebSocket vs Long Polling
  - 訊息同步機制
  - 離線訊息存儲
  - 群聊的 Fanout 問題
- **應用場景**:
  - WhatsApp
  - Telegram
  - Slack
- **ByteByteGo**: Design a Chat System
- **擴展**: 端到端加密（Signal Protocol）

### 17. Notification Service
- **難度**: 2 星
- **時間**: 2-3 週
- **核心概念**: 多渠道推送、訂閱、去重、優先級
- **學習重點**:
  - Email、SMS、Push 的統一抽象
  - 訂閱管理（偏好設置）
  - 去重（同一通知不重複發送）
  - 重試和失敗處理
- **應用場景**:
  - Firebase Cloud Messaging
  - AWS SNS
  - 運營通知系統
- **ByteByteGo**: Design a Notification System

### 18. Instagram
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: 圖片上傳、CDN、圖片處理、社交圖譜
- **學習重點**:
  - 圖片存儲（S3）
  - 圖片壓縮和多尺寸
  - CDN 分發
  - 點贊和評論的一致性
- **應用場景**:
  - Instagram
  - Pinterest
  - 圖片社交平台
- **相關**: Feed 系統（Chapter 15）

### 19. Live Comment System
- **難度**: 3 星
- **時間**: 2-3 週
- **核心概念**: 實時評論、彈幕、高並發寫入
- **學習重點**:
  - WebSocket 廣播優化
  - 削峰填谷（消息隊列）
  - 敏感詞過濾
  - 彈幕碰撞檢測
- **應用場景**:
  - Twitch 直播
  - YouTube Live
  - Bilibili 彈幕
- **技術**: Kafka、Redis Pub/Sub

---

## Phase 4: 媒體平台 (Media Platforms)

> 大文件、串流、轉碼

### 20. YouTube
- **難度**: 4 星
- **時間**: 5-6 週
- **核心概念**: 影片上傳、轉碼、CDN、推薦算法
- **學習重點**:
  - 分片上傳（Chunked Upload）
  - 影片轉碼（FFmpeg）
  - CDN 分發策略
  - 推薦算法（協同過濾）
- **應用場景**:
  - YouTube
  - TikTok
  - 影片平台
- **ByteByteGo**: Design YouTube
- **DDIA**: Chapter 10 - Batch Processing

### 21. Netflix
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: 串流、自適應碼率、預加載
- **學習重點**:
  - HLS / DASH 協議
  - 自適應碼率（ABR）
  - 預加載策略
  - 離線下載
- **應用場景**:
  - Netflix
  - Disney+
  - 串流平台
- **ByteByteGo**: Design Netflix

### 22. Spotify
- **難度**: 3 星
- **時間**: 3-4 週
- **核心概念**: 音樂串流、播放列表、推薦、社交
- **學習重點**:
  - 音頻編碼（Ogg Vorbis）
  - 播放列表同步
  - 歌曲推薦算法
  - 離線播放
- **應用場景**:
  - Spotify
  - Apple Music
  - 音樂平台
- **ByteByteGo**: Design Spotify

### 23. Google Drive
- **難度**: 4 星
- **時間**: 5-6 週
- **核心概念**: 文件存儲、同步、版本控制、衝突解決
- **學習重點**:
  - 分塊存儲（Chunking）+ 去重
  - 增量同步（Delta Sync）
  - 版本控制（Git-like）
  - 衝突解決（OT vs CRDT）
- **應用場景**:
  - Google Drive
  - Dropbox
  - OneDrive
- **ByteByteGo**: Design Google Drive
- **相關**: CRDT 論文

---

## Phase 5: 位置服務 (Location-Based)

> 地理空間、路線規劃、實時追蹤

### 24. Uber/Lyft
- **難度**: 4 星
- **時間**: 5-6 週
- **核心概念**: 司機匹配、路徑規劃、實時追蹤、動態定價
- **學習重點**:
  - Geohash / QuadTree / S2
  - 司機匹配算法
  - ETA 計算
  - 動態定價（Surge Pricing）
- **應用場景**:
  - Uber
  - Lyft
  - 叫車服務
- **ByteByteGo**: Design Uber

### 25. Google Maps
- **難度**: 5 星
- **時間**: 6-8 週
- **核心概念**: 地圖渲染、路線規劃、導航、路況預測
- **學習重點**:
  - 地圖瓦片（Tile System）
  - Dijkstra / A* 算法
  - 路況數據收集
  - 實時導航語音
- **應用場景**:
  - Google Maps
  - 高德地圖
  - 導航系統
- **ByteByteGo**: Design Google Maps

### 26. Yelp (附近的餐廳)
- **難度**: 3 星
- **時間**: 3-4 週
- **核心概念**: 地理空間索引、QuadTree、範圍查詢
- **學習重點**:
  - QuadTree vs Geohash vs S2
  - k-NN 查詢
  - 空間索引優化
  - 評分排序
- **應用場景**:
  - Yelp
  - 大眾點評
  - 附近功能
- **ByteByteGo**: Design Yelp

### 27. Food Delivery (UberEats)
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: 訂單匹配、調度優化、多點取送
- **學習重點**:
  - 外送員匹配算法
  - 多訂單打包（Batching）
  - 路線優化（TSP 問題）
  - ETA 預測
- **應用場景**:
  - UberEats
  - 美團外賣
  - DoorDash

---

## Phase 6: 電商交易 (E-Commerce)

> 高並發交易、庫存、支付

### 28. Flash Sale (秒殺系統)
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: 庫存扣減、超賣問題、削峰填谷
- **學習重點**:
  - Redis 扣庫存（Lua 保證原子性）
  - 樂觀鎖 vs 悲觀鎖
  - 消息隊列削峰
  - 防止黃牛（限流 + 驗證碼）
- **應用場景**:
  - 淘寶雙11
  - 小米搶購
  - 演唱會搶票
- **ByteByteGo**: Design a Flash Sale System
- **DDIA**: Chapter 7 - Transactions

### 29. Payment System
- **難度**: 4 星
- **時間**: 5-6 週
- **核心概念**: 雙寫、冪等性、對帳、分布式事務
- **學習重點**:
  - 冪等性設計（防重複支付）
  - 雙寫一致性
  - 對帳系統（T+1）
  - 分布式事務（Saga）
- **應用場景**:
  - 支付寶
  - PayPal
  - Stripe
- **ByteByteGo**: Design a Payment System
- **DDIA**: Chapter 7, 9 - Transactions

### 30. Stock Exchange
- **難度**: 5 星
- **時間**: 6-8 週
- **核心概念**: 訂單匹配、撮合引擎、低延遲
- **學習重點**:
  - 訂單簿（Order Book）
  - 價格-時間優先算法
  - 低延遲優化（納秒級）
  - 市價單 vs 限價單
- **應用場景**:
  - 證券交易所
  - 加密貨幣交易所
  - 高頻交易
- **ByteByteGo**: Design a Stock Exchange

### 31. Hotel Reservation
- **難度**: 3 星
- **時間**: 3-4 週
- **核心概念**: 分布式鎖、庫存管理、超賣問題
- **學習重點**:
  - 分布式鎖（Redis / etcd）
  - 庫存預扣
  - 訂單超時取消
  - 併發控制
- **應用場景**:
  - Booking.com
  - Airbnb
  - 酒店預訂

---

## Phase 7: AI 平台 (AI Platforms)

> 新興的 AI 系統設計

### 32. ChatGPT-like System
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: LLM API、流式輸出、上下文管理、Token 計費
- **學習重點**:
  - 流式輸出（Server-Sent Events）
  - 上下文窗口管理
  - Token 計數和計費
  - 併發限制（GPU 資源）
- **應用場景**:
  - ChatGPT
  - Claude
  - 對話式 AI
- **ByteByteGo**: Design ChatGPT
- **相關**: Transformer 架構

### 33. AI Agent Platform
- **難度**: 5 星
- **時間**: 6-8 週
- **核心概念**: Agent 編排、Tool Calling、狀態管理、多 Agent 協作
- **學習重點**:
  - Agent 工作流（ReAct、Chain-of-Thought）
  - Tool Calling（函數調用）
  - 多 Agent 通訊協議
  - 狀態持久化
- **應用場景**:
  - LangChain
  - AutoGPT
  - Agent 平台
- **ByteByteGo**: Design an AI Agent Platform（新）
- **相關**: LangChain、LlamaIndex

### 34. RAG System (檢索增強生成)
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: 向量數據庫、Embedding、語義搜尋、上下文注入
- **學習重點**:
  - 文檔切分（Chunking）
  - Embedding 模型（BERT、OpenAI）
  - 向量檢索（Pinecone、Weaviate、Milvus）
  - 重排序（Reranking）
- **應用場景**:
  - 企業知識庫
  - 文檔問答
  - 客服機器人
- **技術**: Pinecone、LlamaIndex、FAISS

### 35. Model Training Platform
- **難度**: 5 星
- **時間**: 6-8 週
- **核心概念**: 分布式訓練、資源調度、GPU 集群、模型版本管理
- **學習重點**:
  - 分布式訓練（數據並行 vs 模型並行）
  - GPU 資源調度（Kubernetes + NVIDIA GPU）
  - 實驗追蹤（MLflow、W&B）
  - 模型版本管理
- **應用場景**:
  - Google Colab
  - AWS SageMaker
  - 訓練平台
- **相關**: Kubeflow、Ray

### 36. Recommendation Engine
- **難度**: 4 星
- **時間**: 5-6 週
- **核心概念**: 協同過濾、內容推薦、特徵工程、實時推薦
- **學習重點**:
  - 協同過濾（User-based vs Item-based）
  - 矩陣分解（SVD、ALS）
  - 深度學習推薦（Wide & Deep）
  - 實時特徵計算
- **應用場景**:
  - YouTube 推薦
  - Amazon 商品推薦
  - Netflix 影片推薦
- **ByteByteGo**: Design a Recommendation System
- **DDIA**: Chapter 10 - Batch Processing

---

## Phase 8: 進階主題 (Advanced)

> 分布式系統核心理論

### 37. Distributed Transaction
- **難度**: 5 星
- **時間**: 6-8 週
- **核心概念**: 2PC、Saga、TCC、最終一致性
- **學習重點**:
  - 兩階段提交（2PC）的問題
  - Saga 模式（編排 vs 協調）
  - TCC（Try-Confirm-Cancel）
  - 事件溯源（Event Sourcing）
- **應用場景**:
  - 電商訂單
  - 微服務事務
  - 跨數據庫一致性
- **DDIA**: Chapter 7, 9 - Transactions
- **論文**: Saga（1987）

### 38. Consensus Algorithm (Raft/Paxos)
- **難度**: 5 星
- **時間**: 8-10 週
- **核心概念**: 分布式共識、Leader Election、日誌複製
- **學習重點**:
  - Raft 算法詳解
  - Paxos vs Raft
  - Leader Election
  - 日誌複製和一致性
- **應用場景**:
  - etcd（Raft）
  - ZooKeeper（Zab）
  - Consul
- **DDIA**: Chapter 9 - Consistency and Consensus
- **論文**: Raft（2014）、Paxos（1998）

### 39. Time-Series Database
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: 時序數據壓縮、聚合、查詢優化
- **學習重點**:
  - 時序數據特點
  - 壓縮算法（Gorilla、Delta-of-Delta）
  - LSM Tree 優化
  - 聚合查詢加速
- **應用場景**:
  - InfluxDB
  - TimescaleDB
  - Prometheus
- **論文**: Gorilla（Facebook, 2015）

### 40. Graph Database
- **難度**: 4 星
- **時間**: 4-5 週
- **核心概念**: 圖遍歷、社交網絡分析、最短路徑
- **學習重點**:
  - 圖存儲（鄰接表 vs 鄰接矩陣）
  - 圖遍歷算法（BFS、DFS）
  - PageRank 算法
  - Cypher 查詢語言
- **應用場景**:
  - Neo4j
  - 社交網絡分析
  - 知識圖譜
- **DDIA**: Chapter 2 - Data Models

---

## 學習策略建議

### 初學者（0-6 個月）

1. **順序學習 Phase 1**
   - 從 Counter Service 開始
   - 理解 Redis + DB 的經典組合
   - 學會畫架構圖

2. **動手實踐**
   - 每個案例都要運行代碼
   - 嘗試修改和擴展
   - 寫測試驗證正確性

3. **閱讀經典**
   - 同步閱讀 DDIA 對應章節
   - 觀看 ByteByteGo 視頻

### 中級（6-12 個月）

1. **深入 Phase 3-4**
   - 社交媒體和媒體平台
   - 理解高並發設計模式
   - 學習 Kafka、Elasticsearch

2. **系統思維**
   - 思考為什麼這樣設計
   - 分析 Trade-offs
   - 練習容量估算

3. **面試準備**
   - 練習白板設計
   - 45 分鐘內完成設計
   - 與他人模擬面試

### 高級（12+ 個月）

1. **挑戰 Phase 5-8**
   - 電商、AI、分布式理論
   - 閱讀論文（Raft、Paxos）
   - 實現複雜算法

2. **貢獻開源**
   - 參與相關開源專案
   - 分享學習心得
   - 幫助他人學習

---

## 配合閱讀材料

| 階段 | DDIA 章節 | ByteByteGo 案例 | Grokking 章節 |
|------|-----------|----------------|--------------|
| Phase 1 | Ch 5, 6 | URL Shortener, Rate Limiter | Ch 2, 13 |
| Phase 2 | Ch 3, 10 | Web Crawler, KV Store | - |
| Phase 3 | Ch 11 | News Feed, Chat, Notification | - |
| Phase 4 | Ch 10 | YouTube, Netflix | - |
| Phase 5 | - | Uber, Google Maps, Yelp | - |
| Phase 6 | Ch 7, 9 | Flash Sale, Payment | - |
| Phase 7 | - | ChatGPT, RAG, Agent | - |
| Phase 8 | Ch 7, 9 | Consensus, Transactions | - |

---

## 面試準備檢查清單

準備 Google 等大廠面試時，確保你能：

- [ ] **在 5 分鐘內**畫出系統高層架構
- [ ] **在 10 分鐘內**進行容量估算（QPS、存儲、帶寬）
- [ ] **解釋 Trade-offs**：為什麼選 Redis 而不是 Memcached？
- [ ] **討論瓶頸**：如何從 10K QPS 擴展到 1M QPS？
- [ ] **處理故障**：Redis 掛了怎麼辦？
- [ ] **估算成本**：AWS 費用大概多少？
- [ ] **安全性**：如何防止 DDoS、SQL 注入？
- [ ] **監控告警**：如何發現系統問題？

---

**祝你學習順利！每完成一個案例都是一次巨大的進步！**
