# Copilot Instructions for rod-mcp

rod-mcp is a Go MCP server that wraps go-rod browser automation and exposes
browser control tools over stdio. Keep suggestions focused on the public
repository and avoid private paths, hostnames, credentials, incidents, support
queues, or agent orchestration process details.

## Repository Context

- Go module: `github.com/aliwatters/rod-mcp`.
- Runtime: follow `go.mod` for the supported Go version and dependency set.
- MCP framework: `github.com/mark3labs/mcp-go`.
- Browser automation: `github.com/go-rod/rod` controlling Chrome or Chromium.
- Main entry points are in `cmd/rod-mcp/` (`main.go`, `cmd.go`, `server.go`, `runner.go`).
- Tool implementations live in `tools/`; shared browser context, config,
  snapshots, profiles, and embedded JavaScript live in `types/`.

## Build and Verification

Use the same commands CI relies on when relevant:

```bash
go build -o rod-mcp ./cmd/rod-mcp
go vet ./...
go test ./...
go test -tags e2e -v -timeout 300s ./e2e/
```

The e2e tests launch a real browser and may need Chrome or Chromium plus system
libraries. For docs-only changes, prefer lightweight checks such as
`git diff --check`.

## Code Guidance

- Follow the existing Go style: standard library imports first, then external
  packages, then internal `github.com/aliwatters/rod-mcp/...` packages.
- Keep MCP tools in the established pattern: define an `mcp.Tool`, then provide
  a handler factory that uses `rodCtx.Execute`.
- Let `Execute` manage snapshot invalidation. Do not call
  `InvalidateSnapshot` inside tool handlers.
- Preserve ref continuity for tools that interact with snapshot elements by
  using `WithSnapshot: true` where the next interaction needs an updated
  accessibility snapshot.
- Resolve elements through the existing helper paths: refs first, CSS selectors
  when supported, and accessible-name matching for semantic targeting.
- For React-aware filling, keep using the `this` binding pattern expected by
  rod's `Eval`; do not pass the DOM element as a JavaScript function argument.
- Wrap errors with useful context and return MCP tool errors through existing
  helpers such as `toolErr`.
- Prefer table-driven tests with `t.Run` and focused assertions for tool schema,
  handler behavior, snapshots, config, and utility functions.

## Generated and Ignored Paths

- Do not review or suggest changes in ignored output such as `rod-mcp`,
  `vendor/`, `node_modules/`, `build/`, `dist/`, `tmp/`, `temp/`, runtime
  browser profiles, screenshots, PDFs, logs, or `.ax/`.
- `types/js/snapshotter.js` is the minified asset used by Go embeds. Its source
  is `types/js/snapshotter_raw.js`, and the minification command is
  `npm run dev`.
- Keep generated, embedded, and minified JavaScript changes paired with their
  source changes when they are part of a functional update.

## Review Focus

- Check that tool changes preserve text mode and vision mode behavior.
- Watch for snapshot lifecycle regressions, stale refs, missing DOM-stability
  waits after mutations, and unnecessary Chrome DevTools Protocol imports.
- Check browser-profile and cookie handling carefully for data exposure risks.
- Prefer small, public-safe documentation updates over copying long guidance
  from `docs/AGENTS.md`.
