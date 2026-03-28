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
	Host       string // default: "0.0.0.0"  — must be 0.0.0.0 in AKS
	Port       int    // default: 8080
	MCPPath    string // default: "/mcp"
	HealthPort int    // default: 8081 — liveness/readiness probe port

	AzureClientID string // AZURE_CLIENT_ID (set by workload identity webhook in AKS)
	AzureTenantID string // AZURE_TENANT_ID
	KeyVaultURL   string // KEYVAULT_URL — set locally only; empty in k8s (secrets injected as env vars)
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
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("invalid PORT %q: must be between 1 and 65535", raw)
		}
		port = p
	}

	mcpPath := "/mcp"
	if p := os.Getenv("MCP_PATH"); p != "" {
		mcpPath = p
	}

	healthPort := 8081
	if raw := os.Getenv("HEALTH_PORT"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid HEALTH_PORT %q: %w", raw, err)
		}
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("invalid HEALTH_PORT %q: must be between 1 and 65535", raw)
		}
		healthPort = p
	}

	return &Config{
		Host:          host,
		Port:          port,
		MCPPath:       mcpPath,
		HealthPort:    healthPort,
		AzureClientID: os.Getenv("AZURE_CLIENT_ID"),
		AzureTenantID: os.Getenv("AZURE_TENANT_ID"),
		KeyVaultURL:   os.Getenv("KEYVAULT_URL"),
	}, nil
}
