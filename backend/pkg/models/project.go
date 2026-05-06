package models

import (
	"time"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
)

type ProjectType string

const (
	ProjectTypeCD       ProjectType = "cd"
	ProjectTypeRegistry ProjectType = "registry"
)

type Project struct {
	basemodel.BaseModel
	UserID       string      `gorm:"index;type:varchar(255);not null" json:"user_id"`
	Type         ProjectType `gorm:"type:varchar(20);default:'cd'" json:"type"`
	Name         string      `gorm:"type:varchar(255);not null" json:"name"`
	RepoURL      string `gorm:"type:varchar(255);not null" json:"repo_url"`
	Branch       string `gorm:"type:varchar(100)" json:"branch"`
	BuildCommand string `gorm:"type:text" json:"build_command"`
	StartCommand string `gorm:"type:text" json:"start_command"`
	Port         int    `gorm:"default:3000" json:"port"`
	TestCommand  string `gorm:"type:text" json:"test_command"`
	Framework    string `gorm:"type:varchar(50)" json:"framework"`
	Status       string `gorm:"type:varchar(50);default:'created'" json:"status"`
	// GitToken is the personal access token for cloning private repositories.
	// It is NEVER serialised into API responses (json:"-").
	GitToken  string `gorm:"type:text" json:"-"`
	IsPrivate bool   `gorm:"default:false" json:"is_private"`
	// Build configuration
	InstallCommand string `gorm:"type:text" json:"install_command"`
	// RunDir is the subdirectory within the repo to use as the working directory.
	RunDir string `gorm:"type:varchar(255)" json:"run_dir"`
	// Resource limits -- passed through to docker run flags.
	CPULimit      string `gorm:"type:varchar(50)" json:"cpu_limit"`
	MemoryLimit   string `gorm:"type:varchar(50)" json:"memory_limit"`
	RestartPolicy string `gorm:"type:varchar(50)" json:"restart_policy"`
	// DeployTarget determines where the project runs: "docker" (default) or "kubernetes".
	DeployTarget string `gorm:"type:varchar(50);default:'docker'"  json:"deploy_target"`
	K8sNamespace string `gorm:"type:varchar(255)"  json:"k8s_namespace"`
	// Deployment limits and backup configuration
	MaxDeployments int    `gorm:"default:2" json:"max_deployments"`         // Max simultaneous deployments (default: 2)
	MaxBackups     int    `gorm:"default:3" json:"max_backups"`             // Max backup versions kept (default: 3)
	CloneDirectory string `gorm:"type:varchar(255)" json:"clone_directory"` // Base directory for git clones
	GitClonePath   string `gorm:"type:varchar(255)"  json:"git_clone_path"` // Current git clone directory for project
	MainDeployID   string `gorm:"type:varchar(255)"  json:"main_deploy_id"` // Current main deployment ID
	// Task tracking
	CurrentTaskID string `gorm:"type:varchar(255)" json:"current_task_id"`
	LatestTaskID  string `gorm:"type:varchar(255)" json:"latest_task_id"`
	TaskStatus    string `gorm:"type:varchar(50)" json:"task_status"`
	// AutoSync fields
	AutoSyncEnabled  bool `gorm:"default:true" json:"auto_sync_enabled"`
	SyncIntervalSecs int  `gorm:"default:10" json:"sync_interval_secs"`
	// Git metadata for the latest commit found remotely
	LatestCommitSHA string    `gorm:"type:varchar(100)" json:"latest_commit_sha"`
	LatestCommitMsg string    `gorm:"type:text" json:"latest_commit_msg"`
	LatestCommitAt  time.Time `json:"latest_commit_at"`
	// DeploymentStatus tracks if the project "should" be running (e.g. 'running', 'stopped')
	DeploymentStatus string `gorm:"type:varchar(50);default:'stopped'" json:"deployment_status"`

	// Detected info
	Language       string `gorm:"type:varchar(50)" json:"language"`
	PackageManager string `gorm:"type:varchar(50)" json:"package_manager"`
}

type CreateProjectRequest struct {
	Type             ProjectType `json:"type" gorm:"default:'cd'"`
	Name             string      `json:"name"            binding:"required,min=2,max=64"`
	RepoURL          string `json:"repo_url"        binding:"omitempty,url"`
	Branch           string `json:"branch"`
	InstallCommand   string `json:"install_command"`
	BuildCommand     string `json:"build_command"`
	StartCommand     string `json:"start_command"`
	TestCommand      string `json:"test_command"`
	RunDir           string `json:"run_dir"`
	Port             int    `json:"port"`
	Framework        string `json:"framework"`
	IsPrivate        bool   `json:"is_private"`
	GitToken         string `json:"git_token"`
	CPULimit         string `json:"cpu_limit"`
	MemoryLimit      string `json:"memory_limit"`
	RestartPolicy    string `json:"restart_policy"`
	DeployTarget     string `json:"deploy_target"`
	K8sNamespace     string `json:"k8s_namespace"`
	MaxDeployments   int    `json:"max_deployments"` // Default: 2 (1 main + 1 testing)
	MaxBackups       int    `json:"max_backups"`     // Default: 3
	AutoSyncEnabled  bool   `json:"auto_sync_enabled"`
	SyncIntervalSecs int    `json:"sync_interval_secs"`
	Language         string `json:"language"`
	PackageManager   string `json:"package_manager"`
}

// UpdateProjectRequest allows updating mutable project fields.
type UpdateProjectRequest struct {
	Name           string `json:"name"`
	RepoURL        string `json:"repo_url"`
	Branch         string `json:"branch"`
	InstallCommand string `json:"install_command"`
	BuildCommand   string `json:"build_command"`
	StartCommand   string `json:"start_command"`
	TestCommand    string `json:"test_command"`
	RunDir         string `json:"run_dir"`
	Port           int    `json:"port"`
	Framework      string `json:"framework"`
	IsPrivate      bool   `json:"is_private"`
	// GitToken -- if empty the existing stored token is preserved.
	GitToken         string `json:"git_token"`
	CPULimit         string `json:"cpu_limit"`
	MemoryLimit      string `json:"memory_limit"`
	RestartPolicy    string `json:"restart_policy"`
	DeployTarget     string `json:"deploy_target"`
	K8sNamespace     string `json:"k8s_namespace"`
	MaxDeployments   int    `json:"max_deployments"`
	MaxBackups       int    `json:"max_backups"`
	AutoSyncEnabled  *bool  `json:"auto_sync_enabled"` // Pointer to distinguish false from unset
	SyncIntervalSecs *int   `json:"sync_interval_secs"`
	Language         string `json:"language"`
	PackageManager   string `json:"package_manager"`
}
