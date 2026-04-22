package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/vikukumar/pushpaka/internal/services"
	"github.com/vikukumar/pushpaka/pkg/models"
)

const UserIDKey = "userID"
const UserRoleKey = "userRole"
const UserKey = "user"

// JWT validates the Bearer token (or ?token= query param for WebSocket) and sets
// userID, userRole, and the full user object in the Gin context.
// Also accepts X-API-Key header as an alternative to Bearer tokens.
func JWT(authSvc *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Try X-API-Key first (machine-to-machine)
		if apiKey := c.GetHeader("X-API-Key"); apiKey != "" {
			user, err := authSvc.ValidateAPIKey(apiKey)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid API key"})
				return
			}
			if !user.IsActive {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "account is disabled"})
				return
			}
			c.Set(UserIDKey, user.ID)
			c.Set(UserRoleKey, user.Role)
			c.Set(UserKey, user)
			c.Next()
			return
		}

		// 2. Bearer token
		var raw string
		if authHeader := c.GetHeader("Authorization"); authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format, expected: Bearer <token>"})
				return
			}
			raw = parts[1]
		} else if q := c.Query("token"); q != "" {
			// Fallback: ?token= for WebSocket upgrades (browsers cannot set custom headers)
			raw = q
		} else {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization required"})
			return
		}

		userID, role, err := authSvc.ValidateToken(raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set(UserIDKey, userID)
		c.Set(UserRoleKey, role)
		c.Next()
	}
}

// GetUserID retrieves the authenticated user ID from context.
func GetUserID(c *gin.Context) string {
	v, _ := c.Get(UserIDKey)
	if id, ok := v.(string); ok {
		return id
	}
	return ""
}

// GetUserRole retrieves the authenticated user's role from context.
func GetUserRole(c *gin.Context) string {
	v, _ := c.Get(UserRoleKey)
	if role, ok := v.(string); ok {
		return role
	}
	return "user"
}

// GetUser retrieves the full User object from context (only set for API key auth).
func GetUser(c *gin.Context) *models.User {
	v, _ := c.Get(UserKey)
	if u, ok := v.(*models.User); ok {
		return u
	}
	return nil
}

// RequireAdmin is a middleware that aborts with 403 unless the user has role "admin".
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role := GetUserRole(c)
		if role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin access required"})
			return
		}
		c.Next()
	}
}
