package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/aliwatters/rod-mcp/types"
)

const (
	BasicAuthToolKey = "rod_basic_auth"
)

var (
	BasicAuth = mcp.NewTool(BasicAuthToolKey,
		mcp.WithDescription("Handle HTTP basic authentication for password-protected pages. Call before navigating to the protected URL. The credentials apply to the next authentication challenge."),
		mcp.WithString("username", mcp.Description("Username for HTTP basic auth"), mcp.Required()),
		mcp.WithString("password", mcp.Description("Password for HTTP basic auth"), mcp.Required()),
	)
)

var (
	BasicAuthHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
		handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			browser, err := rodCtx.ControlledBrowser()
			if err != nil {
				return toolErr("basic auth", err)
			}

			args := request.GetArguments()
			username, err := getStringArg(args, "username")
			if err != nil {
				return toolErr("basic auth", err)
			}
			password, err := getStringArg(args, "password")
			if err != nil {
				return toolErr("basic auth", err)
			}

			// HandleAuth sets up interception for the next auth challenge.
			// It must be called in a goroutine because it blocks until the auth dialog appears.
			wait := browser.HandleAuth(username, password)
			go func() {
				if authErr := wait(); authErr != nil {
					slog.Error(fmt.Sprintf("basic auth handler failed: %s", authErr))
				}
			}()

			return mcp.NewToolResultText(fmt.Sprintf("Basic auth configured for user %q. Navigate to the protected URL now.", username)), nil
		}
		return rodCtx.Execute(handler, types.ToolHandlerCallOpts{WithSnapshot: false})
	}
)
