package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type PATService struct {
	repo *repositories.PATRepository
}

func NewPATService(repo *repositories.PATRepository) *PATService {
	return &PATService{repo: repo}
}

// GenerateToken creates a new secure string token
func generateSecureToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// hashToken creates a SHA256 hash of the plain token
func hashToken(token string) string {
	h := sha256.New()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

type CreatePATRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	ExpiresIn   int    `json:"expires_in_days"` // 0 means no expiration
}

// Create generates a new PAT and returns the PLAINTEXT token (which must be shown once).
func (s *PATService) Create(userID string, req *CreatePATRequest) (*models.PersonalAccessToken, string, error) {
	plainToken := "pushpaka_pat_" + generateSecureToken()
	hashedToken := hashToken(plainToken)

	now := models.NowUTC()
	var expiresAt *time.Time
	if req.ExpiresIn > 0 {
		exp := now.Time.AddDate(0, 0, req.ExpiresIn)
		expiresAt = &exp
	}

	pat := &models.PersonalAccessToken{
		BaseModel: basemodel.BaseModel{
			ID:        uuid.New().String(),
			CreatedAt: now.Time,
			UpdatedAt: now.Time,
		},
		UserID:      userID,
		Name:        req.Name,
		TokenHash:   hashedToken,
		Description: req.Description,
		ExpiresAt:   expiresAt,
		Revoked:     false,
	}

	if err := s.repo.Create(pat); err != nil {
		return nil, "", err
	}

	return pat, plainToken, nil
}

func (s *PATService) List(userID string) ([]models.PersonalAccessToken, error) {
	return s.repo.FindByUserID(userID)
}

func (s *PATService) Delete(id, userID string) error {
	return s.repo.Delete(id, userID)
}

func (s *PATService) VerifyAndTouch(plainToken string) (*models.PersonalAccessToken, error) {
	hash := hashToken(plainToken)
	pat, err := s.repo.FindByHash(hash)
	if err != nil {
		return nil, fmt.Errorf("invalid token")
	}

	if pat.Revoked {
		return nil, fmt.Errorf("token revoked")
	}

	if pat.ExpiresAt != nil && time.Now().After(*pat.ExpiresAt) {
		return nil, fmt.Errorf("token expired")
	}

	// Update last used
	now := time.Now().UTC()
	pat.LastUsedAt = &now
	s.repo.Update(pat)

	return pat, nil
}
