package services

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

var ErrOAuthStateMismatch = errors.New("OAuth state mismatch or expired")
var ErrOAuthExchangeFailed = errors.New("OAuth token exchange failed")

// OAuthState is the GORM model for `oauth_states` table.
// Using a proper model instead of raw SQL ensures AutoMigrate creates the table.
type OAuthState struct {
	State     string    `gorm:"primaryKey;type:varchar(64)" json:"state"`
	Provider  string    `gorm:"type:varchar(50)" json:"provider"`
	Redirect  string    `gorm:"type:varchar(512)" json:"redirect"`
	ExpiresAt time.Time `json:"expires_at"`
}

type OAuthService struct {
	userRepo *repositories.UserRepository
	cfg      *config.Config
	authSvc  *AuthService
	db       *gorm.DB
}

func NewOAuthService(userRepo *repositories.UserRepository, cfg *config.Config, authSvc *AuthService, db *gorm.DB) *OAuthService {
	// Ensure the oauth_states table exists
	_ = db.AutoMigrate(&OAuthState{})
	return &OAuthService{userRepo: userRepo, cfg: cfg, authSvc: authSvc, db: db}
}

func (s *OAuthService) GenerateState(provider, redirect string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	state := hex.EncodeToString(b)
	oauthState := OAuthState{
		State:     state,
		Provider:  provider,
		Redirect:  redirect,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}
	if err := s.db.Create(&oauthState).Error; err != nil {
		return "", fmt.Errorf("storing oauth state: %w", err)
	}
	return state, nil
}

func (s *OAuthService) ValidateState(state string) error {
	var oauthState OAuthState
	err := s.db.First(&oauthState, "state = ?", state).Error
	if err != nil {
		return ErrOAuthStateMismatch
	}
	// Delete state immediately (one-time use)
	s.db.Where("state = ?", state).Delete(&OAuthState{}) //nolint:errcheck
	if time.Now().UTC().After(oauthState.ExpiresAt) {
		return ErrOAuthStateMismatch
	}
	return nil
}

func (s *OAuthService) GithubAuthURL(state string) string {
	params := url.Values{
		"client_id":    {s.cfg.GithubClientID},
		"redirect_uri": {s.cfg.BaseURL + "/api/v1/auth/github/callback"},
		"scope":        {"user:email"},
		"state":        {state},
	}
	return "https://github.com/login/oauth/authorize?" + params.Encode()
}

func (s *OAuthService) GitlabAuthURL(state string) string {
	base := s.cfg.GitlabBaseURL
	if base == "" {
		base = "https://gitlab.com"
	}
	params := url.Values{
		"client_id":     {s.cfg.GitlabClientID},
		"redirect_uri":  {s.cfg.BaseURL + "/api/v1/auth/gitlab/callback"},
		"response_type": {"code"},
		"scope":         {"read_user"},
		"state":         {state},
	}
	return base + "/oauth/authorize?" + params.Encode()
}

type githubUser struct {
	ID        int    `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func (s *OAuthService) ExchangeGithub(code string) (*models.AuthResponse, error) {
	token, err := githubTokenExchange(s.cfg.GithubClientID, s.cfg.GithubClientSecret, code,
		s.cfg.BaseURL+"/api/v1/auth/github/callback")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthExchangeFailed, err)
	}
	ghUser, err := githubFetchUser(token)
	if err != nil {
		return nil, fmt.Errorf("fetching github profile: %w", err)
	}
	email := ghUser.Email
	if email == "" {
		if emails, err2 := githubFetchEmails(token); err2 == nil {
			for _, e := range emails {
				if e.Primary && e.Verified {
					email = e.Email
					break
				}
			}
		}
	}
	if email == "" {
		email = fmt.Sprintf("github_%d@noreply.pushpaka", ghUser.ID)
	}
	name := ghUser.Name
	if name == "" {
		name = ghUser.Login
	}
	return s.findOrCreateUser(email, name, "github", fmt.Sprintf("%d", ghUser.ID), ghUser.AvatarURL)
}

type gitlabUser struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (s *OAuthService) ExchangeGitlab(code string) (*models.AuthResponse, error) {
	base := s.cfg.GitlabBaseURL
	if base == "" {
		base = "https://gitlab.com"
	}
	token, err := gitlabTokenExchange(base, s.cfg.GitlabClientID, s.cfg.GitlabClientSecret, code,
		s.cfg.BaseURL+"/api/v1/auth/gitlab/callback")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthExchangeFailed, err)
	}
	gUser, err := gitlabFetchUser(base, token)
	if err != nil {
		return nil, fmt.Errorf("fetching gitlab profile: %w", err)
	}
	email := gUser.Email
	if email == "" {
		email = fmt.Sprintf("gitlab_%d@noreply.pushpaka", gUser.ID)
	}
	name := gUser.Name
	if name == "" {
		name = gUser.Username
	}
	return s.findOrCreateUser(email, name, "gitlab", fmt.Sprintf("%d", gUser.ID), gUser.AvatarURL)
}

func (s *OAuthService) findOrCreateUser(email, name, provider, providerID, avatarURL string) (*models.AuthResponse, error) {
	user, err := s.userRepo.FindByEmail(email)
	if err != nil {
		// New user — check if they should be admin (first user or matching FIRST_ADMIN_EMAIL)
		role := "user"
		if s.userRepo.Count() == 0 {
			role = "admin"
		}
		if s.cfg.FirstAdminEmail != "" && email == s.cfg.FirstAdminEmail {
			role = "admin"
		}

		now := time.Now().UTC()
		user = &models.User{
			BaseModel: basemodel.BaseModel{
				ID:        uuid.New().String(),
				CreatedAt: now,
				UpdatedAt: now,
			},
			Email:           email,
			Name:            name,
			PasswordHash:    "$oauth$" + provider + "$" + providerID,
			APIKey:          uuid.New().String(),
			Role:            role,
			IsActive:        true,
			AvatarURL:       avatarURL,
			OAuthProvider:   provider,
			OAuthProviderID: providerID,
		}
		if createErr := s.userRepo.Create(user); createErr != nil {
			return nil, fmt.Errorf("creating oauth user: %w", createErr)
		}
	} else {
		// Existing user — update avatar and OAuth fields if they changed
		if user.AvatarURL != avatarURL || user.OAuthProvider == "" {
			user.AvatarURL = avatarURL
			user.OAuthProvider = provider
			user.OAuthProviderID = providerID
			_ = s.userRepo.Update(user)
		}
	}

	jwtToken, err := s.authSvc.GenerateTokenForUser(user)
	if err != nil {
		return nil, err
	}
	return &models.AuthResponse{Token: jwtToken, User: user.ToSafe()}, nil
}

func githubTokenExchange(clientID, clientSecret, code, redirectURI string) (string, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	req, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token",
		strings.NewReader(data.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		Err         string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Err != "" {
		return "", errors.New(result.Err)
	}
	return result.AccessToken, nil
}

func githubFetchUser(token string) (*githubUser, error) {
	u, err := githubGET[githubUser]("https://api.github.com/user", token)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func githubFetchEmails(token string) ([]githubEmail, error) {
	return githubGET[[]githubEmail]("https://api.github.com/user/emails", token)
}

func githubGET[T any](endpoint, token string) (T, error) {
	var zero T
	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, err
	}
	return result, nil
}

func gitlabTokenExchange(baseURL, clientID, clientSecret, code, redirectURI string) (string, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.PostForm(baseURL+"/oauth/token", data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		Err         string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Err != "" {
		return "", errors.New(result.Err)
	}
	return result.AccessToken, nil
}

func gitlabFetchUser(baseURL, token string) (*gitlabUser, error) {
	req, _ := http.NewRequest("GET", baseURL+"/api/v4/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var u gitlabUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}
