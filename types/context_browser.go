package types

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"

	"github.com/aliwatters/rod-mcp/utils"
)

var launchBrowserFunc = launchBrowser

// launchBrowser starts a new Chrome instance. When a user-data-dir is configured
// and cloning is enabled (the default), the profile is cloned to a temp directory
// whose path is returned as clonedDir so the caller can clean it up on exit.
func launchBrowser(ctx context.Context, cfg Config) (browser *rod.Browser, clonedDir string, err error) {

	if cfg.CDPEndpoint != "" {
		slog.Info(fmt.Sprintf("browser launch: connecting to configured CDP endpoint"))
		b, err := controlBrowser(ctx, cfg.CDPEndpoint)
		return b, "", err
	}

	// Validate flag combinations.
	if cfg.UserDataDir == "" && (cfg.NoClone || cfg.CloneAll || len(cfg.CloneDomains) > 0) {
		return nil, "", fmt.Errorf("--no-clone, --clone-all, and --clone-domains require --user-data-dir")
	}
	if cfg.NoClone && cfg.CloneAll {
		return nil, "", fmt.Errorf("--no-clone and --clone-all are mutually exclusive")
	}

	userDataDir, clonedDir, err := resolveUserDataDir(cfg)
	if err != nil {
		return nil, "", err
	}

	// Bound Chrome STARTUP (the launcher) with the launch timeout so a hung
	// local-Chrome launch fails fast. This context is for the launcher only; the
	// rod browser below keeps the long-lived ctx as its lifetime context (rod
	// ties a browser's connection/event loop to its creation context, so it must
	// not be created under a cancel-on-return timeout — rod-mcp#308).
	launchCtx, cancelLaunch := context.WithTimeout(ctx, cfg.LaunchTimeout())
	defer cancelLaunch()
	browserLauncher, err := configureLauncher(launchCtx, cfg, userDataDir)
	if err != nil {
		return nil, "", err
	}

	slog.Info(fmt.Sprintf("browser launch: starting local Chrome headless=%t", cfg.Headless))
	launchStart := time.Now()
	controlUrl, err := browserLauncher.Launch()
	if err != nil {
		return nil, "", fmt.Errorf("launch local browser: %w", err)
	}
	slog.Info(fmt.Sprintf("browser launch: local Chrome returned CDP URL after %s", time.Since(launchStart).Round(time.Millisecond)))
	b, err := controlBrowser(ctx, controlUrl)
	if err != nil {
		return nil, "", err
	}

	// Inject decrypted cookies via CDP when using profile cloning (not --no-clone).
	// The Cookies DB is encrypted by the OS, so we decrypt from the source profile
	// and inject into the fresh browser via CDP.
	if cfg.UserDataDir != "" && !cfg.NoClone {
		cookies, err := ReadChromeCookies(cfg.UserDataDir, cfg.CloneDomains)
		if err != nil {
			slog.Warn(fmt.Sprintf("cookie injection: %s (browser will start without cookies)", err))
		} else if len(cookies) > 0 {
			if err := b.SetCookies(cookies); err != nil {
				slog.Warn(fmt.Sprintf("cookie injection via CDP failed: %s", err))
			} else {
				slog.Info(fmt.Sprintf("injected %d cookies via CDP", len(cookies)))
			}
		}
	}

	return b, clonedDir, nil
}

// resolveUserDataDir determines the Chrome user data directory to use based on
// the Config settings. It clones the profile if needed and returns the path to
// use as well as the cloned temp directory (empty string when no clone was made).
func resolveUserDataDir(cfg Config) (userDataDir, clonedDir string, err error) {
	if cfg.UserDataDir != "" {
		if cfg.NoClone {
			// Use the profile directly — user accepted the risk.
			slog.Warn(fmt.Sprintf("--no-clone: using profile directly at %s (Chrome must not be running with this profile)", cfg.UserDataDir))
			return cfg.UserDataDir, "", nil
		} else if cfg.CloneAll {
			// Full recursive clone — slow but complete.
			slog.Warn(fmt.Sprintf("--clone-all: cloning ENTIRE Chrome profile — this includes passwords, history, extensions, and all browser data"))
			clonedDir, err = cloneProfileFull(cfg.UserDataDir)
			if err != nil {
				return "", "", fmt.Errorf("full profile clone: %w", err)
			}
			return clonedDir, clonedDir, nil
		} else {
			// Default: selective clone with domain-scoped cookies.
			clonedDir, err = cloneProfile(cfg.UserDataDir, cfg.CloneDomains)
			if err != nil {
				return "", "", fmt.Errorf("profile clone: %w", err)
			}
			return clonedDir, clonedDir, nil
		}
	}

	// No user-data-dir configured — use a unique temp subdirectory.
	if cfg.BrowserTempDir == "" {
		cfg.BrowserTempDir = DefaultBrowserTempDir
	}
	dir := fmt.Sprintf("%s/%s", cfg.BrowserTempDir, utils.RandomString(tempDirSuffixLen))
	return dir, "", nil
}

// configureLauncher builds a rod launcher with all Chrome flags applied from cfg.
// The returned launcher is ready to call Launch() on.
func configureLauncher(ctx context.Context, cfg Config, userDataDir string) (*launcher.Launcher, error) {
	l := launcher.New().
		Context(ctx).
		Headless(cfg.Headless).
		NoSandbox(cfg.NoSandbox).
		Set("no-gpu").
		Set("--no-first-run").
		Set("ignore-certificate-errors").
		Set("disable-xss-auditor", "true").
		Set("disable-popup-blocking").
		Set("mute-audio", "true").
		Set("use-mock-keychain").
		Set("--remote-allow-origins", "*").
		Set("--disable-dev-shm-usage").
		Set("--disable-features", "HttpsUpgrades,PasswordLeakDetection").
		UserDataDir(userDataDir)

	if cfg.BrowserBinPath != "" {
		l.Bin(cfg.BrowserBinPath)
	} else {
		if browserPath, has := launcher.LookPath(); has {
			l.Bin(browserPath)
		} else {
			return nil, errors.New("Chrome not found; set browserBinPath in config or install Chrome/Chromium")
		}
	}

	if cfg.ChromeDebugPort != "" {
		port, err := strconv.Atoi(cfg.ChromeDebugPort)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid chrome-debug-port %q: must be 1-65535", cfg.ChromeDebugPort)
		}
		l.Set("remote-debugging-port", cfg.ChromeDebugPort)
	}

	if cfg.Proxy != "" {
		l.Proxy(cfg.Proxy)
	}

	if cfg.Stealth {
		// Remove the automation indicator flag that Chromium sets by default.
		l.Delete("enable-automation")
		// Disable the Blink feature that exposes navigator.webdriver = true.
		l.Set("disable-blink-features", "AutomationControlled")
		slog.Info(fmt.Sprintf("[EXPERIMENTAL] stealth mode enabled — behavior may change between releases"))
	}

	return l, nil
}

func controlBrowser(ctx context.Context, controlURL string) (*rod.Browser, error) {
	browser := rod.New().Context(ctx)
	if err := browser.ControlURL(controlURL).Connect(); err != nil {
		// Do NOT call browser.Close() here: when Connect() fails (e.g. no
		// Chrome listening at the configured CDP endpoint), the browser's CDP
		// client was never established, and go-rod's Close() dereferences that
		// nil client — turning a clean "can't connect" error into a panic.
		// There is nothing to close on a browser that never connected.
		return nil, fmt.Errorf("connect to browser at %s: %w", controlURL, err)
	}
	if err := browser.IgnoreCertErrors(true); err != nil {
		return nil, fmt.Errorf("set ignore certificate errors: %w", err)
	}
	return browser, nil
}
