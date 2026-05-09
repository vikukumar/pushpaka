package repositories

import (
	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type ProjectRepository struct {
	db *gorm.DB
}

func NewProjectRepository(db *gorm.DB) *ProjectRepository {
	basemodel.EnsureSynced[models.Project](db)
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) Create(p *models.Project) error {
	return basemodel.Add(r.db, p)
}

func (r *ProjectRepository) FindByUserID(userID string) ([]models.Project, error) {
	basemodel.EnsureSynced[models.Project](r.db)
	var projects []models.Project
	err := r.db.Where("user_id = ?", userID).Order("created_at desc").Find(&projects).Error
	return projects, err
}

func (r *ProjectRepository) FindByUserIDAndType(userID, projectType string) ([]models.Project, error) {
	basemodel.EnsureSynced[models.Project](r.db)
	var projects []models.Project
	err := r.db.Where("user_id = ? AND type = ?", userID, projectType).Order("created_at desc").Find(&projects).Error
	return projects, err
}

func (r *ProjectRepository) FindByID(id, userID string) (*models.Project, error) {
	return basemodel.First[models.Project](r.db, "id = ? AND user_id = ?", id, userID)
}

func (r *ProjectRepository) Update(p *models.Project) error {
	return basemodel.Modify(r.db, p)
}

// UpdateStatus updates only the status field of a project.
func (r *ProjectRepository) UpdateStatus(id, status string) error {
	basemodel.EnsureSynced[models.Project](r.db)
	return r.db.Model(&models.Project{}).Where("id = ?", id).Update("status", status).Error
}

// FindAllByAutoSync returns all projects that have AutoSync enabled.
func (r *ProjectRepository) FindAllByAutoSync() ([]models.Project, error) {
	var projects []models.Project
	err := r.db.Where("auto_sync_enabled = ?", true).Find(&projects).Error
	return projects, err
}

// FindByIDInternal returns the project including the git_token (for worker jobs).
// Only use this server-side -- never marshal the result directly to an API response.
func (r *ProjectRepository) FindByIDInternal(id string) (*models.Project, error) {
	return basemodel.Get[models.Project](r.db, id)
}

func (r *ProjectRepository) Delete(id, userID string) error {
	return r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&models.Project{}).Error
}

// SetMainDeployID updates the project's main_deploy_id pointer to the given deployment.
func (r *ProjectRepository) SetMainDeployID(projectID, deploymentID string) error {
	return r.db.Model(&models.Project{}).Where("id = ?", projectID).
		Update("main_deploy_id", deploymentID).Error
}

func (r *ProjectRepository) UpdateTaskStatus(id, taskID, status string) error {
	basemodel.EnsureSynced[models.Project](r.db)
	return r.db.Model(&models.Project{}).Where("id = ?", id).Updates(map[string]interface{}{
		"current_task_id": taskID,
		"latest_task_id":  taskID,
		"task_status":     status,
	}).Error
}
