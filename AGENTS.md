# AGENTS.md

Operational reference for agents (Claude Code, Copilot, etc.) working in this repository.

---

## What this is

An MCP (Model Context Protocol) server that exposes Variant internal data — SharePoint, Graph, HR systems, etc. — as MCP resources and tools. Clients (Claude Desktop, Cursor, etc.) connect to it and can read company data in context.

Built with [Foxy Contexts](https://foxy-contexts.str4.io/) (`github.com/strowk/foxy-contexts v0.1.0-beta.6`), deployed in AKS behind oauth2-proxy, using Azure Workload Identity for Graph/SharePoint access.

---

## Project layout

```
cmd/server/main.go            # Entry point — wire resources/tools/prompts here
internal/config/config.go     # Env-var config loader
internal/resources/           # One file per domain resource
internal/tools/               # (not yet created) MCP tools go here
k8s/                          # Kubernetes manifests
Dockerfile                    # Multi-stage, distroless/static:nonroot
Makefile                      # Dev and build targets
.air.toml                     # Air hot-reload config
.env.example                  # Local env var template
```

---

## Adding a resource

1. Create `internal/resources/<domain>.go` with a constructor that returns `fxctx.Resource`:

```go
package resources

import (
    "context"
    "github.com/strowk/foxy-contexts/pkg/fxctx"
    "github.com/strowk/foxy-contexts/pkg/mcp"
)

func ptr[T any](v T) *T { return &v }

func NewMyResource() fxctx.Resource {
    return fxctx.NewResource(
        mcp.Resource{
            Name:        "my-resource",
            Uri:         "variant-internal://my-resource",
            MimeType:    ptr("application/json"),
            Description: ptr("..."),
            Annotations: &mcp.ResourceAnnotations{
                Audience: []mcp.Role{mcp.RoleAssistant, mcp.RoleUser},
            },
        },
        func(_ context.Context, uri string) (*mcp.ReadResourceResult, error) {
            return &mcp.ReadResourceResult{
                Contents: []interface{}{
                    mcp.TextResourceContents{
                        MimeType: ptr("application/json"),
                        Text:     `{"key": "value"}`,
                        Uri:      uri,
                    },
                },
            }, nil
        },
    )
}
```

2. Register it in `cmd/server/main.go`:

```go
app.NewBuilder().
    WithResource(resources.NewHelloWorldResource).
    WithResource(resources.NewMyResource).   // add here
    ...
```

The `ptr[T]` helper is redeclared in each file that needs it — this is intentional (each file is self-contained). Do not create a shared `utils` package for it.

---

## Adding a tool

Create `internal/tools/<name>.go`:

```go
func NewMyTool() fxctx.Tool {
    return fxctx.NewTool(
        &mcp.Tool{
            Name:        "my-tool",
            Description: ptr("..."),
            InputSchema: mcp.ToolInputSchema{
                Type:       "object",
                Properties: map[string]map[string]interface{}{
                    "param": {"type": "string", "description": "..."},
                },
                Required: []string{"param"},
            },
        },
        func(args map[string]interface{}) *mcp.CallToolResult {
            param := args["param"].(string)
            return &mcp.CallToolResult{
                Content: []interface{}{
                    mcp.TextContent{Type: "text", Text: param},
                },
            }
        },
    )
}
```

Register with `.WithTool(tools.NewMyTool)` in `main.go`.

---

## Adding a dynamic resource provider

Use `fxctx.NewResourceProvider` when the list of resources is data-driven (e.g. one resource per SharePoint site). Register with `.WithResourceProvider(...)`.

---

## Configuration

All config comes from environment variables. See `internal/config/config.go`.

| Var | Default | Notes |
|---|---|---|
| `HOST` | `0.0.0.0` | Use `127.0.0.1` locally — **must** be `0.0.0.0` in AKS |
| `PORT` | `8080` | |
| `MCP_PATH` | `/mcp` | |
| `HEALTH_PORT` | `8081` | Liveness/readiness probe port — responds 200 OK to any request |
| `AZURE_TENANT_ID` | — | Set in `k8s/configmap.yaml` |
| `AZURE_CLIENT_ID` | — | Injected automatically by AKS Workload Identity webhook |
| `AZURE_FEDERATED_TOKEN_FILE` | — | Injected automatically by AKS Workload Identity webhook |

New config values: add a field to `Config` in `internal/config/config.go`, load from `os.Getenv`, and add to `k8s/configmap.yaml` (non-sensitive) or a Secret (sensitive).

---

## Azure / Managed Identity

In AKS, `azidentity.NewDefaultAzureCredential()` picks up the Workload Identity token automatically (via `AZURE_CLIENT_ID` + `AZURE_FEDERATED_TOKEN_FILE`). Locally it falls through to `az login`. No code changes needed between environments.

When adding Graph or SharePoint calls:

```go
import "github.com/Azure/azure-sdk-for-go/sdk/azidentity"

cred, err := azidentity.NewDefaultAzureCredential(nil)
```

Add the SDK as a dependency: `go get github.com/Azure/azure-sdk-for-go/sdk/azidentity`.

The ServiceAccount `internal-mcp-sa` in AKS must be annotated with the managed identity client ID:

```yaml
annotations:
  azure.workload.identity/client-id: "<managed-identity-client-id>"
```

---

## Transport

Uses `pkg/streamable_http` (Foxy Contexts v0.1.0-beta.6). This is an HTTP transport built on Echo — not stdio. The MCP endpoint is at `POST /mcp`. GET to `/mcp` returns 405 (correct behaviour).

oauth2-proxy sits in front in AKS. Ensure it does not strip `Mcp-Session-Id` headers, which the transport uses to maintain session state across requests.

A dedicated health server runs on port `8081` (configurable via `HEALTH_PORT`). It responds `200 OK` to any request. AKS liveness/readiness probes should target this port.

---

## Local development

```bash
make run      # HOST=127.0.0.1 go run ./cmd/server
make inspect  # npx @modelcontextprotocol/inspector --url http://localhost:8080/mcp
make dev      # Air hot-reload (go install github.com/air-verse/air@latest)
make test
make lint     # requires golangci-lint
```

Copy `.env.example` → `.env` for local overrides.

---

## Deployment

```bash
make docker-push TAG=<version>
# Update image tag in k8s/deployment.yaml
kubectl apply -f k8s/
```

Registry: `variantplatformacr.azurecr.io`
Namespace: `variant-internal`

The Dockerfile uses `gcr.io/distroless/static:nonroot` (uid 65532, no shell). The binary is statically linked (`CGO_ENABLED=0`).

---

## Foxy Contexts internals (things that bite)

- **Dependency injection**: The library uses Uber `fx`. Resource/tool/prompt constructors are passed as function references (not called instances) to `.WithResource()` etc. `fx` calls them and resolves dependencies. If a constructor needs config, add it as a parameter and provide `*Config` via `.WithFxOptions(fx.Provide(...))`.
- **`fx` logging**: The library logs all DI wiring at startup to stdout. This is normal.
- **Beta version**: `v0.1.0-beta.6` is intentional — it's the only release with `pkg/streamable_http`. Monitor the repo for a stable release.
- **Health server**: A separate `net/http` server on port `8081` handles liveness/readiness probes. It runs as an `fx.Hook` alongside the main app lifecycle. The foxy-contexts Echo instance is private, so health checks cannot be added to port 8080.
