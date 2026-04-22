package handlers

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/vikukumar/pushpaka/pkg/models"
	"gorm.io/gorm"
)

// WorkerStatsProvider is implemented by the in-process queue to expose live worker counts.
type WorkerStatsProvider interface {
	TotalWorkers() int
	ActiveJobs() int
	SyncWorkers() int
	BuildWorkers() int
	TestWorkers() int
	AIWorkers() int
	DeployWorkers() int

	SyncActive() int
	BuildActive() int
	TestActive() int
	AIActive() int
	DeployActive() int
}

type HealthHandler struct {
	db          *gorm.DB
	rdb         *redis.Client
	workerStats WorkerStatsProvider
}

func NewHealthHandler(db *gorm.DB, rdb *redis.Client, ws WorkerStatsProvider) *HealthHandler {
	return &HealthHandler{db: db, rdb: rdb, workerStats: ws}
}

func (h *HealthHandler) Health(c *gin.Context) {
	status := "ok"
	dbOK := true
	redisOK := true

	sqlDB, err := h.db.DB()
	if err != nil || sqlDB.Ping() != nil {
		dbOK = false
		status = "degraded"
	}

	if h.rdb == nil {
		redisOK = false
	} else if err := h.rdb.Ping(c.Request.Context()).Err(); err != nil {
		redisOK = false
		status = "degraded"
	}

	code := http.StatusOK
	if status != "ok" {
		code = http.StatusServiceUnavailable
	}

	c.JSON(code, gin.H{
		"status":  status,
		"version": "v1.0.0",
		"checks": gin.H{
			"database": dbOK,
			"redis":    redisOK,
		},
	})
}

func (h *HealthHandler) Ready(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// Metrics returns the Prometheus handler wrapped for Gin
func MetricsHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// System returns a live snapshot of system capabilities and worker status.
func (h *HealthHandler) System(c *gin.Context) {
	// Docker availability
	dockerAvailable, dockerHost := checkDockerAvailable()

	// Git availability
	gitAvailable := false
	gitVersion := ""
	if out, err := runWithTimeout("git", "--version"); err == nil {
		gitAvailable = true
		gitVersion = strings.TrimSpace(out)
	}

	// Running inside a container?
	inContainer := isRunningInContainer()

	// Queue mode
	queueMode := "redis"
	if h.rdb == nil {
		queueMode = "in-process"
	}

	// Worker stats
	totalWorkers, activeJobs := 0, 0
	syncWorkers, buildWorkers, testWorkers, aiWorkers, deployWorkers := 0, 0, 0, 0, 0
	syncActive, buildActive, testActive, aiActive, deployActive := 0, 0, 0, 0, 0
	tracked := false

	// Worker stats from the database (tracks distributed/child process workers)
	var activeNodes []models.WorkerNode
	if err := h.db.Where("status = ?", models.WorkerStatusActive).Find(&activeNodes).Error; err == nil {
		totalWorkers = len(activeNodes)
		for _, w := range activeNodes {
			for _, r := range w.Roles {
				switch r {
				case "sync":
					syncWorkers++
				case "build":
					buildWorkers++
				case "test":
					testWorkers++
				case "ai":
					aiWorkers++
				case "deploy":
					deployWorkers++
				}
			}
		}
	}

	// Active jobs from the database (tracks running tasks)
	var runningTasks []models.ProjectTask
	if err := h.db.Where("status = ?", models.TaskStatusRunning).Find(&runningTasks).Error; err == nil {
		activeJobs = len(runningTasks)
		for _, t := range runningTasks {
			switch string(t.Type) {
			case "sync", "fetch":
				syncActive++
			case "build":
				buildActive++
			case "test":
				testActive++
			case "ai":
				aiActive++
			case "deploy":
				deployActive++
			}
		}
	}

	// Legacy track mode is true if we are querying from DB successfully
	tracked = true

	// Fetch System Load Metrics
	var memTotal, memUsed uint64
	var memPercent, cpuPercent float64
	if v, err := mem.VirtualMemory(); err == nil {
		memTotal = v.Total
		memUsed = v.Used
		memPercent = v.UsedPercent
	}
	// Pass 0 to cpu.Percent to get the overall CPU usage without blocking too long
	if c, err := cpu.Percent(0, false); err == nil && len(c) > 0 {
		cpuPercent = c[0]
	}

	hostname, _ := os.Hostname()
	ipAddr := getOutboundIP()

	c.JSON(http.StatusOK, gin.H{
		"docker": gin.H{
			"available": dockerAvailable,
			"host":      dockerHost,
		},
		"git": gin.H{
			"available": gitAvailable,
			"version":   gitVersion,
		},
		"workers": gin.H{
			"total":         totalWorkers,
			"active_jobs":   activeJobs,
			"idle":          max(0, totalWorkers-activeJobs),
			"queue_mode":    queueMode,
			"tracked":       tracked,
			"sync":          syncWorkers,
			"sync_active":   syncActive,
			"build":         buildWorkers,
			"build_active":  buildActive,
			"test":          testWorkers,
			"test_active":   testActive,
			"ai":            aiWorkers,
			"ai_active":     aiActive,
			"deploy":        deployWorkers,
			"deploy_active": deployActive,
		},
		"runtime": gin.H{
			"os":           runtime.GOOS,
			"arch":         runtime.GOARCH,
			"in_container": inContainer,
		},
		"load": gin.H{
			"cpu_percent": cpuPercent,
			"ram_total":   memTotal,
			"ram_used":    memUsed,
			"ram_percent": memPercent,
			"hostname":    hostname,
			"ip":          ipAddr,
		},
	})
}

// getOutboundIP gets the preferred outbound ip of this machine
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// checkDockerAvailable probes the Docker socket / named pipe and returns
// (available, detectedHost).
func checkDockerAvailable() (bool, string) {
	// Common socket paths
	candidates := []string{
		"/var/run/docker.sock",
		"/run/docker.sock",
	}
	if runtime.GOOS == "windows" {
		candidates = []string{`\\.\pipe\docker_engine`}
	}
	// Also respect DOCKER_HOST env
	if dh := os.Getenv("DOCKER_HOST"); dh != "" {
		h := strings.TrimPrefix(dh, "unix://")
		h = strings.TrimPrefix(h, "npipe://")
		candidates = append([]string{h}, candidates...)
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			// Socket exists -- also verify we can connect
			if _, err2 := runWithTimeout("docker", "info"); err2 == nil {
				return true, path
			}
		}
	}

	// Last resort: just try the CLI
	if _, err := runWithTimeout("docker", "info"); err == nil {
		return true, os.Getenv("DOCKER_HOST")
	}
	return false, ""
}

// isRunningInContainer checks common signals that we're inside a container.
func isRunningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		return strings.Contains(string(data), "docker") || strings.Contains(string(data), "containerd")
	}
	return false
}

// runWithTimeout runs a command with a 3-second timeout and returns combined output.
func runWithTimeout(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
