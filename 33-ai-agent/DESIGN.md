# Chapter 33: AI Agent Platform（AI 代理平台）

> **難度**：★★★★★
> **預估時間**：6-8 週
> **核心概念**：Agent 編排、Tool Calling、ReAct 模式、多 Agent 協作、狀態管理

---

## Act 1: 從對話到行動

週一早晨，Emma 興奮地展示了上一章做的 ChatGPT 系統。

**Emma**：「我們的對話系統很棒！但我希望 AI 能做更多事情，不只是聊天。」

**David**：「你的意思是？」

**Emma**：「比如，我問『台北今天天氣如何？』，我希望 AI 能真的去查天氣 API，而不是告訴我『抱歉，我不知道即時天氣』。」

**Michael**：「這就是 **AI Agent（AI 代理）** 的概念！Agent 不只能思考，還能採取行動。」

**Sarah**：「什麼是 Agent？」

### Agent vs 傳統 LLM

**David**：「讓我用圖來解釋：」

```
傳統 LLM（ChatGPT）:
用戶問題 → LLM 思考 → 文字回答

AI Agent:
用戶問題 → Agent 思考 → 決定使用工具 → 呼叫工具 → 獲取結果 → 再思考 → 最終回答
             ↑                                                    ↓
             └────────────────── 循環 ─────────────────────────┘
```

**Michael**：「Agent 的關鍵是 **Tool Calling（工具調用）**。它能：」
- 📞 呼叫 API（天氣、股票、新聞）
- 🔍 搜尋網路
- 🗄️ 查詢資料庫
- 🧮 執行計算
- 📧 發送郵件
- 📝 寫入檔案

**Emma**：「太酷了！我們來實作一個！」

### 第一個簡單的 Agent

```go
package agent

// Tool 工具介面
type Tool interface {
    Name() string
    Description() string
    Execute(input string) (string, error)
}

// WeatherTool 天氣工具
type WeatherTool struct {
    APIKey string
}

func (t *WeatherTool) Name() string {
    return "get_weather"
}

func (t *WeatherTool) Description() string {
    return "獲取指定城市的當前天氣。輸入：城市名稱（例如：台北、東京）"
}

func (t *WeatherTool) Execute(city string) (string, error) {
    // 呼叫天氣 API
    weather, err := fetchWeather(t.APIKey, city)
    if err != nil {
        return "", err
    }

    return fmt.Sprintf("溫度：%d°C，天氣：%s", weather.Temp, weather.Condition), nil
}

// Agent 代理
type Agent struct {
    Name    string
    LLM     *llm.Client
    Tools   []Tool
    Memory  []Message
}

// Run 運行 Agent
func (a *Agent) Run(userMessage string) (string, error) {
    // 1. 添加用戶訊息到記憶
    a.Memory = append(a.Memory, Message{
        Role:    "user",
        Content: userMessage,
    })

    // 2. 構建系統提示
    systemPrompt := a.buildSystemPrompt()

    // 3. 準備對話
    messages := []llm.Message{
        {Role: "system", Content: systemPrompt},
    }

    for _, msg := range a.Memory {
        messages = append(messages, llm.Message{
            Role:    msg.Role,
            Content: msg.Content,
        })
    }

    // 4. 呼叫 LLM
    resp, err := a.LLM.Chat(context.Background(), &llm.ChatRequest{
        Model:    "gpt-4",
        Messages: messages,
    })

    if err != nil {
        return "", err
    }

    answer := resp.Choices[0].Message.Content

    // 5. 添加 AI 回應到記憶
    a.Memory = append(a.Memory, Message{
        Role:    "assistant",
        Content: answer,
    })

    return answer, nil
}

// buildSystemPrompt 構建系統提示
func (a *Agent) buildSystemPrompt() string {
    prompt := fmt.Sprintf("你是一個名為 %s 的 AI 助手。\n\n", a.Name)
    prompt += "你有以下工具可以使用：\n\n"

    for _, tool := range a.Tools {
        prompt += fmt.Sprintf("- %s: %s\n", tool.Name(), tool.Description())
    }

    prompt += "\n當需要使用工具時，請以以下格式回應：\n"
    prompt += "TOOL: <工具名稱>\n"
    prompt += "INPUT: <輸入>\n"

    return prompt
}
```

**Sarah**：「等等，這樣 Agent 怎麼知道要呼叫工具？」

**David**：「現在還不知道。我們需要教它如何使用工具。這就是 **Tool Calling** 的精髓。」

---

## Act 2: Tool Calling（工具調用）

**Michael**：「OpenAI 和 Anthropic 都支援 **Function Calling**，這是標準化的工具調用方式。」

### Function Calling API

**Sarah**：「來看看 OpenAI 的 Function Calling：」

```go
// FunctionDefinition 函數定義
type FunctionDefinition struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Parameters  map[string]interface{} `json:"parameters"`
}

// ChatRequestWithFunctions 帶函數的對話請求
type ChatRequestWithFunctions struct {
    Model     string              `json:"model"`
    Messages  []Message           `json:"messages"`
    Functions []FunctionDefinition `json:"functions,omitempty"`
    FunctionCall interface{}       `json:"function_call,omitempty"` // "auto", "none", 或 {"name": "function_name"}
}

// 定義天氣工具的函數
var weatherFunction = FunctionDefinition{
    Name:        "get_weather",
    Description: "獲取指定城市的當前天氣",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "city": map[string]interface{}{
                "type":        "string",
                "description": "城市名稱，例如：台北、東京",
            },
        },
        "required": []string{"city"},
    },
}
```

**Emma**：「所以我們把工具的 schema 告訴 LLM，它就知道何時以及如何使用？」

**David**：「沒錯！LLM 會分析用戶問題，決定是否需要呼叫函數。」

### 完整的 Function Calling 流程

```go
// AgentWithFunctionCalling 支援 Function Calling 的 Agent
type AgentWithFunctionCalling struct {
    LLM       *llm.Client
    Tools     map[string]Tool
    Memory    []Message
    MaxSteps  int // 最大步驟數（防止無限循環）
}

// Run 運行 Agent（支援 Function Calling）
func (a *AgentWithFunctionCalling) Run(ctx context.Context, userMessage string) (string, error) {
    // 1. 添加用戶訊息
    a.Memory = append(a.Memory, Message{
        Role:    "user",
        Content: userMessage,
    })

    // 2. 準備函數定義
    functions := a.buildFunctionDefinitions()

    // 3. 執行推理循環
    for step := 0; step < a.MaxSteps; step++ {
        // 3.1 呼叫 LLM
        req := &ChatRequestWithFunctions{
            Model:        "gpt-4",
            Messages:     a.convertMemoryToMessages(),
            Functions:    functions,
            FunctionCall: "auto", // 讓 LLM 自動決定
        }

        resp, err := a.LLM.ChatWithFunctions(ctx, req)
        if err != nil {
            return "", err
        }

        message := resp.Choices[0].Message

        // 3.2 檢查是否要呼叫函數
        if message.FunctionCall == nil {
            // 沒有函數呼叫，直接返回答案
            a.Memory = append(a.Memory, Message{
                Role:    "assistant",
                Content: message.Content,
            })
            return message.Content, nil
        }

        // 3.3 執行函數
        functionName := message.FunctionCall.Name
        functionArgs := message.FunctionCall.Arguments

        log.Info("Agent 呼叫工具",
            "function", functionName,
            "arguments", functionArgs,
        )

        tool, exists := a.Tools[functionName]
        if !exists {
            return "", fmt.Errorf("工具不存在: %s", functionName)
        }

        // 解析參數
        var args map[string]interface{}
        json.Unmarshal([]byte(functionArgs), &args)

        // 執行工具
        result, err := tool.Execute(args)
        if err != nil {
            result = fmt.Sprintf("錯誤: %v", err)
        }

        // 3.4 添加函數呼叫和結果到記憶
        a.Memory = append(a.Memory, Message{
            Role:         "assistant",
            Content:      "",
            FunctionCall: message.FunctionCall,
        })

        a.Memory = append(a.Memory, Message{
            Role:    "function",
            Name:    functionName,
            Content: result,
        })

        // 繼續下一輪推理
    }

    return "", errors.New("超過最大步驟數")
}

// buildFunctionDefinitions 構建函數定義列表
func (a *AgentWithFunctionCalling) buildFunctionDefinitions() []FunctionDefinition {
    functions := make([]FunctionDefinition, 0, len(a.Tools))

    for _, tool := range a.Tools {
        functions = append(functions, tool.GetDefinition())
    }

    return functions
}
```

**執行範例**:

```
用戶: 台北今天天氣如何？

[步驟 1] LLM 思考
→ 決定呼叫 get_weather(city="台北")

[步驟 2] 執行工具
→ 結果: "溫度：28°C，天氣：晴天"

[步驟 3] LLM 再次思考（基於工具結果）
→ 最終回答: "台北今天天氣晴朗，溫度約 28°C。"
```

**Sarah**：「這太強大了！Agent 能自己決定何時使用工具，而且還能理解工具的結果！」

---

## Act 3: ReAct 模式

**Michael**：「現在讓我們學習一個更進階的模式：**ReAct（Reasoning + Acting）**。」

**Emma**：「ReAct 是什麼？」

**David**：「ReAct 是一個思考和行動交替進行的循環：」

```
用戶問題
  ↓
[Thought] → Agent 思考下一步要做什麼
  ↓
[Action] → 執行具體行動（呼叫工具）
  ↓
[Observation] → 觀察行動結果
  ↓
[Thought] → 基於觀察再思考
  ↓
... 重複直到得出最終答案 ...
  ↓
[Answer] → 最終回答
```

### ReAct 實作

```go
// ReActAgent ReAct 模式的 Agent
type ReActAgent struct {
    LLM      *llm.Client
    Tools    map[string]Tool
    Memory   []ReActStep
    MaxSteps int
}

// ReActStep ReAct 步驟
type ReActStep struct {
    StepType    string // "thought", "action", "observation", "answer"
    Content     string
    ToolName    string
    ToolInput   string
    ToolOutput  string
    Timestamp   time.Time
}

// Run 執行 ReAct 循環
func (a *ReActAgent) Run(ctx context.Context, question string) (string, error) {
    log.Info("ReAct Agent 開始", "question", question)

    for step := 0; step < a.MaxSteps; step++ {
        log.Info("ReAct 步驟", "step", step+1)

        // 1. Thought（思考）
        thought, err := a.think(ctx, question)
        if err != nil {
            return "", err
        }

        a.Memory = append(a.Memory, ReActStep{
            StepType:  "thought",
            Content:   thought,
            Timestamp: time.Now(),
        })

        log.Info("Thought", "content", thought)

        // 2. 檢查是否得出最終答案
        if strings.Contains(thought, "Final Answer:") {
            answer := extractAnswer(thought)
            a.Memory = append(a.Memory, ReActStep{
                StepType:  "answer",
                Content:   answer,
                Timestamp: time.Now(),
            })
            return answer, nil
        }

        // 3. Action（行動）
        action, toolName, toolInput, err := a.decideAction(thought)
        if err != nil {
            return "", err
        }

        a.Memory = append(a.Memory, ReActStep{
            StepType:  "action",
            Content:   action,
            ToolName:  toolName,
            ToolInput: toolInput,
            Timestamp: time.Now(),
        })

        log.Info("Action", "tool", toolName, "input", toolInput)

        // 4. 執行工具
        tool, exists := a.Tools[toolName]
        if !exists {
            return "", fmt.Errorf("工具不存在: %s", toolName)
        }

        output, err := tool.Execute(toolInput)
        if err != nil {
            output = fmt.Sprintf("錯誤: %v", err)
        }

        // 5. Observation（觀察）
        a.Memory = append(a.Memory, ReActStep{
            StepType:   "observation",
            Content:    output,
            ToolOutput: output,
            Timestamp:  time.Now(),
        })

        log.Info("Observation", "output", output)
    }

    return "", errors.New("超過最大步驟數，未能得出答案")
}

// think LLM 思考下一步
func (a *ReActAgent) think(ctx context.Context, question string) (string, error) {
    prompt := a.buildReActPrompt(question)

    resp, err := a.LLM.Chat(ctx, &llm.ChatRequest{
        Model: "gpt-4",
        Messages: []llm.Message{
            {Role: "user", Content: prompt},
        },
    })

    if err != nil {
        return "", err
    }

    return resp.Choices[0].Message.Content, nil
}

// buildReActPrompt 構建 ReAct 提示
func (a *ReActAgent) buildReActPrompt(question string) string {
    prompt := "你是一個 ReAct Agent。你需要通過 Thought → Action → Observation 的循環來回答問題。\n\n"

    prompt += "可用工具：\n"
    for name, tool := range a.Tools {
        prompt += fmt.Sprintf("- %s: %s\n", name, tool.Description())
    }

    prompt += "\n格式：\n"
    prompt += "Thought: [你的思考過程]\n"
    prompt += "Action: [工具名稱]\n"
    prompt += "Action Input: [工具輸入]\n"
    prompt += "Observation: [工具輸出，由系統自動填入]\n"
    prompt += "... (可重複多次)\n"
    prompt += "Thought: [最終思考]\n"
    prompt += "Final Answer: [最終答案]\n\n"

    prompt += fmt.Sprintf("問題：%s\n\n", question)

    // 添加歷史步驟
    for _, step := range a.Memory {
        switch step.StepType {
        case "thought":
            prompt += fmt.Sprintf("Thought: %s\n", step.Content)
        case "action":
            prompt += fmt.Sprintf("Action: %s\n", step.ToolName)
            prompt += fmt.Sprintf("Action Input: %s\n", step.ToolInput)
        case "observation":
            prompt += fmt.Sprintf("Observation: %s\n", step.Content)
        }
    }

    return prompt
}
```

**執行範例**:

```
問題：台北到東京的機票多少錢？東京今天天氣如何？

[Thought 1] 我需要查詢台北到東京的機票價格，也需要查詢東京的天氣。
           先查機票。

[Action 1] search_flights
[Input 1] from=台北&to=東京

[Observation 1] 最低價格：NT$12,000（經濟艙）

[Thought 2] 已獲得機票資訊。現在需要查詢東京天氣。

[Action 2] get_weather
[Input 2] city=東京

[Observation 2] 溫度：18°C，天氣：多雲

[Thought 3] 我已經獲得所有資訊，可以給出最終答案了。

[Final Answer] 台北到東京的機票最低價格是 NT$12,000（經濟艙）。
              東京今天天氣多雲，溫度約 18°C。
```

**Emma**：「哇！Agent 自己規劃了整個執行流程！」

**Michael**：「沒錯。這就是 ReAct 的強大之處：Agent 能自主推理和規劃。」

---

## Act 4: Chain-of-Thought（思維鏈）

**Sarah**：「我注意到 ReAct 中 Agent 會詳細說明自己的思考過程。這是必要的嗎？」

**David**：「絕對必要！這叫 **Chain-of-Thought（CoT，思維鏈）**。研究顯示，讓 LLM 『大聲思考』能顯著提高推理準確性。」

### CoT 範例

**沒有 CoT**:
```
問題：一個披薩切成 8 片，小明吃了 3 片，小華吃了 2 片，還剩多少片？
答案：3 片
```

**有 CoT**:
```
問題：一個披薩切成 8 片，小明吃了 3 片，小華吃了 2 片，還剩多少片？

思考過程：
1. 披薩總共有 8 片
2. 小明吃了 3 片
3. 小華吃了 2 片
4. 已經吃掉的：3 + 2 = 5 片
5. 剩餘：8 - 5 = 3 片

答案：3 片
```

**Michael**：「第二種方式雖然更囉嗦，但推理過程清晰，錯誤率更低。」

### CoT 提示技巧

```go
// CoTPrompt Chain-of-Thought 提示生成器
type CoTPrompt struct{}

// Build 構建 CoT 提示
func (p *CoTPrompt) Build(question string) string {
    return fmt.Sprintf(`請一步步思考來回答以下問題。

問題：%s

請按以下格式回答：

思考步驟：
1. [第一步]
2. [第二步]
...

最終答案：[答案]
`, question)
}
```

**Zero-Shot CoT**（零樣本）:
```
"Let's think step by step."（讓我們一步步思考）
```

這個簡單的提示就能激活 CoT 推理！

**Few-Shot CoT**（少樣本）:
```
範例 1：
問題：5 + 7 = ?
思考：5 + 7 = 12
答案：12

範例 2：
問題：12 × 3 = ?
思考：12 × 3 = 36
答案：36

現在回答：
問題：8 × 9 = ?
```

**Sarah**：「所以 CoT 是通過提示工程來改善推理能力？」

**David**：「沒錯。在 Agent 系統中，CoT 更是核心，因為它讓 Agent 的決策過程透明且可追蹤。」

---

## Act 5: 狀態管理

**Emma**：「如果 Agent 執行一個複雜的多步驟任務，中途崩潰了怎麼辦？」

**Michael**：「這就需要 **狀態持久化（State Persistence）**。」

### Agent 狀態

```go
// AgentState Agent 狀態
type AgentState struct {
    ID            string
    UserID        string
    Question      string
    CurrentStep   int
    Steps         []ReActStep
    Status        string    // "running", "completed", "failed", "paused"
    Result        string
    ErrorMessage  string
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

// StateManager 狀態管理器
type StateManager struct {
    repo AgentStateRepository
}

// Save 保存狀態
func (m *StateManager) Save(ctx context.Context, state *AgentState) error {
    state.UpdatedAt = time.Now()
    return m.repo.Save(ctx, state)
}

// Load 載入狀態
func (m *StateManager) Load(ctx context.Context, stateID string) (*AgentState, error) {
    return m.repo.GetByID(ctx, stateID)
}

// Resume 恢復執行
func (m *StateManager) Resume(ctx context.Context, stateID string, agent *ReActAgent) (string, error) {
    // 1. 載入狀態
    state, err := m.Load(ctx, stateID)
    if err != nil {
        return "", err
    }

    // 2. 恢復 Agent 記憶
    agent.Memory = state.Steps

    // 3. 繼續執行
    result, err := agent.Run(ctx, state.Question)

    // 4. 更新狀態
    if err != nil {
        state.Status = "failed"
        state.ErrorMessage = err.Error()
    } else {
        state.Status = "completed"
        state.Result = result
    }

    m.Save(ctx, state)

    return result, err
}
```

### 檢查點（Checkpointing）

```go
// StatefulAgent 帶狀態持久化的 Agent
type StatefulAgent struct {
    *ReActAgent
    StateManager *StateManager
    StateID      string
}

// Run 執行（帶狀態保存）
func (a *StatefulAgent) Run(ctx context.Context, question string) (string, error) {
    // 1. 建立初始狀態
    state := &AgentState{
        ID:          uuid.New().String(),
        UserID:      getUserID(ctx),
        Question:    question,
        CurrentStep: 0,
        Status:      "running",
        CreatedAt:   time.Now(),
    }

    a.StateID = state.ID
    a.StateManager.Save(ctx, state)

    // 2. 執行 ReAct 循環
    for step := 0; step < a.MaxSteps; step++ {
        // 2.1 思考
        thought, err := a.think(ctx, question)
        if err != nil {
            state.Status = "failed"
            state.ErrorMessage = err.Error()
            a.StateManager.Save(ctx, state)
            return "", err
        }

        a.Memory = append(a.Memory, ReActStep{
            StepType:  "thought",
            Content:   thought,
            Timestamp: time.Now(),
        })

        // 2.2 保存檢查點
        state.CurrentStep = step
        state.Steps = a.Memory
        a.StateManager.Save(ctx, state)

        // 2.3 檢查是否完成
        if strings.Contains(thought, "Final Answer:") {
            answer := extractAnswer(thought)
            state.Status = "completed"
            state.Result = answer
            a.StateManager.Save(ctx, state)
            return answer, nil
        }

        // 2.4 執行 Action（省略...）
        // ...

        // 2.5 再次保存檢查點
        state.Steps = a.Memory
        a.StateManager.Save(ctx, state)
    }

    state.Status = "failed"
    state.ErrorMessage = "超過最大步驟數"
    a.StateManager.Save(ctx, state)

    return "", errors.New("超過最大步驟數")
}
```

**Emma**：「這樣即使 Agent 中途崩潰，也能從上次的檢查點繼續！」

---

## Act 6: 多 Agent 協作

**Sarah**：「單個 Agent 很強大。如果有多個 Agent 合作呢？」

**Michael**：「這就是 **Multi-Agent System（多代理系統）**！不同的 Agent 可以專精不同領域。」

### Multi-Agent 架構

**David**：「想像一個客服系統：」

```
用戶問題
   ↓
[Orchestrator Agent] ← 協調者
   ↓
   ├→ [Technical Support Agent] ← 技術支援專家
   ├→ [Billing Agent] ← 帳單專家
   ├→ [Product Agent] ← 產品專家
   └→ [Escalation Agent] ← 升級處理專家
```

### 實作 Multi-Agent

```go
// AgentRole Agent 角色
type AgentRole struct {
    Name        string
    Description string
    SystemPrompt string
    Tools       []Tool
}

// MultiAgentSystem 多 Agent 系統
type MultiAgentSystem struct {
    Orchestrator *Agent
    Agents       map[string]*Agent
}

// NewMultiAgentSystem 建立多 Agent 系統
func NewMultiAgentSystem(llm *llm.Client) *MultiAgentSystem {
    // 定義各個 Agent 的角色
    technicalAgent := &Agent{
        Name: "Technical Support",
        LLM:  llm,
        SystemPrompt: "你是技術支援專家，專門處理技術問題、bug 回報、系統錯誤等。",
        Tools: []Tool{
            &SearchKnowledgeBaseTool{},
            &CreateTicketTool{},
        },
    }

    billingAgent := &Agent{
        Name: "Billing Support",
        LLM:  llm,
        SystemPrompt: "你是帳單專家，專門處理付款、退款、發票、訂閱等問題。",
        Tools: []Tool{
            &QueryBillingTool{},
            &ProcessRefundTool{},
        },
    }

    productAgent := &Agent{
        Name: "Product Expert",
        LLM:  llm,
        SystemPrompt: "你是產品專家，專門回答產品功能、使用方法、最佳實踐等問題。",
        Tools: []Tool{
            &SearchDocsTool{},
        },
    }

    // Orchestrator（協調者）
    orchestrator := &Agent{
        Name: "Orchestrator",
        LLM:  llm,
        SystemPrompt: buildOrchestratorPrompt(technicalAgent, billingAgent, productAgent),
    }

    return &MultiAgentSystem{
        Orchestrator: orchestrator,
        Agents: map[string]*Agent{
            "technical": technicalAgent,
            "billing":   billingAgent,
            "product":   productAgent,
        },
    }
}

// Route 路由問題到合適的 Agent
func (m *MultiAgentSystem) Route(ctx context.Context, question string) (string, error) {
    // 1. Orchestrator 決定路由
    routePrompt := fmt.Sprintf(`問題：%s

請決定這個問題應該由哪個 Agent 處理？

可用 Agent：
- technical: 技術支援專家
- billing: 帳單專家
- product: 產品專家

請只回答 Agent 名稱。`, question)

    resp, err := m.Orchestrator.LLM.Chat(ctx, &llm.ChatRequest{
        Model: "gpt-4",
        Messages: []llm.Message{
            {Role: "system", Content: m.Orchestrator.SystemPrompt},
            {Role: "user", Content: routePrompt},
        },
    })

    if err != nil {
        return "", err
    }

    agentName := strings.TrimSpace(strings.ToLower(resp.Choices[0].Message.Content))

    log.Info("Orchestrator 路由決策", "question", question, "agent", agentName)

    // 2. 路由到對應的 Agent
    agent, exists := m.Agents[agentName]
    if !exists {
        return "", fmt.Errorf("未知的 Agent: %s", agentName)
    }

    // 3. 執行專業 Agent
    return agent.Run(ctx, question)
}
```

### Agent 間通訊

**Emma**：「如果一個 Agent 需要諮詢另一個 Agent 的意見呢？」

**Michael**：「Agent 可以互相通訊！」

```go
// AgentMessage Agent 間訊息
type AgentMessage struct {
    From    string
    To      string
    Content string
    Type    string // "request", "response"
}

// AgentCommunication Agent 通訊管理
type AgentCommunication struct {
    messages chan *AgentMessage
}

// Send 發送訊息給另一個 Agent
func (c *AgentCommunication) Send(from, to, content string) {
    c.messages <- &AgentMessage{
        From:    from,
        To:      to,
        Content: content,
        Type:    "request",
    }
}

// Receive 接收訊息
func (c *AgentCommunication) Receive(agentName string) (*AgentMessage, bool) {
    select {
    case msg := <-c.messages:
        if msg.To == agentName {
            return msg, true
        }
    default:
    }
    return nil, false
}
```

**協作範例**:

```
用戶：我想退款，但不知道如何操作

[Orchestrator] → 決定路由到 Billing Agent

[Billing Agent] → 查詢退款政策
                → 發現需要技術協助檢查訂單狀態
                → 發送訊息給 Technical Agent

[Technical Agent] → 查詢系統
                  → 返回訂單資訊

[Billing Agent] → 基於訂單資訊處理退款
                → 返回最終結果給用戶
```

**Sarah**：「多個 Agent 合作，就像一個團隊！」

---

## Act 7: 錯誤處理與重試

**David**：「最後一個重要主題：Agent 會出錯，我們需要優雅地處理錯誤。」

### 錯誤類型

```go
// AgentError Agent 錯誤類型
type AgentErrorType string

const (
    ErrorTypeLLM          AgentErrorType = "llm_error"           // LLM API 錯誤
    ErrorTypeTool         AgentErrorType = "tool_error"         // 工具執行錯誤
    ErrorTypeTimeout      AgentErrorType = "timeout"            // 超時
    ErrorTypeMaxSteps     AgentErrorType = "max_steps"          // 超過最大步驟
    ErrorTypeInvalidInput AgentErrorType = "invalid_input"      // 無效輸入
)

// AgentError Agent 錯誤
type AgentError struct {
    Type    AgentErrorType
    Message string
    Cause   error
}

func (e *AgentError) Error() string {
    return fmt.Sprintf("[%s] %s: %v", e.Type, e.Message, e.Cause)
}
```

### 重試策略

```go
// RetryPolicy 重試策略
type RetryPolicy struct {
    MaxRetries int
    Delay      time.Duration
    Backoff    float64 // 指數退避係數
}

// ResilientAgent 帶重試的 Agent
type ResilientAgent struct {
    *ReActAgent
    RetryPolicy *RetryPolicy
}

// Run 執行（帶重試）
func (a *ResilientAgent) Run(ctx context.Context, question string) (string, error) {
    var lastErr error

    for attempt := 0; attempt <= a.RetryPolicy.MaxRetries; attempt++ {
        if attempt > 0 {
            // 指數退避
            delay := time.Duration(float64(a.RetryPolicy.Delay) * math.Pow(a.RetryPolicy.Backoff, float64(attempt-1)))
            log.Info("重試", "attempt", attempt, "delay", delay)
            time.Sleep(delay)
        }

        result, err := a.ReActAgent.Run(ctx, question)
        if err == nil {
            return result, nil
        }

        // 檢查錯誤類型
        agentErr, ok := err.(*AgentError)
        if !ok {
            agentErr = &AgentError{
                Type:    ErrorTypeLLM,
                Message: "未知錯誤",
                Cause:   err,
            }
        }

        lastErr = agentErr

        // 某些錯誤不應重試
        if !shouldRetry(agentErr.Type) {
            return "", lastErr
        }

        log.Warn("Agent 執行失敗，準備重試",
            "error_type", agentErr.Type,
            "attempt", attempt+1,
        )
    }

    return "", fmt.Errorf("達到最大重試次數: %w", lastErr)
}

// shouldRetry 判斷錯誤是否應該重試
func shouldRetry(errorType AgentErrorType) bool {
    switch errorType {
    case ErrorTypeLLM, ErrorTypeTool, ErrorTypeTimeout:
        return true // 這些錯誤可能是暫時的
    case ErrorTypeInvalidInput, ErrorTypeMaxSteps:
        return false // 這些錯誤重試也無濟於事
    default:
        return false
    }
}
```

### 降級策略

```go
// FallbackAgent 降級 Agent
type FallbackAgent struct {
    Primary   *Agent
    Fallback  *Agent
}

// Run 執行（優先使用主 Agent，失敗則降級）
func (a *FallbackAgent) Run(ctx context.Context, question string) (string, error) {
    // 嘗試主 Agent
    result, err := a.Primary.Run(ctx, question)
    if err == nil {
        return result, nil
    }

    log.Warn("主 Agent 失敗，使用降級 Agent", "error", err)

    // 降級到備用 Agent
    return a.Fallback.Run(ctx, question)
}
```

**Emma**：「這樣 Agent 就更可靠了！」

**Michael**：「沒錯。在生產環境中，錯誤處理和重試機制至關重要。」

---

## 總結

本章我們深入學習了 **AI Agent Platform（AI 代理平台）** 的設計，涵蓋：

### 核心技術點

1. **Agent 基礎**
   - Agent vs 傳統 LLM
   - Tool Calling（工具調用）
   - Function Calling API

2. **推理模式**
   - ReAct（Reasoning + Acting）
   - Chain-of-Thought（思維鏈）
   - Zero-Shot vs Few-Shot CoT

3. **狀態管理**
   - 狀態持久化
   - 檢查點（Checkpointing）
   - 恢復執行

4. **多 Agent 系統**
   - Agent 角色分工
   - Orchestrator 模式
   - Agent 間通訊

5. **可靠性**
   - 錯誤處理
   - 重試策略
   - 降級機制

### 架構特點

- **自主性**：Agent 能自主推理和決策
- **可擴展**：通過工具系統擴展能力
- **可追蹤**：完整的思考和行動歷史
- **可靠性**：狀態持久化 + 錯誤處理

AI Agent 是人工智慧的未來。通過本章學習，你已經掌握了構建生產級 AI Agent 平台的核心技術！🤖✨
