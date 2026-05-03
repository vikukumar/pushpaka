.PHONY: dev dev-all build build-api build-worker build-all front-build front-dev clean help lint lint-go lint-front

# Detect OS for binary extension
ifeq ($(OS),Windows_NT)
  EXT := .exe
else
  EXT :=
endif

VERSION ?= v1.0.0
LDFLAGS := -ldflags="-w -s -X main.version=$(VERSION)"

# Development

dev:
	go run ./cmd/pushpaka -dev

dev-all:
	DATABASE_DRIVER=sqlite DATABASE_URL=pushpaka-dev.db APP_ENV=development go run ./cmd/pushpaka

front-dev:
	cd frontend && pnpm dev

# Production

front-build:
	node scripts/patch-layout.js remove
	cd frontend && STATIC_EXPORT=1 pnpm build || (node ../scripts/patch-layout.js restore && exit 1)
	node scripts/patch-layout.js restore
	node scripts/cpfe.js

build: front-build
	go build $(LDFLAGS) -o pushpaka$(EXT) ./cmd/pushpaka
	@echo Built: pushpaka$(EXT)

build-api: front-build
	go build -C backend $(LDFLAGS) -o ../pushpaka-api$(EXT) ./cmd/server
	@echo Built: pushpaka-api$(EXT)

build-worker:
	go build -C worker $(LDFLAGS) -o ../pushpaka-worker$(EXT) .
	@echo Built: pushpaka-worker$(EXT)

build-all: build build-worker
	@echo All binaries built.

# Linting

lint: lint-go lint-front

lint-go:
	@echo "Linting Go code..."
	go vet ./backend/... ./worker/... ./cmd/pushpaka/...

lint-front:
	@echo "Linting frontend..."
	cd frontend && pnpm lint --quiet

clean:
	rm -f pushpaka pushpaka.exe pushpaka-api pushpaka-api.exe pushpaka-worker pushpaka-worker.exe pushpaka-dev.db pushpaka-dev.db-shm pushpaka-dev.db-wal

help:
	@echo "
	@echo "  Dev commands:"
	@echo "    make dev           Run API, SQLite (no external deps)"
	@echo "    make dev-all       Run API + worker (SQLite + Redis + Docker)"
	@echo "    make front-dev     Next.js dev server on :3000"
	@echo "
	@echo "  Build commands:"
	@echo "    make build         All-in-one binary  ->  ./pushpaka"
	@echo "    make build-api     API-only binary    ->  ./pushpaka-api"
	@echo "    make build-worker  Worker-only binary ->  ./pushpaka-worker"
	@echo "    make build-all     All three binaries"
	@echo "