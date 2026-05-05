package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vikukumar/pushpaka/pkg/models"
)

// AIAgentRequest is the request body for the agent chat endpoint.
type AIAgentRequest struct {
	Messages   []models.AIAgentMessage `json:"messages"`
	ProjectID  string                  `json:"project_id,omitempty"`
	Autonomous bool                    `json:"autonomous"` // If true, execute tools without waiting for approval
}

// AIAgentResponse is what the agent endpoint returns.
type AIAgentResponse struct {
	// If PendingToolCall is set, the client must approve before the tool runs.
	PendingToolCall *models.AIPendingToolCall `json:"pending_tool_call,omitempty"`
	// Reply is the final text response when there are no pending approvals.
	Reply    string                  `json:"reply,omitempty"`
	Messages []models.AIAgentMessage `json:"messages,omitempty"` // Full updated conversation
}

// PlatformTools returns the list of platform tools available to the AI agent.
func PlatformTools() []models.AITool {
	return []models.AITool{
		{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        "get_deployment_logs",
				Description: "Retrieve the latest logs for a specific deployment. Use this to diagnose errors or check the deployment output.",
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"deployment_id": {
							Type:        "string",
							Description: "The UUID of the deployment to get logs for.",
						},
					},
					Required: []string{"deployment_id"},
				},
			},
		},
		{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        "get_project_deployments",
				Description: "List all deployments for a project, including their status (running, failed, stopped, queued).",
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"project_id": {
							Type:        "string",
							Description: "The UUID of the project.",
						},
					},
					Required: []string{"project_id"},
				},
			},
		},
		{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        "restart_deployment",
				Description: "Restart a deployment by re-building and re-deploying the same commit. Use when a deployment crashed or needs to be restarted after fixing config.",
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"deployment_id": {
							Type:        "string",
							Description: "The UUID of the deployment to restart.",
						},
					},
					Required: []string{"deployment_id"},
				},
			},
		},
		{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        "sync_project",
				Description: "Check the git repository for new commits and trigger a deployment if changes are found.",
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"project_id": {
							Type:        "string",
							Description: "The UUID of the project to sync.",
						},
					},
					Required: []string{"project_id"},
				},
			},
		},
		{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        "promote_deployment",
				Description: "Promote a running deployment to be the live/default deployment for the project. This routes domain traffic to it.",
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"deployment_id": {
							Type:        "string",
							Description: "The UUID of the running deployment to promote to live/default.",
						},
					},
					Required: []string{"deployment_id"},
				},
			},
		},
		{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        "analyze_deployment_logs",
				Description: "Run an AI-powered analysis on a deployment's logs to identify errors, crashes, and suggest fixes.",
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"deployment_id": {
							Type:        "string",
							Description: "The UUID of the deployment to analyze.",
						},
					},
					Required: []string{"deployment_id"},
				},
			},
		},
	}
}

// AIToolsExecutor holds all dependencies needed to execute tool calls.
type AIToolsExecutor struct {
	deploySvc *DeploymentService
	logSvc    *LogService
	aiSvc     *AIService
	userCfg   *models.AIConfig
	ragDocs   []models.RAGDocument
}

func NewAIToolsExecutor(deploySvc *DeploymentService, logSvc *LogService, aiSvc *AIService) *AIToolsExecutor {
	return &AIToolsExecutor{deploySvc: deploySvc, logSvc: logSvc, aiSvc: aiSvc}
}

// WithUserConfig returns a new executor with user-specific context.
func (e *AIToolsExecutor) WithUserConfig(userCfg *models.AIConfig, ragDocs []models.RAGDocument) *AIToolsExecutor {
	return &AIToolsExecutor{
		deploySvc: e.deploySvc,
		logSvc:    e.logSvc,
		aiSvc:     e.aiSvc,
		userCfg:   userCfg,
		ragDocs:   ragDocs,
	}
}

// ExecuteToolCall runs a tool call and returns the string result to include in the conversation.
func (e *AIToolsExecutor) ExecuteToolCall(ctx context.Context, userID string, call models.AIToolCall) (string, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("invalid tool args: %w", err)
	}

	getStr := func(key string) string {
		if v, ok := args[key]; ok {
			return fmt.Sprint(v)
		}
		return ""
	}

	switch call.Function.Name {
	case "get_deployment_logs":
		depID := getStr("deployment_id")
		logs, err := e.logSvc.GetByDeployment(depID)
		if err != nil {
			return fmt.Sprintf("error fetching logs: %v", err), nil
		}
		// Summarise to avoid huge payloads
		var sb strings.Builder
		for _, l := range logs {
			sb.WriteString(fmt.Sprintf("[%s][%s] %s\n", l.Level, l.Stream, l.Message))
		}
		result := sb.String()
		if len(result) > 6000 {
			result = result[:3000] + "\n... [truncated] ...\n" + result[len(result)-3000:]
		}
		return result, nil

	case "get_project_deployments":
		projectID := getStr("project_id")
		deps, err := e.deploySvc.ListByProject(projectID, userID, 20, 0)
		if err != nil {
			return fmt.Sprintf("error fetching deployments: %v", err), nil
		}
		var sb strings.Builder
		for _, d := range deps {
			defaultMark := ""
			if d.IsDefault {
				defaultMark = " [LIVE]"
			}
			sb.WriteString(fmt.Sprintf("ID:%s Status:%s%s Branch:%s Commit:%s\n",
				d.ID, d.Status, defaultMark, d.Branch, d.CommitSHA[:7]))
		}
		return sb.String(), nil

	case "restart_deployment":
		depID := getStr("deployment_id")
		newDep, err := e.deploySvc.RestartDeployment(userID, depID)
		if err != nil {
			return fmt.Sprintf("restart failed: %v", err), nil
		}
		return fmt.Sprintf("Restart triggered. New deployment ID: %s (status: queued)", newDep.ID), nil

	case "sync_project":
		projectID := getStr("project_id")
		dep, task, err := e.deploySvc.SyncRepo(userID, projectID)
		if err != nil {
			if err.Error() == "already up to date" {
				return "Project is already up to date — no new commits found.", nil
			}
			return fmt.Sprintf("sync failed: %v", err), nil
		}
		if task != nil {
			return fmt.Sprintf("Sync task started: %s. The pipeline (Build -> Test) is now running.", task.ID), nil
		}
		return fmt.Sprintf("Sync triggered new deployment: %s", dep.ID), nil

	case "promote_deployment":
		depID := getStr("deployment_id")
		dep, err := e.deploySvc.PromoteDeployment(userID, depID)
		if err != nil {
			return fmt.Sprintf("promote failed: %v", err), nil
		}
		return fmt.Sprintf("Deployment %s is now the live/default deployment for project %s.", dep.ID, dep.ProjectID), nil

	case "analyze_deployment_logs":
		depID := getStr("deployment_id")
		logs, err := e.logSvc.GetByDeployment(depID)
		if err != nil {
			return fmt.Sprintf("error fetching logs for analysis: %v", err), nil
		}
		var sb strings.Builder
		for _, l := range logs {
			sb.WriteString(l.Message + "\n")
		}
		analysis, err := e.aiSvc.AnalyzeLogsWithConfig(e.userCfg, e.ragDocs, sb.String())
		if err != nil {
			return fmt.Sprintf("AI analysis failed: %v", err), nil
		}
		return analysis, nil

	default:
		return fmt.Sprintf("unknown tool: %s", call.Function.Name), nil
	}
}

// ChatWithTools sends a conversation to the AI with platform tools available.
// It handles one round of tool calls. If autonomous=true, it auto-executes all
// tool calls and returns the final reply. Otherwise, it returns the first pending
// tool call for user approval.
func (s *AIService) ChatWithTools(
	ctx context.Context,
	userID string,
	userCfg *models.AIConfig,
	ragDocs []models.RAGDocument,
	messages []models.AIAgentMessage,
	executor *AIToolsExecutor,
	autonomous bool,
) (*AIAgentResponse, error) {
	registry := NewToolboxRegistry()
	tools := registry.AllTools()

	systemPrompt := `You are an autonomous DevOps AI assistant for Pushpaka, a self-hosted deployment platform.
You have access to tools to manage deployments. Use them to help the user diagnose issues, restart failing deployments, sync repositories, and promote healthy deployments to live.

Guidelines:
- Always fetch logs before suggesting fixes.
- Prefer to analyze logs before restarting.
- When a deployment fails and you identify the issue, restart it after explaining the root cause.
- Never promote a deployment unless it's running and healthy.
- Be concise and actionable in your responses.`

	if userCfg != nil && userCfg.SystemPrompt != "" {
		systemPrompt = userCfg.SystemPrompt
	}
	systemPrompt = buildRAGContext(ragDocs, systemPrompt)

	// Build the openAI request with tools
	openAIMsgs := make([]map[string]interface{}, 0, len(messages)+1)
	openAIMsgs = append(openAIMsgs, map[string]interface{}{
		"role":    "system",
		"content": systemPrompt,
	})
	for _, m := range messages {
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
		openAIMsgs = append(openAIMsgs, msg)
	}

	endpoint := s.resolveEndpointWithConfig(userCfg)
	model := s.resolvedModel(userCfg)
	apiKey := s.resolvedAPIKey(userCfg)

	for attempt := 0; attempt < 6; attempt++ {
		reqBody := map[string]interface{}{
			"model":      model,
			"messages":   openAIMsgs,
			"tools":      tools,
			"max_tokens": 1024,
		}
		data, _ := json.Marshal(reqBody)

		httpReq, _ := http.NewRequestWithContext(ctx, "POST", endpoint+"/chat/completions", bytes.NewReader(data))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		if strings.Contains(endpoint, "openrouter") {
			httpReq.Header.Set("HTTP-Referer", "https://pushpaka")
			httpReq.Header.Set("X-Title", "Pushpaka")
		}

		httpClient := &http.Client{Timeout: 60 * time.Second}
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("AI request failed: %w", err)
		}
		defer resp.Body.Close()

		var result struct {
			Choices []struct {
				Message struct {
					Role      string              `json:"role"`
					Content   string              `json:"content"`
					ToolCalls []models.AIToolCall `json:"tool_calls"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Error *struct{ Message string } `json:"error"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		if result.Error != nil {
			return nil, fmt.Errorf("AI error: %s", result.Error.Message)
		}
		if len(result.Choices) == 0 {
			return nil, fmt.Errorf("AI returned no choices")
		}

		choice := result.Choices[0]

		// Build the assistant message for history tracking
		assistantMsg := models.AIAgentMessage{
			Role:      "assistant",
			Content:   choice.Message.Content,
			ToolCalls: choice.Message.ToolCalls,
		}
		messages = append(messages, assistantMsg)
		openAIMsgs = append(openAIMsgs, assistantMsg.ToOpenAIMap())

		// No tool calls — final response
		if len(choice.Message.ToolCalls) == 0 {
			return &AIAgentResponse{
				Reply:    choice.Message.Content,
				Messages: messages,
			}, nil
		}

		// Tool calls requested
		if !autonomous {
			// Return the FIRST tool call for user approval
			tc := choice.Message.ToolCalls[0]
			var tcArgs map[string]interface{}
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &tcArgs)
			return &AIAgentResponse{
				PendingToolCall: &models.AIPendingToolCall{
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
					Args:       tcArgs,
				},
				Messages: messages,
			}, nil
		}

		// Autonomous: execute all tool calls
		for _, tc := range choice.Message.ToolCalls {
			exec := executor
			if userCfg != nil {
				exec = executor.WithUserConfig(userCfg, ragDocs)
			}
			result, execErr := exec.ExecuteToolCall(ctx, userID, tc)
			if execErr != nil {
				result = fmt.Sprintf("error: %v", execErr)
			}
			toolMsg := models.AIAgentMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolMsg)
			openAIMsgs = append(openAIMsgs, toolMsg.ToOpenAIMap())
		}
		// Loop back to let the AI process the tool results
	}

	return &AIAgentResponse{
		Reply:    "Maximum tool call iterations reached.",
		Messages: messages,
	}, nil
}

