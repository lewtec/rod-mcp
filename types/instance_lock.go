package types

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type instanceLock struct {
	path string
}

var instanceLockRoot = func() string {
	return filepath.Join(os.TempDir(), "rod-mcp-locks")
}

func acquireInstanceLock(cfg Config) (*instanceLock, error) {
	key := instanceLockKey(cfg)
	if key == "" {
		return nil, nil
	}
	root := instanceLockRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create browser instance lock dir: %w", err)
	}
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(root, fmt.Sprintf("%x.lock", sum[:16]))
	for {
		lock, err := createInstanceLock(path, key)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		stale, owner, staleErr := staleInstanceLock(path)
		if staleErr != nil {
			return nil, staleErr
		}
		if !stale {
			return nil, fmt.Errorf("browser instance lock held for %s by pid %d", key, owner)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale browser instance lock: %w", err)
		}
	}
}

func instanceLockKey(cfg Config) string {
	if cfg.CDPEndpoint != "" {
		return "cdp:" + cfg.CDPEndpoint
	}
	if cfg.ChromeDebugPort != "" {
		return "debug-port:" + cfg.ChromeDebugPort
	}
	if cfg.NoClone && cfg.UserDataDir != "" {
		return "profile:" + filepath.Clean(cfg.UserDataDir)
	}
	return ""
}

func createInstanceLock(path, key string) (*instanceLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), key); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write browser instance lock: %w", err)
	}
	return &instanceLock{path: path}, nil
}

func staleInstanceLock(path string) (bool, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, 0, nil
		}
		return false, 0, fmt.Errorf("read browser instance lock: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return true, 0, nil
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return true, 0, nil
	}
	if pid == os.Getpid() {
		return false, pid, nil
	}
	if processExists(pid) {
		return false, pid, nil
	}
	return true, pid, nil
}

func (l *instanceLock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	l.path = ""
	return nil
}
