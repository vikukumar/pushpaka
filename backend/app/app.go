// Package app exposes the Pushpaka API server as a callable function,
// allowing it to be embedded in the combined pushpaka binary.
package app

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/internal/handlers"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/internal/router"
	"github.com/vikukumar/pushpaka/internal/services"
	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/database"
	"github.com/vikukumar/pushpaka/pkg/tunnel"
	"github.com/vikukumar/pushpaka/queue"
	"github.com/vikukumar/pushpaka/ui"
)

// OpenDB opens the database using the given driver and DSN.
// Exposed so that callers embedding multiple components (e.g. the combined
// binary) can open ONE shared connection pool and pass it to both the API
// and the worker, preventing SQLite BUSY_SNAPSHOT errors in WAL mode.
func OpenDB(driver, dsn string) (*gorm.DB, error) {
	return basemodel.Connect(driver, dsn, "development")
}

// RunOptions configures optional behaviour for Run.
type RunOptions struct {
	// InProcessQueue, when non-nil, is used instead of Redis for deployment jobs.
	// Intended for dev mode where the embedded worker reads from the same queue.
	InProcessQueue *queue.InProcess

	// DB, when non-nil, is used instead of opening a new database connection.
	// The caller is responsible for closing the DB after RunWithOptions returns.
	// Used in all-in-one mode to share one SQLite pool between API and worker.
	DB *gorm.DB
}

// Run starts the Pushpaka API server with default options and blocks until ctx is cancelled.
func Run(ctx context.Context) error {
	return RunWithOptions(ctx, RunOptions{})
}

// RunWithOptions starts the Pushpaka API server with the supplied options.
func RunWithOptions(ctx context.Context, opts RunOptions) error {
	cfg := config.Load()

	var db *gorm.DB
	if opts.DB != nil {
		// Caller already opened a shared pool; we must not close it here.
		db = opts.DB
	} else {
		var err error
		db, err = basemodel.Connect(cfg.DatabaseDriver, cfg.DatabaseURL, cfg.AppEnv)
		if err != nil {
			return fmt.Errorf("database: %w", err)
		}
		defer func() {
			sqlDB, err := db.DB()
			if err == nil {
				sqlDB.Close()
			}
		}()
	}

	// Repositories for system-level services
	systemCfgRepo := repositories.NewSystemConfigRepository(db)
	workerNodeRepo := repositories.NewWorkerNodeRepository(db)

	log.Printf("Database connection established - API ready (JIT migrations enabled)")

	// Background Initialization: System Config
	go func() {
		// Initialize System Configuration (ZoneID)
		zoneID, err := systemCfgRepo.Get("ZONE_ID")
		if err != nil {
			zoneID = uuid.New().String()
			if setErr := systemCfgRepo.Set("ZONE_ID", zoneID); setErr != nil {
				log.Error().Err(setErr).Msg("failed to save new ZONE_ID")
			} else {
				log.Info().Str("zone_id", zoneID).Msg("generated new installation ZoneID")
			}
		} else {
			log.Info().Str("zone_id", zoneID).Msg("loaded installation ZoneID")
		}
		log.Info().Msg("background database initialization complete")
	}()

	// Redis is optional: skipped when REDIS_URL is empty or an in-process queue is used.
	var rdb *redis.Client
	if opts.InProcessQueue != nil {
		log.Info().Msg("using in-process job queue (dev mode)")
	} else if cfg.RedisURL != "" {
		var redisErr error
		rdb, redisErr = database.NewRedis(cfg.RedisURL)
		if redisErr != nil {
			log.Warn().Err(redisErr).Msg("redis unavailable - deployment triggers disabled")
		} else {
			defer rdb.Close()
		}
	} else {
		log.Warn().Msg("REDIS_URL not set and no in-process queue -- deployment triggers disabled")
	}

	// Repositories
	projectRepo := repositories.NewProjectRepository(db)
	deploymentRepo := repositories.NewDeploymentRepository(db)
	domainRepo := repositories.NewDomainRepository(db)
	envRepo := repositories.NewEnvVarRepository(db)
	logRepo := repositories.NewLogRepository(db)
	auditRepo := repositories.NewAuditRepository(db)
	notifRepo := repositories.NewNotificationRepository(db)
	webhookRepo := repositories.NewWebhookRepository(db)
	aiConfigRepo := repositories.NewAIConfigRepository(db)
	userRepo := repositories.NewUserRepository(db)
	editorRepo := repositories.NewEditorStateRepository(db)
	gitSyncRepo := repositories.NewGitSyncRepository(db)
	commitRepo := repositories.NewCommitRepository(db)
	taskRepo := repositories.NewTaskRepository(db)
	registryRepo := repositories.NewRegistryManagementRepository(db)

	// Services
	authSvc := services.NewAuthService(userRepo, cfg)
	taskDispatcher := services.NewTaskDispatcher(taskRepo, projectRepo, workerNodeRepo, rdb, opts.InProcessQueue, &log.Logger)
	projectSvc := services.NewProjectService(cfg, projectRepo, deploymentRepo, taskRepo, taskDispatcher)
	deploymentSvc := services.NewDeploymentService(deploymentRepo, projectRepo, envRepo, domainRepo, rdb, opts.InProcessQueue, taskDispatcher, cfg.BaseURL)
	projectSvc.SetDeploymentService(deploymentSvc)
	logSvc := services.NewLogService(logRepo)
	domainSvc := services.NewDomainService(domainRepo, projectRepo)
	envSvc := services.NewEnvService(envRepo, projectRepo)
	auditSvc := services.NewAuditService(auditRepo)
	notifSvc := services.NewNotificationService(notifRepo, cfg)
	oauthSvc := services.NewOAuthService(userRepo, cfg, authSvc, db)
	webhookSvc := services.NewWebhookService(webhookRepo, projectRepo, deploymentSvc, cfg)
	aiSvc := services.NewAIService(cfg)
	aiExecutor := services.NewAIToolsExecutor(deploymentSvc, logSvc, aiSvc)

	registrySvc := services.NewRegistryService(cfg, projectSvc)
	replicationWorker := services.NewReplicationWorker(cfg, registryRepo, &log.Logger)

	gitSyncSvc := services.NewGitSyncService(gitSyncRepo, projectRepo, deploymentRepo, cfg.ProjectsDir)
	gitSyncWorker := services.NewGitSyncWorker(gitSyncSvc, gitSyncRepo, deploymentRepo, projectRepo, commitRepo, taskDispatcher, notifSvc, rdb, cfg, &log.Logger)

	// Start Registry Replication Worker if enabled
	if cfg.RegistryEnabled {
		replicationWorker.Start(ctx)
	}

	// Build Workers
	buildWorkers := make([]*services.BuildWorker, 0, cfg.BuildWorkers)
	for i := 0; i < cfg.BuildWorkers; i++ {
		buildWorkers = append(buildWorkers, services.NewBuildWorker(i, taskDispatcher, projectRepo, commitRepo, rdb, &log.Logger, cfg.BuildsDir, cfg.ProjectsDir))
	}

	// Test Workers
	testWorkers := make([]*services.TestWorker, 0, cfg.TestWorkers)
	for i := 0; i < cfg.TestWorkers; i++ {
		testWorkers = append(testWorkers, services.NewTestWorker(i, taskDispatcher, projectRepo, commitRepo, rdb, &log.Logger, cfg.TestsDir))
	}

	// AI Workers
	aiWorkers := make([]*services.AIWorker, 0, cfg.AIWorkers)
	for i := 0; i < cfg.AIWorkers; i++ {
		aiWorkers = append(aiWorkers, services.NewAIWorker(i, commitRepo, projectRepo, aiSvc, &log.Logger))
	}

	kvRepo, err := repositories.NewKVRepository(cfg.KVStorePath)
	if err != nil {
		log.Warn().Err(err).Msg("failed to initialize fast KV store (BadgerDB), syncing may be slower")
	} else {
		defer kvRepo.Close()
	}

	reg := &router.ServiceRegistry{
		AuthSvc:        authSvc,
		ProjectSvc:     projectSvc,
		DeploymentSvc:  deploymentSvc,
		LogSvc:         logSvc,
		DomainSvc:      domainSvc,
		EnvSvc:         envSvc,
		AuditSvc:       auditSvc,
		NotifSvc:       notifSvc,
		OAuthSvc:       oauthSvc,
		WebhookSvc:     webhookSvc,
		AISvc:          aiSvc,
		WorkerSvc:      services.NewWorkerNodeService(workerNodeRepo, systemCfgRepo),
		AIExecutor:     aiExecutor,
		TaskDispatcher: taskDispatcher,
		UserRepo:       userRepo,
		ProjectRepo:    projectRepo,
		DeploymentRepo: deploymentRepo,
		LogRepo:        logRepo,
		DomainRepo:     domainRepo,
		EnvRepo:        envRepo,
		AuditRepo:      auditRepo,
		NotifRepo:      notifRepo,
		WebhookRepo:    webhookRepo,
		AIConfigRepo:   aiConfigRepo,
		WorkerRepo:     workerNodeRepo,
		SystemRepo:     systemCfgRepo,
		EditorRepo:     editorRepo,
		CommitRepo:     commitRepo,
		TaskRepo:       taskRepo,
		KVRepo:         kvRepo,
		RegistryRepo:   registryRepo,
		RegistrySvc:    registrySvc,
	}

	// Background Tasks
	go deploymentSvc.StartAutoSyncLoop(ctx)
	// Start Workers (only if not using in-process queue provided by all-in-one mode)
	if opts.InProcessQueue == nil {
		if err := gitSyncWorker.Start(ctx, nil); err != nil {
			log.Error().Err(err).Msg("failed to start internal git sync worker")
		}
		for _, bw := range buildWorkers {
			if err := bw.Start(ctx, nil); err != nil {
				log.Error().Err(err).Int("id", bw.WorkerID).Msg("failed to start internal build worker")
			}
		}
		for _, tw := range testWorkers {
			if err := tw.Start(ctx, nil); err != nil {
				log.Error().Err(err).Int("id", tw.WorkerID).Msg("failed to start internal test worker")
			}
		}
		for _, aw := range aiWorkers {
			if err := aw.Start(ctx, nil); err != nil {
				log.Error().Err(err).Int("id", aw.WorkerID).Msg("failed to start internal AI worker")
			}
		}

		// Recover and re-queue any tasks that were running or pending before restart
		go func() {
			time.Sleep(2 * time.Second) // Give workers a moment to initialize
			taskDispatcher.RecoverStuckTasks(ctx)
		}()
	}

	// AI Monitoring Service
	aiMonitorSvc := services.NewAIMonitorService(aiSvc, aiConfigRepo, deploymentRepo, logRepo)
	go aiMonitorSvc.Start(ctx)

	// Graceful Shutdown
	go func() {
		<-ctx.Done()
		deploymentSvc.Shutdown()
	}()

	// Detect whether the frontend was compiled into the binary.
	// In dev mode ui/dist only contains a placeholder, so uiFS stays nil.
	var uiFS fs.FS
	if _, ferr := ui.FS.Open("dist/index.html"); ferr == nil {
		if sub, serr := fs.Sub(ui.FS, "dist"); serr == nil {
			uiFS = sub
			log.Info().Msg("serving embedded frontend")
		}
	}

	// Main User API Router
	r := router.New(cfg, db, rdb, uiFS, opts.InProcessQueue, reg)

	// Worker Management Router
	workerNodeSvc := services.NewWorkerNodeService(workerNodeRepo, systemCfgRepo)
	workerHandler := handlers.NewWorkerHandler(workerNodeSvc)
	workerR := router.NewWorkerRouter(cfg, workerHandler)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	workerSrv := &http.Server{
		Addr:         ":" + cfg.WorkerPort,
		Handler:      workerR,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		log.Info().Str("port", cfg.Port).Str("version", "v1.0.0").Msg("Pushpaka API starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	go func() {
		log.Info().Str("port", cfg.WorkerPort).Msg("Pushpaka Worker Management server starting")
		if err := workerSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Info().Msg("shutting down API and Worker servers...")
	tunnel.GlobalManager.CloseAll()
	if err := workerSrv.Shutdown(shutCtx); err != nil {
		log.Error().Err(err).Msg("worker server shutdown error")
	}
	return srv.Shutdown(shutCtx)
}
