package types

import (
	"os"
	"strings"
	"testing"
)

func TestAcquireInstanceLockOnlyForExplicitSharedResources(t *testing.T) {
	withInstanceLockRoot(t)

	lock, err := acquireInstanceLock(Config{})
	if err != nil {
		t.Fatalf("acquireInstanceLock without explicit resource: %v", err)
	}
	if lock != nil {
		t.Fatal("acquireInstanceLock without explicit resource returned a lock")
	}
}

func TestAcquireInstanceLockRejectsConcurrentDebugPort(t *testing.T) {
	withInstanceLockRoot(t)
	cfg := Config{ChromeDebugPort: "9222"}

	lock, err := acquireInstanceLock(cfg)
	if err != nil {
		t.Fatalf("acquireInstanceLock first: %v", err)
	}
	defer lock.Release()

	_, err = acquireInstanceLock(cfg)
	if err == nil {
		t.Fatal("acquireInstanceLock second: expected contention error")
	}
	if !strings.Contains(err.Error(), "debug-port:9222") {
		t.Fatalf("contention error = %q, want debug-port key", err.Error())
	}
}

func TestAcquireInstanceLockRejectsConcurrentNoCloneProfile(t *testing.T) {
	withInstanceLockRoot(t)
	cfg := Config{NoClone: true, UserDataDir: "/tmp/chrome-profile"}

	lock, err := acquireInstanceLock(cfg)
	if err != nil {
		t.Fatalf("acquireInstanceLock first: %v", err)
	}
	defer lock.Release()

	_, err = acquireInstanceLock(cfg)
	if err == nil {
		t.Fatal("acquireInstanceLock second: expected contention error")
	}
	if !strings.Contains(err.Error(), "profile:/tmp/chrome-profile") {
		t.Fatalf("contention error = %q, want profile key", err.Error())
	}
}

func TestAcquireInstanceLockSkipsClonedProfile(t *testing.T) {
	withInstanceLockRoot(t)
	lock, err := acquireInstanceLock(Config{UserDataDir: "/tmp/chrome-profile"})
	if err != nil {
		t.Fatalf("acquireInstanceLock: %v", err)
	}
	if lock != nil {
		t.Fatal("cloned profile returned a lock")
	}
}

func TestAcquireInstanceLockRemovesStaleLock(t *testing.T) {
	withInstanceLockRoot(t)
	cfg := Config{CDPEndpoint: "http://127.0.0.1:9222"}

	lock, err := acquireInstanceLock(cfg)
	if err != nil {
		t.Fatalf("acquireInstanceLock first: %v", err)
	}
	path := lock.path
	if err := os.WriteFile(path, []byte("999999\n"+instanceLockKey(cfg)+"\n"), 0o600); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	replacement, err := acquireInstanceLock(cfg)
	if err != nil {
		t.Fatalf("acquireInstanceLock replacement: %v", err)
	}
	defer replacement.Release()
	if replacement.path != path {
		t.Fatalf("replacement lock path = %q, want %q", replacement.path, path)
	}
}

func withInstanceLockRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	prev := instanceLockRoot
	instanceLockRoot = func() string { return root }
	t.Cleanup(func() {
		instanceLockRoot = prev
	})
	return root
}
