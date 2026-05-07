package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vikukumar/pushpaka/internal/services"
	"github.com/vikukumar/pushpaka/pkg/models"
)

// AIAgent is a worker-side autonomous repair agent.
type AIAgent struct {
	w       *BuildWorker
	project *models.Project
	job     *models.DeploymentJob
	ctx     context.Context
	aiSvc   *services.AIService
}

func NewAIAgent(w *BuildWorker, project *models.Project, job *models.DeploymentJob, aiSvc *services.AIService) *AIAgent {
	return &AIAgent{w: w, project: project, job: job, ctx: context.Background(), aiSvc: aiSvc}
}

// Repair attempts to fix the current project's build/test/deploy failure.
func (a *AIAgent) Repair(errorMsg string) error {
	a.w.appendLog(a.job.DeploymentID, "info", "ai", "Autonomous AI repair agent starting...")

	// Initial message history
	messages := []models.AIAgentMessage{
		{
			Role:    "user",
			Content: fmt.Sprintf("The following task failed: %s. Error: %s. Please fix it.", a.job.DeploymentID, errorMsg),
		},
	}

	registry := services.NewToolboxRegistry()
	tools := registry.AllTools()

	// Max iterations to prevent infinite loops
	for i := 0; i < 10; i++ {
		// 1. Ask AI for next step
		resp, err := a.askAI(messages, tools)
		if err != nil {
			return err
		}

		// 2. Add assistant response to history
		messages = append(messages, *resp)

		if len(resp.ToolCalls) == 0 {
			a.w.appendLog(a.job.DeploymentID, "info", "ai", "AI Agent finished: "+resp.Content)
			break
		}

		// 3. Execute requested tool calls
		for _, tc := range resp.ToolCalls {
			a.w.appendLog(a.job.DeploymentID, "info", "ai", fmt.Sprintf("AI requested tool: %s", tc.Function.Name))

			result := a.executeTool(tc)

			// 4. Add tool results to history
			messages = append(messages, models.AIAgentMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return nil
}

func (a *AIAgent) askAI(messages []models.AIAgentMessage, tools []models.AITool) (*models.AIAgentMessage, error) {
	// Use the platform's AIService with tool calling enabled
	executor := services.NewAIToolsExecutor(nil, nil, a.aiSvc) // We don't need services here as we execute locally

	// We call ChatWithTools in autonomous mode (but we handle execution loop ourselves for better logging)
	resp, err := a.aiSvc.ChatWithTools(a.ctx, a.job.UserID, nil, nil, messages, executor, false)
	if err != nil {
		return nil, err
	}

	if resp.PendingToolCall != nil {
		// Convert PendingToolCall to AIAgentMessage with ToolCalls
		return &models.AIAgentMessage{
			Role: "assistant",
			ToolCalls: []models.AIToolCall{
				{
					ID: resp.PendingToolCall.ToolCallID,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      resp.PendingToolCall.ToolName,
						Arguments: serializeArgs(resp.PendingToolCall.Args),
					},
				},
			},
		}, nil
	}

	return &models.AIAgentMessage{
		Role:    "assistant",
		Content: resp.Reply,
	}, nil
}

func serializeArgs(args map[string]interface{}) string {
	b, _ := json.Marshal(args)
	return string(b)
}

func (a *AIAgent) executeTool(tc models.AIToolCall) string {
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)

	// Workspace path
	workspace := filepath.Join(a.w.cfg.CloneDir, shortID(a.job.UserID), shortID(a.job.ProjectID))

	// Map tool names to shell commands
	cmdStr := ""
	parts := strings.Split(tc.Function.Name, "_")
	if len(parts) >= 2 {
		pm := parts[0]
		action := strings.Join(parts[1:], "_")

		switch pm {
		case "npm", "yarn", "pnpm", "bun":
			cmdStr = fmt.Sprintf("%s %s", pm, action)
		case "pip", "poetry":
			cmdStr = fmt.Sprintf("%s %s", pm, action)
		case "go":
			cmdStr = fmt.Sprintf("go %s", strings.ReplaceAll(action, "_", " "))
		case "git":
			cmdStr = fmt.Sprintf("git %s", strings.ReplaceAll(action, "_", " "))
		case "fs":
			path := ""
			if v, ok := args["path"]; ok {
				path = fmt.Sprint(v)
			}
			switch action {
			case "ls", "ls_la":
				if runtime.GOOS == "windows" {
					cmdStr = "dir " + path
				} else {
					cmdStr = "ls -la " + path
				}
			case "cat", "read_file":
				if runtime.GOOS == "windows" {
					cmdStr = "type " + path
				} else {
					cmdStr = "cat " + path
				}
			case "rm", "rm_rf":
				if runtime.GOOS == "windows" {
					cmdStr = "rmdir /s /q " + path + " 2>nul || del /f /q " + path
				} else {
					cmdStr = "rm -rf " + path
				}
			case "mkdir", "mkdir_p":
				if runtime.GOOS == "windows" {
					cmdStr = "if not exist " + path + " mkdir " + path
				} else {
					cmdStr = "mkdir -p " + path
				}
			case "write_file", "write":
				content := ""
				if v, ok := args["content"]; ok {
					content = fmt.Sprint(v)
				}
				if runtime.GOOS == "windows" {
					// Use powershell for reliable writing on Windows if possible, or just basic echo
					cmdStr = fmt.Sprintf("powershell -Command \"[System.IO.File]::WriteAllText('%s', '%s')\"", path, strings.ReplaceAll(content, "'", "''"))
				} else {
					// Use printf to handle escaping and write to file
					cmdStr = fmt.Sprintf("printf %%s %q > %s", content, path)
				}
			case "append":
				content := ""
				if v, ok := args["content"]; ok {
					content = fmt.Sprint(v)
				}
				if runtime.GOOS == "windows" {
					cmdStr = fmt.Sprintf("powershell -Command \"[System.IO.File]::AppendAllText('%s', '%s')\"", path, strings.ReplaceAll(content, "'", "''"))
				} else {
					cmdStr = fmt.Sprintf("printf %%s %q >> %s", content, path)
				}
			}
		case "shell":
			if v, ok := args["command"]; ok {
				cmdStr = fmt.Sprint(v)
			}
		case "docker":
			target := ""
			if v, ok := args["target"]; ok {
				target = fmt.Sprint(v)
			}
			extra := ""
			if v, ok := args["args"]; ok {
				extra = fmt.Sprint(v)
			}
			cmdStr = fmt.Sprintf("docker %s %s %s", action, extra, target)
		case "fix":
			switch action {
			case "docker_buildkit_missing":
				// Suggest removing --mount if buildx is not available
				cmdStr = "sed -i 's/--mount=[^ ]* //g' Dockerfile"
			}
		}
	}

	if cmdStr == "" {
		return "Error: Tool not implemented or unknown"
	}

	// Execute locally in workspace
	shell, shellFlag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, shellFlag = "cmd", "/c"
	}
	cmd := exec.CommandContext(a.ctx, shell, shellFlag, cmdStr)
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Command failed: %v\nOutput: %s", err, string(out))
	}

	return string(out)
}
