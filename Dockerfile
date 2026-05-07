# ============================================================
# Stage 1: Builder  (Go + Node.js + pnpm)
# ============================================================
FROM golang:1.26-alpine AS builder

ARG VERSION=v1.0.0
ARG BUILD_DATE
ARG VCS_REF

LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}"

# Build-time system dependencies
RUN apk add --no-cache \
    git \
    ca-certificates \
    make \
    nodejs \
    npm \
    curl \
    docker-cli \
    docker-cli-buildx \
    g++ \
    gcc \
    python3

# Install pnpm (pinned major to keep builds deterministic)
RUN npm install -g pnpm@10

WORKDIR /app

# ── Go workspace: cache dependency downloads ──────────────────
COPY go.work go.work.sum* ./
COPY backend/go.mod backend/go.sum ./backend/
COPY worker/go.mod worker/go.sum ./worker/
COPY cmd/pushpaka/go.mod cmd/pushpaka/go.sum* ./cmd/pushpaka/
RUN go work sync

# ── Node: cache pnpm install separately from source ──────────
COPY frontend/package.json frontend/pnpm-lock.yaml ./frontend/

# pnpm v10+ blocks build scripts for native packages (sharp, unrs-resolver)
# by default (ERR_PNPM_IGNORED_BUILDS).  Writing a .npmrc with
# ignore-scripts=false before install is the correct fix; it allows the
# packages listed in "pnpm.onlyBuiltDependencies" (already in package.json)
# to run their post-install build scripts.
RUN echo "ignore-scripts=false" > /app/frontend/.npmrc && \
    cd frontend && pnpm install --no-frozen-lockfile

# ── Copy full source and build via Makefile ───────────────────
COPY . .

# front-build: patches layout → pnpm build (STATIC_EXPORT=1) → cpfe.js
# go build:    compiles the unified binary to ./pushpaka
RUN make build VERSION=${VERSION}

# ============================================================
# Stage 2: Runtime
# ============================================================
FROM alpine:3.21

# Runtime dependencies (node + pnpm are needed by the worker to
# run user project builds; go is NOT needed — the binary is self-contained)
RUN apk add --no-cache \
    ca-certificates \
    curl \
    git \
    docker-cli \
    docker-cli-buildx \
    nodejs \
    npm \
    python3 \
    py3-pip \
    make \
    g++ \
    gcc

RUN npm install -g pnpm@10

WORKDIR /app

# Copy the single unified binary from the builder stage
COPY --from=builder /app/pushpaka /usr/local/bin/pushpaka

# ── Runtime defaults ────────────────────────────────────────
ENV BUILD_CLONE_DIR=/tmp/pushpaka-builds
ENV BUILD_DEPLOY_DIR=/deploy/pushpaka

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:8080/api/v1/ready || exit 1

# ENTRYPOINT is the binary; CMD provides the default component.
# Override at runtime:
#   docker run ... pushpaka all      (API + embedded worker, default)
#   docker run ... pushpaka api      (API only, needs external Redis worker)
#   docker run ... pushpaka worker   (worker only, needs API + Redis)
# Or via env var (lower priority than CLI arg):
#   docker run -e PUSHPAKA_COMPONENT=worker ...
ENTRYPOINT ["pushpaka"]
CMD ["all"]
