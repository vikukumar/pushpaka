package services

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type RegistryService struct {
	cfg        *config.Config
	projectSvc *ProjectService
}

func NewRegistryService(cfg *config.Config, projectSvc *ProjectService) *RegistryService {
	return &RegistryService{
		cfg:        cfg,
		projectSvc: projectSvc,
	}
}

// HandleOCI implements the Docker Distribution V2 API
func (s *RegistryService) HandleOCI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	path := strings.TrimPrefix(r.URL.Path, "/registry/oci/")
	parts := strings.Split(path, "/")

	if len(parts) < 2 {
		if path == "" || path == "/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "Invalid OCI path", http.StatusBadRequest)
		return
	}

	// Format: <project_id>/<repo_name>/...
	projectID := parts[0]
	repoName := parts[1]

	// 1. Verify project exists and is a Registry project
	project, err := s.projectSvc.GetInternal(projectID)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	if project.Type != models.ProjectTypeRegistry {
		http.Error(w, "Project is not a registry project", http.StatusBadRequest)
		return
	}

	// 2. Auth check
	// Standard Docker clients use Basic Auth.
	// For now, we'll allow public reads if we implemented that logic,
	// but for writes we MUST auth.
	user, password, hasAuth := r.BasicAuth()
	if !hasAuth && r.Method != http.MethodGet {
		w.Header().Set("WWW-Authenticate", `Basic realm="Pushpaka Registry"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if hasAuth {
		// Verify credentials and project ownership
		if !s.verifyAccess(user, password, project) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Pushpaka Registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	subPath := strings.Join(parts[2:], "/")

	if strings.HasPrefix(subPath, "blobs/uploads/") {
		s.handleBlobUpload(w, r, projectID, repoName)
		return
	}
	if strings.HasPrefix(subPath, "blobs/") {
		s.handleBlobDownload(w, r, projectID, repoName)
		return
	}
	if strings.HasPrefix(subPath, "manifests/") {
		s.handleManifest(w, r, projectID, repoName)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}

func (s *RegistryService) handleBlobUpload(w http.ResponseWriter, r *http.Request, projectID, repoName string) {
	repoDir := filepath.Join(s.cfg.RegistryDir, projectID, repoName)
	uploadDir := filepath.Join(repoDir, "_uploads")
	os.MkdirAll(uploadDir, 0755)

	switch r.Method {
	case http.MethodPost:
		uploadID := uuid.New().String()
		w.Header().Set("Location", fmt.Sprintf("/registry/oci/%s/%s/blobs/uploads/%s", projectID, repoName, uploadID))
		w.WriteHeader(http.StatusAccepted)
	case http.MethodPatch:
		parts := strings.Split(r.URL.Path, "/")
		uploadID := parts[len(parts)-1]
		tmpFile := filepath.Join(uploadDir, uploadID)

		f, _ := os.OpenFile(tmpFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		defer f.Close()
		io.Copy(f, r.Body)

		w.Header().Set("Location", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	case http.MethodPut:
		parts := strings.Split(r.URL.Path, "/")
		uploadID := parts[len(parts)-1]
		digest := r.URL.Query().Get("digest")

		tmpFile := filepath.Join(uploadDir, uploadID)
		finalFile := filepath.Join(repoDir, "blobs", digest)
		os.MkdirAll(filepath.Dir(finalFile), 0755)

		os.Rename(tmpFile, finalFile)
		w.WriteHeader(http.StatusCreated)
	}
}

func (s *RegistryService) handleBlobDownload(w http.ResponseWriter, r *http.Request, projectID, repoName string) {
	parts := strings.Split(r.URL.Path, "/")
	digest := parts[len(parts)-1]

	blobFile := filepath.Join(s.cfg.RegistryDir, projectID, repoName, "blobs", digest)
	if _, err := os.Stat(blobFile); os.IsNotExist(err) {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, blobFile)
}

func (s *RegistryService) handleManifest(w http.ResponseWriter, r *http.Request, projectID, repoName string) {
	parts := strings.Split(r.URL.Path, "/")
	reference := parts[len(parts)-1] // tag or digest

	manifestDir := filepath.Join(s.cfg.RegistryDir, projectID, repoName, "manifests")
	os.MkdirAll(manifestDir, 0755)
	manifestFile := filepath.Join(manifestDir, reference)

	if r.Method == http.MethodGet {
		if _, err := os.Stat(manifestFile); os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
		http.ServeFile(w, r, manifestFile)
		return
	}

	if r.Method == http.MethodPut {
		data, _ := io.ReadAll(r.Body)
		os.WriteFile(manifestFile, data, 0644)

		// If it's a tag, also save by digest
		h := sha256.Sum256(data)
		digest := fmt.Sprintf("sha256:%x", h)
		os.WriteFile(filepath.Join(manifestDir, digest), data, 0644)

		w.Header().Set("Docker-Content-Digest", digest)
		w.WriteHeader(http.StatusCreated)
	}
}

// HandleBinary implements a simple artifact store for binaries
func (s *RegistryService) HandleBinary(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/registry/binary/")
	// Format: <project_id>/<repo_name>/<version>/<filename>
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid binary path. Expected /registry/binary/<project>/<repo>/<version>/<file>", http.StatusBadRequest)
		return
	}

	projectID, repoName, version, filename := parts[0], parts[1], parts[2], parts[3]
	filePath := filepath.Join(s.cfg.RegistryDir, "binaries", projectID, repoName, version, filename)

	if r.Method == http.MethodGet {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, filePath)
		return
	}

	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		os.MkdirAll(filepath.Dir(filePath), 0755)
		f, _ := os.Create(filePath)
		defer f.Close()
		io.Copy(f, r.Body)
		w.WriteHeader(http.StatusCreated)
	}
}

func (s *RegistryService) verifyAccess(username, password string, project *models.Project) bool {
	// 1. Try email/password
	authSvc := s.projectSvc.GetAuthService()
	if authSvc == nil {
		return false
	}

	loginResp, err := authSvc.Login(&models.LoginRequest{
		Email:    username,
		Password: password,
	})

	if err == nil && loginResp != nil {
		// User authenticated, check if they own the project or are admin
		if loginResp.User.ID == project.UserID || loginResp.User.Role == "admin" {
			return true
		}
	}

	// 2. Fallback to API Key / Token
	// (Implementation depends on if we want to support long-lived tokens for registry)

	return false
}
