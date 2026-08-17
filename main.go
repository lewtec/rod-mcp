package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/aliwatters/rod-mcp/types"
)

func main() {
	cfg, err := RunCmd()
	if err != nil {
		slog.Error("run command", "err", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	types.InitLogger(cfg.Verbose)

	runner := NewRunner(ctx, *cfg)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(c)
		<-c
		slog.Info("Received signal, exiting...")
		cancel()
	}()
	runner.Run()

	defer func() {
		err := runner.Close()
		if err != nil {
			slog.Error("server close", "err", err)
		}
	}()
	return
}
