package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/internal/middleware"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/internal/services"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type AIHandler struct {
	aiSvc        *services.AIService
	logRepo      *repositories.LogRepository
	deployRepo   *repositories.DeploymentRepository
	aiConfigRepo *repositories.AIConfigRepository
	cfg          *config.Config
	aiExecutor   *services.AIToolsExecutor // New field for agent tool execution
}

func NewAIHandler(aiSvc *services.AIService, logRepo *repositories.LogRepository, deployRepo *repositories.DeploymentRepository, aiConfigRepo *repositories.AIConfigRepository, cfg *config.Config, aiExecutor *services.AIToolsExecutor) *AIHandler {
	return &AIHandler{aiSvc: aiSvc, logRepo: logRepo, deployRepo: deployRepo, aiConfigRepo: aiConfigRepo, cfg: cfg, aiExecutor: aiExecutor}
}

// resolveUserConfig loads the user's AI config; returns nil when not found (falls back to global).
func (h *AIHandler) resolveUserConfig(userID string) *models.AIConfig {
	cfg, _ := h.aiConfigRepo.GetByUserID(userID)
	return cfg
}

// checkRateLimit returns (allowed, errMsg). When the user has their own API key the
// limit is skipped — they pay for their own usage. Global key usage is rate-limited.
func (h *AIHandler) checkRateLimit(userID string, userCfg *models.AIConfig) (bool, string) {
	// User has their own key — no platform rate limit.
	if userCfg != nil && userCfg.APIKey != "" {
		return true, ""
	}
	// No global daily limit configured (0 = unlimited).
	if h.cfg.AIRateLimitPerUserPerDay == 0 {
		return true, ""
	}
	usage, err := h.aiConfigRepo.GetOrCreateTodayUsage(userID)
	if err != nil {
		return true, "" // best-effort: allow on DB error
	}
	if usage.Calls >= h.cfg.AIRateLimitPerUserPerDay {
		return false, fmt.Sprintf(
			"global AI rate limit reached (%d calls/day). Add your own API key in Settings → AI to remove this limit.",
			h.cfg.AIRateLimitPerUserPerDay)
	}
	return true, ""
}

// AnalyzeLogs retrieves deployment logs and sends them to the AI for analysis.
// POST /api/v1/deployments/:id/analyze
func (h *AIHandler) AnalyzeLogs(c *gin.Context) {
	userID := middleware.GetUserID(c)
	userCfg := h.resolveUserConfig(userID)

	if !h.aiSvc.AvailableWithUserConfig(userCfg) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "AI not configured. Add your API key in Settings → AI, or ask your admin to set AI_API_KEY.",
		})
		return
	}

	// Rate-limit global key usage.
	if ok, msg := h.checkRateLimit(userID, userCfg); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": msg})
		return
	}

	deploymentID := c.Param("id")
	deployment, err := h.deployRepo.FindByID(deploymentID)
	if err != nil || deployment.UserID != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
		return
	}

	logEntries, err := h.logRepo.FindByDeploymentID(deploymentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve logs"})
		return
	}
	if len(logEntries) == 0 {
		c.JSON(http.StatusOK, gin.H{"analysis": "No logs found for this deployment."})
		return
	}

	var sb strings.Builder
	for _, entry := range logEntries {
		sb.WriteString(entry.Message)
		sb.WriteByte('\n')
	}

	// Load user's RAG knowledge base for additional context.
	ragDocs, _ := h.aiConfigRepo.ListRAG(userID)

	analysis, err := h.aiSvc.AnalyzeLogsWithConfig(userCfg, ragDocs, sb.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI analysis failed: " + err.Error()})
		return
	}

	// Track usage (best-effort, only for global key consumers).
	if userCfg == nil || userCfg.APIKey == "" {
		_ = h.aiConfigRepo.IncrementTodayUsage(userID, 1)
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis})
}

// Chat handles free-form AI assistant questions, optionally with deployment context.
// POST /api/v1/ai/chat
func (h *AIHandler) Chat(c *gin.Context) {
	userID := middleware.GetUserID(c)
	userCfg := h.resolveUserConfig(userID)

	if !h.aiSvc.AvailableWithUserConfig(userCfg) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "AI not configured. Add your API key in Settings → AI, or ask your admin to set AI_API_KEY.",
		})
		return
	}

	// Rate-limit global key usage.
	if ok, msg := h.checkRateLimit(userID, userCfg); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": msg})
		return
	}

	var req struct {
		Message      string `json:"message" binding:"required"`
		DeploymentID string `json:"deployment_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Default system prompt; overridden by user's saved system_prompt if set.
	systemPrompt := `You are Pushpaka Assistant, an expert AI for the Pushpaka self-hosted cloud deployment platform.
You help users with:
- Debugging deployment failures and build errors
- Docker container issues and resource limits
- Git repository configuration and webhook setup
- Environment variable management
- CI/CD pipeline optimization
- Container networking and domain routing
- Performance monitoring and log analysis
Be concise, technical, and actionable. Format responses with markdown when helpful.`

	// User's custom system prompt overrides the default.
	if userCfg != nil && userCfg.SystemPrompt != "" {
		systemPrompt = userCfg.SystemPrompt
	}

	userMsg := req.Message

	// Inject deployment logs as context when a deployment ID is given.
	if req.DeploymentID != "" {
		deployment, err := h.deployRepo.FindByID(req.DeploymentID)
		if err == nil && deployment.UserID == userID {
			logEntries, err := h.logRepo.FindByDeploymentID(req.DeploymentID)
			if err == nil && len(logEntries) > 0 {
				var sb strings.Builder
				start := 0
				if len(logEntries) > 80 {
					start = len(logEntries) - 80
				}
				for _, entry := range logEntries[start:] {
					sb.WriteString(entry.Message)
					sb.WriteByte('\n')
				}
				userMsg = "Deployment context (status: " + string(deployment.Status) + ", branch: " + deployment.Branch +
					", commit: " + deployment.CommitSHA + "):\n```\n" + sb.String() + "```\n\nUser question: " + req.Message
			}
		}
	}

	// Load user's RAG knowledge base for additional context.
	ragDocs, _ := h.aiConfigRepo.ListRAG(userID)

	reply, err := h.aiSvc.AskWithConfig(userCfg, ragDocs, systemPrompt, userMsg)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI chat failed: " + err.Error()})
		return
	}

	// Track global key usage.
	if userCfg == nil || userCfg.APIKey == "" {
		_ = h.aiConfigRepo.IncrementTodayUsage(userID, 1)
	}

	c.JSON(http.StatusOK, gin.H{"reply": reply})
}

// AgentChat handles autonomous/agentic conversations where the AI can summon tools.
// POST /api/v1/ai/agent
func (h *AIHandler) AgentChat(c *gin.Context) {
	userID := middleware.GetUserID(c)
	userCfg := h.resolveUserConfig(userID)

	if !h.aiSvc.AvailableWithUserConfig(userCfg) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "AI not configured. Add your API key in Settings → AI.",
		})
		return
	}

	if ok, msg := h.checkRateLimit(userID, userCfg); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": msg})
		return
	}

	var req services.AIAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ragDocs, _ := h.aiConfigRepo.ListRAG(userID)

	resp, err := h.aiSvc.ChatWithTools(c.Request.Context(), userID, userCfg, ragDocs, req.Messages, h.aiExecutor, req.Autonomous)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Agent chat failed: " + err.Error()})
		return
	}

	if userCfg == nil || userCfg.APIKey == "" {
		_ = h.aiConfigRepo.IncrementTodayUsage(userID, 1)
	}

	c.JSON(http.StatusOK, resp)
}

// AgentExecute handles the execution of an approved tool call, resuming the chat.
// POST /api/v1/ai/agent/execute
func (h *AIHandler) AgentExecute(c *gin.Context) {
	userID := middleware.GetUserID(c)
	userCfg := h.resolveUserConfig(userID)

	var req struct {
		services.AIAgentRequest
		ApprovedToolCall models.AIToolCall `json:"approved_tool_call"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 1. Execute the approved tool
	exec := h.aiExecutor
	if userCfg != nil {
		ragDocs, _ := h.aiConfigRepo.ListRAG(userID)
		exec = h.aiExecutor.WithUserConfig(userCfg, ragDocs)
	}
	resultStr, err := exec.ExecuteToolCall(c.Request.Context(), userID, req.ApprovedToolCall)
	if err != nil {
		resultStr = fmt.Sprintf("Error executing tool: %v", err)
	}

	// 2. Append the tool's result to the message history
	toolMsg := models.AIAgentMessage{
		Role:       "tool",
		Content:    resultStr,
		ToolCallID: req.ApprovedToolCall.ID,
	}
	req.Messages = append(req.Messages, toolMsg)

	// 3. Resume the chat
	ragDocs, _ := h.aiConfigRepo.ListRAG(userID)
	resp, err := h.aiSvc.ChatWithTools(c.Request.Context(), userID, userCfg, ragDocs, req.Messages, h.aiExecutor, req.Autonomous)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Agent chat failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetAIConfig returns the AI provider config for the authenticated user.
// GET /api/v1/ai/config
func (h *AIHandler) GetAIConfig(c *gin.Context) {
	userID := middleware.GetUserID(c)
	cfg, err := h.aiConfigRepo.GetByUserID(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load AI config"})
		return
	}
	if cfg == nil {
		cfg = &models.AIConfig{UserID: userID, Provider: "openai"}
	}
	// Never expose the raw key — show only a masked version
	if cfg.APIKey != "" {
		visible := cfg.APIKey
		if len(visible) > 8 {
			visible = visible[:4] + strings.Repeat("*", len(visible)-8) + visible[len(visible)-4:]
		} else {
			visible = strings.Repeat("*", len(visible))
		}
		cfg.APIKeyMasked = visible
	}
	c.JSON(http.StatusOK, cfg)
}

// SaveAIConfig upserts the AI provider config for the authenticated user.
// PUT /api/v1/ai/config
func (h *AIHandler) SaveAIConfig(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req struct {
		Provider           string `json:"provider"`
		APIKey             string `json:"api_key"`
		Model              string `json:"model"`
		BaseURL            string `json:"base_url"`
		SystemPrompt       string `json:"system_prompt"`
		MonitoringEnabled  bool   `json:"monitoring_enabled"`
		MonitoringInterval int    `json:"monitoring_interval"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cfg := &models.AIConfig{
		UserID:             userID,
		Provider:           req.Provider,
		APIKey:             req.APIKey,
		Model:              req.Model,
		BaseURL:            req.BaseURL,
		SystemPrompt:       req.SystemPrompt,
		MonitoringEnabled:  req.MonitoringEnabled,
		MonitoringInterval: req.MonitoringInterval,
	}
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.MonitoringInterval == 0 {
		cfg.MonitoringInterval = 300
	}

	if err := h.aiConfigRepo.Upsert(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save AI config"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "AI settings saved"})
}

// ─── RAG Knowledge Base ───────────────────────────────────────────────────────

// ListRAG returns all RAG documents for the authenticated user.
// GET /api/v1/ai/rag
func (h *AIHandler) ListRAG(c *gin.Context) {
	userID := middleware.GetUserID(c)
	docs, err := h.aiConfigRepo.ListRAG(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list documents"})
		return
	}
	c.JSON(http.StatusOK, docs)
}

// CreateRAG adds a new RAG document.
// POST /api/v1/ai/rag
func (h *AIHandler) CreateRAG(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req struct {
		Title   string `json:"title" binding:"required"`
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	doc := &models.RAGDocument{
		UserID:  userID,
		Title:   req.Title,
		Content: req.Content,
	}
	if err := h.aiConfigRepo.CreateRAG(doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create document"})
		return
	}
	c.JSON(http.StatusCreated, doc)
}

// DeleteRAG removes a RAG document.
// DELETE /api/v1/ai/rag/:id
func (h *AIHandler) DeleteRAG(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")
	if err := h.aiConfigRepo.DeleteRAG(id, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "document deleted"})
}

// ─── AI Monitor Alerts ────────────────────────────────────────────────────────

// ListAlerts returns AI monitoring alerts for the authenticated user.
// GET /api/v1/ai/alerts
func (h *AIHandler) ListAlerts(c *gin.Context) {
	userID := middleware.GetUserID(c)
	onlyUnresolved := c.Query("unresolved") == "true"
	alerts, err := h.aiConfigRepo.ListAlerts(userID, 200, onlyUnresolved)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list alerts"})
		return
	}
	c.JSON(http.StatusOK, alerts)
}

// ResolveAlert marks an alert as resolved.
// PUT /api/v1/ai/alerts/:id/resolve
func (h *AIHandler) ResolveAlert(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")
	if err := h.aiConfigRepo.ResolveAlert(id, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "alert resolved"})
}

// GetUsage returns today's AI usage for the authenticated user plus their effective rate limit.
// GET /api/v1/ai/usage
func (h *AIHandler) GetUsage(c *gin.Context) {
	userID := middleware.GetUserID(c)
	userCfg := h.resolveUserConfig(userID)
	hasOwnKey := userCfg != nil && userCfg.APIKey != ""
	usage, _ := h.aiConfigRepo.GetOrCreateTodayUsage(userID)
	calls := 0
	if usage != nil {
		calls = usage.Calls
	}
	c.JSON(http.StatusOK, gin.H{
		"calls_today": calls,
		"limit":       h.cfg.AIRateLimitPerUserPerDay,
		"has_own_key": hasOwnKey,
		"unlimited":   hasOwnKey || h.cfg.AIRateLimitPerUserPerDay == 0,
	})
}
