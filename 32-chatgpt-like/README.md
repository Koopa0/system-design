# ChatGPT-like System（對話式 AI 系統）

> **專案類型**：AI 對話平台
> **技術難度**：★★★★☆
> **核心技術**：LLM API、Server-Sent Events、Token 管理、併發控制

## 目錄

- [系統概述](#系統概述)
- [技術架構](#技術架構)
- [資料庫設計](#資料庫設計)
- [核心功能實作](#核心功能實作)
- [API 文件](#api-文件)
- [效能優化](#效能優化)
- [監控與告警](#監控與告警)
- [部署架構](#部署架構)
- [成本估算](#成本估算)

---

## 系統概述

### 功能需求

| 功能模組 | 描述 | 優先級 |
|---------|------|--------|
| 對話管理 | 多輪對話、上下文記憶 | P0 |
| 流式輸出 | Server-Sent Events 即時返回 | P0 |
| Token 管理 | 計數、限制、計費 | P0 |
| 內容審核 | Moderation API | P0 |
| 用戶管理 | 註冊、登入、配額 | P1 |
| 對話歷史 | 儲存、查詢、分享 | P1 |
| 插件系統 | Function Calling、Tool Use | P2 |

### 非功能需求

| 指標 | 目標值 | 說明 |
|-----|--------|------|
| 首字延遲 | < 1s | 第一個字出現時間 |
| 流式延遲 | < 100ms | 每個 chunk 間隔 |
| 可用性 | 99.9% | 年停機時間 < 8.76 小時 |
| 併發用戶 | 10,000+ | 同時在線用戶 |
| Token 準確率 | 100% | 計費不允許錯誤 |

---

## 技術架構

### 系統架構圖

```
┌──────────────────────────────────────────────────┐
│                   Client Layer                   │
│         (Web App / Mobile App / API)             │
└───────────────┬──────────────────────────────────┘
                │ HTTPS / WebSocket / SSE
                ↓
┌──────────────────────────────────────────────────┐
│            API Gateway (Nginx)                   │
│  - Rate Limiting (100 req/min per user)         │
│  - SSL Termination                               │
│  - Request Routing                               │
└───────────────┬──────────────────────────────────┘
                │
       ┌────────┴────────┐
       │                 │
       ↓                 ↓
┌─────────────┐   ┌──────────────┐
│  Chat API   │   │  Moderation  │
│  Service    │   │  Service     │
└──────┬──────┘   └──────┬───────┘
       │                 │
       ↓                 ↓
┌──────────────────────────────────┐
│     Request Queue (Redis)        │
│  - Rate Limiter                  │
│  - Priority Queue                │
└──────┬───────────────────────────┘
       │
       ↓
┌──────────────────────────────────┐
│   LLM Worker Pool (Go)           │
│  ┌────────────────────────────┐  │
│  │  Worker 1: OpenAI GPT-4    │  │
│  │  Worker 2: Claude 2        │  │
│  │  Worker 3: GPT-3.5         │  │
│  └────────────────────────────┘  │
└──────┬───────┬───────────────┬───┘
       │       │               │
       ↓       ↓               ↓
┌──────────┐ ┌─────┐  ┌────────────┐
│PostgreSQL│ │Redis│  │ LLM APIs   │
│(Convs)   │ │Cache│  │ (External) │
└──────────┘ └─────┘  └────────────┘
```

### 技術棧

| 層級 | 技術選型 | 說明 |
|-----|---------|------|
| **API 層** | Go + Gin | 高效能 HTTP 服務 |
| **流式輸出** | Server-Sent Events | 單向即時通訊 |
| **快取** | Redis | 回應快取、Rate Limiting |
| **資料庫** | PostgreSQL | 對話歷史 |
| **訊息佇列** | Redis Queue | 請求排隊 |
| **LLM Provider** | OpenAI, Anthropic | 第三方 API |
| **Token 計數** | tiktoken-go | Token 編碼 |
| **監控** | Prometheus + Grafana | 指標監控 |

---

## 資料庫設計

### 1. Users（用戶表）

```sql
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,

    -- 配額
    monthly_token_quota BIGINT NOT NULL DEFAULT 100000,  -- 每月 10 萬 Token
    used_tokens BIGINT NOT NULL DEFAULT 0,
    quota_reset_at TIMESTAMP NOT NULL,

    -- API Key
    api_key VARCHAR(64) UNIQUE,

    -- 時間戳
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- 索引
    INDEX idx_email (email),
    INDEX idx_api_key (api_key)
);
```

### 2. Conversations（對話表）

```sql
CREATE TABLE conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGINT NOT NULL REFERENCES users(id),

    -- 對話資訊
    title VARCHAR(255),                  -- 自動生成標題
    model VARCHAR(50) NOT NULL,          -- gpt-4, claude-2, etc.

    -- 統計
    message_count INT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,

    -- 時間戳
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- 索引
    INDEX idx_user_id (user_id),
    INDEX idx_created_at (created_at)
);
```

### 3. Messages（訊息表）

```sql
CREATE TABLE messages (
    id BIGSERIAL PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,

    -- 訊息內容
    role VARCHAR(20) NOT NULL,           -- system, user, assistant
    content TEXT NOT NULL,

    -- Token 資訊
    prompt_tokens INT,
    completion_tokens INT,
    total_tokens INT,

    -- 模型資訊
    model VARCHAR(50),
    finish_reason VARCHAR(50),           -- stop, length, content_filter

    -- 時間戳
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- 索引
    INDEX idx_conversation_id (conversation_id),
    INDEX idx_created_at (created_at)
);
```

### 4. Usage Logs（使用記錄表）

```sql
CREATE TABLE usage_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    conversation_id UUID REFERENCES conversations(id),

    -- 使用資訊
    model VARCHAR(50) NOT NULL,
    prompt_tokens INT NOT NULL,
    completion_tokens INT NOT NULL,
    total_tokens INT NOT NULL,

    -- 成本（美元，保留 6 位小數）
    cost NUMERIC(10, 6) NOT NULL,

    -- 時間戳
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- 索引
    INDEX idx_user_id_created (user_id, created_at),
    INDEX idx_created_at (created_at)
);

-- 按月分表
-- 表名格式：usage_logs_YYYYMM
```

### 5. Cached Responses（快取回應表）

```sql
CREATE TABLE cached_responses (
    id BIGSERIAL PRIMARY KEY,

    -- 快取鍵（提示詞的 SHA-256）
    prompt_hash VARCHAR(64) UNIQUE NOT NULL,

    -- 快取內容
    model VARCHAR(50) NOT NULL,
    response TEXT NOT NULL,

    -- 統計
    hit_count INT NOT NULL DEFAULT 0,

    -- 過期時間
    expires_at TIMESTAMP NOT NULL,

    -- 時間戳
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_accessed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- 索引
    INDEX idx_prompt_hash (prompt_hash),
    INDEX idx_expires_at (expires_at)
);
```

---

## 核心功能實作

### 1. LLM 客戶端（完整實作）

```go
package llm

import (
    "bufio"
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

// Client LLM 客戶端
type Client struct {
    APIKey     string
    BaseURL    string
    HTTPClient *http.Client
}

// NewClient 建立客戶端
func NewClient(apiKey, baseURL string) *Client {
    return &Client{
        APIKey:  apiKey,
        BaseURL: baseURL,
        HTTPClient: &http.Client{
            Timeout: 60 * time.Second,
        },
    }
}

// ChatRequest 對話請求
type ChatRequest struct {
    Model       string    `json:"model"`
    Messages    []Message `json:"messages"`
    Temperature float64   `json:"temperature,omitempty"`
    MaxTokens   int       `json:"max_tokens,omitempty"`
    Stream      bool      `json:"stream,omitempty"`
}

// Message 訊息
type Message struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

// ChatResponse 對話回應
type ChatResponse struct {
    ID      string   `json:"id"`
    Object  string   `json:"object"`
    Created int64    `json:"created"`
    Model   string   `json:"model"`
    Choices []Choice `json:"choices"`
    Usage   Usage    `json:"usage"`
}

// Choice 選項
type Choice struct {
    Index        int     `json:"index"`
    Message      Message `json:"message"`
    FinishReason string  `json:"finish_reason"`
}

// Usage Token 使用量
type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}

// Chat 發送對話請求（非流式）
func (c *Client) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
    reqBody, err := json.Marshal(req)
    if err != nil {
        return nil, fmt.Errorf("序列化請求失敗: %w", err)
    }

    httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/chat/completions", bytes.NewBuffer(reqBody))
    if err != nil {
        return nil, err
    }

    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

    resp, err := c.HTTPClient.Do(httpReq)
    if err != nil {
        return nil, fmt.Errorf("發送請求失敗: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("API 錯誤 [%d]: %s", resp.StatusCode, string(body))
    }

    var chatResp ChatResponse
    if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
        return nil, fmt.Errorf("解析回應失敗: %w", err)
    }

    return &chatResp, nil
}

// StreamChunk 流式回應片段
type StreamChunk struct {
    ID      string         `json:"id"`
    Object  string         `json:"object"`
    Created int64          `json:"created"`
    Model   string         `json:"model"`
    Choices []StreamChoice `json:"choices"`
}

// StreamChoice 流式選項
type StreamChoice struct {
    Index        int          `json:"index"`
    Delta        MessageDelta `json:"delta"`
    FinishReason string       `json:"finish_reason,omitempty"`
}

// MessageDelta 訊息增量
type MessageDelta struct {
    Role    string `json:"role,omitempty"`
    Content string `json:"content,omitempty"`
}

// ChatStream 發送對話請求（流式）
func (c *Client) ChatStream(ctx context.Context, req *ChatRequest, callback func(chunk string) error) error {
    req.Stream = true

    reqBody, err := json.Marshal(req)
    if err != nil {
        return err
    }

    httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/chat/completions", bytes.NewBuffer(reqBody))
    if err != nil {
        return err
    }

    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
    httpReq.Header.Set("Accept", "text/event-stream")

    resp, err := c.HTTPClient.Do(httpReq)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("API 錯誤 [%d]: %s", resp.StatusCode, string(body))
    }

    reader := bufio.NewReader(resp.Body)

    for {
        line, err := reader.ReadBytes('\n')
        if err != nil {
            if err == io.EOF {
                break
            }
            return err
        }

        // SSE 格式：data: {json}
        if !bytes.HasPrefix(line, []byte("data: ")) {
            continue
        }

        data := bytes.TrimPrefix(line, []byte("data: "))
        data = bytes.TrimSpace(data)

        // [DONE] 表示結束
        if bytes.Equal(data, []byte("[DONE]")) {
            break
        }

        // 解析 JSON
        var chunk StreamChunk
        if err := json.Unmarshal(data, &chunk); err != nil {
            continue
        }

        // 提取內容
        if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
            if err := callback(chunk.Choices[0].Delta.Content); err != nil {
                return err
            }
        }
    }

    return nil
}
```

### 2. 對話服務

```go
package service

import (
    "context"
    "fmt"
    "time"
)

// ConversationService 對話服務
type ConversationService struct {
    llmClient   *llm.Client
    convRepo    ConversationRepository
    msgRepo     MessageRepository
    tokenCounter *TokenCounter
    cache       *ResponseCache
}

// SendMessage 發送訊息（非流式）
func (s *ConversationService) SendMessage(ctx context.Context, conversationID, userMessage string) (*MessageResponse, error) {
    // 1. 載入對話
    conv, err := s.convRepo.GetByID(ctx, conversationID)
    if err != nil {
        return nil, err
    }

    // 2. 載入歷史訊息
    messages, err := s.msgRepo.ListByConversation(ctx, conversationID)
    if err != nil {
        return nil, err
    }

    // 3. 添加用戶訊息
    userMsg := &Message{
        ConversationID: conversationID,
        Role:           "user",
        Content:        userMessage,
        CreatedAt:      time.Now(),
    }

    if err := s.msgRepo.Create(ctx, userMsg); err != nil {
        return nil, err
    }

    // 4. 構建 LLM 請求
    llmMessages := make([]llm.Message, 0, len(messages)+1)

    for _, msg := range messages {
        llmMessages = append(llmMessages, llm.Message{
            Role:    msg.Role,
            Content: msg.Content,
        })
    }

    llmMessages = append(llmMessages, llm.Message{
        Role:    "user",
        Content: userMessage,
    })

    // 5. 呼叫 LLM
    req := &llm.ChatRequest{
        Model:    conv.Model,
        Messages: llmMessages,
    }

    resp, err := s.llmClient.Chat(ctx, req)
    if err != nil {
        return nil, err
    }

    // 6. 儲存 AI 回應
    assistantMsg := &Message{
        ConversationID:   conversationID,
        Role:             "assistant",
        Content:          resp.Choices[0].Message.Content,
        PromptTokens:     resp.Usage.PromptTokens,
        CompletionTokens: resp.Usage.CompletionTokens,
        TotalTokens:      resp.Usage.TotalTokens,
        Model:            resp.Model,
        FinishReason:     resp.Choices[0].FinishReason,
        CreatedAt:        time.Now(),
    }

    if err := s.msgRepo.Create(ctx, assistantMsg); err != nil {
        return nil, err
    }

    // 7. 更新對話統計
    conv.MessageCount += 2 // user + assistant
    conv.TotalTokens += int64(resp.Usage.TotalTokens)
    conv.UpdatedAt = time.Now()

    if err := s.convRepo.Update(ctx, conv); err != nil {
        return nil, err
    }

    return &MessageResponse{
        Message:      assistantMsg,
        Usage:        resp.Usage,
        FinishReason: resp.Choices[0].FinishReason,
    }, nil
}

// SendMessageStream 發送訊息（流式）
func (s *ConversationService) SendMessageStream(ctx context.Context, conversationID, userMessage string, callback func(chunk string) error) error {
    // 類似 SendMessage，但使用 ChatStream
    conv, err := s.convRepo.GetByID(ctx, conversationID)
    if err != nil {
        return err
    }

    messages, err := s.msgRepo.ListByConversation(ctx, conversationID)
    if err != nil {
        return err
    }

    // 添加用戶訊息
    userMsg := &Message{
        ConversationID: conversationID,
        Role:           "user",
        Content:        userMessage,
        CreatedAt:      time.Now(),
    }

    s.msgRepo.Create(ctx, userMsg)

    // 構建請求
    llmMessages := make([]llm.Message, 0, len(messages)+1)
    for _, msg := range messages {
        llmMessages = append(llmMessages, llm.Message{
            Role:    msg.Role,
            Content: msg.Content,
        })
    }

    llmMessages = append(llmMessages, llm.Message{
        Role:    "user",
        Content: userMessage,
    })

    req := &llm.ChatRequest{
        Model:    conv.Model,
        Messages: llmMessages,
    }

    // 收集完整回應（用於儲存）
    var fullResponse string

    // 流式呼叫
    err = s.llmClient.ChatStream(ctx, req, func(chunk string) error {
        fullResponse += chunk
        return callback(chunk)
    })

    if err != nil {
        return err
    }

    // 儲存 AI 回應
    assistantMsg := &Message{
        ConversationID: conversationID,
        Role:           "assistant",
        Content:        fullResponse,
        CreatedAt:      time.Now(),
    }

    s.msgRepo.Create(ctx, assistantMsg)

    return nil
}

// MessageResponse 訊息回應
type MessageResponse struct {
    Message      *Message
    Usage        llm.Usage
    FinishReason string
}
```

### 3. Token 計數器

```go
package tokenizer

import (
    "github.com/pkoukk/tiktoken-go"
)

// Counter Token 計數器
type Counter struct {
    encoding string
}

// NewCounter 建立計數器
func NewCounter(model string) (*Counter, error) {
    var encoding string

    switch model {
    case "gpt-4", "gpt-3.5-turbo", "gpt-4-32k":
        encoding = "cl100k_base"
    case "gpt-3", "text-davinci-003":
        encoding = "p50k_base"
    default:
        encoding = "cl100k_base"
    }

    return &Counter{encoding: encoding}, nil
}

// Count 計算文字的 Token 數
func (c *Counter) Count(text string) int {
    tkm, err := tiktoken.GetEncoding(c.encoding)
    if err != nil {
        // 降級：粗略估算（1 token ≈ 4 字符）
        return len(text) / 4
    }

    tokens := tkm.Encode(text, nil, nil)
    return len(tokens)
}

// CountMessages 計算訊息列表的 Token 數
func (c *Counter) CountMessages(messages []llm.Message) int {
    totalTokens := 0

    for _, msg := range messages {
        // 每條訊息固定開銷
        totalTokens += 4

        // Role
        totalTokens += c.Count(msg.Role)

        // Content
        totalTokens += c.Count(msg.Content)
    }

    // 回應起始開銷
    totalTokens += 2

    return totalTokens
}
```

### 4. 請求隊列（併發控制）

```go
package queue

import (
    "context"
    "errors"
    "time"

    "golang.org/x/time/rate"
)

// RequestQueue 請求隊列
type RequestQueue struct {
    queue       chan *QueuedRequest
    rateLimiter *rate.Limiter
    workers     int
}

// QueuedRequest 排隊的請求
type QueuedRequest struct {
    Request  interface{}
    Response chan *QueuedResponse
}

// QueuedResponse 排隊的回應
type QueuedResponse struct {
    Response interface{}
    Error    error
}

// New 建立請求隊列
func New(queueSize, workers, requestsPerMinute int) *RequestQueue {
    r := rate.Limit(float64(requestsPerMinute) / 60.0)
    burst := requestsPerMinute / 10

    return &RequestQueue{
        queue:       make(chan *QueuedRequest, queueSize),
        rateLimiter: rate.NewLimiter(r, burst),
        workers:     workers,
    }
}

// Start 啟動工作者
func (q *RequestQueue) Start(handler func(interface{}) (interface{}, error)) {
    for i := 0; i < q.workers; i++ {
        go q.worker(handler)
    }
}

// worker 工作者
func (q *RequestQueue) worker(handler func(interface{}) (interface{}, error)) {
    for req := range q.queue {
        // 等待 Rate Limiter
        ctx := context.Background()
        if err := q.rateLimiter.Wait(ctx); err != nil {
            req.Response <- &QueuedResponse{Error: err}
            continue
        }

        // 處理請求
        resp, err := handler(req.Request)

        // 返回結果
        req.Response <- &QueuedResponse{
            Response: resp,
            Error:    err,
        }
    }
}

// Submit 提交請求
func (q *RequestQueue) Submit(req interface{}) (interface{}, error) {
    queuedReq := &QueuedRequest{
        Request:  req,
        Response: make(chan *QueuedResponse, 1),
    }

    // 加入隊列
    select {
    case q.queue <- queuedReq:
        // 成功加入
    default:
        return nil, errors.New("隊列已滿，請稍後再試")
    }

    // 等待結果（設定超時）
    select {
    case result := <-queuedReq.Response:
        return result.Response, result.Error
    case <-time.After(60 * time.Second):
        return nil, errors.New("請求超時")
    }
}
```

---

## API 文件

### 1. 建立對話

**端點**: `POST /api/v1/conversations`

**請求**:
```json
{
  "model": "gpt-4"
}
```

**回應**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "conversation_id": "550e8400-e29b-41d4-a716-446655440000",
    "model": "gpt-4",
    "created_at": "2025-05-18T10:30:00Z"
  }
}
```

### 2. 發送訊息（非流式）

**端點**: `POST /api/v1/conversations/:id/messages`

**請求**:
```json
{
  "content": "什麼是系統設計？"
}
```

**回應**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "message_id": 123456,
    "role": "assistant",
    "content": "系統設計是構建大型軟體系統的過程...",
    "usage": {
      "prompt_tokens": 18,
      "completion_tokens": 156,
      "total_tokens": 174
    },
    "finish_reason": "stop",
    "created_at": "2025-05-18T10:30:05Z"
  }
}
```

### 3. 發送訊息（流式）

**端點**: `POST /api/v1/conversations/:id/messages/stream`

**請求**:
```json
{
  "content": "寫一首詩"
}
```

**回應**（Server-Sent Events）:
```
data: {"chunk": "在"}

data: {"chunk": "雲"}

data: {"chunk": "端"}

data: {"chunk": "之"}

data: {"chunk": "上"}

...

data: [DONE]
```

### 4. 查詢對話歷史

**端點**: `GET /api/v1/conversations/:id/messages`

**回應**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "messages": [
      {
        "message_id": 123455,
        "role": "user",
        "content": "什麼是系統設計？",
        "created_at": "2025-05-18T10:30:00Z"
      },
      {
        "message_id": 123456,
        "role": "assistant",
        "content": "系統設計是...",
        "tokens": 174,
        "created_at": "2025-05-18T10:30:05Z"
      }
    ],
    "total": 2
  }
}
```

---

## 效能優化

### 1. 回應快取

```go
// ResponseCache 回應快取
type ResponseCache struct {
    redis *redis.Client
    ttl   time.Duration
}

// Get 獲取快取
func (c *ResponseCache) Get(ctx context.Context, prompt string) (string, bool) {
    key := c.hashPrompt(prompt)

    result, err := c.redis.Get(ctx, key).Result()
    if err != nil {
        return "", false
    }

    return result, true
}

// Set 設定快取
func (c *ResponseCache) Set(ctx context.Context, prompt, response string) error {
    key := c.hashPrompt(prompt)
    return c.redis.Set(ctx, key, response, c.ttl).Err()
}

func (c *ResponseCache) hashPrompt(prompt string) string {
    h := sha256.Sum256([]byte(prompt))
    return fmt.Sprintf("response:%x", h)
}
```

**快取效果**:
- 相同問題命中率：~15%
- 延遲降低：從 2-5s 降至 < 100ms
- 成本節省：15% API 呼叫

### 2. Token 優化

| 優化策略 | 節省 Token | 說明 |
|---------|-----------|------|
| 移除多餘空白 | ~5% | 壓縮空白字符 |
| 縮短系統提示 | ~10% | 精簡 System Prompt |
| 滑動窗口（保留最近 10 輪） | ~30% | 限制上下文長度 |
| **總計** | **~40%** | |

**成本對比**（每月 100 萬次請求）:

| 項目 | 優化前 | 優化後 | 節省 |
|-----|--------|--------|------|
| 平均 Token/請求 | 1,000 | 600 | 40% |
| 月 Token 總量 | 10 億 | 6 億 | 4 億 |
| 月成本（GPT-4） | $30,000 | $18,000 | $12,000 |

---

## 監控與告警

### 核心監控指標

```go
// Metrics 監控指標
type Metrics struct {
    // 請求指標
    TotalRequests    prometheus.Counter
    SuccessRequests  prometheus.Counter
    FailedRequests   prometheus.Counter

    // 延遲指標
    FirstTokenLatency prometheus.Histogram  // 首字延遲
    StreamLatency     prometheus.Histogram  // 流式延遲

    // Token 指標
    PromptTokens     prometheus.Counter
    CompletionTokens prometheus.Counter
    TotalTokens      prometheus.Counter

    // 成本指標
    TotalCost        prometheus.Counter

    // 快取指標
    CacheHits        prometheus.Counter
    CacheMisses      prometheus.Counter

    // 隊列指標
    QueueLength      prometheus.Gauge
    QueueLatency     prometheus.Histogram
}
```

### Grafana Dashboard

```json
{
  "dashboard": {
    "title": "ChatGPT-like System Dashboard",
    "panels": [
      {
        "title": "QPS（每秒請求數）",
        "targets": [
          {"expr": "rate(chat_requests_total[1m])"}
        ]
      },
      {
        "title": "首字延遲（P50/P95/P99）",
        "targets": [
          {"expr": "histogram_quantile(0.50, rate(first_token_latency_seconds_bucket[5m]))"},
          {"expr": "histogram_quantile(0.95, rate(first_token_latency_seconds_bucket[5m]))"},
          {"expr": "histogram_quantile(0.99, rate(first_token_latency_seconds_bucket[5m]))"}
        ]
      },
      {
        "title": "Token 使用量（小時）",
        "targets": [
          {"expr": "increase(total_tokens[1h])"}
        ]
      },
      {
        "title": "成本（美元/小時）",
        "targets": [
          {"expr": "increase(total_cost_usd[1h])"}
        ]
      },
      {
        "title": "快取命中率",
        "targets": [
          {"expr": "rate(cache_hits_total[5m]) / (rate(cache_hits_total[5m]) + rate(cache_misses_total[5m]))"}
        ]
      }
    ]
  }
}
```

---

## 部署架構

### Kubernetes 部署

```yaml
# chat-api-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: chat-api
spec:
  replicas: 6
  selector:
    matchLabels:
      app: chat-api
  template:
    metadata:
      labels:
        app: chat-api
    spec:
      containers:
      - name: chat-api
        image: chat-api:v1.0.0
        ports:
        - containerPort: 8080

        env:
        - name: OPENAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: llm-secrets
              key: openai_api_key

        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: db-secrets
              key: url

        - name: REDIS_URL
          value: "redis://redis-cluster:6379"

        resources:
          requests:
            cpu: "500m"
            memory: "1Gi"
          limits:
            cpu: "1000m"
            memory: "2Gi"

        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10

        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5

---
apiVersion: v1
kind: Service
metadata:
  name: chat-api
spec:
  selector:
    app: chat-api
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
  type: LoadBalancer

---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: chat-api-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: chat-api
  minReplicas: 6
  maxReplicas: 30
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

---

## 成本估算

### 台灣地區成本（中型平台）

**假設**:
- 日活躍用戶：50,000 人
- 每人每天 20 次對話
- 平均 600 tokens/對話

#### 1. 基礎設施

| 資源 | 規格 | 數量 | 單價（月） | 小計 |
|-----|------|------|-----------|------|
| API 服務器 | 4C8G | 6 台 | NT$ 3,000 | NT$ 18,000 |
| PostgreSQL | 8C16G | 1 主 + 1 從 | NT$ 15,000 | NT$ 30,000 |
| Redis Cluster | 16GB | 3 節點 | NT$ 5,000 | NT$ 15,000 |
| 負載平衡器 | - | 1 | NT$ 2,000 | NT$ 2,000 |
| **小計** | | | | **NT$ 65,000** |

#### 2. LLM API 成本

**月對話量**：50,000 人 × 20 次/天 × 30 天 = 30,000,000 次

**模型分布**（假設）:
- GPT-4: 20%（6,000,000 次）
- GPT-3.5-Turbo: 80%（24,000,000 次）

**成本計算**:

| 模型 | 對話數 | Tokens/對話 | 總 Tokens | 成本/1M tokens | 月成本 |
|------|--------|------------|-----------|---------------|--------|
| GPT-4 | 6M | 600 | 3.6B | $30 | $108,000 |
| GPT-3.5 | 24M | 600 | 14.4B | $1 | $14,400 |
| **總計** | **30M** | | **18B** | | **$122,400** |

**換算台幣**（1 USD = 31 TWD）：NT$ 3,794,400

#### 3. 總成本

| 類別 | 月成本 | 年成本 |
|-----|--------|--------|
| 基礎設施 | NT$ 65,000 | NT$ 780,000 |
| LLM API | NT$ 3,794,400 | NT$ 45,532,800 |
| 頻寬與 CDN | NT$ 5,000 | NT$ 60,000 |
| 監控與備份 | NT$ 3,000 | NT$ 36,000 |
| **總計** | **NT$ 3,867,400** | **NT$ 46,408,800** |

**營收模式**（訂閱制）:
- 免費版：每月 100,000 tokens
- 專業版：NT$ 300/月（無限 tokens）
- 假設 10% 轉換率：5,000 付費用戶 × NT$ 300 = NT$ 1,500,000/月

**結論**：需要更高轉換率或調整定價策略。

---

## 延伸閱讀

- [OpenAI API Documentation](https://platform.openai.com/docs/api-reference)
- [Anthropic Claude API](https://docs.anthropic.com/)
- [tiktoken - Token Counting](https://github.com/openai/tiktoken)
- [Server-Sent Events Specification](https://html.spec.whatwg.org/multipage/server-sent-events.html)
- [Prompt Engineering Guide](https://www.promptingguide.ai/)

---

**版本**: v1.0.0
**最後更新**: 2025-05-18
**維護者**: AI Team
