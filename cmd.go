package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliwatters/rod-mcp/types"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	DataDir         string
}

func RunCmd() (*SubCfg, error) {
	return parseCommandArgs(os.Args)
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "rod-mcp",
		Short:         "rod-mcp is a rod mcp server",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	fs := cmd.Flags()
	fs.StringP("config", "c", "", "optional config file")
	fs.String("data-dir", "", "base directory for log, profile, browser temp, and output (default: $XDG_CACHE_HOME/rod-mcp)")
	fs.String("cdp-endpoint", "", "control a running browser by CDP")
	fs.String("chrome-debug-port", "", "launch Chrome with --remote-debugging-port (e.g. 9222)")
	fs.String("user-data-dir", "", "Chrome profile directory (default: <data-dir>/profile)")
	fs.String("clone-domains", "", "comma-separated domains to clone cookies for")
	fs.Bool("no-clone", false, "use --user-data-dir directly instead of cloning")
	fs.Bool("clone-all", false, "clone the entire profile including passwords and extensions")
	fs.Bool("headless", false, "run the browser without a window")
	fs.Bool("gui", false, "force a visible browser window")
	fs.Bool("vision", false, "enable vision tools")
	fs.Bool("compact-snapshot", false, "filter non-interactive elements from snapshots")
	fs.String("output-dir", "", "screenshots and PDFs (default: <data-dir>/output)")
	fs.String("browser-bin-path", "", "path or PATH name of the Chromium-based browser")
	fs.String("browser-temp-dir", "", "ephemeral profiles when --user-data-dir is unset (default: <data-dir>/browser)")
	fs.String("log-file", "", "log file (default: <data-dir>/server.log)")
	fs.Bool("no-sandbox", false, "disable the Chrome sandbox")
	fs.Bool("omit-images", false, "omit inline base64 images from screenshot results")
	fs.Bool("no-banner", false, "")
	fs.Bool("hl", false, "")
	fs.Bool("cs", false, "")
	fs.Bool("vs", false, "")
	fs.String("cdp", "", "")
	_ = fs.MarkHidden("no-banner")
	_ = fs.MarkHidden("hl")
	_ = fs.MarkHidden("cs")
	_ = fs.MarkHidden("vs")
	_ = fs.MarkHidden("cdp")

	mustBind(viper.BindPFlags(fs))
	return cmd
}

func parseCommandArgs(args []string) (*SubCfg, error) {
	viper.Reset()
	cmd := newRootCmd()
	cmd.SetArgs(args[1:])
	if err := cmd.ParseFlags(args[1:]); err != nil {
		return nil, fmt.Errorf("run cmd: %w", err)
	}
	if err := initViper(cmd); err != nil {
		return nil, err
	}
	sub, err := subCfgFromCmd(cmd)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func initViper(cmd *cobra.Command) error {
	viper.SetEnvPrefix("ROD_MCP")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	cfgPath := viper.GetString("config")
	if cfgPath != "" {
		viper.SetConfigFile(cfgPath)
		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("read config %s: %w", cfgPath, err)
		}
	}

	dataDir := viper.GetString("data-dir")
	if dataDir == "" {
		dataDir = defaultDataDir()
		viper.Set("data-dir", dataDir)
	}
	setDerivedDefault(cmd, "log-file", filepath.Join(dataDir, "server.log"))
	setDerivedDefault(cmd, "output-dir", filepath.Join(dataDir, "output"))
	setDerivedDefault(cmd, "browser-temp-dir", filepath.Join(dataDir, "browser"))
	setDerivedDefault(cmd, "user-data-dir", filepath.Join(dataDir, "profile"))
	if !cmd.Flags().Changed("user-data-dir") && !cmd.Flags().Changed("no-clone") && !viper.GetBool("clone-all") {
		viper.Set("no-clone", true)
	}
	return nil
}

func setDerivedDefault(cmd *cobra.Command, name, value string) {
	if cmd.Flags().Changed(name) {
		return
	}
	if viper.GetString(name) != "" {
		return
	}
	viper.Set(name, value)
}

func defaultDataDir() string {
	if cache := os.Getenv("XDG_CACHE_HOME"); cache != "" {
		return filepath.Join(cache, "rod-mcp")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "rod-mcp")
	}
	return filepath.Join(home, ".cache", "rod-mcp")
}

func subCfgFromCmd(cmd *cobra.Command) (*SubCfg, error) {
	headless := viper.GetBool("headless") || viper.GetBool("hl")
	gui := viper.GetBool("gui")
	headlessSet := cmd.Flags().Changed("headless") || cmd.Flags().Changed("hl") || cmd.Flags().Changed("gui")
	if gui {
		if (cmd.Flags().Changed("headless") || cmd.Flags().Changed("hl")) && headless {
			return nil, fmt.Errorf("--gui cannot be combined with --headless")
		}
		headless = false
		headlessSet = true
	}

	mode := types.Mode("")
	if viper.GetBool("vision") || viper.GetBool("vs") {
		mode = types.Vision
	}

	cdp := viper.GetString("cdp-endpoint")
	if cdp == "" {
		cdp = viper.GetString("cdp")
	}

	return &SubCfg{
		Headless:        headless,
		HeadlessSet:     headlessSet,
		ConfigPath:      viper.GetString("config"),
		Mode:            mode,
		CDPEndpoint:     cdp,
		ChromeDebugPort: viper.GetString("chrome-debug-port"),
		UserDataDir:     viper.GetString("user-data-dir"),
		CloneDomains:    viper.GetString("clone-domains"),
		NoClone:         viper.GetBool("no-clone"),
		CloneAll:        viper.GetBool("clone-all"),
		CompactSnapshot: viper.GetBool("compact-snapshot") || viper.GetBool("cs"),
		OutputDir:       viper.GetString("output-dir"),
		OmitImages:      viper.GetBool("omit-images"),
		BrowserBinPath:  viper.GetString("browser-bin-path"),
		BrowserTempDir:  viper.GetString("browser-temp-dir"),
		LogFile:         viper.GetString("log-file"),
		NoSandbox:       viper.GetBool("no-sandbox"),
		DataDir:         viper.GetString("data-dir"),
	}, nil
}

func mustBind(err error) {
	if err != nil {
		panic(err)
	}
}

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
