package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

var (
	ErrUserExists         = errors.New("user with this email already exists")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserNotFound       = errors.New("user not found")
	ErrAccountDisabled    = errors.New("account is disabled — contact your administrator")
)

type AuthService struct {
	userRepo *repositories.UserRepository
	cfg      *config.Config
}

func NewAuthService(userRepo *repositories.UserRepository, cfg *config.Config) *AuthService {
	return &AuthService{userRepo: userRepo, cfg: cfg}
}

func (s *AuthService) Register(req *models.RegisterRequest) (*models.AuthResponse, error) {
	existing, _ := s.userRepo.FindByEmail(req.Email)
	if existing != nil {
		return nil, ErrUserExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	now := time.Now().UTC()

	// Check if this is the very first user — promote to admin automatically.
	isFirstUser := s.userRepo.Count() == 0
	role := "user"
	if isFirstUser {
		role = "admin"
	}
	// Override with FIRST_ADMIN_EMAIL env if configured
	if s.cfg.FirstAdminEmail != "" && req.Email == s.cfg.FirstAdminEmail {
		role = "admin"
	}

	user := &models.User{
		BaseModel: basemodel.BaseModel{
			ID:        uuid.New().String(),
			CreatedAt: now,
			UpdatedAt: now,
		},
		Email:        req.Email,
		Name:         req.Name,
		PasswordHash: string(hash),
		APIKey:       uuid.New().String(),
		Role:         role,
		IsActive:     true,
	}

	if err := s.userRepo.Create(user); err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	token, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	return &models.AuthResponse{Token: token, User: user.ToSafe()}, nil
}

func (s *AuthService) Login(req *models.LoginRequest) (*models.AuthResponse, error) {
	user, err := s.userRepo.FindByEmail(req.Email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if !user.IsActive {
		return nil, ErrAccountDisabled
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	token, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	return &models.AuthResponse{Token: token, User: user.ToSafe()}, nil
}

// GetUserByID fetches a user by ID — used by auth middleware.
func (s *AuthService) GetUserByID(id string) (*models.User, error) {
	return s.userRepo.FindByID(id)
}

// ValidateToken validates a JWT and returns the embedded user claims.
// Returns (userID, role, error).
func (s *AuthService) ValidateToken(tokenStr string) (string, string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return "", "", errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", errors.New("invalid claims")
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		return "", "", errors.New("invalid subject claim")
	}

	role, _ := claims["role"].(string)
	return userID, role, nil
}

// ValidateAPIKey validates an X-API-Key header value.
func (s *AuthService) ValidateAPIKey(apiKey string) (*models.User, error) {
	return s.userRepo.FindByAPIKey(apiKey)
}

// generateToken creates a signed JWT with full user claims embedded.
// This allows the frontend to avoid an extra /me API call after login.
func (s *AuthService) generateToken(user *models.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":   user.ID,
		"email": user.Email,
		"name":  user.Name,
		"role":  user.Role,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Duration(s.cfg.JWTExpiry) * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

// GenerateTokenForUser is the exported version used by OAuth flows.
func (s *AuthService) GenerateTokenForUser(user *models.User) (string, error) {
	return s.generateToken(user)
}
