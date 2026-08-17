package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/log"

	"github.com/aliwatters/rod-mcp/types"
)

// applyOverrides merges CLI flag values from subCfg into cfg,
// letting explicit CLI flags take precedence over the config file.
func applyOverrides(cfg *types.Config, subCfg *SubCfg) {
	if subCfg.HeadlessSet {
		cfg.Headless = subCfg.Headless
	}
	if subCfg.Mode != "" {
		cfg.Mode = subCfg.Mode
	}
	if subCfg.CDPEndpoint != "" {
		cfg.CDPEndpoint = subCfg.CDPEndpoint
	}
	if subCfg.ChromeDebugPort != "" {
		cfg.ChromeDebugPort = subCfg.ChromeDebugPort
	}
	if subCfg.UserDataDir != "" {
		cfg.UserDataDir = subCfg.UserDataDir
	}
	if domains := parseCloneDomains(subCfg.CloneDomains); len(domains) > 0 {
		cfg.CloneDomains = domains
	}
	if subCfg.NoClone {
		cfg.NoClone = true
	}
	if subCfg.CloneAll {
		cfg.CloneAll = true
	}
	if subCfg.CompactSnapshot {
		cfg.CompactSnapshot = true
	}
	if subCfg.OutputDir != "" {
		cfg.OutputDir = subCfg.OutputDir
	}
	if subCfg.OmitImages {
		cfg.ImageResponses = types.ImageResponsesOmit
	}
	if subCfg.BrowserBinPath != "" {
		cfg.BrowserBinPath = subCfg.BrowserBinPath
	}
	if subCfg.BrowserTempDir != "" {
		cfg.BrowserTempDir = subCfg.BrowserTempDir
	}
	if subCfg.LogFile != "" {
		cfg.LoggerConfig.LoggerFileName = subCfg.LogFile
	}
	if subCfg.NoSandbox {
		cfg.NoSandbox = true
	}
}

func main() {
	subCfg, err := RunCmd()
	if err != nil {
		log.Error(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := types.LoadConfig(subCfg.ConfigPath)
	if err != nil {
		log.Errorf("Load config error: %s", err)
		return
	}
	applyOverrides(cfg, subCfg)
	types.InitLogger(cfg.LoggerConfig)

	cfg.ServerVersion = Version

	runner := NewRunner(ctx, *cfg)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(c)
		<-c
		log.Info("Received signal, exiting...")
		cancel()
	}()
	runner.Run()

	defer func() {
		err := runner.Close()
		if err != nil {
			log.Errorf("Server close error: %s", err)
		}
	}()
	return
}
