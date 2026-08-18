#!/usr/bin/env bash
#
# install.sh — Standalone installer for rod-mcp
#
# Builds and installs the rod-mcp binary to a known location with a recorded
# version stamp so future runs can skip a no-op rebuild.
#
# Usage:
#   ./install.sh              # build + install if HEAD changed since last install
#   ./install.sh --force      # force rebuild and reinstall
#   FORCE_REBUILD=1 ./install.sh   # same as --force
#   INSTALL_PREFIX=/opt ./install.sh
#
# Environment:
#   INSTALL_PREFIX   Install root (default: $HOME/.local). The binary goes to
#                    $INSTALL_PREFIX/bin/rod-mcp.
#   XDG_DATA_HOME    Where the version stamp is recorded. Defaults to
#                    $HOME/.local/share. The stamp file is at
#                    $XDG_DATA_HOME/rod-mcp/<prefix-slug>/version, scoped
#                    to the install prefix to prevent false no-ops when
#                    installing to multiple locations.
#   FORCE_REBUILD    When set to 1 (or --force passed), forces rebuild.
#
# Exit codes:
#   0  success (or no-op when up to date)
#   1  build/install failure or invalid environment
#
# Notes:
#   - Idempotent: running twice in a row is a no-op the second time.
#   - Standalone: works without dotfiles. The script only depends on Go,
#     git, and POSIX shell utilities.
#   - The snapshotter.js and other JS assets are checked into the repo and
#     embedded at compile time via //go:embed — no npm install required.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# --- Argument parsing -------------------------------------------------------

FORCE=0
case "${1:-}" in
    --force|-f)
        FORCE=1
        shift
        ;;
    --help|-h)
        sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
        exit 0
        ;;
    "")
        ;;
    *)
        echo "Error: unknown argument: $1" >&2
        echo "Usage: $0 [--force]" >&2
        exit 1
        ;;
esac

if [[ $# -gt 0 ]]; then
    echo "Error: unexpected extra arguments: $*" >&2
    echo "Usage: $0 [--force]" >&2
    exit 1
fi

if [[ "${FORCE_REBUILD:-0}" == "1" ]]; then
    FORCE=1
fi

# --- Configuration ----------------------------------------------------------

if [[ -z "${HOME:-}" ]]; then
    echo "Error: HOME is not set; cannot determine default install prefix" >&2
    exit 1
fi

INSTALL_PREFIX="${INSTALL_PREFIX:-$HOME/.local}"
BIN_DIR="$INSTALL_PREFIX/bin"
ROD_MCP_BINARY="$BIN_DIR/rod-mcp"

XDG_DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
STAMP_DIR="$XDG_DATA_HOME/rod-mcp"

# The stamp is scoped to the install prefix so that installing to different
# prefixes doesn't produce a false "nothing to do" for a prefix that was
# never actually built. We use a sanitised version of the prefix as a subdir.
_PREFIX_SLUG="${INSTALL_PREFIX//\//_}"
_PREFIX_SLUG="${_PREFIX_SLUG#_}"   # strip leading underscore from absolute path
STAMP_FILE="$STAMP_DIR/${_PREFIX_SLUG}/version"

# --- Pre-flight -------------------------------------------------------------

if ! command -v go >/dev/null 2>&1; then
    echo "Error: go not found on PATH; install Go before running this script" >&2
    exit 1
fi

if ! command -v git >/dev/null 2>&1; then
    echo "Error: git not found on PATH; install git before running this script" >&2
    exit 1
fi

# .git is a directory in a normal clone, but a *file* in a worktree.
if [[ ! -e "$SCRIPT_DIR/.git" ]]; then
    echo "Error: $SCRIPT_DIR is not a git repo (no .git)" >&2
    echo "       install.sh must run from inside a clone of aliwatters/rod-mcp" >&2
    exit 1
fi

if [[ ! -f "$SCRIPT_DIR/go.mod" ]]; then
    echo "Error: go.mod not found at $SCRIPT_DIR" >&2
    exit 1
fi

if [[ ! -f "$SCRIPT_DIR/cmd/rod-mcp/main.go" ]]; then
    echo "Error: cmd/rod-mcp/main.go not found at $SCRIPT_DIR" >&2
    exit 1
fi

# --- Version detection ------------------------------------------------------

GIT_HEAD="$(git -C "$SCRIPT_DIR" rev-parse --short HEAD 2>/dev/null || echo unknown)"
if [[ "$GIT_HEAD" == "unknown" ]]; then
    echo "Error: could not resolve git HEAD" >&2
    exit 1
fi

INSTALLED_HEAD=""
if [[ -f "$STAMP_FILE" ]]; then
    INSTALLED_HEAD="$(cat "$STAMP_FILE" 2>/dev/null || true)"
fi

# --- Idempotency check ------------------------------------------------------

if [[ "$FORCE" -ne 1 ]] \
   && [[ -x "$ROD_MCP_BINARY" ]] \
   && [[ -n "$INSTALLED_HEAD" ]] \
   && [[ "$INSTALLED_HEAD" == "$GIT_HEAD" ]]; then
    echo "rod-mcp already installed at $GIT_HEAD; nothing to do"
    echo "  binary: $ROD_MCP_BINARY"
    echo "(re-run with --force or FORCE_REBUILD=1 to rebuild)"
    exit 0
fi

# --- Build ------------------------------------------------------------------

mkdir -p "$BIN_DIR" "$(dirname "$STAMP_FILE")"

BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "Building rod-mcp (HEAD=$GIT_HEAD)"
if ! go build \
        -ldflags "-X main.Version=$GIT_HEAD -X main.BuildTime=$BUILD_TIME" \
        -o "$ROD_MCP_BINARY" \
        ./cmd/rod-mcp; then
    echo "Error: rod-mcp build failed" >&2
    exit 1
fi
chmod +x "$ROD_MCP_BINARY"

# --- Record version stamp ---------------------------------------------------

printf '%s\n' "$GIT_HEAD" > "$STAMP_FILE"

# --- Summary ----------------------------------------------------------------

echo
echo "Installed rod-mcp at $GIT_HEAD"
echo "  binary: $ROD_MCP_BINARY"
echo "  stamp:  $STAMP_FILE"

# Hint about PATH
case ":$PATH:" in
    *":$BIN_DIR:"*)
        ;;
    *)
        echo
        echo "Note: $BIN_DIR is not on your PATH."
        echo "      Add it to your shell rc to use the rod-mcp binary directly:"
        echo "        export PATH=\"$BIN_DIR:\$PATH\""
        ;;
esac
