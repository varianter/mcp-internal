# AGENTS.md

Operational reference for agents (Claude Code, Copilot, etc.) working in this repository.

---

## What this is

An MCP (Model Context Protocol) server that exposes Variant internal data — SharePoint, Graph, HR systems, etc. — as MCP resources and tools. Clients (Claude Desktop, Cursor, etc.) connect to it and can read company data in context.

Built with [mcp-go](https://mcp-go.dev/) (`github.com/mark3labs/mcp-go`), deployed in AKS behind oauth2-proxy, using Azure Workload Identity for Graph/SharePoint access.

---

## Project layout

```
cmd/server/main.go            # Entry point — wire resources/tools here
internal/config/config.go     # Env-var config loader
internal/resources/           # One file per domain resource
internal/tools/               # One file per domain tool
k8s/                          # Kubernetes manifests
Dockerfile                    # Multi-stage, distroless/static:nonroot
Makefile                      # Dev and build targets
.air.toml                     # Air hot-reload config
.env.example                  # Local env var template
```

---

## Adding a tool

1. Create `internal/tools/<name>.go` with a constructor that returns the tool definition and its handler:

```go
package tools

import (
    "context"
    "github.com/mark3labs/mcp-go/mcp"
)

func NewMyTool() (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
    tool := mcp.NewTool("my-tool",
        mcp.WithDescription("Does something useful"),
        mcp.WithString("param",
            mcp.Required(),
            mcp.Description("The input parameter"),
        ),
    )
    handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        param := req.GetString("param", "")
        return mcp.NewToolResultText(param), nil
    }
    return tool, handler
}
```

2. Register it in `cmd/server/main.go`:

```go
tool, handler := tools.NewMyTool()
mcpServer.AddTool(tool, handler)
```

**Error handling:** Return tool-level errors with `mcp.NewToolResultError("message"), nil` — the `nil` Go error keeps the handler alive while surfacing the error to the LLM. Only return a non-nil Go error for unexpected panics/infrastructure failures.

**Parameter extraction:**
- `req.GetString("name", "default")` — optional string with fallback
- `req.GetInt("count", 0)` — optional int
- `req.GetBool("flag", false)` — optional bool
- `req.RequireString("name")` — required; returns `(string, error)` if missing

---

## Adding a resource

1. Create `internal/resources/<domain>.go`:

```go
package resources

import (
    "context"
    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

func NewMyResource() (mcp.Resource, func(context.Context, mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)) {
    resource := mcp.NewResource("variant-internal://my-resource", "My Resource",
        mcp.WithResourceDescription("..."),
        mcp.WithMIMEType("application/json"),
    )
    handler := func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
        return mcp.NewReadResourceResultFromText(`{"key": "value"}`), nil
    }
    return resource, handler
}
```

2. Register it in `cmd/server/main.go`:

```go
resource, handler := resources.NewMyResource()
mcpServer.AddResource(resource, handler)
```

---

## Configuration

All config comes from environment variables. See `internal/config/config.go`.

| Var | Default | Notes |
|---|---|---|
| `HOST` | `0.0.0.0` | Use `127.0.0.1` locally — **must** be `0.0.0.0` in AKS |
| `PORT` | `8080` | |
| `MCP_PATH` | `/mcp` | HTTP endpoint path for MCP |
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

Uses `server.NewStreamableHTTPServer` from mcp-go — not stdio. Both endpoints are on the same port:

- `POST /mcp` (configurable via `MCP_PATH`) — MCP endpoint
- `GET /health` — liveness/readiness probe; returns `200 OK`

oauth2-proxy sits in front in AKS. Ensure it does not strip `Mcp-Session-Id` headers, which the transport uses to maintain session state across requests. AKS liveness/readiness probes should target port 8080 at `/health`.

**Note:** The mcp-go docs claim `GET /mcp/health` is auto-created — it is not. Health is a plain `http.ServeMux` handler added alongside the MCP handler in `main.go`.

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

## mcp-go internals (things to know)

- **No DI framework**: mcp-go has no dependency injection. Tool/resource constructors are plain functions. Pass dependencies (e.g. `*secrets.Loader`) as constructor arguments; call the constructor directly in `main.go`.
- **Tool constructors return two values**: `(mcp.Tool, handlerFunc)`. Register both with `mcpServer.AddTool(tool, handler)`.
- **Error model**: Tool errors go in `mcp.NewToolResultError(msg)` returned with `nil` Go error. The LLM sees the error message. A non-nil Go error from a handler is a protocol-level failure.
- **Server options**: Use `server.WithRecovery()` to prevent panics from crashing the server. Use `server.WithLogging()` to enable built-in request logging.
- **Endpoint path**: Configured via `server.WithEndpointPath(path)` on `NewStreamableHTTPServer`.
- **Session TTL**: `server.WithSessionIdleTTL(10*time.Minute)` prevents memory leaks from clients that disconnect without sending a DELETE request. The sweeper runs in the background.
- **Graceful shutdown**: Call both `httpSrv.Shutdown(ctx)` (stops accepting connections) and `streamableServer.Shutdown(ctx)` (cleans up MCP sessions).
- **Health endpoint**: The mcp-go docs list `GET /mcp/health` as auto-created — it is not. Add it manually to the `http.ServeMux` in `main.go`.
