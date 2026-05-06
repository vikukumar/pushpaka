package repositories

import (
	"fmt"
	"time"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
	"gorm.io/gorm"
)

type RegistryManagementRepository struct {
	db *gorm.DB
}

func NewRegistryManagementRepository(db *gorm.DB) *RegistryManagementRepository {
	basemodel.EnsureSynced[models.RegistryRepo](db)
	basemodel.EnsureSynced[models.RegistryArtifact](db)
	basemodel.EnsureSynced[models.RegistryReplication](db)
	return &RegistryManagementRepository{db: db}
}

func (r *RegistryManagementRepository) CreateRepo(repo *models.RegistryRepo) error {
	return basemodel.Add(r.db, repo)
}

func (r *RegistryManagementRepository) GetRepo(id string) (*models.RegistryRepo, error) {
	return basemodel.Get[models.RegistryRepo](r.db, id)
}

func (r *RegistryManagementRepository) ListReposByProject(projectID string) ([]models.RegistryRepo, error) {
	basemodel.EnsureSynced[models.RegistryRepo](r.db)
	var repos []models.RegistryRepo
	err := r.db.Where("project_id = ?", projectID).Find(&repos).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list repos: %w", err)
	}
	return repos, nil
}

func (r *RegistryManagementRepository) CreateArtifact(art *models.RegistryArtifact) error {
	return basemodel.Add(r.db, art)
}

func (r *RegistryManagementRepository) ListArtifacts(repoID string) ([]models.RegistryArtifact, error) {
	basemodel.EnsureSynced[models.RegistryArtifact](r.db)
	var arts []models.RegistryArtifact
	err := r.db.Where("repo_id = ?", repoID).Order("created_at desc").Find(&arts).Error
	return arts, err
}

func (r *RegistryManagementRepository) CreateReplication(rep *models.RegistryReplication) error {
	return basemodel.Add(r.db, rep)
}

func (r *RegistryManagementRepository) ListPendingReplications() ([]models.RegistryReplication, error) {
	basemodel.EnsureSynced[models.RegistryReplication](r.db)
	var reps []models.RegistryReplication
	// Simple logic: idle replications that haven't sync'd in 1 hour
	oneHourAgo := time.Now().Add(-1 * time.Hour)
	err := r.db.Where("status = ? OR (status = ? AND (last_sync_at < ? OR last_sync_at IS NULL))", "failed", "idle", oneHourAgo).Find(&reps).Error
	return reps, err
}

func (r *RegistryManagementRepository) UpdateReplicationStatus(id, status, errMsg string) error {
	basemodel.EnsureSynced[models.RegistryReplication](r.db)
	now := models.NowUTC()
	updates := map[string]interface{}{
		"status":     status,
		"error_msg":  errMsg,
		"updated_at": now,
	}
	if status == "idle" && errMsg == "" {
		updates["last_sync_at"] = now
	}
	return r.db.Model(&models.RegistryReplication{}).Where("id = ?", id).Updates(updates).Error
}
func (r *RegistryManagementRepository) DeleteRepo(id string) error {
	return r.db.Where("id = ?", id).Delete(&models.RegistryRepo{}).Error
}
func (r *RegistryManagementRepository) UpdateReplicationStatusByRepoID(repoID, status, errMsg string) error {
	basemodel.EnsureSynced[models.RegistryReplication](r.db)
	now := models.NowUTC()
	updates := map[string]interface{}{
		"status":       status,
		"error_msg":    errMsg,
		"updated_at":   now,
		"last_sync_at": nil, // Reset to force sync
	}
	return r.db.Model(&models.RegistryReplication{}).Where("repo_id = ?", repoID).Updates(updates).Error
}
