package types

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aliwatters/rod-mcp/utils"
	"gopkg.in/yaml.v3"
)

const ConfigName = "rod-mcp.yaml"

// DefaultConfigPath returns the canonical config path under XDG_CONFIG_HOME
// (or ~/.config when XDG_CONFIG_HOME is not set). It never references the
// current working directory, so rod-mcp can be invoked from any directory
// without polluting the caller's working tree.
//
// XDG_CONFIG_HOME is only used when it is set to an absolute path; relative or
// tilde-prefixed values are ignored and the home-directory fallback is used
// instead (a non-absolute XDG_CONFIG_HOME would reintroduce cwd-relative paths).
func DefaultConfigPath() string {
	configHome := xdgConfigHome()
	return filepath.Join(configHome, "rod-mcp", ConfigName)
}

// xdgConfigHome returns the config base directory to use, resolving the XDG
// spec (XDG_CONFIG_HOME → $HOME/.config → os.TempDir fallback). It always
// returns an absolute path.
func xdgConfigHome() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		// Only accept absolute paths; discard relative or tilde-prefixed values
		// that would make the config path cwd-relative.
		if filepath.IsAbs(xdg) {
			return xdg
		}
		slog.Warn(fmt.Sprintf("XDG_CONFIG_HOME=%q is not an absolute path; ignoring and using ~/.config instead", xdg))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// os.UserHomeDir should never fail in normal operation; use os.TempDir
		// so we always return an absolute path and never pollute the cwd.
		slog.Warn(fmt.Sprintf("could not determine home directory (%v); using os.TempDir for config", err))
		return filepath.Join(os.TempDir(), ".config")
	}
	return filepath.Join(home, ".config")
}

// ImageResponsesMode controls whether inline base64 image data is included in tool results.
type ImageResponsesMode string

const (
	// ImageResponsesAllow includes inline base64 image data alongside the saved file path.
	ImageResponsesAllow ImageResponsesMode = "allow"
	// ImageResponsesOmit returns only the file path, omitting inline base64 data to save tokens.
	ImageResponsesOmit ImageResponsesMode = "omit"
)

type Config struct {
	DataDir             string            `yaml:"dataDir" json:"dataDir" mapstructure:"dataDir"`
	Mode                Mode              `yaml:"mode" json:"mode" mapstructure:"mode"`
	CDPEndpoint         string            `yaml:"cdpEndpoint" json:"cdpEndpoint" mapstructure:"cdpEndpoint"`
	ChromeDebugPort     string            `yaml:"chromeDebugPort" json:"chromeDebugPort" mapstructure:"chromeDebugPort"`
	LaunchTimeoutMs     int               `yaml:"launchTimeoutMs" json:"launchTimeoutMs" mapstructure:"launchTimeoutMs"`
	NavigationTimeoutMs int               `yaml:"navigationTimeoutMs" json:"navigationTimeoutMs" mapstructure:"navigationTimeoutMs"`
	UserDataDir         string            `yaml:"userDataDir" json:"userDataDir" mapstructure:"userDataDir"`
	CloneDomains        []string          `yaml:"cloneDomains" json:"cloneDomains" mapstructure:"cloneDomains"`
	NoClone             bool              `yaml:"noClone" json:"noClone" mapstructure:"noClone"`
	CloneAll            bool              `yaml:"cloneAll" json:"cloneAll" mapstructure:"cloneAll"`
	ServerName          string            `yaml:"serverName" json:"serverName" mapstructure:"serverName"`
	ServerVersion       string            `yaml:"-" json:"-" mapstructure:"-"`
	BrowserBinPath      string            `yaml:"browserBinPath" json:"browserBinPath" mapstructure:"browserBinPath"`
	Headless            bool              `yaml:"headless" json:"headless" mapstructure:"headless"`
	BrowserTempDir      string            `yaml:"browserTempDir" json:"browserTempDir" mapstructure:"browserTempDir"`
	NoSandbox           bool              `yaml:"noSandbox" json:"noSandbox" mapstructure:"noSandbox"`
	Proxy               string            `yaml:"proxy" json:"proxy" mapstructure:"proxy"`
	Verbose             bool              `yaml:"verbose" json:"verbose" mapstructure:"verbose"`
	ExtraHTTPHeaders    map[string]string `yaml:"extraHTTPHeaders" json:"extraHTTPHeaders" mapstructure:"extraHTTPHeaders"`
	CompactSnapshot     bool              `yaml:"compactSnapshot" json:"compactSnapshot" mapstructure:"compactSnapshot"`
	// DomainHeaders maps domain patterns to headers that should be injected for matching URLs.
	// Patterns support wildcards: "*.example.com" matches "www.example.com", "api.example.com", etc.
	// Headers from matching patterns are merged with ExtraHTTPHeaders.
	DomainHeaders map[string]map[string]string `yaml:"domainHeaders" json:"domainHeaders"`
	// OutputDir is the directory where screenshots and PDFs are saved.
	// Defaults to a "rod-mcp" subdirectory in the OS temp directory.
	OutputDir string `yaml:"outputDir" json:"outputDir" mapstructure:"outputDir"`
	// ImageResponses controls whether inline base64 image data is included in screenshot results.
	// "allow" (default): saves file and includes inline base64 ImageContent.
	// "omit": saves file only, returns just the file path (saves tokens).
	ImageResponses ImageResponsesMode `yaml:"imageResponses" json:"imageResponses" mapstructure:"imageResponses"`
	// LoginUsernameSelectors overrides the default username field selectors tried during login.
	// If empty, the built-in defaults are used (input[type=email], input[name=email], etc.).
	LoginUsernameSelectors []string `yaml:"loginUsernameSelectors" json:"loginUsernameSelectors"`
	// LoginPasswordSelector overrides the default password field selector (input[type=password]).
	LoginPasswordSelector string `yaml:"loginPasswordSelector" json:"loginPasswordSelector"`
	// LoginSubmitSelector overrides the default submit button selector (button[type=submit]).
	LoginSubmitSelector string `yaml:"loginSubmitSelector" json:"loginSubmitSelector"`
	// Stealth enables anti-bot-detection measures: removes automation indicators,
	// patches navigator.webdriver, injects realistic browser fingerprints, and sets
	// a realistic User-Agent header. Useful for automating sites with bot detection.
	// [EXPERIMENTAL] behavior may change between releases.
	Stealth bool `yaml:"stealth" json:"stealth"`
}

var (
	DefaultBrowserTempDir    = "./rod/browser"
	DefaultServerName        = "Rod Server"
	DefaultLaunchTimeout     = 30 * time.Second
	DefaultNavigationTimeout = 30 * time.Second

	DefaultConfig = Config{
		BrowserBinPath:      "",
		Headless:            true,
		BrowserTempDir:      DefaultBrowserTempDir,
		LaunchTimeoutMs:     int(DefaultLaunchTimeout / time.Millisecond),
		NavigationTimeoutMs: int(DefaultNavigationTimeout / time.Millisecond),
		NoSandbox:           false,
		Proxy:               "",
		ServerName:          DefaultServerName,
		Mode:                Text,
		ImageResponses:      ImageResponsesAllow,
	}
)

func (c Config) LaunchTimeout() time.Duration {
	return timeoutFromMilliseconds(c.LaunchTimeoutMs, DefaultLaunchTimeout)
}

func (c Config) NavigationTimeout() time.Duration {
	return timeoutFromMilliseconds(c.NavigationTimeoutMs, DefaultNavigationTimeout)
}

func timeoutFromMilliseconds(ms int, fallback time.Duration) time.Duration {
	if ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

// InitDefaultConfig generates the default configuration file at DefaultConfigPath.
// It creates any missing parent directories. If the file already exists, it is a no-op.
func InitDefaultConfig() error {
	defaultConfigPath := DefaultConfigPath()
	exist, err := utils.PathExists(defaultConfigPath)
	if err != nil {
		slog.Warn(fmt.Sprintf("checking config path %s: %v", defaultConfigPath, err))
	}
	if exist {
		return nil
	}

	// Ensure the parent directory exists before creating the file.
	// Use 0700 (owner-only) since the config may contain sensitive values
	// such as authorization tokens in DomainHeaders.
	if err := os.MkdirAll(filepath.Dir(defaultConfigPath), 0o700); err != nil {
		return fmt.Errorf("create config dir %s: %w", filepath.Dir(defaultConfigPath), err)
	}

	// Use 0600 (owner read/write only) for the same reason.
	f, err := os.OpenFile(defaultConfigPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create default config %s: %w", defaultConfigPath, err)
	}
	defer f.Close()

	encoder := yaml.NewEncoder(f)
	defer encoder.Close()

	if err := encoder.Encode(DefaultConfig); err != nil {
		return fmt.Errorf("write default config %s: %w", defaultConfigPath, err)
	}
	return nil
}

// LoadConfig loads the configuration file at configPath.
// When configPath is empty it first checks DefaultConfigPath() (XDG_CONFIG_HOME/rod-mcp/rod-mcp.yaml),
// then ./rod-mcp.yaml for legacy compatibility. If neither exists, built-in
// defaults are used and no file is created.
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		var err error
		configPath, err = findExistingConfigPath()
		if err != nil {
			return nil, err
		}
		if configPath == "" {
			slog.Info(fmt.Sprintf("no config file, using built-in defaults"))
			cfg := DefaultConfig
			return &cfg, nil
		}
	}

	return loadConfigFile(configPath)
}

func findExistingConfigPath() (string, error) {
	for _, candidate := range []string{DefaultConfigPath(), ConfigName} {
		exist, err := utils.PathExists(candidate)
		if err != nil {
			return "", fmt.Errorf("check config file %s: %w", candidate, err)
		}
		if exist {
			return candidate, nil
		}
	}
	return "", nil
}

func loadConfigFile(configPath string) (*Config, error) {
	// check if config file exist
	exist, err := utils.PathExists(configPath)
	if err != nil {
		return nil, fmt.Errorf("check config file %s: %w", configPath, err)
	}

	if !exist {
		slog.Info(fmt.Sprintf("config file not found at %s, using built-in defaults", configPath))
		config := DefaultConfig
		return &config, nil
	}

	slog.Info(fmt.Sprintf("loading config from %s", configPath))

	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("open config %s: %w", configPath, err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	config := DefaultConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", configPath, err)
	}
	return &config, nil
}

// GetHeadersForURL returns all headers that should be applied for the given URL.
// This merges ExtraHTTPHeaders with any matching DomainHeaders patterns.
func (c *Config) GetHeadersForURL(urlStr string) map[string]string {
	result := make(map[string]string)

	// Start with global headers
	for k, v := range c.ExtraHTTPHeaders {
		result[k] = v
	}

	// If no domain headers configured or URL is empty, return global headers only
	if len(c.DomainHeaders) == 0 || urlStr == "" {
		return result
	}

	// Extract host from URL
	host := extractHost(urlStr)
	if host == "" {
		return result
	}

	// Check each domain pattern
	for pattern, headers := range c.DomainHeaders {
		if matchDomainPattern(pattern, host) {
			for k, v := range headers {
				result[k] = v
			}
		}
	}

	return result
}

// extractHost extracts the hostname from a URL string
func extractHost(urlStr string) string {
	// Handle URLs without scheme
	if !strings.Contains(urlStr, "://") {
		urlStr = "https://" + urlStr
	}

	// Find the host portion
	start := strings.Index(urlStr, "://")
	if start == -1 {
		return ""
	}
	start += 3

	end := strings.IndexAny(urlStr[start:], ":/")
	if end == -1 {
		return urlStr[start:]
	}
	return urlStr[start : start+end]
}

// matchDomainPattern checks if a host matches a domain pattern.
// Supports wildcard prefix: "*.example.com" matches "www.example.com", "api.example.com"
// Exact match: "example.com" only matches "example.com"
func matchDomainPattern(pattern, host string) bool {
	pattern = strings.ToLower(pattern)
	host = strings.ToLower(host)

	// Wildcard pattern: *.example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		// Match if host ends with the suffix (subdomain) or equals the base domain
		if strings.HasSuffix(host, suffix) {
			return true
		}
		// Also match the base domain itself (*.example.com should match example.com)
		baseDomain := pattern[2:]
		return host == baseDomain
	}

	// Exact match
	return pattern == host
}
