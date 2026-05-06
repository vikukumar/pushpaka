package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/vikukumar/pushpaka/internal/middleware"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/internal/services"
	"github.com/vikukumar/pushpaka/pkg/models"
)

func NewRegistryHandler(repo *repositories.RegistryManagementRepository, projectSvc *services.ProjectService) *RegistryHandler {
	return &RegistryHandler{repo: repo, projectSvc: projectSvc}
}

type RegistryHandler struct {
	repo       *repositories.RegistryManagementRepository
	projectSvc *services.ProjectService
}

func (h *RegistryHandler) CreateRepo(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var repo models.RegistryRepo
	if err := c.ShouldBindJSON(&repo); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify project ownership
	if _, err := h.projectSvc.Get(repo.ProjectID, userID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied to project"})
		return
	}

	// Initialize ID and timestamps
	now := models.NowUTC()
	repo.ID = uuid.New().String()
	repo.CreatedAt = now.Time
	repo.UpdatedAt = now.Time

	if err := h.repo.CreateRepo(&repo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, repo)
}

func (h *RegistryHandler) ListRepos(c *gin.Context) {
	projectID := c.Query("project_id")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
		return
	}

	repos, err := h.repo.ListReposByProject(projectID)
	if err != nil {
		// Log the actual error to help debugging
		c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, repos)
}

func (h *RegistryHandler) DeleteRepo(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")

	repo, err := h.repo.GetRepo(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo not found"})
		return
	}

	// Verify project ownership
	if _, err := h.projectSvc.Get(repo.ProjectID, userID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied to project"})
		return
	}

	if err := h.repo.DeleteRepo(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *RegistryHandler) ListArtifacts(c *gin.Context) {
	repoID := c.Param("id")
	arts, err := h.repo.ListArtifacts(repoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, arts)
}

func (h *RegistryHandler) CreateReplication(c *gin.Context) {
	var rep models.RegistryReplication
	if err := c.ShouldBindJSON(&rep); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Initialize ID and timestamps
	now := models.NowUTC()
	rep.ID = uuid.New().String()
	rep.CreatedAt = now.Time
	rep.UpdatedAt = now.Time

	if err := h.repo.CreateReplication(&rep); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, rep)
}

func (h *RegistryHandler) TriggerSync(c *gin.Context) {
	id := c.Param("id")
	err := h.repo.UpdateReplicationStatusByRepoID(id, "idle", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "sync_triggered"})
}
