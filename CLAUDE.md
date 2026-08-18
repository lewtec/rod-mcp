# rod-mcp

See [docs/AGENTS.md](docs/AGENTS.md) for project guidelines, architecture, and code patterns.

## Build & Test

```bash
go build -o rod-mcp ./cmd/rod-mcp   # Build
go test ./...                 # Unit tests
cd e2e && go test -v -timeout 120s  # E2E tests
```
