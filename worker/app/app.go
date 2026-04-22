// Package app exposes the Pushpaka build-worker pool as a callable function,
// allowing it to be embedded in the combined pushpaka binary.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/database"
	"github.com/vikukumar/pushpaka/pkg/models"
	"github.com/vikukumar/pushpaka/queue"

	"github.com/vikukumar/pushpaka/worker/internal/client"
	"github.com/vikukumar/pushpaka/worker/internal/config"
	"github.com/vikukumar/pushpaka/worker/internal/worker"
)

// StatsReporter is implemented by the in-process queue to track worker and job lifecycle events.
// All methods must be safe for concurrent use. Pass nil to disable reporting.
type StatsReporter interface {
	WorkerStarted(role string)
	WorkerStopped(role string)
	JobStarted(role string)
	JobFinished(role string)
}

type RunOptions struct {
	Mode      string
	ServerURL string
	ZonePAT   string
	Role      string
}

// Run starts the Pushpaka worker pool and blocks until ctx is cancelled.
func Run(ctx context.Context, opts RunOptions) error {
	cfg := config.Load()

	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("dirs: %w", err)
	}
	log.Info().
		Str("clone_dir", cfg.CloneDir).
		Str("deploys_dir", cfg.DeploysDir).
		Msg("build directories ready")

	log.Info().
		Str("mode", opts.Mode).
		Str("server", opts.ServerURL).
		Msg("Initializing Pushpaka worker node")

	var db *gorm.DB
	var err error
	if opts.Mode == "hybrid" {
		db, err = basemodel.Connect(cfg.DatabaseDriver, cfg.DatabaseURL, "production")
	} else if opts.Mode == "vaahan" {
		log.Info().Msg("Vaahan mode: using embedded pure-Go SQLite for lightning-fast serverless tracking")
		db, err = basemodel.Connect("sqlite", filepath.Join(cfg.DeploysDir, "vaahan.db"), "production")
	} else {
		return fmt.Errorf("unknown worker mode: %s (must be hybrid or vaahan)", opts.Mode)
	}

	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}

	// In remote modes (vaahan/hybrid), the worker bypasses Redis and receives deployment tasks
	// directly over the Yamux multiplexed websocket from the main API!
	if opts.Mode == "vaahan" || opts.Mode == "hybrid" {
		iq := queue.New(100)

		roles := []string{"sync", "build", "test", "deploy", "ai"}
		if opts.Role != "" {
			roles = []string{opts.Role}
		}
		nodeClient := client.NewWorkerClient(opts.ServerURL, opts.ZonePAT, db, cfg, iq, roles)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			nodeClient.Start(ctx)
		}()

		log.Info().Msg("Starting in-process deployment workers bound to Yamux tunnel")
		if err := runInProcess(ctx, iq, nil, db, cfg, opts.Role); err != nil {
			return err
		}

		wg.Wait()
		return nil
	}

	// Legacy or standalone mode: relies on Redis queue
	rdb, err := database.NewRedis(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	var wg sync.WaitGroup
	// Role-based worker pools
	roles := []struct {
		Count int
		Queue string
		Name  string
	}{
		{cfg.BuildWorkers, "pushpaka:tasks:build", "builder"},
		{cfg.SyncWorkers, "pushpaka:tasks:sync", "syncer"},
		{cfg.TestWorkers, "pushpaka:tasks:test", "tester"},
		{cfg.AIWorkers, "pushpaka:tasks:ai", "ai"},
		{cfg.DeployWorkers, "pushpaka:tasks:deploy", "deployer"},
	}

	for _, role := range roles {
		for i := 0; i < role.Count; i++ {
			wg.Add(1)
			go func(id int, queueName, roleName string) {
				defer wg.Done()
				w := worker.NewBuildWorker(id, db, rdb, cfg, roleName, queueName)
				w.Run(ctx)
			}(i, role.Queue, role.Name)
		}
		if role.Count > 0 {
			log.Info().Int("count", role.Count).Str("role", role.Name).Msg("Worker pool started")
		}
	}

	wg.Wait()
	log.Info().Msg("all workers stopped")
	return nil
}

// RunInProcess starts the worker pool reading from an in-process queue
// instead of Redis. Used in dev/combined-binary mode.
func RunInProcess(ctx context.Context, q interface{}, reporter StatsReporter) error {
	cfg := config.Load()
	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("dirs: %w", err)
	}

	db, err := basemodel.Connect(cfg.DatabaseDriver, cfg.DatabaseURL, "production")
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}

	return runInProcess(ctx, q, reporter, db, cfg, "")
}

// RunInProcessWithDB is identical to RunInProcess but uses a pre-opened database.
func RunInProcessWithDB(ctx context.Context, q interface{}, reporter StatsReporter, db *gorm.DB) error {
	cfg := config.Load()
	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("dirs: %w", err)
	}
	return runInProcess(ctx, q, reporter, db, cfg, "")
}

func runInProcess(ctx context.Context, q interface{}, reporter StatsReporter, db *gorm.DB, cfg *config.Config, targetRole string) error {
	var iq interface {
		Chan(role string) <-chan []byte
	}
	if casted, ok := q.(interface {
		Chan(role string) <-chan []byte
	}); ok {
		iq = casted
	} else {
		return fmt.Errorf("invalid in-process queue type: %T", q)
	}

	var wg sync.WaitGroup

	// Roles to run in-process
	roles := []struct {
		Name  string
		Type  string
		Count int
	}{
		{"installer", "installer", 1},
		{"syncer", "sync", cfg.SyncWorkers},
		{"builder", "build", cfg.BuildWorkers},
		{"tester", "test", cfg.TestWorkers},
		{"deployer", "deploy", cfg.DeployWorkers},
		{"ai", "ai", cfg.AIWorkers},
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}

	// Track all registered node IDs for the single centralized heartbeat.
	var registeredIDs []string
	var idsMu sync.Mutex

	for _, role := range roles {
		if targetRole != "" && role.Type != targetRole {
			continue
		}
		if role.Count <= 0 {
			role.Count = 1
		}
		for i := 0; i < role.Count; i++ {
			nodeName := fmt.Sprintf("%s-internal-%s-%d", hostname, role.Name, i)

			if db != nil {
				now := time.Now().UTC()
				// Use upsert-by-name so restarts don't create duplicate rows
				var existing models.WorkerNode
				res := db.Where("name = ?", nodeName).First(&existing)
				if res.Error != nil {
					// Not found: create new
					workerID := uuid.New().String()
					workerNode := &models.WorkerNode{
						BaseModel: basemodel.BaseModel{
							ID:        workerID,
							CreatedAt: now,
							UpdatedAt: now,
						},
						Name:         nodeName,
						Type:         models.WorkerTypeIntegrated,
						Status:       models.WorkerStatusActive,
						OS:           runtime.GOOS,
						Architecture: runtime.GOARCH,
						Roles:        []string{role.Type},
						LastSeenAt:   &now,
						// AuthToken must be unique — use workerID as the internal token.
						// Internal workers don't use HTTP auth; this satisfies the DB constraint.
						AuthToken: "internal-" + workerID,
					}
					if err := db.Create(workerNode).Error; err != nil {
						log.Error().Err(err).Str("node", nodeName).Msg("Failed to register internal worker in DB")
					} else {
						idsMu.Lock()
						registeredIDs = append(registeredIDs, workerID)
						idsMu.Unlock()
						log.Info().Str("node", nodeName).Str("role", role.Type).Msg("Internal worker registered in DB")
					}
				} else {
					// Found: mark as active again
					db.Model(&existing).Updates(map[string]interface{}{
						"status":       models.WorkerStatusActive,
						"last_seen_at": now,
						"updated_at":   now,
					})
					idsMu.Lock()
					registeredIDs = append(registeredIDs, existing.ID)
					idsMu.Unlock()
					log.Info().Str("node", nodeName).Str("role", role.Type).Msg("Internal worker re-registered in DB")
				}
			}

			wg.Add(1)
			go func(id int, rName, rType string) {
				defer wg.Done()
				if reporter != nil {
					reporter.WorkerStarted(rName)
					defer reporter.WorkerStopped(rName)
				}
				w := worker.NewBuildWorker(id, db, nil, cfg, rName, "pushpaka:tasks:"+rType)
				w.RunInProcess(ctx, iq.Chan(rType), reporter)
			}(i, role.Name, role.Type)
		}
		log.Info().Int("count", role.Count).Str("role", role.Name).Msg("In-process worker pool started")
	}

	// Single centralized heartbeat: one batched query every 30s instead of N separate queries every 10s.
	if db != nil {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					idsMu.Lock()
					ids := append([]string(nil), registeredIDs...)
					idsMu.Unlock()
					if len(ids) > 0 {
						db.Model(&models.WorkerNode{}).Where("id IN ?", ids).
							Update("status", models.WorkerStatusOffline)
					}
					return
				case <-ticker.C:
					idsMu.Lock()
					ids := append([]string(nil), registeredIDs...)
					idsMu.Unlock()
					if len(ids) > 0 {
						now := time.Now().UTC()
						db.Model(&models.WorkerNode{}).Where("id IN ?", ids).Updates(map[string]interface{}{
							"last_seen_at": now,
							"status":       models.WorkerStatusActive,
						})
					}
				}
			}
		}()
	}

	wg.Wait()
	log.Info().Msg("in-process workers stopped")
	return nil
}
