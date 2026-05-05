package repositories

import (
	"errors"

	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type DeploymentManagementRepository struct {
	db *gorm.DB
}

func NewDeploymentManagementRepository(db *gorm.DB) *DeploymentManagementRepository {
	basemodel.EnsureSynced[models.DeploymentCodeSignature](db)
	basemodel.EnsureSynced[models.DeploymentInstance](db)
	basemodel.EnsureSynced[models.DeploymentBackup](db)
	basemodel.EnsureSynced[models.DeploymentAction](db)
	basemodel.EnsureSynced[models.ProjectDeploymentStats](db)
	return &DeploymentManagementRepository{db: db}
}

// ============= Code Signature Operations =============

func (r *DeploymentManagementRepository) CreateCodeSignature(sig *models.DeploymentCodeSignature) error {
	return basemodel.Add(r.db, sig)
}

func (r *DeploymentManagementRepository) GetCodeSignature(id string) (*models.DeploymentCodeSignature, error) {
	return basemodel.Get[models.DeploymentCodeSignature](r.db, id)
}

func (r *DeploymentManagementRepository) GetCodeSignatureByDeployment(deploymentID string) (*models.DeploymentCodeSignature, error) {
	basemodel.EnsureSynced[models.DeploymentCodeSignature](r.db)
	var sig models.DeploymentCodeSignature
	err := r.db.Where("deployment_id = ?", deploymentID).Order("created_at desc").First(&sig).Error
	if err != nil {
		return nil, err
	}
	return &sig, nil
}

// ============= Deployment Instance Operations =============

func (r *DeploymentManagementRepository) CreateDeploymentInstance(inst *models.DeploymentInstance) error {
	return basemodel.Add(r.db, inst)
}

func (r *DeploymentManagementRepository) GetDeploymentInstance(id string) (*models.DeploymentInstance, error) {
	return basemodel.Get[models.DeploymentInstance](r.db, id)
}

func (r *DeploymentManagementRepository) GetDeploymentInstances(deploymentID string) ([]models.DeploymentInstance, error) {
	basemodel.EnsureSynced[models.DeploymentInstance](r.db)
	var instances []models.DeploymentInstance
	err := r.db.Where("deployment_id = ?", deploymentID).Order("role, created_at").Find(&instances).Error
	return instances, err
}

func (r *DeploymentManagementRepository) GetMainDeploymentInstance(projectID string) (*models.DeploymentInstance, error) {
	basemodel.EnsureSynced[models.DeploymentInstance](r.db)
	var inst models.DeploymentInstance
	err := r.db.Where("project_id = ? AND role = ? AND status = 'running'", projectID, models.DeploymentRoleMain).First(&inst).Error
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func (r *DeploymentManagementRepository) UpdateDeploymentInstance(inst *models.DeploymentInstance) error {
	return basemodel.Modify(r.db, inst)
}

func (r *DeploymentManagementRepository) DeleteDeploymentInstance(id string) error {
	basemodel.EnsureSynced[models.DeploymentInstance](r.db)
	return r.db.Where("id = ?", id).Delete(&models.DeploymentInstance{}).Error
}

// ============= Backup Operations =============

func (r *DeploymentManagementRepository) CreateBackup(backup *models.DeploymentBackup) error {
	return basemodel.Add(r.db, backup)
}

func (r *DeploymentManagementRepository) GetBackup(id string) (*models.DeploymentBackup, error) {
	return basemodel.Get[models.DeploymentBackup](r.db, id)
}

func (r *DeploymentManagementRepository) GetBackupsByDeployment(deploymentID string, limit int) ([]models.DeploymentBackup, error) {
	basemodel.EnsureSynced[models.DeploymentBackup](r.db)
	var backups []models.DeploymentBackup
	err := r.db.Where("deployment_id = ?", deploymentID).Order("created_at desc").Limit(limit).Find(&backups).Error
	return backups, err
}

func (r *DeploymentManagementRepository) GetOldestBackups(projectID string, keepCount int) ([]models.DeploymentBackup, error) {
	basemodel.EnsureSynced[models.DeploymentBackup](r.db)
	var backups []models.DeploymentBackup
	// To perform the LIMIT (SELECT COUNT(*) - keepCount) logic, it is easier to fetch all, and slice in memory.
	// Or use an offset. GORM Offset expects an integer.
	var totalCount int64
	r.db.Model(&models.DeploymentBackup{}).Where("project_id = ?", projectID).Count(&totalCount)

	offset := int(totalCount) - keepCount
	if offset <= 0 {
		return backups, nil // No backups to delete
	}

	err := r.db.Where("project_id = ? AND is_restored = ?", projectID, false).Order("created_at asc").Limit(offset).Find(&backups).Error
	return backups, err
}

func (r *DeploymentManagementRepository) UpdateBackup(backup *models.DeploymentBackup) error {
	return basemodel.Modify(r.db, backup)
}

func (r *DeploymentManagementRepository) DeleteBackup(id string) error {
	basemodel.EnsureSynced[models.DeploymentBackup](r.db)
	return r.db.Where("id = ?", id).Delete(&models.DeploymentBackup{}).Error
}

// ============= Deployment Action Operations =============

func (r *DeploymentManagementRepository) CreateDeploymentAction(action *models.DeploymentAction) error {
	return basemodel.Add(r.db, action)
}

func (r *DeploymentManagementRepository) UpdateDeploymentAction(action *models.DeploymentAction) error {
	return basemodel.Modify(r.db, action)
}

func (r *DeploymentManagementRepository) GetDeploymentActions(deploymentID string, limit int) ([]models.DeploymentAction, error) {
	basemodel.EnsureSynced[models.DeploymentAction](r.db)
	var actions []models.DeploymentAction
	err := r.db.Where("deployment_id = ?", deploymentID).Order("created_at desc").Limit(limit).Find(&actions).Error
	return actions, err
}

func (r *DeploymentManagementRepository) GetLastDeploymentAction(deploymentID string) (*models.DeploymentAction, error) {
	basemodel.EnsureSynced[models.DeploymentAction](r.db)
	var action models.DeploymentAction
	err := r.db.Where("deployment_id = ?", deploymentID).Order("created_at desc").First(&action).Error
	if err != nil {
		return nil, err
	}
	return &action, nil
}

// ============= Stats Operations =============

func (r *DeploymentManagementRepository) CreateOrUpdateStats(stats *models.ProjectDeploymentStats) error {
	basemodel.EnsureSynced[models.ProjectDeploymentStats](r.db)
	var existing models.ProjectDeploymentStats
	err := r.db.Where("project_id = ?", stats.ProjectID).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return basemodel.Add(r.db, stats)
		}
		return err
	}

	// Ensure ID is matched to update existing record instead of creating new
	stats.ID = existing.ID
	return basemodel.Modify(r.db, stats)
}

func (r *DeploymentManagementRepository) GetStats(projectID string) (*models.ProjectDeploymentStats, error) {
	basemodel.EnsureSynced[models.ProjectDeploymentStats](r.db)
	var stats models.ProjectDeploymentStats
	err := r.db.Where("project_id = ?", projectID).First(&stats).Error
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// ============= Helper Methods =============

func (r *DeploymentManagementRepository) CountDeploymentsByProject(projectID string) (int, error) {
	var count int64
	err := r.db.Model(&models.DeploymentInstance{}).Where("project_id = ? AND role IN (?, ?)", projectID, models.DeploymentRoleMain, models.DeploymentRoleTesting).Count(&count).Error
	return int(count), err
}

func (r *DeploymentManagementRepository) CountBackupsByProject(projectID string) (int, error) {
	var count int64
	err := r.db.Model(&models.DeploymentBackup{}).Where("project_id = ?", projectID).Count(&count).Error
	return int(count), err
}

func (r *DeploymentManagementRepository) GetProjectDeployments(projectID string) ([]models.DeploymentInstance, error) {
	basemodel.EnsureSynced[models.DeploymentInstance](r.db)
	var instances []models.DeploymentInstance
	err := r.db.Where("project_id = ?", projectID).Order("role, created_at").Find(&instances).Error
	return instances, err
}

func (r *DeploymentManagementRepository) GetBackupByID(backupID string) (*models.DeploymentBackup, error) {
	return basemodel.Get[models.DeploymentBackup](r.db, backupID)
}
