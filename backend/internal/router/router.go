package router

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/internal/handlers"
	"github.com/vikukumar/pushpaka/internal/middleware"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/internal/services"
	"github.com/vikukumar/pushpaka/pkg/tunnel"
	"github.com/vikukumar/pushpaka/queue"
)

// ServiceRegistry holds all the services and repositories needed by the router.
type ServiceRegistry struct {
	AuthSvc        *services.AuthService
	ProjectSvc     *services.ProjectService
	DeploymentSvc  *services.DeploymentService
	LogSvc         *services.LogService
	DomainSvc      *services.DomainService
	EnvSvc         *services.EnvService
	AuditSvc       *services.AuditService
	NotifSvc       *services.NotificationService
	OAuthSvc       *services.OAuthService
	WebhookSvc     *services.WebhookService
	AISvc          *services.AIService
	WorkerSvc      *services.WorkerNodeService
	AIExecutor     *services.AIToolsExecutor
	TaskDispatcher *services.TaskDispatcher
	RegistrySvc    *services.RegistryService

	UserRepo       *repositories.UserRepository
	ProjectRepo    *repositories.ProjectRepository
	DeploymentRepo *repositories.DeploymentRepository
	CommitRepo     *repositories.CommitRepository
	LogRepo        *repositories.LogRepository
	DomainRepo     *repositories.DomainRepository
	EnvRepo        *repositories.EnvVarRepository
	AuditRepo      *repositories.AuditRepository
	NotifRepo      *repositories.NotificationRepository
	WebhookRepo    *repositories.WebhookRepository
	AIConfigRepo   *repositories.AIConfigRepository
	WorkerRepo     *repositories.WorkerNodeRepository
	SystemRepo     *repositories.SystemConfigRepository
	EditorRepo     *repositories.EditorStateRepository
	TaskRepo       *repositories.TaskRepository
	KVRepo         *repositories.KVRepository
}

// New builds the Gin engine. Pass a non-nil uiFS to serve the embedded
// Next.js static export under /; pass nil to skip frontend serving (dev mode).
// inQueue is only non-nil in dev mode (embedded worker, no Redis).
// New builds the Gin engine.
func New(
	cfg *config.Config,
	db *gorm.DB,
	rdb *redis.Client,
	uiFS fs.FS,
	inQueue *queue.InProcess,
	reg *ServiceRegistry,
) *gin.Engine {
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// Global middleware
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())
	r.Use(middleware.SecureHeaders())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-API-Key"},
		AllowCredentials: true,
	}))

	// Repositories & Services from Registry
	projectRepo := reg.ProjectRepo
	deploymentRepo := reg.DeploymentRepo
	logRepo := reg.LogRepo
	aiConfigRepo := reg.AIConfigRepo

	authSvc := reg.AuthSvc
	projectSvc := reg.ProjectSvc
	deploymentSvc := reg.DeploymentSvc
	logSvc := reg.LogSvc
	domainSvc := reg.DomainSvc
	envSvc := reg.EnvSvc
	auditSvc := reg.AuditSvc
	notifSvc := reg.NotifSvc
	oauthSvc := reg.OAuthSvc
	webhookSvc := reg.WebhookSvc
	aiSvc := reg.AISvc
	workerSvc := reg.WorkerSvc
	aiExecutor := reg.AIExecutor
	taskDispatcher := reg.TaskDispatcher
	registrySvc := reg.RegistrySvc

	// Handlers
	authHandler := handlers.NewAuthHandler(authSvc)
	projectHandler := handlers.NewProjectHandler(projectSvc, auditSvc)
	deploymentHandler := handlers.NewDeploymentHandler(deploymentSvc, auditSvc)
	logHandler := handlers.NewLogHandler(logSvc)
	domainHandler := handlers.NewDomainHandler(domainSvc)
	envHandler := handlers.NewEnvHandler(envSvc)
	auditHandler := handlers.NewAuditHandler(auditSvc)
	notifHandler := handlers.NewNotificationHandler(notifSvc)
	oauthHandler := handlers.NewOAuthHandler(oauthSvc)
	webhookHandler := handlers.NewWebhookHandler(webhookSvc)
	taskHandler := handlers.NewTaskHandler(taskDispatcher)

	// Create AI Handler
	aiHandler := handlers.NewAIHandler(aiSvc, logRepo, deploymentRepo, aiConfigRepo, cfg, aiExecutor)

	workerHandler := handlers.NewWorkerHandler(workerSvc)
	terminalHandler := handlers.NewTerminalHandler(deploymentRepo, cfg)
	fileHandler := handlers.NewFileHandler(projectRepo, deploymentRepo, cfg)
	gitHandler := handlers.NewGitHandler(projectRepo, deploymentRepo, cfg)
	infraHandler := handlers.NewInfraHandler()
	// Avoid the nil-interface trap: a nil *queue.InProcess assigned directly to
	// WorkerStatsProvider creates a non-nil interface with a nil concrete value,
	// which passes the != nil check but panics on method calls.
	var workerStats handlers.WorkerStatsProvider
	if inQueue != nil {
		workerStats = inQueue
	}
	// Recover deployments and tasks in background after restart
	go deploymentSvc.RecoverRunningDeployments(context.Background())
	go reg.TaskDispatcher.RecoverStuckTasks(context.Background())

	healthHandler := handlers.NewHealthHandler(db, rdb, workerStats)
	editorHandler := handlers.NewEditorStateHandler(reg.EditorRepo)
	editorWSHandler := handlers.NewEditorWSHandler(projectRepo, deploymentRepo, cfg)

	// Auth middleware
	authMW := middleware.JWT(authSvc)

	// API v1
	api := r.Group("/api/v1")
	{
		// Health & Metrics (public)
		api.GET("/health", healthHandler.Health)
		api.GET("/ready", healthHandler.Ready)
		api.GET("/system", healthHandler.System)
		api.GET("/metrics", handlers.MetricsHandler())

		// Auth (public) — rate-limited to prevent brute-force
		auth := api.Group("/auth")
		auth.Use(middleware.RateLimit("auth"))
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			// OAuth flows (public — redirects handle the JWT issuance)
			auth.GET("/github", oauthHandler.GithubRedirect)
			auth.GET("/github/callback", oauthHandler.GithubCallback)
			auth.GET("/gitlab", oauthHandler.GitlabRedirect)
			auth.GET("/gitlab/callback", oauthHandler.GitlabCallback)
		}

		// Incoming webhooks (public — signature-verified)
		api.POST("/webhooks/:id/receive", webhookHandler.Receive)

		// Internal callbacks for worker communication
		api.POST("/internal/notify", notifHandler.InternalNotify)
		api.POST("/internal/tasks/:id/complete", taskHandler.InternalComplete)

		// Protected API routes
		protected := api.Group("")
		protected.Use(authMW)
		protected.Use(middleware.RateLimit("api"))
		{
			// Projects
			projects := protected.Group("/projects")
			{
				projects.POST("", projectHandler.Create)
				projects.GET("", projectHandler.List)
				projects.GET("/:id", projectHandler.Get)
				projects.PUT("/:id", projectHandler.Update)
				projects.DELETE("/:id", projectHandler.Delete)
				projects.POST("/:id/sync", deploymentHandler.Sync)
			}

			// Deployments
			deployments := protected.Group("/deployments")
			{
				deployments.GET("/stats", deploymentHandler.Stats)
				deployments.POST("", deploymentHandler.Deploy)
				deployments.GET("", deploymentHandler.List)
				deployments.GET("/:id", deploymentHandler.Get)
				deployments.POST("/:id/rollback", deploymentHandler.Rollback)
				deployments.POST("/:id/restart", deploymentHandler.Restart)
				deployments.PATCH("/:id/promote", deploymentHandler.Promote)
				deployments.DELETE("/:id", deploymentHandler.Delete)
			}

			// Logs (REST + WebSocket)
			logs := protected.Group("/logs")
			{
				logs.GET("/:id", logHandler.GetLogs)
				logs.GET("/:id/stream", logHandler.StreamLogs)
			}

			// Domains
			domains := protected.Group("/domains")
			{
				domains.POST("", domainHandler.Add)
				domains.GET("", domainHandler.List)
				domains.DELETE("/:id", domainHandler.Delete)
			}

			// Environment Variables
			env := protected.Group("/env")
			{
				env.POST("", envHandler.Set)
				env.GET("", envHandler.List)
				env.DELETE("", envHandler.Delete)
			}

			// Webhooks (management; the receive endpoint is public above)
			webhooks := protected.Group("/webhooks")
			{
				webhooks.POST("", webhookHandler.Create)
				webhooks.GET("", webhookHandler.List)
				webhooks.DELETE("/:id", webhookHandler.Delete)
			}

			// Notifications config
			notifications := protected.Group("/notifications")
			{
				notifications.GET("/config", notifHandler.Get)
				notifications.PUT("/config", notifHandler.Upsert)
			}

			// Audit logs
			protected.GET("/audit", auditHandler.List)

			// Tasks
			tasks := protected.Group("/tasks")
			{
				tasks.GET("", taskHandler.List)
				tasks.GET("/:id", taskHandler.Get)
				tasks.POST("/:id/restart", taskHandler.Restart)
			}

			// Web terminal (WebSocket)
			protected.GET("/deployments/:id/terminal", terminalHandler.Connect)

			// AI log analysis + chat assistant + config
			protected.POST("/deployments/:id/analyze", aiHandler.AnalyzeLogs)
			protected.POST("/ai/chat", aiHandler.Chat)
			protected.POST("/ai/agent", aiHandler.AgentChat)
			protected.POST("/ai/agent/execute", aiHandler.AgentExecute)
			protected.GET("/ai/config", aiHandler.GetAIConfig)
			protected.PUT("/ai/config", aiHandler.SaveAIConfig)

			// RAG knowledge base
			protected.GET("/ai/rag", aiHandler.ListRAG)
			protected.POST("/ai/rag", aiHandler.CreateRAG)
			protected.DELETE("/ai/rag/:id", aiHandler.DeleteRAG)

			// AI monitoring alerts
			protected.GET("/ai/alerts", aiHandler.ListAlerts)
			protected.PUT("/ai/alerts/:id/resolve", aiHandler.ResolveAlert)

			// AI usage stats (how many calls used today vs limit)
			protected.GET("/ai/usage", aiHandler.GetUsage)

			// Docker container management
			protected.GET("/containers", infraHandler.ListContainers)
			protected.POST("/containers/:id/start", infraHandler.StartContainer)
			protected.POST("/containers/:id/stop", infraHandler.StopContainer)
			protected.POST("/containers/:id/restart", infraHandler.RestartContainer)
			protected.GET("/containers/:id/logs", infraHandler.ContainerLogs)

			// Kubernetes management
			protected.GET("/k8s/namespaces", infraHandler.K8sNamespaces)
			protected.GET("/k8s/pods", infraHandler.K8sPods)
			protected.GET("/k8s/deployments", infraHandler.K8sDeployments)
			protected.GET("/k8s/services", infraHandler.K8sServices)
			protected.POST("/k8s/deployments/:namespace/:name/rollout", infraHandler.K8sRollout)

			// Worker Management Admin Routes
			workers := protected.Group("/workers")
			{
				workers.GET("", workerHandler.ListNodes)
				workers.POST("/pat", workerHandler.GetZonePAT)
			}

			// In-browser code editor (file browser + read + save + manipulations)
			protected.GET("/projects/:id/files", fileHandler.ListFiles)
			protected.GET("/projects/:id/files/*path", fileHandler.ReadFile)
			protected.PUT("/projects/:id/files/*path", fileHandler.SaveFile)
			protected.POST("/projects/:id/files/*path", fileHandler.CreateFile)
			protected.DELETE("/projects/:id/files/*path", fileHandler.DeleteFile)
			protected.PATCH("/projects/:id/files/*path", fileHandler.RenameFile)
			protected.POST("/projects/:id/directories/*path", fileHandler.CreateDirectory)

			// Sync (re-clone) project source to the editor working directory
			protected.POST("/projects/:id/editor-sync", fileHandler.SyncFiles)

			// System-wide Global File Management (/deploy root)
			system := protected.Group("/system")
			{
				system.GET("/files", fileHandler.ListSystemFiles)
				system.GET("/files/*path", fileHandler.ReadSystemFile)
				system.PUT("/files/*path", fileHandler.SaveSystemFile)
				system.POST("/files/*path", fileHandler.CreateSystemFile)
				system.DELETE("/files/*path", fileHandler.DeleteSystemFile)
				system.PATCH("/files/*path", fileHandler.RenameSystemFile)
				system.POST("/directories/*path", fileHandler.CreateSystemDirectory)
			}

			// Git operations
			protected.GET("/projects/:id/git/status", gitHandler.Status)
			protected.POST("/projects/:id/git/commit", gitHandler.Commit)
			protected.POST("/projects/:id/git/push", gitHandler.Push)
			protected.POST("/projects/:id/git/pull", gitHandler.Pull)

			// Editor State Persistence
			protected.GET("/projects/:id/editor/state", editorHandler.GetState)
			protected.POST("/projects/:id/editor/state", editorHandler.SaveState)
			protected.GET("/editor/ws", editorWSHandler.Connect)
			// Current user profile (used by OAuth callback to fetch full user after token)
			protected.GET("/auth/me", authHandler.Me)

			// Admin routes (role=admin only)
			admin := protected.Group("/admin")
			admin.Use(middleware.RequireAdmin())
			{
				adminHandler := handlers.NewAdminHandler(authSvc, reg.UserRepo)
				admin.GET("/users", adminHandler.ListUsers)
				admin.PUT("/users/:id/role", adminHandler.UpdateUserRole)
			}
		}
	}

	// Registry Routes (OCI & Binary)
	// These are served on the same port but separate prefix
	if registrySvc != nil {
		r.Any("/registry/oci/*ociPath", gin.WrapF(registrySvc.HandleOCI))
		r.Any("/registry/binary/*binaryPath", gin.WrapF(registrySvc.HandleBinary))
	}

	// /app/:projectID -- path-based deployment access (when no custom domain is set).
	// Proxies all requests to the running container's external port.
	r.Any("/app/:projectID/*proxyPath", newDeploymentProxyHandler(deploymentRepo))
	r.Any("/app/:projectID", newDeploymentProxyHandler(deploymentRepo))

	// Serve embedded frontend SPA (only when built with frontend assets).
	var spaH http.Handler
	if uiFS != nil {
		spaH = newSPAHandler(uiFS)
	}

	// Root handler:
	// 1. Check for custom domain match -> Proxy to deployment
	// 2. Serve SPA frontend
	r.NoRoute(func(c *gin.Context) {
		host := c.Request.Host
		// Check if this host is a custom domain
		domain, err := reg.DomainRepo.FindByDomain(host)
		if err == nil && domain != nil {
			// Proxy to project
			proxyToProject(c, domain.ProjectID, reg.DeploymentRepo)
			return
		}

		// SPA Fallback
		if spaH != nil {
			spaH.ServeHTTP(c.Writer, c.Request)
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": "path not found"})
		}
	})

	return r
}

func proxyToProject(c *gin.Context, projectID string, repo *repositories.DeploymentRepository) {
	// Prioritize the Default/Live deployment (Constant Endpoint)
	d, err := repo.FindDefaultByProjectID(projectID)
	if err != nil || d == nil {
		// Fallback to any running deployment if no default is set
		d, err = repo.FindRunningByProjectID(projectID)
	}

	if err != nil || d == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no running deployment for this project"})
		return
	}
	if d.ExternalPort == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deployment has no exposed port yet"})
		return
	}

	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", d.ExternalPort))
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Custom error handler to log proxy failures (502)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Error().Err(err).
			Str("project_id", projectID).
			Int("port", d.ExternalPort).
			Str("worker_id", d.WorkerID).
			Msg("Deployment proxy error")
		w.WriteHeader(http.StatusBadGateway)
	}

	if d.WorkerID != "" && d.WorkerID != "local" {
		session, err := tunnel.GlobalManager.GetSession(d.WorkerID)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "remote worker tunnel is offline"})
			return
		}

		// Inform the remote worker's yamux acceptor which local port to forward to
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Header.Set("X-Pushpaka-Target-Port", fmt.Sprintf("%d", d.ExternalPort))
		}

		proxy.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return session.Open()
			},
		}
	}

	// For domain-based proxying, we don't strip prefix,
	// unless we want to support something like mydomain.com/prefix/
	// For now assume the domain points to the root of the app.
	c.Request.Header.Set("X-Forwarded-Host", c.Request.Host)
	if c.Request.TLS != nil {
		c.Request.Header.Set("X-Forwarded-Proto", "https")
	} else {
		c.Request.Header.Set("X-Forwarded-Proto", "http")
	}

	c.Request.Host = target.Host
	proxy.ServeHTTP(c.Writer, c.Request)
}

// newSPAHandler returns an http.Handler that serves a Next.js static export.
//
// Next.js App Router makes two kinds of requests:
//   - Full page / hard navigation  -> GET /dashboard/       (no _rsc param) -> serve .html
//   - RSC navigation fetch         -> GET /dashboard/?_rsc= -> serve .txt with text/x-component
//
// Resolution order for both kinds:
//  1. Exact non-directory file (JS, CSS, images, fonts, explicit .html ...)
//  2. RSC request -> path/index.txt  (RSC payload pre-built by Next.js static export)
//  3. HTML request -> path/index.html (full pre-rendered page)
//  4. Dynamic-segment placeholder: substitute unknown UUID segments with "_"
//  5. SPA fallback -> root index.html
func newSPAHandler(fsys fs.FS) http.Handler {
	fsh := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := r.URL.Path
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
		}
		upath = path.Clean(upath)
		name := strings.TrimPrefix(upath, "/")
		if name == "" {
			name = "index.html"
		}

		// 1. Exact non-directory file (CSS, JS, images, explicit .html, etc.)
		if fi, err := fs.Stat(fsys, name); err == nil && !fi.IsDir() {
			fsh.ServeHTTP(w, r)
			return
		}

		// 2 & 3. Handle RSC navigation vs full-page requests differently.
		//
		// Next.js App Router adds ?_rsc=<ts> to its navigation fetch and sets
		// Accept: text/x-component.  The pre-built static export stores the RSC
		// payload alongside the HTML as  <route>/index.txt.  Returning HTML for
		// this fetch causes the RSC parser to throw "Uncaught (in promise) Event"
		// and navigation silently fails.
		baseName := strings.TrimRight(name, "/")
		isRSC := r.URL.Query().Has("_rsc") ||
			strings.Contains(r.Header.Get("Accept"), "text/x-component")

		if isRSC {
			// Serve RSC payload with the correct content-type.
			w.Header().Set("Content-Type", "text/x-component; charset=utf-8")

			// The path may contain an explicit /index.txt suffix (Next.js 16 Turbopack
			// requests /dashboard/projects/<uuid>/index.txt?_rsc=...).  Strip it so we
			// work with the bare directory path in all steps below.
			dirBase := strings.TrimRight(name, "/")
			dirBase = strings.TrimSuffix(dirBase, "/index.txt")

			// 2a. Exact index.txt for this directory.
			if serveFile(w, r, fsys, dirBase+"/index.txt") {
				return
			}
			// 2b. Dynamic-segment fallback: /projects/<uuid>/ -> projects/_/index.txt
			if segs := splitSegments(dirBase); len(segs) > 0 {
				if resolved := resolveFile(fsys, segs, "index.txt"); resolved != "" {
					serveFile(w, r, fsys, resolved)
					return
				}
			}

			// 2c. __next.X.Y.Z.txt segment payload files.
			//
			// Next.js Turbopack stores segment RSC payloads in a directory tree:
			//   Request filename:  __next.dashboard.projects.$d$id.deployments.__PAGE__.txt
			//   Converted path:    __next.dashboard/projects/$d$id/deployments/__PAGE__.txt
			//   Actual file (FS):  dashboard/projects/_/deployments/__next.dashboard/projects/$d$id/deployments/__PAGE__.txt
			//
			// Two transforms are needed:
			//   1. Dot-to-slash on the filename  ("X.Y.Z" -> "X/Y/Z")
			//   2. UUID-to-placeholder on parent dir segments
			base := path.Base(name)
			if strings.HasPrefix(base, "__next.") && strings.HasSuffix(base, ".txt") {
				dir := path.Dir(name)
				dirSegs := splitSegments(dir)
				inner := strings.TrimPrefix(strings.TrimSuffix(base, ".txt"), "__next.")
				if dot := strings.Index(inner, "."); dot != -1 {
					firstSeg := inner[:dot]
					rest := strings.ReplaceAll(inner[dot+1:], ".", "/")
					convertedFile := "__next." + firstSeg + "/" + rest + ".txt"
					// 2c-i: exact (no dynamic segments in parent path)
					dirPrefix := ""
					if dir != "." {
						dirPrefix = dir + "/"
					}
					if serveFile(w, r, fsys, dirPrefix+convertedFile) {
						return
					}
					// 2c-ii: UUID placeholder substitution in parent dir segments
					if len(dirSegs) > 0 {
						if resolved := resolveFile(fsys, dirSegs, convertedFile); resolved != "" {
							serveFile(w, r, fsys, resolved)
							return
						}
					}
				}
			}

			// 2d. Other RSC files (e.g. __next._tree.txt, __next._head.txt) nested
			// inside a dynamic path that contains UUID segments.
			// e.g.: dashboard/projects/<uuid>/deployments/__next._tree.txt
			//   ->   dashboard/projects/_/deployments/__next._tree.txt
			{
				fileName := path.Base(dirBase)
				parentSegs := splitSegments(path.Dir(dirBase))
				if len(parentSegs) > 0 {
					if resolved := resolveFile(fsys, parentSegs, fileName); resolved != "" {
						serveFile(w, r, fsys, resolved)
						return
					}
				}
			}

			http.NotFound(w, r)
			return
		}

		// 3. Full-page (hard navigation): serve the pre-rendered HTML.
		// Directory-style URL: /dashboard/ -> dashboard/index.html
		if serveFile(w, r, fsys, baseName+"/index.html") {
			return
		}

		// 4. File-style URL: /dashboard -> dashboard.html (trailingSlash: false)
		if serveFile(w, r, fsys, baseName+".html") {
			return
		}

		// 5. Dynamic-segment resolution: /projects/<uuid>/ -> projects/_/index.html
		if segs := splitSegments(baseName); len(segs) > 0 {
			if resolved := resolveFile(fsys, segs, "index.html"); resolved != "" {
				serveFile(w, r, fsys, resolved)
				return
			}
		}

		// 6. SPA fallback -> root index.html
		serveFile(w, r, fsys, "index.html")
	})
}

// serveFile serves a specific file from the FS using http.ServeContent,
// bypassing FileServer's index.html->directory redirect behaviour.
// Returns true if the file was found and served.
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		return false
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f.(io.ReadSeeker))
	return true
}

// splitSegments splits a clean URL path (no leading slash) into non-empty segments.
func splitSegments(name string) []string {
	var out []string
	for _, s := range strings.Split(name, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// resolveFile finds the best static file for URL segments by substituting
// unknown path segments with the "_" placeholder produced by generateStaticParams
// in the Next.js static export.  file is "index.html" or "index.txt".
//
// Example: (["dashboard","projects","abc-123","env"], "index.html")
//
//	-> "dashboard/projects/_/env/index.html"
func resolveFile(fsys fs.FS, segments []string, file string) string {
	var try func(idx int, prefix string) string
	try = func(idx int, prefix string) string {
		if idx == len(segments) {
			candidate := prefix + file
			if _, err := fs.Stat(fsys, candidate); err == nil {
				return candidate
			}
			return ""
		}
		seg := segments[idx]
		if result := try(idx+1, prefix+seg+"/"); result != "" {
			return result
		}
		if seg != "_" {
			if result := try(idx+1, prefix+"_/"); result != "" {
				return result
			}
		}
		return ""
	}
	return try(0, "")
}

// newDeploymentProxyHandler returns a gin.HandlerFunc that reverse-proxies
// requests to the running container for the given :projectID.
// The container must have a non-zero ExternalPort set by the deployment worker.
func newDeploymentProxyHandler(repo *repositories.DeploymentRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("projectID")
		// Strip the /app/:projectID prefix so the downstream app sees a clean path.
		proxyPath := c.Param("proxyPath")
		if proxyPath == "" {
			proxyPath = "/"
		}
		c.Request.URL.Path = proxyPath
		c.Request.URL.RawPath = proxyPath

		proxyToProject(c, projectID, repo)
	}
}
