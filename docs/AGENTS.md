# AI Agent Guidelines for rod-mcp

This document provides AI agents with project-specific context and guidelines for contributing to rod-mcp.

## Quick Reference

| Item | Value |
|------|-------|
| **Language** | Go 1.23+ |
| **Framework** | [mcp-go](https://github.com/mark3labs/mcp-go) v0.20+ |
| **Transport** | stdio (`server.ServeStdio`) |
| **Build** | `go build -o rod-mcp` |
| **Test** | `go test ./...` |
| **E2E** | `cd e2e && go test -v -timeout 120s` |
| **Lint** | `golangci-lint run` |

## Project Overview

**rod-mcp** is a Go-based MCP server that wraps [go-rod](https://github.com/go-rod/rod) for browser automation. It exposes browser control (navigation, clicking, filling forms, screenshots, etc.) as MCP tools consumed by AI agents.

### Key Differentiators
- Two modes: **Text** (ARIA snapshots) and **Vision** (screenshots)
- React-aware smart fill using multi-strategy JS (standard → clipboard → key-by-key)
- ARIA snapshot with ref-based element targeting
- Snapshot compaction for token efficiency
- Tab management for multi-page workflows
- Single Go binary, no runtime dependencies

## Architecture

### Directory Structure
```
rod-mcp/
├── main.go                 # Entry point, CLI flags
├── cmd.go                  # Command setup and configuration
├── server.go               # MCP server setup, tool registration by mode
├── runner.go               # Browser lifecycle management
├── version.go              # Version constant
├── tools/
│   ├── tools.go            # Tool set composition (Text vs Vision)
│   ├── common.go           # Shared tools: navigate, screenshot, evaluate, press
│   ├── snapshot.go          # Snapshot, click, selector tools (Text mode)
│   ├── fill.go             # rod_fill_form with smart fill
│   ├── input.go            # rod_type for character-by-character input
│   ├── browser.go          # Browser-level tools: evaluate, console, cookies
│   ├── tabs.go             # Tab management tools
│   ├── helpers.go          # resolveSnapshotElement, resolveBySelector, waitDOMStable
│   ├── vision.go           # Vision-mode tools
│   └── *_test.go           # Tests per file
├── types/
│   ├── context.go          # Rod browser context, page/tab lifecycle, keepalive
│   ├── config.go           # YAML configuration
│   ├── snapshot.go         # ARIA snapshot builder and compaction
│   ├── profile.go          # Browser profile constants
│   ├── opts.go             # ToolHandlerCallOpts
│   └── js/                 # Embedded JavaScript
│       ├── embed.go        # //go:embed directives
│       └── smart_fill.js   # React-aware fill strategies
├── e2e/                    # E2E tests (separate go.mod)
├── assets/                 # Static test fixtures
└── docs/
    └── AGENTS.md           # This file
```

## Key Patterns

### Tool Handler Pattern

All tools follow the same structure — a `mcp.Tool` definition paired with a handler factory:

```go
var MyTool = mcp.NewTool("rod_my_tool",
    mcp.WithDescription("Description of the tool."),
    mcp.WithString("param", mcp.Description("Parameter description"), mcp.Required()),
)

var MyToolHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
    handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        // 1. Get the controlled page
        page, err := rodCtx.ControlledPage()
        if err != nil {
            return toolErr("my tool", err)
        }

        // 2. Do work (the previous snapshot is still available for ref resolution)...

        // 3. Return result
        return mcp.NewToolResultText("result"), nil
    }
    // Execute invalidates the snapshot after the handler returns and, when
    // WithSnapshot: true, rebuilds and appends a fresh ARIA snapshot.
    // Do NOT call InvalidateSnapshot() inside handlers — Execute handles it.
    return rodCtx.Execute(handler, types.ToolHandlerCallOpts{WithSnapshot: true})
}
```

### MCP Result Types

- `mcp.NewToolResultText(text)` — text response
- `mcp.NewToolResultImage(text, base64Data, mimeType)` — image response (screenshots)

### Snapshot Lifecycle

The `Execute()` wrapper automatically invalidates the snapshot after every handler returns, then rebuilds and appends a fresh ARIA snapshot when `WithSnapshot: true`. Handlers must **not** call `InvalidateSnapshot()` themselves — the previous snapshot remains available during handler execution for ref-based element resolution.

### Element Resolution

Elements can be resolved by three strategies (priority order):
1. **ref** — numeric reference from ARIA snapshot (e.g., `ref: "42"`)
2. **selector** — CSS selector (e.g., `selector: "#email"`)
3. **name** — accessible name match from snapshot

Use `resolveSnapshotElement()` for tools that accept all three, or `resolveBySelector()` for CSS-only resolution.

### Smart Fill (React-Aware)

`smartFillElement()` in `tools/fill.go` handles React controlled inputs via embedded `smart_fill.js`:
1. Standard native setter + input/change events
2. ClipboardEvent paste (works for React)
3. Key-by-key simulation (last resort)

The JS is bound to the element as `this` via rod's `Eval()`. Always use `(function(value) { var el = this; ... })` pattern, never pass the element as a parameter.

### Error Handling

- Always wrap errors: `fmt.Errorf("context: %w", err)`
- Use `toolErr(context, err)` to return MCP error results
- Log before retrying: `log.Warnf("description: %s", err)`
- Never silently swallow errors

### Configuration

Config is loaded from YAML (`rod-mcp.yaml`):
```yaml
mode: text          # "text" or "vision"
headless: true
compact_snapshot: true
user_data_dir: ""   # Browser profile directory
```

`ROD_MCP_GUI=1` (also `true`/`yes`/`on`) forces a visible window after flags are parsed, so it overrides `--headless` from MCP launch args.

## Testing

### Unit Tests
```bash
go test ./...                          # All tests
go test ./tools/ -run TestFillForm -v  # Specific test
```

Tests use table-driven style with `t.Run()`. Tool definition tests verify schema properties exist and have correct types.

### E2E Tests
```bash
cd e2e && go test -v -timeout 120s
```

E2E tests launch a real browser against test fixtures in `assets/`. They have their own `go.mod` to avoid pulling browser dependencies into the main module.

## Code Style

- **Imports**: stdlib, blank line, external, blank line, internal (`github.com/aliwatters/rod-mcp/...`)
- **Naming**: standard Go conventions — `MixedCaps` exported, `mixedCaps` unexported
- **Packages**: lowercase single-word (`tools`, `types`, `js`)
- **Constants**: grouped by purpose with type aliases where appropriate
- **Comments**: godoc style on exported symbols

## Commit Messages

Use conventional commits:
```
fix: description of bug fix
feat: description of new feature
refactor: description of refactoring
test: description of test changes
docs: description of documentation changes
```

## Common Mistakes

1. **Passing element as JS function parameter** — rod's `Eval()` binds the element as `this`, not as a function argument
2. **Calling `InvalidateSnapshot()` inside handlers** — `Execute()` handles invalidation automatically after the handler returns. Calling it inside the handler destroys the snapshot before ref-based resolution can use it
3. **Using `WithSnapshot: false` on tools that need ref continuity** — subsequent ref-based operations will fail with "no snapshot available"
4. **Not calling `waitDOMStable(page)`** — after DOM mutations, wait for stability before reading state
5. **Importing `proto` without using CDP methods** — only import `github.com/go-rod/rod/lib/proto` when using Chrome DevTools Protocol directly
