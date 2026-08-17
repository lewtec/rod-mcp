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
)

const (
	CoverageToolKey = "rod_coverage"
)

var (
	Coverage = mcp.NewTool(CoverageToolKey,
		mcp.WithDescription("Collect CSS and JavaScript code coverage. Start collection, get a delta report (coverage since last report or since start), or stop collection."),
		mcp.WithString("action", mcp.Description("Action to perform: start, report, stop"), mcp.Required(), mcp.Enum("start", "report", "stop")),
		mcp.WithString("type", mcp.Description("Coverage type: js, css, or all (default: all)"), mcp.Enum("js", "css", "all")),
	)
)

var (
	CoverageHandler = func(rodCtx *types.Context) server.ToolHandlerFunc {
		handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			page, err := rodCtx.ControlledPage()
			if err != nil {
				return toolErr("coverage", err)
			}

			action, err := getStringArg(request.GetArguments(), "action")
			if err != nil {
				return toolErr("coverage", err)
			}
			coverType := getOptionalStringArg(request.GetArguments(), "type")
			if coverType == "" {
				coverType = "all"
			}

			doJS := coverType == "js" || coverType == "all"
			doCSS := coverType == "css" || coverType == "all"

			switch action {
			case "start":
				return coverageStart(page, doJS, doCSS)
			case "report":
				return coverageReport(page, doJS, doCSS)
			case "stop":
				return coverageStop(page, doJS, doCSS)
			default:
				return nil, fmt.Errorf("invalid action %q: must be start, report, or stop", action)
			}
		}
		return rodCtx.Execute(handler, types.ToolHandlerCallOpts{WithSnapshot: false})
	}
)

func coverageStart(page *rod.Page, doJS, doCSS bool) (*mcp.CallToolResult, error) {
	var started []string
	if doJS {
		err := proto.ProfilerEnable{}.Call(page)
		if err != nil {
			return toolErr("enable JS profiler", err)
		}
		_, err = proto.ProfilerStartPreciseCoverage{
			CallCount: true,
			Detailed:  true,
		}.Call(page)
		if err != nil {
			return toolErr("start JS coverage", err)
		}
		started = append(started, "JS")
	}
	if doCSS {
		if err := (proto.DOMEnable{}).Call(page); err != nil {
			return toolErr("enable DOM domain", err)
		}
		if err := (proto.CSSEnable{}).Call(page); err != nil {
			return toolErr("enable CSS domain", err)
		}
		if err := (proto.CSSStartRuleUsageTracking{}).Call(page); err != nil {
			return toolErr("start CSS coverage", err)
		}
		started = append(started, "CSS")
	}
	return mcp.NewToolResultText(fmt.Sprintf("Coverage collection started: %s", strings.Join(started, ", "))), nil
}

// formatCoverageEntry formats a single coverage entry as a markdown list item.
func formatCoverageEntry(label string, used, total int) string {
	pct := float64(used) / float64(total) * 100
	return fmt.Sprintf("- %s: %.1f%% (%d/%d bytes)\n", label, pct, used, total)
}

// formatCoverageTotal formats the grand total line for a coverage section.
func formatCoverageTotal(prefix string, used, total int) string {
	pct := float64(used) / float64(total) * 100
	return fmt.Sprintf("\n%s Total: %.1f%% (%d/%d bytes)\n", prefix, pct, used, total)
}

func coverageReport(page *rod.Page, doJS, doCSS bool) (*mcp.CallToolResult, error) {
	var result strings.Builder

	if doJS {
		section, err := coverageReportJS(page)
		if err != nil {
			return nil, err
		}
		result.WriteString(section)
	}

	if doCSS {
		section, err := coverageReportCSS(page)
		if err != nil {
			return nil, err
		}
		result.WriteString(section)
	}

	return mcp.NewToolResultText(result.String()), nil
}

// coverageReportJS collects the current JS precise-coverage snapshot and returns
// a Markdown section summarising per-script and overall byte usage.
func coverageReportJS(page *rod.Page) (string, error) {
	resp, err := proto.ProfilerTakePreciseCoverage{}.Call(page)
	if err != nil {
		return "", fmt.Errorf("get JS coverage: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("## JavaScript Coverage\n\n")

	if len(resp.Result) == 0 {
		sb.WriteString("No JS coverage data (is collection started?)\n\n")
		return sb.String(), nil
	}

	var totalBytes, usedBytes int
	for _, script := range resp.Result {
		if script.URL == "" {
			continue
		}
		scriptTotal, scriptUsed := summariseJSScript(script)
		if scriptTotal > 0 {
			sb.WriteString(formatCoverageEntry(script.URL, scriptUsed, scriptTotal))
			totalBytes += scriptTotal
			usedBytes += scriptUsed
		}
	}
	if totalBytes > 0 {
		sb.WriteString(formatCoverageTotal("JS", usedBytes, totalBytes))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// summariseJSScript calculates total and used byte counts for a single script
// by iterating over its function ranges.
func summariseJSScript(script *proto.ProfilerScriptCoverage) (total, used int) {
	for _, fn := range script.Functions {
		for _, r := range fn.Ranges {
			size := r.EndOffset - r.StartOffset
			total += size
			if r.Count > 0 {
				used += size
			}
		}
	}
	return total, used
}

// coverageReportCSS collects the CSS coverage delta and returns a Markdown section
// summarising per-stylesheet and overall byte usage.
func coverageReportCSS(page *rod.Page) (string, error) {
	resp, err := proto.CSSTakeCoverageDelta{}.Call(page)
	if err != nil {
		return "", fmt.Errorf("get CSS coverage: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("## CSS Coverage\n\n")

	if len(resp.Coverage) == 0 {
		sb.WriteString("No CSS coverage data (is collection started?)\n\n")
		return sb.String(), nil
	}

	type sheetStats struct {
		total int
		used  int
	}
	sheets := make(map[string]*sheetStats)
	for _, rule := range resp.Coverage {
		id := string(rule.StyleSheetID)
		s, ok := sheets[id]
		if !ok {
			s = &sheetStats{}
			sheets[id] = s
		}
		size := int(rule.EndOffset - rule.StartOffset)
		s.total += size
		if rule.Used {
			s.used += size
		}
	}

	var totalBytes, usedBytes int
	for id, s := range sheets {
		if s.total > 0 {
			sb.WriteString(formatCoverageEntry("stylesheet "+id, s.used, s.total))
			totalBytes += s.total
			usedBytes += s.used
		}
	}
	if totalBytes > 0 {
		sb.WriteString(formatCoverageTotal("CSS", usedBytes, totalBytes))
	}
	return sb.String(), nil
}

func coverageStop(page *rod.Page, doJS, doCSS bool) (*mcp.CallToolResult, error) {
	var stopped []string
	if doJS {
		if err := (proto.ProfilerStopPreciseCoverage{}).Call(page); err != nil {
			slog.Warn("stop profiler coverage", "err", err)
		}
		if err := (proto.ProfilerDisable{}).Call(page); err != nil {
			slog.Warn("disable profiler", "err", err)
		}
		stopped = append(stopped, "JS")
	}
	if doCSS {
		if _, err := (proto.CSSStopRuleUsageTracking{}).Call(page); err != nil {
			slog.Warn("stop CSS rule tracking", "err", err)
		}
		if err := (proto.CSSDisable{}).Call(page); err != nil {
			slog.Warn("disable CSS domain", "err", err)
		}
		stopped = append(stopped, "CSS")
	}
	return mcp.NewToolResultText(fmt.Sprintf("Coverage collection stopped: %s", strings.Join(stopped, ", "))), nil
}
