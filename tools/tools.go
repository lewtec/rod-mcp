package tools

import (
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/aliwatters/rod-mcp/types"
)

type ToolHandler = func(rodCtx *types.Context) server.ToolHandlerFunc

// Registration pairs an MCP tool definition with its handler factory.
// It is the single source of truth for tool registration — adding a new tool
// means adding one Registration entry, not editing both a slice and a map.
type Registration struct {
	Tool    mcp.Tool
	Handler ToolHandler
}

// Registrations is a named slice of Registration values with helper methods
// to derive the separate tool-definition slices and handler maps used by the
// MCP server.
type Registrations []Registration

// Tools returns all tool definitions as a slice.
func (rs Registrations) Tools() []mcp.Tool {
	tools := make([]mcp.Tool, len(rs))
	for i, r := range rs {
		tools[i] = r.Tool
	}
	return tools
}

// Handlers returns all handler factories keyed by tool name.
func (rs Registrations) Handlers() map[string]ToolHandler {
	m := make(map[string]ToolHandler, len(rs))
	for _, r := range rs {
		m[r.Tool.Name] = r.Handler
	}
	return m
}

// toolError logs the error and returns a formatted error for the MCP response.
func toolError(action string, err error) error {
	slog.Error("tool failed", "action", action, "err", err)
	return fmt.Errorf("Failed to %s: %s", action, err)
}

// CommonRegistrations is the single source of truth for all common (mode-independent)
// tool definitions and their handlers.
var CommonRegistrations = Registrations{
	{Configure, ConfigureHandler},
	{Navigation, NavigationHandler},
	{GoBack, GoBackHandler},
	{GoForward, GoForwardHandler},
	{Reload, ReloadHandler},
	{PressKey, PressKeyHandler},
	{Type, TypeHandler},
	{Screenshot, ScreenshotHandler},
	{Pdf, PdfHandler},
	{Evaluate, EvaluateHandler},
	{CloseBrowser, CloseBrowserHandler},
	{SetHeaders, SetHeadersHandler},
	{Resize, ResizeHandler},
	{HandleDialog, HandleDialogHandler},
	{TabNew, TabNewHandler},
	{TabList, TabListHandler},
	{TabSelect, TabSelectHandler},
	{TabClose, TabCloseHandler},
	{WaitFor, WaitForHandler},
	{ConsoleMessages, ConsoleMessagesHandler},
	{FileUpload, FileUploadHandler},
	{FillForm, FillFormHandler},
	{NetworkRequests, NetworkRequestsHandler},
	{Scroll, ScrollHandler},
	{HTML, HTMLHandler},
	{Coverage, CoverageHandler},
	{Cookies, CookiesHandler},
	{Storage, StorageHandler},
	{Permissions, PermissionsHandler},
	{Intercept, InterceptHandler},
	{ResponseBody, ResponseBodyHandler},
	{WebSocket, WebSocketHandler},
	{Performance, PerformanceHandler},
	{Drag, DragHandler},
	{A11yAudit, A11yAuditHandler},
	{PageInfo, PageInfoHandler},
	{PageStatus, PageStatusHandler},
	{AssertElement, AssertElementHandler},
	{FrameList, FrameListHandler},
	{Download, DownloadHandler},
	{Race, RaceHandler},
	{Geolocation, GeolocationHandler},
	{BasicAuth, BasicAuthHandler},
	{Batch, BatchHandler},
	{CompareScreenshots, CompareScreenshotsHandler},
	{Login, LoginHandler},
}

// TextRegistrations combines common registrations with text-mode (snapshot) tools.
var TextRegistrations = append(Registrations(nil), append(CommonRegistrations, SnapshotRegistrations...)...)

// VisionRegistrations combines common registrations with vision-mode tools.
var VisionRegistrations = append(Registrations(nil), append(CommonRegistrations, VisionToolRegistrations...)...)

// Derived slices and maps kept for backward compatibility with server.go and tests.
var (
	CommonTools        = CommonRegistrations.Tools()
	CommonToolHandlers = CommonRegistrations.Handlers()

	TextTools        = TextRegistrations.Tools()
	TextToolHandlers = TextRegistrations.Handlers()

	VisionTools            = VisionRegistrations.Tools()
	VisionCombinedHandlers = VisionRegistrations.Handlers()
)
