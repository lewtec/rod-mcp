package types

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Chrome user-data-dir layout constants.
const (
	// chromeDefaultProfile is the subdirectory name for the default Chrome profile.
	chromeDefaultProfile = "Default"
	// chromeLocalState is the top-level file Chrome needs to start (stores global prefs).
	chromeLocalState = "Local State"
	// chromePreferences is the per-profile preferences file.
	chromePreferences = "Preferences"
	// chromeSecurePreferences is the tamper-evident copy of Preferences.
	chromeSecurePreferences = "Secure Preferences"
	// chromeLocalStorage is the per-profile Local Storage directory.
	chromeLocalStorage = "Local Storage"
	// chromeSessionStorage is the per-profile Session Storage directory.
	chromeSessionStorage = "Session Storage"
	// profileTempDirPattern is the glob pattern for temporary profile clone directories.
	profileTempDirPattern = "rod-mcp-profile-*"
	// profileFullTempDirPattern is the glob pattern for full profile clone temp directories.
	profileFullTempDirPattern = "rod-mcp-profile-full-*"
)

// cloneProfile copies auth-relevant files from a Chrome user data directory
// into a temporary directory. Cookies are NOT copied — they are decrypted and
// injected via CDP after browser launch (see ReadChromeCookies in cookies.go).
//
// Returns the path to the cloned directory, which the caller must clean up.
func cloneProfile(srcDir string, domains []string) (string, error) {
	// Chrome profiles live in a chromeDefaultProfile subdirectory (or "Profile N").
	// The top-level dir also contains files Chrome needs to start.
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return "", fmt.Errorf("user-data-dir %q does not exist", srcDir)
	}

	profileDir := filepath.Join(srcDir, chromeDefaultProfile)
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		return "", fmt.Errorf("no %s profile found in %q", chromeDefaultProfile, srcDir)
	}

	tmpDir, err := os.MkdirTemp("", profileTempDirPattern)
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	tmpProfile := filepath.Join(tmpDir, chromeDefaultProfile)
	if err := os.MkdirAll(tmpProfile, 0755); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("create profile dir: %w", err)
	}

	// Copy top-level files Chrome needs to start (Local State, etc.)
	for _, name := range []string{chromeLocalState} {
		src := filepath.Join(srcDir, name)
		if _, err := os.Stat(src); err == nil {
			if err := copyFile(src, filepath.Join(tmpDir, name)); err != nil {
				slog.Warn("clone profile: skip file", "name", name, "err", err)
			}
		}
	}

	// Copy profile-level files needed for auth state.
	profileFiles := []string{
		chromePreferences,
		chromeSecurePreferences,
	}
	for _, name := range profileFiles {
		src := filepath.Join(profileDir, name)
		if _, err := os.Stat(src); err == nil {
			if err := copyFile(src, filepath.Join(tmpProfile, name)); err != nil {
				slog.Warn("clone profile: skip file", "name", name, "err", err)
			}
		}
	}

	// NOTE: Cookies are NOT copied here. Chrome encrypts cookie values with
	// OS-level keys (macOS Keychain, Linux DPAPI, etc.) that are tied to the
	// original profile. Instead, cookies are decrypted and injected via CDP
	// after browser launch — see ReadChromeCookies() in cookies.go.

	// Copy Local Storage directory (usually small, contains auth tokens).
	localStorageSrc := filepath.Join(profileDir, chromeLocalStorage)
	if _, err := os.Stat(localStorageSrc); err == nil {
		if err := copyDir(localStorageSrc, filepath.Join(tmpProfile, chromeLocalStorage)); err != nil {
			slog.Warn("clone profile: skip dir", "name", chromeLocalStorage, "err", err)
		}
	}

	// Copy Session Storage directory.
	sessionStorageSrc := filepath.Join(profileDir, chromeSessionStorage)
	if _, err := os.Stat(sessionStorageSrc); err == nil {
		if err := copyDir(sessionStorageSrc, filepath.Join(tmpProfile, chromeSessionStorage)); err != nil {
			slog.Warn("clone profile: skip dir", "name", chromeSessionStorage, "err", err)
		}
	}

	slog.Info("cloned Chrome profile", "dir", tmpDir)
	return tmpDir, nil
}

// cloneProfileFull recursively copies the entire Chrome user data directory.
// This is slow for large profiles but preserves everything (extensions, history, etc.).
func cloneProfileFull(srcDir string) (string, error) {
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return "", fmt.Errorf("user-data-dir %q does not exist", srcDir)
	}

	tmpDir, err := os.MkdirTemp("", profileFullTempDirPattern)
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	// Remove the temp dir so copyDir creates it fresh from the source.
	if err := os.Remove(tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("prepare temp dir: %w", err)
	}

	if err := copyDir(srcDir, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("full clone failed: %w", err)
	}

	slog.Warn("full profile clone includes all browser data", "dir", tmpDir)
	return tmpDir, nil
}

// isValidDomainPattern checks that a domain string contains only safe characters
// to prevent SQL injection when used in sqlite3 queries.
func isValidDomainPattern(d string) bool {
	for _, c := range d {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '*') {
			return false
		}
	}
	return len(d) > 0
}

// copyFile copies a single file from src to dst, ensuring data is flushed to disk.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file %q: %w", src, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat source file %q: %w", src, err)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("create destination file %q: %w", dst, err)
	}

	_, copyErr := io.Copy(out, in)
	// Always close; prefer the copy error if both fail.
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy %q to %q: %w", src, dst, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close destination file %q: %w", dst, closeErr)
	}
	return nil
}

// copyDir recursively copies a directory tree, skipping symlinks.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source dir %q: %w", src, err)
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("create destination dir %q: %w", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read source dir %q: %w", src, err)
	}

	for _, entry := range entries {
		// Skip symlinks to avoid following links outside the profile directory.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}
