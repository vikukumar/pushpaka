package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/vikukumar/pushpaka/internal/middleware"
	"github.com/vikukumar/pushpaka/internal/services"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type AuthHandler struct {
	authSvc *services.AuthService
}

func NewAuthHandler(authSvc *services.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.authSvc.Register(&req)
	if err != nil {
		if errors.Is(err, services.ErrUserExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "email already in use"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "registration failed"})
		return
	}
	c.JSON(http.StatusCreated, resp)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.authSvc.Login(&req)
	if err != nil {
		if errors.Is(err, services.ErrAccountDisabled) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Me returns the current authenticated user's profile.
// This is called after OAuth to fetch the full user object.
func (h *AuthHandler) Me(c *gin.Context) {
	userID := middleware.GetUserID(c)
	user, err := h.authSvc.GetUserByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": user.ToSafe()})
}

// --- Admin handlers bundled here since they're tightly coupled to auth ---

type AdminHandler struct {
	authSvc  *services.AuthService
	userRepo interface {
		ListAll(limit, offset int) ([]models.User, int64, error)
		UpdateRoleAndStatus(id, role string, isActive *bool) error
		FindByID(id string) (*models.User, error)
	}
}

func NewAdminHandler(authSvc *services.AuthService, userRepo interface {
	ListAll(limit, offset int) ([]models.User, int64, error)
	UpdateRoleAndStatus(id, role string, isActive *bool) error
	FindByID(id string) (*models.User, error)
}) *AdminHandler {
	return &AdminHandler{authSvc: authSvc, userRepo: userRepo}
}

// ListUsers returns a paginated list of all users (admin only).
func (h *AdminHandler) ListUsers(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit > 100 {
		limit = 100
	}

	users, total, err := h.userRepo.ListAll(limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch users"})
		return
	}

	safe := make([]models.SafeUser, 0, len(users))
	for i := range users {
		safe = append(safe, users[i].ToSafe())
	}

	c.JSON(http.StatusOK, models.UsersListResponse{
		Data:   safe,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// UpdateUserRole updates a user's role and/or active status (admin only).
func (h *AdminHandler) UpdateUserRole(c *gin.Context) {
	targetID := c.Param("id")
	callerID := middleware.GetUserID(c)

	// Prevent admin from deactivating themselves
	var req models.UpdateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if targetID == callerID && req.IsActive != nil && !*req.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot deactivate your own account"})
		return
	}

	if err := h.userRepo.UpdateRoleAndStatus(targetID, req.Role, req.IsActive); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
		return
	}

	user, err := h.userRepo.FindByID(targetID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "updated"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": user.ToSafe()})
}
