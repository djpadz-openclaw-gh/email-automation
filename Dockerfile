# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build all four binaries
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/imap-listener ./cmd/imap-listener
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/rules-engine ./cmd/rules-engine
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/scheduler ./cmd/scheduler

# Also build the legacy monolith for backward compatibility
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/email-automation ./cmd/server

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

RUN adduser -D -u 1000 appuser

WORKDIR /app

# Copy all binaries
COPY --from=builder /bin/api .
COPY --from=builder /bin/imap-listener .
COPY --from=builder /bin/rules-engine .
COPY --from=builder /bin/scheduler .
COPY --from=builder /bin/email-automation .

COPY migrations/ ./migrations/
COPY rules/ ./rules/

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

# Default to the API server; override with command in k8s deployments
ENTRYPOINT ["./api"]
