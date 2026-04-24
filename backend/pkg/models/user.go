package models

import "github.com/vikukumar/pushpaka/pkg/basemodel"

// User is the core identity model. PasswordHash is never serialised.
// OAuthProvider/OAuthProviderID track SSO-linked accounts.
type User struct {
	basemodel.BaseModel
	Email           string `gorm:"uniqueIndex;type:varchar(255);not null" json:"email"`
	Name            string `gorm:"type:varchar(255);not null" json:"name"`
	PasswordHash    string `gorm:"type:varchar(255);not null" json:"-"`
	APIKey          string `gorm:"uniqueIndex;type:varchar(255)" json:"-"`
	Role            string `gorm:"type:varchar(50);default:'user'" json:"role"`
	IsActive        bool   `gorm:"default:true" json:"is_active"`
	AvatarURL       string `gorm:"type:varchar(512)" json:"avatar_url"`
	OAuthProvider   string `gorm:"type:varchar(50)" json:"oauth_provider"`
	OAuthProviderID string `gorm:"type:varchar(255)" json:"oauth_provider_id"`
}

// ToSafe returns a SafeUser scrubbed of sensitive fields.
func (u *User) ToSafe() SafeUser {
	return SafeUser{
		ID:            u.ID,
		Email:         u.Email,
		Name:          u.Name,
		Role:          u.Role,
		IsActive:      u.IsActive,
		AvatarURL:     u.AvatarURL,
		OAuthProvider: u.OAuthProvider,
		CreatedAt:     u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:     u.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// SafeUser is the User model with sensitive fields omitted — safe for API responses.
type SafeUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	IsActive      bool   `json:"is_active"`
	AvatarURL     string `json:"avatar_url"`
	OAuthProvider string `json:"oauth_provider"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type RegisterRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Name     string `json:"name"     binding:"required,min=2,max=64"`
	Password string `json:"password" binding:"required,min=8"`
}

type LoginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// AuthResponse is returned on successful login/register/OAuth.
// The Token is a signed JWT containing sub, email, name, role, exp.
type AuthResponse struct {
	Token string   `json:"token"`
	User  SafeUser `json:"user"`
}

// UpdateUserRoleRequest lets admins change a user's role or active status.
type UpdateUserRoleRequest struct {
	Role     string `json:"role"      binding:"required,oneof=admin user viewer"`
	IsActive *bool  `json:"is_active"`
}

// UsersListResponse wraps the paginated admin user list.
type UsersListResponse struct {
	Data   []SafeUser `json:"data"`
	Total  int64      `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}
