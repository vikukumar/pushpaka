package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type RegistryHandler struct {
	repo *repositories.RegistryManagementRepository
}

func NewRegistryHandler(repo *repositories.RegistryManagementRepository) *RegistryHandler {
	return &RegistryHandler{repo: repo}
}

func (h *RegistryHandler) CreateRepo(c *gin.Context) {
	var repo models.RegistryRepo
	if err := c.ShouldBindJSON(&repo); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, repos)
}

func (h *RegistryHandler) DeleteRepo(c *gin.Context) {
	id := c.Param("id")
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
