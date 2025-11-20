# 推薦引擎設計：從協同過濾到深度學習

> 本文檔採用蘇格拉底式對話法（Socratic Method）呈現系統設計的思考過程

## Act 1: 推薦系統的價值與挑戰

**場景**：Emma 的電商平台有 100 萬件商品，用戶不知道該買什麼

**Emma**：「我們的商品太多了！用戶進來後不知道看什麼，轉換率只有 2%。」

**David**：「這就是推薦系統要解決的核心問題：**資訊過載**。」

### 推薦系統的價值

**沒有推薦系統：**
```
用戶進入網站
    ↓
看到 100 萬件商品
    ↓
不知道從何開始
    ↓
隨便逛逛，沒找到想要的
    ↓
離開（轉換率 2%）
```

**有推薦系統：**
```
用戶進入網站
    ↓
看到「為你推薦」的 10 件商品
    ↓
都是符合興趣的商品
    ↓
點擊、購買（轉換率 15%）
```

**Sarah**：「業界數據顯示：」
- **Netflix**：80% 的觀看來自推薦
- **YouTube**：70% 的觀看時間來自推薦
- **Amazon**：35% 的營收來自推薦

**Michael**：「推薦系統的三大挑戰：」

### 挑戰 1：冷啟動問題

```
新用戶：
- 沒有歷史行為
- 不知道他喜歡什麼
- 該推薦什麼？

新商品：
- 沒有人買過
- 沒有評分資料
- 如何推薦出去？
```

### 挑戰 2：稀疏性問題

```
100 萬商品 × 10 萬用戶 = 1000 億個可能的互動
但實際互動：< 0.01%（只有 1000 萬筆）

用戶-商品矩陣：
        商品1  商品2  商品3  ...  商品100萬
用戶1    5     ?      ?     ...    ?
用戶2    ?     4      ?     ...    ?
用戶3    ?     ?      3     ...    ?
...
用戶10萬 ?     ?      ?     ...    5

99.99% 都是空白（用戶沒有評分過）
```

### 挑戰 3：實時性與可擴展性

```
需求：
- 用戶點擊商品 → 立即更新推薦（< 100ms）
- 100 萬用戶同時線上 → 每秒 10 萬次推薦請求
- 商品庫每天更新 → 推薦列表即時反映
```

**Emma**：「這些挑戰怎麼解決？」

**David**：「需要結合多種推薦算法。讓我們從最基礎的開始。」

## Act 2: 協同過濾 - 從用戶行為學習

**Sarah**：「協同過濾（Collaborative Filtering）的核心思想：**物以類聚、人以群分**。」

### 基於用戶的協同過濾 (User-based CF)

**邏輯：**
```
1. 找到跟你相似的用戶
2. 看他們喜歡什麼
3. 推薦給你
```

**範例：**
```
你（Emma）的評分：
- 《星際效應》: 5 星
- 《全面啟動》: 5 星
- 《黑暗騎士》: 4 星

找到相似用戶（David）：
- 《星際效應》: 5 星  ← 相同！
- 《全面啟動》: 4 星  ← 相同！
- 《黑暗騎士》: 5 星  ← 相同！
- 《敦克爾克大行動》: 5 星  ← 你沒看過

推薦：《敦克爾克大行動》給你
（因為 David 跟你品味相似，他喜歡的你可能也喜歡）
```

**計算相似度：餘弦相似度**

```python
import numpy as np

# 用戶評分向量
emma = np.array([5, 5, 4, 0])  # 0 表示未評分
david = np.array([5, 4, 5, 5])

# 只計算兩人都評分的項目
common_items = (emma > 0) & (david > 0)
emma_common = emma[common_items]
david_common = david[common_items]

# 餘弦相似度
similarity = np.dot(emma_common, david_common) / (
    np.linalg.norm(emma_common) * np.linalg.norm(david_common)
)
# similarity = 0.98（非常相似！）
```

**Michael**：「找到前 K 個最相似的用戶，預測評分：」

```python
def predict_rating_user_based(user_id, item_id, k=10):
    # 1. 找到 k 個最相似的用戶（已評分該商品）
    similar_users = find_similar_users(user_id, k)

    # 2. 計算加權平均評分
    weighted_sum = 0
    similarity_sum = 0

    for similar_user, similarity in similar_users:
        rating = get_rating(similar_user, item_id)
        if rating > 0:
            weighted_sum += similarity * rating
            similarity_sum += similarity

    if similarity_sum == 0:
        return None

    predicted_rating = weighted_sum / similarity_sum
    return predicted_rating

# 範例
predicted = predict_rating_user_based(user_id="emma", item_id="dunkirk")
# predicted = 4.8 星（推薦！）
```

### 基於物品的協同過濾 (Item-based CF)

**David**：「換個角度：不找相似用戶，找相似商品。」

**邏輯：**
```
1. 你喜歡商品 A
2. 找到跟 A 相似的商品 B
3. 推薦 B 給你
```

**範例：**
```
《星際效應》的評分分佈：
用戶1: 5 星, 用戶2: 4 星, 用戶3: 5 星, ...

《全面啟動》的評分分佈：
用戶1: 5 星, 用戶2: 4 星, 用戶3: 5 星, ...

→ 兩部電影的評分模式很像！
→ 喜歡《星際效應》的用戶也喜歡《全面啟動》
```

**優勢：**
- **更穩定**：商品數量 < 用戶數量，相似度矩陣較小
- **可解釋性**：「因為你喜歡 A，所以推薦 B」
- **預計算**：商品相似度可以離線計算，線上直接查表

**Emma**：「User-based 和 Item-based 哪個好？」

**Michael**：「取決於場景：」

| 場景 | 選擇 | 原因 |
|------|------|------|
| 用戶 << 商品（電商） | Item-based | 商品相似度穩定，可預計算 |
| 用戶 >> 商品（新聞） | User-based | 新聞量少，用戶興趣多元 |
| 商品更新快（影片） | User-based | 新影片沒有足夠評分計算相似度 |

### 矩陣分解 (Matrix Factorization)

**Sarah**：「協同過濾的進階版本：矩陣分解。」

**核心思想：**
```
用戶-商品評分矩陣（稀疏）
        商品1  商品2  商品3  商品4
用戶1    5     ?      4     ?
用戶2    ?     3      ?     5
用戶3    4     ?      ?     4

分解成兩個小矩陣：

用戶特徵矩陣（User Embedding）
        因子1  因子2
用戶1    0.9   0.2
用戶2    0.1   0.8
用戶3    0.7   0.3

商品特徵矩陣（Item Embedding）
        因子1  因子2
商品1    0.8   0.1
商品2    0.2   0.9
商品3    0.9   0.2
商品4    0.3   0.8

預測評分 = 用戶向量 · 商品向量
```

**實作：使用 SVD（奇異值分解）**

```python
from scipy.sparse.linalg import svds
import numpy as np

# 用戶-商品評分矩陣（稀疏）
R = np.array([
    [5, 0, 4, 0],
    [0, 3, 0, 5],
    [4, 0, 0, 4]
])

# 矩陣分解（k=2 個潛在因子）
U, sigma, Vt = svds(R, k=2)

# 重建完整矩陣（填補空白）
R_pred = np.dot(np.dot(U, np.diag(sigma)), Vt)

# R_pred:
# [[5.0  2.1  4.0  3.8]
#  [2.3  3.0  2.5  5.0]
#  [4.0  2.2  3.9  4.0]]

# 原本空白的位置現在有預測評分了！
```

**ALS (Alternating Least Squares) - 大規模矩陣分解**

```python
from pyspark.ml.recommendation import ALS

# Spark 分散式訓練（處理億級資料）
als = ALS(
    maxIter=10,
    regParam=0.1,
    userCol="user_id",
    itemCol="item_id",
    ratingCol="rating",
    coldStartStrategy="drop"
)

model = als.fit(ratings_df)

# 為所有用戶生成推薦
recommendations = model.recommendForAllUsers(10)
```

**Emma**：「矩陣分解的優勢是什麼？」

**David**：「三大優勢：」

1. **稀疏性問題解決**：可以預測空白位置的評分
2. **可擴展性**：Spark 可處理億級用戶和商品
3. **可解釋性**：潛在因子可能對應「類型偏好」（動作片 vs 文藝片）

## Act 3: 內容推薦 - 基於特徵匹配

**Michael**：「協同過濾有個致命缺陷：冷啟動。新商品沒人買過，就推薦不出去。」

**Sarah**：「內容推薦（Content-Based）解決這個問題：基於商品特徵推薦。」

### 運作原理

```
步驟 1：提取商品特徵
電影《星際效應》：
- 類型：科幻、劇情
- 導演：克里斯多福·諾蘭
- 演員：馬修·麥康納、安海瑟薇
- 標籤：太空、時間旅行、父女情

步驟 2：建立商品向量
《星際效應》= [科幻=1, 劇情=1, 動作=0, ..., 諾蘭=1, ...]

步驟 3：建立用戶興趣向量（從歷史觀看學習）
Emma 看過：
- 《星際效應》✓
- 《全面啟動》✓
- 《黑暗騎士》✓

Emma 的興趣向量 = 加權平均
= [科幻=0.8, 劇情=0.6, 動作=0.4, ..., 諾蘭=1.0, ...]

步驟 4：計算相似度，推薦最相似的商品
新電影《敦克爾克大行動》= [戰爭=1, 劇情=1, ..., 諾蘭=1, ...]
相似度 = 0.75（高！推薦）
```

### TF-IDF 特徵提取

```python
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.metrics.pairwise import cosine_similarity

# 商品描述
movies = {
    "interstellar": "科幻 太空 時間旅行 諾蘭 父女情",
    "inception": "科幻 夢境 諾蘭 動作",
    "dark_knight": "超級英雄 動作 諾蘭 黑暗",
    "dunkirk": "戰爭 歷史 諾蘭 二戰"
}

# TF-IDF 向量化
tfidf = TfidfVectorizer()
movie_vectors = tfidf.fit_transform(movies.values())

# 計算電影之間的相似度
similarity_matrix = cosine_similarity(movie_vectors)

# 相似度矩陣：
#               interstellar  inception  dark_knight  dunkirk
# interstellar      1.00        0.63        0.45       0.32
# inception         0.63        1.00        0.58       0.38
# dark_knight       0.45        0.58        1.00       0.42
# dunkirk           0.32        0.38        0.42       1.00

# 如果用戶喜歡 interstellar，推薦 inception（相似度 0.63）
```

### 深度特徵提取 - BERT Embeddings

```python
from sentence_transformers import SentenceTransformer

# 使用預訓練的 BERT 模型
model = SentenceTransformer('paraphrase-multilingual-mpnet-base-v2')

# 商品描述（更豐富）
descriptions = [
    "星際效應是一部科幻電影，講述太空探險與時間旅行的故事",
    "全面啟動探討夢境與現實的界線，充滿懸疑與動作",
    "黑暗騎士是蝙蝠俠系列的巔峰之作，黑暗寫實風格",
    "敦克爾克大行動重現二戰敦克爾克大撤退的歷史事件"
]

# 生成語意向量（768 維）
embeddings = model.encode(descriptions)

# 計算相似度
from sklearn.metrics.pairwise import cosine_similarity
similarities = cosine_similarity(embeddings)

# BERT 能捕捉更深層的語意關係
```

**Emma**：「內容推薦 vs 協同過濾？」

**David**：「各有優缺點：」

| 特性 | 協同過濾 | 內容推薦 |
|------|----------|----------|
| **冷啟動** | ❌ 無法處理新商品 | ✅ 可以處理 |
| **驚喜度** | ✅ 可能發現意外喜好 | ❌ 只推薦相似的 |
| **多樣性** | ✅ 基於群體智慧 | ❌ 容易陷入同質化 |
| **可解釋性** | ⚠️ 需要額外處理 | ✅ 「因為你喜歡 X 特徵」 |
| **需要資料** | 用戶行為 | 商品特徵 |

**最佳實踐：混合使用！**

## Act 4: 深度學習推薦模型

**Sarah**：「近年來，深度學習徹底改變了推薦系統。」

### Embedding - 一切的基礎

**Michael**：「Embedding 就是把高維稀疏特徵轉成低維稠密向量。」

```python
import torch
import torch.nn as nn

# 假設有 10 萬個商品
num_items = 100000
embedding_dim = 128

# Embedding 層
item_embedding = nn.Embedding(num_items, embedding_dim)

# 商品 ID → 向量
item_id = torch.tensor([42])  # 商品 42
item_vector = item_embedding(item_id)
# item_vector: [0.23, -0.45, 0.67, ..., 0.12]  (128 維)

# 相似商品：找向量相近的商品
similarities = torch.matmul(item_vector, item_embedding.weight.T)
top_similar = torch.topk(similarities, k=10)
```

### Two-Tower 模型 - YouTube 推薦

**David**：「YouTube 的推薦架構：用戶塔 + 商品塔。」

```
用戶塔：                        商品塔：
用戶 ID ──┐                     商品 ID ──┐
觀看歷史 ─┤                     標題 ─────┤
搜尋歷史 ─┤→ Dense → User       描述 ─────┤→ Dense → Item
地理位置 ─┤   Layers   Vector   類別 ─────┤   Layers   Vector
年齡性別 ─┘                     標籤 ─────┘

           User Vector · Item Vector = 相似度分數
                    ↓
              Top-K 推薦
```

**實作：**

```python
import torch.nn as nn

class TwoTowerModel(nn.Module):
    def __init__(self, num_users, num_items, embedding_dim=128):
        super().__init__()

        # 用戶塔
        self.user_embedding = nn.Embedding(num_users, embedding_dim)
        self.user_tower = nn.Sequential(
            nn.Linear(embedding_dim, 256),
            nn.ReLU(),
            nn.Dropout(0.2),
            nn.Linear(256, 128),
            nn.ReLU(),
            nn.Linear(128, 64)  # 最終用戶向量
        )

        # 商品塔
        self.item_embedding = nn.Embedding(num_items, embedding_dim)
        self.item_tower = nn.Sequential(
            nn.Linear(embedding_dim, 256),
            nn.ReLU(),
            nn.Dropout(0.2),
            nn.Linear(256, 128),
            nn.ReLU(),
            nn.Linear(128, 64)  # 最終商品向量
        )

    def forward(self, user_ids, item_ids):
        # 用戶向量
        user_emb = self.user_embedding(user_ids)
        user_vec = self.user_tower(user_emb)

        # 商品向量
        item_emb = self.item_embedding(item_ids)
        item_vec = self.item_tower(item_emb)

        # 點積計算相似度
        scores = (user_vec * item_vec).sum(dim=-1)
        return scores

    def recommend(self, user_id, top_k=10):
        """為用戶生成推薦"""
        # 計算用戶向量
        user_vec = self.get_user_vector(user_id)

        # 與所有商品向量計算相似度
        all_item_vectors = self.get_all_item_vectors()
        scores = torch.matmul(user_vec, all_item_vectors.T)

        # Top-K
        top_items = torch.topk(scores, k=top_k)
        return top_items.indices
```

### Deep & Cross Network (DCN)

**Sarah**：「顯式建模特徵交叉。」

```
輸入特徵：
- 用戶年齡：25
- 用戶性別：男
- 商品類別：3C
- 商品價格：$500

傳統 DNN：只能學到簡單組合
DCN：可以學到複雜交叉
- 年齡 × 性別
- 年齡 × 商品類別
- 年齡 × 性別 × 商品類別（三階交叉）
```

**架構：**

```python
class DeepCrossNetwork(nn.Module):
    def __init__(self, input_dim, num_layers=3):
        super().__init__()

        # Cross Network（顯式交叉）
        self.cross_layers = nn.ModuleList([
            nn.Linear(input_dim, input_dim, bias=False)
            for _ in range(num_layers)
        ])

        # Deep Network（隱式學習）
        self.deep_layers = nn.Sequential(
            nn.Linear(input_dim, 256),
            nn.ReLU(),
            nn.Dropout(0.2),
            nn.Linear(256, 128),
            nn.ReLU(),
            nn.Linear(128, 64)
        )

        # 最終輸出
        self.output_layer = nn.Linear(input_dim + 64, 1)

    def forward(self, x):
        # Cross Network
        x_cross = x
        for cross_layer in self.cross_layers:
            x_cross = x_cross + x * cross_layer(x_cross)

        # Deep Network
        x_deep = self.deep_layers(x)

        # 串接
        x_combined = torch.cat([x_cross, x_deep], dim=-1)

        # 輸出
        output = self.output_layer(x_combined)
        return output.squeeze()
```

### Neural Collaborative Filtering (NCF)

**Michael**：「用神經網路取代矩陣分解的內積。」

```python
class NCF(nn.Module):
    def __init__(self, num_users, num_items, embedding_dim=64):
        super().__init__()

        # Embedding 層
        self.user_embedding = nn.Embedding(num_users, embedding_dim)
        self.item_embedding = nn.Embedding(num_items, embedding_dim)

        # MLP 層（取代簡單內積）
        self.mlp = nn.Sequential(
            nn.Linear(embedding_dim * 2, 128),
            nn.ReLU(),
            nn.Dropout(0.2),
            nn.Linear(128, 64),
            nn.ReLU(),
            nn.Linear(64, 32),
            nn.ReLU(),
            nn.Linear(32, 1)
        )

    def forward(self, user_ids, item_ids):
        user_emb = self.user_embedding(user_ids)
        item_emb = self.item_embedding(item_ids)

        # 串接而非內積
        x = torch.cat([user_emb, item_emb], dim=-1)

        # 通過 MLP
        output = self.mlp(x)
        return output.squeeze()
```

**Emma**：「深度學習模型這麼多，該選哪個？」

**David**：「根據場景：」

| 模型 | 適合場景 | 優勢 | 缺點 |
|------|----------|------|------|
| **Two-Tower** | 大規模檢索（億級商品） | 可離線計算商品向量 | 缺少精細交叉 |
| **DCN** | 需要特徵交叉（廣告 CTR） | 顯式建模交叉 | 計算成本高 |
| **NCF** | 純協同過濾場景 | 比矩陣分解更強 | 冷啟動問題 |

## Act 5: 召回與排序的兩階段架構

**Sarah**：「實際生產環境中，推薦是兩階段的。」

### 兩階段架構

```
階段 1：召回（Retrieval / Candidate Generation）
100 萬商品 → 快速篩選 → 500 個候選

策略：
├─ 協同過濾召回（200 個）
├─ 內容召回（100 個）
├─ 熱門商品（100 個）
└─ 用戶歷史相關（100 個）

階段 2：排序（Ranking）
500 個候選 → 精細排序 → Top 10 推薦

使用複雜模型：
- 考慮更多特徵
- 精確預測點擊率/購買率
- 多目標優化（點擊 + 購買 + 停留時間）
```

**為什麼要兩階段？**

**Michael**：「權衡效能與準確度。」

```
如果只用排序模型：
100 萬商品 × 複雜模型 → 延遲 10 秒（不可接受）

兩階段：
100 萬 → 500（簡單模型，10ms）
500 → 10（複雜模型，80ms）
總延遲：90ms（可接受！）
```

### 召回策略組合

```python
class MultiRecallStrategy:
    def __init__(self):
        self.strategies = {
            'collaborative': CollaborativeRecall(),
            'content': ContentBasedRecall(),
            'popular': PopularItemsRecall(),
            'user_history': UserHistoryRecall(),
            'real_time': RealTimeRecall()
        }

    def recall(self, user_id, num_candidates=500):
        """多路召回"""
        all_candidates = []

        # 1. 協同過濾召回（200 個）
        cf_candidates = self.strategies['collaborative'].recall(user_id, 200)
        all_candidates.extend([(c, 'cf') for c in cf_candidates])

        # 2. 內容召回（100 個）
        content_candidates = self.strategies['content'].recall(user_id, 100)
        all_candidates.extend([(c, 'content') for c in content_candidates])

        # 3. 熱門商品（100 個）
        popular = self.strategies['popular'].recall(user_id, 100)
        all_candidates.extend([(c, 'popular') for c in popular])

        # 4. 用戶歷史相關（100 個）
        history = self.strategies['user_history'].recall(user_id, 100)
        all_candidates.extend([(c, 'history') for c in history])

        # 5. 去重
        unique_items = {}
        for item_id, source in all_candidates:
            if item_id not in unique_items:
                unique_items[item_id] = {'sources': [source], 'score': 0}
            else:
                unique_items[item_id]['sources'].append(source)

        # 6. 多路召回融合分數
        for item_id, info in unique_items.items():
            # 出現在越多召回源，分數越高
            info['score'] = len(info['sources'])

        # 7. 排序取 Top-K
        sorted_items = sorted(
            unique_items.items(),
            key=lambda x: x[1]['score'],
            reverse=True
        )

        return [item_id for item_id, _ in sorted_items[:num_candidates]]
```

### 精排模型 - Wide & Deep

**David**：「Google 的 Wide & Deep 模型：記憶 + 泛化。」

```
Wide（線性模型）：
- 記憶：歷史共現規則
- 例如：用戶點擊過 A，通常也會點 B

Deep（深度模型）：
- 泛化：從特徵學習
- 例如：年輕女性喜歡時尚類商品
```

**實作：**

```python
class WideAndDeep(nn.Module):
    def __init__(self, num_wide_features, num_deep_features):
        super().__init__()

        # Wide：線性模型
        self.wide = nn.Linear(num_wide_features, 1)

        # Deep：DNN
        self.deep = nn.Sequential(
            nn.Linear(num_deep_features, 256),
            nn.ReLU(),
            nn.Dropout(0.2),
            nn.Linear(256, 128),
            nn.ReLU(),
            nn.Linear(128, 64),
            nn.ReLU(),
            nn.Linear(64, 1)
        )

    def forward(self, wide_features, deep_features):
        # Wide 輸出
        wide_out = self.wide(wide_features)

        # Deep 輸出
        deep_out = self.deep(deep_features)

        # 組合
        output = wide_out + deep_out
        return torch.sigmoid(output)

# 訓練
model = WideAndDeep(num_wide_features=100, num_deep_features=200)
optimizer = torch.optim.Adam(model.parameters(), lr=0.001)
criterion = nn.BCELoss()

for wide_feat, deep_feat, labels in dataloader:
    pred = model(wide_feat, deep_feat)
    loss = criterion(pred, labels)

    optimizer.zero_grad()
    loss.backward()
    optimizer.step()
```

## Act 6: 實時特徵與線上學習

**Emma**：「用戶剛剛點擊了一個商品，能立即影響推薦嗎？」

**Sarah**：「這就需要實時特徵工程和線上學習。」

### 實時特徵

```
離線特徵（批次更新，每天一次）：
- 用戶人口統計資訊：年齡、性別、地區
- 用戶長期興趣：過去 30 天的瀏覽類別分佈
- 商品屬性：類別、品牌、價格

實時特徵（即時更新）：
- 用戶最近 10 次點擊
- 當前瀏覽session的行為序列
- 商品實時熱度（最近 1 小時的點擊數）
- 實時庫存狀態
```

**架構：**

```python
class RealTimeFeatureStore:
    def __init__(self):
        self.redis = redis.Redis(host='localhost', port=6379)

    def update_user_action(self, user_id, item_id, action_type):
        """更新用戶實時行為"""
        key = f"user:{user_id}:recent_actions"

        # 添加到 List（最近 N 次行為）
        self.redis.lpush(key, json.dumps({
            'item_id': item_id,
            'action': action_type,  # 'click', 'view', 'cart', 'purchase'
            'timestamp': time.time()
        }))

        # 只保留最近 100 條
        self.redis.ltrim(key, 0, 99)

        # 設定過期時間（24 小時）
        self.redis.expire(key, 86400)

    def get_user_recent_actions(self, user_id, limit=10):
        """獲取用戶最近行為"""
        key = f"user:{user_id}:recent_actions"
        actions = self.redis.lrange(key, 0, limit-1)
        return [json.loads(a) for a in actions]

    def update_item_popularity(self, item_id):
        """更新商品實時熱度"""
        # 使用 HyperLogLog 統計去重用戶數
        key_hour = f"item:{item_id}:popularity:{datetime.now().hour}"
        self.redis.pfadd(key_hour, user_id)
        self.redis.expire(key_hour, 7200)  # 2 小時過期

    def get_item_popularity(self, item_id):
        """獲取商品最近 1 小時熱度"""
        current_hour = datetime.now().hour
        key = f"item:{item_id}:popularity:{current_hour}"
        return self.redis.pfcount(key)
```

### 用戶行為序列建模

**Michael**：「Transformer 建模用戶興趣演化。」

```python
class UserBehaviorSequenceModel(nn.Module):
    def __init__(self, num_items, embedding_dim=128):
        super().__init__()

        self.item_embedding = nn.Embedding(num_items, embedding_dim)

        # Transformer Encoder
        encoder_layer = nn.TransformerEncoderLayer(
            d_model=embedding_dim,
            nhead=8,
            dim_feedforward=512,
            dropout=0.1
        )
        self.transformer = nn.TransformerEncoder(encoder_layer, num_layers=3)

        # 輸出層
        self.output = nn.Linear(embedding_dim, num_items)

    def forward(self, item_sequence):
        """
        item_sequence: [batch_size, seq_len]
        """
        # Embedding
        x = self.item_embedding(item_sequence)  # [batch, seq_len, emb_dim]

        # Transformer（學習序列模式）
        x = x.transpose(0, 1)  # [seq_len, batch, emb_dim]
        x = self.transformer(x)
        x = x.transpose(0, 1)

        # 取最後一個時間步
        last_hidden = x[:, -1, :]  # [batch, emb_dim]

        # 預測下一個商品
        logits = self.output(last_hidden)  # [batch, num_items]

        return logits

# 使用
model = UserBehaviorSequenceModel(num_items=100000)

# 用戶最近 20 次點擊序列
sequence = torch.tensor([[42, 123, 456, ..., 789]])  # [1, 20]

# 預測用戶接下來可能點擊的商品
next_items = model(sequence)
top_10 = torch.topk(next_items, k=10)
```

### 線上學習 - Bandit 算法

**David**：「探索與利用的平衡：Thompson Sampling。」

```python
class ThompsonSamplingBandit:
    def __init__(self, num_arms):
        self.num_arms = num_arms
        # Beta 分佈參數（成功次數 α, 失敗次數 β）
        self.alpha = np.ones(num_arms)
        self.beta = np.ones(num_arms)

    def select_arm(self):
        """選擇要推薦的商品"""
        # 從每個商品的 Beta 分佈中採樣
        samples = np.random.beta(self.alpha, self.beta)

        # 選擇採樣值最大的商品
        return np.argmax(samples)

    def update(self, arm, reward):
        """更新商品的統計資訊"""
        if reward == 1:  # 用戶點擊/購買
            self.alpha[arm] += 1
        else:  # 用戶未點擊
            self.beta[arm] += 1

# 使用
bandit = ThompsonSamplingBandit(num_arms=1000)

for _ in range(10000):
    # 選擇推薦商品
    item = bandit.select_arm()

    # 展示給用戶，獲得反饋
    reward = show_to_user_and_get_feedback(item)

    # 更新模型
    bandit.update(item, reward)
```

**優勢：**
- 自動平衡探索（嘗試新商品）與利用（推薦已知好商品）
- 適應用戶興趣變化
- 無需離線重訓練

## Act 7: 重排序與業務約束

**Sarah**：「排序完成後，還需要重排序（Re-ranking）考慮業務目標。」

### 多樣性

**問題：**
```
排序後的推薦列表：
1. iPhone 13 Pro Max
2. iPhone 13 Pro
3. iPhone 13
4. iPhone 12 Pro Max
5. iPhone 12 Pro

全是 iPhone！用戶選擇有限。
```

**解決：MMR (Maximal Marginal Relevance)**

```python
def mmr_rerank(items, scores, lambda_param=0.5, top_k=10):
    """
    平衡相關性與多樣性
    lambda_param: 1=純相關性, 0=純多樣性
    """
    selected = []
    remaining = list(range(len(items)))

    # 選擇第一個（分數最高）
    first = np.argmax(scores)
    selected.append(first)
    remaining.remove(first)

    # 迭代選擇剩餘商品
    while len(selected) < top_k and remaining:
        mmr_scores = []

        for idx in remaining:
            # 相關性分數
            relevance = scores[idx]

            # 與已選商品的最大相似度
            max_similarity = max([
                similarity(items[idx], items[s])
                for s in selected
            ])

            # MMR 分數
            mmr = lambda_param * relevance - (1 - lambda_param) * max_similarity
            mmr_scores.append(mmr)

        # 選擇 MMR 分數最高的
        best_idx = remaining[np.argmax(mmr_scores)]
        selected.append(best_idx)
        remaining.remove(best_idx)

    return selected

# 重排序後：
# 1. iPhone 13 Pro Max
# 2. Samsung Galaxy S21 (不同品牌)
# 3. MacBook Air (不同類別)
# 4. AirPods Pro
# 5. iPad Pro
```

### 新鮮度

**Emma**：「如何提升新商品的曝光？」

**Michael**：「時間衰減 + 新品加權。」

```python
def apply_freshness_boost(items, scores, current_time):
    """新品加權"""
    boosted_scores = []

    for item, score in zip(items, scores):
        # 計算商品年齡（天數）
        age_days = (current_time - item.created_at).days

        # 時間衰減函數
        if age_days <= 3:
            boost = 1.5  # 3 天內新品，分數 × 1.5
        elif age_days <= 7:
            boost = 1.2  # 7 天內，分數 × 1.2
        elif age_days <= 30:
            boost = 1.0  # 30 天內，不加權
        else:
            boost = 0.9  # 舊商品，稍微降權

        boosted_scores.append(score * boost)

    return boosted_scores
```

### 業務規則

**David**：「硬約束與軟約束。」

```python
class BusinessRuleReranker:
    def rerank(self, user, items, scores):
        """應用業務規則重排序"""

        # 1. 硬約束：過濾不符合的商品
        items, scores = self.apply_hard_constraints(user, items, scores)

        # 2. 軟約束：調整分數
        scores = self.apply_soft_constraints(user, items, scores)

        # 3. 重新排序
        sorted_indices = np.argsort(scores)[::-1]
        return [items[i] for i in sorted_indices]

    def apply_hard_constraints(self, user, items, scores):
        """硬約束：必須滿足"""
        filtered_items = []
        filtered_scores = []

        for item, score in zip(items, scores):
            # 規則 1：庫存必須 > 0
            if item.stock <= 0:
                continue

            # 規則 2：不推薦已購買的商品
            if item.id in user.purchased_items:
                continue

            # 規則 3：年齡限制
            if item.age_restricted and user.age < 18:
                continue

            filtered_items.append(item)
            filtered_scores.append(score)

        return filtered_items, filtered_scores

    def apply_soft_constraints(self, user, items, scores):
        """軟約束：調整分數"""
        adjusted_scores = []

        for item, score in zip(items, scores):
            # 規則 1：利潤率高的商品加權
            if item.profit_margin > 0.5:
                score *= 1.2

            # 規則 2：庫存積壓的商品加權
            if item.stock > 1000:
                score *= 1.1

            # 規則 3：用戶 VIP 等級影響
            if user.is_vip:
                score *= 1.15

            adjusted_scores.append(score)

        return adjusted_scores
```

### 多目標優化

**Sarah**：「不只優化點擊率，還要考慮轉換率、利潤等。」

```python
class MultiObjectiveRanker:
    def __init__(self, weights):
        """
        weights: {'ctr': 0.4, 'cvr': 0.4, 'profit': 0.2}
        """
        self.weights = weights

    def predict(self, user, item):
        """預測多個目標"""
        # 預測點擊率（CTR）
        ctr = self.ctr_model.predict(user, item)

        # 預測轉換率（CVR）
        cvr = self.cvr_model.predict(user, item)

        # 預計利潤
        profit = item.price * item.profit_margin

        return {
            'ctr': ctr,
            'cvr': cvr,
            'profit': profit
        }

    def score(self, predictions):
        """綜合評分"""
        # 正規化
        ctr_norm = predictions['ctr']
        cvr_norm = predictions['cvr']
        profit_norm = predictions['profit'] / 1000  # 假設最大利潤 1000

        # 加權求和
        final_score = (
            self.weights['ctr'] * ctr_norm +
            self.weights['cvr'] * cvr_norm +
            self.weights['profit'] * profit_norm
        )

        return final_score
```

**Emma**：「推薦系統的完整流程總結一下！」

**Michael**：「完整架構：」

```
用戶請求推薦
    ↓
1. 召回階段（多路召回）
   ├─ 協同過濾
   ├─ 內容推薦
   ├─ 熱門商品
   └─ 實時興趣
   → 500 個候選
    ↓
2. 粗排階段
   - 簡單模型快速打分
   → 100 個候選
    ↓
3. 精排階段
   - Wide & Deep / DCN
   - 考慮更多特徵
   → 20 個候選
    ↓
4. 重排序階段
   ├─ 多樣性調整
   ├─ 新鮮度加權
   ├─ 業務規則
   └─ 多目標優化
   → Top 10 推薦
    ↓
5. 展示給用戶
    ↓
6. 收集反饋 → 線上學習
```

---

## 總結

**David**：「推薦系統是機器學習與工程的完美結合。」

| 階段 | 核心技術 | 關鍵指標 |
|------|----------|----------|
| **召回** | 協同過濾、內容推薦、深度學習 | 召回率、多樣性 |
| **排序** | Wide & Deep、DCN、Two-Tower | CTR、CVR、AUC |
| **重排序** | MMR、業務規則、多目標優化 | 用戶滿意度、GMV |
| **線上** | 實時特徵、Bandit、A/B Testing | 實時效果、系統延遲 |

**透過本章學習，你掌握了：**

1. ✅ **協同過濾**：User-based、Item-based、矩陣分解
2. ✅ **內容推薦**：TF-IDF、BERT Embeddings
3. ✅ **深度學習**：Two-Tower、DCN、NCF
4. ✅ **召回排序**：多路召回、Wide & Deep
5. ✅ **實時特徵**：Redis Feature Store、序列建模
6. ✅ **重排序**：多樣性、新鮮度、業務約束
7. ✅ **完整架構**：從召回到展示的端到端系統

**Emma**：「現在我完全理解如何設計一個生產級的推薦系統了！」

**Sarah**：「恭喜！你已經掌握了從基礎到進階的推薦技術。」

**Michael**：「下一章將整合所有知識，打造一個完整的推薦平台。」
