package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/server"

	"github.com/aliwatters/rod-mcp/tools"
	"github.com/aliwatters/rod-mcp/types"
)

type Server struct {
	ctx       *types.Context
	mcpServer *server.MCPServer
}

func NewServer(stdCtx context.Context, cfg types.Config) *Server {
	ctx := types.NewContext(stdCtx, cfg)
	mcpServer := server.NewMCPServer(cfg.ServerName, cfg.ServerVersion)
	ser := &Server{
		ctx:       ctx,
		mcpServer: mcpServer,
	}
	switch ctx.CurrentMode() {
	case types.Text:
		ser.registerTools(tools.TextRegistrations)
	case types.Vision:
		ser.registerTools(tools.VisionRegistrations)
	}
	return ser
}

// registerTools registers all tools from the given Registrations into the MCP server.
// Each Registration is self-contained — it pairs a tool definition with its handler,
// so there is no risk of a tool being registered without a handler.
func (s *Server) registerTools(regs tools.Registrations) *Server {
	for _, reg := range regs {
		slog.Debug(fmt.Sprintf("register tool: %s", reg.Tool.Name))
		s.mcpServer.AddTool(reg.Tool, reg.Handler(s.ctx))
	}
	return s
}

func (s *Server) Start() error {
	return server.ServeStdio(s.mcpServer)
}

func (s *Server) Close() error {
	return s.ctx.Close()
}
