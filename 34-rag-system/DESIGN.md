# RAG 系統設計：讓 AI 基於你的知識庫回答問題

> 本文檔採用蘇格拉底式對話法（Socratic Method）呈現系統設計的思考過程

## Act 1: LLM 的知識邊界

**場景**：Emma 的公司想要建立一個客服 AI，能夠回答產品相關問題

**Emma**：「我們已經有 ChatGPT 了，為什麼還需要 RAG 系統？」

**David**：「很好的問題！讓我們看一個實際例子：」

```
用戶：「你們的 Pro Max 方案包含哪些功能？」

ChatGPT：「抱歉，我無法訪問實時的產品資訊。我的訓練數據截止於 2024 年 1 月...」
```

**Sarah**：「問題在於 LLM 有三個天生的限制：」

1. **知識截止日期**：訓練完成後的新資訊它不知道
2. **沒有私有資料**：你的內部文件、客戶資料它看不到
3. **會編造答案（Hallucination）**：不知道答案時可能會瞎編

**Michael**：「RAG（Retrieval-Augmented Generation）就是解決方案：」

```
傳統 LLM：
用戶問題 → LLM → 答案（可能不準確）

RAG 系統：
用戶問題 → 搜尋知識庫 → 找到相關文檔 → LLM（基於文檔回答） → 準確答案
```

**Emma**：「所以 RAG 就是讓 LLM 先查資料再回答？」

**David**：「正確！就像學生考試時可以翻書一樣。」

## Act 2: RAG 的核心流程

**Sarah**：「RAG 的完整流程有兩個階段：」

### 階段一：建立知識庫（Indexing）

```
1. 文檔載入
   PDF、Word、網頁 → 純文字

2. 文檔切割（Chunking）
   長文檔 → 小段落（chunk）

   原始文檔（5000 字）
   ↓
   Chunk 1（500 字）：產品介紹...
   Chunk 2（500 字）：價格方案...
   Chunk 3（500 字）：技術規格...

3. 向量化（Embedding）
   文字 → 數字向量

   "Pro Max 方案包含無限儲存空間"
   ↓
   [0.23, -0.45, 0.67, ..., 0.12]  (1536 維向量)

4. 儲存到向量資料庫
   Vector DB（Pinecone、Weaviate、Qdrant）
```

**Michael**：「為什麼要轉成向量？」

**David**：「因為語意相似的文字，向量也會相近：」

```python
# 範例（簡化為 3 維）
"無限儲存空間" → [0.8, 0.6, 0.2]
"不限容量"     → [0.7, 0.5, 0.3]  # 很接近！
"紅色汽車"     → [0.1, 0.2, 0.9]  # 很遠

# 計算相似度（餘弦相似度）
similarity("無限儲存空間", "不限容量") = 0.95  # 很相似
similarity("無限儲存空間", "紅色汽車") = 0.12  # 不相似
```

### 階段二：檢索與生成（Retrieval & Generation）

```
1. 用戶問題
   "Pro Max 方案有什麼功能？"
   ↓

2. 問題向量化
   [0.75, 0.55, 0.25, ...]
   ↓

3. 向量搜尋（找最相似的 chunks）
   Vector DB 搜尋 → Top 3 最相關的段落

   Chunk #127（相似度 0.92）：Pro Max 方案包含無限儲存...
   Chunk #45 （相似度 0.87）：Pro Max 提供 24/7 客服...
   Chunk #203（相似度 0.84）：Pro Max 支援 API 整合...
   ↓

4. 構建 Prompt
   """
   基於以下文檔回答問題：

   [文檔 1] Pro Max 方案包含無限儲存...
   [文檔 2] Pro Max 提供 24/7 客服...
   [文檔 3] Pro Max 支援 API 整合...

   問題：Pro Max 方案有什麼功能？

   請根據上述文檔回答，如果文檔中沒有相關資訊，請說「我不知道」。
   """
   ↓

5. LLM 生成答案
   "根據資料，Pro Max 方案主要功能包括：
   1. 無限儲存空間
   2. 24/7 客服支援
   3. API 整合能力..."
```

**Emma**：「這樣 LLM 就不會瞎編了！因為它被要求只基於文檔回答。」

**Sarah**：「沒錯！這就是 RAG 的核心價值。」

## Act 3: Chunking 策略 - 如何切割文檔

**Michael**：「為什麼要把文檔切成小段？為什麼不整篇丟給 LLM？」

**David**：「三個原因：」

1. **Token 限制**：LLM 有輸入長度限制（4K、8K、128K tokens）
2. **成本控制**：輸入越長，費用越高
3. **檢索精度**：小段落更容易精準匹配問題

**Sarah**：「Chunking 有多種策略：」

### 策略 1：固定大小切割

```python
def chunk_by_size(text, chunk_size=500, overlap=50):
    """
    chunk_size: 每個 chunk 的字數
    overlap: 重疊字數（避免切斷語意）
    """
    chunks = []
    start = 0

    while start < len(text):
        end = start + chunk_size
        chunk = text[start:end]
        chunks.append(chunk)
        start = end - overlap  # 重疊 50 字

    return chunks

# 範例
原文：「...產品介紹。我們的 Pro Max 方案包含無限儲存空間。此外還提供...」

Chunk 1（0-500）：「...產品介紹。我們的 Pro Max 方案包含無限儲存空間。此外還...」
Chunk 2（450-950）：「...無限儲存空間。此外還提供 24/7 客服支援。另外...」
                    ↑ 重疊部分，避免切斷「無限儲存空間」這個概念
```

**優點**：簡單、快速
**缺點**：可能切斷段落，破壞語意

### 策略 2：按段落切割

```python
def chunk_by_paragraph(text, max_chunk_size=1000):
    """按段落切割，但不超過最大長度"""
    paragraphs = text.split('\n\n')
    chunks = []
    current_chunk = ""

    for para in paragraphs:
        if len(current_chunk) + len(para) < max_chunk_size:
            current_chunk += para + "\n\n"
        else:
            if current_chunk:
                chunks.append(current_chunk)
            current_chunk = para + "\n\n"

    if current_chunk:
        chunks.append(current_chunk)

    return chunks
```

**優點**：保留語意完整性
**缺點**：chunk 大小不均勻

### 策略 3：語意切割（Semantic Chunking）

```python
def semantic_chunking(text, embedding_model):
    """根據語意相似度切割"""
    sentences = split_into_sentences(text)
    embeddings = [embedding_model.encode(s) for s in sentences]

    chunks = []
    current_chunk = [sentences[0]]

    for i in range(1, len(sentences)):
        # 計算前後句子的相似度
        similarity = cosine_similarity(embeddings[i-1], embeddings[i])

        if similarity > 0.8:  # 語意相近，繼續加入同一個 chunk
            current_chunk.append(sentences[i])
        else:  # 語意跳躍，開始新 chunk
            chunks.append(' '.join(current_chunk))
            current_chunk = [sentences[i]]

    chunks.append(' '.join(current_chunk))
    return chunks
```

**優點**：語意完整、檢索精度高
**缺點**：計算成本高

**Emma**：「實務上用哪種策略？」

**Michael**：「通常混合使用：先按段落切，如果段落太長再按語意切。」

## Act 4: Embedding 模型選擇

**David**：「Embedding 就是把文字轉成向量。有很多選擇：」

### 選項 1：OpenAI text-embedding-3-large

```python
from openai import OpenAI

client = OpenAI(api_key="sk-...")

response = client.embeddings.create(
    model="text-embedding-3-large",
    input="Pro Max 方案包含無限儲存空間"
)

vector = response.data[0].embedding  # 3072 維向量
```

**規格**：
- 向量維度：3072
- 成本：$0.13 / 1M tokens
- 效能：MTEB 排行榜前 5%

### 選項 2：Cohere embed-multilingual-v3.0

```python
import cohere

co = cohere.Client(api_key="...")

response = co.embed(
    texts=["Pro Max 方案包含無限儲存空間"],
    model="embed-multilingual-v3.0",
    input_type="search_document"  # 或 "search_query"
)

vector = response.embeddings[0]  # 1024 維向量
```

**規格**：
- 向量維度：1024
- 成本：$0.10 / 1M tokens
- 特色：區分文檔 embedding 與查詢 embedding

### 選項 3：開源模型（sentence-transformers）

```python
from sentence_transformers import SentenceTransformer

model = SentenceTransformer('paraphrase-multilingual-mpnet-base-v2')

vector = model.encode("Pro Max 方案包含無限儲存空間")  # 768 維向量
```

**規格**：
- 向量維度：768
- 成本：免費（自己部署）
- 延遲：本地推理，< 50ms

**Sarah**：「如何選擇？」

```
決策樹：

成本敏感？
├─ Yes → 開源模型（sentence-transformers）
└─ No →
    需要多語言支援？
    ├─ Yes → Cohere multilingual
    └─ No → OpenAI text-embedding-3
```

**Michael**：「還有一個重要考量：向量維度。」

```
維度越高：
✅ 語意捕捉更精確
✅ 檢索準確度更高
❌ 儲存空間更大（3072 vs 768 = 4 倍）
❌ 搜尋速度更慢

建議：
- 小型知識庫（< 10萬文檔）：使用高維度（3072）
- 大型知識庫（> 100萬文檔）：使用低維度（768）+ 重排序
```

## Act 5: 向量資料庫選擇

**Emma**：「向量要儲存在哪裡？普通資料庫不行嗎？」

**David**：「理論上可以，但向量搜尋需要特殊優化：」

```sql
-- PostgreSQL with pgvector (可行但慢)
SELECT * FROM documents
ORDER BY embedding <-> '[0.23, -0.45, ...]'::vector
LIMIT 5;

-- 問題：
-- 1. 全表掃描，慢（> 1秒）
-- 2. 無法處理大規模資料（> 100萬筆）
```

**Sarah**：「專門的向量資料庫使用 ANN（Approximate Nearest Neighbor）演算法：」

### 選項對比

| 資料庫 | 類型 | 速度 | 規模 | 成本 | 適合場景 |
|--------|------|------|------|------|----------|
| **Pinecone** | 雲端 | ⚡⚡⚡ | 10億+ | $$$ | 生產環境、零維運 |
| **Weaviate** | 開源/雲端 | ⚡⚡ | 1000萬+ | $$ | 混合搜尋（向量+關鍵字） |
| **Qdrant** | 開源/雲端 | ⚡⚡⚡ | 1億+ | $$ | 高效能、自主部署 |
| **Milvus** | 開源 | ⚡⚡ | 10億+ | $ | 大規模、Kubernetes |
| **Chroma** | 開源 | ⚡ | 10萬 | Free | 開發、原型 |
| **pgvector** | PostgreSQL 擴充 | ⚡ | 100萬 | $ | 已有 PG、簡單場景 |

### Pinecone 範例（最簡單）

```python
import pinecone

# 初始化
pinecone.init(api_key="...", environment="us-west1-gcp")

# 建立索引
index = pinecone.Index("my-knowledge-base")

# 插入向量
index.upsert(vectors=[
    {
        "id": "doc-1",
        "values": [0.23, -0.45, 0.67, ...],  # 向量
        "metadata": {
            "text": "Pro Max 方案包含無限儲存空間",
            "source": "pricing.pdf",
            "page": 3
        }
    }
])

# 搜尋
results = index.query(
    vector=[0.75, 0.55, 0.25, ...],  # 問題的向量
    top_k=5,
    include_metadata=True
)

for match in results.matches:
    print(f"相似度：{match.score}")
    print(f"文字：{match.metadata['text']}")
```

### Qdrant 範例（開源、高效能）

```python
from qdrant_client import QdrantClient
from qdrant_client.models import Distance, VectorParams, PointStruct

client = QdrantClient(host="localhost", port=6333)

# 建立 collection
client.create_collection(
    collection_name="knowledge_base",
    vectors_config=VectorParams(size=1536, distance=Distance.COSINE)
)

# 插入
client.upsert(
    collection_name="knowledge_base",
    points=[
        PointStruct(
            id=1,
            vector=[0.23, -0.45, 0.67, ...],
            payload={
                "text": "Pro Max 方案包含無限儲存空間",
                "source": "pricing.pdf"
            }
        )
    ]
)

# 搜尋
results = client.search(
    collection_name="knowledge_base",
    query_vector=[0.75, 0.55, 0.25, ...],
    limit=5
)
```

**Michael**：「實務建議：」

```
開發階段：Chroma（免費、簡單）
    ↓
小規模生產（< 100萬文檔）：Pinecone（零維運）
    ↓
大規模生產（> 100萬文檔）：Qdrant（自主部署、成本低）
    ↓
超大規模（> 10億文檔）：Milvus（分散式架構）
```

## Act 6: 重排序（Reranking）提升準確度

**Emma**：「向量搜尋有時會找到不太相關的結果，怎麼辦？」

**David**：「向量搜尋是『語意相似』，但不一定『真正相關』：」

```
問題：「Pro Max 方案的價格是多少？」

向量搜尋結果（Top 5）：
1. "Pro Max 方案包含無限儲存空間"（相似度 0.85）← 相似但不相關
2. "Enterprise 方案價格為 $999/月"（相似度 0.82）← 相關但不是 Pro Max
3. "Pro Max 定價為 $499/月"（相似度 0.78）← 最相關！但排第 3
4. "我們提供多種付款方式"（相似度 0.76）
5. "Pro 方案與 Pro Max 的差異"（相似度 0.75）
```

**Sarah**：「Reranking 就是用更精確的模型重新排序：」

```
流程：
1. 向量搜尋（快速、召回率高）→ Top 100 結果
2. Reranking（慢但準確）→ Top 5 最相關結果
3. 丟給 LLM 生成答案
```

### 使用 Cohere Rerank API

```python
import cohere

co = cohere.Client(api_key="...")

# 向量搜尋得到的結果
candidates = [
    "Pro Max 方案包含無限儲存空間",
    "Enterprise 方案價格為 $999/月",
    "Pro Max 定價為 $499/月",
    "我們提供多種付款方式",
    "Pro 方案與 Pro Max 的差異"
]

# Rerank
results = co.rerank(
    query="Pro Max 方案的價格是多少？",
    documents=candidates,
    model="rerank-multilingual-v2.0",
    top_n=3
)

for doc in results:
    print(f"排名 {doc.index}: {candidates[doc.index]} (分數: {doc.relevance_score})")

# 輸出：
# 排名 2: Pro Max 定價為 $499/月 (分數: 0.95) ← 現在排第一了！
# 排名 0: Pro Max 方案包含無限儲存空間 (分數: 0.72)
# 排名 4: Pro 方案與 Pro Max 的差異 (分數: 0.68)
```

### 開源 Reranking 模型

```python
from sentence_transformers import CrossEncoder

model = CrossEncoder('cross-encoder/ms-marco-MiniLM-L-6-v2')

# 計算問題與每個候選文檔的相關分數
query = "Pro Max 方案的價格是多少？"
scores = model.predict([
    (query, "Pro Max 方案包含無限儲存空間"),
    (query, "Enterprise 方案價格為 $999/月"),
    (query, "Pro Max 定價為 $499/月"),
])

# scores = [0.42, 0.38, 0.89] ← 第 3 個最高分
```

**Michael**：「Reranking 的效能提升：」

```
準確度指標（NDCG@5）：
- 僅向量搜尋：0.72
- 向量搜尋 + Reranking：0.89（提升 24%）

成本：
- Cohere Rerank API：$2 / 1000 次搜尋
- 自部署模型：免費，但需要 GPU（推理時間 ~100ms）

建議：
- 高價值查詢（客戶諮詢、法律問答）→ 使用 Reranking
- 一般查詢（內部搜尋）→ 僅向量搜尋
```

## Act 7: 混合搜尋與進階技巧

**Sarah**：「向量搜尋很強大,但有個盲點：」

```
問題：「產品型號 XB-2049 的規格」

向量搜尋：
❌ "XB-2049" 是唯一識別碼，不是語意
❌ 向量無法精準匹配型號、日期、專有名詞
```

**David**：「解決方案：混合搜尋（Hybrid Search）= 向量 + 關鍵字」

### 混合搜尋範例（Weaviate）

```python
import weaviate

client = weaviate.Client("http://localhost:8080")

result = client.query.get("Document", ["text", "source"]) \
    .with_hybrid(
        query="產品型號 XB-2049 的規格",
        alpha=0.5  # 0=純關鍵字, 1=純向量, 0.5=混合
    ) \
    .with_limit(5) \
    .do()

# alpha=0.5 意思是：
# - 50% 權重給向量搜尋（語意相似）
# - 50% 權重給 BM25 關鍵字搜尋（精確匹配）
```

### 自己實作混合搜尋

```python
def hybrid_search(query, vector_db, bm25_index, alpha=0.5):
    """
    alpha: 0=純關鍵字, 1=純向量
    """
    # 1. 向量搜尋
    vector_results = vector_db.search(query, top_k=20)
    vector_scores = {doc.id: doc.score for doc in vector_results}

    # 2. BM25 關鍵字搜尋
    bm25_results = bm25_index.search(query, top_k=20)
    bm25_scores = {doc.id: doc.score for doc in bm25_results}

    # 3. 正規化分數到 [0, 1]
    vector_scores = normalize(vector_scores)
    bm25_scores = normalize(bm25_scores)

    # 4. 合併分數
    all_doc_ids = set(vector_scores.keys()) | set(bm25_scores.keys())
    hybrid_scores = {}

    for doc_id in all_doc_ids:
        v_score = vector_scores.get(doc_id, 0)
        b_score = bm25_scores.get(doc_id, 0)
        hybrid_scores[doc_id] = alpha * v_score + (1 - alpha) * b_score

    # 5. 排序
    ranked = sorted(hybrid_scores.items(), key=lambda x: x[1], reverse=True)
    return ranked[:5]
```

### 進階技巧 1：Query Expansion（查詢擴展）

```python
def expand_query(query, llm):
    """用 LLM 生成相關的查詢變體"""
    prompt = f"""
    原始問題：{query}

    請生成 3 個意思相近但用詞不同的問題變體：
    1.
    2.
    3.
    """

    variations = llm.generate(prompt)

    # 對每個變體做向量搜尋，合併結果
    all_results = []
    for variant in variations:
        results = vector_db.search(variant, top_k=5)
        all_results.extend(results)

    # 去重、重排序
    return deduplicate_and_rank(all_results)

# 範例：
# 原始：「如何取消訂閱？」
# 變體 1：「怎麼解除會員資格？」
# 變體 2：「退訂流程是什麼？」
# 變體 3：「如何停止自動續約？」
```

### 進階技巧 2：Hypothetical Document Embeddings (HyDE)

```python
def hyde_search(query, llm, vector_db):
    """先用 LLM 生成假設性答案，再用答案做向量搜尋"""

    # 1. 讓 LLM 生成假設性答案（可能不準確，沒關係）
    hypothetical_answer = llm.generate(f"請回答：{query}")

    # 2. 用這個假設性答案做向量搜尋
    # 理論：答案的向量會比問題的向量更接近真實文檔
    results = vector_db.search(hypothetical_answer, top_k=5)

    # 3. 用找到的真實文檔再讓 LLM 生成最終答案
    final_answer = llm.generate_with_context(query, results)

    return final_answer

# 範例：
# 問題：「降低客戶流失率的方法？」
# HyDE 假設答案：「可以提供優惠、改善客服、增加功能...」
# 搜尋結果：找到公司內部的「客戶留存策略文件」（因為內容相似）
```

### 進階技巧 3：Multi-Query Retrieval

```python
def multi_query_retrieval(query, llm, vector_db):
    """從不同角度生成多個子問題"""

    # 1. 生成子問題
    prompt = f"""
    原始問題：{query}

    請將這個問題拆解成 3 個更具體的子問題：
    """
    sub_queries = llm.generate(prompt)  # ["子問題1", "子問題2", "子問題3"]

    # 2. 對每個子問題做檢索
    all_docs = []
    for sub_q in sub_queries:
        docs = vector_db.search(sub_q, top_k=3)
        all_docs.extend(docs)

    # 3. 去重、合併
    unique_docs = deduplicate(all_docs)

    # 4. 用所有文檔生成答案
    return llm.generate_with_context(query, unique_docs)

# 範例：
# 原始問題：「如何提升網站效能？」
# 子問題 1：「資料庫查詢優化方法」
# 子問題 2：「前端載入速度優化」
# 子問題 3：「CDN 和快取策略」
```

**Emma**：「這些技巧都要用嗎？」

**Michael**：「根據場景選擇：」

```
基礎 RAG（夠用）：
向量搜尋 → LLM 生成

中階 RAG（提升準確度）：
向量搜尋 → Reranking → LLM 生成

高階 RAG（最高品質）：
Query Expansion + 混合搜尋 → Reranking → Multi-Query → LLM 生成

權衡：
準確度 ⬆️
延遲 ⬆️ (從 1 秒 → 5 秒)
成本 ⬆️ (多次 LLM 呼叫)
```

**David**：「對了，還有一個關鍵問題要處理...」

**Sarah**：「什麼？」

**David**：「如果檢索到的文檔根本不相關，該怎麼辦？」

**Michael**：「這就需要設定相似度門檻：」

```python
def safe_rag_query(query, vector_db, llm, threshold=0.7):
    """設定最低相似度門檻"""

    results = vector_db.search(query, top_k=5)

    # 過濾低於門檻的結果
    relevant_docs = [doc for doc in results if doc.score >= threshold]

    if not relevant_docs:
        return "抱歉，我在知識庫中找不到相關資訊。請聯繫客服人員。"

    # 有足夠相關的文檔才回答
    return llm.generate_with_context(query, relevant_docs)
```

**Emma**：「明白了！RAG 系統的完整架構是：」

```
用戶問題
    ↓
1. Query Expansion（可選）
    ↓
2. 混合搜尋（向量 + 關鍵字）
    ↓
3. Reranking（重排序）
    ↓
4. 相似度過濾（門檻檢查）
    ↓
5. Prompt 構建
    ↓
6. LLM 生成答案
    ↓
7. 答案 + 來源引用
```

**David**：「完全正確！你已經掌握了 RAG 系統的精髓。」

---

## 總結：RAG vs Fine-tuning

**Sarah**：「最後一個問題：什麼時候用 RAG？什麼時候用 Fine-tuning？」

| 場景 | RAG | Fine-tuning |
|------|-----|-------------|
| **知識更新頻繁** | ✅ 即時更新 | ❌ 需要重新訓練 |
| **資料量** | ✅ 少量文檔即可 | ❌ 需大量訓練資料 |
| **可解釋性** | ✅ 可追溯來源 | ❌ 黑盒子 |
| **成本** | 💰 便宜（僅 API 費用） | 💰💰💰 昂貴（GPU、訓練時間） |
| **延遲** | ⏱️ 稍慢（檢索+生成） | ⚡ 快（僅生成） |
| **準確度** | 🎯 依賴檢索品質 | 🎯🎯 高（模型內化知識） |
| **適用場景** | 文檔問答、客服、知識庫 | 特定領域語言風格、推理能力 |

**Michael**：「實務建議：」

```
99% 的情況 → 先用 RAG
    ↓
RAG 解決不了（需要特殊推理、風格）→ 考慮 Fine-tuning
    ↓
最佳方案 → RAG + Fine-tuned Model（兩者結合）
```

**Emma**：「太棒了！我現在完全理解 RAG 系統了。讓我們開始建置吧！」

**David**：「下一章我們會看到完整的技術實作細節。」
