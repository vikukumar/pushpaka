package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
	"github.com/vikukumar/pushpaka/pkg/tunnel"
	"github.com/vikukumar/pushpaka/queue"
)

type TaskDispatcher struct {
	taskRepo       *repositories.TaskRepository
	projectRepo    *repositories.ProjectRepository
	workerRepo     *repositories.WorkerNodeRepository
	domainRepo     *repositories.DomainRepository
	deploymentRepo *repositories.DeploymentRepository
	rdb            *redis.Client
	inQueue        *queue.InProcess
	log            *zerolog.Logger
}

func NewTaskDispatcher(
	taskRepo *repositories.TaskRepository,
	projectRepo *repositories.ProjectRepository,
	workerRepo *repositories.WorkerNodeRepository,
	domainRepo *repositories.DomainRepository,
	deploymentRepo *repositories.DeploymentRepository,
	rdb *redis.Client,
	inQueue *queue.InProcess,
	log *zerolog.Logger,
) *TaskDispatcher {
	return &TaskDispatcher{
		taskRepo:       taskRepo,
		projectRepo:    projectRepo,
		workerRepo:     workerRepo,
		domainRepo:     domainRepo,
		deploymentRepo: deploymentRepo,
		rdb:            rdb,
		inQueue:        inQueue,
		log:            log,
	}
}

// CreateTask creates a new task and queues it for the appropriate worker role
func (d *TaskDispatcher) CreateTask(projectID string, taskType models.TaskType, sha string) (*models.ProjectTask, error) {
	task := &models.ProjectTask{
		BaseModel: basemodel.BaseModel{ID: uuid.New().String()},
		ProjectID: projectID,
		Type:      taskType,
		Status:    models.TaskStatusPending,
		CommitSHA: sha,
	}

	// Check if a task of this type for this project/commit is already pending or running
	if d.taskRepo.Exists(projectID, taskType, sha) {
		// Task already exists, skip creating duplicate
		return nil, nil
	}

	if err := d.taskRepo.Create(task); err != nil {
		return nil, err
	}

	// Update project's current task
	d.projectRepo.UpdateTaskStatus(projectID, task.ID, string(taskType))

	// Queue for worker
	d.queueTask(task)

	return task, nil
}

func (d *TaskDispatcher) queueTask(task *models.ProjectTask) {
	// Payload for worker
	payload := []byte(task.ID)

	// 1. Check for Active Tunnels (Hybrid/Vaahan Mode)
	// We iterate through active sessions to see if any worker can handle this task.
	// In the future, we should target the specific worker assigned to the project.
	go d.dispatchViaTunnel(task)

	// 2. In-Process Queue (Dev/Single Binary Mode)
	if d.inQueue != nil {
		roleString := string(task.Type)
		// "aifix" tasks are consumed by the "ai" worker — map appropriately
		if roleString == string(models.TaskTypeAIFix) {
			roleString = "ai"
		}
		_ = d.inQueue.Push(roleString, payload)
	}

	// 3. Redis Queue (Production/Distributed Mode)
	if d.rdb != nil {
		roleQueue := fmt.Sprintf("pushpaka:tasks:%s", task.Type)
		// Map aifix to the ai worker queue in Redis too
		if task.Type == models.TaskTypeAIFix {
			roleQueue = "pushpaka:tasks:ai"
		}
		d.rdb.LPush(context.Background(), roleQueue, payload)
	}
}

func (d *TaskDispatcher) dispatchViaTunnel(task *models.ProjectTask) {
	// Find active workers that might be able to handle this.
	// For simplicity, we check all registered workers that are "active"
	workers, err := d.workerRepo.ListAll()
	if err != nil {
		return
	}

	for _, w := range workers {
		if w.Status != models.WorkerStatusActive || w.ID == "local" {
			continue
		}

		// Ensure this worker supports the task type
		supportsTask := false
		for _, r := range w.Roles {
			if r == string(task.Type) {
				supportsTask = true
				break
			}
		}
		if len(w.Roles) > 0 && !supportsTask {
			continue // Skip this worker if it explicitly lists roles and this task isn't one of them
		}

		// Try to get tunnel session
		session, err := tunnel.GlobalManager.GetSession(w.ID)
		if err != nil {
			continue
		}

		// Create a transport that dials through the Yamux session
		tr := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return session.Open()
			},
		}
		hc := &http.Client{
			Transport: tr,
			Timeout:   5 * time.Second,
		}

		// Send task info as JSON to the worker for role routing
		taskPayload, _ := json.Marshal(map[string]string{
			"id":   task.ID,
			"type": string(task.Type),
		})

		resp, err := hc.Post("http://worker/internal/task", "application/json", bytes.NewReader(taskPayload))
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusAccepted {
				d.log.Info().Str("task_id", task.ID).Str("type", string(task.Type)).Str("worker_id", w.ID).Msg("task dispatched via tunnel")
				return // Dispatched successfully to one worker
			}
		}
	}
}

// HandleTaskCompletion is called by workers when a task finishes.
func (d *TaskDispatcher) HandleTaskCompletion(taskID string, success bool, errStr string) error {
	task, err := d.taskRepo.Get(taskID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	task.FinishedAt = &now
	if success {
		task.Status = models.TaskStatusCompleted
	} else {
		task.Status = models.TaskStatusFailed
		task.Error = errStr
	}

	ifSuccess := "succeeded"
	if !success {
		ifSuccess = "failed"
	}
	d.log.Info().Str("task_id", task.ID).Str("status", ifSuccess).Msg("task finished")

	if err := d.taskRepo.Update(task); err != nil {
		return err
	}

	if success {
		// Chain next task
		d.triggerNextTask(task)
	} else if task.Type == models.TaskTypeBuild || task.Type == models.TaskTypeTest || task.Type == models.TaskTypeDeploy {
		// Trigger AI Fix on failure for critical pipeline tasks, but respect retry limit
		if task.RetryCount < 3 {
			d.log.Info().Str("task_id", task.ID).Int("retry_count", task.RetryCount).Msg("task failed, triggering AI self-healing...")

			// Increment retry count for the NEXT attempt
			task.RetryCount++
			d.taskRepo.Update(task)

			aiTask, _ := d.CreateTask(task.ProjectID, models.TaskTypeAIFix, task.CommitSHA)
			if aiTask != nil {
				aiTask.Error = task.Error
				d.taskRepo.Update(aiTask)
			}
		} else {
			d.log.Warn().Str("task_id", task.ID).Msg("task reached maximum retry limit (3), stopping.")
			d.projectRepo.UpdateStatus(task.ProjectID, "failed")
		}
	}

	return nil
}

// RestartTask resets a task to pending and re-queues it.
func (d *TaskDispatcher) RestartTask(taskID string) error {
	task, err := d.taskRepo.Get(taskID)
	if err != nil {
		return err
	}

	task.Status = models.TaskStatusPending
	task.StartedAt = nil
	task.FinishedAt = nil
	task.Error = ""
	task.Log = "" // Clear logs on restart? Probably better.

	if err := d.taskRepo.Update(task); err != nil {
		return err
	}

	d.queueTask(task)
	return nil
}

func (d *TaskDispatcher) triggerNextTask(task *models.ProjectTask) {
	switch task.Type {
	case models.TaskTypeSync, models.TaskTypeFetch:
		// Sync done → Build
		d.CreateTask(task.ProjectID, models.TaskTypeBuild, task.CommitSHA)

	case models.TaskTypeBuild:
		// Build done → Test only if the project has a test command configured.
		// Otherwise skip straight to Deploy so we don't block on an empty test.
		project, err := d.projectRepo.FindByIDInternal(task.ProjectID)
		if err == nil && project.TestCommand != "" {
			d.CreateTask(task.ProjectID, models.TaskTypeTest, task.CommitSHA)
		} else {
			d.log.Info().Str("project_id", task.ProjectID).Msg("no test command configured — skipping test, going straight to deploy")
			d.CreateTask(task.ProjectID, models.TaskTypeDeploy, task.CommitSHA)
		}

	case models.TaskTypeTest:
		// Test passed → Deploy
		d.CreateTask(task.ProjectID, models.TaskTypeDeploy, task.CommitSHA)

	case models.TaskTypeDeploy:
		// Deploy succeeded → promote as default + attach verified domains to Traefik.
		d.onDeploySuccess(task)
	}
}

// onDeploySuccess promotes the fresh deployment as the live default and wires
// any verified custom domains into the running container's Traefik labels.
func (d *TaskDispatcher) onDeploySuccess(task *models.ProjectTask) {
	// 1. Find the deployment record that was just deployed for this commit.
	var dep models.Deployment
	if err := d.deploymentRepo.FindLatestRunningByProject(task.ProjectID, &dep); err != nil {
		d.log.Warn().Err(err).Str("project_id", task.ProjectID).Msg("onDeploySuccess: could not find running deployment")
		d.projectRepo.UpdateStatus(task.ProjectID, "running")
		return
	}

	// 2. Promote this deployment as the default/live one.
	_ = d.deploymentRepo.ClearDefault(task.ProjectID)
	_ = d.deploymentRepo.SetDefault(dep.ID)
	_ = d.projectRepo.SetMainDeployID(task.ProjectID, dep.ID)
	d.log.Info().Str("deployment_id", dep.ID).Msg("deployment promoted to default")

	// 3. Attach verified custom domains via Traefik labels.
	containerName := fmt.Sprintf("pushpaka_%s_%s", task.ProjectID[:8], dep.ID[:8])
	domains, err := d.domainRepo.FindByProjectID(task.ProjectID)
	if err != nil || len(domains) == 0 {
		d.log.Info().Str("project_id", task.ProjectID).Msg("no custom domains found, using path-prefix routing only")
		d.projectRepo.UpdateStatus(task.ProjectID, "running")
		return
	}

	// Build a combined Host() OR PathPrefix() Traefik rule.
	hostRules := []string{}
	for _, dom := range domains {
		if dom.Verified {
			hostRules = append(hostRules, fmt.Sprintf("Host(`%s`)", dom.Domain))
		}
	}
	if len(hostRules) == 0 {
		d.log.Info().Str("project_id", task.ProjectID).Msg("no verified domains — using path-prefix routing only")
		d.projectRepo.UpdateStatus(task.ProjectID, "running")
		return
	}

	combinedRule := strings.Join(hostRules, " || ")
	fullRule := fmt.Sprintf("%s || PathPrefix(`/p/%s`)", combinedRule, task.ProjectID[:8])

	// Use `docker update` to hot-patch the label without restarting the container.
	updateArgs := []string{
		"update",
		"--label-add", fmt.Sprintf("traefik.http.routers.%s.rule=%s", containerName, fullRule),
		containerName,
	}
	out, err := exec.CommandContext(context.Background(), "docker", updateArgs...).CombinedOutput()
	if err != nil {
		d.log.Warn().Err(err).Str("output", string(out)).Str("container", containerName).
			Msg("failed to update Traefik domain label on container")
	} else {
		for _, dom := range domains {
			if dom.Verified {
				d.log.Info().Str("domain", dom.Domain).Str("container", containerName).Msg("domain attached to container via Traefik")
			}
		}
	}

	d.projectRepo.UpdateStatus(task.ProjectID, "running")
}
func (d *TaskDispatcher) GetProjectTasks(projectID string) ([]models.ProjectTask, error) {
	return d.taskRepo.FindByProjectID(projectID)
}

func (d *TaskDispatcher) GetTask(id string) (*models.ProjectTask, error) {
	return d.taskRepo.Get(id)
}

// RecoverStuckTasks finds all tasks currently in "running" or "pending" state from before the restart
// and re-queues them for execution. This ensures no tasks are lost during server restarts.
func (d *TaskDispatcher) RecoverStuckTasks(ctx context.Context) error {
	// Find tasks that were in progress
	runningTasks, _ := d.taskRepo.FindByStatus(models.TaskStatusRunning)
	// Find tasks that were waiting
	pendingTasks, _ := d.taskRepo.FindByStatus(models.TaskStatusPending)

	tasks := append(runningTasks, pendingTasks...)

	if len(tasks) == 0 {
		return nil
	}

	d.log.Info().Int("count", len(tasks)).Msg("recovering stuck tasks after restart")

	for _, task := range tasks {
		// Reset running tasks to pending state for re-execution
		if task.Status == models.TaskStatusRunning {
			task.Status = models.TaskStatusPending
			task.StartedAt = nil
			task.Error = "Task was interrupted by server restart"

			if err := d.taskRepo.Update(&task); err != nil {
				d.log.Error().Err(err).Str("task_id", task.ID).Msg("failed to reset stuck task")
				continue
			}
		}

		// Re-queue the task
		d.queueTask(&task)
		d.log.Info().Str("task_id", task.ID).Str("type", string(task.Type)).Msg("recovered and re-queued task")
	}

	return nil
}
