package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/strowk/foxy-contexts/pkg/app"
	"github.com/strowk/foxy-contexts/pkg/mcp"
	"github.com/strowk/foxy-contexts/pkg/streamable_http"

	"github.com/varianter/internal-mcp/internal/config"
	"github.com/varianter/internal-mcp/internal/resources"
	"github.com/varianter/internal-mcp/internal/secrets"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
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
	_ = secretLoader // pass to resources/tools that need secrets

	slog.Info("starting server", "host", cfg.Host, "port", cfg.Port, "path", cfg.MCPPath)

	err = app.NewBuilder().
		WithResource(resources.NewRandomJokeResource).
		WithName("variant-internal-mcp").
		WithVersion("0.1.0").
		WithServerCapabilities(&mcp.ServerCapabilities{
			Resources: &mcp.ServerCapabilitiesResources{},
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
