package main

import (
	"context"
	"github.com/aliwatters/rod-mcp/types"
	"github.com/charmbracelet/log"
)

type Runner struct {
	server *Server
}

func NewRunner(ctx context.Context, cfg types.Config) *Runner {
	return &Runner{
		server: NewServer(ctx, cfg),
	}
}

func (r *Runner) Run() {
	err := r.server.Start()
	if err != nil {
		log.Errorf("Server start error: %s", err)
	}
}

func (r *Runner) Close() error {
	return r.server.Close()
}
