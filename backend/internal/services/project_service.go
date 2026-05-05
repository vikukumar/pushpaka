package services

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

var ErrProjectNotFound = errors.New("project not found")

type DeploymentSync interface {
	SyncRepo(userID, projectID string) (*models.Deployment, *models.ProjectTask, error)
}

type ProjectService struct {
	cfg            *config.Config
	projectRepo    *repositories.ProjectRepository
	deploymentRepo *repositories.DeploymentRepository
	taskRepo       *repositories.TaskRepository
	deploymentSvc  DeploymentSync
	taskDispatcher *TaskDispatcher
}

func NewProjectService(
	cfg *config.Config,
	projectRepo *repositories.ProjectRepository,
	deploymentRepo *repositories.DeploymentRepository,
	taskRepo *repositories.TaskRepository,
	taskDispatcher *TaskDispatcher,
) *ProjectService {
	return &ProjectService{
		cfg:            cfg,
		projectRepo:    projectRepo,
		deploymentRepo: deploymentRepo,
		taskRepo:       taskRepo,
		taskDispatcher: taskDispatcher,
	}
}

func (s *ProjectService) SetDeploymentService(svc DeploymentSync) {
	s.deploymentSvc = svc
}

func (s *ProjectService) Create(userID string, req *models.CreateProjectRequest) (*models.Project, error) {
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	port := req.Port
	if port == 0 {
		port = 3000
	}

	restart := req.RestartPolicy
	if restart == "" {
		restart = "unless-stopped"
	}
	// The instruction's diff for restart was malformed. Assuming the intent was to keep "unless-stopped"
	// and potentially add a default for CPULimit if it's empty, as suggested by the instruction's snippet.
	// However, without clear instruction on where to place `req.CPULimit = "1"`,
	// and to avoid making assumptions beyond the explicit instruction,
	// I will only apply the `time.Now().UTC()` and `ProjectInactive` changes.
	// If `req.CPULimit = "1"` was intended as a default, it would typically be:
	// if req.CPULimit == "" {
	//     req.CPULimit = "1"
	// }
	// But the instruction snippet placed it inside the `restart` if block, which is incorrect.
	// Sticking to the explicit and syntactically correct parts of the instruction.

	now := time.Now().UTC() // Changed from models.NowUTC()
	p := &models.Project{
		BaseModel: basemodel.BaseModel{
			ID:        uuid.New().String(),
			CreatedAt: now,
			UpdatedAt: now,
		},
		UserID:           userID,
		Name:             req.Name,
		RepoURL:          req.RepoURL,
		Branch:           branch,
		InstallCommand:   req.InstallCommand,
		BuildCommand:     req.BuildCommand,
		StartCommand:     req.StartCommand,
		RunDir:           req.RunDir,
		Port:             port,
		Framework:        req.Framework,
		Status:           "inactive",
		IsPrivate:        req.IsPrivate,
		GitToken:         req.GitToken,
		CPULimit:         req.CPULimit,
		MemoryLimit:      req.MemoryLimit,
		RestartPolicy:    restart,
		DeployTarget:     req.DeployTarget,
		K8sNamespace:     req.K8sNamespace,
		AutoSyncEnabled:  req.AutoSyncEnabled,
		SyncIntervalSecs: req.SyncIntervalSecs,
	}

	if err := s.projectRepo.Create(p); err != nil {
		return nil, fmt.Errorf("creating project: %w", err)
	}

	// Trigger initial sync task instead of direct goroutine sync
	if s.taskDispatcher != nil {
		s.taskDispatcher.CreateTask(p.ID, models.TaskTypeSync, "")
	} else if s.deploymentSvc != nil {
		go s.deploymentSvc.SyncRepo(userID, p.ID)
	}

	return p, nil
}

func (s *ProjectService) List(userID string) ([]models.Project, error) {
	return s.projectRepo.FindByUserID(userID)
}

func (s *ProjectService) Get(id, userID string) (*models.Project, error) {
	p, err := s.projectRepo.FindByID(id, userID)
	if err != nil {
		return nil, ErrProjectNotFound
	}
	return p, nil
}

func (s *ProjectService) Update(id, userID string, req *models.UpdateProjectRequest) (*models.Project, error) {
	p, err := s.projectRepo.FindByID(id, userID)
	if err != nil {
		return nil, ErrProjectNotFound
	}
	if req.Name != "" {
		p.Name = req.Name
	}
	if req.RepoURL != "" {
		p.RepoURL = req.RepoURL
	}
	if req.Branch != "" {
		p.Branch = req.Branch
	}
	// Allow clearing install/build/start command by setting to empty
	p.InstallCommand = req.InstallCommand
	p.BuildCommand = req.BuildCommand
	p.StartCommand = req.StartCommand
	p.RunDir = req.RunDir
	if req.Port > 0 {
		p.Port = req.Port
	}
	if req.Framework != "" {
		p.Framework = req.Framework
	}
	p.IsPrivate = req.IsPrivate
	// Only update the token when a new one is explicitly provided.
	if req.GitToken != "" {
		p.GitToken = req.GitToken
	}
	// Resource limits (allow clearing by setting to "")
	p.CPULimit = req.CPULimit
	p.MemoryLimit = req.MemoryLimit
	if req.RestartPolicy != "" {
		p.RestartPolicy = req.RestartPolicy
	}
	if req.AutoSyncEnabled != nil {
		p.AutoSyncEnabled = *req.AutoSyncEnabled
	}
	if req.SyncIntervalSecs != nil {
		p.SyncIntervalSecs = *req.SyncIntervalSecs
	}
	p.UpdatedAt = time.Now().UTC()

	if err := s.projectRepo.Update(p); err != nil {
		return nil, fmt.Errorf("updating project: %w", err)
	}

	// Trigger sync if branch or repo changed
	if s.deploymentSvc != nil {
		go s.deploymentSvc.SyncRepo(userID, p.ID)
	}

	return p, nil
}

func (s *ProjectService) Delete(id, userID string) error {
	p, err := s.projectRepo.FindByID(id, userID)
	if err != nil {
		return ErrProjectNotFound
	}

	// 1. Cleanup Docker resources
	s.cleanupDockerResources(p)

	// 2. Cleanup Filesystem resources
	s.cleanupFilesystemResources(p)

	// 3. Cleanup Database records (Cascading)
	// We manually delete deployments and tasks if they don't have FK constraints
	_ = s.deploymentRepo.DeleteByProjectID(id)
	_ = s.taskRepo.DeleteByProjectID(id)

	return s.projectRepo.Delete(id, userID)
}

func (s *ProjectService) cleanupDockerResources(p *models.Project) {
	// Find and remove all containers with the pushpaka_vahan prefix and project ID
	prefix := "pushpaka_vahan_" + p.ID[:8]
	
	// Stop and remove containers
	cmd := exec.Command("docker", "ps", "-a", "--filter", "name="+prefix, "--format", "{{.Names}}")
	out, err := cmd.Output()
	if err == nil {
		names := strings.Split(string(out), "\n")
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name != "" {
				_ = exec.Command("docker", "stop", name).Run()
				_ = exec.Command("docker", "rm", "-f", name).Run()
			}
		}
	}

	// Remove project-related images
	imagePrefix := "pushpaka_vahan/" + p.ID[:8]
	cmdImg := exec.Command("docker", "images", "--filter", "reference="+imagePrefix+"*", "--format", "{{.ID}}")
	outImg, errImg := cmdImg.Output()
	if errImg == nil {
		ids := strings.Split(string(outImg), "\n")
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id != "" {
				_ = exec.Command("docker", "rmi", "-f", id).Run()
			}
		}
	}
}

func (s *ProjectService) cleanupFilesystemResources(p *models.Project) {
	if s.cfg == nil {
		return
	}

	// Paths to clean
	// Note: these use the same logic as BuildWorker.getWorkspaceDir
	uID := p.UserID[:8]
	pID := p.ID[:8]

	dirs := []string{
		filepath.Join(s.cfg.ProjectsDir, uID, pID),
		filepath.Join(s.cfg.BuildsDir, uID, pID),
		filepath.Join(s.cfg.DeploysDir, uID, pID),
		filepath.Join(s.cfg.TestsDir, uID, pID),
		filepath.Join(s.cfg.CloneDir, ".buildcache", pID),
	}

	for _, dir := range dirs {
		if dir != "" && dir != "/" { // safety check
			_ = os.RemoveAll(dir)
		}
	}
}
