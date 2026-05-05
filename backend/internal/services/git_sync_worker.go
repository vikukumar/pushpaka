package services

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
	"github.com/vikukumar/pushpaka/queue"
)

// GitSyncWorker handles background git synchronization tasks
type GitSyncWorker struct {
	gitSyncService  *GitSyncService
	gitSyncRepo     *repositories.GitSyncRepository
	deploymentRepo  *repositories.DeploymentRepository
	projectRepo     *repositories.ProjectRepository
	commitRepo      *repositories.CommitRepository
	notificationSvc *NotificationService
	taskDispatcher  *TaskDispatcher
	rdb             *redis.Client
	name            string
	pollInterval    time.Duration
	projectsDir     string
	syncWorkers     int
	commitLimit     int
	logger          *zerolog.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	projectQueue    chan *models.Project
	queueStats      *queue.InProcess
}

// NewGitSyncWorker creates a new git sync worker
func NewGitSyncWorker(
	gitSyncService *GitSyncService,
	gitSyncRepo *repositories.GitSyncRepository,
	deploymentRepo *repositories.DeploymentRepository,
	projectRepo *repositories.ProjectRepository,
	commitRepo *repositories.CommitRepository,
	taskDispatcher *TaskDispatcher,
	notificationSvc *NotificationService,
	rdb *redis.Client,
	cfg *config.Config,
	logger *zerolog.Logger,
) *GitSyncWorker {
	return &GitSyncWorker{
		gitSyncService:  gitSyncService,
		gitSyncRepo:     gitSyncRepo,
		deploymentRepo:  deploymentRepo,
		projectRepo:     projectRepo,
		commitRepo:      commitRepo,
		taskDispatcher:  taskDispatcher,
		notificationSvc: notificationSvc,
		rdb:             rdb,
		name:            "git-sync-worker",
		pollInterval:    10 * time.Second,
		projectsDir:     cfg.ProjectsDir,
		syncWorkers:     cfg.SyncWorkers,
		commitLimit:     cfg.CommitLimit,
		logger:          logger,
		projectQueue:    make(chan *models.Project, 100),
	}
}

// Start begins the git sync worker
func (w *GitSyncWorker) Start(ctx context.Context, q *queue.InProcess) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.queueStats = q
	w.logger.Info().Int("workers", w.syncWorkers).Msg("git sync worker starting")

	// Start sync pool
	for i := 0; i < w.syncWorkers; i++ {
		go w.runSyncWorker(i)
	}

	go w.runPolling()
	go w.runApprovalCleanup()

	return nil
}

// runPolling runs the main polling loop for auto-sync
func (w *GitSyncWorker) runPolling() {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info().Msg("git sync polling stopped")
			return

		case <-ticker.C:
			w.dispatchProjects()
		}
	}
}

// runSyncWorker is a consumer from the projectQueue
func (w *GitSyncWorker) runSyncWorker(id int) {
	if w.queueStats != nil {
		w.queueStats.WorkerStarted("syncer")
		defer w.queueStats.WorkerStopped("syncer")
	}
	w.logger.Info().Int("worker_id", id).Str("role", "syncer").Msgf("sync worker [%d] started", id)
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
			if w.rdb != nil {
				// 1. Try Redis first
				res, err := w.rdb.BRPop(w.ctx, 1*time.Second, "pushpaka:tasks:sync").Result()
				if err == nil {
					taskID := res[1]
					w.processSyncTask(taskID)
					continue
				}
			}

			if w.queueStats != nil {
				// 2. Try In-Process queue
				select {
				case payload := <-w.queueStats.Chan("sync"):
					taskID := string(payload)
					w.queueStats.JobStarted("sync")
					w.processSyncTask(taskID)
					w.queueStats.JobFinished("sync")
				case p := <-w.projectQueue:
					// Polling detected an auto-sync project
					w.queueStats.JobStarted("sync")
					w.syncProject(p)
					w.queueStats.JobFinished("sync")
				case <-time.After(1 * time.Second):
					// No job, loop again
				}
			} else {
				// Standalone mode or minimal config
				select {
				case p := <-w.projectQueue:
					w.syncProject(p)
				case <-time.After(1 * time.Second):
				}
			}
		}
	}
}

func (w *GitSyncWorker) processSyncTask(taskID string) {
	task, err := w.taskDispatcher.taskRepo.Get(taskID)
	if err != nil {
		w.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to fetch task for processing")
		return
	}

	// Update task status to running
	now := time.Now().UTC()
	task.Status = models.TaskStatusRunning
	task.StartedAt = &now
	_ = w.taskDispatcher.taskRepo.Update(task)

	// Get project details
	p, err := w.projectRepo.FindByIDInternal(task.ProjectID)
	if err != nil {
		w.taskDispatcher.HandleTaskCompletion(task.ID, false, fmt.Sprintf("Project not found: %v", err))
		return
	}

	// 1. Sync/Clone
	// If CommitSHA is empty (initial sync), fetch latest from remote
	if task.CommitSHA == "" {
		info, err := w.gitSyncService.GetLatestCommitInfo(p)
		if err != nil {
			w.logger.Error().Err(err).Str("project_id", p.ID).Msg("failed to fetch latest commit info for initial sync")
			w.taskDispatcher.HandleTaskCompletion(task.ID, false, fmt.Sprintf("Failed to fetch latest commit info: %v", err))
			return
		}
		task.CommitSHA = info.SHA
		_ = w.taskDispatcher.taskRepo.Update(task)
	}

	sourcePath := os.ExpandEnv(w.projectsDir + "/" + p.ID + "/" + task.CommitSHA)
	err = w.gitSyncService.CloneTo(p.RepoURL, p.Branch, sourcePath)
	if err != nil {
		w.taskDispatcher.HandleTaskCompletion(task.ID, false, fmt.Sprintf("Git sync failed: %v", err))
		return
	}

	// 2. Ensure commit record exists
	// We might need more info (message, author) if it's the first sync
	latestInfo := &models.GitCommitInfo{
		SHA:       task.CommitSHA,
		Timestamp: time.Now().UTC(),
	}

	// Try to get full info if we just fetched it or if we can
	if info, err := w.gitSyncService.GetLatestCommitInfo(p); err == nil && info.SHA == task.CommitSHA {
		latestInfo = info
	}

	w.ensureCommitRecord(p, latestInfo, sourcePath)

	// 3. Complete task
	w.taskDispatcher.HandleTaskCompletion(task.ID, true, "")
}

// dispatchProjects fetches all projects and sends them to the queue
func (w *GitSyncWorker) dispatchProjects() {
	projects, err := w.projectRepo.FindAllByAutoSync()
	if err != nil {
		w.logger.Error().Err(err).Msg("failed to fetch auto-sync projects")
		return
	}

	for _, p := range projects {
		select {
		case w.projectQueue <- &p:
		default:
			w.logger.Warn().Str("project_id", p.ID).Msg("sync queue full, skipping project")
		}
	}
}

// syncProject handles the actual git operations for a single project
func (w *GitSyncWorker) syncProject(p *models.Project) {
	// 1. Check for latest commit info (ls-remote or similar)
	latest, err := w.gitSyncService.GetLatestCommitInfo(p)
	if err != nil {
		w.logger.Warn().Str("project_id", p.ID).Err(err).Msg("failed to check for updates")
		return
	}
	// 2. If new commit or message detected, create Sync task
	if latest != nil && (latest.SHA != p.LatestCommitSHA || latest.Message != p.LatestCommitMsg) {
		// Check if there's already a pending/running sync task for this project
		if w.taskDispatcher.taskRepo.Exists(p.ID, models.TaskTypeSync, latest.SHA) {
			// Already being synced, skip
			return
		}

		w.logger.Info().Str("project_id", p.ID).Str("sha", latest.SHA).Msg("new commit detected, emitting Sync task")

		_, err = w.taskDispatcher.CreateTask(p.ID, models.TaskTypeSync, latest.SHA)
		if err != nil {
			w.logger.Error().Err(err).Str("project_id", p.ID).Msg("failed to create sync task")
		}

		// Cleanup old commits
		w.cleanupOldCommits(p.ID)
	}
}

func (w *GitSyncWorker) cleanupOldCommits(projectID string) {
	oldCommits, err := w.commitRepo.FindOldCommits(projectID, w.commitLimit)
	if err != nil {
		return
	}

	for _, c := range oldCommits {
		// 1. Delete source files
		if c.SourcePath != "" {
			_ = os.RemoveAll(c.SourcePath)
		}
		// 2. Delete build files
		if c.BuildPath != "" {
			_ = os.RemoveAll(c.BuildPath)
		}
	}

	// 3. Delete from DB
	_ = w.commitRepo.DeleteOldCommits(projectID, w.commitLimit)
}

// runApprovalCleanup runs cleanup of stale approval requests
func (w *GitSyncWorker) runApprovalCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return

		case <-ticker.C:
			w.cleanupStalePendingApprovals()
		}
	}
}

// cleanupStalePendingApprovals removes approval requests that have timed out
func (w *GitSyncWorker) cleanupStalePendingApprovals() {
	// TODO: Implement cleanup logic
}

// HealthCheck returns worker health status
func (w *GitSyncWorker) HealthCheck() map[string]interface{} {
	return map[string]interface{}{
		"name":         w.name,
		"status":       "healthy",
		"running":      w.ctx != nil && w.ctx.Err() == nil,
		"sync_workers": w.syncWorkers,
		"queue_len":    len(w.projectQueue),
	}
}

// Stop stops the git sync worker
func (w *GitSyncWorker) Stop() error {
	w.logger.Info().Msg("stopping git sync worker")
	if w.cancel != nil {
		w.cancel()
	}
	return nil
}

func (w *GitSyncWorker) ensureCommitRecord(p *models.Project, latest *models.GitCommitInfo, sourcePath string) {
	// Check if already exists
	existing, err := w.commitRepo.FindBySHA(p.ID, latest.SHA)
	if err == nil && existing != nil {
		return
	}

	commit := &models.ProjectCommit{
		BaseModel: basemodel.BaseModel{
			ID:        uuid.New().String(),
			CreatedAt: time.Now().UTC(),
		},
		ProjectID:   p.ID,
		SHA:         latest.SHA,
		Message:     latest.Message,
		Author:      latest.Author,
		Branch:      p.Branch,
		Status:      models.CommitStatusSynced,
		SourcePath:  sourcePath,
		CommittedAt: latest.Timestamp,
	}
	_ = w.commitRepo.Create(commit)

	// Update project's latest commit info
	p.LatestCommitSHA = latest.SHA
	p.LatestCommitMsg = latest.Message
	p.LatestCommitAt = latest.Timestamp
	_ = w.projectRepo.Update(p)
}
