package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigMergesDefaults(t *testing.T) {
	// Write a minimal config with only mode set
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "minimal.yaml")
	if err := os.WriteFile(cfgPath, []byte("mode: vision\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.ServerName != DefaultServerName {
		t.Errorf("ServerName = %q, want %q (from DefaultConfig)", cfg.ServerName, DefaultServerName)
	}
	if cfg.Mode != Vision {
		t.Errorf("Mode = %q, want %q (from YAML)", cfg.Mode, Vision)
	}
	if cfg.ImageResponses != ImageResponsesAllow {
		t.Errorf("ImageResponses = %q, want %q (from DefaultConfig)", cfg.ImageResponses, ImageResponsesAllow)
	}
	if !cfg.Headless {
		t.Error("Headless = false, want true from DefaultConfig")
	}
	if cfg.LaunchTimeout() != DefaultLaunchTimeout {
		t.Errorf("LaunchTimeout = %s, want %s", cfg.LaunchTimeout(), DefaultLaunchTimeout)
	}
	if cfg.NavigationTimeout() != DefaultNavigationTimeout {
		t.Errorf("NavigationTimeout = %s, want %s", cfg.NavigationTimeout(), DefaultNavigationTimeout)
	}
}

func TestConfigTimeoutOverrides(t *testing.T) {
	cfg := Config{
		LaunchTimeoutMs:     1500,
		NavigationTimeoutMs: 2500,
	}
	if cfg.LaunchTimeout() != 1500*time.Millisecond {
		t.Errorf("LaunchTimeout = %s, want 1.5s", cfg.LaunchTimeout())
	}
	if cfg.NavigationTimeout() != 2500*time.Millisecond {
		t.Errorf("NavigationTimeout = %s, want 2.5s", cfg.NavigationTimeout())
	}
}

func TestConfigTimeoutFallbacks(t *testing.T) {
	cfg := Config{
		LaunchTimeoutMs:     -1,
		NavigationTimeoutMs: 0,
	}
	if cfg.LaunchTimeout() != DefaultLaunchTimeout {
		t.Errorf("LaunchTimeout fallback = %s, want %s", cfg.LaunchTimeout(), DefaultLaunchTimeout)
	}
	if cfg.NavigationTimeout() != DefaultNavigationTimeout {
		t.Errorf("NavigationTimeout fallback = %s, want %s", cfg.NavigationTimeout(), DefaultNavigationTimeout)
	}
}

func TestLoadConfigAcceptsCustomFilename(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "my-custom-config.yaml")
	if err := os.WriteFile(cfgPath, []byte("mode: text\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig should accept custom filename, got: %v", err)
	}
	if cfg.Mode != Text {
		t.Errorf("Mode = %q, want %q", cfg.Mode, Text)
	}
}

func TestMatchDomainPattern(t *testing.T) {
	tests := []struct {
		pattern string
		host    string
		want    bool
	}{
		// Exact matches
		{"example.com", "example.com", true},
		{"example.com", "www.example.com", false},
		{"example.com", "other.com", false},

		// Wildcard matches
		{"*.example.com", "www.example.com", true},
		{"*.example.com", "api.example.com", true},
		{"*.example.com", "sub.api.example.com", true},
		{"*.example.com", "example.com", true}, // base domain should also match
		{"*.example.com", "other.com", false},
		{"*.example.com", "exampleXcom", false},

		// Case insensitivity
		{"*.Example.COM", "www.example.com", true},
		{"example.com", "EXAMPLE.COM", true},

		// Additional wildcard patterns
		{"*.myapp.org", "www.myapp.org", true},
		{"*.myapp.org", "cdn.myapp.org", true},
		{"*.myapp.org", "myapp.org", true},
		{"*.myapp.test", "www.myapp.test", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.host, func(t *testing.T) {
			got := matchDomainPattern(tt.pattern, tt.host)
			if got != tt.want {
				t.Errorf("matchDomainPattern(%q, %q) = %v, want %v", tt.pattern, tt.host, got, tt.want)
			}
		})
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://www.example.com/path", "www.example.com"},
		{"https://www.example.com:443/path", "www.example.com"},
		{"http://example.com", "example.com"},
		{"https://api.myapp.org/v1/endpoint", "api.myapp.org"},
		{"www.example.com", "www.example.com"}, // no scheme
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractHost(tt.url)
			if got != tt.want {
				t.Errorf("extractHost(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestGetHeadersForURL(t *testing.T) {
	config := &Config{
		ExtraHTTPHeaders: map[string]string{
			"X-Global": "global-value",
		},
		DomainHeaders: map[string]map[string]string{
			"*.myapp.org": {
				"X-Bypass-Token": "secret123",
			},
			"*.myapp.test": {
				"X-Bypass-Token": "test-secret",
			},
			"api.example.com": {
				"Authorization": "Bearer token",
			},
		},
	}

	tests := []struct {
		url         string
		wantHeaders map[string]string
	}{
		{
			"https://www.myapp.org/page",
			map[string]string{
				"X-Global":       "global-value",
				"X-Bypass-Token": "secret123",
			},
		},
		{
			"https://www.myapp.test/page",
			map[string]string{
				"X-Global":       "global-value",
				"X-Bypass-Token": "test-secret",
			},
		},
		{
			"https://api.example.com/v1",
			map[string]string{
				"X-Global":      "global-value",
				"Authorization": "Bearer token",
			},
		},
		{
			"https://other.com/page",
			map[string]string{
				"X-Global": "global-value",
			},
		},
		{
			"", // empty URL
			map[string]string{
				"X-Global": "global-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := config.GetHeadersForURL(tt.url)
			if len(got) != len(tt.wantHeaders) {
				t.Errorf("GetHeadersForURL(%q) returned %d headers, want %d", tt.url, len(got), len(tt.wantHeaders))
			}
			for k, v := range tt.wantHeaders {
				if got[k] != v {
					t.Errorf("GetHeadersForURL(%q)[%q] = %q, want %q", tt.url, k, got[k], v)
				}
			}
		})
	}
}

// TestDefaultConfigPathNeverUsesCwd verifies that DefaultConfigPath never returns
// a path under the current working directory. Regression test for #283.
//
// The test changes cwd to a fresh temp dir so the assertion is unambiguous
// regardless of what directory the test runner is invoked from. It uses
// filepath.Rel rather than strings.HasPrefix to avoid false positives from
// case-insensitive filesystems or "/tmp/foo" vs "/tmp/foobar" overlaps.
func TestDefaultConfigPathNeverUsesCwd(t *testing.T) {
	// Use an isolated temp dir as cwd so we know exactly what to check against.
	fakeCwd := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(fakeCwd); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	// Point XDG_CONFIG_HOME to a known absolute path outside fakeCwd.
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	got := DefaultConfigPath()

	// filepath.Rel returns a path starting with ".." when got is outside fakeCwd.
	// If it does NOT start with "..", got is inside (or equal to) fakeCwd.
	rel, err := filepath.Rel(fakeCwd, got)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if !strings.HasPrefix(rel, "..") {
		t.Errorf("DefaultConfigPath() = %q is inside cwd %q (rel=%q); must not reference the caller's working directory", got, fakeCwd, rel)
	}
}

// TestLoadConfigDoesNotPolluteCwd verifies that calling LoadConfig with an empty
// configPath (the no-flag default) creates no files in the current working directory.
// Regression test for #283: rod-mcp was writing rod-mcp.yaml into cwd.
func TestLoadConfigDoesNotPolluteCwd(t *testing.T) {
	// Use a temp dir as our synthetic "project repo" cwd so we can check it cleanly.
	fakeCwd := t.TempDir()

	// Point XDG_CONFIG_HOME at a separate temp dir so InitDefaultConfig has somewhere
	// to write without touching the user's real ~/.config.
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	// Change the process working directory to fakeCwd for the duration of the test.
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(fakeCwd); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	// LoadConfig with empty path must not drop any file in fakeCwd.
	if _, err := LoadConfig(""); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	entries, err := os.ReadDir(fakeCwd)
	if err != nil {
		t.Fatalf("os.ReadDir: %v", err)
	}
	if len(entries) > 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("LoadConfig wrote %d file(s) into cwd %q: %v", len(entries), fakeCwd, names)
	}

	// Flags-only mode must not create a default yaml under XDG either.
	wantPath := filepath.Join(xdgDir, "rod-mcp", ConfigName)
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Errorf("did not want default config at %q, stat: %v", wantPath, err)
	}
}

func TestLoadConfigUsesLegacyCwdConfigWhenCanonicalMissing(t *testing.T) {
	fakeCwd := t.TempDir()
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(fakeCwd); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	if err := os.WriteFile(filepath.Join(fakeCwd, ConfigName), []byte("mode: vision\nheadless: true\nserverName: Legacy Config\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Mode != Vision {
		t.Errorf("Mode = %q, want %q from legacy config", cfg.Mode, Vision)
	}
	if !cfg.Headless {
		t.Error("Headless = false, want true from legacy config")
	}
	if cfg.ServerName != "Legacy Config" {
		t.Errorf("ServerName = %q, want legacy config value", cfg.ServerName)
	}

	canonicalPath := filepath.Join(xdgDir, "rod-mcp", ConfigName)
	if _, err := os.Stat(canonicalPath); !os.IsNotExist(err) {
		t.Errorf("canonical config should not be created when legacy config exists, stat err: %v", err)
	}
}

func TestLoadConfigPrefersCanonicalConfigOverLegacyCwdConfig(t *testing.T) {
	fakeCwd := t.TempDir()
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(fakeCwd); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	if err := os.WriteFile(filepath.Join(fakeCwd, ConfigName), []byte("mode: vision\nheadless: true\nserverName: Legacy Config\n"), 0644); err != nil {
		t.Fatal(err)
	}

	canonicalPath := filepath.Join(xdgDir, "rod-mcp", ConfigName)
	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalPath, []byte("mode: text\nheadless: false\nserverName: Canonical Config\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Mode != Text {
		t.Errorf("Mode = %q, want %q from canonical config", cfg.Mode, Text)
	}
	if cfg.Headless {
		t.Error("Headless = true, want false from canonical config")
	}
	if cfg.ServerName != "Canonical Config" {
		t.Errorf("ServerName = %q, want canonical config value", cfg.ServerName)
	}
}

// TestDefaultConfigPathIgnoresRelativeXDG verifies that a relative XDG_CONFIG_HOME
// is rejected and the home-directory fallback is used instead. Addresses the
// Copilot review finding that a relative XDG path would reintroduce cwd-pollution.
func TestDefaultConfigPathIgnoresRelativeXDG(t *testing.T) {
	// Set XDG_CONFIG_HOME to a relative path — must be ignored.
	t.Setenv("XDG_CONFIG_HOME", ".config")

	got := DefaultConfigPath()
	if !filepath.IsAbs(got) {
		t.Errorf("DefaultConfigPath() = %q is not absolute; relative XDG_CONFIG_HOME must be ignored", got)
	}
	// The path must NOT start with cwd/.config.
	cwd, _ := os.Getwd()
	rel, _ := filepath.Rel(cwd, got)
	if !strings.HasPrefix(rel, "..") {
		t.Errorf("DefaultConfigPath() = %q appears to be inside cwd %q; relative XDG_CONFIG_HOME must be ignored", got, cwd)
	}
}
