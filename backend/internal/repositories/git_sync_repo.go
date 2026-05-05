package repositories

import (
	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type GitSyncRepository struct {
	db *gorm.DB
}

func NewGitSyncRepository(db *gorm.DB) *GitSyncRepository {
	basemodel.EnsureSynced[models.GitSyncTrack](db)
	basemodel.EnsureSynced[models.GitChange](db)
	basemodel.EnsureSynced[models.GitAutoSyncConfig](db)
	basemodel.EnsureSynced[models.DeploymentSyncHistory](db)
	return &GitSyncRepository{db: db}
}

// CreateGitSyncTrack creates a new git sync tracking record
func (r *GitSyncRepository) CreateGitSyncTrack(track *models.GitSyncTrack) error {
	return basemodel.Add(r.db, track)
}

// GetGitSyncTrackByDeploymentID retrieves sync track by deployment ID
func (r *GitSyncRepository) GetGitSyncTrackByDeploymentID(deploymentID string) (*models.GitSyncTrack, error) {
	return basemodel.First[models.GitSyncTrack](r.db, "deployment_id = ?", deploymentID)
}

// GetGitSyncTrackByID retrieves sync track by ID
func (r *GitSyncRepository) GetGitSyncTrackByID(id string) (*models.GitSyncTrack, error) {
	return basemodel.Get[models.GitSyncTrack](r.db, id)
}

// GetGitSyncTracksByProjectID retrieves all sync tracks for a project
func (r *GitSyncRepository) GetGitSyncTracksByProjectID(projectID string, limit, offset int) ([]models.GitSyncTrack, error) {
	basemodel.EnsureSynced[models.GitSyncTrack](r.db)
	var dest []models.GitSyncTrack
	err := r.db.Where("project_id = ?", projectID).Order("updated_at DESC").Limit(limit).Offset(offset).Find(&dest).Error
	return dest, err
}

// GetOutOfSyncDeployments retrieves all out-of-sync deployments
func (r *GitSyncRepository) GetOutOfSyncDeployments(limit int) ([]models.GitSyncTrack, error) {
	basemodel.EnsureSynced[models.GitSyncTrack](r.db)
	var dest []models.GitSyncTrack
	err := r.db.Where("sync_status = ?", models.GitSyncOutOfSync).Order("updated_at ASC").Limit(limit).Find(&dest).Error
	return dest, err
}

// GetPendingApprovalSyncs retrieves syncs pending approval
func (r *GitSyncRepository) GetPendingApprovalSyncs(projectID string) ([]models.GitSyncTrack, error) {
	return basemodel.Query[models.GitSyncTrack](r.db, "project_id = ? AND sync_status = ? ORDER BY updated_at DESC", projectID, models.GitSyncPending)
}

// UpdateGitSyncTrack updates an existing git sync track
func (r *GitSyncRepository) UpdateGitSyncTrack(track *models.GitSyncTrack) error {
	return basemodel.Modify(r.db, track)
}

// CreateGitChange creates a new git change record
func (r *GitSyncRepository) CreateGitChange(change *models.GitChange) error {
	return basemodel.Add(r.db, change)
}

// GetGitChangesBySyncTrackID retrieves all changes for a sync track
func (r *GitSyncRepository) GetGitChangesBySyncTrackID(syncTrackID string) ([]models.GitChange, error) {
	return basemodel.Query[models.GitChange](r.db, "sync_track_id = ? ORDER BY created_at DESC", syncTrackID)
}

// CreateAutoSyncConfig creates auto-sync configuration
func (r *GitSyncRepository) CreateAutoSyncConfig(config *models.GitAutoSyncConfig) error {
	return basemodel.Add(r.db, config)
}

// GetAutoSyncConfig retrieves auto-sync config by deployment ID
func (r *GitSyncRepository) GetAutoSyncConfig(deploymentID string) (*models.GitAutoSyncConfig, error) {
	return basemodel.First[models.GitAutoSyncConfig](r.db, "deployment_id = ?", deploymentID)
}

// UpdateAutoSyncConfig updates auto-sync configuration
func (r *GitSyncRepository) UpdateAutoSyncConfig(config *models.GitAutoSyncConfig) error {
	return basemodel.Modify(r.db, config)
}

// CreateSyncHistory creates a sync history record
func (r *GitSyncRepository) CreateSyncHistory(history *models.DeploymentSyncHistory) error {
	return basemodel.Add(r.db, history)
}

// GetSyncHistory retrieves sync history for a deployment
func (r *GitSyncRepository) GetSyncHistory(deploymentID string, limit, offset int) ([]models.DeploymentSyncHistory, error) {
	basemodel.EnsureSynced[models.DeploymentSyncHistory](r.db)
	var dest []models.DeploymentSyncHistory
	err := r.db.Where("deployment_id = ?", deploymentID).Order("created_at DESC").Limit(limit).Offset(offset).Find(&dest).Error
	return dest, err
}

// GetSyncHistoryCount returns the total number of sync history records
func (r *GitSyncRepository) GetSyncHistoryCount(deploymentID string) (int, error) {
	basemodel.EnsureSynced[models.DeploymentSyncHistory](r.db)
	var count int64
	err := r.db.Model(&models.DeploymentSyncHistory{}).Where("deployment_id = ?", deploymentID).Count(&count).Error
	return int(count), err
}
