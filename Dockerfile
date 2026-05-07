# Stage 1: Builder (Go + Node.js + Pnpm)
FROM golang:1.26-alpine AS builder

# Build arguments
ARG VERSION=v1.0.0
ARG BUILD_DATE
ARG VCS_REF

# Labels for image metadata
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.revision="${VCS_REF}"

# Install dependencies for building both Go and Node.js components
RUN apk add --no-cache \
    git \
    ca-certificates \
    make \
    nodejs \
    npm \
    curl \
    docker-cli \
    docker-cli-buildx

# Install pnpm for frontend builds
RUN npm install -g pnpm@latest

WORKDIR /app

# Copy workspace descriptor first for better layer caching
COPY go.work go.work.sum* ./

# Copy module manifests
COPY backend/go.mod backend/go.sum ./backend/
COPY worker/go.mod worker/go.sum ./worker/
COPY cmd/pushpaka/go.mod cmd/pushpaka/go.sum* ./cmd/pushpaka/

# Download all workspace dependencies
RUN go work sync

# Copy frontend dependency manifests
COPY frontend/package.json frontend/pnpm-lock.yaml ./frontend/
RUN cd frontend && echo "only-allow-trusted-dependencies=false" > .npmrc && pnpm install --no-frozen-lockfile

# Copy all source
COPY . .

# Build the entire project using Makefile
# This will run front-build (Next.js) and then go build
RUN make build VERSION=${VERSION}

# Stage 2: Runtime
FROM alpine:3.21

# Runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    curl \
    git \
    docker-cli \
    docker-cli-buildx \
    nodejs \
    npm \
    go \
    python3 \
    py3-pip \
    make \
    g++

# Install pnpm globally for workers
RUN npm install -g pnpm@latest

WORKDIR /app

# Copy the unified binary from builder
COPY --from=builder /app/pushpaka /usr/local/bin/pushpaka

# Runtime defaults
ENV PUSHPAKA_COMPONENT=all
ENV BUILD_CLONE_DIR=/tmp/pushpaka-builds
ENV BUILD_DEPLOY_DIR=/deploy/pushpaka

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:8080/api/v1/ready || exit 1

ENTRYPOINT ["pushpaka"]

