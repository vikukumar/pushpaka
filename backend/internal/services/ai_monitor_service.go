package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/models"
)

const (
	// StuckBuildThresholdMinutes is the time in minutes after which a deployment
	// stuck in "building" state is considered anomalous.
	StuckBuildThresholdMinutes = 30
)

type AIMonitorService struct {
	aiSvc        *AIService
	aiRepo       *repositories.AIConfigRepository
	deployRepo   *repositories.DeploymentRepository
	logRepo      *repositories.LogRepository
	pollInterval time.Duration
}

func NewAIMonitorService(
	aiSvc *AIService,
	aiRepo *repositories.AIConfigRepository,
	deployRepo *repositories.DeploymentRepository,
	logRepo *repositories.LogRepository,
) *AIMonitorService {
	return &AIMonitorService{
		aiSvc:        aiSvc,
		aiRepo:       aiRepo,
		deployRepo:   deployRepo,
		logRepo:      logRepo,
		pollInterval: 5 * time.Minute,
	}
}

func (s *AIMonitorService) Start(ctx context.Context) {
	log.Info().Msg("AI Monitoring Service started")
	// Run immediately on startup (don't wait for first tick)
	go s.runCheck(ctx)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("AI Monitoring Service stopping")
			return
		case <-ticker.C:
			s.runCheck(ctx)
		}
	}
}

func (s *AIMonitorService) runCheck(ctx context.Context) {
	// 1. Check for stuck builds (building > 30 minutes)
	s.checkStuckBuilds(ctx)

	// 2. Check per-user AI monitoring configs for failed deployments
	userIDs, err := s.aiRepo.ListUsersWithMonitoring()
	if err != nil {
		log.Error().Err(err).Msg("failed to list users for AI monitoring")
		return
	}

	for _, userID := range userIDs {
		s.checkUserDeployments(ctx, userID)
	}
}

// checkStuckBuilds finds any deployment that has been in "building" state for longer
// than StuckBuildThresholdMinutes and creates a warning alert.
func (s *AIMonitorService) checkStuckBuilds(ctx context.Context) {
	threshold := time.Now().UTC().Add(-time.Duration(StuckBuildThresholdMinutes) * time.Minute)
	stuckDeployments, err := s.deployRepo.FindStuckBuilding(threshold)
	if err != nil {
		log.Error().Err(err).Msg("monitoring: failed to query stuck builds")
		return
	}

	for _, d := range stuckDeployments {
		_ = ctx
		// Check if a "stuck" alert already exists
		exists, _ := s.aiRepo.AlertExistsForDeployment(d.ID)
		if exists {
			continue
		}

		elapsed := time.Since(d.StartedAt.UTC()).Round(time.Minute)
		log.Warn().
			Str("deployment_id", d.ID).
			Str("project_id", d.ProjectID).
			Dur("elapsed", elapsed).
			Msg("monitoring: stuck build detected")

		alert := &models.AIMonitorAlert{
			UserID:       d.UserID,
			DeploymentID: d.ID,
			Severity:     "warning",
			Title:        fmt.Sprintf("Build stuck for %s — possible infrastructure issue", elapsed),
			Message: fmt.Sprintf(
				"Deployment %s (project: %s, branch: %s) has been in 'building' state for %s. "+
					"This usually indicates a build command hanging, an out-of-memory condition, "+
					"or the worker process has crashed. Consider canceling and re-triggering the deployment.",
				d.ID[:8], d.ProjectID[:8], d.Branch, elapsed,
			),
			Resolved: false,
		}

		if err := s.aiRepo.CreateAlert(alert); err != nil {
			log.Error().Err(err).Str("deployment_id", d.ID).Msg("failed to create stuck-build alert")
		} else {
			log.Info().Str("alert_id", alert.ID).Str("deployment_id", d.ID).Msg("stuck-build alert created")
		}
	}
}

func (s *AIMonitorService) checkUserDeployments(ctx context.Context, userID string) {
	// Load user config
	userCfg, err := s.aiRepo.GetByUserID(userID)
	if err != nil || userCfg == nil || !userCfg.MonitoringEnabled {
		return
	}

	// Fetch recent failed deployments for this user
	deployments, err := s.deployRepo.ListFailedRecent(userID, 10)
	if err != nil {
		return
	}

	for _, d := range deployments {
		// Skip if alert already exists
		exists, _ := s.aiRepo.AlertExistsForDeployment(d.ID)
		if exists {
			continue
		}

		log.Info().Str("deployment_id", d.ID).Str("user_id", userID).Msg("monitoring: analyzing failed deployment")

		// Get logs
		logEntries, err := s.logRepo.FindByDeploymentID(d.ID)
		if err != nil || len(logEntries) == 0 {
			continue
		}

		var sb strings.Builder
		for _, l := range logEntries {
			sb.WriteString(l.Message + "\n")
		}

		// Load RAG knowledge base for this user
		ragDocs, _ := s.aiRepo.ListRAG(userID)

		// Run AI analysis
		analysis, err := s.aiSvc.AnalyzeLogsWithConfig(userCfg, ragDocs, sb.String())
		if err != nil {
			log.Warn().Err(err).Str("deployment_id", d.ID).Msg("monitoring: AI analysis failed")
			continue
		}

		// Determine severity from analysis
		severity := "error"
		analysisLower := strings.ToLower(analysis)
		if strings.Contains(analysisLower, "critical") || strings.Contains(analysisLower, "out of memory") {
			severity = "critical"
		}

		// Create Alert
		alert := &models.AIMonitorAlert{
			UserID:       userID,
			DeploymentID: d.ID,
			Severity:     severity,
			Title:        fmt.Sprintf("Deployment failure: %s → %s", d.Branch, d.CommitSHA[:8]),
			Message:      analysis,
			Resolved:     false,
		}

		if err := s.aiRepo.CreateAlert(alert); err != nil {
			log.Error().Err(err).Msg("failed to create AI monitor alert")
		} else {
			log.Info().Str("alert_id", alert.ID).Str("severity", severity).Msg("AI monitor alert created")
		}
	}
}
