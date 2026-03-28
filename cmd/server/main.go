package main

import (
	"errors"
	"log"
	"net/http"

	"github.com/strowk/foxy-contexts/pkg/app"
	"github.com/strowk/foxy-contexts/pkg/streamable_http"

	"github.com/varianter/internal-mcp/internal/config"
	"github.com/varianter/internal-mcp/internal/resources"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	err = app.NewBuilder().
		WithResource(resources.NewHelloWorldResource).
		WithName("variant-internal-mcp").
		WithVersion("0.1.0").
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
		log.Fatalf("server: %v", err)
	}
}
