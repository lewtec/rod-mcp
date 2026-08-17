package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/aliwatters/rod-mcp/types"
	"github.com/urfave/cli/v2"
)

type SubCfg struct {
	Headless        bool
	HeadlessSet     bool
	ConfigPath      string
	Mode            types.Mode
	CDPEndpoint     string
	ChromeDebugPort string
	UserDataDir     string
	CloneDomains    string
	NoClone         bool
	CloneAll        bool
	CompactSnapshot bool
	OutputDir       string
	OmitImages      bool
	BrowserBinPath  string
	BrowserTempDir  string
	LogFile         string
	NoSandbox       bool
}

func RunCmd() (*SubCfg, error) {
	return parseCommandArgs(os.Args)
}

func parseCommandArgs(args []string) (*SubCfg, error) {
	subConfig := SubCfg{}
	cmd := &cli.App{
		Name:        "Rod MCP Server",
		Description: "Model Context Protocol Server of Rod",
		Usage:       "rod-mcp is a rod mcp server",
		Version:     Version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				Usage:       "path to config file (default: $XDG_CONFIG_HOME/rod-mcp/rod-mcp.yaml, or ~/.config/rod-mcp/rod-mcp.yaml)",
				Destination: &subConfig.ConfigPath,
			}, &cli.StringFlag{
				Name:        "cdp-endpoint",
				Aliases:     []string{"cdp"},
				Usage:       "use to control running browser by cdp",
				Destination: &subConfig.CDPEndpoint,
			}, &cli.StringFlag{
				Name:        "chrome-debug-port",
				Usage:       "launch Chrome with --remote-debugging-port on the given port (e.g. 9222)",
				Destination: &subConfig.ChromeDebugPort,
			}, &cli.StringFlag{
				Name:        "user-data-dir",
				Usage:       "Chrome profile directory to clone from (inherits cookies, sessions, auth state)",
				Destination: &subConfig.UserDataDir,
			}, &cli.StringFlag{
				Name:        "clone-domains",
				Usage:       "comma-separated domains to clone cookies for (e.g. \"localhost,*.clerk.dev\")",
				Destination: &subConfig.CloneDomains,
			},
			&cli.BoolFlag{
				Name:        "no-clone",
				Usage:       "use the profile directory directly instead of cloning (locks your main Chrome)",
				Destination: &subConfig.NoClone,
			},
			&cli.BoolFlag{
				Name:  "clone-all",
				Usage: "clone the ENTIRE profile including extensions, history, passwords (slow, use with caution)",
			},
			&cli.BoolFlag{
				Name:        "headless",
				Aliases:     []string{"hl"},
				Value:       false,
				Usage:       "use to enable headless,if false browser will shown window",
				Destination: &subConfig.Headless,
			},
			&cli.BoolFlag{
				Name:  "gui",
				Usage: "force a visible browser window by setting headless=false",
			},
			&cli.BoolFlag{
				Name:    "vision",
				Aliases: []string{"vs"},
				Usage:   "use to support vision LLM will load  vision tools",
			},
			&cli.BoolFlag{
				Name:    "compact-snapshot",
				Aliases: []string{"cs"},
				Usage:   "enable compact snapshot mode to reduce token usage by filtering non-interactive elements",
			},
			&cli.StringFlag{
				Name:        "output-dir",
				Usage:       "directory for saving screenshots and PDFs (default: OS temp dir)",
				Destination: &subConfig.OutputDir,
			},
			&cli.StringFlag{
				Name:        "browser-bin-path",
				Usage:       "path or PATH name of the Chromium-based browser to launch",
				Destination: &subConfig.BrowserBinPath,
			},
			&cli.StringFlag{
				Name:        "browser-temp-dir",
				Usage:       "directory for ephemeral browser profiles when --user-data-dir is not set",
				Destination: &subConfig.BrowserTempDir,
			},
			&cli.StringFlag{
				Name:        "log-file",
				Usage:       "write logs to this file (default: stderr only)",
				Destination: &subConfig.LogFile,
			},
			&cli.BoolFlag{
				Name:        "no-sandbox",
				Usage:       "disable the Chrome sandbox",
				Destination: &subConfig.NoSandbox,
			},
			&cli.BoolFlag{
				Name:  "omit-images",
				Usage: "omit inline base64 image data from screenshot results (saves tokens)",
			},
			&cli.BoolFlag{
				Name:   "no-banner",
				Hidden: true,
			},
		},
		Action: func(c *cli.Context) error {
			if c.IsSet("headless") {
				subConfig.Headless = c.Bool("headless")
				subConfig.HeadlessSet = true
			}
			if c.Bool("gui") {
				if subConfig.HeadlessSet && subConfig.Headless {
					return fmt.Errorf("--gui cannot be combined with --headless")
				}
				subConfig.Headless = false
				subConfig.HeadlessSet = true
			}

			if c.Bool("vision") {
				subConfig.Mode = types.Vision
			}
			if c.Bool("compact-snapshot") {
				subConfig.CompactSnapshot = true
			}
			if c.Bool("omit-images") {
				subConfig.OmitImages = true
			}
			if c.Bool("clone-all") {
				subConfig.CloneAll = true
			}
			return nil
		},
	}
	err := cmd.Run(args)
	if err != nil {
		return nil, fmt.Errorf("run cmd: %w", err)
	}
	return &subConfig, nil
}

// parseCloneDomains splits a comma-separated domain string into a slice.
func parseCloneDomains(s string) []string {
	if s == "" {
		return nil
	}
	var domains []string
	for _, d := range strings.Split(s, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			domains = append(domains, d)
		}
	}
	return domains
}
