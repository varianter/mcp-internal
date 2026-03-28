package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds runtime configuration loaded from environment variables.
// In AKS, these are injected via ConfigMap. Azure Workload Identity vars
// (AZURE_CLIENT_ID, AZURE_FEDERATED_TOKEN_FILE) are injected automatically
// by the AKS webhook and do not need to be set manually.
type Config struct {
	Host    string // default: "0.0.0.0"  — must be 0.0.0.0 in AKS
	Port    int    // default: 8080
	MCPPath string // default: "/mcp"

	AzureClientID string // AZURE_CLIENT_ID (set by workload identity webhook in AKS)
	AzureTenantID string // AZURE_TENANT_ID
}

func Load() (*Config, error) {
	host := "0.0.0.0"
	if h := os.Getenv("HOST"); h != "" {
		host = h
	}

	port := 8080
	if raw := os.Getenv("PORT"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT %q: %w", raw, err)
		}
		port = p
	}

	mcpPath := "/mcp"
	if p := os.Getenv("MCP_PATH"); p != "" {
		mcpPath = p
	}

	return &Config{
		Host:          host,
		Port:          port,
		MCPPath:       mcpPath,
		AzureClientID: os.Getenv("AZURE_CLIENT_ID"),
		AzureTenantID: os.Getenv("AZURE_TENANT_ID"),
	}, nil
}
