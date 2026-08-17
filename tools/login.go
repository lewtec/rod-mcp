package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/aliwatters/rod-mcp/types"
	"github.com/aliwatters/rod-mcp/types/js"
)

const (
	LoginToolKey = "rod_login"
)

var (
	Login = mcp.NewTool(LoginToolKey,
		mcp.WithDescription("Execute a login flow in one call: navigate to URL, fill credentials, submit, and verify success. Supports both dedicated login pages and modal dialogs triggered by a button click. Replaces 5-6 individual MCP calls."),
		mcp.WithString("url", mcp.Description("Login page URL (or base page URL when using trigger_selector)"), mcp.Required()),
		mcp.WithString("username", mcp.Description("Username or email to fill"), mcp.Required()),
		mcp.WithString("password", mcp.Description("Password to fill"), mcp.Required()),
		mcp.WithString("username_selector", mcp.Description("CSS selector for username/email field (default: input[type=email], input[name=email], input[name=username])")),
		mcp.WithString("password_selector", mcp.Description("CSS selector for password field (default: input[type=password])")),
		mcp.WithString("submit_selector", mcp.Description("CSS selector for submit button (default: button[type=submit])")),
		mcp.WithString("success_selector", mcp.Description("CSS selector that indicates login succeeded (waits for it to appear)")),
		mcp.WithString("success_url_contains", mcp.Description("Substring to match in URL after login to verify success")),
		mcp.WithNumber("timeout", mcp.Description("Max wait time for success verification in milliseconds (default: 15000)")),
		mcp.WithString("trigger_selector", mcp.Description("CSS selector for a button/link that opens the login form (for modal-based logins). Navigate to url first, then click this element before filling credentials.")),
		mcp.WithString("form_container", mcp.Description("CSS selector for the login form container to wait for after clicking trigger (default: [role=dialog]). Only used with trigger_selector.")),
	)
)

// defaultUsernameSelectors are tried in order when no username_selector is provided.
var defaultUsernameSelectors = []string{
	"input[type=email]",
	"input[name=email]",
	"input[name=username]",
	"input[name=login]",
	"input[id=email]",
	"input[id=username]",
}

// loginSmartFill fills an input using the embedded smart fill JS for React support.
func loginSmartFill(element *rod.Element, value string) error {
	obj, err := element.Eval(js.SmartFillJS, value)
	if err != nil {
		return fmt.Errorf("smart fill failed: %w", err)
	}
	var result struct {
		Success bool   `json:"success"`
		Value   string `json:"value"`
	}
	if parseErr := json.Unmarshal([]byte(obj.Value.Str()), &result); parseErr != nil {
		slog.Warn("loginSmartFill: failed to parse smart fill result", "err", parseErr)
		return nil // fill was attempted, can't verify
	}
	if !result.Success {
		return fmt.Errorf("fill produced %q instead of expected value", result.Value)
	}
	return nil
}

// loginParams holds parsed and defaulted parameters for the login flow.
type loginParams struct {
	loginURL        string
	username        string
	password        string
	userSelector    string
	passSelector    string
	submitSelector  string
	successSelector string
	successURL      string
	timeout         float64
	triggerSelector string
	formContainer   string
}

// parseLoginParams extracts and defaults login parameters from the MCP request.
// Config-level defaults are used when the request doesn't specify selectors.
func parseLoginParams(args map[string]interface{}, cfg types.Config) (loginParams, error) {
	loginURL, err := getStringArg(args, "url")
	if err != nil {
		return loginParams{}, err
	}
	username, err := getStringArg(args, "username")
	if err != nil {
		return loginParams{}, err
	}
	password, err := getStringArg(args, "password")
	if err != nil {
		return loginParams{}, err
	}

	p := loginParams{
		loginURL:        loginURL,
		username:        username,
		password:        password,
		userSelector:    getOptionalStringArg(args, "username_selector"),
		passSelector:    getOptionalStringArg(args, "password_selector"),
		submitSelector:  getOptionalStringArg(args, "submit_selector"),
		successSelector: getOptionalStringArg(args, "success_selector"),
		successURL:      getOptionalStringArg(args, "success_url_contains"),
		timeout:         getOptionalFloatArg(args, "timeout", defaultLoginTimeoutMs),
		triggerSelector: getOptionalStringArg(args, "trigger_selector"),
		formContainer:   getOptionalStringArg(args, "form_container"),
	}

	if p.passSelector == "" {
		p.passSelector = cfg.LoginPasswordSelector
	}
	if p.passSelector == "" {
		p.passSelector = "input[type=password]"
	}
	if p.submitSelector == "" {
		p.submitSelector = cfg.LoginSubmitSelector
	}
	if p.submitSelector == "" {
		p.submitSelector = "button[type=submit]"
	}
	if p.formContainer == "" {
		p.formContainer = "[role=dialog]"
	}
	return p, nil
}

// loginFillUsername finds and fills the username field on the page.
// If userSelector is empty, tries the provided selectors (or defaults) with optional scope prefix.
func loginFillUsername(page *rod.Page, username, userSelector, scope string, selectors []string) error {
	if userSelector != "" {
		el, err := page.Timeout(defaultSelectorTimeout).Element(userSelector)
		if err != nil {
			return fmt.Errorf("selector %q: %w", userSelector, err)
		}
		return loginSmartFill(el, username)
	}

	if len(selectors) == 0 {
		selectors = defaultUsernameSelectors
	}
	// Use a short probe timeout when trying multiple fallback selectors — each one
	// should fail fast if not present rather than blocking the full defaultSelectorTimeout.
	shortProbe := page.Timeout(2 * time.Second)
	for _, sel := range selectors {
		if scope != "" {
			sel = scope + " " + sel
		}
		el, err := shortProbe.Element(sel)
		if err == nil {
			if fillErr := loginSmartFill(el, username); fillErr == nil {
				return nil
			}
		}
	}
	if scope != "" {
		return fmt.Errorf("could not find username field in %s; provide username_selector", scope)
	}
	return fmt.Errorf("could not find username field; provide username_selector")
}

// loginFillPassword finds and fills the password field, trying a scoped selector as fallback.
func loginFillPassword(page *rod.Page, password, passSelector, scope string) error {
	timedPage := page.Timeout(defaultSelectorTimeout)
	el, err := timedPage.Element(passSelector)
	if err != nil && scope != "" {
		el, err = timedPage.Element(scope + " " + passSelector)
	}
	if err != nil {
		return fmt.Errorf("selector %q: %w", passSelector, err)
	}
	return loginSmartFill(el, password)
}

// loginSubmit finds and clicks the submit button, trying a scoped selector as fallback.
func loginSubmit(page *rod.Page, submitSelector, scope string) error {
	timedPage := page.Timeout(defaultSelectorTimeout)
	el, err := timedPage.Element(submitSelector)
	if err != nil && scope != "" {
		el, err = timedPage.Element(scope + " " + submitSelector)
	}
	if err != nil {
		return fmt.Errorf("selector %q: %w", submitSelector, err)
	}
	return el.Click(proto.InputMouseButtonLeft, 1)
}

// loginOpenTrigger clicks a trigger element and waits for the form container to appear.
func loginOpenTrigger(page *rod.Page, triggerSelector, formContainer string) error {
	triggerEl, err := page.Timeout(defaultSelectorTimeout).Element(triggerSelector)
	if err != nil {
		return fmt.Errorf("find trigger %q: %w", triggerSelector, err)
	}
	if err := triggerEl.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click trigger: %w", err)
	}
	if _, err := page.Timeout(defaultFormContainerTimeout).Element(formContainer); err != nil {
		return fmt.Errorf("form container %q did not appear after clicking trigger: %w", formContainer, err)
	}
	waitDOMStable(page)
	return nil
}

// loginVerifySuccess polls for success indicators until the user-provided timeout expires
// or the context is cancelled. All rod calls are wrapped with the remaining timeout so
// that page.Element() and page.Info() cannot hang longer than the budget.
func loginVerifySuccess(ctx context.Context, page *rod.Page, successSelector, successURL string, timeout float64) bool {
	if successSelector == "" && successURL == "" {
		// No success indicator specified — just wait for the page load.
		// Use a short bounded timeout rather than rod's default so we don't hang.
		shortTimeout := time.Duration(timeout) * time.Millisecond
		if navErr := page.Timeout(shortTimeout).WaitLoad(); navErr == nil {
			waitDOMStable(page)
		}
		return true
	}

	timeoutDur := time.Duration(timeout) * time.Millisecond
	deadline := time.Now().Add(timeoutDur)
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}

		// Use a short probe timeout per check — at most 500ms or remaining budget,
		// whichever is smaller. This keeps each individual rod call bounded.
		probeTimeout := 500 * time.Millisecond
		if remaining < probeTimeout {
			probeTimeout = remaining
		}

		if successSelector != "" {
			if _, err := page.Timeout(probeTimeout).Element(successSelector); err == nil {
				return true
			}
		}
		if successURL != "" {
			if info, err := page.Timeout(probeTimeout).Info(); err == nil && strings.Contains(info.URL, successURL) {
				return true
			}
		}

		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			continue
		case <-time.After(remaining):
			return false
		}
	}
}

// loginBuildResult collects the login result (URL, title, cookies) into JSON.
func loginBuildResult(page *rod.Page, verified bool, timeout float64) (string, error) {
	info, infoErr := page.Info()
	currentURL := ""
	title := ""
	if infoErr != nil {
		slog.Debug("login page info", "err", infoErr)
	} else if info != nil {
		currentURL = info.URL
		title = info.Title
	}

	resp, cookieErr := proto.NetworkGetCookies{}.Call(page)
	cookieCount := 0
	if cookieErr != nil {
		slog.Debug("login get cookies", "err", cookieErr)
	} else if resp != nil {
		cookieCount = len(resp.Cookies)
	}

	result := map[string]interface{}{
		"success":     verified,
		"url":         currentURL,
		"title":       title,
		"cookies_set": cookieCount,
	}
	if !verified {
		result["error"] = fmt.Sprintf("login verification timed out after %dms", int(timeout))
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal login result: %w", err)
	}
	return string(out), nil
}

var (
	LoginHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
		handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			cfg := rodCtx.Config()
			p, err := parseLoginParams(request.GetArguments(), cfg)
			if err != nil {
				return toolErr("login", err)
			}

			// Step 1: Navigate to login page
			page, err := rodCtx.ControlledPage()
			if err != nil {
				return toolErr("login navigate", err)
			}
			if err := page.Navigate(p.loginURL); err != nil {
				return toolErr("login navigate", err)
			}
			if err := page.WaitLoad(); err != nil {
				return toolErr("login wait load", err)
			}
			waitDOMStable(page)

			// Fail fast if the login page returned an HTTP error (e.g. 404).
			checkURL := p.loginURL
			if pageInfo, infoErr := page.Info(); infoErr == nil && pageInfo.URL != "" {
				checkURL = pageInfo.URL
			}
			if err := checkNavigationStatus(rodCtx, checkURL); err != nil {
				return toolErr("login navigate", err)
			}

			// Step 2: Fill credentials and submit
			scope := "" // no scoping for standard login pages
			if p.triggerSelector != "" {
				if err := loginOpenTrigger(page, p.triggerSelector, p.formContainer); err != nil {
					return toolErr("login trigger", err)
				}
				scope = p.formContainer
			}

			if err := loginFillUsername(page, p.username, p.userSelector, scope, cfg.LoginUsernameSelectors); err != nil {
				return toolErr("login fill username", err)
			}
			if err := loginFillPassword(page, p.password, p.passSelector, scope); err != nil {
				return toolErr("login fill password", err)
			}
			if err := loginSubmit(page, p.submitSelector, scope); err != nil {
				return toolErr("login submit", err)
			}

			// Step 3: Verify success — pass ctx so cancellation is respected.
			verified := loginVerifySuccess(ctx, page, p.successSelector, p.successURL, p.timeout)

			// Step 4: Build result
			out, err := loginBuildResult(page, verified, p.timeout)
			if err != nil {
				return toolErr("login", err)
			}
			return mcp.NewToolResultText(out), nil
		}
		return rodCtx.Execute(handler, types.ToolHandlerCallOpts{WithSnapshot: true})
	}
)
