package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/aliwatters/rod-mcp/types"
	"github.com/aliwatters/rod-mcp/utils"
)

const (
	InterceptToolKey = "rod_intercept"
)

var (
	Intercept = mcp.NewTool(InterceptToolKey,
		mcp.WithDescription("Intercept network requests to mock responses, block requests, or simulate errors. Enable interception first, then add rules. Each intercepted request is matched against rules in order."),
		mcp.WithString("action", mcp.Description("Action to perform"), mcp.Required(), mcp.Enum("enable", "mock", "block", "fail", "disable", "list")),
		mcp.WithString("urlPattern", mcp.Description("URL pattern to match (wildcards: * = zero or more, ? = exactly one). Required for mock, block, fail.")),
		mcp.WithNumber("status", mcp.Description(fmt.Sprintf("HTTP status code for mock response (default: %d)", defaultHTTPStatus))),
		mcp.WithObject("headers", mcp.Description("Response headers for mock response as key-value pairs")),
		mcp.WithString("body", mcp.Description("Response body for mock response")),
		mcp.WithString("errorReason", mcp.Description("Error reason for fail action"), mcp.Enum("Failed", "Aborted", "TimedOut", "AccessDenied", "ConnectionClosed", "ConnectionReset", "ConnectionRefused", "ConnectionAborted", "ConnectionFailed", "NameNotResolved", "InternetDisconnected", "AddressUnreachable", "BlockedByClient", "BlockedByResponse")),
	)
)

var (
	InterceptHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
		handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			page, err := rodCtx.ControlledPage()
			if err != nil {
				return toolErr("intercept", err)
			}

			action, err := getStringArg(request.GetArguments(), "action")
			if err != nil {
				return toolErr("intercept", err)
			}

			switch action {
			case "enable":
				return interceptEnable(rodCtx, page)
			case "mock":
				return interceptMock(rodCtx, request.GetArguments())
			case "block":
				return interceptBlock(rodCtx, request.GetArguments())
			case "fail":
				return interceptFail(rodCtx, request.GetArguments())
			case "list":
				return interceptList(rodCtx)
			case "disable":
				return interceptDisable(rodCtx, page)
			default:
				return nil, fmt.Errorf("invalid action %q: must be enable, mock, block, fail, list, or disable", action)
			}
		}
		return rodCtx.Execute(handler, types.ToolHandlerCallOpts{WithSnapshot: false})
	}
)

func interceptEnable(rodCtx *types.Context, page *rod.Page) (*mcp.CallToolResult, error) {
	err := proto.FetchEnable{
		Patterns: []*proto.FetchRequestPattern{
			{URLPattern: "*"},
		},
	}.Call(page)
	if err != nil {
		return toolErr("enable request interception", err)
	}

	rodCtx.SetInterceptEnabled(true)

	// Use a cancelable page copy so the goroutine can be stopped when
	// interception is disabled or the page is closed.
	cancelPage, cancelFn := page.WithCancel()
	rodCtx.SetInterceptCancel(cancelFn)

	go cancelPage.EachEvent(func(e *proto.FetchRequestPaused) {
		rules := rodCtx.InterceptRules()

		for _, rule := range rules {
			if matchURLPattern(e.Request.URL, rule.URLPattern) {
				switch rule.Action {
				case "mock":
					if err := (proto.FetchFulfillRequest{
						RequestID:       e.RequestID,
						ResponseCode:    rule.Status,
						ResponseHeaders: rule.Headers,
						Body:            rule.Body,
					}).Call(cancelPage); err != nil {
						slog.Warn("fulfill request", "id", e.RequestID, "err", err)
					}
					return
				case "block", "fail":
					reason := rule.ErrorReason
					if reason == "" {
						reason = proto.NetworkErrorReasonBlockedByClient
					}
					if err := (proto.FetchFailRequest{
						RequestID:   e.RequestID,
						ErrorReason: reason,
					}).Call(cancelPage); err != nil {
						slog.Warn("fail request", "id", e.RequestID, "err", err)
					}
					return
				}
			}
		}
		if err := (proto.FetchContinueRequest{
			RequestID: e.RequestID,
		}).Call(cancelPage); err != nil {
			slog.Warn("continue request", "id", e.RequestID, "err", err)
		}
	})()

	return mcp.NewToolResultText("Request interception enabled"), nil
}

func interceptMock(rodCtx *types.Context, args map[string]any) (*mcp.CallToolResult, error) {
	if !rodCtx.InterceptEnabled() {
		return toolErr("intercept mock", fmt.Errorf("interception not enabled, call with action=enable first"))
	}
	urlPattern, err := getStringArg(args, "urlPattern")
	if err != nil {
		return toolErr("intercept mock", err)
	}
	status := int(getOptionalFloatArg(args, "status", float64(defaultHTTPStatus)))
	body := getOptionalStringArg(args, "body")

	var headers []*proto.FetchHeaderEntry
	if h, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			headers = append(headers, &proto.FetchHeaderEntry{
				Name:  k,
				Value: fmt.Sprintf("%v", v),
			})
		}
	}
	if len(headers) == 0 {
		headers = []*proto.FetchHeaderEntry{
			{Name: "Content-Type", Value: "application/json"},
		}
	}

	rodCtx.AddInterceptRule(types.InterceptRule{
		URLPattern: urlPattern,
		Action:     "mock",
		Status:     status,
		Headers:    headers,
		Body:       []byte(body),
	})

	return mcp.NewToolResultText(fmt.Sprintf("Mock rule added: %s → %d", urlPattern, status)), nil
}

func interceptBlock(rodCtx *types.Context, args map[string]any) (*mcp.CallToolResult, error) {
	if !rodCtx.InterceptEnabled() {
		return toolErr("intercept block", fmt.Errorf("interception not enabled, call with action=enable first"))
	}
	urlPattern, err := getStringArg(args, "urlPattern")
	if err != nil {
		return toolErr("intercept block", err)
	}

	rodCtx.AddInterceptRule(types.InterceptRule{
		URLPattern:  urlPattern,
		Action:      "block",
		ErrorReason: proto.NetworkErrorReasonBlockedByClient,
	})

	return mcp.NewToolResultText(fmt.Sprintf("Block rule added: %s", urlPattern)), nil
}

func interceptFail(rodCtx *types.Context, args map[string]any) (*mcp.CallToolResult, error) {
	if !rodCtx.InterceptEnabled() {
		return toolErr("intercept fail", fmt.Errorf("interception not enabled, call with action=enable first"))
	}
	urlPattern, err := getStringArg(args, "urlPattern")
	if err != nil {
		return toolErr("intercept fail", err)
	}
	reason := getOptionalStringArg(args, "errorReason")
	if reason == "" {
		reason = "Failed"
	}

	rodCtx.AddInterceptRule(types.InterceptRule{
		URLPattern:  urlPattern,
		Action:      "fail",
		ErrorReason: proto.NetworkErrorReason(reason),
	})

	return mcp.NewToolResultText(fmt.Sprintf("Fail rule added: %s → %s", urlPattern, reason)), nil
}

func interceptList(rodCtx *types.Context) (*mcp.CallToolResult, error) {
	rules := rodCtx.InterceptRules()
	enabled := rodCtx.InterceptEnabled()

	if !enabled {
		return mcp.NewToolResultText("Interception is not enabled"), nil
	}
	if len(rules) == 0 {
		return mcp.NewToolResultText("Interception enabled, no rules configured"), nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Interception rules (%d):\n", len(rules)))
	for i, r := range rules {
		switch r.Action {
		case "mock":
			result.WriteString(fmt.Sprintf("  %d. MOCK %s → %d\n", i+1, r.URLPattern, r.Status))
		case "block":
			result.WriteString(fmt.Sprintf("  %d. BLOCK %s\n", i+1, r.URLPattern))
		case "fail":
			result.WriteString(fmt.Sprintf("  %d. FAIL %s → %s\n", i+1, r.URLPattern, r.ErrorReason))
		}
	}
	return mcp.NewToolResultText(result.String()), nil
}

func interceptDisable(rodCtx *types.Context, page *rod.Page) (*mcp.CallToolResult, error) {
	err := proto.FetchDisable{}.Call(page)
	if err != nil {
		return toolErr("disable request interception", err)
	}
	rodCtx.SetInterceptEnabled(false)
	return mcp.NewToolResultText("Request interception disabled and rules cleared"), nil
}

// matchURLPattern matches a URL against a simple wildcard pattern.
func matchURLPattern(url, pattern string) bool {
	return utils.MatchWildcard(url, pattern)
}
