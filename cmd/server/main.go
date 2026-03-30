package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/strowk/foxy-contexts/pkg/app"
	"github.com/strowk/foxy-contexts/pkg/mcp"
	"github.com/strowk/foxy-contexts/pkg/streamable_http"
	"go.uber.org/fx"

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

	slog.Info("starting server", "host", cfg.Host, "port", cfg.Port, "path", cfg.MCPPath)

	healthServer := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.HealthPort),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}

	err = app.NewBuilder().
		WithFxOptions(
			fx.Provide(func() *secrets.Loader { return secretLoader }),
			fx.Invoke(func(lc fx.Lifecycle) {
				lc.Append(fx.Hook{
					OnStart: func(ctx context.Context) error {
						slog.Info("starting health server", "port", cfg.HealthPort)
						go func() {
							if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
								slog.Error("health server error", "error", err)
							}
						}()
						return nil
					},
					OnStop: func(ctx context.Context) error {
						return healthServer.Shutdown(ctx)
					},
				})
			}),
		).
		WithTool(tools.NewRandomJokeTool).
		WithTool(tools.NewFlowcaseCVTool).
		WithName("variant-internal-mcp").
		WithVersion("0.1.0").
		WithServerCapabilities(&mcp.ServerCapabilities{
			Tools: &mcp.ServerCapabilitiesTools{},
		}).
		WithTransport(
			streamable_http.NewTransport(
				streamable_http.Endpoint{
					Hostname: cfg.Host,
					Port:     cfg.Port,
					Path:     cfg.MCPPath,
				},
			),
		).
		Run()

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server exited unexpectedly", "error", err)
		os.Exit(1)
	}
}
