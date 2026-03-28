package config

import (
	"testing"
)

func TestLoad_defaults(t *testing.T) {
	t.Setenv("HOST", "")
	t.Setenv("PORT", "")
	t.Setenv("MCP_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want %d", cfg.Port, 8080)
	}
	if cfg.MCPPath != "/mcp" {
		t.Errorf("MCPPath = %q, want %q", cfg.MCPPath, "/mcp")
	}
}

func TestLoad_envOverrides(t *testing.T) {
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "9090")
	t.Setenv("MCP_PATH", "/custom")
	t.Setenv("AZURE_CLIENT_ID", "client-abc")
	t.Setenv("AZURE_TENANT_ID", "tenant-xyz")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want %d", cfg.Port, 9090)
	}
	if cfg.MCPPath != "/custom" {
		t.Errorf("MCPPath = %q, want %q", cfg.MCPPath, "/custom")
	}
	if cfg.AzureClientID != "client-abc" {
		t.Errorf("AzureClientID = %q, want %q", cfg.AzureClientID, "client-abc")
	}
	if cfg.AzureTenantID != "tenant-xyz" {
		t.Errorf("AzureTenantID = %q, want %q", cfg.AzureTenantID, "tenant-xyz")
	}
}

func TestLoad_invalidPort(t *testing.T) {
	t.Setenv("PORT", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid PORT, got nil")
	}
}
