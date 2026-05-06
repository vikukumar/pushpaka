package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimit is a simple in-memory rate limiter.
// In a production environment with multiple instances, this should ideally use Redis.
func RateLimit(key string) gin.HandlerFunc {
	var mu sync.Mutex
	clients := make(map[string]time.Time)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		mu.Lock()
		lastSeen, exists := clients[ip]
		now := time.Now()

		if exists && now.Sub(lastSeen) < 100*time.Millisecond {
			mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests",
			})
			return
		}

		clients[ip] = now
		mu.Unlock()
		c.Next()
	}
}
