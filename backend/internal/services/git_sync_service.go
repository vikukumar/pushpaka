package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type GitSyncService struct {
	gitSyncRepo *repositories.GitSyncRepository
	projectRepo *repositories.ProjectRepository
	deployRepo  *repositories.DeploymentRepository
	projectsDir string
	tempDir     string
}

func NewGitSyncService(
	gitSyncRepo *repositories.GitSyncRepository,
	projectRepo *repositories.ProjectRepository,
	deployRepo *repositories.DeploymentRepository,
	projectsDir string,
) *GitSyncService {
	return &GitSyncService{
		gitSyncRepo: gitSyncRepo,
		projectRepo: projectRepo,
		deployRepo:  deployRepo,
		projectsDir: projectsDir,
		tempDir:     os.TempDir(),
	}
}

// InitializeSyncTracking initializes git sync tracking for a new deployment
func (s *GitSyncService) InitializeSyncTracking(deployment *models.Deployment, project *models.Project) (*models.GitSyncTrack, error) {
	if project.RepoURL == "" {
		return nil, fmt.Errorf("project does not have a git repository configured")
	}

	now := time.Now().UTC()
	track := &models.GitSyncTrack{
		BaseModel: basemodel.BaseModel{
			ID:        uuid.New().String(),
			CreatedAt: now,
			UpdatedAt: now,
		},
		DeploymentID:     deployment.ID,
		ProjectID:        project.ID,
		Repository:       project.RepoURL,
		Branch:           deployment.Branch,
		CurrentCommitSHA: deployment.CommitSHA,
		LatestCommitSHA:  deployment.CommitSHA,
		SyncStatus:       models.GitSyncSynced,
	}

	if err := s.gitSyncRepo.CreateGitSyncTrack(track); err != nil {
		return nil, fmt.Errorf("failed to create sync track: %w", err)
	}

	return track, nil
}

// CheckForUpdates checks for new commits in the git repository
func (s *GitSyncService) CheckForUpdates(track *models.GitSyncTrack) error {
	p, err := s.projectRepo.FindByIDInternal(track.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to fetch project: %w", err)
	}

	// Get latest commit from remote
	latestCommit, err := s.getFetchLatestCommit(track.Repository, track.Branch, p.GitToken)
	if err != nil {
		return fmt.Errorf("failed to fetch latest commit: %w", err)
	}

	if latestCommit == nil {
		return nil // No new commits
	}

	// Store the latest commit info
	track.LatestCommitSHA = latestCommit.SHA
	track.LatestCommitMessage = latestCommit.Message
	track.LatestCommitAuthor = latestCommit.Author

	// Check if out of sync
	if track.CurrentCommitSHA != track.LatestCommitSHA {
		track.SyncStatus = models.GitSyncOutOfSync

		// Get git diff if available
		diff, err := s.getGitDiff(track.Repository, track.CurrentCommitSHA, track.LatestCommitSHA, p.GitToken)
		if err == nil && diff != nil {
			track.TotalChanges = diff.TotalChanges
			track.TotalAdditions = diff.TotalAdditions
			track.TotalDeletions = diff.TotalDeletions

			// Store changes summary
			summary := map[string]interface{}{
				"files_changed": diff.FilesChanged,
				"commits_ahead": diff.CommitsAhead,
				"is_clean":      diff.IsClean,
			}
			if summaryBytes, err := json.Marshal(summary); err == nil {
				track.ChangesSummary = string(summaryBytes)
			}

			// Store individual changes
			for _, change := range diff.ChangedFiles {
				change.ID = uuid.New().String()
				change.CreatedAt = time.Now().UTC()
				change.UpdatedAt = time.Now().UTC()
				change.SyncTrackID = track.ID
				if err := s.gitSyncRepo.CreateGitChange(&change); err != nil {
					log.Printf("failed to store git change: %v", err)
				}
			}
		}
	}

	track.UpdatedAt = time.Now().UTC()
	return s.gitSyncRepo.UpdateGitSyncTrack(track)
}

// SyncDeployment syncs deployment to the latest code
func (s *GitSyncService) SyncDeployment(trackID string, userID string, force bool) error {
	track, err := s.gitSyncRepo.GetGitSyncTrackByID(trackID)
	if err != nil {
		return fmt.Errorf("failed to get sync track: %w", err)
	}

	deployment, err := s.deployRepo.FindByID(track.DeploymentID)
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Check approval if required
	if track.SyncApprovalRequired && !force && track.SyncApprovedBy == "" {
		track.SyncStatus = models.GitSyncPending
		track.UpdatedAt = time.Now().UTC()
		return s.gitSyncRepo.UpdateGitSyncTrack(track)
	}

	// Start sync
	track.SyncStatus = models.GitSyncSyncing
	attemptTime := time.Now().UTC()
	track.LastSyncAttemptAt = &attemptTime
	track.UpdatedAt = time.Now().UTC()

	if err := s.gitSyncRepo.UpdateGitSyncTrack(track); err != nil {
		return fmt.Errorf("failed to update sync track: %w", err)
	}

	now := time.Now().UTC()
	history := &models.DeploymentSyncHistory{
		BaseModel: basemodel.BaseModel{
			ID:        uuid.New().String(),
			CreatedAt: now,
			UpdatedAt: now,
		},
		DeploymentID:  track.DeploymentID,
		ProjectID:     track.ProjectID,
		FromCommitSHA: deployment.CommitSHA,
		ToCommitSHA:   track.LatestCommitSHA,
		SyncType:      "manual",
		TriggeredBy:   userID,
	}

	startTime := time.Now()

	// Attempt to sync
	p, err := s.projectRepo.FindByIDInternal(track.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to fetch project: %w", err)
	}

	clonePath, err := s.cloneRepository(track.Repository, track.Branch, p.GitToken)
	if err != nil {
		track.SyncStatus = models.GitSyncFailed
		track.LastSyncAttemptError = err.Error()
		track.UpdatedAt = time.Now().UTC()
		s.gitSyncRepo.UpdateGitSyncTrack(track)

		history.Status = "failed"
		history.SyncError = err.Error()
		s.gitSyncRepo.CreateSyncHistory(history)
		return fmt.Errorf("sync failed: %w", err)
	}

	defer os.RemoveAll(clonePath)

	// Update deployment with new commit
	deployment.CommitSHA = track.LatestCommitSHA
	deployment.CommitMsg = track.LatestCommitMessage
	deployment.UpdatedAt = time.Now().UTC()

	if err := s.deployRepo.Update(deployment); err != nil {
		track.SyncStatus = models.GitSyncFailed
		track.LastSyncAttemptError = fmt.Sprintf("failed to update deployment: %v", err)
		track.UpdatedAt = time.Now().UTC()
		s.gitSyncRepo.UpdateGitSyncTrack(track)

		history.Status = "failed"
		history.SyncError = track.LastSyncAttemptError
		s.gitSyncRepo.CreateSyncHistory(history)
		return fmt.Errorf("failed to update deployment: %w", err)
	}

	// Sync successful
	track.CurrentCommitSHA = track.LatestCommitSHA
	track.SyncStatus = models.GitSyncSynced
	successTime := time.Now().UTC()
	track.LastSuccessfulSyncAt = &successTime
	track.UpdatedAt = time.Now().UTC()

	if err := s.gitSyncRepo.UpdateGitSyncTrack(track); err != nil {
		return fmt.Errorf("failed to update sync track: %w", err)
	}

	history.Status = "success"
	history.Duration = int(time.Since(startTime).Seconds())
	history.TotalChanges = track.TotalChanges
	if err := s.gitSyncRepo.CreateSyncHistory(history); err != nil {
		log.Printf("failed to create sync history: %v", err)
	}

	return nil
}

// ApproveSyncRequest approves a pending sync request
func (s *GitSyncService) ApproveSyncRequest(trackID string, userID string, approved bool) error {
	track, err := s.gitSyncRepo.GetGitSyncTrackByID(trackID)
	if err != nil {
		return fmt.Errorf("failed to get sync track: %w", err)
	}

	if approved {
		track.SyncApprovedBy = userID
		approvedTime := time.Now().UTC()
		track.SyncApprovedAt = &approvedTime
		// Auto-sync after approval
		return s.SyncDeployment(trackID, userID, true)
	}

	// Rejected
	track.SyncStatus = models.GitSyncOutOfSync
	track.UpdatedAt = time.Now().UTC()
	return s.gitSyncRepo.UpdateGitSyncTrack(track)
}

// getFetchLatestCommit fetches the latest commit info from remote
func (s *GitSyncService) getFetchLatestCommit(repo, branch, token string) (*models.GitCommitInfo, error) {
	// Using GitHub API for demo - can be extended to support other git providers
	if !strings.Contains(repo, "github.com") {
		// If we have a local clone in ProjectsDir, use it to get full info
		return s.getLocalLatestCommit(repo, branch)
	}

	// Parse GitHub URL
	parts := strings.Split(strings.TrimPrefix(repo, "https://github.com/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid github repository URL")
	}

	owner := parts[0]
	repoName := strings.TrimSuffix(parts[1], ".git")

	// Fetch from GitHub API
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", owner, repoName, branch)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch commit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var data struct {
		SHA    string `json:"sha"`
		Commit struct {
			Author struct {
				Name string `json:"name"`
			} `json:"author"`
			Message string `json:"message"`
		} `json:"commit"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode commit data: %w", err)
	}

	// Extract first line of commit message
	message := data.Commit.Message
	if idx := strings.Index(message, "\n"); idx > 0 {
		message = message[:idx]
	}

	return &models.GitCommitInfo{
		SHA:       data.SHA,
		Author:    data.Commit.Author.Name,
		Message:   message,
		Timestamp: time.Now().UTC(),
	}, nil
}

// GetLatestCommitInfo fetches current commit info for a project
func (s *GitSyncService) GetLatestCommitInfo(project *models.Project) (*models.GitCommitInfo, error) {
	// If it's github, use API
	if strings.Contains(project.RepoURL, "github.com") {
		return s.getFetchLatestCommit(project.RepoURL, project.Branch, project.GitToken)
	}

	// For non-GitHub or private-access, check ProjectsDir if cloned
	if s.projectsDir != "" {
		projDir := filepath.Join(s.projectsDir, project.ID)
		if _, err := os.Stat(projDir); err == nil {
			// Get info using git log
			cmd := exec.Command("git", "log", "-1", "--format=%H|%an|%s")
			cmd.Dir = projDir
			out, err := cmd.Output()
			if err == nil {
				parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 3)
				if len(parts) == 3 {
					return &models.GitCommitInfo{
						SHA:       parts[0],
						Author:    parts[1],
						Message:   parts[2],
						Timestamp: time.Now().UTC(),
					}, nil
				}
			}
		}
	}

	// Last resort: ls-remote (SHA only)
	return s.getLocalLatestCommit(project.RepoURL, project.Branch)
}

// getLocalLatestCommit gets latest commit from local git repo
func (s *GitSyncService) getLocalLatestCommit(repo, branch string) (*models.GitCommitInfo, error) {
	cmd := exec.Command("git", "ls-remote", repo, fmt.Sprintf("refs/heads/%s", branch))
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest commit: %w", err)
	}

	parts := strings.Fields(string(output))
	if len(parts) == 0 {
		return nil, fmt.Errorf("no commits found")
	}

	return &models.GitCommitInfo{
		SHA:       parts[0],
		Timestamp: time.Now().UTC(),
	}, nil
}

// getGitDiff gets the diff between two commits
func (s *GitSyncService) getGitDiff(repo, fromCommit, toCommit, token string) (*models.GitDiffSummary, error) {
	clonePath, err := s.cloneRepository(repo, "", token)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	defer os.RemoveAll(clonePath)

	// Get diff format
	cmd := exec.Command("git", "diff", "--numstat", fmt.Sprintf("%s..%s", fromCommit, toCommit))
	cmd.Dir = clonePath
	numstatOutput, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get numstat: %w", err)
	}

	summary := &models.GitDiffSummary{
		LastCommit: models.GitCommitInfo{
			SHA:       toCommit,
			Timestamp: time.Now().UTC(),
		},
	}

	for _, line := range strings.Split(string(numstatOutput), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			var additions, deletions int
			fmt.Sscanf(fields[0], "%d", &additions)
			fmt.Sscanf(fields[1], "%d", &deletions)
			filePath := strings.Join(fields[2:], " ")

			summary.TotalAdditions += additions
			summary.TotalDeletions += deletions
			summary.FilesChanged++

			summary.ChangedFiles = append(summary.ChangedFiles, models.GitChange{
				FilePath:  filePath,
				Additions: additions,
				Deletions: deletions,
			})
		}
	}

	summary.TotalChanges = len(summary.ChangedFiles)
	summary.IsClean = summary.TotalChanges == 0

	return summary, nil
}

// cloneRepository clones a git repository
func (s *GitSyncService) cloneRepository(repo, branch, token string) (string, error) {
	clonePath := filepath.Join(s.tempDir, fmt.Sprintf("pushpaka-sync-%d", time.Now().UnixNano()))

	// Inject token into URL if provided
	authRepo := repo
	if token != "" && strings.HasPrefix(repo, "https://") {
		authRepo = strings.Replace(repo, "https://", fmt.Sprintf("https://%s@", token), 1)
	}

	cmd := exec.Command("git", "clone")
	if branch != "" {
		cmd.Args = append(cmd.Args, "-b", branch)
	}
	cmd.Args = append(cmd.Args, "--depth", "1", authRepo, clonePath)

	if output, err := cmd.CombinedOutput(); err != nil {
		errMsg := strings.ReplaceAll(string(output), token, "****")
		return "", fmt.Errorf("git clone failed: %v: %s", err, errMsg)
	}

	return clonePath, nil
}

// EnableAutoSync enables automatic synchronization for a deployment
func (s *GitSyncService) EnableAutoSync(deploymentID string, config *models.GitAutoSyncConfig) error {
	if config.ID == "" {
		config.ID = uuid.New().String()
		now := time.Now().UTC()
		config.CreatedAt = now
	}
	config.DeploymentID = deploymentID
	config.UpdatedAt = time.Now().UTC()

	return s.gitSyncRepo.CreateAutoSyncConfig(config)
}

// GetAutoSyncConfig retrieves auto-sync configuration
func (s *GitSyncService) GetAutoSyncConfig(deploymentID string) (*models.GitAutoSyncConfig, error) {
	return s.gitSyncRepo.GetAutoSyncConfig(deploymentID)
}

// UpdateAutoSyncConfig updates auto-sync configuration
func (s *GitSyncService) UpdateAutoSyncConfig(config *models.GitAutoSyncConfig) error {
	config.UpdatedAt = time.Now().UTC()
	return s.gitSyncRepo.UpdateAutoSyncConfig(config)
}

// ShouldAutoSync determines if a deployment should auto-sync based on config
func (s *GitSyncService) ShouldAutoSync(track *models.GitSyncTrack, config *models.GitAutoSyncConfig) bool {
	if !config.Enabled {
		return false
	}
	return track.SyncStatus == models.GitSyncOutOfSync
}

// CloneTo clones a repository to a specific target directory with optional token
func (s *GitSyncService) CloneTo(repo, branch, targetDir, token string) error {
	// Create target directory if it doesn't exist
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// If directory is not empty, assume it's already cloned
	entries, _ := os.ReadDir(targetDir)
	if len(entries) > 0 {
		return nil
	}

	// Inject token into URL if provided
	authRepo := repo
	if token != "" && strings.HasPrefix(repo, "https://") {
		authRepo = strings.Replace(repo, "https://", fmt.Sprintf("https://%s@", token), 1)
	}

	// Clone
	cmd := exec.Command("git", "clone", "--depth", "1", "-b", branch, authRepo, ".")
	cmd.Dir = targetDir
	if output, err := cmd.CombinedOutput(); err != nil {
		// Scrub token from error message for security
		errMsg := strings.ReplaceAll(string(output), token, "****")
		return fmt.Errorf("git clone failed: %v, output: %s", err, errMsg)
	}

	return nil
}
