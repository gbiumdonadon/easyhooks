# =============================================================================
# EasyHooks — Go multi-stage Dockerfile
# Stage 1: Build API and Worker binaries
# Stage 2: Minimal runtime image (distroless)
# =============================================================================

# ---- Builder ----------------------------------------------------------------
FROM golang:1.24-alpine AS builder

WORKDIR /build

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOTOOLCHAIN=auto

# Download dependencies first (cached layer)
COPY go-api/go.mod go-api/go.sum ./
RUN go mod download

# Copy source
COPY go-api/ ./

# Build both binaries
RUN go build -ldflags="-s -w" -o /app/api ./cmd/api
RUN go build -ldflags="-s -w" -o /app/worker ./cmd/worker

# ---- Runtime ----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12

WORKDIR /app

# Binaries
COPY --from=builder /app/api    /app/api
COPY --from=builder /app/worker /app/worker

# Migrations (applied at API startup via golang-migrate)
COPY migrations/ /migrations/

EXPOSE 8000

CMD ["/app/api"]
