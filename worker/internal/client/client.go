package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/vikukumar/pushpaka/pkg/models"
	"github.com/vikukumar/pushpaka/pkg/tunnel"
	"github.com/vikukumar/pushpaka/worker/internal/config"
)

type TaskPusher interface {
	Push(role string, payload []byte) error
}

type WorkerClient struct {
	serverURL string
	zonePAT   string
	db        *gorm.DB
	cfg       *config.Config
	pusher    TaskPusher
	roles     []string
}

func NewWorkerClient(serverURL, zonePAT string, db *gorm.DB, cfg *config.Config, pusher TaskPusher, roles []string) *WorkerClient {
	return &WorkerClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		zonePAT:   zonePAT,
		db:        db,
		cfg:       cfg,
		pusher:    pusher,
		roles:     roles,
	}
}

func (c *WorkerClient) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.connectAndServe(ctx)
		if err != nil {
			log.Error().Err(err).Msg("Worker connection dropped, retrying in 5s...")
		} else {
			log.Info().Msg("Worker connection closed cleanly, reconnecting...")
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *WorkerClient) connectAndServe(ctx context.Context) error {
	// 1. Register with the Main API
	hostname, _ := os.Hostname()
	if len(c.roles) > 0 {
		hostname = fmt.Sprintf("%s-%s", hostname, strings.Join(c.roles, "-"))
	}
	reqBody := models.RegisterWorkerRequest{
		ZonePAT:      c.zonePAT,
		Type:         "vaahan", // Always register as remote node type
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		Name:         hostname,
		Roles:        c.roles,
	}
	bodyData, _ := json.Marshal(reqBody)

	apiURL := c.serverURL
	if strings.HasPrefix(apiURL, "ws://") {
		apiURL = "http://" + strings.TrimPrefix(apiURL, "ws://")
	} else if strings.HasPrefix(apiURL, "wss://") {
		apiURL = "https://" + strings.TrimPrefix(apiURL, "wss://")
	}

	regURL := fmt.Sprintf("%s/api/v1/worker/register", apiURL)
	resp, err := http.Post(regURL, "application/json", bytes.NewReader(bodyData))
	if err != nil {
		return fmt.Errorf("failed to register: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration rejected (status %d): %s", resp.StatusCode, string(b))
	}

	var regResp models.WorkerAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return fmt.Errorf("failed to decode register response: %w", err)
	}

	log.Info().Str("worker_id", regResp.WorkerID).Msg("Successfully registered worker to zone")

	// 2. Connect WebSocket
	wsURL := "ws://" + strings.TrimPrefix(c.serverURL, "http://")
	if strings.HasPrefix(c.serverURL, "https://") {
		wsURL = "wss://" + strings.TrimPrefix(c.serverURL, "https://")
	}
	if strings.HasPrefix(c.serverURL, "ws://") || strings.HasPrefix(c.serverURL, "wss://") {
		wsURL = c.serverURL
	}
	wsConnectURL := fmt.Sprintf("%s/api/v1/worker/ws?token=%s", wsURL, regResp.AuthToken)

	wsConn, _, err := websocket.DefaultDialer.DialContext(ctx, wsConnectURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	defer wsConn.Close()

	log.Info().Msg("WebSocket connected, establishing Yamux tunnel")

	// 3. Establish Yamux Client Session
	// The main API runs yamux.Server, so we run yamux.Client over the tunnel wrappers.
	netConn := tunnel.NewWSConn(wsConn)
	yamuxCfg := yamux.DefaultConfig()
	yamuxCfg.EnableKeepAlive = true

	session, err := yamux.Client(netConn, yamuxCfg)
	if err != nil {
		return fmt.Errorf("yamux client init failed: %w", err)
	}
	defer session.Close()

	log.Info().Msg("Yamux tunnel established. Listening for API streams...")

	// 4. Start Embedded HTTP Server on the Yamux session
	mux := http.NewServeMux()

	// Handle Generic Tasks (Sync, Build, Test, AI, etc.)
	mux.HandleFunc("/internal/task", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var task struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := c.pusher.Push(task.Type, []byte(task.ID)); err != nil {
			log.Error().Err(err).Str("task_id", task.ID).Str("type", task.Type).Msg("failed to push task to local queue")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"worker saturated"}`))
			return
		}

		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"accepted"}`))
	})

	// Handle API Deployment Triggers (Backward compatibility - assume "build" role)
	mux.HandleFunc("/internal/deploy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := c.pusher.Push("build", body); err != nil {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	// Handle Reverse Proxied Traffic intended for local deployed applications!
	mux.HandleFunc("/", c.reverseProxyHandler)

	srv := &http.Server{Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		// Serve accepts a net.Listener (and yamux.Session implements net.Listener!)
		if err := srv.Serve(session); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		srv.Shutdown(context.Background())
		return ctx.Err()
	case err := <-errCh:
		return err
	case <-session.CloseChan():
		// Reconnect if session drops
		srv.Shutdown(context.Background())
		return fmt.Errorf("yamux session closed by remote")
	}
}

// reverseProxyHandler dynamically looks up the target port based on Hostname
// and reverse proxies the Yamux HTTP request to the local raw process or Traefik.
func (c *WorkerClient) reverseProxyHandler(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	// Find the deployment bound to this host
	var domain models.Domain
	if err := c.db.Where("name = ?", host).First(&domain).Error; err != nil {
		// Try routing by raw App Name if host is missing, or send 404
		http.Error(w, "Deployment not found for host mapping", http.StatusNotFound)
		return
	}

	var deployment models.Deployment
	if err := c.db.Where("project_id = ? AND status = ?", domain.ProjectID, models.DeploymentRunning).Order("created_at desc").First(&deployment).Error; err != nil {
		http.Error(w, "No active deployment running", http.StatusBadGateway)
		return
	}

	targetURL := fmt.Sprintf("http://localhost:%d", deployment.ExternalPort)
	if deployment.URL != "" {
		// If URL is populated, it usually means Docker/Traefik is routing it directly via localhost/p/<ID>
		targetURL = "http://localhost/" + strings.TrimPrefix(deployment.URL, "http://localhost/")
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "Internal routing error", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	// Modify the request to match the target
	r.URL.Host = u.Host
	r.URL.Scheme = u.Scheme
	r.Header.Set("X-Forwarded-Host", r.Header.Get("Host"))
	r.Host = u.Host

	proxy.ServeHTTP(w, r)
}
