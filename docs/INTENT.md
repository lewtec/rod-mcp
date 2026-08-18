# What rod-mcp is for

**rod-mcp is a locally run Go MCP server that lets MCP clients automate Chromium pages through go-rod.**

## Why this document exists

This settles whether the repository is an MCP browser-control server or a browser-facing application of its own. The executable, tool registration, test harness, packaging, and recent history all identify one role: expose browser automation to an MCP client over standard input and output.

## What it does

- Builds the `rod-mcp` executable. `cmd/rod-mcp` loads `types.Config`, applies CLI flags, and starts `Runner`; the runner creates an MCP server and serves it with `server.ServeStdio`.
- Supports local headless or visible Chrome with `--headless` and `--gui`, as well as `--cdp-endpoint` and `--chrome-debug-port` for a browser controlled through Chrome DevTools Protocol.
- In text mode, registers the common tools plus snapshot tools such as `rod_snapshot`, `rod_click`, `rod_hover`, `rod_fill`, and `rod_selector`. Snapshot interactions can target page elements by accessibility reference, CSS selector, or accessible name and role.
- In vision mode, registers the common tools plus `rod_vision_click` and `rod_vision_fill` for coordinate-based interaction.
- Exposes browser-workflow surfaces including `rod_navigate`, tab tools, `rod_login`, `rod_screenshot`, `rod_pdf`, `rod_console_messages`, `rod_network_requests`, `rod_storage`, and `rod_a11y_audit`.

## What it is not for

- It is not an HTTP API or web application: its only server start path calls `server.ServeStdio`, and the E2E harness communicates with the subprocess through stdin and stdout JSON-RPC.
- It is not a browser engine: `types/context_browser.go` starts or attaches to Chromium with go-rod, and the Docker image installs Chromium separately.
- It is not only a screenshot service: the registered tools and E2E coverage include navigation, semantic element interaction, form submission, tab management, browser state, and inspection in addition to screenshots and PDFs.

## How to tell it is working

- An MCP client can start `rod-mcp`, initialize over stdio, and receive the tool set selected by text or vision mode.
- With a usable Chrome or Chromium backend, `rod_navigate` can load an HTTP(S) page; its handler reports HTTP error responses instead of treating them as successful navigation.
- In text mode, `rod_snapshot` returns accessibility content and `rod_click` or `rod_fill` can act on a named, role-qualified page element. The E2E suite checks these flows.
- The same E2E suite verifies tab operations, screenshots and PDFs, storage operations, network inspection, performance, accessibility, and browser-dialog handling.

## Where it fits

It depends on `github.com/go-rod/rod` for Chromium control and `github.com/mark3labs/mcp-go` for the MCP server. The checked-in `mcp-registry.json` provides two local launch definitions: `rod-mcp` for headless operation and `rod-mcp-gui` for a visible browser session. `Dockerfile` packages the executable with Chromium for a containerized stdio process, while `.goreleaser.yml` builds release binaries for macOS, Linux, and Windows.
