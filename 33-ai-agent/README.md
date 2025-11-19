# AI Agent 平台

## 系統概述

AI Agent 平台是一個能夠自主執行任務的智慧系統，與傳統 ChatGPT 的主要差異在於：

- **ChatGPT**: 對話式系統，只能回答問題
- **AI Agent**: 行動式系統，能夠使用工具、執行動作、完成任務

### 核心能力

1. **工具調用 (Tool Calling)**: 能夠呼叫外部 API、資料庫、第三方服務
2. **推理與行動 (ReAct)**: 結合思考與執行的迭代過程
3. **思維鏈 (Chain-of-Thought)**: 複雜問題的分步驟推理
4. **狀態管理**: 保存執行進度，支援中斷與恢復
5. **多 Agent 協作**: 多個專業 Agent 協同完成複雜任務

### 應用場景

- **客服自動化**: 自動查詢訂單、處理退款、更新資訊
- **資料分析**: 自動查詢資料庫、生成報表、視覺化
- **程式碼助手**: 搜尋文件、執行測試、修復 Bug
- **研究助理**: 搜尋論文、總結資料、生成報告

## 功能需求

### 1. 核心功能

#### 1.1 Agent 執行引擎
- 支援多種 Agent 模式：ReAct、CoT、Planning
- 工具註冊與管理
- 執行步驟追蹤
- 自動重試與錯誤處理

#### 1.2 工具系統
- 工具定義與註冊
- 參數驗證
- 執行超時控制
- 結果格式化

#### 1.3 狀態管理
- 執行狀態持久化
- Checkpoint 機制
- 中斷恢復
- 狀態查詢

#### 1.4 多 Agent 協作
- Agent 路由與編排
- Agent 間通訊
- 任務分解與合併
- 衝突解決

### 2. 非功能需求

| 需求 | 指標 | 說明 |
|------|------|------|
| **可靠性** | 99.9% | Agent 執行成功率 |
| **效能** | < 5s | 單步驟執行時間 |
| **併發** | 10,000 | 同時執行的 Agent 數量 |
| **恢復** | < 1min | 故障恢復時間 |
| **可觀測性** | 100% | 所有執行步驟可追蹤 |

## 技術架構

### 系統架構圖

```
┌─────────────────────────────────────────────────────────────────┐
│                          Client Layer                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │   Web    │  │  Mobile  │  │   API    │  │   CLI    │       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                         API Gateway                              │
│              (認證、限流、路由、監控)                              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Agent Service Layer                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │   ReAct      │  │     CoT      │  │  Planning    │         │
│  │   Agent      │  │    Agent     │  │    Agent     │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
│                                                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │ Orchestrator │  │    State     │  │    Tool      │         │
│  │   Service    │  │   Manager    │  │   Registry   │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Tool Layer                                │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │  Search  │  │ Database │  │   API    │  │  Python  │       │
│  │   Tool   │  │   Tool   │  │   Tool   │  │   Tool   │       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Storage Layer                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │  PostgreSQL  │  │     Redis    │  │      S3      │         │
│  │  (狀態/歷史) │  │   (快取)     │  │   (檔案)     │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     External Services                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │   OpenAI     │  │   Anthropic  │  │   External   │         │
│  │     API      │  │     API      │  │     APIs     │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
└─────────────────────────────────────────────────────────────────┘
```

### 技術棧

| 層級 | 技術選型 | 原因 |
|------|----------|------|
| **API** | Go + Gin | 高效能、併發支援 |
| **Agent 引擎** | Go | 狀態機實作、goroutine 並行 |
| **LLM** | OpenAI / Anthropic | Function Calling 支援 |
| **資料庫** | PostgreSQL | JSONB 存儲複雜狀態 |
| **快取** | Redis | 執行狀態快取、分散式鎖 |
| **訊息佇列** | Kafka | Agent 間通訊、事件溯源 |
| **監控** | Prometheus + Grafana | 指標收集與視覺化 |
| **日誌** | ELK Stack | 執行步驟追蹤 |

## 資料庫設計

### 1. Agent 執行表 (agent_executions)

```sql
CREATE TABLE agent_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    agent_type VARCHAR(50) NOT NULL,  -- 'react', 'cot', 'planning'
    status VARCHAR(20) NOT NULL,      -- 'running', 'completed', 'failed', 'paused'
    input JSONB NOT NULL,             -- 使用者輸入
    output JSONB,                     -- 最終輸出
    steps JSONB[] NOT NULL DEFAULT '{}',  -- 執行步驟歷史
    current_step INTEGER DEFAULT 0,
    max_steps INTEGER DEFAULT 10,
    metadata JSONB,                   -- 額外資訊
    error_message TEXT,
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_executions_user_id ON agent_executions(user_id);
CREATE INDEX idx_executions_status ON agent_executions(status);
CREATE INDEX idx_executions_started_at ON agent_executions(started_at);
```

### 2. 執行步驟表 (execution_steps)

```sql
CREATE TABLE execution_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL REFERENCES agent_executions(id) ON DELETE CASCADE,
    step_number INTEGER NOT NULL,
    step_type VARCHAR(50) NOT NULL,   -- 'thought', 'action', 'observation', 'answer'
    content TEXT NOT NULL,
    tool_name VARCHAR(100),
    tool_input JSONB,
    tool_output JSONB,
    llm_tokens INTEGER,               -- 使用的 token 數
    duration_ms INTEGER,              -- 執行時間（毫秒）
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    UNIQUE(execution_id, step_number)
);

CREATE INDEX idx_steps_execution_id ON execution_steps(execution_id);
CREATE INDEX idx_steps_created_at ON execution_steps(created_at);
```

### 3. 工具定義表 (tools)

```sql
CREATE TABLE tools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT NOT NULL,
    category VARCHAR(50),             -- 'search', 'database', 'api', 'compute'
    parameters JSONB NOT NULL,        -- JSON Schema 格式
    endpoint VARCHAR(500),            -- API endpoint 或執行路徑
    auth_required BOOLEAN DEFAULT false,
    timeout_ms INTEGER DEFAULT 30000,
    rate_limit INTEGER,               -- 每分鐘請求限制
    is_active BOOLEAN DEFAULT true,
    metadata JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tools_category ON tools(category);
CREATE INDEX idx_tools_is_active ON tools(is_active);
```

### 4. 工具執行日誌表 (tool_executions)

```sql
CREATE TABLE tool_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL REFERENCES agent_executions(id),
    step_id UUID NOT NULL REFERENCES execution_steps(id),
    tool_id UUID NOT NULL REFERENCES tools(id),
    input JSONB NOT NULL,
    output JSONB,
    status VARCHAR(20) NOT NULL,      -- 'success', 'failed', 'timeout'
    error_message TEXT,
    duration_ms INTEGER,
    retry_count INTEGER DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tool_executions_tool_id ON tool_executions(tool_id);
CREATE INDEX idx_tool_executions_status ON tool_executions(status);
CREATE INDEX idx_tool_executions_created_at ON tool_executions(created_at);
```

### 5. Agent 定義表 (agents)

```sql
CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    type VARCHAR(50) NOT NULL,        -- 'specialist', 'orchestrator'
    system_prompt TEXT NOT NULL,
    available_tools UUID[] NOT NULL,  -- 可用工具 ID 陣列
    config JSONB,                     -- Agent 特定設定
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agents_type ON agents(type);
CREATE INDEX idx_agents_is_active ON agents(is_active);
```

### 6. Multi-Agent 協作表 (agent_collaborations)

```sql
CREATE TABLE agent_collaborations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_execution_id UUID NOT NULL REFERENCES agent_executions(id),
    orchestrator_agent_id UUID NOT NULL REFERENCES agents(id),
    specialist_agent_id UUID NOT NULL REFERENCES agents(id),
    task TEXT NOT NULL,
    result JSONB,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP
);

CREATE INDEX idx_collaborations_parent ON agent_collaborations(parent_execution_id);
CREATE INDEX idx_collaborations_status ON agent_collaborations(status);
```

### 7. Checkpoints 表 (execution_checkpoints)

```sql
CREATE TABLE execution_checkpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL REFERENCES agent_executions(id) ON DELETE CASCADE,
    checkpoint_number INTEGER NOT NULL,
    state JSONB NOT NULL,             -- 完整執行狀態快照
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    UNIQUE(execution_id, checkpoint_number)
);

CREATE INDEX idx_checkpoints_execution_id ON execution_checkpoints(execution_id);
```

## 核心功能實作

### 1. ReAct Agent 引擎

```go
package agent

import (
    "context"
    "fmt"
    "strings"
    "time"
)

type ReActAgent struct {
    ID          string
    LLM         LLMClient
    Tools       map[string]Tool
    Memory      []Step
    MaxSteps    int
    SystemPrompt string
}

type Step struct {
    StepNumber  int       `json:"step_number"`
    StepType    string    `json:"step_type"`    // "thought", "action", "observation", "answer"
    Content     string    `json:"content"`
    ToolName    string    `json:"tool_name,omitempty"`
    ToolInput   string    `json:"tool_input,omitempty"`
    ToolOutput  string    `json:"tool_output,omitempty"`
    Timestamp   time.Time `json:"timestamp"`
    Tokens      int       `json:"tokens"`
    DurationMS  int64     `json:"duration_ms"`
}

func (a *ReActAgent) Run(ctx context.Context, input string) (*ExecutionResult, error) {
    executionID := generateUUID()

    // 初始化執行記錄
    execution := &Execution{
        ID:        executionID,
        AgentType: "react",
        Status:    "running",
        Input:     input,
        StartedAt: time.Now(),
    }

    if err := a.saveExecution(ctx, execution); err != nil {
        return nil, err
    }

    // ReAct 迴圈
    for step := 1; step <= a.MaxSteps; step++ {
        // 1. Thought - 讓 LLM 思考下一步
        thought, tokens, err := a.think(ctx, input, execution)
        if err != nil {
            return a.handleError(ctx, execution, err)
        }

        a.addStep(ctx, execution, Step{
            StepNumber: step,
            StepType:   "thought",
            Content:    thought,
            Timestamp:  time.Now(),
            Tokens:     tokens,
        })

        // 2. 檢查是否已經得到最終答案
        if strings.Contains(thought, "Final Answer:") {
            answer := extractAnswer(thought)
            a.addStep(ctx, execution, Step{
                StepNumber: step,
                StepType:   "answer",
                Content:    answer,
                Timestamp:  time.Now(),
            })

            execution.Status = "completed"
            execution.Output = answer
            execution.CompletedAt = time.Now()
            a.saveExecution(ctx, execution)

            return &ExecutionResult{
                ExecutionID: executionID,
                Answer:      answer,
                Steps:       a.Memory,
            }, nil
        }

        // 3. Action - 決定執行哪個工具
        action, toolName, toolInput, err := a.decideAction(ctx, thought)
        if err != nil {
            return a.handleError(ctx, execution, err)
        }

        a.addStep(ctx, execution, Step{
            StepNumber: step,
            StepType:   "action",
            Content:    action,
            ToolName:   toolName,
            ToolInput:  toolInput,
            Timestamp:  time.Now(),
        })

        // 4. 執行工具
        start := time.Now()
        toolOutput, err := a.executeTool(ctx, toolName, toolInput)
        duration := time.Since(start)

        if err != nil {
            // 工具執行失敗，記錄但繼續
            toolOutput = fmt.Sprintf("Tool execution failed: %v", err)
        }

        // 5. Observation - 記錄工具輸出
        a.addStep(ctx, execution, Step{
            StepNumber: step,
            StepType:   "observation",
            Content:    toolOutput,
            ToolName:   toolName,
            ToolOutput: toolOutput,
            Timestamp:  time.Now(),
            DurationMS: duration.Milliseconds(),
        })

        // 6. 建立 checkpoint（每 3 步）
        if step%3 == 0 {
            a.createCheckpoint(ctx, execution, step)
        }
    }

    // 達到最大步驟數，返回失敗
    execution.Status = "failed"
    execution.ErrorMessage = "Reached maximum steps without finding answer"
    execution.CompletedAt = time.Now()
    a.saveExecution(ctx, execution)

    return nil, fmt.Errorf("agent reached max steps (%d) without answer", a.MaxSteps)
}

func (a *ReActAgent) think(ctx context.Context, originalInput string, execution *Execution) (string, int, error) {
    // 構建 prompt，包含歷史步驟
    prompt := a.buildThinkPrompt(originalInput, a.Memory)

    resp, err := a.LLM.Chat(ctx, &ChatRequest{
        Model: "gpt-4",
        Messages: []Message{
            {Role: "system", Content: a.SystemPrompt},
            {Role: "user", Content: prompt},
        },
        Temperature: 0.7,
    })

    if err != nil {
        return "", 0, err
    }

    return resp.Choices[0].Message.Content, resp.Usage.TotalTokens, nil
}

func (a *ReActAgent) buildThinkPrompt(input string, memory []Step) string {
    var sb strings.Builder

    sb.WriteString(fmt.Sprintf("Question: %s\n\n", input))
    sb.WriteString("You have access to the following tools:\n")
    for name, tool := range a.Tools {
        sb.WriteString(fmt.Sprintf("- %s: %s\n", name, tool.Description))
    }

    sb.WriteString("\nPrevious steps:\n")
    for _, step := range memory {
        sb.WriteString(fmt.Sprintf("%s: %s\n", step.StepType, step.Content))
        if step.ToolOutput != "" {
            sb.WriteString(fmt.Sprintf("Tool output: %s\n", step.ToolOutput))
        }
    }

    sb.WriteString("\nWhat should you do next? Think step by step.\n")
    sb.WriteString("If you have the final answer, respond with 'Final Answer: [your answer]'\n")
    sb.WriteString("Otherwise, explain your thought process.\n")

    return sb.String()
}

func (a *ReActAgent) decideAction(ctx context.Context, thought string) (string, string, string, error) {
    // 使用 Function Calling 讓 LLM 決定使用哪個工具
    functions := make([]FunctionDefinition, 0, len(a.Tools))
    for name, tool := range a.Tools {
        functions = append(functions, FunctionDefinition{
            Name:        name,
            Description: tool.Description,
            Parameters:  tool.Parameters,
        })
    }

    resp, err := a.LLM.Chat(ctx, &ChatRequest{
        Model: "gpt-4",
        Messages: []Message{
            {Role: "user", Content: thought},
        },
        Functions: functions,
        FunctionCall: "auto",
    })

    if err != nil {
        return "", "", "", err
    }

    if resp.Choices[0].Message.FunctionCall == nil {
        return "", "", "", fmt.Errorf("LLM did not call any function")
    }

    fc := resp.Choices[0].Message.FunctionCall
    return thought, fc.Name, fc.Arguments, nil
}

func (a *ReActAgent) executeTool(ctx context.Context, toolName, toolInput string) (string, error) {
    tool, exists := a.Tools[toolName]
    if !exists {
        return "", fmt.Errorf("tool %s not found", toolName)
    }

    // 工具執行超時控制
    toolCtx, cancel := context.WithTimeout(ctx, time.Duration(tool.TimeoutMS)*time.Millisecond)
    defer cancel()

    // 執行工具
    output, err := tool.Execute(toolCtx, toolInput)
    if err != nil {
        return "", err
    }

    return output, nil
}

func (a *ReActAgent) addStep(ctx context.Context, execution *Execution, step Step) {
    a.Memory = append(a.Memory, step)
    execution.Steps = append(execution.Steps, step)

    // 非同步儲存步驟到資料庫
    go a.saveStep(ctx, execution.ID, step)
}

func (a *ReActAgent) createCheckpoint(ctx context.Context, execution *Execution, stepNumber int) error {
    checkpoint := &Checkpoint{
        ExecutionID:      execution.ID,
        CheckpointNumber: stepNumber,
        State: CheckpointState{
            Memory:      a.Memory,
            CurrentStep: stepNumber,
            UpdatedAt:   time.Now(),
        },
    }

    return a.saveCheckpoint(ctx, checkpoint)
}
```

### 2. Function Calling 工具系統

```go
package tool

import (
    "context"
    "encoding/json"
    "fmt"
)

type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]interface{}
    Execute(ctx context.Context, input string) (string, error)
}

type ToolRegistry struct {
    tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
    return &ToolRegistry{
        tools: make(map[string]Tool),
    }
}

func (r *ToolRegistry) Register(tool Tool) {
    r.tools[tool.Name()] = tool
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
    tool, exists := r.tools[name]
    return tool, exists
}

func (r *ToolRegistry) List() []Tool {
    tools := make([]Tool, 0, len(r.tools))
    for _, tool := range r.tools {
        tools = append(tools, tool)
    }
    return tools
}

// 搜尋工具實作
type SearchTool struct {
    apiKey string
}

func (t *SearchTool) Name() string {
    return "search"
}

func (t *SearchTool) Description() string {
    return "搜尋網路資訊，輸入查詢關鍵字，返回搜尋結果"
}

func (t *SearchTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "query": map[string]interface{}{
                "type":        "string",
                "description": "搜尋關鍵字",
            },
            "num_results": map[string]interface{}{
                "type":        "integer",
                "description": "返回結果數量",
                "default":     5,
            },
        },
        "required": []string{"query"},
    }
}

func (t *SearchTool) Execute(ctx context.Context, input string) (string, error) {
    var params struct {
        Query      string `json:"query"`
        NumResults int    `json:"num_results"`
    }

    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    if params.NumResults == 0 {
        params.NumResults = 5
    }

    // 呼叫搜尋 API（這裡使用假資料示意）
    results := []string{
        fmt.Sprintf("Result 1 for '%s': ...", params.Query),
        fmt.Sprintf("Result 2 for '%s': ...", params.Query),
        fmt.Sprintf("Result 3 for '%s': ...", params.Query),
    }

    output, _ := json.Marshal(map[string]interface{}{
        "query":   params.Query,
        "results": results[:min(len(results), params.NumResults)],
    })

    return string(output), nil
}

// 資料庫查詢工具
type DatabaseTool struct {
    db *sql.DB
}

func (t *DatabaseTool) Name() string {
    return "query_database"
}

func (t *DatabaseTool) Description() string {
    return "執行 SQL 查詢，返回結果。僅支援 SELECT 語句"
}

func (t *DatabaseTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "sql": map[string]interface{}{
                "type":        "string",
                "description": "SQL 查詢語句（僅限 SELECT）",
            },
        },
        "required": []string{"sql"},
    }
}

func (t *DatabaseTool) Execute(ctx context.Context, input string) (string, error) {
    var params struct {
        SQL string `json:"sql"`
    }

    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    // 安全檢查：只允許 SELECT
    if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(params.SQL)), "SELECT") {
        return "", fmt.Errorf("only SELECT queries are allowed")
    }

    // 執行查詢
    rows, err := t.db.QueryContext(ctx, params.SQL)
    if err != nil {
        return "", fmt.Errorf("query failed: %w", err)
    }
    defer rows.Close()

    // 解析結果
    columns, _ := rows.Columns()
    results := make([]map[string]interface{}, 0)

    for rows.Next() {
        values := make([]interface{}, len(columns))
        valuePtrs := make([]interface{}, len(columns))
        for i := range values {
            valuePtrs[i] = &values[i]
        }

        rows.Scan(valuePtrs...)

        row := make(map[string]interface{})
        for i, col := range columns {
            row[col] = values[i]
        }
        results = append(results, row)
    }

    output, _ := json.Marshal(map[string]interface{}{
        "columns": columns,
        "rows":    results,
        "count":   len(results),
    })

    return string(output), nil
}

// Python 程式碼執行工具
type PythonTool struct {
    sandboxURL string
}

func (t *PythonTool) Name() string {
    return "execute_python"
}

func (t *PythonTool) Description() string {
    return "在沙箱環境中執行 Python 程式碼，返回執行結果"
}

func (t *PythonTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "code": map[string]interface{}{
                "type":        "string",
                "description": "要執行的 Python 程式碼",
            },
        },
        "required": []string{"code"},
    }
}

func (t *PythonTool) Execute(ctx context.Context, input string) (string, error) {
    var params struct {
        Code string `json:"code"`
    }

    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    // 發送到沙箱執行
    resp, err := http.Post(
        t.sandboxURL+"/execute",
        "application/json",
        bytes.NewBufferString(fmt.Sprintf(`{"code": %q}`, params.Code)),
    )
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result struct {
        Stdout string `json:"stdout"`
        Stderr string `json:"stderr"`
        Error  string `json:"error"`
    }

    json.NewDecoder(resp.Body).Decode(&result)

    output, _ := json.Marshal(result)
    return string(output), nil
}
```

### 3. 狀態管理與恢復

```go
package state

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
)

type StateManager struct {
    db    *sql.DB
    cache *redis.Client
}

func NewStateManager(db *sql.DB, cache *redis.Client) *StateManager {
    return &StateManager{
        db:    db,
        cache: cache,
    }
}

// 保存執行狀態
func (sm *StateManager) SaveExecution(ctx context.Context, execution *Execution) error {
    query := `
        INSERT INTO agent_executions (id, user_id, agent_type, status, input, output, steps, current_step, max_steps, metadata, error_message, started_at, completed_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
        ON CONFLICT (id) DO UPDATE SET
            status = EXCLUDED.status,
            output = EXCLUDED.output,
            steps = EXCLUDED.steps,
            current_step = EXCLUDED.current_step,
            error_message = EXCLUDED.error_message,
            completed_at = EXCLUDED.completed_at,
            updated_at = NOW()
    `

    stepsJSON, _ := json.Marshal(execution.Steps)
    metadataJSON, _ := json.Marshal(execution.Metadata)
    outputJSON, _ := json.Marshal(execution.Output)

    _, err := sm.db.ExecContext(ctx, query,
        execution.ID,
        execution.UserID,
        execution.AgentType,
        execution.Status,
        execution.Input,
        outputJSON,
        stepsJSON,
        execution.CurrentStep,
        execution.MaxSteps,
        metadataJSON,
        execution.ErrorMessage,
        execution.StartedAt,
        execution.CompletedAt,
    )

    if err != nil {
        return err
    }

    // 同時快取到 Redis（TTL 1小時）
    cacheKey := fmt.Sprintf("execution:%s", execution.ID)
    data, _ := json.Marshal(execution)
    sm.cache.Set(ctx, cacheKey, data, time.Hour)

    return nil
}

// 載入執行狀態
func (sm *StateManager) LoadExecution(ctx context.Context, executionID string) (*Execution, error) {
    // 先檢查快取
    cacheKey := fmt.Sprintf("execution:%s", executionID)
    cached, err := sm.cache.Get(ctx, cacheKey).Result()
    if err == nil {
        var execution Execution
        json.Unmarshal([]byte(cached), &execution)
        return &execution, nil
    }

    // 從資料庫載入
    query := `
        SELECT id, user_id, agent_type, status, input, output, steps, current_step, max_steps, metadata, error_message, started_at, completed_at, created_at
        FROM agent_executions
        WHERE id = $1
    `

    var execution Execution
    var stepsJSON, metadataJSON, outputJSON []byte

    err = sm.db.QueryRowContext(ctx, query, executionID).Scan(
        &execution.ID,
        &execution.UserID,
        &execution.AgentType,
        &execution.Status,
        &execution.Input,
        &outputJSON,
        &stepsJSON,
        &execution.CurrentStep,
        &execution.MaxSteps,
        &metadataJSON,
        &execution.ErrorMessage,
        &execution.StartedAt,
        &execution.CompletedAt,
        &execution.CreatedAt,
    )

    if err != nil {
        return nil, err
    }

    json.Unmarshal(stepsJSON, &execution.Steps)
    json.Unmarshal(metadataJSON, &execution.Metadata)
    json.Unmarshal(outputJSON, &execution.Output)

    // 回寫快取
    data, _ := json.Marshal(execution)
    sm.cache.Set(ctx, cacheKey, data, time.Hour)

    return &execution, nil
}

// 從 Checkpoint 恢復
func (sm *StateManager) RestoreFromCheckpoint(ctx context.Context, executionID string, checkpointNumber int) (*Execution, error) {
    query := `
        SELECT state
        FROM execution_checkpoints
        WHERE execution_id = $1 AND checkpoint_number = $2
    `

    var stateJSON []byte
    err := sm.db.QueryRowContext(ctx, query, executionID, checkpointNumber).Scan(&stateJSON)
    if err != nil {
        return nil, err
    }

    var state CheckpointState
    json.Unmarshal(stateJSON, &state)

    // 載入原始執行
    execution, err := sm.LoadExecution(ctx, executionID)
    if err != nil {
        return nil, err
    }

    // 恢復狀態
    execution.Steps = state.Memory
    execution.CurrentStep = state.CurrentStep
    execution.Status = "running"
    execution.ErrorMessage = ""

    return execution, nil
}

// 暫停執行
func (sm *StateManager) PauseExecution(ctx context.Context, executionID string) error {
    query := `UPDATE agent_executions SET status = 'paused', updated_at = NOW() WHERE id = $1`
    _, err := sm.db.ExecContext(ctx, query, executionID)
    return err
}

// 恢復執行
func (sm *StateManager) ResumeExecution(ctx context.Context, executionID string) error {
    query := `UPDATE agent_executions SET status = 'running', updated_at = NOW() WHERE id = $1`
    _, err := sm.db.ExecContext(ctx, query, executionID)
    return err
}
```

### 4. Multi-Agent Orchestrator

```go
package orchestrator

import (
    "context"
    "encoding/json"
    "fmt"
)

type MultiAgentOrchestrator struct {
    agents       map[string]*Agent
    routingAgent *Agent
    stateManager *StateManager
}

type Agent struct {
    ID           string
    Name         string
    Type         string
    SystemPrompt string
    Tools        []Tool
    LLM          LLMClient
}

func NewOrchestrator(agents map[string]*Agent, routingAgent *Agent, sm *StateManager) *MultiAgentOrchestrator {
    return &MultiAgentOrchestrator{
        agents:       agents,
        routingAgent: routingAgent,
        stateManager: sm,
    }
}

func (o *MultiAgentOrchestrator) Execute(ctx context.Context, userID, input string) (*OrchestratorResult, error) {
    // 1. 使用 Routing Agent 決定任務分配
    route, err := o.routeTask(ctx, input)
    if err != nil {
        return nil, err
    }

    // 2. 根據路由策略執行
    switch route.Strategy {
    case "single":
        return o.executeSingle(ctx, userID, input, route.AgentName)
    case "sequential":
        return o.executeSequential(ctx, userID, input, route.AgentNames)
    case "parallel":
        return o.executeParallel(ctx, userID, input, route.AgentNames)
    default:
        return nil, fmt.Errorf("unknown strategy: %s", route.Strategy)
    }
}

// 單一 Agent 執行
func (o *MultiAgentOrchestrator) executeSingle(ctx context.Context, userID, input, agentName string) (*OrchestratorResult, error) {
    agent, exists := o.agents[agentName]
    if !exists {
        return nil, fmt.Errorf("agent %s not found", agentName)
    }

    reactAgent := &ReActAgent{
        ID:           generateUUID(),
        LLM:          agent.LLM,
        Tools:        mapTools(agent.Tools),
        MaxSteps:     10,
        SystemPrompt: agent.SystemPrompt,
    }

    result, err := reactAgent.Run(ctx, input)
    if err != nil {
        return nil, err
    }

    return &OrchestratorResult{
        Strategy: "single",
        Results: map[string]*ExecutionResult{
            agentName: result,
        },
        FinalAnswer: result.Answer,
    }, nil
}

// 循序執行多個 Agents
func (o *MultiAgentOrchestrator) executeSequential(ctx context.Context, userID, input string, agentNames []string) (*OrchestratorResult, error) {
    results := make(map[string]*ExecutionResult)
    currentInput := input

    for _, agentName := range agentNames {
        agent, exists := o.agents[agentName]
        if !exists {
            return nil, fmt.Errorf("agent %s not found", agentName)
        }

        reactAgent := &ReActAgent{
            ID:           generateUUID(),
            LLM:          agent.LLM,
            Tools:        mapTools(agent.Tools),
            MaxSteps:     10,
            SystemPrompt: agent.SystemPrompt,
        }

        result, err := reactAgent.Run(ctx, currentInput)
        if err != nil {
            return nil, fmt.Errorf("agent %s failed: %w", agentName, err)
        }

        results[agentName] = result

        // 下一個 Agent 的輸入是前一個的輸出
        currentInput = result.Answer
    }

    return &OrchestratorResult{
        Strategy:    "sequential",
        Results:     results,
        FinalAnswer: currentInput,
    }, nil
}

// 並行執行多個 Agents
func (o *MultiAgentOrchestrator) executeParallel(ctx context.Context, userID, input string, agentNames []string) (*OrchestratorResult, error) {
    results := make(map[string]*ExecutionResult)
    errors := make(chan error, len(agentNames))
    resultChan := make(chan struct {
        name   string
        result *ExecutionResult
    }, len(agentNames))

    // 並行執行
    for _, agentName := range agentNames {
        go func(name string) {
            agent, exists := o.agents[name]
            if !exists {
                errors <- fmt.Errorf("agent %s not found", name)
                return
            }

            reactAgent := &ReActAgent{
                ID:           generateUUID(),
                LLM:          agent.LLM,
                Tools:        mapTools(agent.Tools),
                MaxSteps:     10,
                SystemPrompt: agent.SystemPrompt,
            }

            result, err := reactAgent.Run(ctx, input)
            if err != nil {
                errors <- err
                return
            }

            resultChan <- struct {
                name   string
                result *ExecutionResult
            }{name, result}
        }(agentName)
    }

    // 收集結果
    for i := 0; i < len(agentNames); i++ {
        select {
        case err := <-errors:
            return nil, err
        case r := <-resultChan:
            results[r.name] = r.result
        case <-ctx.Done():
            return nil, ctx.Err()
        }
    }

    // 合併結果
    finalAnswer, err := o.mergeResults(ctx, results)
    if err != nil {
        return nil, err
    }

    return &OrchestratorResult{
        Strategy:    "parallel",
        Results:     results,
        FinalAnswer: finalAnswer,
    }, nil
}

// 路由決策
func (o *MultiAgentOrchestrator) routeTask(ctx context.Context, input string) (*RouteDecision, error) {
    prompt := fmt.Sprintf(`
你是一個任務路由器，需要決定如何分配任務給不同的專業 Agent。

可用的 Agents：
%s

使用者問題：%s

請決定：
1. 使用哪個/哪些 Agent
2. 執行策略：single（單一）、sequential（循序）、parallel（並行）

以 JSON 格式回答：
{
    "strategy": "single|sequential|parallel",
    "agent_name": "agent名稱（single模式）",
    "agent_names": ["agent1", "agent2"]（sequential/parallel模式）,
    "reasoning": "決策理由"
}
`, o.describeAgents(), input)

    resp, err := o.routingAgent.LLM.Chat(ctx, &ChatRequest{
        Model: "gpt-4",
        Messages: []Message{
            {Role: "system", Content: o.routingAgent.SystemPrompt},
            {Role: "user", Content: prompt},
        },
        Temperature: 0.3,
    })

    if err != nil {
        return nil, err
    }

    var decision RouteDecision
    json.Unmarshal([]byte(resp.Choices[0].Message.Content), &decision)

    return &decision, nil
}

func (o *MultiAgentOrchestrator) mergeResults(ctx context.Context, results map[string]*ExecutionResult) (string, error) {
    // 使用 LLM 合併多個 Agent 的結果
    prompt := "請綜合以下各個專業 Agent 的分析結果，給出最終答案：\n\n"

    for agentName, result := range results {
        prompt += fmt.Sprintf("【%s】：%s\n\n", agentName, result.Answer)
    }

    resp, err := o.routingAgent.LLM.Chat(ctx, &ChatRequest{
        Model: "gpt-4",
        Messages: []Message{
            {Role: "user", Content: prompt},
        },
    })

    if err != nil {
        return "", err
    }

    return resp.Choices[0].Message.Content, nil
}
```

## API 文件

### 1. 建立 Agent 執行

```http
POST /api/v1/agents/execute
Content-Type: application/json
Authorization: Bearer <token>

{
    "agent_type": "react",
    "input": "幫我查詢最近一週的銷售報表，並分析趨勢",
    "config": {
        "max_steps": 15,
        "tools": ["search", "query_database", "execute_python"]
    }
}

Response 200 OK:
{
    "execution_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "running",
    "created_at": "2025-01-15T10:00:00Z"
}
```

### 2. 查詢執行狀態

```http
GET /api/v1/agents/executions/{execution_id}
Authorization: Bearer <token>

Response 200 OK:
{
    "execution_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "running",
    "current_step": 5,
    "max_steps": 15,
    "steps": [
        {
            "step_number": 1,
            "step_type": "thought",
            "content": "我需要查詢資料庫獲取最近一週的銷售資料",
            "timestamp": "2025-01-15T10:00:01Z"
        },
        {
            "step_number": 2,
            "step_type": "action",
            "tool_name": "query_database",
            "tool_input": "{\"sql\": \"SELECT * FROM sales WHERE created_at >= NOW() - INTERVAL '7 days'\"}",
            "timestamp": "2025-01-15T10:00:02Z"
        },
        {
            "step_number": 3,
            "step_type": "observation",
            "content": "查詢到 1,234 筆銷售記錄",
            "tool_output": "{\"rows\": [...], \"count\": 1234}",
            "duration_ms": 250,
            "timestamp": "2025-01-15T10:00:02Z"
        }
    ],
    "started_at": "2025-01-15T10:00:00Z"
}
```

### 3. 暫停執行

```http
POST /api/v1/agents/executions/{execution_id}/pause
Authorization: Bearer <token>

Response 200 OK:
{
    "execution_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "paused",
    "checkpoint_number": 6
}
```

### 4. 恢復執行

```http
POST /api/v1/agents/executions/{execution_id}/resume
Authorization: Bearer <token>

{
    "from_checkpoint": 6  // 可選，從特定 checkpoint 恢復
}

Response 200 OK:
{
    "execution_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "running",
    "resumed_from_step": 6
}
```

### 5. Multi-Agent 執行

```http
POST /api/v1/agents/orchestrate
Content-Type: application/json
Authorization: Bearer <token>

{
    "input": "分析我們的競爭對手在搜尋引擎上的表現，並給出 SEO 優化建議",
    "auto_route": true  // 自動路由，或手動指定 agents
}

Response 200 OK:
{
    "orchestration_id": "660e8400-e29b-41d4-a716-446655440000",
    "strategy": "sequential",
    "agents": ["seo_researcher", "data_analyst", "strategist"],
    "status": "running"
}
```

### 6. 註冊自訂工具

```http
POST /api/v1/tools
Content-Type: application/json
Authorization: Bearer <token>

{
    "name": "send_email",
    "description": "發送電子郵件給指定收件人",
    "category": "communication",
    "parameters": {
        "type": "object",
        "properties": {
            "to": {
                "type": "string",
                "description": "收件人電子郵件地址"
            },
            "subject": {
                "type": "string",
                "description": "郵件主旨"
            },
            "body": {
                "type": "string",
                "description": "郵件內容"
            }
        },
        "required": ["to", "subject", "body"]
    },
    "endpoint": "https://api.example.com/send-email",
    "auth_required": true,
    "timeout_ms": 5000
}

Response 201 Created:
{
    "tool_id": "770e8400-e29b-41d4-a716-446655440000",
    "name": "send_email",
    "status": "active"
}
```

## 效能優化

### 1. 並行工具執行

當多個工具呼叫之間沒有依賴關係時，可以並行執行：

```go
func (a *ReActAgent) executeToolsParallel(ctx context.Context, toolCalls []ToolCall) ([]string, error) {
    results := make([]string, len(toolCalls))
    errors := make(chan error, len(toolCalls))

    var wg sync.WaitGroup
    for i, tc := range toolCalls {
        wg.Add(1)
        go func(index int, call ToolCall) {
            defer wg.Done()

            output, err := a.executeTool(ctx, call.ToolName, call.ToolInput)
            if err != nil {
                errors <- err
                return
            }
            results[index] = output
        }(i, tc)
    }

    wg.Wait()
    close(errors)

    if len(errors) > 0 {
        return nil, <-errors
    }

    return results, nil
}
```

**效能提升**：
- 3 個獨立工具呼叫：從 6 秒 → 2 秒（66% 提升）

### 2. Prompt 快取

對於重複的系統提示詞，使用 Anthropic 的 Prompt Caching：

```go
func (a *ReActAgent) thinkWithCache(ctx context.Context, input string) (string, int, error) {
    resp, err := a.LLM.Chat(ctx, &ChatRequest{
        Model: "claude-3-5-sonnet-20241022",
        Messages: []Message{
            {
                Role:    "system",
                Content: a.SystemPrompt,
                CacheControl: &CacheControl{Type: "ephemeral"},  // 快取系統提示
            },
            {Role: "user", Content: input},
        },
    })

    // Cache hits 可節省 90% tokens
    return resp.Choices[0].Message.Content, resp.Usage.TotalTokens, nil
}
```

**成本節省**：
- 10 次相同 Agent 呼叫：$0.50 → $0.14（72% 節省）

### 3. 智慧 Checkpoint

只在關鍵步驟建立 checkpoint，降低儲存成本：

```go
func (a *ReActAgent) shouldCreateCheckpoint(step Step) bool {
    // 1. 每 3 步
    if step.StepNumber%3 == 0 {
        return true
    }

    // 2. 成功的工具執行
    if step.StepType == "observation" && step.ToolOutput != "" {
        return true
    }

    // 3. 接近最終答案
    if strings.Contains(step.Content, "Final Answer") {
        return true
    }

    return false
}
```

**儲存優化**：
- 15 步執行：15 個 checkpoints → 6 個 checkpoints（60% 減少）

### 4. 工具結果摘要

當工具返回大量資料時，使用 LLM 摘要後再傳入下一步：

```go
func (a *ReActAgent) summarizeToolOutput(ctx context.Context, output string, maxLength int) (string, error) {
    if len(output) <= maxLength {
        return output, nil
    }

    resp, err := a.LLM.Chat(ctx, &ChatRequest{
        Model: "gpt-3.5-turbo",  // 使用便宜的模型摘要
        Messages: []Message{
            {Role: "user", Content: fmt.Sprintf("請將以下內容摘要為 %d 字以內：\n\n%s", maxLength/4, output)},
        },
    })

    return resp.Choices[0].Message.Content, nil
}
```

**Token 節省**：
- 5000 字工具輸出 → 200 字摘要（96% 減少）

## 監控與告警

### 1. 關鍵指標

```go
// Prometheus 指標定義
var (
    executionDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "agent_execution_duration_seconds",
            Help:    "Agent execution duration in seconds",
            Buckets: prometheus.ExponentialBuckets(1, 2, 10),
        },
        []string{"agent_type", "status"},
    )

    stepDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "agent_step_duration_seconds",
            Help:    "Agent step duration in seconds",
            Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
        },
        []string{"agent_type", "step_type"},
    )

    toolExecutions = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "agent_tool_executions_total",
            Help: "Total number of tool executions",
        },
        []string{"tool_name", "status"},
    )

    llmTokens = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "agent_llm_tokens",
            Help:    "Number of LLM tokens used",
            Buckets: prometheus.ExponentialBuckets(100, 2, 12),
        },
        []string{"agent_type", "model"},
    )
)
```

### 2. 告警規則

```yaml
# Prometheus Alert Rules
groups:
  - name: agent_platform
    interval: 30s
    rules:
      # 執行失敗率過高
      - alert: HighExecutionFailureRate
        expr: |
          rate(agent_execution_duration_seconds_count{status="failed"}[5m])
          / rate(agent_execution_duration_seconds_count[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Agent execution failure rate > 10%"

      # 執行時間過長
      - alert: SlowExecution
        expr: |
          histogram_quantile(0.95,
            rate(agent_execution_duration_seconds_bucket[5m])
          ) > 60
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "P95 execution time > 60s"

      # 工具執行失敗
      - alert: ToolExecutionFailure
        expr: |
          rate(agent_tool_executions_total{status="failed"}[5m]) > 10
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Tool execution failure rate > 10/min"

      # Token 使用量異常
      - alert: HighTokenUsage
        expr: |
          rate(agent_llm_tokens_sum[1h]) > 1000000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Token usage > 1M/hour"
```

### 3. 日誌追蹤

使用結構化日誌記錄所有執行步驟：

```go
func (a *ReActAgent) logStep(step Step, execution *Execution) {
    log.WithFields(log.Fields{
        "execution_id": execution.ID,
        "user_id":      execution.UserID,
        "agent_type":   execution.AgentType,
        "step_number":  step.StepNumber,
        "step_type":    step.StepType,
        "tool_name":    step.ToolName,
        "duration_ms":  step.DurationMS,
        "tokens":       step.Tokens,
    }).Info("Agent step executed")
}
```

**查詢範例（ELK）**：
```
# 查詢特定執行的所有步驟
execution_id:"550e8400-e29b-41d4-a716-446655440000"

# 查詢失敗的工具執行
step_type:"action" AND tool_name:* AND error:*

# 分析平均 token 使用
{
  "aggs": {
    "avg_tokens": {
      "avg": {"field": "tokens"}
    }
  }
}
```

## 部署架構

### Kubernetes 部署

```yaml
# agent-service-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-service
spec:
  replicas: 5
  selector:
    matchLabels:
      app: agent-service
  template:
    metadata:
      labels:
        app: agent-service
    spec:
      containers:
      - name: agent-service
        image: agent-platform:v1.0.0
        resources:
          requests:
            memory: "2Gi"
            cpu: "1000m"
          limits:
            memory: "4Gi"
            cpu: "2000m"
        env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: db-secret
              key: url
        - name: REDIS_URL
          valueFrom:
            configMapKeyRef:
              name: redis-config
              key: url
        - name: OPENAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: llm-secret
              key: openai-key
        ports:
        - containerPort: 8080
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
  name: agent-service
spec:
  selector:
    app: agent-service
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
  type: LoadBalancer

---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: agent-service-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: agent-service
  minReplicas: 5
  maxReplicas: 50
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

## 成本估算

### 每月運營成本（10,000 用戶，每人每天 5 次 Agent 執行）

| 項目 | 用量 | 單價 | 月成本 |
|------|------|------|--------|
| **LLM API** | | | |
| GPT-4 Turbo | 150M tokens | $10/1M | $1,500 |
| GPT-3.5 Turbo (摘要) | 50M tokens | $0.5/1M | $25 |
| Claude 3.5 Sonnet | 100M tokens | $3/1M | $300 |
| **基礎設施** | | | |
| PostgreSQL (RDS) | db.r5.2xlarge | $0.504/hr | $365 |
| Redis (ElastiCache) | cache.r5.xlarge | $0.252/hr | $183 |
| Application (EKS) | 20 × c5.2xlarge | $0.34/hr | $4,896 |
| **儲存** | | | |
| Database Storage | 500GB | $0.115/GB | $58 |
| S3 (Checkpoints) | 1TB | $0.023/GB | $23 |
| **網路** | | | |
| Data Transfer | 5TB | $0.09/GB | $450 |
| **監控** | | | |
| CloudWatch | - | - | $150 |
| **總計** | | | **$7,950** |

### 成本優化策略

1. **LLM 模型選擇**
   - 簡單任務使用 GPT-3.5：成本降低 90%
   - 使用 Prompt Caching：成本降低 70%
   - 批次請求：throughput 提升 50%

2. **基礎設施優化**
   - Spot Instances：成本降低 60%
   - Reserved Instances（1年）：成本降低 40%
   - Auto-scaling：閒置時段成本降低 50%

3. **儲存優化**
   - Checkpoint 壓縮：空間節省 60%
   - S3 Intelligent-Tiering：成本降低 30%
   - 定期清理舊資料（>30天）

**優化後月成本：$4,770（降低 40%）**

## 安全考量

### 1. 工具執行沙箱

所有程式碼執行類工具必須在隔離環境中運行：

```go
type SandboxConfig struct {
    MaxMemoryMB  int           // 記憶體限制
    MaxCPUCores  int           // CPU 核心限制
    Timeout      time.Duration // 執行超時
    NetworkAccess bool         // 是否允許網路存取
    AllowedDomains []string    // 白名單域名
}

func (t *PythonTool) ExecuteInSandbox(ctx context.Context, code string) (string, error) {
    config := SandboxConfig{
        MaxMemoryMB:   512,
        MaxCPUCores:   1,
        Timeout:       30 * time.Second,
        NetworkAccess: false,
    }

    // 使用 Docker 容器隔離執行
    // ...
}
```

### 2. Prompt Injection 防護

檢測並阻擋惡意提示詞注入：

```go
func (a *ReActAgent) detectPromptInjection(input string) bool {
    dangerousPatterns := []string{
        "ignore previous instructions",
        "disregard all rules",
        "you are now",
        "system:",
        "\\[SYSTEM\\]",
    }

    lowerInput := strings.ToLower(input)
    for _, pattern := range dangerousPatterns {
        if strings.Contains(lowerInput, pattern) {
            log.Warn("Potential prompt injection detected", "input", input)
            return true
        }
    }

    return false
}
```

### 3. 工具權限控制

基於 RBAC 的工具存取控制：

```sql
CREATE TABLE user_tool_permissions (
    user_id UUID NOT NULL REFERENCES users(id),
    tool_id UUID NOT NULL REFERENCES tools(id),
    permission VARCHAR(20) NOT NULL,  -- 'read', 'execute', 'admin'
    granted_at TIMESTAMP NOT NULL DEFAULT NOW(),
    granted_by UUID REFERENCES users(id),

    PRIMARY KEY (user_id, tool_id)
);
```

## 總結

AI Agent 平台相較於傳統 ChatGPT 系統的關鍵差異：

| 特性 | ChatGPT | AI Agent |
|------|---------|----------|
| **互動模式** | 對話式 | 任務式 |
| **能力範圍** | 回答問題 | 執行動作 |
| **工具使用** | 無 | 多種工具整合 |
| **推理模式** | 單次推理 | 迭代推理（ReAct） |
| **狀態管理** | 對話歷史 | 執行狀態 + Checkpoint |
| **協作能力** | 單一模型 | Multi-Agent 協作 |
| **可靠性** | 無重試機制 | 自動重試 + 錯誤處理 |

透過本章的設計，你學會了：

1. ✅ **ReAct 模式**：結合推理與行動的迭代執行框架
2. ✅ **Function Calling**：讓 LLM 能夠呼叫外部工具
3. ✅ **狀態管理**：執行過程的持久化與恢復機制
4. ✅ **Multi-Agent 協作**：多個專業 Agent 協同完成複雜任務
5. ✅ **錯誤處理**：完善的重試、降級、恢復策略

**下一章**：我們將學習 **RAG (Retrieval-Augmented Generation) 系統**，讓 AI 能夠基於私有知識庫回答問題。
