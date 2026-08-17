package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/aliwatters/rod-mcp/types"
)

const (
	DownloadToolKey = "rod_download"

	// defaultDownloadTimeoutMs is the default timeout for waiting for a download to complete.
	defaultDownloadTimeoutMs = 30000.0
)

var (
	Download = mcp.NewTool(DownloadToolKey,
		mcp.WithDescription("Click a trigger element and capture the resulting file download. Sets up download interception, clicks the element, waits for completion. Returns filename, size, and saved path."),
		mcp.WithString("trigger_selector", mcp.Description("CSS selector of the element to click to trigger the download")),
		mcp.WithString("trigger_ref", mcp.Description("Snapshot ref of the element to click to trigger the download")),
		mcp.WithNumber("timeout", mcp.Description("Max wait time in milliseconds (default: 30000)")),
	)
)

var (
	DownloadHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
		handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			browser, err := rodCtx.ControlledBrowser()
			if err != nil {
				return toolErr("download", err)
			}
			page, err := rodCtx.ControlledPage()
			if err != nil {
				return toolErr("download", err)
			}

			args := request.GetArguments()
			selector := getOptionalStringArg(args, "trigger_selector")
			ref := getOptionalStringArg(args, "trigger_ref")
			timeout := getOptionalFloatArg(args, "timeout", defaultDownloadTimeoutMs)

			if selector == "" && ref == "" {
				return toolErr("download", fmt.Errorf("one of 'trigger_selector' or 'trigger_ref' is required"))
			}

			// Resolve the output directory for saving downloads
			outDir, err := types.ResolveOutputDir(rodCtx.Config())
			if err != nil {
				return toolErr("download", err)
			}

			// Set up download listener BEFORE triggering the download
			wait := browser.Context(ctx).WaitDownload(outDir)

			// Resolve and click the trigger element
			var clickErr error
			if ref != "" {
				el, resolveErr := resolveByRef(rodCtx, ref)
				if resolveErr != nil {
					return toolErr("download trigger", resolveErr)
				}
				clickErr = el.Click(proto.InputMouseButtonLeft, 1)
			} else {
				el, resolveErr := resolveBySelector(page, selector)
				if resolveErr != nil {
					return toolErr("download trigger", resolveErr)
				}
				clickErr = el.Click(proto.InputMouseButtonLeft, 1)
			}
			if clickErr != nil {
				return toolErr("download click", clickErr)
			}

			// Wait for the download to complete with timeout
			type downloadResult struct {
				filename string
				guid     string
				url      string
			}
			done := make(chan downloadResult, 1)
			errCh := make(chan error, 1)

			go func() {
				info := wait()
				if info == nil {
					errCh <- fmt.Errorf("download cancelled or failed")
					return
				}
				done <- downloadResult{
					filename: info.SuggestedFilename,
					guid:     info.GUID,
					url:      info.URL,
				}
			}()

			var result downloadResult
			select {
			case result = <-done:
			case dlErr := <-errCh:
				return toolErr("download", dlErr)
			case <-time.After(time.Duration(timeout) * time.Millisecond):
				return toolErr("download", fmt.Errorf("timed out after %dms waiting for download", int(timeout)))
			}

			// The file is saved as the GUID in the download directory
			downloadedPath := filepath.Join(outDir, result.guid)

			// Rename to the suggested filename if available (sanitized to prevent path traversal)
			finalPath := downloadedPath
			if result.filename != "" {
				safeName := filepath.Base(result.filename)
				candidate := filepath.Clean(filepath.Join(outDir, safeName))
				if !strings.HasPrefix(candidate, filepath.Clean(outDir)+string(os.PathSeparator)) {
					slog.Warn(fmt.Sprintf("download: filename %q resolved outside output dir, using GUID", result.filename))
				} else if renameErr := os.Rename(downloadedPath, candidate); renameErr != nil {
					slog.Warn(fmt.Sprintf("download: rename to %q failed (keeping GUID name): %s", safeName, renameErr))
				} else {
					finalPath = candidate
				}
			}

			// Get file size
			stat, statErr := os.Stat(finalPath)
			var fileSize int64
			if statErr == nil {
				fileSize = stat.Size()
			}

			out, err := json.MarshalIndent(map[string]interface{}{
				"filename":     result.filename,
				"path":         finalPath,
				"size_bytes":   fileSize,
				"download_url": result.url,
			}, "", "  ")
			if err != nil {
				return toolErr("marshal download result", err)
			}
			return mcp.NewToolResultText(string(out)), nil
		}
		return rodCtx.Execute(handler, types.ToolHandlerCallOpts{WithSnapshot: false})
	}
)
