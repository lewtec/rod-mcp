//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// Timeout constants for use across all test groups.
const (
	timeoutShort        = 5 * time.Second
	timeoutMedium       = 10 * time.Second
	timeoutLong         = 30 * time.Second
	timeoutColdNavigate = 90 * time.Second
)

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response (partial, for assertion).
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// mcpResult is the MCP CallToolResult shape.
type mcpResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Data string `json:"data,omitempty"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// harness manages the rod-mcp subprocess and JSON-RPC communication.
type harness struct {
	t         *testing.T
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	scanner   *bufio.Scanner
	lines     <-chan scanLine
	mu        sync.Mutex
	nextID    int
	navigated bool
	// responses stores responses by ID for out-of-order reading.
	responses map[int]jsonRPCResponse
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	binary := os.Getenv("ROD_MCP_BINARY")
	if binary == "" {
		// Build the binary
		binary = t.TempDir() + "/rod-mcp"
		build := exec.Command("go", "build", "-o", binary, "./cmd/rod-mcp")
		build.Dir = ".."
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			t.Fatalf("failed to build rod-mcp: %v", err)
		}
	}

	args := []string{"--no-banner"}

	// Default to headless: headed Chrome on macOS can abort in
	// TransformProcessType under concurrent launches, and CI has no X server.
	// Set ROD_MCP_E2E_HEADED only for local visual debugging.
	if os.Getenv("ROD_MCP_E2E_HEADED") == "" {
		args = append(args, "--headless")
	}

	cmd := exec.Command(binary, args...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start rod-mcp: %v", err)
	}

	h := &harness{
		t:         t,
		cmd:       cmd,
		stdin:     stdin,
		scanner:   bufio.NewScanner(stdout),
		nextID:    1,
		responses: make(map[int]jsonRPCResponse),
	}

	// Set a large scanner buffer for big responses (screenshots, HTML).
	h.scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024)
	h.lines = h.startScanner()

	t.Cleanup(func() {
		stdin.Close()
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(timeoutMedium):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	return h
}

// send sends a JSON-RPC request and returns the assigned ID.
func (h *harness) send(method string, params any) int {
	h.t.Helper()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	h.mu.Lock()
	req.ID = h.nextID
	h.nextID++
	data, err := json.Marshal(req)
	if err != nil {
		h.mu.Unlock()
		h.t.Fatalf("marshal request: %v", err)
	}
	data = append(data, '\n')
	_, err = h.stdin.Write(data)
	h.mu.Unlock()
	if err != nil {
		h.t.Fatalf("write request: %v", err)
	}
	return req.ID
}

type scanLine struct {
	line string
	eof  bool
	err  error
}

// startScanner launches a goroutine that feeds scanned lines into a channel.
// Must be called once after creating the harness.
func (h *harness) startScanner() <-chan scanLine {
	ch := make(chan scanLine, 16)
	go func() {
		defer close(ch)
		for h.scanner.Scan() {
			ch <- scanLine{line: h.scanner.Text()}
		}
		if err := h.scanner.Err(); err != nil {
			ch <- scanLine{err: err}
		} else {
			ch <- scanLine{eof: true}
		}
	}()
	return ch
}

// recv waits for a response with the given ID, with timeout.
func (h *harness) recv(id int, timeout time.Duration) jsonRPCResponse {
	h.t.Helper()

	// Check if already received.
	h.mu.Lock()
	if resp, ok := h.responses[id]; ok {
		delete(h.responses, id)
		h.mu.Unlock()
		return resp
	}
	h.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case sl, ok := <-h.lines:
			if !ok || sl.eof {
				h.t.Fatal("unexpected EOF from rod-mcp")
			}
			if sl.err != nil {
				h.t.Fatalf("scanner error: %v", sl.err)
			}

			var resp jsonRPCResponse
			if err := json.Unmarshal([]byte(sl.line), &resp); err != nil {
				h.t.Logf("non-JSON line: %s", sl.line)
				continue
			}

			if resp.ID == id {
				return resp
			}
			// Store for later retrieval.
			h.mu.Lock()
			h.responses[resp.ID] = resp
			h.mu.Unlock()

		case <-timer.C:
			h.t.Fatalf("timeout (%v) waiting for response id=%d", timeout, id)
			return jsonRPCResponse{} // unreachable
		}
	}
}

// call sends a tools/call request and returns the text result.
func (h *harness) call(tool string, args map[string]any) string {
	return h.callWithTimeout(tool, args, 15*time.Second)
}

// callWithTimeout sends a tools/call request with a custom timeout.
func (h *harness) callWithTimeout(tool string, args map[string]any, timeout time.Duration) string {
	h.t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	id := h.send("tools/call", map[string]any{
		"name":      tool,
		"arguments": args,
	})
	resp := h.recv(id, timeout)
	return h.extractText(resp)
}

// extractText extracts the text content from an MCP response.
func (h *harness) extractText(resp jsonRPCResponse) string {
	h.t.Helper()
	if resp.Error != nil {
		return string(resp.Error)
	}
	var result mcpResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		h.t.Fatalf("unmarshal result: %v (raw: %s)", err, string(resp.Result))
	}
	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// assertContains checks that the result contains the expected substring.
func assertContains(t *testing.T, result, substr string) {
	t.Helper()
	if !strings.Contains(result, substr) {
		t.Errorf("expected result to contain %q, got: %s", substr, truncate(result, 300))
	}
}

// assertContainsAny checks that the result contains at least one of the substrings.
func assertContainsAny(t *testing.T, result string, substrs ...string) {
	t.Helper()
	for _, s := range substrs {
		if strings.Contains(result, s) {
			return
		}
	}
	t.Errorf("expected result to contain one of %v, got: %s", substrs, truncate(result, 300))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// navigate navigates to a URL and waits for the page body to be visible.
// Uses a longer timeout to accommodate browser launch on first navigation.
func (h *harness) navigate(url string) string {
	h.t.Helper()
	result := h.callWithTimeout("rod_navigate", map[string]any{"url": url}, h.navigateTimeout())
	h.callWithTimeout("rod_wait_for", map[string]any{"selector": "body", "timeout": 15000}, 20*time.Second)
	return result
}

func (h *harness) navigateTimeout() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.navigated {
		return timeoutLong
	}
	h.navigated = true
	return timeoutColdNavigate
}

// initialize performs the MCP handshake.
func (h *harness) initialize() {
	h.t.Helper()
	id := h.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "e2e-test",
			"version": "1.0",
		},
	})
	resp := h.recv(id, timeoutShort)
	text := string(resp.Result)
	if !strings.Contains(text, "protocolVersion") {
		h.t.Fatalf("handshake failed: %s", text)
	}
}

// retry calls fn up to maxAttempts times with a delay between attempts,
// returning once fn returns true or all attempts are exhausted.
func retry(maxAttempts int, delay time.Duration, fn func() bool) bool {
	for range maxAttempts {
		if fn() {
			return true
		}
		time.Sleep(delay)
	}
	return false
}

// skipIfShort skips the test if running in short mode.
func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
}

// TestE2E_Configure tests the rod_configure tool.
func TestE2E_Configure(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()

	result := h.call("rod_configure", nil)
	assertContainsAny(t, result, "No configuration", "headless", "Configuration")
}

// TestE2E_Navigation tests navigate, go_back, go_forward, and reload.
func TestE2E_Navigation(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()

	t.Run("navigate", func(t *testing.T) {
		result := h.navigate("https://the-internet.herokuapp.com")
		assertContains(t, result, "Navigated to")
	})

	t.Run("go_back", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com/login")
		result := h.call("rod_go_back", nil)
		assertContainsAny(t, result, "Go back successfully", "back")
	})

	t.Run("go_forward", func(t *testing.T) {
		result := h.call("rod_go_forward", nil)
		assertContainsAny(t, result, "Go forward successfully", "forward")
	})

	t.Run("reload", func(t *testing.T) {
		result := h.call("rod_reload", nil)
		assertContainsAny(t, result, "Reload current page successfully", "reload")
	})

	t.Run("wait_for", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com")
		result := h.call("rod_wait_for", map[string]any{
			"selector": "h1", "timeout": 5000,
		})
		assertContainsAny(t, result, "visible", "found", "appeared")
	})

	t.Run("navigate_404_fails_fast", func(t *testing.T) {
		start := time.Now()
		result := h.callWithTimeout("rod_navigate", map[string]any{
			"url": "https://the-internet.herokuapp.com/nonexistent-page-404",
		}, timeoutLong)
		elapsed := time.Since(start)
		assertContains(t, result, "HTTP 404")
		if elapsed > 10*time.Second {
			t.Errorf("expected fast failure but took %v", elapsed)
		}
	})

	t.Run("close_browser_then_navigate_relaunches", func(t *testing.T) {
		result := h.call("rod_close_browser", nil)
		assertContains(t, result, "Close browser successfully")

		result = h.callWithTimeout("rod_navigate", map[string]any{
			"url": "https://the-internet.herokuapp.com",
		}, timeoutColdNavigate)
		assertContains(t, result, "Navigated to")
	})
}

// TestE2E_Login tests the rod_login tool.
func TestE2E_Login(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()

	t.Run("login_404_fails_fast", func(t *testing.T) {
		start := time.Now()
		result := h.callWithTimeout("rod_login", map[string]any{
			"url":      "https://the-internet.herokuapp.com/nonexistent-login-404",
			"username": "test@example.com",
			"password": "password123",
		}, timeoutLong)
		elapsed := time.Since(start)
		assertContains(t, result, "HTTP 404")
		if elapsed > 10*time.Second {
			t.Errorf("expected fast failure but took %v", elapsed)
		}
	})
}

// TestE2E_Tabs tests tab_new, tab_list, tab_select, and tab_close.
func TestE2E_Tabs(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	t.Run("new", func(t *testing.T) {
		result := h.call("rod_tab_new", map[string]any{"url": "https://example.com"})
		assertContainsAny(t, result, "New tab", "created", "content")
	})

	t.Run("list", func(t *testing.T) {
		h.call("rod_wait_for", map[string]any{"selector": "body", "timeout": 10000})
		result := h.call("rod_tab_list", nil)
		assertContainsAny(t, result, "example.com", "the-internet")
	})

	t.Run("select", func(t *testing.T) {
		result := h.call("rod_tab_select", map[string]any{"index": 0})
		assertContainsAny(t, result, "Switched", "switched", "content")
	})

	t.Run("close", func(t *testing.T) {
		result := h.call("rod_tab_close", map[string]any{"index": 1})
		assertContainsAny(t, result, "Closed", "closed")
	})
}

// TestE2E_Input tests scroll, press, hover, and drag interactions.
func TestE2E_Input(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()

	t.Run("scroll", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com")

		t.Run("down", func(t *testing.T) {
			result := h.call("rod_scroll", map[string]any{"direction": "down", "amount": 500})
			assertContains(t, result, "Scrolled down")
		})
		t.Run("up", func(t *testing.T) {
			result := h.call("rod_scroll", map[string]any{"direction": "up", "amount": 500})
			assertContains(t, result, "Scrolled up")
		})
		t.Run("absolute", func(t *testing.T) {
			result := h.call("rod_scroll", map[string]any{"x": 0, "y": 0})
			assertContains(t, result, "Scrolled to")
		})
	})

	t.Run("press_key", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com/key_presses")
		result := h.call("rod_press", map[string]any{"key": "a"})
		assertContainsAny(t, result, "Press key", "pressed", "successfully")
	})

	t.Run("type_unicode", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com")
		text := "em—dash, é, “smart”, 👍"

		result := h.call("rod_evaluate", map[string]any{
			"script": `() => {
				document.body.innerHTML = '<input id="unicode-input" />';
				const input = document.querySelector("#unicode-input");
				input.focus();
				return document.activeElement === input ? "focused" : "not-focused";
			}`,
		})
		assertContains(t, result, "focused")

		result = h.call("rod_type", map[string]any{"text": text, "delay": 0})
		assertContains(t, result, "Typed")

		result = h.call("rod_evaluate", map[string]any{
			"script": `() => document.querySelector("#unicode-input").value`,
		})
		assertContains(t, result, text)
	})

	t.Run("hover_via_evaluate", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com/hovers")
		result := h.call("rod_evaluate", map[string]any{
			"script": `() => { const el = document.querySelector(".figure img"); if (!el) return "no-element"; el.dispatchEvent(new MouseEvent("mouseover", {bubbles:true})); return "hovered"; }`,
		})
		assertContains(t, result, "hovered")
	})

	t.Run("drag", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com/drag_and_drop")

		t.Run("selector_based", func(t *testing.T) {
			result := h.call("rod_drag", map[string]any{
				"sourceSelector": "#column-a", "targetSelector": "#column-b", "steps": 20,
			})
			assertContains(t, result, "Dragged from")
		})
		t.Run("coordinate_based", func(t *testing.T) {
			result := h.call("rod_drag", map[string]any{
				"startX": 200, "startY": 300, "endX": 500, "endY": 300, "steps": 15,
			})
			assertContains(t, result, "Dragged from")
		})
	})
}

// TestE2E_FormInteraction tests click, fill, and form submission via semantic selectors.
func TestE2E_FormInteraction(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()

	t.Run("semantic_click", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com/add_remove_elements/")
		result := h.call("rod_click", map[string]any{
			"element": "Add Element button",
			"name":    "Add Element",
		})
		assertContains(t, result, "Click element")

		result = h.call("rod_snapshot", nil)
		assertContains(t, result, "Delete")
	})

	t.Run("semantic_click_with_role", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com/login")
		result := h.call("rod_click", map[string]any{
			"element": "Login button",
			"name":    "Login",
			"role":    "button",
		})
		assertContainsAny(t, result, "Click element", "click element")
	})

	t.Run("semantic_fill", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com/login")
		result := h.call("rod_fill", map[string]any{
			"element": "Username field",
			"name":    "Username",
			"role":    "textbox",
			"value":   "tomsmith",
			"submit":  false,
		})
		assertContains(t, result, "Fill element")
	})

	t.Run("form_submit", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com/login")

		// Fill form fields via evaluate (snapshot refs are dynamic).
		// Public test credentials displayed on the-internet.herokuapp.com/login page.
		// Chrome's PasswordLeakDetection is disabled in browser launch flags.
		result := h.call("rod_evaluate", map[string]any{
			"script": `() => { document.querySelector("#username").value = "tomsmith"; return "filled"; }`,
		})
		assertContains(t, result, "filled")

		result = h.call("rod_evaluate", map[string]any{
			"script": `() => { document.querySelector("#password").value = "SuperSecretPassword!"; return "filled"; }`,
		})
		assertContains(t, result, "filled")

		result = h.call("rod_evaluate", map[string]any{
			"script": `() => { document.querySelector("button[type=submit]").click(); return "clicked"; }`,
		})
		assertContains(t, result, "clicked")

		h.call("rod_wait_for", map[string]any{"selector": "h2", "timeout": 10000})

		result = h.call("rod_html", map[string]any{"action": "element", "selector": "h2"})
		assertContains(t, result, "Secure Area")
	})
}

// TestE2E_Snapshot tests snapshot, selector, and html tools.
func TestE2E_Snapshot(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	t.Run("snapshot", func(t *testing.T) {
		result := h.call("rod_snapshot", nil)
		assertContainsAny(t, result, "heading", "link")
	})

	t.Run("html_page", func(t *testing.T) {
		result := h.call("rod_html", map[string]any{"action": "page"})
		assertContains(t, result, "html")
	})

	t.Run("html_element", func(t *testing.T) {
		result := h.call("rod_html", map[string]any{"action": "element", "selector": "h1"})
		assertContainsAny(t, result, "the-internet", "Welcome")
	})
}

// TestE2E_Screenshot tests screenshot, pdf, and resize tools.
func TestE2E_Screenshot(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	t.Run("screenshot", func(t *testing.T) {
		result := h.call("rod_screenshot", map[string]any{"name": "e2e-test"})
		assertContainsAny(t, result, "image", "screenshot", "saved")
	})

	t.Run("pdf", func(t *testing.T) {
		result := h.call("rod_pdf", nil)
		assertContainsAny(t, result, "PDF", "pdf", "saved")
	})

	t.Run("resize", func(t *testing.T) {
		t.Run("mobile", func(t *testing.T) {
			result := h.call("rod_resize", map[string]any{
				"width": 375, "height": 812, "device_scale_factor": 3,
				"is_mobile": true, "has_touch": true,
				"user_agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X)",
			})
			assertContains(t, result, "375x812")
		})
		t.Run("desktop", func(t *testing.T) {
			result := h.call("rod_resize", map[string]any{
				"width": 1920, "height": 1080, "device_scale_factor": 1,
				"is_mobile": false, "has_touch": false,
			})
			assertContains(t, result, "1920x1080")
		})
	})
}

// TestE2E_Storage tests localStorage and sessionStorage operations.
func TestE2E_Storage(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	t.Run("local_set", func(t *testing.T) {
		result := h.call("rod_storage", map[string]any{
			"type": "local", "action": "set", "key": "e2eKey", "value": "e2eVal",
		})
		assertContains(t, result, "Set localStorage")
	})

	t.Run("local_get", func(t *testing.T) {
		result := h.call("rod_storage", map[string]any{
			"type": "local", "action": "get", "key": "e2eKey",
		})
		assertContains(t, result, "e2eVal")
	})

	t.Run("local_list", func(t *testing.T) {
		result := h.call("rod_storage", map[string]any{
			"type": "local", "action": "list",
		})
		assertContains(t, result, "e2eKey")
	})

	t.Run("local_remove", func(t *testing.T) {
		result := h.call("rod_storage", map[string]any{
			"type": "local", "action": "remove", "key": "e2eKey",
		})
		assertContains(t, result, "Removed")
	})

	t.Run("session_list", func(t *testing.T) {
		result := h.call("rod_storage", map[string]any{
			"type": "session", "action": "list",
		})
		assertContainsAny(t, result, "empty", "sessionStorage")
	})

	t.Run("cookies_set", func(t *testing.T) {
		url := "https://the-internet.herokuapp.com"
		result := h.call("rod_cookies", map[string]any{
			"action": "set", "name": "e2e_cookie", "value": "hello42", "url": url,
		})
		assertContainsAny(t, result, "set successfully", "Set cookie")
	})

	t.Run("cookies_get", func(t *testing.T) {
		url := "https://the-internet.herokuapp.com"
		result := h.call("rod_cookies", map[string]any{
			"action": "get", "url": url,
		})
		assertContainsAny(t, result, "e2e_cookie", "hello42")
	})

	t.Run("cookies_delete", func(t *testing.T) {
		url := "https://the-internet.herokuapp.com"
		result := h.call("rod_cookies", map[string]any{
			"action": "delete", "name": "e2e_cookie", "url": url,
		})
		assertContainsAny(t, result, "deleted successfully", "Deleted")
	})
}

// TestE2E_Evaluate tests the rod_evaluate tool.
func TestE2E_Evaluate(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	t.Run("sync_arrow", func(t *testing.T) {
		result := h.call("rod_evaluate", map[string]any{
			"script": "() => document.title",
		})
		assertContainsAny(t, result, "the-internet", "Internet")
	})

	// Regression test for issue #280: async functions and promise-returning
	// scripts must serialize the resolved value, not the Promise reference.
	// Before the fix the result was "{}" because the async arrow was treated
	// as a bare expression that evaluated to a function reference.
	t.Run("async_arrow_returns_resolved_value", func(t *testing.T) {
		result := h.callWithTimeout("rod_evaluate", map[string]any{
			"script": `async () => {
				await new Promise(r => setTimeout(r, 50));
				return { ok: true, count: 7 };
			}`,
		}, timeoutMedium)
		assertContains(t, result, `"ok":true`)
		assertContains(t, result, `"count":7`)
	})

	t.Run("async_arrow_returns_array", func(t *testing.T) {
		result := h.callWithTimeout("rod_evaluate", map[string]any{
			"script": `async () => {
				await new Promise(r => setTimeout(r, 10));
				return [1, 2, 3];
			}`,
		}, timeoutMedium)
		assertContains(t, result, "[1,2,3]")
	})
}

func TestE2E_SetHeadersBeforeNavigate(t *testing.T) {
	skipIfShort(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer srv.Close()

	h := newHarness(t)
	h.initialize()

	result := h.callWithTimeout("rod_set_headers", map[string]any{
		"headers": map[string]string{
			"X-Test-Header": "first-call",
		},
	}, timeoutMedium)
	assertContains(t, result, "no active page")
	assertContains(t, result, "domainHeaders")
	if strings.Contains(result, "panic") {
		t.Fatalf("rod_set_headers before navigation returned panic text: %s", truncate(result, 300))
	}

	result = h.navigate(srv.URL)
	assertContains(t, result, "Navigated to "+srv.URL)
}

// TestE2E_Network tests network_requests, response_body, set_headers, intercept, and websocket.
func TestE2E_Network(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	t.Run("network_requests", func(t *testing.T) {
		result := h.call("rod_network_requests", nil)
		assertContains(t, result, "the-internet")
	})

	t.Run("response_body", func(t *testing.T) {
		result := h.call("rod_response_body", map[string]any{"index": 0})
		assertContainsAny(t, result, "Response body", "html", "DOCTYPE")
	})

	t.Run("set_headers", func(t *testing.T) {
		result := h.call("rod_set_headers", map[string]any{
			"headers": map[string]string{
				"X-Test-Header": "e2e-value",
			},
		})
		assertContains(t, result, "Set 1 header")
	})

	t.Run("intercept", func(t *testing.T) {
		t.Run("enable", func(t *testing.T) {
			result := h.call("rod_intercept", map[string]any{"action": "enable"})
			assertContains(t, result, "interception enabled")
		})
		t.Run("mock", func(t *testing.T) {
			result := h.call("rod_intercept", map[string]any{
				"action": "mock", "urlPattern": "*mock-test*", "status": 200,
				"body": `{"mocked":true}`,
			})
			assertContains(t, result, "Mock rule added")
		})
		t.Run("block", func(t *testing.T) {
			result := h.call("rod_intercept", map[string]any{
				"action": "block", "urlPattern": "*blocked-test*",
			})
			assertContains(t, result, "Block rule added")
		})
		t.Run("fail", func(t *testing.T) {
			result := h.call("rod_intercept", map[string]any{
				"action": "fail", "urlPattern": "*fail-test*", "errorReason": "ConnectionRefused",
			})
			assertContains(t, result, "Fail rule added")
		})
		t.Run("list", func(t *testing.T) {
			result := h.call("rod_intercept", map[string]any{"action": "list"})
			assertContains(t, result, "Interception rules")
		})
		t.Run("disable", func(t *testing.T) {
			result := h.call("rod_intercept", map[string]any{"action": "disable"})
			assertContains(t, result, "disabled and rules cleared")
		})
	})

	t.Run("intercept_live_mock", func(t *testing.T) {
		result := h.call("rod_intercept", map[string]any{"action": "enable"})
		assertContains(t, result, "enabled")

		h.call("rod_intercept", map[string]any{
			"action": "mock", "urlPattern": "*mocked-endpoint*", "status": 200,
			"body":    `{"status":"mocked"}`,
			"headers": map[string]string{"Content-Type": "application/json"},
		})

		result = h.callWithTimeout("rod_evaluate", map[string]any{
			"script": `() => fetch("/mocked-endpoint").then(r => r.json()).then(d => JSON.stringify(d))`,
		}, timeoutMedium)
		assertContainsAny(t, result, "mocked", "status")

		h.call("rod_intercept", map[string]any{"action": "disable"})
	})

	t.Run("websocket", func(t *testing.T) {
		t.Run("list", func(t *testing.T) {
			result := h.call("rod_websocket", map[string]any{"action": "list"})
			assertContainsAny(t, result, "No WebSocket", "WebSocket connections")
		})
		t.Run("frames", func(t *testing.T) {
			result := h.call("rod_websocket", map[string]any{"action": "frames"})
			assertContainsAny(t, result, "No WebSocket", "WebSocket frames")
		})
		t.Run("clear", func(t *testing.T) {
			result := h.call("rod_websocket", map[string]any{"action": "clear"})
			assertContains(t, result, "cleared")
		})
	})
}

// TestE2E_Coverage tests the rod_coverage tool.
func TestE2E_Coverage(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	t.Run("start", func(t *testing.T) {
		result := h.call("rod_coverage", map[string]any{"action": "start"})
		assertContains(t, result, "Coverage collection started")
	})

	t.Run("report", func(t *testing.T) {
		result := h.call("rod_coverage", map[string]any{"action": "report"})
		assertContains(t, result, "Coverage")
	})

	t.Run("stop", func(t *testing.T) {
		result := h.call("rod_coverage", map[string]any{"action": "stop"})
		assertContains(t, result, "Coverage collection stopped")
	})
}

// TestE2E_Performance tests the rod_performance tool.
func TestE2E_Performance(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	t.Run("metrics", func(t *testing.T) {
		result := h.call("rod_performance", map[string]any{"action": "metrics"})
		assertContains(t, result, "Performance metrics")
	})

	t.Run("vitals", func(t *testing.T) {
		result := h.call("rod_performance", map[string]any{"action": "vitals"})
		assertContainsAny(t, result, "ttfb", "fcp", "cls")
	})
}

// TestE2E_Accessibility tests the rod_a11y_audit tool.
func TestE2E_Accessibility(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()

	t.Run("audit_page", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com")
		result := h.call("rod_a11y_audit", nil)
		assertContains(t, result, "summary")
		assertContains(t, result, "elements_scanned")
		assertContains(t, result, "coverage")
	})

	t.Run("audit_with_selector", func(t *testing.T) {
		h.navigate("https://the-internet.herokuapp.com/login")
		result := h.call("rod_a11y_audit", map[string]any{
			"selector": "form#login",
		})
		assertContains(t, result, "summary")
		assertContains(t, result, "elements_scanned")
	})
}

// TestE2E_Dialog tests the rod_handle_dialog tool.
func TestE2E_Dialog(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com/javascript_alerts")

	// rod_handle_dialog sets up a listener and blocks until a dialog appears.
	// Send it first, then trigger the dialog — MCP processes requests concurrently.
	handleID := h.send("tools/call", map[string]any{
		"name":      "rod_handle_dialog",
		"arguments": map[string]any{"action": "accept"},
	})

	// Use setTimeout to trigger the dialog after a delay inside the browser.
	// This eliminates the race between MCP request processing and CDP listener
	// registration — the evaluate returns immediately and the dialog fires
	// after the listener is guaranteed to be ready.
	clickID := h.send("tools/call", map[string]any{
		"name":      "rod_evaluate",
		"arguments": map[string]any{"script": `() => { setTimeout(() => document.querySelector("button[onclick='jsAlert()']").click(), 1000); return "scheduled"; }`},
	})

	// The evaluate returns immediately with "scheduled".
	clickResp := h.recv(clickID, timeoutShort)
	clickResult := h.extractText(clickResp)
	assertContains(t, clickResult, "scheduled")

	// handle_dialog should complete once the dialog appears and is accepted.
	handleResp := h.recv(handleID, timeoutMedium)
	handleResult := h.extractText(handleResp)
	assertContains(t, handleResult, "Dialog accepted successfully")
	assertContains(t, handleResult, "alert")
}

// TestE2E_Console tests the rod_console_messages tool.
func TestE2E_Console(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	h.call("rod_evaluate", map[string]any{
		"script": `() => { console.log("e2e-test-msg-42"); return "done"; }`,
	})

	// Poll for the console message (CDP event propagation is async).
	var result string
	found := retry(10, 100*time.Millisecond, func() bool {
		result = h.call("rod_console_messages", nil)
		return strings.Contains(result, "e2e-test-msg-42")
	})
	if !found {
		t.Errorf("expected console message not found after retries; last result: %s", truncate(result, 300))
	}
}

// TestE2E_Permissions tests the rod_permissions tool.
func TestE2E_Permissions(t *testing.T) {
	skipIfShort(t)
	h := newHarness(t)
	h.initialize()
	h.navigate("https://the-internet.herokuapp.com")

	t.Run("grant", func(t *testing.T) {
		result := h.call("rod_permissions", map[string]any{
			"action":      "grant",
			"permissions": []string{"geolocation", "notifications"},
		})
		assertContains(t, result, "Granted")
	})

	t.Run("reset", func(t *testing.T) {
		result := h.call("rod_permissions", map[string]any{"action": "reset"})
		assertContainsAny(t, result, "Reset", "reset")
	})
}
