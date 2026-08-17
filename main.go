package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/log"

	"github.com/aliwatters/rod-mcp/types"
)

func main() {
	cfg, err := RunCmd()
	if err != nil {
		log.Error(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	types.InitLogger(cfg.LoggerConfig)

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
