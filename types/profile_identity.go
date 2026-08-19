package types

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrInvalidProfileSlug is returned when a user-supplied profile slug is empty
// or reduces to a path component that is not usable as a directory name.
var ErrInvalidProfileSlug = errors.New("invalid profile slug")

const profileNameHashLen = 6

// GitOrigin returns the origin remote URL for the git checkout containing dir.
// Tests replace this.
var GitOrigin = gitOriginURL

func gitOriginURL(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return ""
	}
	out, err = exec.CommandContext(ctx, "git", "-C", root, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// NormalizeRemoteURL maps git remote forms onto host/path so https, ssh, and
// scp-style URLs for the same repo share a profile.
func NormalizeRemoteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ""
	}

	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		host := u.Hostname()
		path := strings.TrimPrefix(u.Path, "/")
		switch {
		case host == "":
			return path
		case path == "":
			return host
		default:
			return host + "/" + path
		}
	}

	// scp-like: git@github.com:org/repo
	if at := strings.Index(raw, "@"); at >= 0 {
		rest := raw[at+1:]
		host, path, ok := strings.Cut(rest, ":")
		if ok && host != "" && !strings.Contains(host, "/") {
			return host + "/" + strings.TrimPrefix(path, "/")
		}
	}
	return raw
}

// SanitizeProfileSlug keeps a user-supplied slug path-safe.
func SanitizeProfileSlug(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "." || s == ".." {
		return "", fmt.Errorf("%w: %q", ErrInvalidProfileSlug, s)
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" || out == "." || out == ".." {
		return "", fmt.Errorf("%w: %q", ErrInvalidProfileSlug, s)
	}
	return out, nil
}

// HashedProfileName is the directory name under <data-dir>/profiles.
func HashedProfileName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:profileNameHashLen])
}

// DefaultProfileIdentity returns a stable key and directory name for the
// process working directory: git origin when present, otherwise the cwd.
func DefaultProfileIdentity() (key, name string) {
	if remote := GitOrigin("."); remote != "" {
		key = "git:" + NormalizeRemoteURL(remote)
		return key, HashedProfileName(key)
	}
	cwd, err := os.Getwd()
	if err != nil {
		key = "cwd:unknown"
		return key, HashedProfileName(key)
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	key = "cwd:" + filepath.Clean(abs)
	return key, HashedProfileName(key)
}

func writeProfileSource(profileDir, key string) {
	if key == "" {
		return
	}
	dir := filepath.Dir(profileDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Warn("create profile parent dir", "dir", dir, "err", err)
		return
	}
	path := profileDir + ".source"
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		slog.Warn("write profile source sidecar", "path", path, "err", err)
	}
}
