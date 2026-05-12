package repositories

import (
	"time"

	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type DeploymentRepository struct {
	db *gorm.DB
}

func NewDeploymentRepository(db *gorm.DB) *DeploymentRepository {
	return &DeploymentRepository{db: db}
}

func (r *DeploymentRepository) Create(d *models.Deployment) error {
	return basemodel.Add(r.db, d)
}

func (r *DeploymentRepository) FindByProjectID(projectID string, limit, offset int) ([]models.Deployment, error) {
	basemodel.EnsureSynced[models.Deployment](r.db)
	var deployments []models.Deployment
	err := r.db.Where("project_id = ?", projectID).Order("created_at desc").Limit(limit).Offset(offset).Find(&deployments).Error
	return deployments, err
}

func (r *DeploymentRepository) FindByUserID(userID string, limit, offset int) ([]models.Deployment, error) {
	basemodel.EnsureSynced[models.Deployment](r.db)
	var deployments []models.Deployment
	err := r.db.Where("user_id = ?", userID).Order("created_at desc").Limit(limit).Offset(offset).Find(&deployments).Error
	return deployments, err
}

func (r *DeploymentRepository) FindByID(id string) (*models.Deployment, error) {
	return basemodel.Get[models.Deployment](r.db, id)
}

func (r *DeploymentRepository) UpdateStatus(id string, status models.DeploymentStatus, errorMsg string) error {
	basemodel.EnsureSynced[models.Deployment](r.db)
	return r.db.Model(&models.Deployment{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":     status,
		"error_msg":  errorMsg,
		"updated_at": time.Now().UTC(),
	}).Error
}

func (r *DeploymentRepository) Update(d *models.Deployment) error {
	return basemodel.Modify(r.db, d)
}

func (r *DeploymentRepository) FindRunningByProjectID(projectID string) (*models.Deployment, error) {
	return basemodel.First[models.Deployment](r.db, "project_id = ? AND status = ?", projectID, "running")
}

// FindLatestRunningByProject finds the most-recently-started running deployment
// for a project and writes it into dest. Returns an error if none exists.
func (r *DeploymentRepository) FindLatestRunningByProject(projectID string, dest *models.Deployment) error {
	return r.db.Where("project_id = ? AND status = ?", projectID, "running").
		Order("created_at desc").
		First(dest).Error
}

func (r *DeploymentRepository) FindAllRunning() ([]models.Deployment, error) {
	return basemodel.Query[models.Deployment](r.db, "status = ?", "running")
}

func (r *DeploymentRepository) FixLocalProtocols() error {
	basemodel.EnsureSynced[models.Deployment](r.db)
	// Update any 'https://localhost' or 'https://127.0.0.1' records to 'http'
	return r.db.Model(&models.Deployment{}).
		Where("url LIKE ?", "https://localhost%").
		Or("url LIKE ?", "https://127.0.0.1%").
		Update("url", gorm.Expr("REPLACE(url, 'https://', 'http://')")).Error
}

func (r *DeploymentRepository) FindLatestByProjectID(projectID string) (*models.Deployment, error) {
	return basemodel.First[models.Deployment](r.db, "project_id = ?", projectID)
}

// FailStaleQueued marks all deployments that are still "queued" as "failed".
// Called at startup when no Redis/worker is available, to clear stuck records.
func (r *DeploymentRepository) Delete(id string) error {
	return basemodel.Delete[models.Deployment](r.db, id)
}

func (r *DeploymentRepository) FailStaleQueued(errorMsg string) error {
	basemodel.EnsureSynced[models.Deployment](r.db)
	// Use models.NowUTC() so Value() emits RFC3339Nano text, which scans correctly
	// back into *models.Time via Scan() without format ambiguity.
	now := models.NowUTC()
	nowStr, _ := now.Value()
	return r.db.Model(&models.Deployment{}).Where("status = ?", "queued").Updates(map[string]interface{}{
		"status":      "failed",
		"error_msg":   errorMsg,
		"finished_at": nowStr,
		"updated_at":  nowStr,
	}).Error
}

// ClearDefault clears is_default on all deployments for the given project.
func (r *DeploymentRepository) ClearDefault(projectID string) error {
	return basemodel.Update[models.Deployment](r.db, projectID, map[string]interface{}{"is_default": false})
}

// SetDefault marks a single deployment as the default/live one.
func (r *DeploymentRepository) SetDefault(deploymentID string) error {
	return basemodel.Update[models.Deployment](r.db, deploymentID, map[string]interface{}{"is_default": true})
}

// FindDefaultByProjectID returns the deployment marked as Default/Live for a project.
func (r *DeploymentRepository) FindDefaultByProjectID(projectID string) (*models.Deployment, error) {
	return basemodel.First[models.Deployment](r.db, "project_id = ? AND is_default = ? AND status = ?", projectID, true, models.DeploymentRunning)
}

// ListFailedRecent returns the latest failed deployments for a user.
func (r *DeploymentRepository) ListFailedRecent(userID string, limit int) ([]models.Deployment, error) {
	basemodel.EnsureSynced[models.Deployment](r.db)
	var dest []models.Deployment
	err := r.db.Where("user_id = ? AND (status = ? OR (status = ? AND error_msg != ''))",
		userID, models.DeploymentFailed, models.DeploymentStopped).
		Order("created_at DESC").Limit(limit).Find(&dest).Error
	return dest, err
}

// FindStuckBuilding returns deployments stuck in "building" state that started before the given threshold time.
// Used by AI monitoring to detect hung builds.
func (r *DeploymentRepository) FindStuckBuilding(olderThan time.Time) ([]models.Deployment, error) {
	basemodel.EnsureSynced[models.Deployment](r.db)
	var dest []models.Deployment
	err := r.db.Where("status = ? AND started_at IS NOT NULL AND started_at < ?",
		models.DeploymentBuilding, olderThan).
		Order("started_at ASC").Find(&dest).Error
	return dest, err
}

// GetStats returns aggregate counts of deployments by status.
func (r *DeploymentRepository) GetStats() (map[string]int64, error) {
	basemodel.EnsureSynced[models.Deployment](r.db)
	var results []struct {
		Status string
		Count  int64
	}
	err := r.db.Model(&models.Deployment{}).Select("status, count(*) as count").Group("status").Scan(&results).Error
	if err != nil {
		return nil, err
	}

	stats := make(map[string]int64)
	for _, res := range results {
		stats[res.Status] = res.Count
	}
	return stats, nil
}
func (r *DeploymentRepository) DeleteByProjectID(projectID string) error {
	return r.db.Where("project_id = ?", projectID).Delete(&models.Deployment{}).Error
}
