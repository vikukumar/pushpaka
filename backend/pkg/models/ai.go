package models

// AITool represents an OpenAI-compatible tool definition.
type AITool struct {
	Type     string     `json:"type"`
	Function AIToolFunc `json:"function"`
}

type AIToolFunc struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Parameters  AIToolParams `json:"parameters"`
}

type AIToolParams struct {
	Type       string                         `json:"type"`
	Properties map[string]AIToolParamProperty `json:"properties"`
	Required   []string                       `json:"required,omitempty"`
}

type AIToolParamProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// AIToolCall is a tool call requested by the model.
type AIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// AIToolResult is the result of executing a tool call.
type AIToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

// AIAgentMessage represents one chat turn in the agent conversation.
type AIAgentMessage struct {
	Role       string       `json:"role"` // user / assistant / tool
	Content    string       `json:"content"`
	ToolCallID string       `json:"tool_call_id,omitempty"` // for role=tool
	ToolCalls  []AIToolCall `json:"tool_calls,omitempty"`   // for role=assistant
}

// AIAgentResponse is what the agent endpoint returns.
type AIAgentResponse struct {
	PendingToolCall *AIPendingToolCall `json:"pending_tool_call,omitempty"`
	Reply           string             `json:"reply,omitempty"`
	Messages        []AIAgentMessage   `json:"messages,omitempty"`
}

// AIPendingToolCall is returned when the AI wants to run a tool but needs approval.
type AIPendingToolCall struct {
	ToolCallID string                 `json:"tool_call_id"`
	ToolName   string                 `json:"tool_name"`
	Args       map[string]interface{} `json:"args"`
}

// ToOpenAIMap serializes a message for the OpenAI API request body.
func (m AIAgentMessage) ToOpenAIMap() map[string]interface{} {
	msg := map[string]interface{}{
		"role":    m.Role,
		"content": m.Content,
	}
	if len(m.ToolCalls) > 0 {
		msg["tool_calls"] = m.ToolCalls
	}
	if m.ToolCallID != "" {
		msg["tool_call_id"] = m.ToolCallID
	}
	return msg
}
