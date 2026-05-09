package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/vikukumar/pushpaka/internal/middleware"
	"github.com/vikukumar/pushpaka/internal/services"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type ProjectHandler struct {
	projectSvc *services.ProjectService
	auditSvc   *services.AuditService
}

func NewProjectHandler(projectSvc *services.ProjectService, auditSvc *services.AuditService) *ProjectHandler {
	return &ProjectHandler{projectSvc: projectSvc, auditSvc: auditSvc}
}

func (h *ProjectHandler) Create(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req models.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	project, err := h.projectSvc.Create(userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create project"})
		return
	}
	h.auditSvc.Log(userID, "create", "project", project.ID, map[string]any{"name": project.Name, "repo_url": project.RepoURL}, c.ClientIP(), c.Request.UserAgent())
	c.JSON(http.StatusCreated, project)
}

func (h *ProjectHandler) List(c *gin.Context) {
	userID := middleware.GetUserID(c)
	projectType := c.Query("type")
	if projectType == "" {
		projectType = "cd"
	}

	projects, err := h.projectSvc.ListByType(userID, projectType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch projects"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": projects})
}

func (h *ProjectHandler) Get(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")

	project, err := h.projectSvc.Get(id, userID)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch project"})
		return
	}
	c.JSON(http.StatusOK, project)
}

func (h *ProjectHandler) Delete(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")

	if err := h.projectSvc.Delete(id, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete project"})
		return
	}
	h.auditSvc.Log(userID, "delete", "project", id, nil, c.ClientIP(), c.Request.UserAgent())
	c.JSON(http.StatusOK, gin.H{"message": "project deleted"})
}

func (h *ProjectHandler) Update(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")
	var req models.UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	project, err := h.projectSvc.Update(id, userID, &req)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update project"})
		return
	}
	h.auditSvc.Log(userID, "update", "project", id, map[string]any{"name": project.Name}, c.ClientIP(), c.Request.UserAgent())
	c.JSON(http.StatusOK, project)
}
