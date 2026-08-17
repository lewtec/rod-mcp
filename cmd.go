package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliwatters/rod-mcp/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func RunCmd() (*types.Config, error) {
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
	fs.String("data-dir", "", "base directory for profile, browser temp, and output (default: user cache/rod-mcp)")
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
	fs.BoolP("verbose", "v", false, "debug logs on stderr")
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

	bindFlags(fs)
	return cmd
}

func bindFlags(fs *pflag.FlagSet) {
	mustBind(viper.BindPFlag("dataDir", fs.Lookup("data-dir")))
	mustBind(viper.BindPFlag("cdpEndpoint", fs.Lookup("cdp-endpoint")))
	mustBind(viper.BindPFlag("chromeDebugPort", fs.Lookup("chrome-debug-port")))
	mustBind(viper.BindPFlag("userDataDir", fs.Lookup("user-data-dir")))
	mustBind(viper.BindPFlag("cloneDomains", fs.Lookup("clone-domains")))
	mustBind(viper.BindPFlag("noClone", fs.Lookup("no-clone")))
	mustBind(viper.BindPFlag("cloneAll", fs.Lookup("clone-all")))
	mustBind(viper.BindPFlag("headless", fs.Lookup("headless")))
	mustBind(viper.BindPFlag("compactSnapshot", fs.Lookup("compact-snapshot")))
	mustBind(viper.BindPFlag("outputDir", fs.Lookup("output-dir")))
	mustBind(viper.BindPFlag("browserBinPath", fs.Lookup("browser-bin-path")))
	mustBind(viper.BindPFlag("browserTempDir", fs.Lookup("browser-temp-dir")))
	mustBind(viper.BindPFlag("verbose", fs.Lookup("verbose")))
	mustBind(viper.BindPFlag("noSandbox", fs.Lookup("no-sandbox")))
}

func parseCommandArgs(args []string) (*types.Config, error) {
	viper.Reset()
	cmd := newRootCmd()
	cmd.SetArgs(args[1:])
	if err := cmd.ParseFlags(args[1:]); err != nil {
		return nil, fmt.Errorf("run cmd: %w", err)
	}
	if err := initViper(cmd); err != nil {
		return nil, err
	}
	if err := resolveAliases(cmd); err != nil {
		return nil, err
	}
	derivePaths(cmd)

	cfg := types.DefaultConfig
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if !headlessFlagSet(cmd) {
		cfg.Headless = types.DefaultConfig.Headless
	}
	cfg.ServerVersion = Version
	return &cfg, nil
}

func initViper(cmd *cobra.Command) error {
	viper.SetEnvPrefix("ROD_MCP")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if path, _ := cmd.Flags().GetString("config"); path != "" {
		viper.SetConfigFile(path)
		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("read config %s: %w", path, err)
		}
	}
	return nil
}

func resolveAliases(cmd *cobra.Command) error {
	headless, _ := cmd.Flags().GetBool("headless")
	hl, _ := cmd.Flags().GetBool("hl")
	gui, _ := cmd.Flags().GetBool("gui")
	if (cmd.Flags().Changed("headless") || cmd.Flags().Changed("hl")) && gui && (headless || hl) {
		return fmt.Errorf("--gui cannot be combined with --headless")
	}
	if gui {
		viper.Set("headless", false)
	} else if cmd.Flags().Changed("hl") && !cmd.Flags().Changed("headless") {
		viper.Set("headless", hl)
	}

	if flagBool(cmd, "vision") || flagBool(cmd, "vs") {
		viper.Set("mode", string(types.Vision))
	}
	if flagBool(cmd, "cs") && !cmd.Flags().Changed("compact-snapshot") {
		viper.Set("compactSnapshot", true)
	}
	if cdp := flagString(cmd, "cdp"); cdp != "" && !cmd.Flags().Changed("cdp-endpoint") {
		viper.Set("cdpEndpoint", cdp)
	}
	if flagBool(cmd, "omit-images") {
		viper.Set("imageResponses", string(types.ImageResponsesOmit))
	}
	if raw := viper.Get("cloneDomains"); raw != nil {
		if s, ok := raw.(string); ok {
			viper.Set("cloneDomains", parseCloneDomains(s))
		}
	}
	return nil
}

func derivePaths(cmd *cobra.Command) {
	dataDir := viper.GetString("dataDir")
	if dataDir == "" {
		dataDir = defaultDataDir()
		viper.Set("dataDir", dataDir)
	}
	setDerivedDefault(cmd, "user-data-dir", "userDataDir", filepath.Join(dataDir, "profile"))
	setDerivedDefault(cmd, "browser-temp-dir", "browserTempDir", filepath.Join(dataDir, "browser"))
	setDerivedDefault(cmd, "output-dir", "outputDir", filepath.Join(dataDir, "output"))
	if !cmd.Flags().Changed("user-data-dir") && !cmd.Flags().Changed("no-clone") && !viper.GetBool("cloneAll") {
		viper.Set("noClone", true)
	}
}

func setDerivedDefault(cmd *cobra.Command, flagName, viperKey, value string) {
	if cmd.Flags().Changed(flagName) {
		return
	}
	if viper.GetString(viperKey) != "" {
		return
	}
	viper.Set(viperKey, value)
}

func defaultDataDir() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "rod-mcp")
	}
	return filepath.Join(cache, "rod-mcp")
}

func headlessFlagSet(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("headless") || cmd.Flags().Changed("hl") || cmd.Flags().Changed("gui")
}

func flagBool(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

func flagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
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
