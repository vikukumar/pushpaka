package models

import (
	"github.com/vikukumar/pushpaka/pkg/basemodel"
)

type RegistryType string

const (
	RegistryTypeDocker RegistryType = "docker"
	RegistryTypeHelm   RegistryType = "helm"
	RegistryTypeBinary RegistryType = "binary"
)

type RegistryRepo struct {
	basemodel.BaseModel
	ProjectID   string       `json:"project_id" gorm:"index"`
	Name        string       `json:"name" gorm:"index"`
	Type        RegistryType `json:"type"`
	Description string       `json:"description"`
	IsPublic    bool         `json:"is_public" gorm:"default:false"`

	// Stats
	ArtifactCount int64 `json:"artifact_count" gorm:"-"`
	DownloadCount int64 `json:"download_count" gorm:"default:0"`
}

type RegistryArtifact struct {
	basemodel.BaseModel
	RepoID    string `json:"repo_id" gorm:"index"`
	Tag       string `json:"tag" gorm:"index"`
	Digest    string `json:"digest" gorm:"index"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
	Metadata  string `json:"metadata"` // JSON metadata (labels, architecture, etc.)
	Downloads int64  `json:"downloads" gorm:"default:0"`
}

type RegistryReplication struct {
	basemodel.BaseModel
	RepoID      string `json:"repo_id" gorm:"index"`
	SourceURL   string `json:"source_url"`
	Schedule    string `json:"schedule"` // Cron expression or "on_push"
	Concurrency int    `json:"concurrency" gorm:"default:4"`
	LastSyncAt  *Time  `json:"last_sync_at"`
	Status      string `json:"status"` // "idle", "syncing", "failed"
	ErrorMsg    string `json:"error_msg"`
}
