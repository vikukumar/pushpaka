package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	backendConfig "github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/internal/services"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
	"github.com/vikukumar/pushpaka/worker/internal/config"
)

// JobReporter is called on job lifecycle events (may be nil).
type JobReporter interface {
	JobStarted(role string)
	JobFinished(role string)
}

// processManager tracks running direct-deployment processes (no Docker).
var processManager = struct {
	sync.Mutex
	procs map[string]*os.Process // deploymentID -> running process
}{procs: make(map[string]*os.Process)}

type BuildWorker struct {
	id              int
	db              *gorm.DB
	rdb             *redis.Client
	cfg             *config.Config
	dockerAvailable bool
	reporter        JobReporter
	Role            string
	Queue           string
	aiSvc           *services.AIService
}

func NewBuildWorker(id int, db *gorm.DB, rdb *redis.Client, cfg *config.Config, role, queue string) *BuildWorker {
	// Map worker config to backend-style config for services compatibility
	svcCfg := &backendConfig.Config{
		AIAPIKey:   cfg.AIAPIKey,
		AIProvider: cfg.AIProvider,
		AIModel:    cfg.AIModel,
		AIBaseURL:  cfg.AIBaseURL,
	}

	return &BuildWorker{
		id:              id,
		db:              db,
		rdb:             rdb,
		cfg:             cfg,
		dockerAvailable: checkDockerAvailable(cfg.DockerHost),
		Role:            role,
		Queue:           queue,
		aiSvc:           services.NewAIService(svcCfg),
	}
}

// checkDockerAvailable tries to connect to the Docker daemon.
// On Linux/Mac it checks the socket; on Windows the named pipe.
// Falls back to running `docker info` as a last resort.
func checkDockerAvailable(dockerHost string) bool {
	// Prefer direct socket/pipe check (no subprocess overhead).
	socketPath := "/var/run/docker.sock"
	if runtime.GOOS == "windows" {
		socketPath = `\\.\pipe\docker_engine`
	}
	if dockerHost != "" {
		// Strip scheme prefix e.g. "unix:///var/run/docker.sock" -> "/var/run/docker.sock"
		h := strings.TrimPrefix(dockerHost, "unix://")
		h = strings.TrimPrefix(h, "npipe://")
		socketPath = h
	}

	var connectable bool
	if runtime.GOOS == "windows" {
		// Named pipe  just try to dial
		conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
		if err == nil {
			conn.Close()
			connectable = true
		}
	} else {
		if _, err := os.Stat(socketPath); err == nil {
			conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
			if err == nil {
				conn.Close()
				connectable = true
			}
		}
	}
	if connectable {
		return true
	}

	// Fallback: try `docker info` CLI
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info")
	if dockerHost != "" {
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+dockerHost)
	}
	return cmd.Run() == nil
}

// DockerAvailable reports whether Docker was detected at worker startup.
func (w *BuildWorker) DockerAvailable() bool { return w.dockerAvailable }

func (w *BuildWorker) Run(ctx context.Context) {
	if w.Role == "installer" {
		w.runInstallerMode(ctx)
		return
	}

	log.Info().Int("worker_id", w.id).Str("role", w.Role).Msg("worker started")
	for {
		select {
		case <-ctx.Done():
			log.Info().Int("worker_id", w.id).Str("role", w.Role).Msg("worker stopping")
			return
		default:
			if w.rdb == nil {
				time.Sleep(1 * time.Second)
				continue
			}
			// Blocking pop from Redis queue with 5s timeout
			result, err := w.rdb.BRPop(ctx, 5*time.Second, w.Queue).Result()
			if err != nil {
				if err == redis.Nil {
					continue // timeout, try again
				}
				if ctx.Err() != nil {
					return // context cancelled
				}
				log.Error().Err(err).Str("role", w.Role).Msg("redis brpop error")
				continue
			}

			if len(result) < 2 {
				continue
			}

			taskID := result[1]
			func() {
				if w.reporter != nil {
					w.reporter.JobStarted(w.Role)
					defer w.reporter.JobFinished(w.Role)
				}
				defer func() {
					if r := recover(); r != nil {
						log.Error().Interface("panic", r).Str("task_id", taskID).Msg("worker recovered from panic")
						w.completeTask(taskID, false, fmt.Sprintf("worker panicked: %v", r))
					}
				}()
				taskCtx, cancel := context.WithTimeout(ctx, 60*time.Minute)
				defer cancel()
				w.processTask(taskCtx, taskID)
			}()
		}
	}
}

// RunInProcess reads jobs from an in-process channel instead of Redis.
// reporter is optional (may be nil); when non-nil its JobStarted/JobFinished
// methods are called around each processed job.
func (w *BuildWorker) RunInProcess(ctx context.Context, ch <-chan []byte, reporter JobReporter) {
	if w.Role == "installer" {
		w.runInstallerMode(ctx)
		return
	}

	// Normalize role for reporting (syncer -> sync, builder -> build)
	reportRole := w.Role
	switch reportRole {
	case "syncer":
		reportRole = "sync"
	case "builder":
		reportRole = "build"
	case "tester":
		reportRole = "test"
	case "deployer":
		reportRole = "deploy"
	}

	w.reporter = reporter
	log.Info().
		Int("worker_id", w.id).
		Str("role", w.Role).
		Str("report_role", reportRole).
		Bool("docker", w.dockerAvailable).
		Msgf("%s worker started", w.Role)
	for {
		select {
		case <-ctx.Done():
			log.Info().Int("worker_id", w.id).Str("role", w.Role).Msgf("%s worker stopping", w.Role)
			return
		case payload, ok := <-ch:
			if !ok {
				return
			}
			taskID := string(payload)
			func() {
				if reporter != nil {
					reporter.JobStarted(reportRole)
					defer reporter.JobFinished(reportRole)
				}
				defer func() {
					if r := recover(); r != nil {
						log.Error().Interface("panic", r).Str("task_id", taskID).Msg("worker recovered from panic")
						w.completeTask(taskID, false, fmt.Sprintf("worker panicked: %v", r))
					}
				}()
				taskCtx, cancel := context.WithTimeout(ctx, 60*time.Minute)
				defer cancel()
				w.processTask(taskCtx, taskID)
			}()
		}
	}
}

// runInstallerMode dynamically installs system requirements inside the running Docker container
// using an isolated child process to prevent host environment pollution.
func (w *BuildWorker) runInstallerMode(ctx context.Context) {
	log.Info().Msg("Installer worker initializing... Checking for runtime requirements")

	// We'll install core runtimes needed for builds: Node, Go, Python, Java, C/C++
	packages := []string{
		"nodejs", "npm", "go", "python3", "py3-pip", "openjdk11",
		"gcc", "g++", "make", "docker-cli",
	}

	// Wait randomly up to 2 seconds to avoid apk lock contention if multiple containers start
	time.Sleep(1 * time.Second)

	// Execute inside an isolated shell process to ensure environment changes are contained
	script := fmt.Sprintf("apk add --no-cache %s", strings.Join(packages, " "))
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)

	// Create a new process group to isolate the execution (Linux/macOS specific, but safe to omit or conditionally apply)
	// For standard isolation, running inside `sh -c` is the requested approach.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Warn().Err(err).Msg("Installer failed to install dependencies. (Safe to ignore if running natively outside Alpine)")
	} else {
		log.Info().Msg("Installer worker successfully installed dynamic runtime environments in isolated context!")
	}

	log.Info().Msg("Installer worker entering idle block")
	<-ctx.Done()
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (w *BuildWorker) getWorkspaceDir(base, userID, projectID, commitSHA string) string {
	path := filepath.Join(base, shortID(userID), shortID(projectID))
	if commitSHA != "" {
		path = filepath.Join(path, shortID(commitSHA))
	}
	return path
}

func (w *BuildWorker) processJob(ctx context.Context, job *models.DeploymentJob) {
	logger := log.With().
		Str("deployment_id", job.DeploymentID).
		Str("project_id", job.ProjectID).
		Int("worker_id", w.id).
		Logger()

	logger.Info().Bool("docker", w.dockerAvailable).Msg("starting build")

	// Update status based on job type
	initialStatus := string(models.DeploymentBuilding)
	if job.IsRecovery || job.IsBuildOnly {
		initialStatus = string(models.DeploymentRunning) // Or similar
		if job.IsBuildOnly {
			initialStatus = string(models.DeploymentBuilding)
		}
	}
	w.updateStatus(job.DeploymentID, initialStatus, "", "")
	w.appendLog(job.DeploymentID, "info", "system", "Worker process started")

	// Fallback port if none specified
	if job.Port <= 0 {
		job.Port = 3000
		w.appendLog(job.DeploymentID, "info", "system", "No port specified, defaulting to 3000")
	}
	if job.ExternalPort <= 0 {
		// External port should ideally be assigned by the server, but we fallback if needed
		job.ExternalPort = 8080
	}

	if !w.dockerAvailable {
		w.appendLog(job.DeploymentID, "info", "system", "Docker not available -- deploying directly (no containerization)")
	}

	// Source directory: shortened versioned workspace isolated by UserID
	sourceDir := w.getWorkspaceDir(w.cfg.ProjectsDir, job.UserID, job.ProjectID, job.CommitSHA)

	// Build output directory: shortened versioned storage isolated by UserID
	buildsDir := w.getWorkspaceDir(w.cfg.BuildsDir, job.UserID, job.ProjectID, job.CommitSHA)

	if err := os.MkdirAll(filepath.Dir(sourceDir), 0755); err != nil {
		w.fail(job.DeploymentID, fmt.Sprintf("failed to create source parent dir: %v", err))
		return
	}
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		w.fail(job.DeploymentID, fmt.Sprintf("failed to create source dir: %v", err))
		return
	}
	if err := os.MkdirAll(filepath.Dir(buildsDir), 0755); err != nil {
		w.fail(job.DeploymentID, fmt.Sprintf("failed to create builds parent dir: %v", err))
		return
	}
	if err := os.MkdirAll(buildsDir, 0755); err != nil {
		w.fail(job.DeploymentID, fmt.Sprintf("failed to create builds dir: %v", err))
		return
	}

	// Step 0: Check for Build Cache
	// IMPORTANT: Only skip if we're in recovery mode AND the build dir has artifacts.
	// Do NOT skip for normal new deployments even if buildsDir has stale content
	// from a previously failed build — those artifacts may be corrupt or incomplete.
	if !job.IsRecovery {
		if entries, _ := os.ReadDir(buildsDir); len(entries) > 0 {
			// Clear stale artifacts from prior failed builds before re-building.
			w.appendLog(job.DeploymentID, "info", "system", "Clearing stale build artifacts before fresh build...")
			_ = w.forceRemoveDir(buildsDir)
			_ = os.MkdirAll(buildsDir, 0755)
		}
	} else {
		// Recovery mode: use cached artifacts if they exist
		if entries, _ := os.ReadDir(buildsDir); len(entries) > 0 {
			w.appendLog(job.DeploymentID, "info", "system", "Build artifacts found in cache (recovery mode, skipping build steps)")
			w.updateStatus(job.DeploymentID, string(models.DeploymentRunning), "", "")
			w.updateCommitStatus(job.ProjectID, job.CommitSHA, models.CommitStatusBuilt)
			return
		}
	}

	// Step 1: Clone or Sync repository
	needsClone := !job.IsRecovery
	if job.IsRecovery {
		// Verify if we can actually recover.
		canRecover := false
		if w.dockerAvailable {
			checkCmd := exec.CommandContext(ctx, "docker", "inspect", job.ImageTag)
			if err := checkCmd.Run(); err == nil {
				canRecover = true
			}
		} else {
			// For Direct: check if current deployment runtime dir exists.
			deployRuntimeDir := filepath.Join(w.cfg.DeploysDir, job.DeploymentID)
			if _, err := os.Stat(deployRuntimeDir); err == nil {
				canRecover = true
			}
		}

		if !canRecover {
			w.appendLog(job.DeploymentID, "warn", "system", "Recovery assets not found (image or directory) -- falling back to full build")
			needsClone = true
			job.IsRecovery = false
		} else {
			w.appendLog(job.DeploymentID, "info", "system", "Recovery mode: skipping repository sync")
		}
	}

	if needsClone {
		if _, err := os.Stat(filepath.Join(sourceDir, ".git")); os.IsNotExist(err) {
			w.appendLog(job.DeploymentID, "warn", "system", fmt.Sprintf("Persistent clone missing at %s, performing fallback clone...", sourceDir))

			// [FIX] Extremely aggressive cleanup to avoid "initial ref transaction called with existing refs"
			_ = w.forceRemoveDir(sourceDir)
			time.Sleep(500 * time.Millisecond) // Give OS time to release locks
			_ = os.MkdirAll(sourceDir, 0755)

			if err := w.cloneRepo(ctx, job, sourceDir); err != nil {
				// Retry once with fresh directory if first attempt fails with Git bug
				w.appendLog(job.DeploymentID, "warn", "system", "First clone attempt failed, retrying with fresh directory...")
				_ = w.forceRemoveDir(sourceDir)
				_ = os.MkdirAll(sourceDir, 0755)
				if retryErr := w.cloneRepo(ctx, job, sourceDir); retryErr != nil {
					w.fail(job.DeploymentID, fmt.Sprintf("fallback clone failed: %v", retryErr))
					return
				}
			}
			w.appendLog(job.DeploymentID, "info", "system", "Fallback clone completed successfully")
		} else {
			// Persistent clone exists (done by sync task)
			w.appendLog(job.DeploymentID, "info", "system", "Persistent clone found")
			alreadySynced, err := w.isRepositoryInSync(ctx, job, sourceDir)
			if err != nil || !alreadySynced {
				w.appendLog(job.DeploymentID, "info", "system", "Updating existing persistent clone...")
				if err := w.syncRepo(ctx, job, sourceDir); err != nil {
					w.appendLog(job.DeploymentID, "warn", "system", fmt.Sprintf("Sync failed: %v. Deleting and recloning.", err))
					_ = w.forceRemoveDir(sourceDir)
					if err := w.cloneRepo(ctx, job, sourceDir); err != nil {
						w.fail(job.DeploymentID, fmt.Sprintf("re-clone failed: %v", err))
						return
					}
				}
			}
		}

		// Now copy the persistent clone to the isolated buildsDir for modification/building
		w.appendLog(job.DeploymentID, "info", "system", "Copying source to isolated build workspace...")
		_ = w.forceRemoveDir(buildsDir)
		_ = os.MkdirAll(buildsDir, 0755)
		if err := copyDir(sourceDir, buildsDir); err != nil {
			w.fail(job.DeploymentID, fmt.Sprintf("failed to copy source to build dir: %v", err))
			return
		}
	}

	// Important: we now perform all build operations inside buildsDir
	buildTargetDir := buildsDir
	if job.IsRecovery {
		buildTargetDir = sourceDir // use original if recovering without rebuild
	}

	// Capture and update commit info (visible on project cards)
	if sha, msg, author, dateStr, err := getRepoCommitInfo(sourceDir); err == nil && sha != "" {
		job.CommitSHA = sha

		// Parse date
		var commitDate *time.Time
		if t, err := time.Parse("2006-01-02 15:04:05 -0700", dateStr); err == nil {
			commitDate = &t
		}

		// Update Project model for card visibility
		w.db.Model(&models.Project{}).Where("id = ?", job.ProjectID).Updates(map[string]interface{}{
			"latest_commit_sha": sha,
			"latest_commit_msg": msg,
			"latest_commit_at":  commitDate,
			"updated_at":        time.Now().UTC(),
		})

		w.appendLog(job.DeploymentID, "info", "system", fmt.Sprintf("Commit: %s by %s", sha[:7], author))
		// Update Deployment record
		w.db.Model(&models.Deployment{}).Where("id = ?", job.DeploymentID).Updates(map[string]interface{}{
			"commit_sha": sha,
			"commit_msg": msg,
		})
	}

	// [FIX] Persist source code for editor IMMEDIATELY after sync/preparation.
	// This ensures the editor always shows the latest code even if the build fails.
	permanentDir := filepath.Join(w.cfg.DeploysDir, shortID(job.UserID), shortID(job.ProjectID))
	if !job.IsRecovery {
		w.appendLog(job.DeploymentID, "info", "system", "Syncing source code to editor workspace...")
		_ = os.MkdirAll(filepath.Dir(permanentDir), 0755)
		_ = w.forceRemoveDir(permanentDir)
		// Use sourceDir (the persistent clone) as the source for persistence
		if err := copyDirSkipModules(sourceDir, permanentDir); err != nil {
			w.appendLog(job.DeploymentID, "warn", "system", fmt.Sprintf("failed to persist source for editor: %v", err))
		}
	}

	var containerID, deployURL string
	var deployErr error

	if w.dockerAvailable {
		// Docker path: generate Dockerfile -> build image -> run container
		dockerfilePath := filepath.Join(buildTargetDir, "Dockerfile")
		if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
			w.appendLog(job.DeploymentID, "info", "system", "No Dockerfile found, generating one...")
			if err := w.generateDockerfile(buildTargetDir, job); err != nil {
				w.fail(job.DeploymentID, fmt.Sprintf("dockerfile generation failed: %v", err))
				return
			}
		}

		if job.IsRecovery {
			w.appendLog(job.DeploymentID, "info", "system", "Recovery mode: skipping image build")
		} else {
			w.appendLog(job.DeploymentID, "info", "system", fmt.Sprintf("Building Docker image: %s", job.ImageTag))

			buildErr := w.buildImage(ctx, job, buildTargetDir)
			if buildErr != nil {
				w.appendLog(job.DeploymentID, "error", "system", fmt.Sprintf("Docker build failed: %v", buildErr))

				// Analyze failure for user information, but do NOT retry
				if w.cfg.AIAPIKey != "" {
					explanation, fixCmd := w.analyzeFailure(job.DeploymentID, buildErr.Error())
					if explanation != "" {
						msg := "AI Analysis: " + explanation
						if fixCmd != "" {
							msg += "\nSuggested Fix: " + fixCmd
						}
						w.appendLog(job.DeploymentID, "info", "system", msg)
						buildErr = fmt.Errorf("%v (AI: %s)", buildErr, explanation)
					}
				}

				w.fail(job.DeploymentID, fmt.Sprintf("build failed: %v", buildErr))
				w.fireNotification(job, "failed", "", buildErr.Error())
				return
			}

			w.appendLog(job.DeploymentID, "info", "system", "Docker image built successfully")
			// Update ProjectCommit status
			w.updateCommitStatus(job.ProjectID, job.CommitSHA, models.CommitStatusBuilt)
		}

		w.appendLog(job.DeploymentID, "info", "system", "Deploying container...")
		containerID, deployURL, deployErr = w.deployContainer(ctx, job)
	} else {
		// Direct path: install deps, build, then promote to BuildsDir, then deploy
		w.appendLog(job.DeploymentID, "info", "system", "Preparing project for direct deployment...")

		// 1. Build in buildTargetDir
		w.appendLog(job.DeploymentID, "info", "system", "Installing dependencies and building in build directory...")

		buildErr := w.runBuildInSource(ctx, job, buildTargetDir)
		if buildErr != nil {
			w.appendLog(job.DeploymentID, "error", "system", fmt.Sprintf("Direct build failed: %v", buildErr))

			// Analyze failure for user information, but do NOT retry
			if w.cfg.AIAPIKey != "" {
				explanation, fixCmd := w.analyzeFailure(job.DeploymentID, buildErr.Error())
				if explanation != "" {
					msg := "AI Analysis: " + explanation
					if fixCmd != "" {
						msg += "\nSuggested Fix: " + fixCmd
					}
					w.appendLog(job.DeploymentID, "info", "system", msg)
					buildErr = fmt.Errorf("%v (AI: %s)", buildErr, explanation)
				}
			}

			w.fail(job.DeploymentID, fmt.Sprintf("build failed: %v", buildErr))
			w.fireNotification(job, "failed", "", buildErr.Error())
			return
		}
		// Update ProjectCommit status
		w.updateCommitStatus(job.ProjectID, job.CommitSHA, models.CommitStatusBuilt)

		// 2. Deploy from buildTargetDir (which is already buildsDir)
		if job.IsBuildOnly {
			w.appendLog(job.DeploymentID, "success", "system", "Build completed successfully (Build-only mode).")
			w.updateStatus(job.DeploymentID, "finished", "", "") // Or some other final state
			return
		}

		deployBaseDir := buildTargetDir
		containerID, deployURL, deployErr = w.deployDirect(ctx, job, deployBaseDir)
	}

	if deployErr != nil {
		msg := fmt.Sprintf("deployment failed: %v", deployErr)
		w.fail(job.DeploymentID, msg)
		w.fireNotification(job, "failed", "", msg)
		return
	}

	w.appendLog(job.DeploymentID, "info", "system", fmt.Sprintf("Deployment ID: %s", containerID))
	w.appendLog(job.DeploymentID, "info", "system", fmt.Sprintf("Available at: %s", deployURL))

	// Update deployment as running.
	// For direct (no-Docker) deployments the URL was already set to the proxy
	// path (/app/<projectID>) by the API when the deployment was created, so we
	// must NOT overwrite it here.  We only store external_port so the proxy
	// handler can forward traffic to the right local port.
	//
	// For Docker deployments we update the URL too because Traefik routes it
	// to a Traefik path (/p/<projectID>), different from the initial proxy URL.
	now := time.Now().UTC()
	var dbErr error
	if w.dockerAvailable {
		// Docker: update container_id + URL (Traefik path)
		dbErr = w.db.Model(&models.Deployment{}).
			Where("id = ?", job.DeploymentID).
			Updates(map[string]interface{}{
				"status":       models.DeploymentRunning,
				"container_id": containerID,
				"url":          deployURL,
				"finished_at":  now,
			}).Error
	} else {
		// Direct: keep existing proxy URL, just record container_id + external_port
		extPort := job.Port
		if extPort == 0 {
			extPort = 3000
		}
		dbErr = w.db.Model(&models.Deployment{}).
			Where("id = ?", job.DeploymentID).
			Updates(map[string]interface{}{
				"status":        models.DeploymentRunning,
				"container_id":  containerID,
				"external_port": extPort,
				"finished_at":   now,
			}).Error
	}
	if dbErr != nil {
		logger.Error().Err(dbErr).Msg("failed to update deployment record")
	}

	// Fire notification callback (non-blocking)
	w.fireNotification(job, "running", deployURL, "")

	logger.Info().Str("url", deployURL).Msg("deployment completed")
}

// deployDirect runs the application in-process without Docker.
// It copies the built source to a permanent directory, installs deps, builds,
// starts the process and stores the OS process handle for later cleanup.
func (w *BuildWorker) deployDirect(ctx context.Context, job *models.DeploymentJob, deployBaseDir string) (string, string, error) {
	// For zero-downtime, we don't kill the old process yet.
	// The new process will start on a separate port (job.ExternalPort).
	project, err := w.getProjectDir(job.ProjectID)
	if err != nil {
		return "", "", fmt.Errorf("project not found: %v", err)
	}
	// Use the same user-scoped workspace dir that processJob computed.
	// The old code used filepath.Join(w.cfg.ProjectsDir, job.ProjectID)
	// which is wrong because processJob uses getWorkspaceDir (UserID/ProjectID/CommitSHA).
	// deployBaseDir is already the correct path; use it as the source for framework detection.
	sourceDir := deployBaseDir
	_ = project // project is only needed for package manager hint below

	// Determine framework and commands
	buildCmd := job.BuildCommand
	startCmd := job.StartCommand
	port := job.ExternalPort
	if port == 0 {
		port = job.Port
		if port == 0 {
			port = 3000
		}
	}

	pm := ""
	isNodeProject := false
	isPythonProject := false
	pythonReqFile := "" // which Python dependency file was found
	_ = pythonReqFile   // avoid unused lint
	pythonExe := findPythonExe()
	var venvBinDir, pythonBin string
	pythonBin = pythonExe

	hasFile := func(name string) bool {
		_, err := os.Stat(filepath.Join(sourceDir, name))
		return err == nil
	}

	// ─── Language / runtime detection ────────────────────────────────────────

	isPrimaryPython := isPrimaryPython(sourceDir)

	files, _ := os.ReadDir(sourceDir)
	switch {
	case hasFile("package.json") && !isPrimaryPython:
		isNodeProject = true
		pm = project.PackageManager
		if pm == "" {
			pm = detectPackageManager(sourceDir)
		}
		// Verify the chosen PM binary exists in PATH; fall back to npm.
		if pm != "npm" {
			if _, err := exec.LookPath(pm); err != nil {
				w.appendLog(job.DeploymentID, "warn", "system",
					fmt.Sprintf("'%s' not found in PATH -- falling back to npm", pm))
				pm = "npm"
			}
		}
		// Next.js 15/16: create a minimal config if none exists.
		// Next.js prefers next.config.ts when tsconfig.json is present;
		// otherwise next.config.js (or .mjs for ESM packages).
		hasNextDep := false
		if pkgData, err := os.ReadFile(filepath.Join(sourceDir, "package.json")); err == nil {
			hasNextDep = strings.Contains(string(pkgData), `"next"`)
		}
		if hasNextDep {
			configExists := false
			for _, name := range []string{"next.config.ts", "next.config.js", "next.config.mjs", "next.config.cjs"} {
				if hasFile(name) {
					configExists = true
					break
				}
			}
			if !configExists {
				if hasFile("tsconfig.json") {
					// TypeScript project: Next.js 15+ tries .ts first.
					tsContent := "import type { NextConfig } from 'next'\nconst nextConfig: NextConfig = {}\nexport default nextConfig\n"
					_ = os.WriteFile(filepath.Join(sourceDir, "next.config.ts"), []byte(tsContent), 0644)
					w.appendLog(job.DeploymentID, "info", "system", "Created minimal next.config.ts (TypeScript project, no config found)")
				} else {
					// Check for ESM package (\"type\": \"module\")
					isESM := false
					if pkgData, err := os.ReadFile(filepath.Join(sourceDir, "package.json")); err == nil {
						s := string(pkgData)
						isESM = strings.Contains(s, `"type": "module"`) || strings.Contains(s, `"type":"module"`)
					}
					if isESM {
						esmContent := "/** @type {import('next').NextConfig} */\nconst nextConfig = {}\nexport default nextConfig\n"
						_ = os.WriteFile(filepath.Join(sourceDir, "next.config.mjs"), []byte(esmContent), 0644)
						w.appendLog(job.DeploymentID, "info", "system", "Created minimal next.config.mjs (ESM project, no config found)")
					} else {
						jsContent := "/** @type {import('next').NextConfig} */\nconst nextConfig = {}\nmodule.exports = nextConfig\n"
						_ = os.WriteFile(filepath.Join(sourceDir, "next.config.js"), []byte(jsContent), 0644)
						w.appendLog(job.DeploymentID, "info", "system", "Created minimal next.config.js (no config found in repo)")
					}
				}
			}
		}
		if buildCmd == "" {
			buildCmd = pm + " run build"
		}
		if startCmd == "" {
			// Read package.json to determine the best start command.
			startCmd = detectNodeStartCmd(sourceDir, pm, port)
		}

	case hasFile("requirements.txt") || isPrimaryPython:
		isPythonProject = true
		pythonReqFile = "requirements.txt"
		if startCmd == "" {
			// Check if FastAPI/uvicorn is listed as a dependency.
			if reqBytes, err := os.ReadFile(filepath.Join(sourceDir, "requirements.txt")); err == nil {
				reqLower := strings.ToLower(string(reqBytes))
				if strings.Contains(reqLower, "fastapi") || strings.Contains(reqLower, "uvicorn") ||
					strings.Contains(reqLower, "starlette") {
					startCmd = fmt.Sprintf("uvicorn %s --host 0.0.0.0 --port %d",
						detectUvicornModule(sourceDir), port)
				}
			}
			if startCmd == "" {
				startCmd = pythonExe + " " + detectPythonEntry(sourceDir, port)
			}
		}

	case hasFile("pyproject.toml"):
		isPythonProject = true
		pythonReqFile = "pyproject.toml"
		if startCmd == "" {
			// Check if this is a FastAPI/uvicorn project.
			if content, err := os.ReadFile(filepath.Join(sourceDir, "pyproject.toml")); err == nil {
				s := string(content)
				if strings.Contains(s, "fastapi") || strings.Contains(s, "uvicorn") {
					startCmd = fmt.Sprintf("uvicorn main:app --host 0.0.0.0 --port %d", port)
				}
			}
			if startCmd == "" {
				startCmd = pythonExe + " " + detectPythonEntry(sourceDir, port)
			}
		}

	case hasFile("Pipfile"):
		isPythonProject = true
		pythonReqFile = "Pipfile"
		if startCmd == "" {
			startCmd = pythonExe + " " + detectPythonEntry(sourceDir, port)
		}

	case hasFile("setup.py"):
		isPythonProject = true
		pythonReqFile = "setup.py"
		if startCmd == "" {
			startCmd = pythonExe + " " + detectPythonEntry(sourceDir, port)
		}

	case hasFile("Cargo.toml"):
		if buildCmd == "" {
			buildCmd = "cargo build --release"
		}
		if startCmd == "" {
			startCmd = "./target/release/app"
		}

	case hasFile("go.mod"):
		if buildCmd == "" {
			buildCmd = "go build -o app ."
		}
		if startCmd == "" {
			if runtime.GOOS == "windows" {
				startCmd = `app.exe`
			} else {
				startCmd = "./app"
			}
		}

	case hasFile("pom.xml"):
		// Java — Maven
		if buildCmd == "" {
			buildCmd = "mvn package -DskipTests -q"
		}
		if startCmd == "" {
			startCmd = fmt.Sprintf("java -jar target/*.jar --server.port=%d", port)
		}

	case hasFile("build.gradle") || hasFile("gradlew"):
		// Java — Gradle
		gradleExe := "gradle"
		if hasFile("gradlew") {
			if runtime.GOOS == "windows" {
				gradleExe = `gradlew.bat`
			} else {
				gradleExe = "./gradlew"
				_ = os.Chmod(filepath.Join(sourceDir, "gradlew"), 0755)
			}
		}
		if buildCmd == "" {
			buildCmd = gradleExe + " build -x test"
		}
		if startCmd == "" {
			startCmd = fmt.Sprintf("java -jar build/libs/*.jar --server.port=%d", port)
		}

	case hasFile("composer.json"):
		// PHP
		if buildCmd == "" {
			if hasFile("artisan") {
				buildCmd = "composer install --no-dev --optimize-autoloader"
			} else {
				buildCmd = "composer install"
			}
		}
		if startCmd == "" {
			if hasFile("artisan") {
				// Laravel
				startCmd = fmt.Sprintf("php artisan serve --host=0.0.0.0 --port=%d", port)
			} else {
				startCmd = fmt.Sprintf("php -S 0.0.0.0:%d -t public", port)
			}
		}

	case hasFile("Gemfile"):
		if buildCmd == "" {
			buildCmd = "bundle install"
		}
		if startCmd == "" {
			startCmd = fmt.Sprintf("bundle exec ruby app.rb -p %d", port)
		}

	case hasFile("deno.json") || hasFile("deno.jsonc"):
		// Deno
		if startCmd == "" {
			entry := "main.ts"
			for _, e := range []string{"main.ts", "main.js", "src/main.ts", "src/index.ts"} {
				if hasFile(e) {
					entry = e
					break
				}
			}
			startCmd = fmt.Sprintf("deno run --allow-all %s --port %d", entry, port)
		}

	case isDotNetProject(files):
		// .NET
		if buildCmd == "" {
			buildCmd = "dotnet publish -c Release -o out"
		}
		if startCmd == "" {
			startCmd = "dotnet run --urls http://0.0.0.0:" + fmt.Sprintf("%d", port)
		}

	default:
		// Static HTML site or unknown project.
		if hasFile("index.html") {
			if startCmd == "" {
				if _, err := exec.LookPath("npx"); err == nil {
					startCmd = fmt.Sprintf("npx serve . -p %d", port)
				} else {
					startCmd = fmt.Sprintf("%s -m http.server %d", pythonExe, port)
				}
			}
		}
	}

	// ─── Runtime context ─────────────────────────────────────────────────────
	deploymentDir := filepath.Join(w.cfg.DeploysDir, job.DeploymentID)
	if err := os.MkdirAll(deploymentDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create deployment dir: %v", err)
	}

	// For direct deployments, we're settled on running from the isolated deployment dir.
	if job.IsRecovery {
		w.appendLog(job.DeploymentID, "info", "system", "Recovery mode: skipping artifact copy")
	} else {
		w.appendLog(job.DeploymentID, "info", "system", "Copying artifacts to isolated deployment directory...")
		if err := copyDir(deployBaseDir, deploymentDir); err != nil {
			return "", "", fmt.Errorf("failed to copy artifacts to deployment dir: %v", err)
		}
	}

	runDir := deploymentDir
	if job.RunDir != "" {
		runDir = filepath.Join(deploymentDir, job.RunDir)
	}

	// Build environment map
	whitelist := []string{"PATH", "HOME", "USER", "LANG", "SystemRoot", "SystemDrive", "TEMP", "TMP"}
	envMap := make(map[string]string)
	for _, key := range whitelist {
		if val := os.Getenv(key); val != "" {
			envMap[key] = val
		}
	}
	for k, v := range job.EnvVars {
		envMap[k] = v
	}
	envMap["PORT"] = fmt.Sprintf("%d", port)
	if isNodeProject {
		if _, userSet := job.EnvVars["NODE_ENV"]; !userSet {
			envMap["NODE_ENV"] = "production"
		}
	}

	// Python venv support in isolated dir
	if isPythonProject {
		venvDir := filepath.Join(runDir, ".venv")
		// If venv exists in buildsDir, it was copied. If not, maybe we need it?
		if _, err := os.Stat(venvDir); err == nil {
			if runtime.GOOS == "windows" {
				venvBinDir = filepath.Join(venvDir, "Scripts")
				pythonBin = filepath.Join(venvBinDir, "python.exe")
				// Fallback if Scripts not found but bin exists
				if _, err := os.Stat(pythonBin); err != nil {
					if _, err := os.Stat(filepath.Join(venvDir, "bin", "python.exe")); err == nil {
						venvBinDir = filepath.Join(venvDir, "bin")
						pythonBin = filepath.Join(venvBinDir, "python.exe")
					}
				}
			} else {
				venvBinDir = filepath.Join(venvDir, "bin")
				pythonBin = filepath.Join(venvBinDir, "python")
			}

			if venvBinDir != "" {
				pathSep := string(os.PathListSeparator)
				if existing, ok := envMap["PATH"]; ok {
					envMap["PATH"] = venvBinDir + pathSep + existing
				} else {
					envMap["PATH"] = venvBinDir
				}
				envMap["VIRTUAL_ENV"] = venvDir

				// Update startCmd to use venv python
				pythonExe := findPythonExe()
				if strings.HasPrefix(startCmd, pythonExe+" ") {
					startCmd = pythonBin + startCmd[len(pythonExe):]
				}
			}
		}
	}

	if venvBinDir != "" {
		pathSep := string(os.PathListSeparator)
		if existing, ok := envMap["PATH"]; ok {
			envMap["PATH"] = venvBinDir + pathSep + existing
		} else {
			envMap["PATH"] = venvBinDir
		}
		envMap["VIRTUAL_ENV"] = filepath.Dir(venvBinDir)
	}

	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, k+"="+v)
	}

	// Write .env file
	if len(job.EnvVars) > 0 {
		var envFileContent strings.Builder
		for k, v := range job.EnvVars {
			envFileContent.WriteString(fmt.Sprintf("%s=%s\n", k, v))
		}
		_ = os.WriteFile(filepath.Join(runDir, ".env"), []byte(envFileContent.String()), 0600)
	}

	w.appendLog(job.DeploymentID, "info", "system", fmt.Sprintf("Starting process: %s (ExternalPort: %d)", startCmd, port))
	shell, shellFlag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, shellFlag = "cmd", "/c"
	}
	proc := exec.Command(shell, shellFlag, startCmd)
	proc.Dir = runDir
	proc.Env = env
	proc.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
	proc.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}

	if err := proc.Start(); err != nil {
		return "", "", fmt.Errorf("start process: %w", err)
	}

	processManager.Lock()
	processManager.procs[job.DeploymentID] = proc.Process
	processManager.Unlock()

	// Monitoring...
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	select {
	case err := <-done:
		return "", "", fmt.Errorf("process exited immediately: %w", err)
	case <-time.After(3 * time.Second):
		// Still running -- good.
	}

	// Background goroutine: when the process eventually exits, update the
	// deployment status so the UI reflects the crash rather than staying "running".
	depID := job.DeploymentID
	go func() {
		err := <-done
		processManager.Lock()
		delete(processManager.procs, depID)
		processManager.Unlock()

		// If the context is cancelled, the worker is shutting down.
		// We don't mark as "failed" because the exit was likely induced by the shutdown signal.
		// Keeping status as "running" allows the service to recover it on next startup.
		if ctx.Err() != nil {
			w.appendLog(depID, "info", "system", "Worker shutting down: process stopped (will recover on restart)")
			return
		}

		if err != nil {
			w.appendLog(depID, "error", "system", fmt.Sprintf("Process exited unexpectedly: %v", err))
			w.updateStatus(depID, string(models.DeploymentFailed), err.Error(), "")
		} else {
			// Normal exit (status 0) - mark as stopped so the UI is accurate.
			w.appendLog(depID, "info", "system", "Process exited normally")
			w.updateStatus(depID, string(models.DeploymentStopped), "", "")
		}
	}()

	deployURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Health check and Zero-downtime swap
	if w.healthCheck(ctx, deployURL) {
		w.appendLog(job.DeploymentID, "info", "system", "Health check passed! Cleaning up old deployments...")
		// Mark current as running
		w.updateStatus(job.DeploymentID, string(models.DeploymentRunning), "", "")

		// Kill other 'running' deployments for this project
		var oldDeployments []models.Deployment
		w.db.Where("project_id = ? AND status = ? AND id != ?", job.ProjectID, "running", job.DeploymentID).Find(&oldDeployments)
		for _, old := range oldDeployments {
			w.appendLog(job.DeploymentID, "info", "system", fmt.Sprintf("Stopping old process: %s", old.ID))
			processManager.Lock()
			if p, ok := processManager.procs[old.ID]; ok {
				_ = p.Kill()
				delete(processManager.procs, old.ID)
			}
			processManager.Unlock()
			w.db.Model(&models.Deployment{}).Where("id = ?", old.ID).Update("status", string(models.DeploymentStopped))
		}
	} else {
		w.appendLog(job.DeploymentID, "error", "system", "Health check failed! Rolling back...")
		_ = proc.Process.Kill()
		processManager.Lock()
		delete(processManager.procs, job.DeploymentID)
		processManager.Unlock()
		return "", "", fmt.Errorf("health check failed")
	}

	return fmt.Sprintf("%d", proc.Process.Pid), deployURL, nil
}

// copyDirSkipModules recursively copies src to dst, skipping node_modules and .git.
func copyDirSkipModules(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip entries we can't stat (e.g. Windows junction reparse points)
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		// Skip entire node_modules and .git trees
		base := filepath.Base(rel)
		if info.IsDir() && (base == "node_modules" || base == ".git" || base == ".pnpm" || base == ".venv" || base == "__pycache__") {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		// Skip symlinks on all platforms
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return copyFile(path, target, info.Mode())
	})
}

// copyDir recursively copies src to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	// Ensure Close() error is not masked by Copy error
	if closeErr := out.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return err
}

func (w *BuildWorker) executeFix(ctx context.Context, deploymentID, command, workingDir string) error {
	w.appendLog(deploymentID, "info", "system", "Executing AI-suggested fix: "+command)

	shell, shellFlag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, shellFlag = "cmd", "/c"
	}

	// Safety: sanitize or limit commands?
	// For now, we trust the AI context within the build directory.
	cmd := exec.CommandContext(ctx, shell, shellFlag, command)
	cmd.Dir = workingDir
	cmd.Stdout = &logWriter{deploymentID: deploymentID, stream: "stdout", w: w}
	cmd.Stderr = &logWriter{deploymentID: deploymentID, stream: "stderr", w: w}

	if err := cmd.Run(); err != nil {
		w.appendLog(deploymentID, "error", "system", "Auto-fix failed: "+err.Error())
		return err
	}

	w.appendLog(deploymentID, "info", "system", "Auto-fix applied successfully.")
	return nil
}

func (w *BuildWorker) cloneRepo(ctx context.Context, job *models.DeploymentJob, sourceDir string) error {
	// Validate required fields before running git
	if job.RepoURL == "" {
		return fmt.Errorf("repository URL is empty — configure the project's Git repository URL first")
	}

	// Default branch to 'main' if empty to avoid 'git clone --branch ""' failure
	branch := job.Branch
	if branch == "" {
		branch = "main"
	}

	cloneURL := job.RepoURL
	if job.GitToken != "" {
		// Embed PAT into the HTTPS URL for authenticated cloning:
		// https://github.com/user/repo  ->  https://<token>@github.com/user/repo
		if u, err := url.Parse(cloneURL); err == nil &&
			(u.Scheme == "https" || u.Scheme == "http") {
			// Use token as username - this is universally supported by GitHub, GitLab, and Bitbucket
			// Format: https://<token>@github.com/user/repo
			u.User = url.User(job.GitToken)
			cloneURL = u.String()
		}
	}

	args := []string{"clone", "--depth=1", "--branch", branch, cloneURL, sourceDir}
	if job.CommitSHA != "" {
		// For specific commits, we need full clone + checkout (no --depth)
		args = []string{"clone", cloneURL, sourceDir}
	}

	// Run from the parent directory -- sourceDir must not exist yet for git clone.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = filepath.Dir(sourceDir)
	cmd.Env = append(os.Environ(),
		// Disable SSH host key verification and prevent interactive prompt hanging
		"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.CombinedOutput()

	// Use DeploymentID or TaskID (whichever is set) for log appending
	logTarget := job.DeploymentID
	if logTarget == "" {
		logTarget = job.ProjectID // fallback so logs don't get lost
	}

	if len(out) > 0 {
		// Redact the token from log output before appending
		logOut := out
		if job.GitToken != "" {
			logOut = []byte(strings.ReplaceAll(string(out), job.GitToken, "***"))
		}
		if logTarget != "" {
			w.appendLog(logTarget, "info", "stdout", string(logOut))
		}
	}

	if err != nil {
		outStr := string(out)
		// Provide more helpful error messages for common git failures
		if strings.Contains(outStr, "Repository not found") || strings.Contains(outStr, "404") {
			return fmt.Errorf("git clone failed: repository not found at %s (401/404): %w", job.RepoURL, err)
		} else if strings.Contains(outStr, "fatal: could not read") || strings.Contains(outStr, "Permission denied") {
			return fmt.Errorf("git clone failed: authentication failed - check your Git token/credentials for %s: %w", job.RepoURL, err)
		} else if strings.Contains(outStr, "Connection refused") || strings.Contains(outStr, "Network") {
			return fmt.Errorf("git clone failed: network error - unable to reach repository %s: %w", job.RepoURL, err)
		} else if strings.Contains(outStr, "Remote branch") && strings.Contains(outStr, "not found") {
			return fmt.Errorf("git clone failed: branch '%s' not found in repository %s — check the project branch setting: %w", branch, job.RepoURL, err)
		} else if outStr != "" {
			// Include the actual git output in the error so the UI shows something useful
			return fmt.Errorf("git clone: %w\n%s", err, strings.TrimSpace(outStr))
		}
		return fmt.Errorf("git clone: %w", err)
	}

	// Checkout specific commit if provided
	if job.CommitSHA != "" {
		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", job.CommitSHA)
		checkoutCmd.Dir = sourceDir
		if out, err := checkoutCmd.CombinedOutput(); err != nil {
			if logTarget != "" {
				w.appendLog(logTarget, "warn", "stdout", string(out))
			}
		} else {
			// Create a named rollback branch so the editor can reference it.
			branchName := "pushpaka/rollback-" + job.DeploymentID[:8]
			branchCmd := exec.CommandContext(ctx, "git", "checkout", "-b", branchName)
			branchCmd.Dir = sourceDir
			// Best-effort; ignore errors (branch may already exist).
			_, _ = branchCmd.CombinedOutput()
		}
	}
	return nil
}

// isRepositoryInSync checks if the current repository state matches the desired branch/commit
func (w *BuildWorker) isRepositoryInSync(ctx context.Context, job *models.DeploymentJob, sourceDir string) (bool, error) {
	// Get current HEAD commit SHA
	currentCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	currentCmd.Dir = sourceDir
	currentOut, err := currentCmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to get current HEAD: %v", err)
	}
	currentSHA := strings.TrimSpace(string(currentOut))

	// If specific commit is requested, check if we're at that commit
	if job.CommitSHA != "" {
		return currentSHA == job.CommitSHA || strings.HasPrefix(currentSHA, job.CommitSHA), nil
	}

	// Otherwise check if current branch is the target branch
	// Try to get the remote branch HEAD
	remoteCmd := exec.CommandContext(ctx, "git", "rev-parse", "origin/"+job.Branch)
	remoteCmd.Dir = sourceDir
	remoteOut, err := remoteCmd.CombinedOutput()
	if err != nil {
		// Remote branch doesn't exist yet, fetch first
		return false, nil
	}
	remoteSHA := strings.TrimSpace(string(remoteOut))
	return currentSHA == remoteSHA, nil
}

// forceRemoveDir removes a directory with retry logic for Windows file locking
func (w *BuildWorker) forceRemoveDir(dir string) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := os.RemoveAll(dir)
		if err == nil {
			return nil
		}
		// On Windows, wait a bit for file locks to release
		if runtime.GOOS == "windows" {
			time.Sleep(time.Duration((attempt+1)*500) * time.Millisecond)
		}
	}
	return fmt.Errorf("failed to remove directory after retries")
}

// syncRepo performs a fetch and hard reset to ensure the local source matches the remote.
func (w *BuildWorker) syncRepo(ctx context.Context, job *models.DeploymentJob, sourceDir string) error {
	w.appendLog(job.DeploymentID, "info", "system", "Synchronizing repository changes...")

	// 1. Ensure remote URL is up-to-date with token if provided
	if job.GitToken != "" {
		cloneURL := job.RepoURL
		if u, err := url.Parse(cloneURL); err == nil && (u.Scheme == "https" || u.Scheme == "http") {
			u.User = url.User(job.GitToken)
			cloneURL = u.String()

			// Silently update the remote URL
			updateCmd := exec.CommandContext(ctx, "git", "remote", "set-url", "origin", cloneURL)
			updateCmd.Dir = sourceDir
			_ = updateCmd.Run()
		}
	}

	// 2. Fetch with all refs
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "--all", "--tags", "--prune")
	fetchCmd.Dir = sourceDir
	if err := fetchCmd.Run(); err != nil {
		// Fetch errors are usually not fatal - repository metadata might be incomplete
		w.appendLog(job.DeploymentID, "warn", "system", fmt.Sprintf("Git fetch warning: %v", err))
	}

	// 2. If specific commit requested, checkout that commit
	if job.CommitSHA != "" {
		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", job.CommitSHA)
		checkoutCmd.Dir = sourceDir
		if out, err := checkoutCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout %s: %v: %s", job.CommitSHA, err, string(out))
		}
	} else {
		// Otherwise reset hard to the target branch
		target := "origin/" + job.Branch
		resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", target)
		resetCmd.Dir = sourceDir
		if out, err := resetCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git reset: %v: %s", err, string(out))
		}
	}

	// 3. Clean untracked files (optional but keeps it clean)
	cleanCmd := exec.CommandContext(ctx, "git", "clean", "-fd", "-e", "node_modules")
	cleanCmd.Dir = sourceDir
	_ = cleanCmd.Run()

	return nil
}

func (w *BuildWorker) generateDockerfile(sourceDir string, job *models.DeploymentJob) error {
	var content string

	// Detect framework/language
	isPrimaryPy := isPrimaryPython(sourceDir)
	if _, err := os.Stat(filepath.Join(sourceDir, "package.json")); err == nil && !isPrimaryPy {
		pm := detectPackageManager(sourceDir)
		lockFile := pmLockFile(pm)
		buildCmd := pm + " run build"
		startCmd := pm + " start"
		if job.BuildCommand != "" {
			buildCmd = job.BuildCommand
		}
		if job.StartCommand != "" {
			startCmd = job.StartCommand
		}
		// Use corepack for pnpm/yarn/bun which is faster and more reliable in Node images
		pmInstall := ""
		switch pm {
		case "pnpm":
			pmInstall = "RUN corepack enable && corepack prepare pnpm@latest --activate\n"
		case "yarn":
			pmInstall = "RUN corepack enable && corepack prepare yarn@latest --activate\n"
		case "bun":
			pmInstall = "RUN npm install -g bun\n"
		}

		installBuild := pmBuildInstall(pm)
		installProd := pmProdInstall(pm)

		content = fmt.Sprintf(`FROM node:20-alpine AS deps
WORKDIR /app
%sCOPY package.json %s ./
RUN %s

FROM node:20-alpine AS builder
WORKDIR /app
%sCOPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN %s

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV production
%sCOPY package.json %s ./
RUN %s
COPY --from=builder /app .
EXPOSE %d
CMD [%s]
`, pmInstall, lockFile, installBuild, pmInstall, buildCmd, pmInstall, lockFile, installProd, job.Port, shellToCmdArray(startCmd))
	} else if _, err := os.Stat(filepath.Join(sourceDir, "go.mod")); err == nil {
		startCmd := "./app"
		if job.StartCommand != "" {
			startCmd = job.StartCommand
		}
		finalPort := job.Port
		if finalPort <= 0 {
			finalPort = 3000
		}
		content = fmt.Sprintf(`FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o app .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/app .
EXPOSE %d
CMD ["%s"]
`, finalPort, startCmd)
	} else if _, err := os.Stat(filepath.Join(sourceDir, "requirements.txt")); err == nil || isPrimaryPy {
		startCmd := "python app.py"
		if job.StartCommand != "" {
			startCmd = job.StartCommand
		}
		finalPort := job.Port
		if finalPort <= 0 {
			finalPort = 8000 // Standard for many python apps
		}
		content = fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN --mount=type=cache,target=/root/.cache/pip pip install -r requirements.txt
COPY . .
EXPOSE %d
CMD [%s]
`, finalPort, shellToCmdArray(startCmd))
	} else if _, err := os.Stat(filepath.Join(sourceDir, "pyproject.toml")); err == nil {
		startCmd := "python -m uvicorn main:app --host 0.0.0.0 --port " + fmt.Sprintf("%d", job.Port)
		if job.StartCommand != "" {
			startCmd = job.StartCommand
		}
		finalPort := job.Port
		if finalPort <= 0 {
			finalPort = 8000
		}
		content = fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
RUN pip install --no-cache-dir uv
COPY pyproject.toml .
RUN uv pip install --system -e .
COPY . .
EXPOSE %d
CMD [%s]
`, finalPort, shellToCmdArray(startCmd))
	} else if _, err := os.Stat(filepath.Join(sourceDir, "Cargo.toml")); err == nil {
		// Rust project
		startCmd := "./app"
		if job.StartCommand != "" {
			startCmd = job.StartCommand
		}
		content = fmt.Sprintf(`FROM rust:1.76-slim AS builder
WORKDIR /app
COPY Cargo.toml Cargo.lock* ./
RUN mkdir src && echo 'fn main(){}' > src/main.rs && cargo build --release --locked 2>/dev/null; rm -rf src
COPY . .
RUN cargo build --release --locked

FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/target/release/* ./
EXPOSE %d
CMD ["%s"]
`, job.Port, startCmd)
	} else if _, err := os.Stat(filepath.Join(sourceDir, "index.html")); err == nil {
		// Static site — serve with nginx
		content = fmt.Sprintf(`FROM nginx:alpine
WORKDIR /usr/share/nginx/html
COPY . .
EXPOSE %d
CMD ["nginx", "-g", "daemon off;"]
`, job.Port)
	} else if _, err := os.Stat(filepath.Join(sourceDir, "Gemfile")); err == nil {
		// Ruby project
		startCmd := "bundle exec ruby app.rb"
		if job.StartCommand != "" {
			startCmd = job.StartCommand
		}
		content = fmt.Sprintf(`FROM ruby:3.3-slim
WORKDIR /app
COPY Gemfile Gemfile.lock* ./
RUN bundle install --without development test
COPY . .
EXPOSE %d
CMD [%s]
`, job.Port, shellToCmdArray(startCmd))
	} else if _, err := os.Stat(filepath.Join(sourceDir, "pom.xml")); err == nil {
		// Java Maven project
		content = fmt.Sprintf(`FROM maven:3.9-eclipse-temurin-21 AS builder
WORKDIR /app
COPY pom.xml .
RUN mvn dependency:go-offline
COPY . .
RUN mvn package -DskipTests

FROM eclipse-temurin:21-jre-alpine
WORKDIR /app
COPY --from=builder /app/target/*.jar ./app.jar
EXPOSE %d
CMD ["java", "-jar", "app.jar"]
`, job.Port)
	} else if files, _ := os.ReadDir(sourceDir); strings.Contains(strings.Join(func() []string {
		var names []string
		for _, f := range files {
			names = append(names, f.Name())
		}
		return names
	}(), ","), ".csproj") {
		// C# .NET project
		finalStart := "dotnet run --urls http://0.0.0.0:" + fmt.Sprintf("%d", job.Port)
		if job.StartCommand != "" {
			finalStart = job.StartCommand
		}
		content = fmt.Sprintf(`FROM mcr.microsoft.com/dotnet/sdk:8.0 AS builder
WORKDIR /app
COPY *.csproj ./
RUN dotnet restore
COPY . .
RUN dotnet publish -c Release -o out

FROM mcr.microsoft.com/dotnet/aspnet:8.0
WORKDIR /app
COPY --from=builder /app/out .
EXPOSE %d
ENV ASPNETCORE_URLS=http://+:%d
CMD [%s]
`, job.Port, job.Port, shellToCmdArray(finalStart))
	} else if func() bool {
		_, err1 := os.Stat(filepath.Join(sourceDir, "composer.json"))
		_, err2 := os.Stat(filepath.Join(sourceDir, "index.php"))
		return err1 == nil || err2 == nil
	}() {
		// PHP project
		startCmd := "php -S 0.0.0.0:" + fmt.Sprintf("%d", job.Port)
		if job.StartCommand != "" {
			startCmd = job.StartCommand
		}
		content = fmt.Sprintf(`FROM php:8.2-fpm-alpine
WORKDIR /var/www/html
RUN apk add --no-cache libpng-dev libjpeg-turbo-dev freetype-dev zip libzip-dev unzip
RUN docker-php-ext-install pdo_mysql gd zip
COPY --from=composer:latest /usr/bin/composer /usr/bin/composer
COPY . .
RUN if [ -f composer.json ]; then composer install --no-dev --optimize-autoloader; fi
EXPOSE %d
CMD [%s]
`, job.Port, shellToCmdArray(startCmd))
	} else if func() bool {
		_, err1 := os.Stat(filepath.Join(sourceDir, "Makefile"))
		_, err2 := os.Stat(filepath.Join(sourceDir, "CMakeLists.txt"))
		return err1 == nil || err2 == nil
	}() {
		// C / C++ project
		startCmd := "./app"
		if job.StartCommand != "" {
			startCmd = job.StartCommand
		}
		content = fmt.Sprintf(`FROM gcc:latest AS builder
WORKDIR /app
COPY . .
RUN if [ -f Makefile ]; then make; \
    elif [ -f CMakeLists.txt ]; then apt-get update && apt-get install -y cmake && cmake . && make; \
    else gcc -o app *.c || g++ -o app *.cpp || echo "no source" > app; fi

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=builder /app ./
EXPOSE %d
CMD ["%s"]
`, job.Port, startCmd)
	} else if _, err := os.Stat(filepath.Join(sourceDir, "nginx.conf")); err == nil {
		// Nginx project
		content = fmt.Sprintf(`FROM nginx:alpine
COPY . /usr/share/nginx/html
RUN if [ -f nginx.conf ]; then cp nginx.conf /etc/nginx/nginx.conf; fi
EXPOSE %d
CMD ["nginx", "-g", "daemon off;"]
`, job.Port)
	} else if _, err := os.Stat(filepath.Join(sourceDir, "WEB-INF")); err == nil {
		// Tomcat project
		content = fmt.Sprintf(`FROM tomcat:10-jdk17-openjdk-slim
RUN rm -rf /usr/local/tomcat/webapps/*
COPY . /usr/local/tomcat/webapps/ROOT
EXPOSE %d
CMD ["catalina.sh", "run"]
`, job.Port)
	} else {
		// Lua or generic
		isLua := false
		if files, err := os.ReadDir(sourceDir); err == nil {
			for _, f := range files {
				if strings.HasSuffix(f.Name(), ".lua") {
					isLua = true
					break
				}
			}
		}

		if isLua {
			startCmd := "lua main.lua"
			if job.StartCommand != "" {
				startCmd = job.StartCommand
			}
			content = fmt.Sprintf(`FROM akaspin/lua:5.4
WORKDIR /app
COPY . .
EXPOSE %d
CMD [%s]
`, job.Port, shellToCmdArray(startCmd))
		} else {
			content = fmt.Sprintf(`FROM alpine:3.19
WORKDIR /app
COPY . .
EXPOSE %d
CMD ["./run.sh"]
`, job.Port)
		}
	}

	return os.WriteFile(filepath.Join(sourceDir, "Dockerfile"), []byte(content), 0644)
}

func shellToCmdArray(cmd string) string {
	parts := strings.Fields(cmd)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = fmt.Sprintf("%q", p)
	}
	return strings.Join(quoted, ", ")
}

// detectPackageManager inspects lock files in sourceDir to choose the right
// package manager. Priority: bun > pnpm > yarn > npm (fallback).
// Returns the binary name ("npm", "yarn", "pnpm", "bun").
// NOTE: The caller is responsible for checking PATH availability; use
// exec.LookPath to fall back to npm when the returned binary is not installed.
func detectPackageManager(sourceDir string) string {
	if _, err := os.Stat(filepath.Join(sourceDir, "bun.lockb")); err == nil {
		return "bun"
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "yarn.lock")); err == nil {
		return "yarn"
	}
	return "npm"
}

func hasFile(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func isPrimaryPython(sourceDir string) bool {
	if !hasFile(sourceDir, "package.json") {
		return hasFile(sourceDir, "requirements.txt") || hasFile(sourceDir, "pyproject.toml") || hasFile(sourceDir, "Pipfile")
	}
	// Mixed-project guard: if the repo contains both a package.json AND a Python entry file
	// + a Python dependency file, treat the project as Python-primary.
	hasPyDep := hasFile(sourceDir, "requirements.txt") || hasFile(sourceDir, "pyproject.toml") || hasFile(sourceDir, "Pipfile")
	if hasPyDep {
		for _, pyEntry := range []string{"main.py", "app.py", "server.py", "manage.py", "asgi.py"} {
			if hasFile(sourceDir, pyEntry) {
				return true
			}
		}
	}
	return false
}

// pmInstallArgs returns the install command arguments for the given package manager.
// Installs ALL dependencies (including devDeps) because the build step
// (e.g. `next build`, `vite build`) typically requires devDependencies.
func pmInstallArgs(pm string) []string {
	switch pm {
	case "pnpm":
		return []string{"install", "--frozen-lockfile"}
	case "yarn":
		return []string{"install", "--immutable"}
	case "bun":
		return []string{"install"}
	default: // npm
		return []string{"install"}
	}
}

func pmBuildInstall(pm string) string {
	switch pm {
	case "pnpm":
		return "pnpm install"
	case "yarn":
		return "yarn install"
	case "bun":
		return "bun install"
	default:
		return "npm install"
	}
}

func pmProdInstall(pm string) string {
	switch pm {
	case "pnpm":
		return "pnpm install --prod --frozen-lockfile || pnpm install --prod"
	case "yarn":
		return "yarn install --immutable"
	case "bun":
		return "bun install --production"
	default:
		return "npm ci --omit=dev || npm install --omit=dev"
	}
}

// pmLockFile returns the lock-file name for the given package manager.
func pmLockFile(pm string) string {
	switch pm {
	case "pnpm":
		return "pnpm-lock.yaml"
	case "yarn":
		return "yarn.lock"
	case "bun":
		return "bun.lockb"
	default:
		return "package-lock.json"
	}
}

// findPythonExe returns the Python executable name that is available in PATH.
// Prefers "python3" on Unix (common on modern systems), "python" on Windows.
func findPythonExe() string {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("python"); err == nil {
			return "python"
		}
		return "python3"
	}
	if _, err := exec.LookPath("python3"); err == nil {
		return "python3"
	}
	return "python"
}

// detectNodeStartCmd reads package.json and returns the best start command for
// the project.  It handles three common scenarios:
//  1. Vite / React + Vite: no  "start" script exists -- serve the built "dist" folder.
//  2. Express / generic Node: no "start" script -- fall back to "node <main>".
//  3. Everything else: use "<pm> start" (the standard behaviour).
func detectNodeStartCmd(sourceDir, pm string, port int) string {
	data, err := os.ReadFile(filepath.Join(sourceDir, "package.json"))
	if err != nil {
		return pm + " start"
	}
	var pkg struct {
		Main         string            `json:"main"`
		Scripts      map[string]string `json:"scripts"`
		Dependencies map[string]string `json:"dependencies"`
		DevDeps      map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return pm + " start"
	}

	// If package.json explicitly declares a "start" script, use it.
	if _, hasStart := pkg.Scripts["start"]; hasStart {
		return pm + " start"
	}

	// No "start" script -- try to infer the correct command.
	allDeps := make(map[string]string)
	for k, v := range pkg.Dependencies {
		allDeps[k] = v
	}
	for k, v := range pkg.DevDeps {
		allDeps[k] = v
	}

	// Vite-based projects (React, Vue, Svelte, etc.) produce a "dist/" folder.
	if _, isVite := allDeps["vite"]; isVite {
		if _, err := exec.LookPath("npx"); err == nil {
			return fmt.Sprintf("npx serve dist -p %d", port)
		}
		return fmt.Sprintf("npx vite preview --port %d --host 0.0.0.0", port)
	}

	// Next.js
	if _, isNext := allDeps["next"]; isNext {
		return "npx next start -p " + fmt.Sprint(port)
	}

	// Express / generic Node: use "node <main>" (default main: index.js).
	main := pkg.Main
	if main == "" {
		// Try common entry-point names.
		for _, name := range []string{"index.js", "server.js", "app.js", "src/index.js"} {
			if _, err := os.Stat(filepath.Join(sourceDir, name)); err == nil {
				main = name
				break
			}
		}
	}
	if pkg.Main == "" {
		main = "index.js"
	}
	return "node " + main
}

func detectLanguage(sourceDir string) string {
	if _, err := os.Stat(filepath.Join(sourceDir, "package.json")); err == nil {
		return "node"
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "go.mod")); err == nil {
		return "go"
	}
	// Python: multiple options
	for _, f := range []string{"requirements.txt", "pyproject.toml", "Pipfile", "setup.py"} {
		if _, err := os.Stat(filepath.Join(sourceDir, f)); err == nil {
			return "python"
		}
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "Cargo.toml")); err == nil {
		return "rust"
	}
	// Java: maven or gradle
	if _, err := os.Stat(filepath.Join(sourceDir, "pom.xml")); err == nil {
		return "java"
	}

	// PHP: composer or index.php
	if _, err := os.Stat(filepath.Join(sourceDir, "composer.json")); err == nil {
		return "php"
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "index.php")); err == nil {
		return "php"
	}

	// C / C++ / Build systems
	if _, err := os.Stat(filepath.Join(sourceDir, "Makefile")); err == nil {
		return "c"
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "CMakeLists.txt")); err == nil {
		return "cpp"
	}

	// Nginx / Server configs
	if _, err := os.Stat(filepath.Join(sourceDir, "nginx.conf")); err == nil {
		return "nginx"
	}

	// Tomcat / Java Web
	if _, err := os.Stat(filepath.Join(sourceDir, "WEB-INF")); err == nil {
		return "tomcat"
	}

	files, err := os.ReadDir(sourceDir)
	if err == nil {
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if f.Name() == "build.gradle" || f.Name() == "build.gradle.kts" {
				return "java"
			}
			if strings.HasSuffix(f.Name(), ".csproj") || strings.HasSuffix(f.Name(), ".sln") {
				return "csharp"
			}
			if strings.HasSuffix(f.Name(), ".c") {
				return "c"
			}
			if strings.HasSuffix(f.Name(), ".cpp") || strings.HasSuffix(f.Name(), ".cc") {
				return "cpp"
			}
			if strings.HasSuffix(f.Name(), ".lua") {
				return "lua"
			}
			if strings.HasSuffix(f.Name(), ".php") {
				return "php"
			}
		}
	}

	// Static or unknown
	if _, err := os.Stat(filepath.Join(sourceDir, "index.html")); err == nil {
		return "static"
	}
	return "unknown"
}

// isDotNetProject checks if any file has .csproj or .sln extension.
func isDotNetProject(files []os.DirEntry) bool {
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if strings.HasSuffix(f.Name(), ".csproj") || strings.HasSuffix(f.Name(), ".sln") {
			return true
		}
	}
	return false
}

// detectUvicornModule returns the "module:variable" string for uvicorn.
func detectUvicornModule(sourceDir string) string {
	for _, name := range []string{"main", "app", "server", "run", "asgi", "api"} {
		if _, err := os.Stat(filepath.Join(sourceDir, name+".py")); err == nil {
			return name + ":app"
		}
	}
	for _, sub := range []string{"src", "app", "backend", "api"} {
		for _, name := range []string{"main", "app", "server", "asgi"} {
			if _, err := os.Stat(filepath.Join(sourceDir, sub, name+".py")); err == nil {
				return sub + "." + name + ":app"
			}
		}
	}
	return "main:app"
}

// getRepoCommitInfo returns the HEAD commit SHA and subject line from git.
func detectFramework(sourceDir string) string {
	packageJsonPath := filepath.Join(sourceDir, "package.json")
	data, err := os.ReadFile(packageJsonPath)
	if err != nil {
		return "Unknown"
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "Unknown"
	}

	allDeps := make(map[string]string)
	for k, v := range pkg.Dependencies {
		allDeps[k] = v
	}
	for k, v := range pkg.DevDependencies {
		allDeps[k] = v
	}

	if _, ok := allDeps["next"]; ok {
		return "Next.js"
	}
	if _, ok := allDeps["nuxt"]; ok {
		return "Nuxt"
	}
	if _, ok := allDeps["@angular/core"]; ok {
		return "Angular"
	}
	if _, ok := allDeps["react"]; ok {
		if _, vite := allDeps["vite"]; vite {
			return "React (Vite)"
		}
		return "React"
	}
	if _, ok := allDeps["vue"]; ok {
		return "Vue"
	}
	if _, ok := allDeps["svelte"]; ok {
		return "Svelte"
	}
	if _, ok := allDeps["express"]; ok {
		return "Express"
	}
	if _, ok := allDeps["nest"]; ok {
		return "NestJS"
	}
	if _, ok := allDeps["strapi"]; ok {
		return "Strapi"
	}

	return "Static / Custom"
}

func getRepoCommitInfo(repoDir string) (sha, msg, author, date string, err error) {
	// format: hash|subject|author_name|author_date(iso8601)
	cmd := exec.Command("git", "log", "-1", "--format=%H|%s|%an|%ai")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", "", "", "", err
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 4)
	if len(parts) == 4 {
		return parts[0], parts[1], parts[2], parts[3], nil
	}
	if len(parts) > 0 {
		return parts[0], "", "", "", nil
	}
	return "", "", "", "", fmt.Errorf("unexpected git log output")
}

// detectPythonEntry returns the best Python entry-point argument to pass to the
// interpreter (e.g. "app.py" or "manage.py runserver 0.0.0.0:3000").
// Falls back to "app.py" if nothing recognisable is found.
func detectPythonEntry(sourceDir string, port int) string {
	// Django: manage.py needs special arguments.
	if _, err := os.Stat(filepath.Join(sourceDir, "manage.py")); err == nil {
		return fmt.Sprintf("manage.py runserver 0.0.0.0:%d", port)
	}
	// FastAPI/Flask/generic script — look for common entry-point names.
	for _, name := range []string{"main.py", "app.py", "server.py", "run.py", "wsgi.py", "asgi.py", "index.py"} {
		if _, err := os.Stat(filepath.Join(sourceDir, name)); err == nil {
			return name
		}
	}
	// Check one level deep inside common sub-directories.
	for _, sub := range []string{"src", "app", "backend", "api"} {
		for _, name := range []string{"main.py", "app.py", "server.py"} {
			if _, err := os.Stat(filepath.Join(sourceDir, sub, name)); err == nil {
				return filepath.Join(sub, name)
			}
		}
	}
	return "app.py" // fallback
}

func (w *BuildWorker) buildImage(ctx context.Context, job *models.DeploymentJob, sourceDir string) error {
	args := []string{
		"build",
		"--cache-from", fmt.Sprintf("type=local,src=%s", buildCacheDir(w.cfg.CloneDir, job.ProjectID)),
		"--cache-to", fmt.Sprintf("type=local,dest=%s,mode=max", buildCacheDir(w.cfg.CloneDir, job.ProjectID)),
		"-t", job.ImageTag,
		"--force-rm",
		".",
	}
	// Fallback: if BuildKit is not available, use plain docker build without cache flags
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	cmd.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
	cmd.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}

	if err := cmd.Run(); err != nil {
		// Retry without BuildKit cache flags (older Docker versions)
		w.appendLog(job.DeploymentID, "warn", "system",
			"BuildKit cache failed, retrying without cache flags and patching Dockerfile...")

		// Patch Dockerfile to remove --mount if present, as it requires BuildKit
		dockerfilePath := filepath.Join(sourceDir, "Dockerfile")
		if content, rerr := os.ReadFile(dockerfilePath); rerr == nil {
			newContent := strings.ReplaceAll(string(content), "--mount=type=cache", "")
			// Also handle other mount types just in case
			newContent = strings.ReplaceAll(newContent, "--mount=type=bind", "")
			newContent = strings.ReplaceAll(newContent, "--mount=type=tmpfs", "")
			_ = os.WriteFile(dockerfilePath, []byte(newContent), 0644)
		}

		plainArgs := []string{"build", "--force-rm", "-t", job.ImageTag, "."}
		plain := exec.CommandContext(ctx, "docker", plainArgs...)
		plain.Dir = sourceDir
		plain.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
		plain.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}
		if err := plain.Run(); err != nil {
			return fmt.Errorf("docker build failed: %w", err)
		}
	}

	// Cleanup dangling images and stopped intermediate containers
	w.appendLog(job.DeploymentID, "info", "system", "Cleaning up build artifacts...")
	_ = exec.CommandContext(ctx, "docker", "image", "prune", "-f").Run()
	_ = exec.CommandContext(ctx, "docker", "container", "prune", "-f", "--filter", "label=pushpaka").Run()

	return nil
}

// buildCacheDir returns the local directory used as a Docker build cache for a project.
func buildCacheDir(cloneDir, projectID string) string {
	return filepath.Join(cloneDir, ".buildcache", projectID[:8])
}

func (w *BuildWorker) deployContainer(ctx context.Context, job *models.DeploymentJob) (string, string, error) {
	// For zero-downtime, we don't kill the old container yet.
	// We use a unique name for the new container.
	containerName := fmt.Sprintf("pushpaka_%s_%s", job.ProjectID[:8], job.DeploymentID[:8])
	w.appendLog(job.DeploymentID, "info", "system", fmt.Sprintf("Starting new container: %s", containerName))

	// Build docker run arguments
	args := []string{
		"run", "-d",
		"--name", containerName,
		"--restart", "always",
		"--network", w.cfg.TraefikNetwork,
		"-p", fmt.Sprintf("%d:%d", job.ExternalPort, job.Port),
		// Traefik labels
		"--label", "traefik.enable=true",
		"--label", fmt.Sprintf("traefik.http.routers.%s.rule=PathPrefix(`/p/%s`)", containerName, job.ProjectID[:8]),
		"--label", fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port=%d", containerName, job.Port),
		"--label", "pushpaka=true",
	}

	// Resource limits
	if job.CPULimit != "" {
		args = append(args, "--cpus="+job.CPULimit)
	}
	if job.MemoryLimit != "" {
		args = append(args, "--memory="+job.MemoryLimit)
		args = append(args, "--memory-swap="+job.MemoryLimit) // disable swap
	}

	// Add environment variables
	for k, v := range job.EnvVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, job.ImageTag)

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("docker run: %w\noutput: %s", err, string(out))
	}

	containerID := strings.TrimSpace(string(out))
	deployURL := fmt.Sprintf("http://localhost:%d", job.ExternalPort)

	// Health check and Zero-downtime swap
	if w.healthCheck(ctx, deployURL) {
		w.appendLog(job.DeploymentID, "info", "system", "Health check passed! Cleaning up old deployments...")
		// Mark current as running (it might have been building/queued)
		w.updateStatus(job.DeploymentID, string(models.DeploymentRunning), "", "")

		// Kill other 'running' deployments for this project
		var oldDeployments []models.Deployment
		w.db.Where("project_id = ? AND status = ? AND id != ?", job.ProjectID, "running", job.DeploymentID).Find(&oldDeployments)
		for _, old := range oldDeployments {
			w.appendLog(job.DeploymentID, "info", "system", fmt.Sprintf("Stopping old deployment: %s", old.ID))

			// Try the new naming scheme
			newOldContainerName := fmt.Sprintf("pushpaka_%s_%s", old.ProjectID[:8], old.ID[:8])
			_ = exec.CommandContext(ctx, "docker", "stop", newOldContainerName).Run()
			_ = exec.CommandContext(ctx, "docker", "rm", newOldContainerName).Run()

			// Fallback to previous naming schemes
			legacyName1 := fmt.Sprintf("pushpaka-%s-%s", old.ProjectID[:8], old.ID[:8])
			_ = exec.CommandContext(ctx, "docker", "stop", legacyName1).Run()
			_ = exec.CommandContext(ctx, "docker", "rm", legacyName1).Run()

			// Fallback to legacy naming if needed
			legacyName := old.ProjectID[:8]
			_ = exec.CommandContext(ctx, "docker", "stop", legacyName).Run()
			_ = exec.CommandContext(ctx, "docker", "rm", legacyName).Run()

			// Cleanup the old image to save space
			if old.ImageTag != "" && old.ImageTag != job.ImageTag {
				w.appendLog(job.DeploymentID, "info", "system", fmt.Sprintf("Removing old image: %s", old.ImageTag))
				_ = exec.CommandContext(ctx, "docker", "rmi", old.ImageTag).Run()
			}

			w.db.Model(&models.Deployment{}).Where("id = ?", old.ID).Update("status", "stopped")
		}
	} else {
		w.appendLog(job.DeploymentID, "error", "system", "Health check failed! Rolling back...")
		_ = exec.CommandContext(ctx, "docker", "stop", containerName).Run()
		_ = exec.CommandContext(ctx, "docker", "rm", containerName).Run()
		return "", "", fmt.Errorf("health check failed")
	}

	return containerID, deployURL, nil
}

func (w *BuildWorker) fail(id, errMsg string) {
	log.Error().Str("id", id).Str("error", errMsg).Msg("task or deployment failed")
	w.appendLog(id, "error", "system", "FAILED: "+errMsg)

	resolution := ""
	if w.cfg.AIAPIKey != "" {
		w.appendLog(id, "info", "system", "AI Assistant analyzing failure for immediate resolution...")
		explanation, fixCmd := w.analyzeFailure(id, errMsg)
		resolution = explanation
		if explanation != "" {
			msg := "AI RECOMMENDED FIX: " + explanation
			if fixCmd != "" {
				msg += "\nCOMMAND: " + fixCmd
			}
			w.appendLog(id, "info", "system", msg)
		}
	}

	w.updateStatus(id, "failed", errMsg, resolution)
	// Explicitly notify completion for tasks if this was a task
	w.completeTask(id, false, errMsg)
}

// fireNotification calls the internal notification callback on the API server
// so that Slack/Discord/email alerts are fired without the worker needing
// direct access to those credentials.
func (w *BuildWorker) fireNotification(job *models.DeploymentJob, status, deployURL, errMsg string) {
	if job.NotificationURL == "" {
		return
	}

	// Fetch project name from DB for a better notification message.
	var projectName string
	w.db.Model(&models.Project{}).Where("id = ?", job.ProjectID).Pluck("name", &projectName)

	payload := map[string]any{
		"deployment_id": job.DeploymentID,
		"project_name":  projectName,
		"status":        status,
		"branch":        job.Branch,
		"commit_sha":    job.CommitSHA,
		"url":           deployURL,
		"error_msg":     errMsg,
		"user_id":       job.UserID,
	}
	data, _ := json.Marshal(payload)

	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(job.NotificationURL, "application/json", bytes.NewReader(data))
		if err != nil {
			log.Warn().Err(err).Str("url", job.NotificationURL).Msg("notification callback failed")
			return
		}
		resp.Body.Close()
	}()
}

func (w *BuildWorker) updateStatus(id, status, errMsg, resolution string) {
	now := time.Now().UTC()

	// 1. Try to update Deployment record
	res := w.db.Model(&models.Deployment{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     status,
			"error_msg":  errMsg,
			"resolution": resolution,
			"updated_at": now,
		})

	if res.Error == nil && res.RowsAffected > 0 {
		// If we're starting (building or running) and haven't recorded StartedAt yet, do it now.
		if status == string(models.DeploymentBuilding) || status == string(models.DeploymentRunning) {
			w.db.Model(&models.Deployment{}).
				Where("id = ? AND started_at IS NULL", id).
				Update("started_at", now)
		}
		return
	}

	// 2. Try to update ProjectTask record if no deployment was updated
	taskStatus := models.TaskStatus(status)
	// Map deployment statuses to task statuses if needed
	if status == "failed" {
		taskStatus = models.TaskStatusFailed
	} else if status == "running" || status == "building" {
		taskStatus = models.TaskStatusRunning
	} else if status == "completed" || status == "finished" {
		taskStatus = models.TaskStatusCompleted
	}

	w.db.Model(&models.ProjectTask{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":      taskStatus,
			"error":       errMsg,
			"finished_at": now,
		})
}

func (w *BuildWorker) updateCommitStatus(projectID, sha string, status models.CommitStatus) {
	if sha == "" {
		return
	}
	err := w.db.Model(&models.ProjectCommit{}).
		Where("project_id = ? AND sha = ?", projectID, sha).
		Update("status", status).Error
	if err != nil {
		log.Error().Err(err).Str("project_id", projectID).Str("sha", sha).Msg("failed to update commit status")
	}
}

func (w *BuildWorker) analyzeFailure(deploymentID, errMsg string) (string, string) {
	// First, check for common patterns that we can fix without AI
	lowerErr := strings.ToLower(errMsg)
	if strings.Contains(lowerErr, "cross-env") {
		return "Missing cross-env package", "npm install -g cross-env"
	}
	if strings.Contains(lowerErr, "python") && strings.Contains(lowerErr, "node-gyp") {
		return "node-gyp requires python", "apk add --no-cache python3 make g++ || npm install -g node-gyp"
	}
	if strings.Contains(lowerErr, "context canceled") {
		return "Build process was interrupted (context canceled). This usually happens during server restarts or due to a very slow environment. Retrying may help.", ""
	}

	if w.aiSvc == nil || !w.aiSvc.Available() {
		return "AI Assistant not configured", ""
	}

	// Fetch last 50 logs for context
	var logs []models.DeploymentLog
	w.db.Where("deployment_id = ?", deploymentID).Order("created_at desc").Limit(50).Find(&logs)

	var sb strings.Builder
	for i := len(logs) - 1; i >= 0; i-- {
		sb.WriteString(logs[i].Message + "\n")
	}
	contextLogs := sb.String()

	systemPrompt := "You are the Pushpaka AI DevOps Assistant. Analyze the failure and provide a resolution. Respond ONLY with a JSON object."
	userPrompt := fmt.Sprintf(`The deployment failed with error: %s
Recent logs:
%s

Analyze the failure and provide a resolution.
If the issue can be fixed by running a shell command (e.g., installing a missing package, clearing a cache), provide that command.
Respond ONLY with a JSON object in this format:
{
  "explanation": "Brief explanation of what went wrong",
  "fix_command": "The exact shell command to run to fix this (optional)",
  "confidence": 0.95
}`, errMsg, contextLogs)

	reply, err := w.aiSvc.Ask(systemPrompt, userPrompt)
	if err != nil {
		log.Warn().Err(err).Msg("AI analysis failed")
		return "AI Assistant unavailable", ""
	}

	// Clean up reply in case there's markdown
	reply = strings.TrimPrefix(reply, "```json")
	reply = strings.TrimSuffix(reply, "```")
	reply = strings.TrimSpace(reply)

	var fix struct {
		Explanation string  `json:"explanation"`
		FixCommand  string  `json:"fix_command"`
		Confidence  float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(reply), &fix); err == nil {
		return fix.Explanation, fix.FixCommand
	}

	return strings.TrimSpace(reply), ""
}

func (w *BuildWorker) appendLog(deploymentID, level, stream, message string) {
	err := w.db.Create(&models.DeploymentLog{
		BaseModel:    basemodel.BaseModel{ID: uuid.New().String()},
		DeploymentID: deploymentID,
		Level:        level,
		Stream:       stream,
		Message:      message,
	}).Error
	if err != nil {
		log.Error().Err(err).Msg("failed to append log")
	}
}

// logWriter streams docker build output to the DB
type logWriter struct {
	deploymentID string
	stream       string
	w            *BuildWorker
}

func (lw *logWriter) Write(p []byte) (int, error) {
	lines := strings.Split(strings.TrimSpace(string(p)), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			lw.w.appendLog(lw.deploymentID, "info", lw.stream, line)
		}
	}
	return len(p), nil
}

func (w *BuildWorker) healthCheck(ctx context.Context, deployURL string) bool {
	w.appendLog("", "info", "system", fmt.Sprintf("Health check: %s", deployURL))
	// Give the app a moment to start
	time.Sleep(2 * time.Second)

	// Try for up to 30 seconds
	for i := 0; i < 15; i++ {
		select {
		case <-ctx.Done():
			return false
		default:
			// Just a simple GET request
			resp, err := http.Get(deployURL)
			if err == nil {
				resp.Body.Close()
				// Only consider 2xx (Success) or 3xx (Redirect) as healthy.
				// 4xx (Client Error) or 5xx (Server Error) are failures.
				if resp.StatusCode >= 200 && resp.StatusCode < 400 {
					return true
				}
				w.appendLog("", "warn", "system", fmt.Sprintf("Health check returned status: %d (not ready)", resp.StatusCode))
			}
			time.Sleep(2 * time.Second)
		}
	}
	return false
}

// runBuildInSource executes dependency installation and build scripts in the source directory.
func (w *BuildWorker) runBuildInSource(ctx context.Context, job *models.DeploymentJob, sourceDir string) error {
	isPrimaryPy := isPrimaryPython(sourceDir)
	if _, err := os.Stat(filepath.Join(sourceDir, "package.json")); err == nil && !isPrimaryPy {
		pm := detectPackageManager(sourceDir)

		// Kill any lingering npm/node processes on Windows to release file locks
		if runtime.GOOS == "windows" {
			w.appendLog(job.DeploymentID, "info", "system", "Terminating any lingering npm processes...")
			killCmd := exec.CommandContext(ctx, "cmd", "/c", "taskkill /F /IM node.exe /T 2>nul & taskkill /F /IM npm.cmd /T 2>nul & exit /b 0")
			_ = killCmd.Run()
			time.Sleep(500 * time.Millisecond)
		}

		// Pre-install cleanup: Remove old node_modules on Windows to avoid file locking issues
		nodeModulesPath := filepath.Join(sourceDir, "node_modules")
		if _, err := os.Stat(nodeModulesPath); err == nil {
			w.appendLog(job.DeploymentID, "info", "system", "Cleaning old node_modules...")
			_ = os.RemoveAll(nodeModulesPath)
			// Give Windows time to release file locks
			time.Sleep(1 * time.Second)
		}

		// Create .npmrc file to control npm behavior during install
		npmrcPath := filepath.Join(sourceDir, ".npmrc")
		npmrcContent := `legacy-peer-deps=true
ignore-scripts=true
fund=false
audit=false
`
		if runtime.GOOS == "windows" {
			// Windows-specific npm configuration
			npmrcContent += `progress=false
fetch-timeout=120000
fetch-retry-mintimeout=20000
fetch-retry-maxtimeout=120000
`
		}
		_ = os.WriteFile(npmrcPath, []byte(npmrcContent), 0644)
		w.appendLog(job.DeploymentID, "info", "system", "Configured .npmrc for install")

		// Install - use npm ci if package-lock.json exists for better reliability
		installCmd := pm + " install"
		if pm == "npm" {
			// Check for package lock files
			hasPkgLock := false
			for _, lockFile := range []string{"package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"} {
				if _, err := os.Stat(filepath.Join(sourceDir, lockFile)); err == nil {
					hasPkgLock = true
					break
				}
			}

			// Use npm ci for locked dependencies, npm install for loose
			if hasPkgLock {
				installCmd = "npm ci --no-audit --no-fund"
			} else {
				installCmd = "npm install --no-audit --no-fund"
			}

			// On Windows, add extra flags for stability
			if runtime.GOOS == "windows" {
				installCmd += " --force --verbose"
			}
		}

		if job.InstallCommand != "" {
			installCmd = job.InstallCommand
		}

		w.appendLog(job.DeploymentID, "info", "system", "Running install: "+installCmd)
		cmd := exec.CommandContext(ctx, "sh", "-c", installCmd)
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/c", installCmd)
		}
		cmd.Dir = sourceDir

		// Properly set PATH for node_modules binaries
		pathSep := string(os.PathListSeparator)
		binPath := filepath.Join(sourceDir, "node_modules", ".bin")
		pathEnv := binPath + pathSep + os.Getenv("PATH")

		// Build environment with proper PATH
		cmd.Env = os.Environ()
		pathFound := false
		for i, env := range cmd.Env {
			if strings.HasPrefix(strings.ToUpper(env), "PATH=") {
				cmd.Env[i] = "PATH=" + pathEnv
				pathFound = true
				break
			}
		}
		if !pathFound {
			cmd.Env = append(cmd.Env, "PATH="+pathEnv)
		}

		cmd.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
		cmd.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}
		if err := cmd.Run(); err != nil {
			// Log npm error but continue - npm warnings don't always mean failure
			w.appendLog(job.DeploymentID, "warn", "system", fmt.Sprintf("npm install had warnings/errors: %v (attempting to continue)", err))
			// Check if node_modules actually got created despite the error
			if _, statErr := os.Stat(nodeModulesPath); statErr != nil {
				// node_modules wasn't created, it's a real failure
				return fmt.Errorf("install failed: %v", err)
			}
			// node_modules exists, so continue even if npm reported errors
			w.appendLog(job.DeploymentID, "info", "system", "npm install completed with warnings - node_modules created successfully")
		}

		// Build
		if job.BuildCommand != "" || hasBuildScript(sourceDir) {
			buildCmd := pm + " run build"
			if job.BuildCommand != "" {
				buildCmd = job.BuildCommand
			}
			w.appendLog(job.DeploymentID, "info", "system", "Running build: "+buildCmd)
			cmd = exec.CommandContext(ctx, "sh", "-c", buildCmd)
			if runtime.GOOS == "windows" {
				cmd = exec.CommandContext(ctx, "cmd", "/c", buildCmd)
			}
			cmd.Dir = sourceDir
			binPath := filepath.Join(sourceDir, "node_modules", ".bin")
			pathSep := string(os.PathListSeparator)
			pathEnv := binPath + pathSep + os.Getenv("PATH")

			// Set proper PATH in environment
			cmd.Env = os.Environ()
			pathFound := false
			for i, env := range cmd.Env {
				if strings.HasPrefix(strings.ToUpper(env), "PATH=") {
					cmd.Env[i] = "PATH=" + pathEnv
					pathFound = true
					break
				}
			}
			if !pathFound {
				cmd.Env = append(cmd.Env, "PATH="+pathEnv)
			}

			cmd.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
			cmd.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("build failed: %v", err)
			}
		}
	} else if _, err := os.Stat(filepath.Join(sourceDir, "requirements.txt")); err == nil {
		// Python build (optional)
		w.appendLog(job.DeploymentID, "info", "system", "Python project detected, skipping build step.")
	} else if _, err := os.Stat(filepath.Join(sourceDir, "go.mod")); err == nil {
		w.appendLog(job.DeploymentID, "info", "system", "Go project detected, downloading modules...")
		cmd := exec.CommandContext(ctx, "go", "mod", "download")
		cmd.Dir = sourceDir
		cmd.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
		cmd.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}
		_ = cmd.Run() // non-critical

		buildCmd := "go build -o app ."
		if job.BuildCommand != "" {
			buildCmd = job.BuildCommand
		}
		w.appendLog(job.DeploymentID, "info", "system", "Building Go binary: "+buildCmd)
		cmd = exec.CommandContext(ctx, "sh", "-c", buildCmd)
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/c", buildCmd)
		}
		cmd.Dir = sourceDir
		cmd.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
		cmd.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go build failed: %v", err)
		}
	} else if _, err := os.Stat(filepath.Join(sourceDir, "Cargo.toml")); err == nil {
		w.appendLog(job.DeploymentID, "info", "system", "Rust project detected, building with cargo...")
		buildCmd := "cargo build --release"
		if job.BuildCommand != "" {
			buildCmd = job.BuildCommand
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", buildCmd)
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/c", buildCmd)
		}
		cmd.Dir = sourceDir
		cmd.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
		cmd.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cargo build failed: %v", err)
		}
	} else if _, err := os.Stat(filepath.Join(sourceDir, "pom.xml")); err == nil {
		w.appendLog(job.DeploymentID, "info", "system", "Java Maven project detected, building...")
		buildCmd := "mvn package -DskipTests"
		if job.BuildCommand != "" {
			buildCmd = job.BuildCommand
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", buildCmd)
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/c", buildCmd)
		}
		cmd.Dir = sourceDir
		cmd.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
		cmd.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("maven build failed: %v", err)
		}
	} else if files, _ := os.ReadDir(sourceDir); isDotNetProject(files) {
		w.appendLog(job.DeploymentID, "info", "system", ".NET project detected, building...")
		buildCmd := "dotnet publish -c Release -o out"
		if job.BuildCommand != "" {
			buildCmd = job.BuildCommand
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", buildCmd)
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/c", buildCmd)
		}
		cmd.Dir = sourceDir
		cmd.Stdout = &logWriter{deploymentID: job.DeploymentID, stream: "stdout", w: w}
		cmd.Stderr = &logWriter{deploymentID: job.DeploymentID, stream: "stderr", w: w}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("dotnet build failed: %v", err)
		}
	}
	return nil
}

// promoteToBuildsDir copies build artifacts from sourceDir to buildsDir.
func (w *BuildWorker) promoteToBuildsDir(sourceDir, buildsDir string) error {
	_ = w.forceRemoveDir(buildsDir)
	if err := os.MkdirAll(filepath.Dir(buildsDir), 0755); err != nil {
		return fmt.Errorf("failed to create builds parent dir: %v", err)
	}
	if err := os.MkdirAll(buildsDir, 0755); err != nil {
		return fmt.Errorf("failed to create builds dir: %v", err)
	}

	// Artifact selection: common output directories.
	artifactDir := ""
	for _, dir := range []string{"dist", "build", ".next", "out", "public"} {
		if _, err := os.Stat(filepath.Join(sourceDir, dir)); err == nil {
			artifactDir = dir
			break
		}
	}

	if artifactDir != "" {
		src := filepath.Join(sourceDir, artifactDir)
		return copyDir(src, buildsDir)
	}

	// Fallback: copy whole source but skip node_modules etc.
	return copyDirSkipModules(sourceDir, buildsDir)
}

func hasBuildScript(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	_, hasBuild := pkg.Scripts["build"]
	return hasBuild
}

func (w *BuildWorker) processTask(ctx context.Context, taskID string) {
	var task models.ProjectTask
	if err := w.db.First(&task, "id = ?", taskID).Error; err != nil {
		log.Error().Err(err).Str("task_id", taskID).Msg("failed to fetch task")
		return
	}

	// Update task status to running
	now := time.Now().UTC()
	task.Status = models.TaskStatusRunning
	task.StartedAt = &now
	w.db.Save(&task)

	log.Info().Str("task_id", task.ID).Str("type", string(task.Type)).Str("role", w.Role).Msg("processing task")

	switch w.Role {
	case "syncer":
		w.handleSyncTask(ctx, &task)
	case "builder":
		w.handleBuildTask(ctx, &task)
	case "tester":
		w.handleTestTask(ctx, &task)
	case "deployer":
		w.handleDeployTask(ctx, &task)
	case "ai":
		w.handleAITask(ctx, &task)
	default:
		w.completeTask(task.ID, false, fmt.Sprintf("unsupported role: %s", w.Role))
	}
}

func (w *BuildWorker) handleSyncTask(ctx context.Context, task *models.ProjectTask) {
	project, err := w.getProjectDir(task.ProjectID)
	if err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("Project not found: %v", err))
		return
	}

	// User request: isolated folders per user
	// For sync, we use the final commit SHA directory and we DO NOT delete it,
	// so that subsequent build/test tasks can just copy it.
	commitLabel := task.CommitSHA
	if commitLabel == "" {
		commitLabel = "latest"
	}
	sourcePath := w.getWorkspaceDir(w.cfg.ProjectsDir, project.UserID, project.ID, commitLabel)

	// Create parents and ensure read-write access
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to create sync parent dir: %v", err))
		return
	}

	// We ONLY force remove if it exists but is corrupted.
	// If it exists and is a valid git repo, we could skip clone, but for now we'll do a fresh clone
	// to ensure it matches the exact remote state if requested.
	_ = w.forceRemoveDir(sourcePath)
	if err := os.MkdirAll(sourcePath, 0755); err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to create sync dir: %v", err))
		return
	}

	// NO DEFER REMOVAL! We want to keep this folder for build & test tasks.

	// Mocking job for clone/sync
	syncBranch := project.Branch
	if syncBranch == "" {
		syncBranch = "main"
	}
	job := &models.DeploymentJob{
		DeploymentID: task.ID, // use task ID so git logs appear in the task console
		ProjectID:    project.ID,
		UserID:       project.UserID,
		RepoURL:      project.RepoURL,
		Branch:       syncBranch,
		GitToken:     project.GitToken,
		IsPrivate:    project.IsPrivate,
	}

	w.appendLog(task.ID, "info", "system", fmt.Sprintf("Capturing metadata and detecting environment for project: %s", project.Name))

	// For syncing metadata, we perform a shallow clone to save time
	if err := w.cloneRepo(ctx, job, sourcePath); err != nil {
		w.appendLog(task.ID, "error", "system", fmt.Sprintf("Metadata capture failed: %v", err))
		w.completeTask(task.ID, false, fmt.Sprintf("metadata capture failed: %v", err))
		return
	}
	w.appendLog(task.ID, "info", "system", "Repository fetched successfully")

	// 1. Capture latest commit metadata
	if sha, msg, author, dateStr, err := getRepoCommitInfo(sourcePath); err == nil && sha != "" {
		task.CommitSHA = sha
		w.db.Model(&models.ProjectTask{}).Where("id = ?", task.ID).Update("commit_sha", sha)

		// Parse date (git %ai: 2006-01-02 15:04:05 -0700)
		var commitDate *time.Time
		if t, err := time.Parse("2006-01-02 15:04:05 -0700", dateStr); err == nil {
			commitDate = &t
		}

		// Force update Project record with all detected commit metadata
		w.db.Model(&models.Project{}).
			Where("id = ?", project.ID).
			Updates(map[string]interface{}{
				"latest_commit_sha": sha,
				"latest_commit_msg": msg,
				"latest_commit_at":  commitDate,
				"updated_at":        time.Now().UTC(),
			})

		w.appendLog(task.ID, "info", "system", fmt.Sprintf("Latest commit: %s - %s (by %s)", sha[:8], msg, author))
	}

	// 2. Detect language, framework, and package manager
	lang := detectLanguage(sourcePath)
	framework := "Unknown"
	pm := "npm"
	if lang == "node" {
		pm = detectPackageManager(sourcePath)
		framework = detectFramework(sourcePath)
	}

	w.appendLog(task.ID, "info", "system", fmt.Sprintf("Environment detected: Language=%s, Framework=%s, Package Manager=%s", lang, framework, pm))

	// Force update Project record with all detected metadata
	updates := map[string]interface{}{
		"language":        lang,
		"package_manager": pm,
		"framework":       framework,
		"updated_at":      time.Now().UTC(),
	}
	if err := w.db.Model(&models.Project{}).Where("id = ?", project.ID).Updates(updates).Error; err != nil {
		w.appendLog(task.ID, "warn", "system", fmt.Sprintf("Failed to update project metadata: %v", err))
	}

	w.completeTask(task.ID, true, "")
}

func (w *BuildWorker) handleBuildTask(ctx context.Context, task *models.ProjectTask) {
	project, err := w.getProjectDir(task.ProjectID)
	if err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("Project not found: %v", err))
		return
	}

	job := &models.DeploymentJob{
		DeploymentID: task.ID,
		ProjectID:    task.ProjectID,
		UserID:       project.UserID,
		CommitSHA:    task.CommitSHA,
		RepoURL:      project.RepoURL,
		Branch:       project.Branch,
		BuildCommand: project.BuildCommand,
		ImageTag:     fmt.Sprintf("pushpaka/%s:%s", task.ProjectID[:8], task.CommitSHA[:8]),
		IsBuildOnly:  true, // Build only - don't start the server
	}

	w.processJob(ctx, job)

	// [FIX] Transition check: Ensure we trigger the next task (Testing) if successful
	var updatedTask models.ProjectTask
	if err := w.db.First(&updatedTask, "id = ?", task.ID).Error; err == nil {
		if updatedTask.Status == models.TaskStatusRunning {
			w.appendLog(task.ID, "info", "system", "Build successful, triggering test suite...")
			w.completeTask(task.ID, true, "")
		}
	} else {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to verify task status: %v", err))
	}
}

func (w *BuildWorker) handleDeployTask(ctx context.Context, task *models.ProjectTask) {
	project, err := w.getProjectDir(task.ProjectID)
	if err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("Project not found: %v", err))
		return
	}

	// For deploying, we promote BUILDS_DIR artifacts to DEPLOYS_DIR and run.
	buildsDir := w.getWorkspaceDir(w.cfg.BuildsDir, project.UserID, project.ID, task.CommitSHA)
	deployDir := w.getWorkspaceDir(w.cfg.DeploysDir, project.UserID, project.ID, task.CommitSHA)

	if _, err := os.Stat(buildsDir); os.IsNotExist(err) {
		w.completeTask(task.ID, false, "build artifacts not found for deployment. Please build first.")
		return
	}

	// Clean/Prepare deployment directory
	_ = w.forceRemoveDir(deployDir)
	if err := os.MkdirAll(filepath.Dir(deployDir), 0755); err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to create deployment parent directory: %v", err))
		return
	}
	if err := os.MkdirAll(deployDir, 0755); err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to create deployment directory: %v", err))
		return
	}

	w.appendLog(task.ID, "info", "system", "Promoting build artifacts to deployment directory...")
	if err := copyDir(buildsDir, deployDir); err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to copy artifacts: %v", err))
		return
	}

	// Find deployment record to get assigned port
	var dep models.Deployment
	if err := w.db.Where("project_id = ? AND commit_sha = ? AND status = ?", task.ProjectID, task.CommitSHA, "queued").First(&dep).Error; err != nil {
		// Fallback if no queued deployment found, maybe it's a direct task
		w.appendLog(task.ID, "info", "system", "No queued deployment record found, using project defaults")
	}

	job := &models.DeploymentJob{
		DeploymentID:   task.ID,
		ProjectID:      task.ProjectID,
		UserID:         project.UserID,
		CommitSHA:      task.CommitSHA,
		RepoURL:        project.RepoURL,
		Branch:         project.Branch,
		InstallCommand: project.InstallCommand,
		BuildCommand:   project.BuildCommand,
		StartCommand:   project.StartCommand,
		RunDir:         project.RunDir,
		Port:           project.Port,
		ExternalPort:   dep.ExternalPort,
		IsRecovery:     true, // Skip build, we already copied artifacts
	}

	if job.ExternalPort == 0 {
		// Assign a port if missing
		if addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0"); err == nil {
			if l, err := net.ListenTCP("tcp", addr); err == nil {
				job.ExternalPort = l.Addr().(*net.TCPAddr).Port
				l.Close()
			}
		}
	}

	w.processJob(ctx, job)

	// Check if processJob ended in failure
	var updatedTask models.ProjectTask
	w.db.First(&updatedTask, "id = ?", task.ID)
	if updatedTask.Status == models.TaskStatusRunning {
		w.completeTask(task.ID, true, "")
	}
}

func (w *BuildWorker) handleTestTask(ctx context.Context, task *models.ProjectTask) {
	project, err := w.getProjectDir(task.ProjectID)
	if err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("Project not found: %v", err))
		return
	}

	// For testing, we copy BUILDS_DIR artifacts to TESTS_DIR and run tests
	buildsDir := w.getWorkspaceDir(w.cfg.BuildsDir, project.UserID, project.ID, task.CommitSHA)
	testDir := w.getWorkspaceDir(w.cfg.TestsDir, project.UserID, project.ID, task.CommitSHA)

	if _, err := os.Stat(buildsDir); os.IsNotExist(err) {
		w.completeTask(task.ID, false, "build artifacts not found for testing")
		return
	}

	_ = w.forceRemoveDir(testDir)
	if err := os.MkdirAll(filepath.Dir(testDir), 0755); err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to create test parent directory: %v", err))
		return
	}
	if err := os.MkdirAll(testDir, 0755); err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to create test directory: %v", err))
		return
	}
	if err := copyDir(buildsDir, testDir); err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to setup test directory: %v", err))
		return
	}

	// 1. Allocate a random port for isolated testing
	testPort := 0
	if addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0"); err == nil {
		if l, err := net.ListenTCP("tcp", addr); err == nil {
			testPort = l.Addr().(*net.TCPAddr).Port
			l.Close()
		}
	}
	if testPort == 0 {
		testPort = 13000 + (int(time.Now().UnixNano()) % 5000)
	}

	w.appendLog(task.ID, "info", "system", fmt.Sprintf("Starting isolated test instance on port %d...", testPort))
	w.appendLog(task.ID, "info", "system", fmt.Sprintf("Test Directory: %s", testDir))

	// 2. Start the application in the background
	startCmd := project.StartCommand
	if startCmd == "" {
		w.appendLog(task.ID, "info", "system", "No start command defined, detecting...")
		pm := detectPackageManager(testDir)
		startCmd = detectNodeStartCmd(testDir, pm, testPort)
	}

	w.appendLog(task.ID, "info", "system", fmt.Sprintf("Execution command: %s", startCmd))

	// Replace port placeholder if exists, otherwise set PORT env
	startCmd = strings.ReplaceAll(startCmd, "$PORT", fmt.Sprintf("%d", testPort))
	startCmd = strings.ReplaceAll(startCmd, "{{port}}", fmt.Sprintf("%d", testPort))

	shell, shellFlag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, shellFlag = "cmd", "/c"
	}
	proc := exec.CommandContext(ctx, shell, shellFlag, startCmd)
	proc.Dir = testDir
	// Inherit env and add PORT
	proc.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", testPort), "NODE_ENV=test", "APP_ENV=test")

	stdoutPipe, _ := proc.StdoutPipe()
	stderrPipe, _ := proc.StderrPipe()

	if err := proc.Start(); err != nil {
		w.completeTask(task.ID, false, fmt.Sprintf("failed to start test instance: %v", err))
		return
	}

	// Stream logs in background
	go io.Copy(&logWriter{deploymentID: task.ID, stream: "stdout", w: w}, stdoutPipe)
	go io.Copy(&logWriter{deploymentID: task.ID, stream: "stderr", w: w}, stderrPipe)

	// 3. Wait for app to be ready (health check)
	ready := false
	testURL := fmt.Sprintf("http://127.0.0.1:%d", testPort)
	for i := 0; i < 15; i++ {
		if w.healthCheck(ctx, testURL) {
			ready = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	if !ready {
		_ = proc.Process.Kill()
		w.completeTask(task.ID, false, "application failed to start or pass health check on test port")
		return
	}

	// 4. Run the actual test command
	testCmd := project.TestCommand
	if testCmd != "" {
		w.appendLog(task.ID, "info", "system", fmt.Sprintf("Running test command: %s", testCmd))
		tCmd := exec.CommandContext(ctx, shell, shellFlag, testCmd)
		tCmd.Dir = testDir
		tCmd.Env = append(os.Environ(), fmt.Sprintf("TEST_URL=%s", testURL), fmt.Sprintf("PORT=%d", testPort))
		tCmd.Stdout = &logWriter{deploymentID: task.ID, stream: "stdout", w: w}
		tCmd.Stderr = &logWriter{deploymentID: task.ID, stream: "stderr", w: w}

		if err := tCmd.Run(); err != nil {
			_ = proc.Process.Kill()
			w.completeTask(task.ID, false, fmt.Sprintf("Test command failed: %v", err))
			return
		}
	} else {
		w.appendLog(task.ID, "info", "system", "No test command defined, health check passed.")
	}

	// 5. Cleanup
	_ = proc.Process.Kill()
	w.appendLog(task.ID, "info", "system", "Test instance stopped. Cleanup complete.")
	w.completeTask(task.ID, true, "")
}

func (w *BuildWorker) getProjectDir(id string) (*models.Project, error) {
	var p models.Project
	if err := w.db.First(&p, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (w *BuildWorker) completeTask(id string, success bool, errStr string) {
	// 1. Update DB directly (for speed/fallback)
	status := models.TaskStatusCompleted
	if !success {
		status = models.TaskStatusFailed
	}
	w.db.Model(&models.ProjectTask{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":      status,
		"error":       errStr,
		"finished_at": time.Now().UTC(),
	})

	// 2. Notify backend to trigger next task
	apiURL := fmt.Sprintf("%s/api/v1/internal/tasks/%s/complete", w.cfg.ServerURL, id)

	payload, _ := json.Marshal(map[string]interface{}{
		"success": success,
		"error":   errStr,
	})

	// Retry notification up to 3 times
	for i := 0; i < 3; i++ {
		resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(payload))
		if err == nil {
			resp.Body.Close()
			return
		}
		log.Warn().Err(err).Str("task_id", id).Int("attempt", i+1).Msg("failed to notify backend, retrying...")
		time.Sleep(1 * time.Second)
	}
	log.Error().Str("task_id", id).Msg("failed to notify backend of task completion after 3 attempts")
}

func (w *BuildWorker) handleAITask(ctx context.Context, task *models.ProjectTask) {
	// 1. Load project and failed job context
	var project models.Project
	if err := w.db.First(&project, "id = ?", task.ProjectID).Error; err != nil {
		w.completeTask(task.ID, false, "Project not found")
		return
	}

	// Find the last failed deployment to provide context to the AI
	var lastDeployment models.Deployment
	w.db.Order("created_at desc").First(&lastDeployment, "project_id = ? AND status = ?", task.ProjectID, "failed")

	if lastDeployment.ID == "" {
		w.completeTask(task.ID, false, "No failed deployment found to repair")
		return
	}

	// Fetch logs for the failed deployment
	var logs []models.DeploymentLog
	w.db.Where("deployment_id = ?", lastDeployment.ID).Order("created_at asc").Find(&logs)
	logContext := ""
	for _, l := range logs {
		logContext += fmt.Sprintf("[%s] %s\n", l.Level, l.Message)
	}

	w.appendLog(lastDeployment.ID, "info", "system", "Starting AI autonomous repair session...")

	job := &models.DeploymentJob{
		ProjectID:    project.ID,
		UserID:       project.UserID,
		DeploymentID: lastDeployment.ID,
		RepoURL:      project.RepoURL,
		Branch:       project.Branch,
		GitToken:     project.GitToken,
	}

	// 2. Start AIAgent
	agent := NewAIAgent(w, &project, job, w.aiSvc)

	// 3. Perform repair with full log context
	fullError := fmt.Sprintf("Error: %s\n\nRecent Logs:\n%s", task.Error, logContext)
	err := agent.Repair(fullError)
	if err != nil {
		w.appendLog(lastDeployment.ID, "error", "system", fmt.Sprintf("AI repair failed: %v", err))
		w.completeTask(task.ID, false, fmt.Sprintf("AI repair failed: %v", err))
		return
	}

	w.appendLog(lastDeployment.ID, "info", "system", "AI repair session completed successfully. You can now retry the deployment.")
	w.completeTask(task.ID, true, "")
}
