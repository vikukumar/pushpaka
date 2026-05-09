package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vikukumar/pushpaka/internal/middleware"
	"github.com/vikukumar/pushpaka/internal/services"
)

type PATHandler struct {
	patSvc *services.PATService
}

func NewPATHandler(patSvc *services.PATService) *PATHandler {
	return &PATHandler{patSvc: patSvc}
}

func (h *PATHandler) Create(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req services.CreatePATRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pat, plainToken, err := h.patSvc.Create(userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token": pat,
		"plain_token": plainToken, // ONLY SHOWN ONCE
	})
}

func (h *PATHandler) List(c *gin.Context) {
	userID := middleware.GetUserID(c)
	pats, err := h.patSvc.List(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tokens"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": pats})
}

func (h *PATHandler) Delete(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")

	if err := h.patSvc.Delete(id, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "token deleted"})
}
