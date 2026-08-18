# Build stage
FROM golang:1.24-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w \
    -X main.Version=${VERSION} \
    -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /rod-mcp ./cmd/rod-mcp

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium \
    ca-certificates \
    fonts-liberation \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 1000 rodmcp

COPY --from=builder /rod-mcp /usr/local/bin/rod-mcp

WORKDIR /app

# Default config: headless + no-sandbox for container environment
RUN printf 'mode: text\nheadless: true\nnoSandbox: true\nbrowserTempDir: /tmp/rod/browser\n' > rod-mcp.yaml

USER rodmcp

ENTRYPOINT ["rod-mcp", "--no-banner"]
