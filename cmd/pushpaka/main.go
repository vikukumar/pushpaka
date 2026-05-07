// Command pushpaka is the combined Pushpaka binary.
// Use -dev flag for local SQLite development (no Postgres/Redis required).
// Use PUSHPAKA_COMPONENT={api|worker|all} to select which component to run.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	backendApp "github.com/vikukumar/pushpaka/app"
	"github.com/vikukumar/pushpaka/queue"
	workerApp "github.com/vikukumar/pushpaka/worker/app"
)

func main() {
	// Load .env file if it exists (non-fatal if missing)
	_ = godotenv.Load()

	// Build DATABASE_URL from individual env vars if not already set
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DB_HOST") != "" {
		buildDatabaseURL()
	}

	// Build REDIS_URL from individual env vars if not already set
	if os.Getenv("REDIS_URL") == "" && os.Getenv("REDIS_HOST") != "" {
		buildRedisURLENV()
	}

	dev := flag.Bool("dev", false, "dev mode: use SQLite + embedded worker (no Postgres/Redis required)")
	configPath := flag.String("config", os.Getenv("PUSHPAKA_CONFIG_FILE"), "path to config.yaml")
	mode := flag.String("mode", "", "config mode: development, staging, production")
	flag.Parse()

	if *configPath != "" {
		selectedMode := *mode
		if selectedMode == "" {
			if *dev {
				selectedMode = "development"
			} else if envMode := os.Getenv("APP_ENV"); envMode != "" {
				selectedMode = envMode
			} else {
				selectedMode = "production"
			}
		}
		if err := applyConfigFile(*configPath, selectedMode); err != nil {
			log.Fatal().Err(err).Msg("failed to load config file")
		}
		log.Info().Str("config_file", *configPath).Str("mode", normalizeConfigMode(selectedMode)).Msg("loaded config file")
	}

	if *dev {
		os.Setenv("DATABASE_DRIVER", "sqlite")
		os.Setenv("DATABASE_URL", "pushpaka-dev.db")
		os.Setenv("REDIS_URL", "")
		os.Setenv("REDIS_ENABLED", "false")
		os.Setenv("APP_ENV", "development")
		os.Setenv("PORT", "8080")
		setIfEmpty("JWT_SECRET", "dev-secret-change-in-production")
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if os.Getenv("APP_ENV") == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Component resolution order:
	//  1. CLI positional arg: pushpaka <all|api|worker>
	//  2. PUSHPAKA_COMPONENT environment variable
	//  3. Default: "all"
	component := os.Getenv("PUSHPAKA_COMPONENT")
	if args := flag.Args(); len(args) > 0 {
		component = args[0]
	}
	if component == "" {
		component = "all"
	}

	var wg sync.WaitGroup

	switch component {
	case "all":
		// API + embedded worker in one process, connected by a fast in-process channel.
		// Works in both -dev mode (SQLite) and production (Postgres).
		// Redis is NOT used for job routing in this mode — the in-process channel
		// handles all dispatch and gives the API live worker/job counts.
		// For horizontal worker scaling, use PUSHPAKA_COMPONENT=api + PUSHPAKA_COMPONENT=worker.
		//
		// IMPORTANT: open ONE shared database pool and hand it to BOTH components.
		// This prevents SQLITE_BUSY_SNAPSHOT (error 261) in WAL mode: that error
		// occurs when two separate connection pools hold overlapping read snapshots
		// and one tries to write.  A single pool serialises all access.
		sharedDB, err := backendApp.OpenDB(os.Getenv("DATABASE_DRIVER"), os.Getenv("DATABASE_URL"))
		if err != nil {
			log.Fatal().Err(err).Msg("failed to open shared database")
		}

		sqlDB, err := sharedDB.DB()
		if err != nil {
			log.Fatal().Err(err).Msg("failed to extract sql.DB from gorm")
		}
		defer sqlDB.Close()

		log.Info().Bool("dev", *dev).Msg("all-in-one mode: internal workers will run in-process")

		q := queue.New(100)

		wg.Add(1)
		go func() {
			defer wg.Done()
			opts := backendApp.RunOptions{InProcessQueue: q, DB: sharedDB}
			if err := backendApp.RunWithOptions(ctx, opts); err != nil {
				log.Error().Err(err).Msg("API server error")
				cancel()
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := workerApp.RunInProcessWithDB(ctx, q, nil, sharedDB); err != nil {
				log.Error().Err(err).Msg("Internal workers error")
				cancel()
			}
		}()

	case "api":
		// API only — expects external worker processes reading from Redis.
		log.Info().Msg("api-only mode: external workers must connect via Redis")
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := backendApp.Run(ctx); err != nil {
				log.Error().Err(err).Msg("API server error")
				cancel()
			}
		}()

	case "worker":
		// Worker only — reads jobs from Redis, pairs with PUSHPAKA_COMPONENT=api.
		modeOpt := os.Getenv("MODE")
		if modeOpt == "" {
			modeOpt = "hybrid" // default for standalone worker
		}
		roleOpt := os.Getenv("WORKER_ROLE")
		serverURL := os.Getenv("SERVER_URL")

		log.Info().Str("mode", modeOpt).Str("role", roleOpt).Msg("worker-only mode starting")
		wg.Add(1)
		go func() {
			defer wg.Done()
			opts := workerApp.RunOptions{
				Mode:      modeOpt,
				ServerURL: serverURL,
				Role:      roleOpt,
			}
			if err := workerApp.Run(ctx, opts); err != nil {
				log.Error().Err(err).Msg("worker error")
				cancel()
			}
		}()

	default:
		log.Fatal().Str("component", component).Msg("unknown PUSHPAKA_COMPONENT; valid values: all, api, worker")
	}

	wg.Wait()
	log.Info().Msg("pushpaka stopped")
}

func setIfEmpty(key, val string) {
	if os.Getenv(key) == "" {
		os.Setenv(key, val)
	}
}

// buildDatabaseURL constructs DATABASE_URL from individual DB_* env vars
func buildDatabaseURL() {
	driver := os.Getenv("DATABASE_DRIVER")
	if driver == "" {
		driver = "postgres"
	}

	if driver == "sqlite" {
		dbpath := os.Getenv("DB_NAME")
		if dbpath == "" {
			dbpath = os.Getenv("DB_PATH")
		}
		if dbpath == "" {
			dbpath = "pushpaka.db"
		}
		os.Setenv("DATABASE_URL", dbpath)
		return
	}

	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")

	var url string
	switch driver {
	case "mysql":
		if port == "" {
			port = "3306" // Default MySQL port
		}
		url = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", user, password, host, port, dbname)
	case "sqlserver", "mssql":
		if port == "" {
			port = "1433" // Default SQL Server port
		}
		url = fmt.Sprintf("sqlserver://%s:%s@%s:%s?database=%s", user, password, host, port, dbname)
	default:
		// postgres
		if port == "" {
			port = "5432" // Default PostgreSQL port
		}
		sslmode := os.Getenv("DB_SSL_MODE")
		if sslmode == "" {
			sslmode = "prefer"
		}
		url = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, dbname, sslmode)
	}

	os.Setenv("DATABASE_URL", url)
}

// buildRedisURLENV constructs REDIS_URL from individual REDIS_* env vars
func buildRedisURLENV() {
	host := os.Getenv("REDIS_HOST")
	if host == "" {
		return
	}

	port := os.Getenv("REDIS_PORT")
	if port == "" {
		port = "6379"
	}

	password := os.Getenv("REDIS_PASSWORD")
	db := os.Getenv("REDIS_DB")
	if db == "" {
		db = "0"
	}

	// Build redis URL
	if password != "" {
		os.Setenv("REDIS_URL", fmt.Sprintf("redis://:%s@%s:%s/%s", password, host, port, db))
	} else {
		os.Setenv("REDIS_URL", fmt.Sprintf("redis://%s:%s/%s", host, port, db))
	}
}

func init() {
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "Usage: %s [flags] [all|api|worker]\n\n", os.Args[0])
		fmt.Fprintln(out, "Components:")
		fmt.Fprintln(out, "  all     Run API server + embedded worker in a single process (default)")
		fmt.Fprintln(out, "  api     Run API server only (requires external worker + Redis)")
		fmt.Fprintln(out, "  worker  Run worker only (requires API server + Redis)")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Flags:")
		flag.PrintDefaults()
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "The component can also be set via the PUSHPAKA_COMPONENT environment variable.")
		fmt.Fprintln(out, "CLI arg takes priority over the environment variable.")
		fmt.Fprintln(out, "The config file, when provided, overrides environment-derived values for the selected mode.")
	}
}
