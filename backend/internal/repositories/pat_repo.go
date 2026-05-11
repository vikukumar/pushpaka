package repositories

import (
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
	"gorm.io/gorm"
)

type PATRepository struct {
	db *gorm.DB
}

func NewPATRepository(db *gorm.DB) *PATRepository {
	basemodel.EnsureSynced[models.PersonalAccessToken](db)
	return &PATRepository{db: db}
}

func (r *PATRepository) Create(pat *models.PersonalAccessToken) error {
	return basemodel.Add(r.db, pat)
}

func (r *PATRepository) FindByUserID(userID string) ([]models.PersonalAccessToken, error) {
	var pats []models.PersonalAccessToken
	err := r.db.Where("user_id = ?", userID).Order("created_at desc").Find(&pats).Error
	return pats, err
}

func (r *PATRepository) FindByHash(hash string) (*models.PersonalAccessToken, error) {
	var pat models.PersonalAccessToken
	err := r.db.Where("token_hash = ?", hash).First(&pat).Error
	if err != nil {
		return nil, err
	}
	return &pat, nil
}

func (r *PATRepository) Update(pat *models.PersonalAccessToken) error {
	return r.db.Save(pat).Error
}

func (r *PATRepository) Delete(id, userID string) error {
	return r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&models.PersonalAccessToken{}).Error
}
