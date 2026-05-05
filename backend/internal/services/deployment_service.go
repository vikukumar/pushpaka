package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

const deployJobQueue = "pushpaka:deploy:queue"

var ErrDeploymentNotFound = errors.New("deployment not found")
var ErrQueueUnavailable = errors.New("deployment queue unavailable")

// DeploymentJobQueue captures the only queue operation the service needs.
// Using an interface here keeps services decoupled from the concrete queue package.
type DeploymentJobQueue interface {
	Push(role string, payload []byte) error
}

type DeploymentService struct {
	deploymentRepo *repositories.DeploymentRepository
	projectRepo    *repositories.ProjectRepository
	envRepo        *repositories.EnvVarRepository
	domainRepo     *repositories.DomainRepository
	rdb            *redis.Client
	inQueue        DeploymentJobQueue // non-nil in dev mode (no Redis)
	baseURL        string
	// lastSyncCheck tracks the last time we checked a project for auto-sync
	lastSyncCheck  map[string]time.Time
	syncMu         sync.Mutex
	taskDispatcher *TaskDispatcher
}

func NewDeploymentService(
	deploymentRepo *repositories.DeploymentRepository,
	projectRepo *repositories.ProjectRepository,
	envRepo *repositories.EnvVarRepository,
	domainRepo *repositories.DomainRepository,
	rdb *redis.Client,
	inQueue DeploymentJobQueue,
	taskDispatcher *TaskDispatcher,
	baseURL string,
) *DeploymentService {
	svc := &DeploymentService{
		deploymentRepo: deploymentRepo,
		projectRepo:    projectRepo,
		envRepo:        envRepo,
		domainRepo:     domainRepo,
		rdb:            rdb,
		inQueue:        inQueue,
		baseURL:        baseURL,
		lastSyncCheck:  make(map[string]time.Time),
		taskDispatcher: taskDispatcher,
	}
	// The in-process queue is ephemeral: jobs do not survive process restarts.
	// Any deployment left in "queued" state from a previous run has no
	// corresponding job in the current queue, so fail them immediately.
	// When Redis IS configured the external worker handles its own queue.
	if rdb == nil {
		_ = deploymentRepo.FailStaleQueued(
			"Deployment cancelled: process was restarted before this job was processed. Trigger a new deployment.",
		)
	}
	return svc
}

func (s *DeploymentService) Trigger(userID string, req *models.DeployRequest) (*models.Deployment, error) {
	project, err := s.projectRepo.FindByID(req.ProjectID, userID)
	if err != nil {
		return nil, ErrProjectNotFound
	}

	branch := req.Branch
	if branch == "" {
		branch = project.Branch
	}

	now := time.Now().UTC()
	imageTag := fmt.Sprintf("pushpaka_vahan/%s:%s", project.ID[:8], uuid.New().String()[:8])

	// Determine the public URL for this deployment:
	//   - If the project has a verified custom domain -> https://<domain>
	//   - Otherwise -> <baseURL>/app/<projectID>
	deployURL := s.baseURL + "/app/" + project.ID
	if strings.Contains(s.baseURL, "localhost") || strings.Contains(s.baseURL, "127.0.0.1") {
		// For local development, always force http as we don't support local TLS yet.
		// This prevents ERR_SSL_PROTOCOL_ERROR if BASE_URL was set to https by mistake.
		if strings.HasPrefix(deployURL, "https://") {
			deployURL = "http://" + strings.TrimPrefix(deployURL, "https://")
		}
	}
	if domains, err := s.domainRepo.FindByProjectID(project.ID); err == nil {
		for _, d := range domains {
			if d.Verified {
				scheme := "http"
				if d.SSLEnabled {
					scheme = "https"
				}
				deployURL = scheme + "://" + d.Domain
				break
			}
		}
	}

	d := &models.Deployment{
		BaseModel: basemodel.BaseModel{
			ID:        uuid.New().String(),
			CreatedAt: now,
			UpdatedAt: now,
		},
		ProjectID: project.ID,
		UserID:    userID,
		CommitSHA: req.CommitSHA,
		CommitMsg: req.CommitMsg,
		Branch:    branch,
		Status:    "queued",
		ImageTag:  imageTag,
		URL:       deployURL,
	}

	if err := s.deploymentRepo.Create(d); err != nil {
		return nil, fmt.Errorf("creating deployment record: %w", err)
	}

	// Auto-promote: if this is the first deployment for the project or ShouldPromote is set,
	// mark it as default immediately (will be confirmed healthy by the worker).
	if req.ShouldPromote {
		_ = s.deploymentRepo.ClearDefault(project.ID)
		// We'll set the actual flag to true once the deployment succeeds (worker will call the API).
		// For now, store it in the job so the worker knows to trigger promotion.
	}

	// If no queue is available at all, mark the deployment failed immediately
	// so the UI shows a clear error instead of spinning forever.
	if s.rdb == nil && s.inQueue == nil {
		failedAt := time.Now().UTC()
		d.Status = "failed"
		d.ErrorMsg = "Deployment worker unavailable: start Pushpaka with -dev for " +
			"the embedded worker, or configure REDIS_URL for a production worker."
		d.FinishedAt = &failedAt
		d.UpdatedAt = failedAt
		_ = s.deploymentRepo.Update(d)
		return d, nil
	}

	// Load env vars
	envVars, err := s.envRepo.FindMapByProjectID(project.ID)
	if err != nil {
		envVars = map[string]string{}
	}

	// Find an available host port for the new deployment (Zero-Downtime)
	externalPort := 0
	for i := 0; i < 5; i++ {
		if addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0"); err == nil {
			if l, err := net.ListenTCP("tcp", addr); err == nil {
				externalPort = l.Addr().(*net.TCPAddr).Port
				l.Close()
				if externalPort > 0 {
					break // Successfully found a port
				}
			}
		}
		time.Sleep(10 * time.Millisecond) // Small backoff before retry
	}
	if externalPort == 0 {
		return nil, fmt.Errorf("failed to allocate a free port for deployment after multiple attempts")
	}
	d.ExternalPort = externalPort

	// Fallback port if none specified in project
	jobPort := project.Port
	if jobPort <= 0 {
		jobPort = 3000
		log.Info().Str("project_id", project.ID).Msg("No port specified in project, defaulting to 3000 for deployment")
	}

	job := &models.DeploymentJob{
		DeploymentID:    d.ID,
		ProjectID:       project.ID,
		UserID:          userID,
		RepoURL:         project.RepoURL,
		Branch:          branch,
		CommitSHA:       req.CommitSHA,
		CommitMsg:       req.CommitMsg,
		InstallCommand:  project.InstallCommand,
		BuildCommand:    project.BuildCommand,
		StartCommand:    project.StartCommand,
		RunDir:          project.RunDir,
		Port:            jobPort,
		ExternalPort:    externalPort,
		EnvVars:         envVars,
		ImageTag:        imageTag,
		GitToken:        project.GitToken,
		CPULimit:        project.CPULimit,
		MemoryLimit:     project.MemoryLimit,
		RestartPolicy:   project.RestartPolicy,
		NotificationURL: s.baseURL + "/api/v1/internal/notify",
		ShouldPromote:   req.ShouldPromote,
		IsBuildOnly:     req.IsBuildOnly,
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return nil, fmt.Errorf("marshaling job: %w", err)
	}

	if s.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.rdb.LPush(ctx, deployJobQueue, payload).Err(); err != nil {
			return nil, fmt.Errorf("queuing job: %w", err)
		}
	} else {
		// In-process queue: faster than Redis, no network round-trip.
		if err := s.inQueue.Push("deploy", payload); err != nil {
			return nil, fmt.Errorf("in-process queue: %w", err)
		}
	}

	return d, nil
}

func (s *DeploymentService) List(userID string, limit, offset int) ([]models.Deployment, error) {
	return s.deploymentRepo.FindByUserID(userID, limit, offset)
}

func (s *DeploymentService) ListByProject(projectID, userID string, limit, offset int) ([]models.Deployment, error) {
	// Verify project ownership
	if _, err := s.projectRepo.FindByID(projectID, userID); err != nil {
		return nil, ErrProjectNotFound
	}
	return s.deploymentRepo.FindByProjectID(projectID, limit, offset)
}

func (s *DeploymentService) Get(id string) (*models.Deployment, error) {
	d, err := s.deploymentRepo.FindByID(id)
	if err != nil {
		return nil, ErrDeploymentNotFound
	}
	return d, nil
}

func (s *DeploymentService) GetStats() (map[string]int64, error) {
	return s.deploymentRepo.GetStats()
}

func (s *DeploymentService) Delete(id, userID string) error {
	d, err := s.deploymentRepo.FindByID(id)
	if err != nil {
		return ErrDeploymentNotFound
	}
	if d.UserID != userID {
		return errors.New("forbidden")
	}
	return s.deploymentRepo.Delete(id)
}

func (s *DeploymentService) Rollback(deploymentID, userID string) (*models.Deployment, error) {
	prev, err := s.deploymentRepo.FindByID(deploymentID)
	if err != nil {
		return nil, ErrDeploymentNotFound
	}

	req := &models.DeployRequest{
		ProjectID: prev.ProjectID,
		Branch:    prev.Branch,
		CommitSHA: prev.CommitSHA,
	}
	return s.Trigger(userID, req)
}

// promoteToDefault atomically makes newDeployID the live/default deployment for the project.
// It clears is_default on all other deployments and updates the project's main_deploy_id.
func (s *DeploymentService) promoteToDefault(projectID, newDeployID string) error {
	// Clear is_default on all deployments for the project
	if err := s.deploymentRepo.ClearDefault(projectID); err != nil {
		return fmt.Errorf("clearing default: %w", err)
	}
	// Mark the new one as default
	if err := s.deploymentRepo.SetDefault(newDeployID); err != nil {
		return fmt.Errorf("setting default: %w", err)
	}
	// Update the project's main_deploy_id pointer
	if err := s.projectRepo.SetMainDeployID(projectID, newDeployID); err != nil {
		log.Warn().Err(err).Str("project_id", projectID).Msg("failed to update project main_deploy_id")
	}
	log.Info().Str("project_id", projectID).Str("deployment_id", newDeployID).Msg("deployment promoted to default/live")
	return nil
}

// PromoteDeployment makes the given deployment the live/default for the project.
func (s *DeploymentService) PromoteDeployment(userID, deploymentID string) (*models.Deployment, error) {
	d, err := s.deploymentRepo.FindByID(deploymentID)
	if err != nil {
		return nil, ErrDeploymentNotFound
	}
	if d.UserID != userID {
		return nil, errors.New("forbidden")
	}
	if d.Status != models.DeploymentRunning {
		return nil, errors.New("only running deployments can be promoted to default")
	}
	if err := s.promoteToDefault(d.ProjectID, d.ID); err != nil {
		return nil, err
	}
	// Re-fetch to return updated record
	return s.deploymentRepo.FindByID(deploymentID)
}

// RecoverRunningDeployments finds all deployments currently in "running" state
// and pushes them back to the worker queue with the IsRecovery flag set.
// This ensures that after a server restart, the worker reconciles and restarts
// any missing containers or processes.
func (s *DeploymentService) RecoverRunningDeployments(ctx context.Context) error {
	// One-time fix for local URLs that might be stuck on https from old .env
	if strings.Contains(s.baseURL, "localhost") || strings.Contains(s.baseURL, "127.0.0.1") {
		_ = s.deploymentRepo.FixLocalProtocols()
	}

	running, err := s.deploymentRepo.FindAllRunning()
	if err != nil {
		return fmt.Errorf("finding running deployments: %w", err)
	}

	if len(running) == 0 {
		return nil
	}

	log.Info().Int("count", len(running)).Msg("recovering running deployments after restart")

	for _, d := range running {
		project, err := s.projectRepo.FindByID(d.ProjectID, d.UserID)
		if err != nil {
			log.Error().Err(err).Str("deployment_id", d.ID).Msg("failed to find project for recovery")
			continue
		}

		envVars, _ := s.envRepo.FindMapByProjectID(project.ID)

		job := &models.DeploymentJob{
			DeploymentID:    d.ID,
			ProjectID:       project.ID,
			UserID:          d.UserID,
			WorkerID:        d.WorkerID,
			RepoURL:         project.RepoURL,
			Branch:          d.Branch,
			CommitSHA:       d.CommitSHA,
			InstallCommand:  project.InstallCommand,
			BuildCommand:    project.BuildCommand,
			StartCommand:    project.StartCommand,
			RunDir:          project.RunDir,
			Port:            project.Port,
			EnvVars:         envVars,
			ImageTag:        d.ImageTag,
			GitToken:        project.GitToken,
			CPULimit:        project.CPULimit,
			MemoryLimit:     project.MemoryLimit,
			RestartPolicy:   project.RestartPolicy,
			NotificationURL: s.baseURL + "/api/v1/internal/notify",
			IsRecovery:      true,
		}

		payload, err := json.Marshal(job)
		if err != nil {
			log.Error().Err(err).Str("deployment_id", d.ID).Msg("marshaling recovery job")
			continue
		}

		if s.rdb != nil {
			if err := s.rdb.LPush(ctx, deployJobQueue, payload).Err(); err != nil {
				log.Error().Err(err).Str("deployment_id", d.ID).Msg("queuing recovery job to redis")
			}
		} else if s.inQueue != nil {
			if err := s.inQueue.Push("deploy", payload); err != nil {
				log.Error().Err(err).Str("deployment_id", d.ID).Msg("queuing recovery job to in-process queue")
			}
		}
	}

	return nil
}

// Shutdown gracefully stops the deployment service and marks running deployments for recovery.
func (s *DeploymentService) Shutdown() {
	log.Info().Msg("shutting down deployment service and stopping all running deployments")

	running, err := s.deploymentRepo.FindAllRunning()
	if err != nil {
		log.Error().Err(err).Msg("failed to find running deployments during shutdown")
		return
	}

	for _, d := range running {
		log.Info().Str("deployment_id", d.ID).Str("project_id", d.ProjectID).Msg("marking deployment for recovery")
		// Update project status to 'stopped' to track it was running before shutdown
		_ = s.projectRepo.UpdateStatus(d.ProjectID, "stopped")
	}
}

// StartAutoSyncLoop starts a background goroutine that periodically checks for new commits.
func (s *DeploymentService) StartAutoSyncLoop(ctx context.Context) {
	log.Info().Msg("starting background auto-sync loop (10s ticker)")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAutoSync()
		}
	}
}

func (s *DeploymentService) checkAutoSync() {
	projects, err := s.projectRepo.FindAllByAutoSync()
	if err != nil {
		log.Error().Err(err).Msg("auto-sync: failed to fetch projects")
		return
	}

	for _, p := range projects {
		if p.SyncIntervalSecs <= 0 {
			continue
		}

		s.syncMu.Lock()
		lastCheck, ok := s.lastSyncCheck[p.ID]
		s.syncMu.Unlock()

		if ok && time.Since(lastCheck) < time.Duration(p.SyncIntervalSecs)*time.Second {
			continue
		}

		// Update last check time immediately to prevent concurrent triggers
		s.syncMu.Lock()
		s.lastSyncCheck[p.ID] = time.Now()
		s.syncMu.Unlock()

		log.Debug().Str("project_id", p.ID).Msg("auto-sync: checking for updates")
		// Use a background context so a slow sync doesn't block the loop
		go func(proj models.Project) {
			_, _, err := s.SyncRepo(proj.UserID, proj.ID)
			if err != nil && err.Error() != "already up to date" {
				log.Error().Err(err).Str("project_id", proj.ID).Msg("auto-sync: sync failed")
			}
		}(p)
	}
}

// SyncRepo checks the remote repository for the latest commit on the project's branch.
// If a new commit is found (different from the last deployment), it triggers a new deployment.
// On success, the new deployment is automatically promoted to default/live.
func (s *DeploymentService) SyncRepo(userID, projectID string) (*models.Deployment, *models.ProjectTask, error) {
	project, err := s.projectRepo.FindByID(projectID, userID)
	if err != nil {
		return nil, nil, ErrProjectNotFound
	}

	// 1. Get the latest SHA from remote
	cloneURL := project.RepoURL
	if project.GitToken != "" {
		if u, err := url.Parse(cloneURL); err == nil && (u.Scheme == "https" || u.Scheme == "http") {
			u.User = url.User(project.GitToken)
			cloneURL = u.String()
		}
	}

	cmd := exec.Command("git", "ls-remote", cloneURL, project.Branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := string(output)
		if project.GitToken != "" {
			errMsg = strings.ReplaceAll(errMsg, project.GitToken, "***")
		}
		return nil, nil, fmt.Errorf("git ls-remote failed: %s", errMsg)
	}

	parts := strings.Fields(string(output))
	if len(parts) < 1 {
		return nil, nil, fmt.Errorf("could not determine remote SHA for branch %s", project.Branch)
	}
	latestSHA := parts[0]

	// 2. Try to get the commit message
	// If we have a local clone, we can fetch and get the message.
	// Otherwise, we'll try to get it during the build phase or leave it empty for now.
	latestMsg := "Sync triggered (fetching commit details...)"
	if project.GitClonePath != "" {
		// Attempt to fetch and get message if local dir exists
		fetchCmd := exec.Command("git", "-C", project.GitClonePath, "fetch", "origin", project.Branch)
		_ = fetchCmd.Run()
		msgCmd := exec.Command("git", "-C", project.GitClonePath, "log", "-1", "--format=%B", latestSHA)
		if msgOut, err := msgCmd.Output(); err == nil {
			latestMsg = strings.TrimSpace(string(msgOut))
		}
	}

	// Update project metadata
	project.LatestCommitSHA = latestSHA
	project.LatestCommitMsg = latestMsg
	project.LatestCommitAt = time.Now().UTC()
	_ = s.projectRepo.Update(project)

	// Check if we already have a deployment for this SHA
	latest, err := s.deploymentRepo.FindLatestByProjectID(projectID)
	// If the local SHA is the same as the remote SHA, and there's no task dispatcher,
	// then we consider it "already up to date" and skip.
	// If a task dispatcher is present, we allow re-runs even if SHAs are the same.
	if err == nil && latest != nil && latest.CommitSHA == latestSHA && s.taskDispatcher == nil {
		log.Debug().Str("project_id", projectID).Str("sha", latestSHA).Msg("Sync skipped: already up to date")
		return nil, nil, errors.New("already up to date")
	}

	// If taskDispatcher is available, use it for the full automated flow
	if s.taskDispatcher != nil {
		task, err := s.taskDispatcher.CreateTask(projectID, models.TaskTypeSync, latestSHA)
		return nil, task, err
	}

	// Fallback to old direct trigger logic if No task dispatcher
	req := &models.DeployRequest{
		ProjectID:     projectID,
		Branch:        project.Branch,
		CommitSHA:     latestSHA,
		CommitMsg:     latestMsg,
		ShouldPromote: true, // Auto-promote build artifacts on success
		IsBuildOnly:   true, // Just build and store artifacts, do not start process
	}
	d, err := s.Trigger(userID, req)
	return d, nil, err
}

// RestartDeployment triggers a new deployment for the same version as the given deployment ID.
// The new deployment will be automatically promoted to default/live on success.
func (s *DeploymentService) RestartDeployment(userID, deploymentID string) (*models.Deployment, error) {
	d, err := s.deploymentRepo.FindByID(deploymentID)
	if err != nil {
		return nil, ErrDeploymentNotFound
	}
	if d.UserID != userID {
		return nil, errors.New("forbidden")
	}

	req := &models.DeployRequest{
		ProjectID:     d.ProjectID,
		Branch:        d.Branch,
		CommitSHA:     d.CommitSHA,
		ShouldPromote: d.IsDefault, // Promote if the restarted one was the default
	}
	return s.Trigger(userID, req)
}
