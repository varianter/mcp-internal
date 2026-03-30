package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/varianter/internal-mcp/internal/config"
	"github.com/varianter/internal-mcp/internal/secrets"
	"github.com/varianter/internal-mcp/internal/tools"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	secretLoader, err := secrets.New(cfg.KeyVaultURL)
	if err != nil {
		slog.Error("failed to init secrets loader", "error", err)
		os.Exit(1)
	}

	mcpServer := server.NewMCPServer("variant-internal-mcp", "0.1.0",
		server.WithRecovery(),
		server.WithLogging(),
		server.WithInstructions("MCP for accessing internal tools for Variant. Getting employees, getting jokes, internal data, etc."),
	)

	randomJokeTool, randomJokeHandler := tools.NewRandomJokeTool()
	mcpServer.AddTool(randomJokeTool, randomJokeHandler)

	flowcaseTool, flowcaseHandler := tools.NewFlowcaseCVTool(secretLoader)
	mcpServer.AddTool(flowcaseTool, flowcaseHandler)

	flowcaseSearchTool, flowcaseSearchHandler := tools.NewFlowcaseSearchTool(secretLoader)
	mcpServer.AddTool(flowcaseSearchTool, flowcaseSearchHandler)

	githubExistsTool, githubExistsHandler := tools.NewGithubAppExistsTool(secretLoader)
	mcpServer.AddTool(githubExistsTool, githubExistsHandler)

	githubDeployTool, githubDeployHandler := tools.NewGithubDeployAppTool(secretLoader)
	mcpServer.AddTool(githubDeployTool, githubDeployHandler)

	streamableServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithSessionIdleTTL(10*time.Minute),
	)

	mux := http.NewServeMux()
	mux.Handle(cfg.MCPPath, streamableServer)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		slog.Info("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
		_ = streamableServer.Shutdown(ctx)
	}()

	slog.Info("starting server", "addr", addr, "mcp", cfg.MCPPath, "health", "/health")

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server exited unexpectedly", "error", err)
		os.Exit(1)
	}
}
