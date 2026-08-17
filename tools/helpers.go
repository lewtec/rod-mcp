package tools

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/aliwatters/rod-mcp/types"
)

// toolErr creates a nil result + toolError pair for handler return statements.
func toolErr(action string, err error) (*mcp.CallToolResult, error) {
	return nil, toolError(action, err)
}

// truncateContent truncates s to maxLen characters.
// Returns the (possibly truncated) string and whether truncation occurred.
func truncateContent(s string, maxLen int) (string, bool) {
	if len(s) <= maxLen {
		return s, false
	}
	return s[:maxLen], true
}

// applyMaxChars applies an opt-in character limit to content returned by tools.
//
// maxChars == 0 means unlimited — the full string is returned unchanged.
// maxChars  > 0 truncates s to that many characters and appends a notice that
// includes both the limit and the original length, pointing callers toward
// rod_evaluate for targeted extraction.
//
// Deferred decision: whether to make a non-zero default the standard is a
// maintainer choice (behaviour-change) tracked on aliwatters/rod-mcp#295.
// This helper implements only the opt-in path.
func applyMaxChars(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	return fmt.Sprintf("%s\n\n…[truncated: %d of %d chars; use rod_evaluate for targeted extraction]",
		s[:maxChars], maxChars, len(s))
}

// checkNavigationStatus inspects captured network requests for the navigated URL
// and returns an error if the main document response has a 4xx or 5xx status code.
// This allows tools to fail fast instead of waiting for timeout on error pages.
// Iterates from newest to oldest to handle stale entries in the ring buffer.
func checkNavigationStatus(rodCtx *types.Context, url string) error {
	requests := rodCtx.NetworkRequests(url, "", false)
	for i := len(requests) - 1; i >= 0; i-- {
		req := requests[i]
		if req.Type != "Document" {
			continue
		}
		if req.Status >= 400 {
			return fmt.Errorf("page returned HTTP %d — check the URL", req.Status)
		}
		return nil // found the most recent document request with a non-error status
	}
	return nil // no matching document request found — don't block
}

// waitDOMStable waits for the DOM to stabilize, logging errors at debug level.
func waitDOMStable(page *rod.Page, timeout ...time.Duration) {
	waitTimeout := defaultNavigationTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		waitTimeout = timeout[0]
	}
	if err := page.Timeout(waitTimeout).WaitDOMStable(defaultWaitStableDur, defaultDomDiff); err != nil {
		slog.Debug(fmt.Sprintf("WaitDOMStable: %s", err))
	}
}

// resolveSnapshotElement resolves a snapshot element reference from an MCP request.
// Supports three targeting modes (in priority order):
//   - ref-based: uses the exact ref from a prior snapshot (fast, requires rod_snapshot first)
//   - selector-based: uses a CSS selector to find the element directly in the DOM
//   - name-based: searches by accessible name/role, building a snapshot if needed (semantic targeting)
//
// Returns the page, resolved element, and the human-readable element description.
func resolveSnapshotElement(rodCtx *types.Context, args map[string]interface{}, toolName string) (*rod.Page, *rod.Element, string, error) {
	ele, err := getStringArg(args, "element")
	if err != nil {
		return nil, nil, "", fmt.Errorf("%s: %w", toolName, err)
	}
	page, err := rodCtx.ControlledPage()
	if err != nil {
		return nil, nil, ele, fmt.Errorf("%s %s: %w", toolName, ele, err)
	}

	ref := getOptionalStringArg(args, "ref")
	selector := getOptionalStringArg(args, "selector")
	name := getOptionalStringArg(args, "name")

	if ref == "" && selector == "" && name == "" {
		return nil, nil, ele, fmt.Errorf("%s %s: one of 'ref', 'selector', or 'name' must be provided", toolName, ele)
	}

	var element *rod.Element
	if ref != "" {
		element, err = resolveByRef(rodCtx, ref)
	} else if selector != "" {
		element, err = resolveBySelector(page, selector)
	} else {
		role := getOptionalStringArg(args, "role")
		element, err = resolveByName(rodCtx, name, role)
	}
	if err != nil {
		return nil, nil, ele, fmt.Errorf("%s %s: %w", toolName, ele, err)
	}
	return page, element, ele, nil
}

// resolveBySelector locates an element using a CSS selector.
// Supports shadow DOM piercing via >>> combinator (e.g. "my-component >>> .inner").
// Returns an error if no element is found within defaultSelectorTimeout.
func resolveBySelector(page *rod.Page, selector string) (*rod.Element, error) {
	var element *rod.Element
	var err error

	timedPage := page.Timeout(defaultSelectorTimeout)
	if strings.Contains(selector, ">>>") {
		element, err = resolvePiercingSelector(timedPage, selector)
	} else {
		element, err = timedPage.Element(selector)
	}
	if err != nil {
		info, infoErr := page.Info()
		if infoErr == nil {
			return nil, fmt.Errorf("no element found for CSS selector %q on page %q (%s): %w", selector, info.Title, info.URL, err)
		}
		return nil, fmt.Errorf("no element found for CSS selector %q: %w", selector, err)
	}
	return element, nil
}

// resolveByRef locates an element using an exact snapshot ref (e.g. "e42" or "f1e7").
func resolveByRef(rodCtx *types.Context, ref string) (*rod.Element, error) {
	snapshot, err := rodCtx.LatestSnapshot()
	if err != nil {
		return nil, err
	}
	return snapshot.LocatorInFrame(ref)
}

// resolveByName searches for an element by accessible name and optional ARIA role,
// building a snapshot if one doesn't already exist.
func resolveByName(rodCtx *types.Context, name, role string) (*rod.Element, error) {
	snapshot, err := rodCtx.EnsureSnapshot()
	if err != nil {
		return nil, fmt.Errorf("failed to build snapshot for semantic search: %w", err)
	}

	matches := snapshot.FindByNameRole(name, role)

	if len(matches) == 0 {
		ctx := pageContext(rodCtx)
		// Find similar elements with the same role for actionable suggestions
		var similar []string
		if role != "" {
			sameRole := snapshot.FindByNameRole("", role)
			for i, m := range sameRole {
				if i >= 5 {
					break
				}
				similar = append(similar, fmt.Sprintf("  ref=%s  %s", m.Ref, m.Raw))
			}
		}
		msg := fmt.Sprintf("no element found with name %q", name)
		if role != "" {
			msg = fmt.Sprintf("no element found with name %q and role %q", name, role)
		}
		if ctx != "" {
			msg += fmt.Sprintf(" on %s", ctx)
		}
		if len(similar) > 0 {
			msg += fmt.Sprintf("\n\nAvailable %ss:\n%s", role, strings.Join(similar, "\n"))
		}
		return nil, errors.New(msg)
	}

	if len(matches) > 1 {
		var descriptions []string
		for _, m := range matches {
			descriptions = append(descriptions, fmt.Sprintf("  ref=%s  %s", m.Ref, m.Raw))
		}
		return nil, fmt.Errorf(
			"%d elements match name %q — use 'ref' to disambiguate:\n%s",
			len(matches), name, strings.Join(descriptions, "\n"),
		)
	}

	return snapshot.LocatorInFrame(matches[0].Ref)
}

// keyMap maps string key names to input.Key constants.
// Supports both named keys (Tab, Enter, ArrowUp) and single characters.
var keyMap = map[string]input.Key{
	// Function keys
	"Escape": input.Escape,
	"F1":     input.F1,
	"F2":     input.F2,
	"F3":     input.F3,
	"F4":     input.F4,
	"F5":     input.F5,
	"F6":     input.F6,
	"F7":     input.F7,
	"F8":     input.F8,
	"F9":     input.F9,
	"F10":    input.F10,
	"F11":    input.F11,
	"F12":    input.F12,

	// Navigation
	"Backspace":  input.Backspace,
	"Tab":        input.Tab,
	"Enter":      input.Enter,
	"Return":     input.Enter,
	"CapsLock":   input.CapsLock,
	"Delete":     input.Delete,
	"End":        input.End,
	"Home":       input.Home,
	"Insert":     input.Insert,
	"PageDown":   input.PageDown,
	"PageUp":     input.PageUp,
	"ArrowDown":  input.ArrowDown,
	"ArrowLeft":  input.ArrowLeft,
	"ArrowRight": input.ArrowRight,
	"ArrowUp":    input.ArrowUp,

	// Modifiers
	"Alt":          input.AltLeft,
	"AltLeft":      input.AltLeft,
	"AltRight":     input.AltRight,
	"Control":      input.ControlLeft,
	"ControlLeft":  input.ControlLeft,
	"ControlRight": input.ControlRight,
	"Meta":         input.MetaLeft,
	"MetaLeft":     input.MetaLeft,
	"MetaRight":    input.MetaRight,
	"Shift":        input.ShiftLeft,
	"ShiftLeft":    input.ShiftLeft,
	"ShiftRight":   input.ShiftRight,

	// Special keys
	"Space":       input.Space,
	"PrintScreen": input.PrintScreen,
	"ScrollLock":  input.ScrollLock,
	"Pause":       input.Pause,
	"ContextMenu": input.ContextMenu,
	"NumLock":     input.NumLock,
}

// parseKey converts a key string to an input.Key.
// For single characters, returns the rune as input.Key.
// For named keys (Tab, Enter, ArrowUp, etc.), looks up in keyMap.
func parseKey(keyStr string) (input.Key, error) {
	// Check if it's a named key first
	if key, ok := keyMap[keyStr]; ok {
		return key, nil
	}

	// For single characters, use the rune value directly
	if len(keyStr) == 1 {
		return input.Key(keyStr[0]), nil
	}

	return 0, fmt.Errorf("unknown key: %s", keyStr)
}

// modifierMap maps lowercase modifier names to input.Key constants.
var modifierMap = map[string]input.Key{
	"control": input.ControlLeft,
	"ctrl":    input.ControlLeft,
	"shift":   input.ShiftLeft,
	"alt":     input.AltLeft,
	"meta":    input.MetaLeft,
	"command": input.MetaLeft,
}

// parseModifier converts a modifier string to an input.Key.
func parseModifier(mod string) (input.Key, error) {
	if key, ok := modifierMap[strings.ToLower(mod)]; ok {
		return key, nil
	}
	return 0, fmt.Errorf("unknown modifier %q: must be control, shift, alt, or meta", mod)
}

// joinModifiers joins modifier names with "+" for display.
func joinModifiers(mods []string) string {
	return strings.Join(mods, "+")
}
