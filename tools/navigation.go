package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/aliwatters/rod-mcp/types"
	"github.com/aliwatters/rod-mcp/utils"
)

const (
	NavigationToolKey = "rod_navigate"
	GoBackToolKey     = "rod_go_back"
	GoForwardToolKey  = "rod_go_forward"
	ReloadToolKey     = "rod_reload"
)

var (
	Navigation = mcp.NewTool(NavigationToolKey,
		mcp.WithDescription("Navigate to a URL. Optionally include an accessibility snapshot or screenshot in the response to save round trips."),
		mcp.WithString("url", mcp.Description("URL to navigate to"), mcp.Required()),
		mcp.WithBoolean("snapshot", mcp.Description("Include accessibility snapshot in response (default: false). Only available in text mode.")),
		mcp.WithBoolean("screenshot", mcp.Description("Include screenshot in response (default: false)")),
	)
	GoBack = mcp.NewTool(GoBackToolKey,
		mcp.WithDescription("Go back in the browser history, go back to the previous page"),
	)
	GoForward = mcp.NewTool(GoForwardToolKey,
		mcp.WithDescription("Go forward in the browser history, go to the next page"),
	)
	Reload = mcp.NewTool(ReloadToolKey,
		mcp.WithDescription("Reload the current page"),
	)
)

// simplePageAction creates a handler for simple page navigation actions
// (go back, go forward, reload) that share the same pattern.
// A timeout is applied so the action cannot hang indefinitely (e.g. when a
// beforeunload dialog blocks navigation before our auto-accept fires).
func simplePageAction(rodCtx *types.Context, name string, action func(*rod.Page) error) server.ToolHandlerFunc {
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		page, err := rodCtx.ControlledPage()
		if err != nil {
			return toolErr(name, err)
		}
		navTimeout := rodCtx.Config().NavigationTimeout()
		timedPage := page.Timeout(navTimeout)
		if err = action(timedPage); err != nil {
			rodCtx.RecoverBrowserAfterError(err)
			return toolErr(name, err)
		}
		waitDOMStable(page, navTimeout)
		return mcp.NewToolResultText(fmt.Sprintf("%s successfully", name)), nil
	}
	return rodCtx.Execute(handler, types.ToolHandlerCallOpts{WithSnapshot: rodCtx.CurrentMode() == types.Text})
}

var (
	NavigationHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
		handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			url, err := getStringArg(args, "url")
			if err != nil {
				return toolErr("navigate", err)
			}
			if !utils.IsHttp(url) {
				return nil, toolError("navigate", fmt.Errorf("invalid URL: %s", url))
			}

			wantSnapshot := getOptionalBoolArg(args, "snapshot", false)
			wantScreenshot := getOptionalBoolArg(args, "screenshot", false)

			page, err := rodCtx.EnsurePage()
			if err != nil {
				return toolErr("navigate to "+url, err)
			}
			if page == nil {
				return toolErr("navigate to "+url, errors.New("no active page after EnsurePage"))
			}

			// Update headers for the target URL (applies domain-specific headers from config)
			if err := rodCtx.UpdateHeadersForURL(url); err != nil {
				slog.Warn("failed to update headers", "url", url, "err", err)
			}

			// Apply a timeout so navigation cannot hang indefinitely (e.g. if a
			// beforeunload dialog blocks, or the server never responds).
			navTimeout := rodCtx.Config().NavigationTimeout()
			timedPage := page.Timeout(navTimeout)
			if err = timedPage.Navigate(url); err != nil {
				rodCtx.RecoverBrowserAfterError(err)
				return toolErr("navigate to "+url, err)
			}
			waitDOMStable(page, navTimeout)

			// Fail fast if the page returned an HTTP error (e.g. 404, 500).
			// Use the final page URL to handle redirects (e.g. http→https).
			checkURL := url
			if info, infoErr := page.Info(); infoErr == nil && info.URL != "" {
				checkURL = info.URL
			}
			if err = checkNavigationStatus(rodCtx, checkURL); err != nil {
				return toolErr("navigate to "+url, err)
			}

			result := fmt.Sprintf("Navigated to %s", url)

			// Append inline snapshot if requested
			if wantSnapshot && rodCtx.CurrentMode() == types.Text {
				snapshot, snapErr := rodCtx.BuildSnapshot()
				if snapErr != nil {
					slog.Warn("navigate snapshot", "err", snapErr)
				} else {
					result += "\n\n" + snapshot
				}
			}

			// Return screenshot inline if requested
			if wantScreenshot {
				bin, shotErr := page.Screenshot(false, &proto.PageCaptureScreenshot{
					Format: proto.PageCaptureScreenshotFormatPng,
				})
				if shotErr != nil {
					slog.Warn("navigate screenshot", "err", shotErr)
				} else {
					encoded := base64.StdEncoding.EncodeToString(bin)
					return mcp.NewToolResultImage(result, encoded, "image/png"), nil
				}
			}

			return mcp.NewToolResultText(result), nil
		}
		// In text mode, auto-append snapshot (preserving existing behavior).
		// The explicit snapshot=true param adds it earlier in the response for
		// agents that want it bundled with navigation in non-text mode.
		return rodCtx.Execute(handler, types.ToolHandlerCallOpts{WithSnapshot: rodCtx.CurrentMode() == types.Text})
	}

	GoBackHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
		return simplePageAction(rodCtx, "Go back", (*rod.Page).NavigateBack)
	}

	GoForwardHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
		return simplePageAction(rodCtx, "Go forward", (*rod.Page).NavigateForward)
	}

	ReloadHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
		return simplePageAction(rodCtx, "Reload current page", (*rod.Page).Reload)
	}
)
