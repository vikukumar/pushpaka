<div align="center">

<img src="branding/logo.svg" alt="Pushpaka Logo" width="320" />

# Pushpaka

### *Carry your code to the cloud effortlessly.*

[![Version](https://img.shields.io/badge/version-v1.0.0-6366f1?style=flat-square)](https://github.com/vikukumar/pushpaka)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Next.js](https://img.shields.io/badge/Next.js-16.2-black?style=flat-square&logo=next.js)](https://nextjs.org)
[![React](https://img.shields.io/badge/React-19.2-61DAFB?style=flat-square&logo=react)](https://react.dev)
[![Tailwind](https://img.shields.io/badge/Tailwind-4.2-38BDF8?style=flat-square&logo=tailwindcss)](https://tailwindcss.com)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker)](https://docker.com)
[![License](https://img.shields.io/badge/license-MIT-22c55e?style=flat-square)](LICENSE)

**Pushpaka** is a production-grade self-hosted cloud deployment platform — deploy applications from any Git repository (public or private) with automated container builds, real-time logs, custom domains, dark/light theming, and Traefik-powered routing. Featuring a **Distributed Worker Engine** with secure **Yamux Tunneling** and enterprise **Multi-DB ORM** support.

🌐 **[Visit Website](https://vikukumar.github.io/Pushpaka/)** - Modern, beautiful documentation site with installation guides, feature showcase, and release tracker.

[Quick Start](#quick-start) · [Dev Mode](#dev-mode-single-binary) · [Features](#features) · [Architecture](#architecture) · [API](#api) · [Configuration](#configuration) · [Website](#website) · [Roadmap](#roadmap)

</div>

---

## What is Pushpaka?

Pushpaka brings the Vercel/Render/Railway experience to your own infrastructure. It orchestrates the full deployment pipeline:

1. **Connect** a Git repository (public or private with PAT)
2. **Trigger** a deployment (manually or via API)
3. **Build** — auto-detects framework and generates a Dockerfile, or uses your own
4. **Deploy** — distributed execution across **Integrated**, **Vaahan (Serverless)**, or **Hybrid** worker nodes
5. **Tunnel** — secure reverse tunneling (Yamux) serves apps from remote workers without open ports
6. **Route** — traffic via Traefik + optional custom domains + auto-SSL
7. **Monitor** — real-time WebSocket log streaming, live system status, and worker telemetry

---

## Architecture

```
                         Internet
                            |  HTTPS/WSS
               +------------v-------------+
               |      Traefik v3          |  Reverse Proxy / TLS
               |   Port 80 / 443          |  Let's Encrypt Auto-SSL
               +------+----------+--------+
                      |          |
           +----------v---+  +---v-----------+
           |  Dashboard   |  |  Backend API  |
           |  (Next.js 16)|  |  (Go 1.25/   |
           |   :3000      |  |   Gin 1.12)  |
           +--------------+  +----+---------+
                                   |
                +------------------+-------------------+
                |                  |                   |
      +---------v------+  +--------v-------+  +--------v-------+
      |  Multi-DB GORM |  |   Redis 8      |  | Worker Manager |
      | (PG/MY/MS/SQLI)|  |  (Job queue)   |  |   (Port 8081)  |
      +----------------+  +----------------+  +--------+-------+
                                                       |
               +---------------------------------------+---------------------------------------+
               |                                       |                                       |
     +---------v----------+              +-------------v-----------+             +-------------v-----------+
     | Integrated Worker  |              | Vaahan (Serverless)     |             | Hybrid Worker Node      |
     | (In-process Gor)   |              | (SQLite + Tunnel)       |             | (GORM DB + Tunnel)      |
     +--------------------+              +-------------------------+             +-------------------------+
               |                                       |                                       |
               +---------------------------------------+---------------------------------------+
                                                       |
                                         +-------------v-----------+
                                         |     Docker Engine       |
                                         |  git -> build -> run    |
                                         |  or direct deploy       |
                                         +-------------------------+

   Tunneling: Secure reverse multiplexing (Yamux) over WebSocket ensures remote workers
   serve traffic through the main gateway without requiring public IP exposure.
```

---

## Features

### Platform
- 🚀 **One-click Git deployments** — public repos and private repos with Personal Access Token
- 🔒 **Private repository support** — PAT stored securely, never returned via API, redacted from logs
- 🐳 **Automatic Dockerization** — detects Next.js, React, Vue, Go, Python, and more; generates optimized Dockerfile
- 🚫 **Docker-free direct deploy** — falls back to in-place process deployment when Docker is unavailable
- ♻️ **Rollback support** — redeploy any previous deployment instantly
- 🔀 **Multi-project** — unlimited projects per user
- 👥 **Multi-user** — team-ready with role-based access (admin/user)
- 🗑️ **Project management** — create, update settings, and delete projects from the dashboard
- 🎯 **Promote to Default** — Mark any successful deployment as "Default" to provide a stable, constant endpoint for users and custom domains.
- 📈 **Running Counts** — Instant visibility into the number of active deployments per project directly on the dashboard.

### Infrastructure
- 🛰️ **Distributed Worker Engine** — scale execution across remote `Vaahan` or `Hybrid` nodes
- 🚇 **Secure Reverse Tunneling** — serve apps from remote workers via Yamux-multiplexed WebSockets
- 🗄️ **Multi-DB ORM** — native support for PostgreSQL, MySQL, SQL Server, MSSQL, and SQLite via GORM
- 🔄 **Auto-Migrations** — schema synchronization on startup, no manual migration files required
- 📊 **Prometheus metrics** — export to Grafana at `/api/v1/metrics`
- ❤️ **Health checks** — `/health`, `/ready`, and live `/system` status endpoint
- 🔧 **Worker Management** — dedicated dashboard to monitor node health, resources, and PAT rotation

### Developer Experience
- 📡 **Real-time logs** — WebSocket streaming during builds with level/stream filtering
- 🌍 **Custom domains** — map any domain to any project
- 🔑 **Environment variables** — secure write-only storage, keys visible, values never returned
- 🌓 **Premium Enterprise UI** — Clean, responsive design with glassmorphism, staggered animations, and perfected dark/light modes.
- 📦 **Single binary dev mode** — `pushpaka -dev` starts everything with SQLite + in-process queue
- 🧰 **Package manager auto-detect** — build steps auto-detect `npm` / `yarn` / `pnpm` / `bun`, with PATH fallback
- 🤖 **AI Assistant** — Integrated AI for intelligent log analysis, deployment troubleshooting, and live support.
- 🔄 **Live Updates** — Real-time dashboard polling for instantaneous status feedback.

### Security
- 🔒 **JWT v5 + API key authentication**
- 🔑 **bcrypt password hashing** (cost 10)
- 🛡️ **Secure headers** (HSTS, CSP, X-Frame-Options, X-Content-Type-Options)
- 🚦 **Rate limiting** on all endpoints
- 🌐 **Configurable CORS**
- 🙈 **Git token redaction** — PAT never appears in deployment logs or API responses

---

## Quick Start

### Docker Compose (Recommended for Production)

```bash
# Clone
git clone https://github.com/vikukumar/pushpaka
cd Pushpaka

# Configure
cp .env.example .env
# Edit .env: set DOMAIN, JWT_SECRET, POSTGRES_PASSWORD, REDIS_PASSWORD, ACME_EMAIL

# Launch
docker compose up -d --build

# Open dashboard
open https://app.YOUR_DOMAIN
```

**Minimum `.env` for production:**
```env
DOMAIN=pushpaka.example.com
JWT_SECRET=<openssl rand -hex 32>
POSTGRES_PASSWORD=<strong-password>
REDIS_PASSWORD=<strong-password>
ACME_EMAIL=you@example.com
```

---

## Dev Mode (Single Binary)

The fastest way to run Pushpaka locally — **no Docker, Redis, or PostgreSQL required**:

```bash
# Build
cd cmd/pushpaka
go build -o pushpaka .

# Run (SQLite + embedded worker + in-process queue)
./pushpaka -dev

# Frontend (separate terminal)
cd frontend
pnpm install
pnpm dev
# Open http://localhost:3000
```

Dev mode automatically:
- Uses SQLite (`pushpaka-dev.db`) instead of PostgreSQL
- Skips Redis — uses a fast in-process channel queue
- Embeds the build worker in the same process
- Enables pretty console logging
- Sets `JWT_SECRET=dev-secret-change-in-production`

---

## Worker Scaling

Pushpaka supports three worker modes for enterprise scalability:

| Mode | Name | Persistence | Connectivity |
|------|------|-------------|--------------|
| **Integrated** | Default | Shared with API | In-process |
| **Vaahan** | Serverless | Embedded SQLite | WebSocket + Tunnel |
| **Hybrid** | Remote | External GORM DB | WebSocket + Tunnel |

### Running a Remote Worker (Vaahan)

Remote workers connect back to the Management API using a Zone PAT.

```bash
# On the remote node
cd cmd/worker
pushpaka-worker \
  --mode vaahan \
  --server ws://your-api-domain.com \
  --zone-pat YOUR_ZONE_PAT
```

Workers report their telemetry (CPU, RAM, Architecture) and available tools (Docker, Go, Node) back to the dashboard automatically.

---

## Project Structure

```
pushpaka/
├── cmd/pushpaka/             # Combined binary entry point (-dev flag)
├── backend/                  # Go API server (module: Pushpaka)
│   └── internal/
│       ├── handlers/         # HTTP handlers (auth, projects, deployments, logs, domains, env, health)
│       ├── services/         # Business logic
│       ├── repositories/     # Database layer (SQLite + PostgreSQL)
│       ├── models/           # Data models (Project, Deployment, User, ...)
│       ├── middleware/        # JWT, logging, secure headers, recovery
│       ├── config/           # Configuration loader
│       ├── database/         # DB init + SQLite schema
│       └── router/           # Route definitions
├── worker/                   # Build & deploy worker (module: Pushpaka-worker)
│   └── internal/worker/
│       └── build_worker.go   # Pipeline: clone -> detect PM -> build -> run/deploy
├── frontend/                 # Next.js 16 / React 19 dashboard
│   ├── app/                  # App Router pages
│   │   ├── dashboard/        # Main shell, projects, deployments, settings
│   │   └── login/            # Auth page
│   ├── components/           # UI components (layout, dashboard cards, log viewer)
│   ├── lib/                  # theme.tsx, api.ts, utils.ts
│   └── types/                # TypeScript interfaces
├── queue/                    # In-process job queue (shared by cmd + backend)
├── migrations/               # PostgreSQL SQL migrations (001-006)
├── infrastructure/           # Traefik dynamic config
├── branding/                 # Logo, favicon, OG image
├── scripts/                  # Seed data
├── docs/                     # Full documentation
├── Dockerfile                # Multi-stage: Go workspace build -> alpine runtime
├── docker-compose.yml        # Production stack
├── docker-compose.dev.yml    # Dev overrides (ports exposed, debug logging)
```

---

## Tech Stack

| Layer | Technology | Version |
|-------|-----------|---------|
| Backend language | Go | 1.26 |
| HTTP framework | Gin | 1.12.0 |
| WebSocket | gorilla/websocket | 1.5.3 |
| Multiplexer | hashicorp/yamux | 0.1.2 |
| ORM | GORM | 1.31.x |
| Database (SQL) | PG / MySQL / MSSQL / SQLite | — |
| Database (NoSQL) | MongoDB (v2 driver) | 2.5.0 |
| Queue (prod) | Redis (go-redis v9) | 9.18.0 |
| Queue (dev) | In-process channel | — |
| Metrics | Prometheus client_golang | 1.23.2 |
| Logging | zerolog | 1.34.0 |
| Frontend framework | Next.js | 16.2.3 |
| UI library | React | 19.2.4 |
| Styling | Tailwind CSS | 4.2.1 |
| Data fetching | TanStack Query | 5.90.21 |
| HTTP client | Axios | 1.13.6 |
| State management | Zustand | 5.0.11 |
| Icons | Lucide React | 0.577.0 |
| Date utilities | date-fns | 4.1.0 |
| TypeScript | TypeScript | 5.9.3 |
| Reverse proxy | Traefik | v3.x |
| Container runtime | Docker | 24+ |

---

## API

Full documentation: [docs/api.md](docs/api.md)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/auth/register` | Register user |
| POST | `/api/v1/auth/login` | Login, returns JWT |
| GET | `/api/v1/projects` | List projects |
| POST | `/api/v1/projects` | Create project (supports `is_private`, `git_token`) |
| PUT | `/api/v1/projects/:id` | Update project settings |
| DELETE | `/api/v1/projects/:id` | Delete project |
| POST | `/api/v1/deployments` | Trigger deployment |
| GET | `/api/v1/deployments/:id` | Get deployment |
| POST | `/api/v1/deployments/:id/rollback` | Rollback to previous |
| POST | `/api/v1/deployments/:id/promote` | Promote to Default (Constant Endpoint) |
| GET | `/api/v1/logs/:id` | Get deployment logs |
| POST | `/api/v1/deployments/:id/analyze` | AI Log Analysis & Fix Suggestions |
| WS | `/api/v1/logs/:id/stream` | Stream logs live (WebSocket + JWT) |
| POST | `/api/v1/domains` | Add custom domain |
| POST | `/api/v1/env` | Set env variable |
| GET | `/api/v1/metrics` | Prometheus metrics |
| GET | `/api/v1/health` | Health check (DB + Redis) |
| GET | `/api/v1/ready` | Readiness probe |
| GET | `/api/v1/system` | Live system info (Docker, Git, workers, runtime) |

---

## Documentation

| Document | Description |
|----------|-------------|
| [docs/architecture.md](docs/architecture.md) | System architecture and design decisions |
| [docs/api.md](docs/api.md) | Complete API reference |
| [docs/local-dev.md](docs/local-dev.md) | Local development setup |
| [docs/deployment.md](docs/deployment.md) | Production deployment guide |
| [docs/platform-overview.md](docs/platform-overview.md) | Platform concepts and states |

---

## Configuration

Key environment variables (see [`.env.example`](.env.example)):

| Variable | Default | Description |
|----------|---------|-------------|
| `DOMAIN` | `localhost` | Base domain for Traefik routing |
| `JWT_SECRET` | — | **Required**: JWT signing secret (min 32 chars) |
| `POSTGRES_PASSWORD` | — | **Required** (prod): DB password |
| `REDIS_PASSWORD` | — | **Required** (prod): Redis password |
| `BUILD_WORKERS` | `3` | Concurrent build worker goroutines |
| `BUILD_CLONE_DIR` | `/tmp/pushpaka-builds` | Temp dir for git clones |
| `BUILD_DEPLOY_DIR` | `/deploy/pushpaka` | Persistent dir for direct (no-Docker) deploys |
| `ACME_EMAIL` | — | Let's Encrypt contact email |
| `APP_ENV` | `production` | `development` enables pretty logging |
| `DATABASE_DRIVER` | `postgres` | `sqlite` for dev/single-node |
| `PUSHPAKA_COMPONENT` | `all` | `api` / `worker` / `all` — split or combined |

---

## Roadmap — v1.0.0 (Improvements)

- [x] GitHub / GitLab OAuth (one-click repo connect)
- [x] Webhook auto-deploy on `git push`
- [ ] Pull Request preview deployments
- [ ] Blue-green zero-downtime deployments
- [x] Distributed Worker Engine (Vaahan/Hybrid)
- [x] CPU/memory resource limits reporting
- [x] Slack / Discord / email notifications
- [x] Web terminal (exec into containers)
- [x] Audit log viewer in dashboard
- [ ] Build caching for faster deployments

---

## License

MIT © 2026 Pushpaka Contributors

---

<div align="center">
  <sub>Built with love — Pushpaka v1.0.0 · Go 1.26 · Next.js 16.2 · React 19</sub>
</div>
