package secrets

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
)

// Loader retrieves secrets from env vars (k8s) or Key Vault (local dev).
// In k8s, KEYVAULT_URL is not set and secrets are injected as env vars via values.yaml.
// Locally, KEYVAULT_URL is set and DefaultAzureCredential uses `az login`.
type Loader struct {
	kv *azsecrets.Client
}

func New(vaultURL string) (*Loader, error) {
	if vaultURL == "" {
		return &Loader{}, nil
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("default credential: %w", err)
	}
	client, err := azsecrets.NewClient(vaultURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("keyvault client: %w", err)
	}
	return &Loader{kv: client}, nil
}

// Get returns the value for name. Env var is checked first (k8s path);
// if not set, falls back to Key Vault (local dev path).
// Use the same name for both the env var and the Key Vault secret name.
func (l *Loader) Get(ctx context.Context, name string) (string, error) {
	if v := os.Getenv(name); v != "" {
		return v, nil
	}
	if l.kv == nil {
		return "", fmt.Errorf("secret %q: env var not set and no KEYVAULT_URL configured", name)
	}
	resp, err := l.kv.GetSecret(ctx, name, "", nil)
	if err != nil {
		return "", fmt.Errorf("keyvault get %q: %w", name, err)
	}
	return *resp.Value, nil
}
