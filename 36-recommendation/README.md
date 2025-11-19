# 推薦引擎 (Recommendation Engine)

## 系統概述

推薦引擎是一個結合協同過濾、內容推薦、深度學習的個性化推薦平台，透過分析用戶行為和商品特徵，為每位用戶提供精準的個性化推薦，提升轉換率和用戶滿意度。

### 核心能力

1. **多策略召回** - 協同過濾、內容推薦、熱門榜單、實時興趣
2. **深度學習排序** - Two-Tower、Wide & Deep、DCN 等先進模型
3. **實時特徵工程** - 毫秒級特徵計算，捕捉用戶即時興趣
4. **智能重排序** - 多樣性、新鮮度、業務規則優化
5. **線上學習** - Bandit 算法、A/B Testing、持續優化
6. **可擴展架構** - 支援億級用戶、千萬級商品

### 業務價值

| 指標 | 優化前 | 優化後 | 提升 |
|------|--------|--------|------|
| **點擊率 (CTR)** | 2% | 8% | **4×** |
| **轉換率 (CVR)** | 1.5% | 6% | **4×** |
| **用戶停留時間** | 3 分鐘 | 12 分鐘 | **4×** |
| **GMV** | $100M/月 | $250M/月 | **2.5×** |
| **用戶留存率** | 25% | 45% | **+20%** |

### 應用場景

- **電商平台**：商品推薦、交叉銷售、個性化首頁
- **影音串流**：影片/音樂推薦、播放清單生成
- **新聞資訊**：文章推薦、個性化資訊流
- **社交媒體**：內容推薦、好友推薦、廣告投放
- **線上教育**：課程推薦、學習路徑規劃

## 功能需求

### 1. 核心功能

#### 1.1 召回層
- 協同過濾召回（User-CF、Item-CF、Matrix Factorization）
- 內容召回（TF-IDF、BERT Embeddings）
- 熱門召回（全站熱門、分類熱門、實時熱門）
- 用戶歷史召回（recently viewed、購買相關）
- 實時興趣召回（session-based、序列模型）

#### 1.2 排序層
- 粗排模型（簡單模型快速打分）
- 精排模型（Wide & Deep、DCN、Two-Tower）
- 多目標優化（CTR、CVR、停留時間、利潤）
- 實時特徵融合

#### 1.3 重排序層
- 多樣性優化（MMR、DPP）
- 新鮮度提升（時間衰減、新品加權）
- 業務規則（庫存、利潤率、運營策略）
- 個性化調整（VIP 用戶、地域差異）

#### 1.4 線上學習
- Contextual Bandit（Thompson Sampling、UCB）
- A/B Testing 框架
- 實時模型更新
- 效果監控與反饋

### 2. 非功能需求

| 需求 | 指標 | 說明 |
|------|------|------|
| **響應延遲** | < 100ms | P99 推薦請求延遲 |
| **吞吐量** | 100K QPS | 高峰期請求支援 |
| **召回規模** | 1000 萬+ 商品 | 商品庫規模 |
| **用戶規模** | 1 億+ 用戶 | 日活用戶支援 |
| **模型更新** | 每小時 | 線上模型更新頻率 |
| **特徵延遲** | < 10ms | 實時特徵計算延遲 |
| **可用性** | 99.99% | 服務可用性 |

## 技術架構

### 系統架構圖

```
┌─────────────────────────────────────────────────────────────────┐
│                          Client Layer                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │   Web    │  │  Mobile  │  │   App    │  │  WeChat  │       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                         API Gateway                              │
│              (限流、認證、A/B 分流)                               │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Recommendation Service                        │
│  ┌──────────────────────────────────────────────────────┐       │
│  │              Request Handler                          │       │
│  │  1. 解析請求   2. 獲取用戶畫像   3. 協調召回排序     │       │
│  └──────────────────────────────────────────────────────┘       │
└─────────────────────────────────────────────────────────────────┘
        │                      │                      │
        ▼                      ▼                      ▼
┌──────────────┐   ┌──────────────┐   ┌──────────────┐
│ Recall Layer │   │ Ranking Layer│   │ Rerank Layer │
└──────────────┘   └──────────────┘   └──────────────┘
        │                      │                      │
        ▼                      ▼                      ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Recall Service                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │Collab.   │  │ Content  │  │ Popular  │  │ RealTime │       │
│  │Filtering │  │  Based   │  │  Items   │  │ Interest │       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
│                      ↓ 500 candidates                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Ranking Service                              │
│  ┌──────────────────────────────────────────────────────┐       │
│  │  Coarse Ranking (500 → 100)                          │       │
│  │  - Simple Model (LR/GBDT)                            │       │
│  └──────────────────────────────────────────────────────┘       │
│  ┌──────────────────────────────────────────────────────┐       │
│  │  Fine Ranking (100 → 20)                             │       │
│  │  - Deep Model (Wide & Deep / DCN / Two-Tower)       │       │
│  └──────────────────────────────────────────────────────┘       │
│                      ↓ Top 20                                    │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                   Re-ranking Service                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │Diversity │  │Freshness │  │ Business │  │Multi-Obj │       │
│  │   MMR    │  │  Boost   │  │  Rules   │  │Optimize  │       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
│                      ↓ Final Top 10                              │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌──────────────┐   ┌──────────────┐   ┌──────────────┐
│  Feature     │   │    Model     │   │   Online     │
│  Service     │   │   Service    │   │  Learning    │
└──────────────┘   └──────────────┘   └──────────────┘
        │                      │                      │
        ▼                      ▼                      ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Storage Layer                               │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │  Redis   │  │PostgreSQL│  │   HBase  │  │  HDFS    │       │
│  │(實時特徵)│  │(用戶畫像)│  │(行為日誌)│  │(離線訓練)│       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
└─────────────────────────────────────────────────────────────────┘
```

### 技術棧

| 層級 | 技術選型 | 原因 |
|------|----------|------|
| **API 服務** | Go + Gin | 高效能、低延遲 |
| **召回** | Faiss / Milvus | 向量檢索、ANN 搜尋 |
| **排序** | TensorFlow Serving | 深度學習模型部署 |
| **實時特徵** | Redis | 毫秒級讀寫 |
| **離線特徵** | Hive + Spark | 大規模資料處理 |
| **用戶畫像** | PostgreSQL | 結構化資料 |
| **行為日誌** | Kafka + HBase | 高吞吐、時序資料 |
| **模型訓練** | PyTorch + Ray | 分散式訓練 |
| **A/B Testing** | 自研框架 | 靈活配置 |
| **監控** | Prometheus + Grafana | 指標收集與視覺化 |

## 資料庫設計

### 1. 用戶畫像表 (user_profiles)

```sql
CREATE TABLE user_profiles (
    user_id BIGINT PRIMARY KEY,
    age INTEGER,
    gender VARCHAR(10),
    city VARCHAR(50),
    vip_level INTEGER,
    registration_date DATE,

    -- 統計特徵
    total_orders INTEGER DEFAULT 0,
    total_gmv DECIMAL(12, 2) DEFAULT 0,
    avg_order_value DECIMAL(10, 2),
    favorite_categories INTEGER[],  -- 最喜歡的類別 ID 陣列

    -- 向量特徵（JSON 存儲）
    user_embedding JSONB,           -- 用戶 embedding 向量

    -- 時間戳
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_profiles_city ON user_profiles(city);
CREATE INDEX idx_user_profiles_vip_level ON user_profiles(vip_level);
CREATE INDEX idx_user_profiles_favorite_categories ON user_profiles USING GIN(favorite_categories);
```

### 2. 商品表 (items)

```sql
CREATE TABLE items (
    item_id BIGINT PRIMARY KEY,
    title VARCHAR(500) NOT NULL,
    category_id INTEGER NOT NULL,
    brand_id INTEGER,
    price DECIMAL(10, 2) NOT NULL,
    stock INTEGER DEFAULT 0,
    sales_count INTEGER DEFAULT 0,
    rating DECIMAL(3, 2),

    -- 特徵
    tags VARCHAR(100)[],
    attributes JSONB,               -- 商品屬性（顏色、尺寸等）
    item_embedding JSONB,           -- 商品 embedding 向量

    -- 統計
    view_count INTEGER DEFAULT 0,
    click_count INTEGER DEFAULT 0,
    cart_count INTEGER DEFAULT 0,
    purchase_count INTEGER DEFAULT 0,

    -- 業務
    profit_margin DECIMAL(5, 4),    -- 利潤率
    is_new BOOLEAN DEFAULT true,
    is_active BOOLEAN DEFAULT true,

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_items_category ON items(category_id);
CREATE INDEX idx_items_brand ON items(brand_id);
CREATE INDEX idx_items_price ON items(price);
CREATE INDEX idx_items_is_active ON items(is_active) WHERE is_active = true;
CREATE INDEX idx_items_tags ON items USING GIN(tags);
```

### 3. 用戶行為表 (user_behaviors) - HBase

```
RowKey: user_id:timestamp
Column Family: action
Columns:
  - item_id
  - action_type (view/click/cart/purchase)
  - duration (停留時間)
  - device_type
  - context (上下文資訊)
```

### 4. 商品相似度表 (item_similarities)

```sql
CREATE TABLE item_similarities (
    item_id_1 BIGINT NOT NULL,
    item_id_2 BIGINT NOT NULL,
    similarity_score REAL NOT NULL,
    similarity_type VARCHAR(50),    -- 'collaborative', 'content', 'embedding'
    computed_at TIMESTAMP NOT NULL DEFAULT NOW(),

    PRIMARY KEY (item_id_1, item_id_2)
);

CREATE INDEX idx_item_sim_score ON item_similarities(item_id_1, similarity_score DESC);
```

### 5. 推薦日誌表 (recommendation_logs) - HBase

```
RowKey: user_id:request_id
Column Family: request
Columns:
  - timestamp
  - recommended_items (推薦的商品列表)
  - recall_sources (召回來源)
  - scores (排序分數)

Column Family: feedback
Columns:
  - clicked_items
  - purchased_items
  - dwell_times
```

### 6. A/B Testing 配置表 (ab_experiments)

```sql
CREATE TABLE ab_experiments (
    experiment_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(200) NOT NULL,
    description TEXT,
    status VARCHAR(20) NOT NULL,    -- 'draft', 'running', 'completed'

    -- 流量分配
    traffic_allocation JSONB,       -- {"control": 0.5, "treatment": 0.5}

    -- 配置
    variants JSONB,                 -- 各變體的配置

    -- 時間
    start_time TIMESTAMP,
    end_time TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE ab_assignments (
    user_id BIGINT NOT NULL,
    experiment_id UUID NOT NULL REFERENCES ab_experiments(id),
    variant VARCHAR(50) NOT NULL,   -- 'control', 'treatment'
    assigned_at TIMESTAMP NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, experiment_id)
);

CREATE INDEX idx_ab_assignments_experiment ON ab_assignments(experiment_id);
```

## 核心功能實作

### 1. 協同過濾召回

```python
# recall/collaborative_filtering.py
import numpy as np
from scipy.sparse import csr_matrix
from sklearn.metrics.pairwise import cosine_similarity

class ItemBasedCF:
    def __init__(self):
        self.item_similarity_matrix = None

    def train(self, user_item_matrix):
        """
        user_item_matrix: scipy sparse matrix
        rows = users, cols = items
        """
        # 計算 Item-Item 相似度矩陣
        self.item_similarity_matrix = cosine_similarity(
            user_item_matrix.T,  # 轉置：item × user
            dense_output=False
        )

    def recall(self, user_id, user_history, top_k=100):
        """
        為用戶召回商品
        user_history: 用戶歷史互動的商品 ID 列表
        """
        # 計算候選商品分數
        scores = {}

        for item_id in user_history:
            # 找到與該商品相似的商品
            similar_items = self.item_similarity_matrix[item_id].toarray()[0]

            for candidate_id, similarity in enumerate(similar_items):
                if candidate_id not in user_history and similarity > 0:
                    if candidate_id not in scores:
                        scores[candidate_id] = 0
                    scores[candidate_id] += similarity

        # 排序取 Top-K
        sorted_items = sorted(scores.items(), key=lambda x: x[1], reverse=True)
        return [item_id for item_id, _ in sorted_items[:top_k]]

# 使用 Spark ALS for large scale
from pyspark.ml.recommendation import ALS

class ALSRecall:
    def __init__(self, rank=100, max_iter=10):
        self.model = None
        self.rank = rank
        self.max_iter = max_iter

    def train(self, ratings_df):
        """
        ratings_df: Spark DataFrame with columns [user_id, item_id, rating]
        """
        als = ALS(
            rank=self.rank,
            maxIter=self.max_iter,
            regParam=0.1,
            userCol="user_id",
            itemCol="item_id",
            ratingCol="rating",
            coldStartStrategy="drop",
            implicitPrefs=True  # 隱式反饋（點擊、觀看）
        )

        self.model = als.fit(ratings_df)

    def recall(self, user_id, top_k=100):
        """為單一用戶召回"""
        user_df = spark.createDataFrame([(user_id,)], ["user_id"])
        recommendations = self.model.recommendForUserSubset(user_df, top_k)

        return recommendations.select("recommendations.item_id").collect()[0][0]
```

### 2. 深度學習排序模型

```python
# ranking/wide_and_deep.py
import torch
import torch.nn as nn

class WideAndDeepModel(nn.Module):
    def __init__(self, wide_dim, deep_dim, embedding_dims):
        super().__init__()

        # Wide component (線性模型)
        self.wide = nn.Linear(wide_dim, 1)

        # Embedding layers
        self.embeddings = nn.ModuleDict({
            name: nn.Embedding(vocab_size, emb_dim)
            for name, (vocab_size, emb_dim) in embedding_dims.items()
        })

        # Deep component (DNN)
        self.deep = nn.Sequential(
            nn.Linear(deep_dim, 512),
            nn.ReLU(),
            nn.BatchNorm1d(512),
            nn.Dropout(0.3),

            nn.Linear(512, 256),
            nn.ReLU(),
            nn.BatchNorm1d(256),
            nn.Dropout(0.2),

            nn.Linear(256, 128),
            nn.ReLU(),
            nn.BatchNorm1d(128),

            nn.Linear(128, 1)
        )

    def forward(self, wide_features, categorical_features, numeric_features):
        # Wide part
        wide_out = self.wide(wide_features)

        # Embeddings
        emb_outputs = []
        for name, ids in categorical_features.items():
            emb_outputs.append(self.embeddings[name](ids))

        # Concatenate embeddings + numeric features
        deep_input = torch.cat(emb_outputs + [numeric_features], dim=-1)

        # Deep part
        deep_out = self.deep(deep_input)

        # Combine
        output = wide_out + deep_out
        return torch.sigmoid(output)

# 訓練
model = WideAndDeepModel(
    wide_dim=100,
    deep_dim=300,
    embedding_dims={
        'user_id': (1000000, 64),
        'item_id': (10000000, 64),
        'category': (1000, 32),
        'brand': (5000, 32)
    }
)

optimizer = torch.optim.Adam(model.parameters(), lr=0.001)
criterion = nn.BCELoss()

for epoch in range(10):
    for batch in train_loader:
        wide_feat, cat_feat, num_feat, labels = batch

        predictions = model(wide_feat, cat_feat, num_feat)
        loss = criterion(predictions, labels)

        optimizer.zero_grad()
        loss.backward()
        optimizer.step()
```

### 3. 實時特徵服務

```python
# feature/realtime_feature_service.py
import redis
import json
from datetime import datetime, timedelta

class RealtimeFeatureService:
    def __init__(self, redis_client):
        self.redis = redis_client

    def update_user_action(self, user_id, item_id, action_type):
        """更新用戶實時行為"""
        # 1. 最近點擊序列
        key = f"user:{user_id}:recent_clicks"
        self.redis.lpush(key, json.dumps({
            'item_id': item_id,
            'action': action_type,
            'timestamp': datetime.now().isoformat()
        }))
        self.redis.ltrim(key, 0, 99)  # 只保留最近 100 個
        self.redis.expire(key, 86400)  # 24 小時過期

        # 2. Session 內行為
        session_key = f"user:{user_id}:session:{self._get_session_id()}"
        self.redis.sadd(session_key, item_id)
        self.redis.expire(session_key, 1800)  # 30 分鐘過期

        # 3. 類別偏好（實時更新）
        category_id = self._get_item_category(item_id)
        category_key = f"user:{user_id}:category_pref"
        self.redis.zincrby(category_key, 1, category_id)
        self.redis.expire(category_key, 604800)  # 7 天過期

    def get_user_realtime_features(self, user_id):
        """獲取用戶實時特徵"""
        features = {}

        # 1. 最近點擊的商品
        recent_key = f"user:{user_id}:recent_clicks"
        recent_clicks = self.redis.lrange(recent_key, 0, 9)
        features['recent_items'] = [
            json.loads(c)['item_id'] for c in recent_clicks
        ]

        # 2. Session 內瀏覽的商品數
        session_key = f"user:{user_id}:session:{self._get_session_id()}"
        features['session_item_count'] = self.redis.scard(session_key)

        # 3. 熱門類別偏好
        category_key = f"user:{user_id}:category_pref"
        top_categories = self.redis.zrevrange(category_key, 0, 4, withscores=True)
        features['top_categories'] = [
            {'category_id': int(c), 'score': s}
            for c, s in top_categories
        ]

        return features

    def update_item_popularity(self, item_id):
        """更新商品實時熱度"""
        # 使用 HyperLogLog 統計獨立訪客數
        hour_key = f"item:{item_id}:uv:{datetime.now().hour}"
        self.redis.pfadd(hour_key, user_id)
        self.redis.expire(hour_key, 7200)

        # 點擊計數（滑動窗口）
        click_key = f"item:{item_id}:clicks"
        self.redis.incr(click_key)
        self.redis.expire(click_key, 3600)  # 1 小時窗口

    def get_item_popularity_features(self, item_id):
        """獲取商品熱度特徵"""
        # 最近 1 小時的獨立訪客數
        current_hour = datetime.now().hour
        uv_key = f"item:{item_id}:uv:{current_hour}"
        uv_count = self.redis.pfcount(uv_key)

        # 最近 1 小時的點擊數
        click_key = f"item:{item_id}:clicks"
        click_count = self.redis.get(click_key) or 0

        return {
            'hourly_uv': uv_count,
            'hourly_clicks': int(click_count),
            'ctr': click_count / max(uv_count, 1)
        }
```

### 4. 重排序服務

```python
# rerank/reranker.py
import numpy as np
from sklearn.metrics.pairwise import cosine_similarity

class Reranker:
    def __init__(self, config):
        self.config = config

    def rerank(self, user, items, scores):
        """
        應用多種重排序策略
        """
        # 1. MMR 多樣性
        if self.config.get('diversity_enabled'):
            items, scores = self.apply_mmr(items, scores, lambda_param=0.7)

        # 2. 新鮮度提升
        if self.config.get('freshness_enabled'):
            scores = self.apply_freshness_boost(items, scores)

        # 3. 業務規則
        items, scores = self.apply_business_rules(user, items, scores)

        # 4. 多目標優化
        if self.config.get('multi_objective'):
            scores = self.apply_multi_objective(user, items, scores)

        # 最終排序
        sorted_indices = np.argsort(scores)[::-1]
        return [items[i] for i in sorted_indices], [scores[i] for i in sorted_indices]

    def apply_mmr(self, items, scores, lambda_param=0.7, top_k=10):
        """
        MMR (Maximal Marginal Relevance) 多樣性優化
        """
        selected_indices = []
        remaining_indices = list(range(len(items)))

        # 計算商品之間的相似度矩陣
        item_embeddings = np.array([item.embedding for item in items])
        similarity_matrix = cosine_similarity(item_embeddings)

        # 選擇第一個（分數最高）
        first_idx = np.argmax(scores)
        selected_indices.append(first_idx)
        remaining_indices.remove(first_idx)

        # 迭代選擇
        while len(selected_indices) < top_k and remaining_indices:
            mmr_scores = []

            for idx in remaining_indices:
                # 相關性分數
                relevance = scores[idx]

                # 與已選商品的最大相似度
                max_sim = max([
                    similarity_matrix[idx][s_idx]
                    for s_idx in selected_indices
                ])

                # MMR 分數
                mmr = lambda_param * relevance - (1 - lambda_param) * max_sim
                mmr_scores.append(mmr)

            # 選擇 MMR 分數最高的
            best_idx = remaining_indices[np.argmax(mmr_scores)]
            selected_indices.append(best_idx)
            remaining_indices.remove(best_idx)

        return [items[i] for i in selected_indices], [scores[i] for i in selected_indices]

    def apply_freshness_boost(self, items, scores):
        """新鮮度加權"""
        boosted_scores = []
        current_time = datetime.now()

        for item, score in zip(items, scores):
            age_days = (current_time - item.created_at).days

            if age_days <= 3:
                boost = 1.5
            elif age_days <= 7:
                boost = 1.3
            elif age_days <= 14:
                boost = 1.1
            else:
                boost = 1.0

            boosted_scores.append(score * boost)

        return np.array(boosted_scores)

    def apply_business_rules(self, user, items, scores):
        """業務規則過濾與調整"""
        filtered_items = []
        filtered_scores = []

        for item, score in zip(items, scores):
            # 硬約束：必須滿足
            if item.stock <= 0:
                continue  # 無庫存，跳過

            if item.id in user.purchased_items:
                continue  # 已購買，跳過

            # 軟約束：調整分數
            if item.profit_margin > 0.5:
                score *= 1.2  # 高利潤商品加權

            if item.sales_count > 10000:
                score *= 1.1  # 熱銷商品加權

            if user.is_vip:
                score *= 1.15  # VIP 用戶看到更好的商品

            filtered_items.append(item)
            filtered_scores.append(score)

        return filtered_items, np.array(filtered_scores)

    def apply_multi_objective(self, user, items, scores):
        """多目標優化"""
        # 預測多個目標
        ctr_scores = self.ctr_model.predict(user, items)
        cvr_scores = self.cvr_model.predict(user, items)

        # 計算預期利潤
        profits = np.array([item.price * item.profit_margin for item in items])

        # 正規化
        ctr_norm = (ctr_scores - ctr_scores.min()) / (ctr_scores.max() - ctr_scores.min())
        cvr_norm = (cvr_scores - cvr_scores.min()) / (cvr_scores.max() - cvr_scores.min())
        profit_norm = (profits - profits.min()) / (profits.max() - profits.min())

        # 加權組合
        weights = self.config['multi_objective_weights']
        final_scores = (
            weights['ctr'] * ctr_norm +
            weights['cvr'] * cvr_norm +
            weights['profit'] * profit_norm
        )

        return final_scores
```

## API 文件

### 1. 獲取個性化推薦

```http
POST /api/v1/recommend
Content-Type: application/json
Authorization: Bearer <token>

{
    "user_id": 123456,
    "scene": "homepage",      // 場景：homepage, detail_page, cart
    "num_items": 10,
    "context": {
        "device": "mobile",
        "location": "taipei",
        "time": "2025-01-15T10:00:00Z"
    }
}

Response 200 OK:
{
    "request_id": "req_550e8400",
    "items": [
        {
            "item_id": 789012,
            "title": "iPhone 15 Pro Max",
            "price": 39900,
            "image_url": "https://cdn.example.com/iphone15.jpg",
            "score": 0.92,
            "reason": "基於你的瀏覽歷史",
            "source": "collaborative_filtering"
        },
        {
            "item_id": 345678,
            "title": "AirPods Pro",
            "price": 7490,
            "image_url": "https://cdn.example.com/airpods.jpg",
            "score": 0.88,
            "reason": "經常一起購買",
            "source": "item_similarity"
        }
    ],
    "latency_ms": 85
}
```

### 2. 相似商品推薦

```http
GET /api/v1/items/{item_id}/similar?limit=20
Authorization: Bearer <token>

Response 200 OK:
{
    "item_id": 789012,
    "similar_items": [
        {
            "item_id": 789013,
            "title": "iPhone 15 Pro",
            "similarity_score": 0.95,
            "similarity_type": "collaborative"
        },
        {
            "item_id": 789011,
            "title": "iPhone 14 Pro Max",
            "similarity_score": 0.87,
            "similarity_type": "content"
        }
    ]
}
```

### 3. 記錄用戶行為

```http
POST /api/v1/events
Content-Type: application/json
Authorization: Bearer <token>

{
    "user_id": 123456,
    "events": [
        {
            "event_type": "view",
            "item_id": 789012,
            "timestamp": "2025-01-15T10:00:00Z",
            "duration_seconds": 30,
            "context": {
                "page": "detail",
                "source": "recommendation"
            }
        },
        {
            "event_type": "click",
            "item_id": 789012,
            "timestamp": "2025-01-15T10:00:30Z"
        }
    ]
}

Response 200 OK:
{
    "message": "Events recorded successfully",
    "event_count": 2
}
```

### 4. A/B Testing 分配

```http
GET /api/v1/ab/assign?user_id=123456&experiment=rec_algo_v2
Authorization: Bearer <token>

Response 200 OK:
{
    "experiment_id": "exp_123",
    "experiment_name": "rec_algo_v2",
    "variant": "treatment",
    "config": {
        "recall_strategies": ["cf", "content", "deep_learning"],
        "ranking_model": "wide_and_deep_v2"
    }
}
```

## 效能優化

### 1. 向量檢索優化（Faiss）

```python
import faiss
import numpy as np

class FaissIndex:
    def __init__(self, dimension=128):
        self.dimension = dimension
        # 使用 IVF (Inverted File) + PQ (Product Quantization)
        self.index = faiss.IndexIVFPQ(
            faiss.IndexFlatL2(dimension),
            dimension,
            nlist=1000,        # 聚類中心數量
            m=64,              # PQ 子向量數量
            nbits=8            # 每個子向量的位元數
        )

    def train(self, vectors):
        """訓練索引"""
        self.index.train(vectors)
        self.index.add(vectors)

    def search(self, query_vector, top_k=100):
        """檢索最相似的向量"""
        self.index.nprobe = 10  # 搜尋的聚類數
        distances, indices = self.index.search(query_vector, top_k)
        return indices[0], distances[0]

# 效能比較
# Flat Index: 100 萬向量，搜尋時間 ~500ms
# IVF+PQ: 100 萬向量，搜尋時間 ~20ms（25× 加速）
# 準確度影響：< 2%
```

### 2. 模型服務優化

```python
# 使用 TensorFlow Serving 批次推理
import tensorflow as tf

# 模型批次配置
batching_parameters = """
max_batch_size { value: 128 }
batch_timeout_micros { value: 5000 }
max_enqueued_batches { value: 100 }
num_batch_threads { value: 8 }
"""

# 部署時啟用批次
tensorflow_model_server \
    --rest_api_port=8501 \
    --model_name=ranking_model \
    --model_base_path=/models/ranking \
    --enable_batching=true \
    --batching_parameters_file=batching_config.txt

# 效能提升：
# 單次推理：10ms × 100 請求 = 1000ms
# 批次推理（batch=100）：50ms（20× 加速）
```

### 3. 特徵快取

```python
class FeatureCache:
    def __init__(self, redis_client, ttl=3600):
        self.redis = redis_client
        self.ttl = ttl

    def get_user_features(self, user_id):
        """獲取用戶特徵（帶快取）"""
        cache_key = f"user_features:{user_id}"

        # 嘗試從快取獲取
        cached = self.redis.get(cache_key)
        if cached:
            return json.loads(cached)

        # 從資料庫載入
        features = self.load_from_db(user_id)

        # 寫入快取
        self.redis.setex(cache_key, self.ttl, json.dumps(features))

        return features

# 效能提升：
# 資料庫查詢：20ms
# Redis 快取：< 1ms（20× 加速）
# 快取命中率：85%
```

## 部署架構

```yaml
# kubernetes/recommendation-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: recommendation-service
spec:
  replicas: 20
  selector:
    matchLabels:
      app: recommendation
  template:
    spec:
      containers:
      - name: recommendation
        image: recommendation:v1.0.0
        resources:
          requests:
            memory: "4Gi"
            cpu: "2000m"
          limits:
            memory: "8Gi"
            cpu: "4000m"
        env:
        - name: REDIS_URL
          value: "redis://redis-cluster:6379"
        - name: POSTGRES_URL
          valueFrom:
            secretKeyRef:
              name: db-secret
              key: url
        - name: MODEL_SERVING_URL
          value: "http://tf-serving:8501"

---
# TensorFlow Serving for ranking model
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tf-serving
spec:
  replicas: 10
  template:
    spec:
      containers:
      - name: tensorflow-serving
        image: tensorflow/serving:latest
        args:
          - --model_name=ranking_model
          - --model_base_path=/models/ranking
          - --rest_api_port=8501
          - --enable_batching=true
        resources:
          limits:
            nvidia.com/gpu: 1
        volumeMounts:
        - name: model-storage
          mountPath: /models

---
# Faiss vector search service
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vector-search
spec:
  replicas: 5
  template:
    spec:
      containers:
      - name: faiss-server
        image: faiss-server:v1.0.0
        resources:
          requests:
            memory: "16Gi"
            cpu: "4000m"
```

## 成本估算

### 每月運營成本（1000 萬 DAU，每人每天 20 次推薦）

| 項目 | 用量 | 單價 | 月成本 |
|------|------|------|--------|
| **運算資源** | | | |
| API 服務 | 20 × c5.2xlarge | $0.34/hr | $4,896 |
| TF Serving (GPU) | 10 × p3.2xlarge | $3.06/hr | $22,032 |
| Vector Search | 5 × r5.4xlarge | $1.008/hr | $3,629 |
| **儲存** | | | |
| Redis Cluster | 3 × r5.4xlarge | $1.008/hr | $2,177 |
| PostgreSQL | db.r5.4xlarge | $1.008/hr | $726 |
| HBase (EC2) | 10 × i3.2xlarge | $0.624/hr | $4,493 |
| S3 (模型/日誌) | 50TB | $0.023/GB | $1,150 |
| **資料處理** | | | |
| Kafka | 5 × r5.xlarge | $0.252/hr | $907 |
| Spark (離線訓練) | 20 × r5.2xlarge | $0.504/hr | $7,258 |
| **網路** | | | |
| Data Transfer | 100TB | $0.09/GB | $9,000 |
| **監控** | | | |
| Prometheus + Grafana | - | - | $500 |
| **總計** | | | **$56,768** |

### 成本優化策略

**優化後成本：$34,061（降低 40%）**

1. **Spot Instances for 訓練**：Spark 成本降低 70% = 節省 $5,081
2. **模型量化 (INT8)**：GPU 需求減半 = 節省 $11,016
3. **Faiss 量化**：記憶體需求降低 50% = 節省 $1,815
4. **Redis 智慧過期**：記憶體節省 30% = 節省 $653
5. **Reserved Instances（1 年）**：基礎設施降低 30% = 節省 $3,142

### ROI 分析

**業務價值：**
- GMV 提升：$100M → $250M（+$150M/月）
- 推薦貢獻比例：35%
- 歸因於推薦系統的增量 GMV：$52.5M/月
- 利潤率：10%
- **增量利潤：$5.25M/月**

**ROI = (增量利潤 - 系統成本) / 系統成本**
**ROI = ($5,250,000 - $34,061) / $34,061 = 15,303%**

## 監控與告警

```yaml
# Prometheus 告警規則
groups:
  - name: recommendation_system
    rules:
      # 推薦延遲過高
      - alert: HighRecommendationLatency
        expr: histogram_quantile(0.99, rate(recommendation_latency_seconds_bucket[5m])) > 0.2
        for: 5m
        annotations:
          summary: "P99 推薦延遲 > 200ms"

      # 召回數量不足
      - alert: LowRecallCount
        expr: avg(recall_candidate_count) < 100
        for: 10m
        annotations:
          summary: "召回候選數量 < 100"

      # CTR 下降
      - alert: CTRDrop
        expr: rate(recommendation_clicks_total[1h]) / rate(recommendation_impressions_total[1h]) < 0.05
        for: 30m
        annotations:
          summary: "CTR < 5%，可能模型降級"

      # 模型服務不可用
      - alert: ModelServingDown
        expr: up{job="tf-serving"} == 0
        for: 2m
        annotations:
          summary: "TensorFlow Serving 無法連線"
```

## 總結

推薦引擎透過多策略召回、深度學習排序、智能重排序，打造個性化體驗：

| 模組 | 技術 | 價值 |
|------|------|------|
| **召回** | 協同過濾 + 內容 + 深度學習 | 覆蓋率 > 95% |
| **排序** | Wide & Deep / DCN | CTR 提升 4× |
| **重排序** | MMR + 業務規則 | 多樣性 +30% |
| **實時** | Redis + Kafka | 延遲 < 100ms |
| **A/B Testing** | 實驗框架 | 持續優化 |

透過本章學習，你掌握了：

1. ✅ **協同過濾**：Item-CF、ALS 大規模訓練
2. ✅ **深度學習**：Two-Tower、Wide & Deep、DCN
3. ✅ **實時特徵**：Redis Feature Store、序列建模
4. ✅ **重排序**：MMR 多樣性、新鮮度、業務規則
5. ✅ **向量檢索**：Faiss ANN、IVF+PQ 優化
6. ✅ **A/B Testing**：實驗設計、效果評估
7. ✅ **完整架構**：從召回到部署的生產級系統

**Phase 7: AI Platforms 完成！** 🎉
