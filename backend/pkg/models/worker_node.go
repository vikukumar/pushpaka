package models

import (
	"time"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
)

type WorkerType string

const (
	WorkerTypeIntegrated WorkerType = "integrated"
	WorkerTypeVaahan     WorkerType = "vaahan"
	WorkerTypeHybrid     WorkerType = "hybrid"
)

type WorkerStatus string

const (
	WorkerStatusActive       WorkerStatus = "active"
	WorkerStatusOffline      WorkerStatus = "offline"
	WorkerStatusDisconnected WorkerStatus = "disconnected"
)

// WorkerNode represents a registered distributed worker
type WorkerNode struct {
	basemodel.BaseModel
	Name          string       `gorm:"type:varchar(255);not null" json:"name"`
	Type          WorkerType   `gorm:"type:varchar(50);not null" json:"type"`
	Status        WorkerStatus `gorm:"type:varchar(50);not null;default:'offline'" json:"status"`
	IPAddress     string       `gorm:"type:varchar(100)" json:"ip_address"`
	OS            string       `gorm:"type:varchar(50)" json:"os"`
	Architecture  string       `gorm:"type:varchar(50)" json:"architecture"`
	GoVersion     string       `gorm:"type:varchar(50)" json:"go_version"`
	DockerVersion string       `gorm:"type:varchar(50)" json:"docker_version"`
	NodeVersion   string       `gorm:"type:varchar(50)" json:"node_version"`
	MemoryTotal   uint64       `json:"memory_total"`
	CPUCount      int          `json:"cpu_count"`
	AuthToken     string       `gorm:"type:varchar(512);uniqueIndex" json:"-"` // Hidden from JSON responses
	Roles         []string     `gorm:"type:text;serializer:json" json:"roles"` // syncer, builder, tester, ai
	LastSeenAt    *time.Time   `json:"last_seen_at"`
}

// RegisterWorkerRequest payload sent from worker
type RegisterWorkerRequest struct {
	Name          string     `json:"name" binding:"required"`
	Type          WorkerType `json:"type" binding:"required"` // integrated, vaahan, hybrid
	IPAddress     string     `json:"ip_address"`
	OS            string     `json:"os"`
	Architecture  string     `json:"architecture"`
	GoVersion     string     `json:"go_version"`
	DockerVersion string     `json:"docker_version"`
	NodeVersion   string     `json:"node_version"`
	MemoryTotal   uint64     `json:"memory_total"`
	CPUCount      int        `json:"cpu_count"`
	Roles         []string   `json:"roles"`
	ZonePAT       string     `json:"zone_pat" binding:"required"` // The installation PAT for authentication
}

// WorkerAuthResponse payload sent to worker upon successful registration or rotation
type WorkerAuthResponse struct {
	WorkerID  string `json:"worker_id"`
	AuthToken string `json:"auth_token"` // Exchanged JWT token for websocket/HTTP communication
	ExpiresIn int64  `json:"expires_in"` // Standard TTL before rotation is required
}
