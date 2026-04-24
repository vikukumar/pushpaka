package repositories

import (
	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type TaskRepository struct {
	db *gorm.DB
}

func NewTaskRepository(db *gorm.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) Create(task *models.ProjectTask) error {
	return basemodel.Add(r.db, task)
}

func (r *TaskRepository) Update(task *models.ProjectTask) error {
	return basemodel.Modify(r.db, task)
}

func (r *TaskRepository) Get(id string) (*models.ProjectTask, error) {
	return basemodel.Get[models.ProjectTask](r.db, id)
}

func (r *TaskRepository) FindByProject(projectID string, limit int) ([]models.ProjectTask, error) {
	basemodel.EnsureSynced[models.ProjectTask](r.db)
	var dest []models.ProjectTask
	err := r.db.Where("project_id = ?", projectID).Order("created_at DESC").Limit(limit).Find(&dest).Error
	return dest, err
}

func (r *TaskRepository) FindByProjectID(projectID string) ([]models.ProjectTask, error) {
	return r.FindByProject(projectID, 50)
}

func (r *TaskRepository) FindPending(taskType models.TaskType, limit int) ([]models.ProjectTask, error) {
	basemodel.EnsureSynced[models.ProjectTask](r.db)
	var dest []models.ProjectTask
	err := r.db.Where("type = ? AND status = ?", taskType, models.TaskStatusPending).Order("created_at ASC").Limit(limit).Find(&dest).Error
	return dest, err
}

func (r *TaskRepository) FindByStatus(status models.TaskStatus) ([]models.ProjectTask, error) {
	basemodel.EnsureSynced[models.ProjectTask](r.db)
	var dest []models.ProjectTask
	err := r.db.Where("status = ?", status).Order("created_at ASC").Find(&dest).Error
	return dest, err
}

func (r *TaskRepository) Exists(projectID string, taskType models.TaskType, sha string) bool {
	var count int64
	r.db.Model(&models.ProjectTask{}).
		Where("project_id = ? AND type = ? AND commit_sha = ? AND status IN ?", projectID, taskType, sha, []string{string(models.TaskStatusPending), string(models.TaskStatusRunning)}).
		Count(&count)
	return count > 0
}
