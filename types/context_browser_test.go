package types

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestControlBrowserConnectFailureReturnsError locks in the fix for the
// rod_navigate panic: connecting to a CDP endpoint with no Chrome listening
// must return a clean error, never panic. Before the fix, controlBrowser
// called browser.Close() on the connect-failure path, and go-rod's Close()
// dereferenced the never-established nil CDP client.
func TestControlBrowserConnectFailureReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Port 1 has no CDP server; Connect() must fail (connection refused).
	b, err := controlBrowser(ctx, "ws://127.0.0.1:1")
	if err == nil {
		if b != nil {
			_ = b.Close()
		}
		t.Fatal("controlBrowser: expected error connecting to a dead endpoint, got nil")
	}
	if b != nil {
		t.Fatalf("controlBrowser: expected nil browser on connect failure, got %v", b)
	}
	if !strings.Contains(err.Error(), "connect to browser") {
		t.Fatalf("controlBrowser error = %q, want a connect-to-browser wrap", err.Error())
	}
}

func TestConfigureLauncherHeadlessFlag(t *testing.T) {
	l, err := configureLauncher(context.Background(), Config{
		Headless:       true,
		BrowserBinPath: "/bin/echo",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("configureLauncher: %v", err)
	}
	if !launcherArgsContain(l.FormatArgs(), "--headless") {
		t.Fatalf("headless launcher args = %v, want --headless", l.FormatArgs())
	}
}

func TestConfigureLauncherWindowedOmitsHeadlessFlag(t *testing.T) {
	l, err := configureLauncher(context.Background(), Config{
		Headless:       false,
		BrowserBinPath: "/bin/echo",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("configureLauncher: %v", err)
	}
	if launcherArgsContain(l.FormatArgs(), "--headless") {
		t.Fatalf("windowed launcher args = %v, want no --headless", l.FormatArgs())
	}
}

func TestResolveUserDataDirWritesSourceSidecar(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profiles", "abc123")
	got, cloned, err := resolveUserDataDir(Config{
		UserDataDir: profile,
		NoClone:     true,
		ProfileKey:  "git:example.com/foo",
	})
	if err != nil {
		t.Fatalf("resolveUserDataDir: %v", err)
	}
	if got != profile {
		t.Fatalf("userDataDir = %q, want %q", got, profile)
	}
	if cloned != "" {
		t.Fatalf("clonedDir = %q, want empty", cloned)
	}
	data, err := os.ReadFile(profile + ".source")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "git:example.com/foo\n" {
		t.Fatalf("sidecar = %q", data)
	}
}

func launcherArgsContain(args []string, want string) bool {
	for _, arg := range args {
		if arg == want || strings.HasPrefix(arg, want+"=") {
			return true
		}
	}
	return false
}
