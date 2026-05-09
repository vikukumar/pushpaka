package models

import (
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"time"
)

type PersonalAccessToken struct {
	basemodel.BaseModel
	UserID      string     `json:"user_id" gorm:"index;not null"`
	Name        string     `json:"name" gorm:"not null"`
	TokenHash   string     `json:"-" gorm:"not null"` // Hashed token, never exposed after creation
	TokenMasked string     `json:"token_masked" gorm:"-"`
	Description string     `json:"description"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	Revoked     bool       `json:"revoked" gorm:"default:false"`
}
