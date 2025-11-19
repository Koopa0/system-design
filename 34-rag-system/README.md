# RAG (Retrieval-Augmented Generation) 系統

## 系統概述

RAG 系統透過結合資訊檢索與大型語言模型，讓 AI 能夠基於私有知識庫準確回答問題，解決 LLM 的三大限制：

1. **知識時效性**：訓練數據截止日期後的新資訊
2. **私有資料存取**：企業內部文件、產品文檔、客戶資料
3. **幻覺問題（Hallucination）**：基於檢索結果回答，降低編造答案的機率

### 核心流程

```
階段一：建立知識庫（離線）
文檔載入 → 切割 (Chunking) → 向量化 (Embedding) → 存入向量資料庫

階段二：查詢回答（線上）
用戶問題 → 向量化 → 向量搜尋 → 重排序 → 構建 Prompt → LLM 生成答案
```

### 應用場景

- **企業知識庫問答**：員工手冊、SOP、技術文件
- **客服自動化**：產品說明、FAQ、政策文件
- **法律/醫療諮詢**：法規條文、判例、醫學文獻
- **程式碼助手**：API 文件、內部程式碼庫
- **研究助理**：論文檢索、文獻綜述

## 功能需求

### 1. 核心功能

#### 1.1 文檔處理
- 支援多種格式：PDF、Word、Markdown、HTML、純文字
- 文檔解析與清理
- 智慧分塊（Chunking）：固定大小、段落切割、語意切割
- 元資料提取：標題、作者、日期、來源

#### 1.2 向量化與索引
- Embedding 模型整合：OpenAI、Cohere、開源模型
- 批次處理：大量文檔平行處理
- 增量索引：新增/更新/刪除文檔
- 版本控制：文檔更新歷史

#### 1.3 檢索系統
- 向量搜尋：語意相似度匹配
- 混合搜尋：向量 + 關鍵字（BM25）
- 重排序（Reranking）：提升精準度
- 過濾條件：依元資料篩選（日期、來源、分類）

#### 1.4 生成與回答
- Prompt 工程：上下文注入、指令優化
- 來源引用：標註答案來源
- 信心分數：評估答案可靠度
- 串流輸出：即時回應

### 2. 非功能需求

| 需求 | 指標 | 說明 |
|------|------|------|
| **準確度** | > 90% | 答案與人工標註一致性 |
| **檢索延遲** | < 200ms | 向量搜尋時間 |
| **端到端延遲** | < 3s | 從問題到完整答案 |
| **吞吐量** | 1,000 QPS | 並發查詢支援 |
| **索引速度** | 10,000 docs/min | 文檔處理速度 |
| **可用性** | 99.9% | 服務穩定性 |

## 技術架構

### 系統架構圖

```
┌─────────────────────────────────────────────────────────────────┐
│                          Client Layer                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │   Web    │  │  Mobile  │  │   API    │  │  Slack   │       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                         API Gateway                              │
│              (限流、認證、負載均衡)                               │
└─────────────────────────────────────────────────────────────────┘
                              │
                ┌─────────────┴─────────────┐
                ▼                           ▼
┌───────────────────────────┐   ┌───────────────────────────┐
│    Indexing Pipeline      │   │     Query Pipeline        │
│  (離線/背景處理)           │   │     (線上服務)            │
└───────────────────────────┘   └───────────────────────────┘
        │                                   │
        ▼                                   ▼
┌──────────────────┐              ┌──────────────────┐
│  Document        │              │  Query           │
│  Processor       │              │  Processor       │
│  ┌────────────┐  │              │  ┌────────────┐  │
│  │ PDF Parser │  │              │  │ Query      │  │
│  │ Chunker    │  │              │  │ Expansion  │  │
│  │ Metadata   │  │              │  │ Embedding  │  │
│  └────────────┘  │              │  └────────────┘  │
└──────────────────┘              └──────────────────┘
        │                                   │
        ▼                                   ▼
┌──────────────────┐              ┌──────────────────┐
│  Embedding       │              │  Retrieval       │
│  Service         │              │  Service         │
│  ┌────────────┐  │              │  ┌────────────┐  │
│  │ OpenAI     │  │              │  │ Vector     │  │
│  │ Cohere     │  │              │  │ Search     │  │
│  │ Local Model│  │              │  │ Hybrid     │  │
│  └────────────┘  │              │  │ Search     │  │
└──────────────────┘              │  └────────────┘  │
        │                         │  ┌────────────┐  │
        ▼                         │  │ Reranker   │  │
┌──────────────────┐              │  └────────────┘  │
│  Vector Database │◄─────────────┘                  │
│  ┌────────────┐  │              └──────────────────┘
│  │ Qdrant     │  │                        │
│  │ Pinecone   │  │                        ▼
│  │ Weaviate   │  │              ┌──────────────────┐
│  └────────────┘  │              │  Generation      │
└──────────────────┘              │  Service         │
        │                         │  ┌────────────┐  │
        ▼                         │  │ LLM Client │  │
┌──────────────────┐              │  │ Prompt     │  │
│  Metadata Store  │              │  │ Engine     │  │
│  (PostgreSQL)    │              │  │ Citation   │  │
│  ┌────────────┐  │              │  └────────────┘  │
│  │ Documents  │  │              └──────────────────┘
│  │ Chunks     │  │                        │
│  │ Jobs       │  │                        ▼
│  └────────────┘  │              ┌──────────────────┐
└──────────────────┘              │  Response        │
        │                         │  ┌────────────┐  │
        ▼                         │  │ Answer     │  │
┌──────────────────┐              │  │ Sources    │  │
│  Object Storage  │              │  │ Confidence │  │
│  (S3)            │              │  └────────────┘  │
│  ┌────────────┐  │              └──────────────────┘
│  │ Raw Docs   │  │
│  │ Processed  │  │
│  └────────────┘  │
└──────────────────┘
```

### 技術棧

| 層級 | 技術選型 | 原因 |
|------|----------|------|
| **API 框架** | Go + Gin | 高效能、低延遲 |
| **文檔處理** | Apache Tika / PyPDF2 | 多格式支援 |
| **Embedding** | OpenAI / sentence-transformers | 高品質向量 |
| **向量資料庫** | Qdrant | 開源、高效能、易部署 |
| **元資料庫** | PostgreSQL | 結構化資料、JSONB 支援 |
| **快取** | Redis | 查詢快取、去重 |
| **訊息佇列** | Kafka | 索引任務、事件驅動 |
| **物件儲存** | MinIO / S3 | 原始文檔儲存 |
| **監控** | Prometheus + Grafana | 指標與視覺化 |
| **日誌** | ELK Stack | 查詢追蹤、除錯 |

## 資料庫設計

### 1. 文檔表 (documents)

```sql
CREATE TABLE documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(500) NOT NULL,
    source VARCHAR(500) NOT NULL,        -- 檔案路徑或 URL
    source_type VARCHAR(50) NOT NULL,    -- 'pdf', 'docx', 'html', 'text'
    content_hash VARCHAR(64) NOT NULL UNIQUE,  -- SHA-256, 去重用
    file_size BIGINT,                    -- bytes
    page_count INTEGER,
    author VARCHAR(200),
    metadata JSONB,                      -- 自訂元資料
    status VARCHAR(20) NOT NULL,         -- 'pending', 'processing', 'indexed', 'failed'
    indexed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_documents_status ON documents(status);
CREATE INDEX idx_documents_source ON documents(source);
CREATE INDEX idx_documents_content_hash ON documents(content_hash);
CREATE INDEX idx_documents_metadata ON documents USING GIN(metadata);
```

### 2. 文檔塊表 (chunks)

```sql
CREATE TABLE chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,        -- 在文檔中的順序
    content TEXT NOT NULL,               -- chunk 的文字內容
    content_length INTEGER NOT NULL,
    vector_id VARCHAR(100) UNIQUE,       -- 對應向量資料庫的 ID
    embedding_model VARCHAR(50),         -- 使用的 embedding 模型
    page_number INTEGER,                 -- 對應頁數（如果適用）
    section_title VARCHAR(500),          -- 所屬章節
    metadata JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    UNIQUE(document_id, chunk_index)
);

CREATE INDEX idx_chunks_document_id ON chunks(document_id);
CREATE INDEX idx_chunks_vector_id ON chunks(vector_id);
CREATE INDEX idx_chunks_embedding_model ON chunks(embedding_model);
```

### 3. 查詢日誌表 (queries)

```sql
CREATE TABLE queries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id),
    query_text TEXT NOT NULL,
    query_embedding_id VARCHAR(100),     -- 問題的向量 ID
    retrieved_chunks UUID[] NOT NULL,    -- 檢索到的 chunk IDs
    rerank_scores FLOAT[],               -- 重排序後的分數
    generated_answer TEXT,
    answer_sources JSONB,                -- 引用來源
    confidence_score FLOAT,              -- 0-1
    latency_ms INTEGER,                  -- 總延遲
    retrieval_latency_ms INTEGER,        -- 檢索延遲
    generation_latency_ms INTEGER,       -- 生成延遲
    llm_model VARCHAR(50),
    llm_tokens INTEGER,
    user_feedback VARCHAR(20),           -- 'positive', 'negative', null
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_queries_user_id ON queries(user_id);
CREATE INDEX idx_queries_created_at ON queries(created_at);
CREATE INDEX idx_queries_user_feedback ON queries(user_feedback) WHERE user_feedback IS NOT NULL;
```

### 4. 索引任務表 (indexing_jobs)

```sql
CREATE TABLE indexing_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type VARCHAR(50) NOT NULL,       -- 'index', 'reindex', 'delete'
    document_ids UUID[] NOT NULL,
    status VARCHAR(20) NOT NULL,         -- 'pending', 'running', 'completed', 'failed'
    total_documents INTEGER,
    processed_documents INTEGER DEFAULT 0,
    total_chunks INTEGER,
    processed_chunks INTEGER DEFAULT 0,
    error_message TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_indexing_jobs_status ON indexing_jobs(status);
CREATE INDEX idx_indexing_jobs_created_at ON indexing_jobs(created_at);
```

### 5. Embedding 快取表 (embedding_cache)

```sql
CREATE TABLE embedding_cache (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    text_hash VARCHAR(64) NOT NULL UNIQUE,  -- SHA-256(text)
    text TEXT NOT NULL,
    embedding_model VARCHAR(50) NOT NULL,
    vector_id VARCHAR(100) NOT NULL,        -- 向量資料庫 ID
    hit_count INTEGER DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    last_accessed_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_embedding_cache_text_hash ON embedding_cache(text_hash);
CREATE INDEX idx_embedding_cache_model ON embedding_cache(embedding_model);
```

## 核心功能實作

### 1. 文檔處理與切割

```go
package processor

import (
    "crypto/sha256"
    "fmt"
    "strings"
)

type DocumentProcessor struct {
    parsers map[string]Parser
}

type Parser interface {
    Parse(filePath string) (*Document, error)
}

type Document struct {
    Title    string
    Content  string
    Metadata map[string]interface{}
    Pages    []Page
}

type Page struct {
    Number  int
    Content string
}

func (p *DocumentProcessor) Process(filePath string, fileType string) ([]Chunk, error) {
    // 1. 解析文檔
    parser, exists := p.parsers[fileType]
    if !exists {
        return nil, fmt.Errorf("unsupported file type: %s", fileType)
    }

    doc, err := parser.Parse(filePath)
    if err != nil {
        return nil, err
    }

    // 2. 計算內容雜湊（去重）
    contentHash := computeHash(doc.Content)

    // 3. 切割文檔
    chunker := NewSemanticChunker(500, 50) // chunk_size=500, overlap=50
    chunks := chunker.Chunk(doc)

    // 4. 豐富元資料
    for i, chunk := range chunks {
        chunk.DocumentHash = contentHash
        chunk.Index = i
        chunk.Metadata = doc.Metadata
    }

    return chunks, nil
}

func computeHash(content string) string {
    h := sha256.New()
    h.Write([]byte(content))
    return fmt.Sprintf("%x", h.Sum(nil))
}

// 語意切割器
type SemanticChunker struct {
    MaxChunkSize int
    Overlap      int
}

func (c *SemanticChunker) Chunk(doc *Document) []Chunk {
    // 1. 按段落分割
    paragraphs := strings.Split(doc.Content, "\n\n")

    chunks := []Chunk{}
    currentChunk := ""
    currentPage := 1

    for _, para := range paragraphs {
        para = strings.TrimSpace(para)
        if para == "" {
            continue
        }

        // 如果加入這個段落會超過大小限制
        if len(currentChunk)+len(para) > c.MaxChunkSize && currentChunk != "" {
            // 儲存當前 chunk
            chunks = append(chunks, Chunk{
                Content:      currentChunk,
                ContentLength: len(currentChunk),
                PageNumber:   currentPage,
            })

            // 開始新 chunk，保留 overlap
            words := strings.Fields(currentChunk)
            overlapWords := c.Overlap
            if len(words) < overlapWords {
                overlapWords = len(words)
            }
            currentChunk = strings.Join(words[len(words)-overlapWords:], " ") + " " + para
        } else {
            if currentChunk != "" {
                currentChunk += "\n\n"
            }
            currentChunk += para
        }
    }

    // 最後一個 chunk
    if currentChunk != "" {
        chunks = append(chunks, Chunk{
            Content:       currentChunk,
            ContentLength: len(currentChunk),
            PageNumber:    currentPage,
        })
    }

    return chunks
}

type Chunk struct {
    DocumentHash  string
    Index         int
    Content       string
    ContentLength int
    PageNumber    int
    SectionTitle  string
    Metadata      map[string]interface{}
}
```

### 2. Embedding 服務

```go
package embedding

import (
    "context"
    "crypto/sha256"
    "fmt"
)

type EmbeddingService struct {
    provider Provider
    cache    *Cache
    db       *sql.DB
}

type Provider interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
    ModelName() string
}

// OpenAI Provider
type OpenAIProvider struct {
    client *openai.Client
    model  string
}

func (p *OpenAIProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
        Model: p.model,
        Input: texts,
    })
    if err != nil {
        return nil, err
    }

    embeddings := make([][]float32, len(resp.Data))
    for i, item := range resp.Data {
        embeddings[i] = item.Embedding
    }

    return embeddings, nil
}

func (p *OpenAIProvider) Dimensions() int {
    switch p.model {
    case "text-embedding-3-large":
        return 3072
    case "text-embedding-3-small":
        return 1536
    default:
        return 1536
    }
}

// 本地模型 Provider
type LocalProvider struct {
    modelPath string
    model     *transformers.Model
}

func (p *LocalProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    // 使用本地 sentence-transformers 模型
    embeddings, err := p.model.Encode(texts)
    return embeddings, err
}

// Embedding 服務主邏輯
func (s *EmbeddingService) EmbedChunks(ctx context.Context, chunks []Chunk) error {
    texts := make([]string, len(chunks))
    for i, chunk := range chunks {
        texts[i] = chunk.Content
    }

    // 1. 檢查快取
    uncachedIndices := []int{}
    uncachedTexts := []string{}

    for i, text := range texts {
        hash := computeTextHash(text)
        cached, err := s.cache.Get(ctx, hash, s.provider.ModelName())

        if err != nil || cached == nil {
            uncachedIndices = append(uncachedIndices, i)
            uncachedTexts = append(uncachedTexts, text)
        } else {
            // 使用快取的向量
            chunks[i].VectorID = cached.VectorID
        }
    }

    // 2. 批次生成 embeddings（未快取的）
    if len(uncachedTexts) > 0 {
        embeddings, err := s.provider.Embed(ctx, uncachedTexts)
        if err != nil {
            return err
        }

        // 3. 儲存到向量資料庫
        vectorIDs, err := s.storeVectors(ctx, embeddings, uncachedTexts)
        if err != nil {
            return err
        }

        // 4. 更新 chunks 和快取
        for i, idx := range uncachedIndices {
            chunks[idx].VectorID = vectorIDs[i]

            // 寫入快取
            s.cache.Set(ctx, CacheEntry{
                TextHash:       computeTextHash(uncachedTexts[i]),
                Text:           uncachedTexts[i],
                EmbeddingModel: s.provider.ModelName(),
                VectorID:       vectorIDs[i],
            })
        }
    }

    return nil
}

func (s *EmbeddingService) storeVectors(ctx context.Context, embeddings [][]float32, texts []string) ([]string, error) {
    // 儲存到 Qdrant
    points := make([]*qdrant.PointStruct, len(embeddings))

    for i, emb := range embeddings {
        vectorID := generateUUID()
        points[i] = &qdrant.PointStruct{
            Id:      vectorID,
            Vector:  emb,
            Payload: map[string]interface{}{"text": texts[i]},
        }
    }

    err := s.vectorDB.Upsert(ctx, "knowledge_base", points)
    if err != nil {
        return nil, err
    }

    vectorIDs := make([]string, len(points))
    for i, p := range points {
        vectorIDs[i] = p.Id
    }

    return vectorIDs, nil
}

func computeTextHash(text string) string {
    h := sha256.New()
    h.Write([]byte(text))
    return fmt.Sprintf("%x", h.Sum(nil))
}
```

### 3. 檢索服務

```go
package retrieval

import (
    "context"
    "sort"
)

type RetrievalService struct {
    vectorDB  VectorDatabase
    bm25Index BM25Index
    reranker  Reranker
    db        *sql.DB
}

type SearchRequest struct {
    Query           string
    TopK            int
    HybridAlpha     float64  // 0=純關鍵字, 1=純向量, 0.5=混合
    UseReranking    bool
    MinScore        float64  // 最低相似度門檻
    Filters         map[string]interface{}
}

type SearchResult struct {
    ChunkID    string
    Content    string
    Score      float64
    Source     string
    PageNumber int
    Metadata   map[string]interface{}
}

func (s *RetrievalService) Search(ctx context.Context, req *SearchRequest) ([]SearchResult, error) {
    // 1. 向量搜尋
    vectorResults, err := s.vectorSearch(ctx, req.Query, req.TopK*2)
    if err != nil {
        return nil, err
    }

    // 2. 關鍵字搜尋（如果 alpha < 1）
    var hybridResults []SearchResult
    if req.HybridAlpha < 1.0 {
        bm25Results, err := s.bm25Search(ctx, req.Query, req.TopK*2)
        if err != nil {
            return nil, err
        }

        // 混合分數
        hybridResults = s.hybridScoring(vectorResults, bm25Results, req.HybridAlpha)
    } else {
        hybridResults = vectorResults
    }

    // 3. 重排序
    if req.UseReranking {
        hybridResults, err = s.rerank(ctx, req.Query, hybridResults, req.TopK)
        if err != nil {
            return nil, err
        }
    }

    // 4. 過濾低分結果
    filtered := []SearchResult{}
    for _, result := range hybridResults {
        if result.Score >= req.MinScore {
            filtered = append(filtered, result)
        }
    }

    // 5. 限制返回數量
    if len(filtered) > req.TopK {
        filtered = filtered[:req.TopK]
    }

    // 6. 記錄檢索日誌
    go s.logRetrieval(ctx, req, filtered)

    return filtered, nil
}

func (s *RetrievalService) vectorSearch(ctx context.Context, query string, topK int) ([]SearchResult, error) {
    // 1. 問題向量化
    embedding, err := s.embedder.Embed(ctx, []string{query})
    if err != nil {
        return nil, err
    }

    // 2. 向量搜尋
    qdrantResults, err := s.vectorDB.Search(ctx, SearchParams{
        CollectionName: "knowledge_base",
        Vector:         embedding[0],
        Limit:          topK,
    })
    if err != nil {
        return nil, err
    }

    // 3. 載入 chunk 詳細資訊
    results := make([]SearchResult, len(qdrantResults))
    for i, qr := range qdrantResults {
        chunk, err := s.getChunkByVectorID(ctx, qr.ID)
        if err != nil {
            continue
        }

        results[i] = SearchResult{
            ChunkID:    chunk.ID,
            Content:    chunk.Content,
            Score:      qr.Score,
            Source:     chunk.Source,
            PageNumber: chunk.PageNumber,
            Metadata:   chunk.Metadata,
        }
    }

    return results, nil
}

func (s *RetrievalService) bm25Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
    // BM25 關鍵字搜尋
    scores := s.bm25Index.Search(query, topK)

    results := make([]SearchResult, len(scores))
    for i, score := range scores {
        chunk, _ := s.getChunkByID(ctx, score.ChunkID)
        results[i] = SearchResult{
            ChunkID: score.ChunkID,
            Content: chunk.Content,
            Score:   score.Score,
            Source:  chunk.Source,
        }
    }

    return results, nil
}

func (s *RetrievalService) hybridScoring(vectorResults, bm25Results []SearchResult, alpha float64) []SearchResult {
    // 正規化分數到 [0, 1]
    vectorScores := normalize(vectorResults)
    bm25Scores := normalize(bm25Results)

    // 合併
    scoreMap := make(map[string]float64)
    resultMap := make(map[string]SearchResult)

    for i, r := range vectorResults {
        scoreMap[r.ChunkID] = alpha * vectorScores[i]
        resultMap[r.ChunkID] = r
    }

    for i, r := range bm25Results {
        if score, exists := scoreMap[r.ChunkID]; exists {
            scoreMap[r.ChunkID] = score + (1-alpha)*bm25Scores[i]
        } else {
            scoreMap[r.ChunkID] = (1 - alpha) * bm25Scores[i]
            resultMap[r.ChunkID] = r
        }
    }

    // 排序
    type scoredResult struct {
        result SearchResult
        score  float64
    }

    scored := make([]scoredResult, 0, len(scoreMap))
    for chunkID, score := range scoreMap {
        result := resultMap[chunkID]
        result.Score = score
        scored = append(scored, scoredResult{result, score})
    }

    sort.Slice(scored, func(i, j int) bool {
        return scored[i].score > scored[j].score
    })

    results := make([]SearchResult, len(scored))
    for i, s := range scored {
        results[i] = s.result
    }

    return results
}

func (s *RetrievalService) rerank(ctx context.Context, query string, candidates []SearchResult, topK int) ([]SearchResult, error) {
    // 使用 Cross-Encoder 重排序
    texts := make([]string, len(candidates))
    for i, c := range candidates {
        texts[i] = c.Content
    }

    scores, err := s.reranker.Rerank(ctx, query, texts)
    if err != nil {
        return nil, err
    }

    // 更新分數並重新排序
    for i := range candidates {
        candidates[i].Score = scores[i]
    }

    sort.Slice(candidates, func(i, j int) bool {
        return candidates[i].Score > candidates[j].Score
    })

    if len(candidates) > topK {
        candidates = candidates[:topK]
    }

    return candidates, nil
}

func normalize(results []SearchResult) []float64 {
    if len(results) == 0 {
        return []float64{}
    }

    scores := make([]float64, len(results))
    minScore, maxScore := results[0].Score, results[0].Score

    for _, r := range results {
        if r.Score < minScore {
            minScore = r.Score
        }
        if r.Score > maxScore {
            maxScore = r.Score
        }
    }

    scoreRange := maxScore - minScore
    if scoreRange == 0 {
        for i := range scores {
            scores[i] = 1.0
        }
        return scores
    }

    for i, r := range results {
        scores[i] = (r.Score - minScore) / scoreRange
    }

    return scores
}
```

### 4. 生成服務

```go
package generation

import (
    "context"
    "fmt"
    "strings"
)

type GenerationService struct {
    llm    LLMClient
    config GenerationConfig
}

type GenerationConfig struct {
    Model            string
    MaxContextChunks int
    Temperature      float64
    MaxTokens        int
    IncludeSources   bool
}

type GenerateRequest struct {
    Query          string
    RetrievedChunks []SearchResult
    ConversationHistory []Message
}

type GenerateResponse struct {
    Answer          string
    Sources         []Source
    ConfidenceScore float64
    TokensUsed      int
}

type Source struct {
    ChunkID    string
    Content    string
    Source     string
    PageNumber int
    Relevance  float64
}

func (s *GenerationService) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
    // 1. 構建 context
    context := s.buildContext(req.RetrievedChunks)

    // 2. 構建 prompt
    prompt := s.buildPrompt(req.Query, context, req.RetrievedChunks)

    // 3. 呼叫 LLM
    messages := append(req.ConversationHistory, Message{
        Role:    "user",
        Content: prompt,
    })

    resp, err := s.llm.Chat(ctx, &ChatRequest{
        Model:       s.config.Model,
        Messages:    messages,
        Temperature: s.config.Temperature,
        MaxTokens:   s.config.MaxTokens,
    })

    if err != nil {
        return nil, err
    }

    answer := resp.Choices[0].Message.Content

    // 4. 評估信心分數
    confidence := s.evaluateConfidence(req.RetrievedChunks, answer)

    // 5. 提取來源引用
    sources := s.extractSources(req.RetrievedChunks)

    return &GenerateResponse{
        Answer:          answer,
        Sources:         sources,
        ConfidenceScore: confidence,
        TokensUsed:      resp.Usage.TotalTokens,
    }, nil
}

func (s *GenerationService) buildContext(chunks []SearchResult) string {
    var sb strings.Builder

    // 最多使用前 N 個 chunks
    maxChunks := s.config.MaxContextChunks
    if len(chunks) < maxChunks {
        maxChunks = len(chunks)
    }

    for i := 0; i < maxChunks; i++ {
        chunk := chunks[i]
        sb.WriteString(fmt.Sprintf("\n[文檔 %d]\n", i+1))
        sb.WriteString(fmt.Sprintf("來源：%s", chunk.Source))
        if chunk.PageNumber > 0 {
            sb.WriteString(fmt.Sprintf("（第 %d 頁）", chunk.PageNumber))
        }
        sb.WriteString("\n")
        sb.WriteString(chunk.Content)
        sb.WriteString("\n")
    }

    return sb.String()
}

func (s *GenerationService) buildPrompt(query, context string, chunks []SearchResult) string {
    prompt := fmt.Sprintf(`你是一個專業的知識庫問答助手。請根據以下提供的文檔回答用戶的問題。

重要規則：
1. 只根據提供的文檔回答，不要使用文檔以外的知識
2. 如果文檔中沒有相關資訊，請明確說「根據現有資料，我無法回答這個問題」
3. 回答要準確、簡潔、有條理
4. 如果可能，請引用文檔編號（例如：「根據文檔 1...」）

參考文檔：
%s

用戶問題：%s

請回答：`, context, query)

    return prompt
}

func (s *GenerationService) evaluateConfidence(chunks []SearchResult, answer string) float64 {
    if len(chunks) == 0 {
        return 0.0
    }

    // 簡單的信心評估：基於最高檢索分數
    maxScore := chunks[0].Score

    // 如果答案包含「無法回答」、「不知道」等，降低信心分數
    lowConfidenceKeywords := []string{"無法回答", "不知道", "沒有相關", "找不到"}
    for _, keyword := range lowConfidenceKeywords {
        if strings.Contains(answer, keyword) {
            return maxScore * 0.3
        }
    }

    // 正常情況下，信心分數等於最高檢索分數
    return maxScore
}

func (s *GenerationService) extractSources(chunks []SearchResult) []Source {
    sources := make([]Source, 0, len(chunks))

    for _, chunk := range chunks {
        sources = append(sources, Source{
            ChunkID:    chunk.ChunkID,
            Content:    chunk.Content[:min(len(chunk.Content), 200)], // 摘要
            Source:     chunk.Source,
            PageNumber: chunk.PageNumber,
            Relevance:  chunk.Score,
        })
    }

    return sources
}

// 串流生成
func (s *GenerationService) GenerateStream(ctx context.Context, req *GenerateRequest, callback func(chunk string) error) error {
    context := s.buildContext(req.RetrievedChunks)
    prompt := s.buildPrompt(req.Query, context, req.RetrievedChunks)

    messages := append(req.ConversationHistory, Message{
        Role:    "user",
        Content: prompt,
    })

    return s.llm.ChatStream(ctx, &ChatRequest{
        Model:       s.config.Model,
        Messages:    messages,
        Temperature: s.config.Temperature,
        MaxTokens:   s.config.MaxTokens,
        Stream:      true,
    }, callback)
}
```

## API 文件

### 1. 上傳文檔

```http
POST /api/v1/documents
Content-Type: multipart/form-data
Authorization: Bearer <token>

file: document.pdf
metadata: {
    "category": "product",
    "tags": ["pricing", "features"]
}

Response 201 Created:
{
    "document_id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Product Pricing Guide",
    "status": "pending",
    "job_id": "660e8400-e29b-41d4-a716-446655440000"
}
```

### 2. 查詢索引狀態

```http
GET /api/v1/documents/{document_id}
Authorization: Bearer <token>

Response 200 OK:
{
    "document_id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Product Pricing Guide",
    "status": "indexed",
    "chunks_count": 42,
    "indexed_at": "2025-01-15T10:05:00Z"
}
```

### 3. RAG 查詢

```http
POST /api/v1/query
Content-Type: application/json
Authorization: Bearer <token>

{
    "query": "Pro Max 方案的價格是多少？",
    "top_k": 5,
    "use_reranking": true,
    "min_score": 0.7,
    "filters": {
        "category": "product",
        "tags": ["pricing"]
    }
}

Response 200 OK:
{
    "query_id": "770e8400-e29b-41d4-a716-446655440000",
    "answer": "根據文檔 1，Pro Max 方案的定價為 $499/月。此方案包含無限儲存空間、24/7 客服支援以及 API 整合能力。",
    "confidence_score": 0.92,
    "sources": [
        {
            "chunk_id": "880e8400-e29b-41d4-a716-446655440000",
            "source": "pricing.pdf",
            "page_number": 3,
            "content": "Pro Max 定價為 $499/月...",
            "relevance": 0.95
        },
        {
            "chunk_id": "990e8400-e29b-41d4-a716-446655440000",
            "source": "features.pdf",
            "page_number": 7,
            "content": "Pro Max 方案包含無限儲存空間...",
            "relevance": 0.87
        }
    ],
    "latency_ms": 1250,
    "tokens_used": 850
}
```

### 4. 串流查詢

```http
POST /api/v1/query/stream
Content-Type: application/json
Authorization: Bearer <token>

{
    "query": "Pro Max 方案有哪些功能？",
    "top_k": 5
}

Response 200 OK (Server-Sent Events):
event: retrieval
data: {"retrieved_chunks": 5, "latency_ms": 180}

event: chunk
data: {"content": "根據"}

event: chunk
data: {"content": "文檔"}

event: chunk
data: {"content": " 1，"}

event: chunk
data: {"content": "Pro Max"}

...

event: sources
data: {"sources": [...]}

event: done
data: {"tokens_used": 850}
```

### 5. 對話式查詢（多輪）

```http
POST /api/v1/chat
Content-Type: application/json
Authorization: Bearer <token>

{
    "conversation_id": "aa0e8400-e29b-41d4-a716-446655440000",
    "message": "還有其他方案嗎？",
    "context_messages": [
        {
            "role": "user",
            "content": "Pro Max 方案的價格是多少？"
        },
        {
            "role": "assistant",
            "content": "Pro Max 方案的定價為 $499/月..."
        }
    ]
}

Response 200 OK:
{
    "answer": "除了 Pro Max，我們還提供 Pro 方案（$299/月）和 Enterprise 方案（$999/月）...",
    "sources": [...]
}
```

### 6. 刪除文檔

```http
DELETE /api/v1/documents/{document_id}
Authorization: Bearer <token>

Response 200 OK:
{
    "message": "Document and associated chunks deleted successfully",
    "chunks_deleted": 42
}
```

## 效能優化

### 1. Embedding 快取

```go
// 快取命中率分析
總查詢：100,000 次
快取命中：65,000 次
命中率：65%

成本節省：
- 無快取：100,000 × $0.00013 / 1K tokens × 50 tokens = $65
- 有快取：35,000 × $0.00013 / 1K tokens × 50 tokens = $22.75
- 節省：65% ($42.25)

延遲降低：
- 無快取：平均 150ms（API 呼叫）
- 有快取：平均 5ms（記憶體/Redis 讀取）
- 提升：97%
```

### 2. 批次 Embedding

```go
func (s *EmbeddingService) BatchEmbed(texts []string, batchSize int) ([][]float32, error) {
    var allEmbeddings [][]float32

    for i := 0; i < len(texts); i += batchSize {
        end := i + batchSize
        if end > len(texts) {
            end = len(texts)
        }

        batch := texts[i:end]
        embeddings, err := s.provider.Embed(ctx, batch)
        if err != nil {
            return nil, err
        }

        allEmbeddings = append(allEmbeddings, embeddings...)
    }

    return allEmbeddings, nil
}

// 效能比較
單次呼叫（每次 1 個文字）：
- 1000 次呼叫
- 總時間：150s (150ms/次)
- 成本：$0.13

批次呼叫（每次 100 個）：
- 10 次呼叫
- 總時間：5s (500ms/次 × 10 次)
- 成本：$0.13（相同）
- 時間節省：97%
```

### 3. 向量量化（Quantization）

```go
// 將 float32 向量量化為 int8
func quantizeVector(vector []float32) []int8 {
    quantized := make([]int8, len(vector))

    for i, v := range vector {
        // 映射 [-1, 1] → [-127, 127]
        quantized[i] = int8(v * 127)
    }

    return quantized
}

// 儲存空間比較
float32: 1536 維 × 4 bytes = 6,144 bytes
int8:    1536 維 × 1 byte  = 1,536 bytes
節省：75%

100 萬文檔：
- float32: 6.144 GB
- int8:    1.536 GB
- 節省：4.6 GB

準確度影響：
- 原始：NDCG@10 = 0.89
- 量化：NDCG@10 = 0.87（下降 2.2%）
```

### 4. HNSW 索引優化

```python
# Qdrant HNSW 參數調整
client.create_collection(
    collection_name="knowledge_base",
    vectors_config={
        "size": 1536,
        "distance": "Cosine"
    },
    hnsw_config={
        "m": 16,              # 每個節點的連接數（預設 16）
        "ef_construct": 100,  # 建構時的搜尋深度（預設 100）
    }
)

# 搜尋時調整
client.search(
    collection_name="knowledge_base",
    query_vector=vector,
    limit=10,
    search_params={
        "hnsw_ef": 128  # 搜尋時的深度（越大越準但越慢）
    }
)

# 參數權衡
m=16, ef=100:  建構慢、搜尋快、準確度高
m=8,  ef=50:   建構快、搜尋稍慢、準確度稍低

實測（100 萬向量）：
m=16, ef=128: 搜尋 5ms, 召回率 0.95
m=8,  ef=64:  搜尋 8ms, 召回率 0.91
```

### 5. 查詢結果快取

```go
type QueryCache struct {
    redis *redis.Client
    ttl   time.Duration
}

func (c *QueryCache) Get(ctx context.Context, query string) (*CachedResult, error) {
    key := "query:" + computeHash(query)
    data, err := c.redis.Get(ctx, key).Result()

    if err == redis.Nil {
        return nil, nil // 未快取
    }

    var result CachedResult
    json.Unmarshal([]byte(data), &result)
    return &result, nil
}

func (c *QueryCache) Set(ctx context.Context, query string, result *GenerateResponse) error {
    key := "query:" + computeHash(query)
    data, _ := json.Marshal(result)
    return c.redis.Set(ctx, key, data, c.ttl).Err()
}

// 效能提升
快取命中場景（常見問題）：
- 延遲：3000ms → 50ms（降低 98%）
- 成本：$0.01/查詢 → $0（LLM 呼叫省略）

快取命中率：15%
每日查詢：10,000 次
每日節省：
- LLM 成本：10,000 × 15% × $0.01 = $15
- 每月節省：$450
```

## 監控與告警

### 關鍵指標

```go
var (
    // 查詢延遲
    queryLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "rag_query_latency_seconds",
            Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
        },
        []string{"stage"}, // "retrieval", "reranking", "generation"
    )

    // 檢索準確度
    retrievalAccuracy = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "rag_retrieval_accuracy",
        },
        []string{"method"}, // "vector", "hybrid", "reranked"
    )

    // 用戶反饋
    userFeedback = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rag_user_feedback_total",
        },
        []string{"feedback"}, // "positive", "negative"
    )

    // Embedding 快取命中率
    embeddingCacheHits = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rag_embedding_cache_total",
        },
        []string{"status"}, // "hit", "miss"
    )
)
```

### 告警規則

```yaml
groups:
  - name: rag_system
    interval: 30s
    rules:
      # 查詢延遲過高
      - alert: HighQueryLatency
        expr: |
          histogram_quantile(0.95,
            rate(rag_query_latency_seconds_bucket[5m])
          ) > 5
        for: 5m
        annotations:
          summary: "P95 query latency > 5s"

      # 檢索準確度下降
      - alert: LowRetrievalAccuracy
        expr: rag_retrieval_accuracy < 0.7
        for: 10m
        annotations:
          summary: "Retrieval accuracy < 70%"

      # 負面反饋過多
      - alert: HighNegativeFeedback
        expr: |
          rate(rag_user_feedback_total{feedback="negative"}[1h])
          / rate(rag_user_feedback_total[1h]) > 0.3
        for: 30m
        annotations:
          summary: "Negative feedback rate > 30%"

      # 快取命中率低
      - alert: LowCacheHitRate
        expr: |
          rate(rag_embedding_cache_total{status="hit"}[10m])
          / rate(rag_embedding_cache_total[10m]) < 0.5
        for: 15m
        annotations:
          summary: "Embedding cache hit rate < 50%"
```

## 部署架構

```yaml
# rag-system-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rag-query-service
spec:
  replicas: 10
  selector:
    matchLabels:
      app: rag-query
  template:
    metadata:
      labels:
        app: rag-query
    spec:
      containers:
      - name: rag-query
        image: rag-system:v1.0.0
        resources:
          requests:
            memory: "4Gi"
            cpu: "2000m"
          limits:
            memory: "8Gi"
            cpu: "4000m"
        env:
        - name: QDRANT_URL
          value: "http://qdrant:6333"
        - name: POSTGRES_URL
          valueFrom:
            secretKeyRef:
              name: db-secret
              key: url
        - name: OPENAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: llm-secret
              key: openai-key

---
# Indexing Worker (背景任務)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rag-indexing-worker
spec:
  replicas: 5
  template:
    spec:
      containers:
      - name: indexing-worker
        image: rag-system:v1.0.0
        command: ["./indexing-worker"]
        resources:
          requests:
            memory: "8Gi"
            cpu: "4000m"

---
# Qdrant Vector Database
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: qdrant
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: qdrant
        image: qdrant/qdrant:v1.7.0
        resources:
          requests:
            memory: "16Gi"
            cpu: "4000m"
        volumeMounts:
        - name: qdrant-storage
          mountPath: /qdrant/storage
  volumeClaimTemplates:
  - metadata:
      name: qdrant-storage
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 500Gi
```

## 成本估算

### 每月運營成本（50,000 用戶，每人每天 10 次查詢）

| 項目 | 用量 | 單價 | 月成本 |
|------|------|------|--------|
| **LLM API** | | | |
| GPT-4 Turbo (生成) | 500M tokens | $10/1M | $5,000 |
| GPT-3.5 (Reranking) | 200M tokens | $0.5/1M | $100 |
| **Embedding API** | | | |
| text-embedding-3 | 300M tokens | $0.13/1M | $390 |
| 快取節省（65% 命中） | -195M tokens | | -$254 |
| **基礎設施** | | | |
| PostgreSQL (RDS) | db.r5.2xlarge | $0.504/hr | $365 |
| Redis (ElastiCache) | cache.r5.2xlarge | $0.504/hr | $365 |
| Qdrant (EC2) | 3 × r5.4xlarge | $1.008/hr × 3 | $2,177 |
| Application (EKS) | 15 × c5.2xlarge | $0.34/hr × 15 | $3,672 |
| **儲存** | | | |
| Qdrant Storage (EBS) | 1.5TB SSD | $0.10/GB | $150 |
| S3 (文檔) | 500GB | $0.023/GB | $12 |
| PostgreSQL Storage | 200GB | $0.115/GB | $23 |
| **網路** | | | |
| Data Transfer | 10TB | $0.09/GB | $900 |
| **總計** | | | **$12,900** |

### 成本優化方案

**優化後成本：$7,740（降低 40%）**

1. **使用本地 Embedding 模型**：-$390 + 部署成本 $200 = 節省 $190
2. **Spot Instances**：Application 成本降低 60% = 節省 $2,203
3. **向量量化**：Qdrant 儲存降低 75% = 節省 $113
4. **查詢快取**：LLM 成本降低 15% = 節省 $750
5. **Reserved Instances（Qdrant）**：降低 40% = 節省 $871

## 總結

RAG 系統讓 LLM 能夠基於私有知識庫準確回答問題，核心優勢：

| 特性 | 說明 | 價值 |
|------|------|------|
| **即時更新** | 新文檔上傳即可查詢 | 知識永遠最新 |
| **可追溯性** | 標註答案來源 | 可驗證、可信賴 |
| **成本效益** | 無需 Fine-tuning | 節省訓練成本 |
| **準確度** | 基於事實回答 | 減少幻覺 |
| **可擴展** | 知識庫無限擴充 | 適應業務成長 |

透過本章學習，你掌握了：

1. ✅ **文檔處理**：解析、切割、向量化
2. ✅ **向量檢索**：語意搜尋、混合搜尋
3. ✅ **重排序**：提升精準度
4. ✅ **提示工程**：Context 注入、來源引用
5. ✅ **效能優化**：快取、批次、量化

**下一章**：我們將學習**模型訓練平台**，從資料準備到模型部署的完整 MLOps 流程。
