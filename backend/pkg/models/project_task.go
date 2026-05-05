package models

import (
	"time"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
)

type TaskType string

const (
	TaskTypeSync   TaskType = "sync"
	TaskTypeFetch  TaskType = "fetch"
	TaskTypeBuild  TaskType = "build"
	TaskTypeTest   TaskType = "test"
	TaskTypeDeploy TaskType = "deploy"
	TaskTypeAIFix  TaskType = "aifix"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

type ProjectTask struct {
	basemodel.BaseModel
	ProjectID string     `gorm:"index;type:varchar(255);not null" json:"project_id"`
	Type      TaskType   `gorm:"type:varchar(50);not null" json:"type"`
	Status    TaskStatus `gorm:"type:varchar(50);not null;default:'pending'" json:"status"`
	CommitSHA string     `gorm:"type:varchar(100)" json:"commit_sha"`

	// Logs and Error messages
	Log   string `gorm:"type:text" json:"log"`
	Error string `gorm:"type:text" json:"error"`

	// Worker info
	WorkerID string `gorm:"index;type:varchar(255)" json:"worker_id"`

	// Chaining
	NextTaskID string `gorm:"type:varchar(255)" json:"next_task_id"`

	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	RetryCount int        `gorm:"default:0" json:"retry_count"`
}
