package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/varianter/internal-mcp/internal/secrets"
)

// toolError wraps a message as an MCP tool error result.
func toolError(msg string) *mcp.CallToolResult {
	return mcp.NewToolResultError("Error: " + msg)
}

// fcSecret resolves a secret by checking the env var first (local dev), then
// falling back to Key Vault. Key Vault does not allow underscores in names,
// hence the two separate name parameters.
func fcSecret(ctx context.Context, loader *secrets.Loader, envName, kvName string) (string, error) {
	if v := os.Getenv(envName); v != "" {
		return v, nil
	}
	v, err := loader.Get(ctx, kvName)
	if err != nil {
		return "", fmt.Errorf("secret %s / Key Vault %s: %w", envName, kvName, err)
	}
	return v, nil
}
