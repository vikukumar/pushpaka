package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	Port               string
	WorkerPort         string // Dedicated port for worker sync API
	DatabaseDriver     string // "postgres" (default) or "sqlite"
	DatabaseURL        string
	DatabaseConfig     *DatabaseConfig // Structured database config (alternative to DSN)
	RedisURL           string
	RedisConfig        *RedisConfig // Structured Redis config (alternative to URL)
	JWTSecret          string
	JWTExpiry          int // hours
	AppEnv             string
	AppMode            string // development, staging, production
	BaseURL            string // public base URL, e.g. https://push.example.com
	AllowedOrigins     []string
	DockerHost         string
	TraefikNetwork     string
	GithubClientID     string
	GithubClientSecret string
	// GitLab OAuth (also supports self-hosted instances via GitlabBaseURL)
	GitlabClientID     string
	GitlabClientSecret string
	GitlabBaseURL      string // default: https://gitlab.com
	// CloneDir is the temporary workspace where repositories are git-cloned.
	CloneDir string
	// ProjectsDir is the persistent directory for project source code.
	// Set via PROJECTS_DIR.
	ProjectsDir string
	// BuildsDir is the persistent directory for build artifacts.
	// Set via BUILDS_DIR.
	BuildsDir string
	// DeployDir is the directory where active deployments run.
	// Each deployment gets its own subdirectory by ID.
	// Set via DEPLOYS_DIR.
	DeploysDir string
	TestsDir   string
	// Registry settings
	RegistryDir     string
	RegistryEnabled bool
	KVStorePath     string
	LogLevel        string

	// Worker counts
	BuildWorkers  int
	AIWorkers     int
	SyncWorkers   int
	TestWorkers   int
	DeployWorkers int

	// Limits
	CommitLimit int

	// AI integration (OpenAI-compatible API)
	AIProvider string // "openai", "openrouter", "gemini", "anthropic", "ollama"
	AIAPIKey   string
	AIModel    string // e.g. "gpt-4o-mini", "claude-3-haiku", "gemini-pro"
	AIBaseURL  string // override endpoint for OpenRouter / Ollama / self-hosted
	// AIRateLimitPerUserPerDay caps daily AI calls per user when they are using the
	// global platform key (not their own). 0 = unlimited.
	AIRateLimitPerUserPerDay int

	// RBAC
	FirstAdminEmail string // Auto-promote this user to admin on registration/startup

	// Default notification channels (can be overridden per-user in DB)
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
}

func Load() *Config {
	jwtExpiry, _ := strconv.Atoi(getEnv("JWT_EXPIRY_HOURS", "24"))
	smtpPort, _ := strconv.Atoi(getEnv("SMTP_PORT", "587"))

	cloneDir := getEnv("BUILD_CLONE_DIR", "")
	if cloneDir == "" {
		cloneDir = getEnv("BUILD_DIR", defaultCloneDir())
	}
	deployDir := getEnv("BUILD_DEPLOY_DIR", defaultDeployDir())

	appMode := normalizeMode(getEnv("APP_ENV", "development"))

	// Build DatabaseURL from structured config if DATABASE_URL not explicitly set
	databaseURL := getEnv("DATABASE_URL", "")
	dbDriver := getEnv("DATABASE_DRIVER", "postgres")
	if databaseURL == "" && dbDriver != "sqlite" {
		dbCfg := LoadDatabaseConfig(appMode)
		databaseURL = dbCfg.BuildDSN(dbDriver)
	} else if dbDriver == "sqlite" && databaseURL == "" {
		databaseURL = getEnv("DB_NAME", "pushpaka-dev.db")
	}

	// Build RedisURL from structured config if REDIS_URL not explicitly set
	redisURL := getEnv("REDIS_URL", "")
	if redisURL == "" && getEnv("REDIS_HOST", "") != "" {
		redisCfg := LoadRedisConfig(appMode)
		redisURL = redisCfg.BuildRedisURL()
	}

	return &Config{
		Port:               getEnv("PORT", "8080"),
		WorkerPort:         getEnv("WORKER_PORT", "8081"),
		DatabaseDriver:     getEnv("DATABASE_DRIVER", "postgres"),
		DatabaseURL:        databaseURL,
		DatabaseConfig:     LoadDatabaseConfig(appMode),
		RedisURL:           redisURL,
		RedisConfig:        LoadRedisConfig(appMode),
		JWTSecret:          getEnv("JWT_SECRET", "change-me-in-production"),
		JWTExpiry:          jwtExpiry,
		AppEnv:             getEnv("APP_ENV", "development"),
		AppMode:            appMode,
		BaseURL:            getEnv("BASE_URL", "http://localhost:"+getEnv("PORT", "8080")),
		AllowedOrigins:     strings.Split(getEnv("CORS_ORIGINS", "http://localhost:3000"), ","),
		DockerHost:         getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		TraefikNetwork:     getEnv("TRAEFIK_NETWORK", "pushpaka-network"),
		GithubClientID:     getEnv("GITHUB_CLIENT_ID", ""),
		GithubClientSecret: getEnv("GITHUB_CLIENT_SECRET", ""),
		GitlabClientID:     getEnv("GITLAB_CLIENT_ID", ""),
		GitlabClientSecret: getEnv("GITLAB_CLIENT_SECRET", ""),
		GitlabBaseURL:      getEnv("GITLAB_BASE_URL", "https://gitlab.com"),
		CloneDir:           cloneDir,
		ProjectsDir:        getEnv("PROJECTS_DIR", defaultProjectsDir()),
		BuildsDir:          getEnv("BUILDS_DIR", defaultBuildsDir()),
		DeploysDir:         getEnv("DEPLOYS_DIR", deployDir),
		TestsDir:           getEnv("TESTS_DIR", filepath.Join(defaultPushpakaBase(), "tests")),
		RegistryDir:        getEnv("REGISTRY_DIR", filepath.Join(defaultPushpakaBase(), "registry")),
		RegistryEnabled:    getEnv("REGISTRY_ENABLED", "true") == "true",
		KVStorePath:        getEnv("KV_STORE_PATH", filepath.Join(defaultPushpakaBase(), "kv")),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		BuildWorkers: func() int {
			v, _ := strconv.Atoi(getEnv("BUILD_WORKERS", "3"))
			return v
		}(),
		AIWorkers: func() int {
			v, _ := strconv.Atoi(getEnv("AI_WORKERS", "2"))
			return v
		}(),
		SyncWorkers: func() int {
			v, _ := strconv.Atoi(getEnv("SYNC_WORKERS", "4"))
			return v
		}(),
		DeployWorkers: func() int {
			v, _ := strconv.Atoi(getEnv("DEPLOY_WORKERS", "1"))
			return v
		}(),
		TestWorkers: func() int {
			// Backwards compatibility: check TEST_WORKERS then TEST_WORKER
			v, err := strconv.Atoi(getEnv("TEST_WORKERS", ""))
			if err != nil {
				v, _ = strconv.Atoi(getEnv("TEST_WORKER", "2"))
			}
			return v
		}(),
		CommitLimit: func() int {
			v, _ := strconv.Atoi(getEnv("COMMIT_LIMIT", "3"))
			return v
		}(),
		AIProvider: getEnv("AI_PROVIDER", "openai"),
		AIAPIKey:   getEnv("AI_API_KEY", ""),
		AIModel:    getEnv("AI_MODEL", "gpt-4o-mini"),
		AIBaseURL:  getEnv("AI_BASE_URL", ""),
		AIRateLimitPerUserPerDay: func() int {
			v, _ := strconv.Atoi(getEnv("AI_RATE_LIMIT_PER_USER_PER_DAY", "0"))
			return v
		}(),
		FirstAdminEmail: getEnv("FIRST_ADMIN_EMAIL", ""),
		SMTPHost:        getEnv("SMTP_HOST", ""),
		SMTPPort:        smtpPort,
		SMTPUsername:    getEnv("SMTP_USERNAME", ""),
		SMTPPassword:    getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:        getEnv("SMTP_FROM", ""),
	}
}

// EnsureDirs creates CloneDir and DeployDir if they do not already exist.
func (c *Config) EnsureDirs() error {
	dirs := []string{c.CloneDir, c.ProjectsDir, c.BuildsDir, c.DeploysDir, c.TestsDir, c.KVStorePath, c.RegistryDir}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("pushpaka: cannot create directory %q: %w", dir, err)
		}
	}
	return nil
}

func defaultCloneDir() string {
	return filepath.Join(os.TempDir(), "pushpaka-builds")
}

func defaultProjectsDir() string {
	return filepath.Join(defaultPushpakaBase(), "projects")
}

func defaultBuildsDir() string {
	return filepath.Join(defaultPushpakaBase(), "builds")
}

func defaultDeployDir() string {
	return filepath.Join(defaultPushpakaBase(), "deployments")
}

func defaultPushpakaBase() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.Getenv("USERPROFILE")
		}
		if base == "" {
			base = `C:\pushpaka`
		}
		return filepath.Join(base, "pushpaka")
	}
	return "/var/lib/pushpaka"
}

// normalizeMode converts app environment names to standard modes
func normalizeMode(env string) string {
	switch strings.ToLower(env) {
	case "prod", "production":
		return "production"
	case "stage", "staging":
		return "staging"
	default:
		return "development"
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
