package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

type Config struct {
	DatabaseDriver string // "postgres" (default) or "sqlite"
	DatabaseURL    string
	RedisURL       string
	DockerHost     string
	TraefikNetwork string
	// CloneDir is the temporary workspace where repositories are git-cloned
	CloneDir string
	// ProjectsDir is the persistent directory for project source code.
	ProjectsDir string
	// BuildsDir is the persistent directory for build artifacts.
	BuildsDir     string
	DeploysDir    string
	TestsDir      string
	RegistryDir   string
	BuildWorkers  int
	SyncWorkers   int
	TestWorkers   int
	AIWorkers     int
	DeployWorkers int
	AppEnv        string
	// AI Settings
	AIProvider string
	AIAPIKey   string
	AIModel    string
	AIBaseURL  string
	ServerURL  string
}

func Load() *Config {
	workers, _ := strconv.Atoi(getEnv("BUILD_WORKERS", "3"))
	syncWorkers, _ := strconv.Atoi(getEnv("SYNC_WORKERS", "4"))
	testWorkers, _ := strconv.Atoi(getEnv("TEST_WORKERS", getEnv("TEST_WORKER", "2")))
	aiWorkers, _ := strconv.Atoi(getEnv("AI_WORKERS", "2"))
	deployWorkers, _ := strconv.Atoi(getEnv("DEPLOY_WORKERS", "1"))

	// CloneDir: prefer BUILD_CLONE_DIR, fall back to legacy BUILD_DIR, then platform default.
	cloneDir := getEnv("BUILD_CLONE_DIR", "")
	if cloneDir == "" {
		cloneDir = getEnv("BUILD_DIR", defaultCloneDir())
	}

	deployDir := getEnv("BUILD_DEPLOY_DIR", getEnv("DEPLOYS_DIR", defaultDeployDir()))

	return &Config{
		DatabaseDriver: getEnv("DATABASE_DRIVER", "postgres"),
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://pushpaka:pushpaka@localhost:5432/pushpaka?sslmode=disable"),
		RedisURL:       getEnv("REDIS_URL", "redis://localhost:6379"),
		DockerHost:     getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		TraefikNetwork: getEnv("TRAEFIK_NETWORK", "pushpaka-network"),
		CloneDir:       cloneDir,
		ProjectsDir:    getEnv("PROJECTS_DIR", defaultProjectsDir()),
		BuildsDir:      getEnv("BUILDS_DIR", defaultBuildsDir()),
		DeploysDir:     getEnv("DEPLOYS_DIR", deployDir),
		TestsDir:       getEnv("TESTS_DIR", filepath.Join(defaultPushpakaBase(), "tests")),
		RegistryDir:    getEnv("REGISTRY_DIR", filepath.Join(defaultPushpakaBase(), "registry")),
		BuildWorkers:   workers,
		SyncWorkers:    syncWorkers,
		TestWorkers:    testWorkers,
		AIWorkers:      aiWorkers,
		DeployWorkers:  deployWorkers,
		AppEnv:         getEnv("APP_ENV", "development"),
		AIProvider:     getEnv("AI_PROVIDER", "openai"),
		AIAPIKey:       getEnv("AI_API_KEY", ""),
		AIModel:        getEnv("AI_MODEL", "gpt-4o-mini"),
		AIBaseURL:      getEnv("AI_BASE_URL", ""),
		ServerURL:      getEnv("SERVER_URL", "http://localhost:8080"),
	}
}

// EnsureDirs creates CloneDir and DeployDir (and their parents) if they do
// not already exist. Call this once at worker startup before processing jobs.
func (c *Config) EnsureDirs() error {
	dirs := []string{c.CloneDir, c.ProjectsDir, c.BuildsDir, c.DeploysDir, c.TestsDir, c.RegistryDir}
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

// defaultCloneDir returns a platform-appropriate temp directory for git clones.
func defaultCloneDir() string {
	return filepath.Join(os.TempDir(), "pushpaka-builds")
}

// defaultDeployDir returns a platform-appropriate permanent directory for
// running in-place (no-Docker) deployments.
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

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
