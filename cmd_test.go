package main

import (
	"encoding/json"
	"os"
	"testing"
)

type registryEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func TestParseCommandArgsGUIForcesHeadful(t *testing.T) {
	cfg, err := parseCommandArgs([]string{"rod-mcp", "--gui", "--compact-snapshot"})
	if err != nil {
		t.Fatalf("parseCommandArgs: %v", err)
	}
	if cfg.Headless {
		t.Fatal("Headless = true, want false")
	}
	if !cfg.CompactSnapshot {
		t.Fatal("CompactSnapshot = false, want true")
	}
}

func TestParseCommandArgsRejectsConflictingHeadlessAndGUI(t *testing.T) {
	if _, err := parseCommandArgs([]string{"rod-mcp", "--headless", "--gui"}); err == nil {
		t.Fatal("parseCommandArgs accepted conflicting --headless and --gui")
	}
}

func TestGUIServerRegistryLaunchesHeadful(t *testing.T) {
	data, err := os.ReadFile("mcp-registry.json")
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var registry map[string]registryEntry
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatalf("parse registry: %v", err)
	}
	entry, ok := registry["rod-mcp-gui"]
	if !ok {
		t.Fatal("registry missing rod-mcp-gui entry")
	}

	if !containsArg(entry.Args, "--gui") {
		t.Fatalf("rod-mcp-gui args = %v, want --gui", entry.Args)
	}
	if containsArg(entry.Args, "--headless") {
		t.Fatalf("rod-mcp-gui args = %v, must not include --headless", entry.Args)
	}

	cfg, err := parseCommandArgs(append([]string{"rod-mcp"}, entry.Args...))
	if err != nil {
		t.Fatalf("parse registry args: %v", err)
	}
	if cfg.Headless {
		t.Fatalf("rod-mcp-gui registry args yielded Headless=true: %v", entry.Args)
	}
}

func TestDataDirDerivesLogProfileOutputAndBrowser(t *testing.T) {
	cfg, err := parseCommandArgs([]string{"rod-mcp", "--data-dir", "/tmp/rod-data"})
	if err != nil {
		t.Fatalf("parseCommandArgs: %v", err)
	}
	if cfg.DataDir != "/tmp/rod-data" {
		t.Fatalf("DataDir = %q, want /tmp/rod-data", cfg.DataDir)
	}
	if cfg.OutputDir != "/tmp/rod-data/output" {
		t.Fatalf("OutputDir = %q", cfg.OutputDir)
	}
	if cfg.BrowserTempDir != "/tmp/rod-data/browser" {
		t.Fatalf("BrowserTempDir = %q", cfg.BrowserTempDir)
	}
	if cfg.UserDataDir != "/tmp/rod-data/profile" {
		t.Fatalf("UserDataDir = %q", cfg.UserDataDir)
	}
	if !cfg.NoClone {
		t.Fatal("derived profile should default to --no-clone")
	}
}

func TestExplicitPathsWinOverDataDir(t *testing.T) {
	cfg, err := parseCommandArgs([]string{
		"rod-mcp",
		"--data-dir", "/tmp/rod-data",
		"--output-dir", "/tmp/custom-out",
	})
	if err != nil {
		t.Fatalf("parseCommandArgs: %v", err)
	}
	if cfg.OutputDir != "/tmp/custom-out" {
		t.Fatalf("OutputDir = %q, want /tmp/custom-out", cfg.OutputDir)
	}
}

func TestVerboseFlag(t *testing.T) {
	cfg, err := parseCommandArgs([]string{"rod-mcp", "--verbose"})
	if err != nil {
		t.Fatalf("parseCommandArgs: %v", err)
	}
	if !cfg.Verbose {
		t.Fatal("Verbose = false, want true")
	}
}

func TestParseCommandArgsBrowserAndTempDir(t *testing.T) {
	cfg, err := parseCommandArgs([]string{
		"rod-mcp",
		"--browser-bin-path", "helium",
		"--browser-temp-dir", "/tmp/rod-browser",
		"--no-sandbox",
	})
	if err != nil {
		t.Fatalf("parseCommandArgs: %v", err)
	}
	if cfg.BrowserBinPath != "helium" {
		t.Fatalf("BrowserBinPath = %q, want helium", cfg.BrowserBinPath)
	}
	if cfg.BrowserTempDir != "/tmp/rod-browser" {
		t.Fatalf("BrowserTempDir = %q, want /tmp/rod-browser", cfg.BrowserTempDir)
	}
	if !cfg.NoSandbox {
		t.Fatal("NoSandbox = false, want true")
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
