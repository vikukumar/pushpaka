package repositories

import (
	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	basemodel.EnsureSynced[models.User](db)
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(user *models.User) error {
	return basemodel.Add(r.db, user)
}

func (r *UserRepository) FindByEmail(email string) (*models.User, error) {
	return basemodel.First[models.User](r.db, "email = ?", email)
}

func (r *UserRepository) FindByID(id string) (*models.User, error) {
	return basemodel.Get[models.User](r.db, id)
}

func (r *UserRepository) FindByAPIKey(apiKey string) (*models.User, error) {
	return basemodel.First[models.User](r.db, "api_key = ?", apiKey)
}

func (r *UserRepository) Update(user *models.User) error {
	return basemodel.Modify(r.db, user)
}

// Count returns the total number of active (non-deleted) users.
// Used to auto-promote the first registered user to admin.
func (r *UserRepository) Count() int64 {
	var count int64
	r.db.Model(&models.User{}).Count(&count)
	return count
}

// ListAll returns all users paginated for the admin panel.
func (r *UserRepository) ListAll(limit, offset int) ([]models.User, int64, error) {
	var users []models.User
	var total int64
	if err := r.db.Model(&models.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := r.db.Limit(limit).Offset(offset).Order("created_at DESC").Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// UpdateRoleAndStatus sets role and/or is_active on a user.
func (r *UserRepository) UpdateRoleAndStatus(id, role string, isActive *bool) error {
	updates := map[string]interface{}{}
	if role != "" {
		updates["role"] = role
	}
	if isActive != nil {
		updates["is_active"] = *isActive
	}
	return r.db.Model(&models.User{}).Where("id = ?", id).Updates(updates).Error
}
