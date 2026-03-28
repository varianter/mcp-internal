# internal-mcp

MCP server exposing Variant internal data via the [Model Context Protocol](https://modelcontextprotocol.io). Built with [Foxy Contexts](https://foxy-contexts.str4.io/).

Deployed in AKS behind oauth2-proxy, using Azure Workload Identity (managed identity) for Azure/Graph access.

## Tools

| Name | Description | Required secrets |
|------|-------------|-----------------|
| `get-cv-for-consultant` | Fetches a consultant's full CV from FlowCase by name. Returns a Markdown summary with profile, skills, work history, projects, education, certifications, and languages. | `FLOWCASE_API_KEY`, `FLOWCASE_ORG` |

## Resources

| URI | Description |
|-----|-------------|
| `variant-internal://random-joke` | A random IT, programming, or design joke. |

## Local development

**Prerequisites**: Go 1.23+, Node.js (for MCP Inspector)

```bash
# Start the server
make run

# In a second terminal, open MCP Inspector
make inspect
# → Open http://localhost:6274, click Connect
```

Hot-reload (requires [Air](https://github.com/air-verse/air)):

```bash
go install github.com/air-verse/air@latest
make dev
```

Copy `.env.example` to `.env` and adjust if you need specific values locally.

## Adding a resource

1. Create `internal/resources/<name>.go` with a `NewXxxResource() fxctx.Resource` function.
2. Register it in `cmd/server/main.go` with `.WithResource(resources.NewXxxResource)`.

For Azure Graph/SharePoint data, use `azidentity.NewDefaultAzureCredential()` — it falls through to `az login` locally and uses the managed identity in AKS automatically.

## Deployment

```bash
make docker-push TAG=<version>
# Then update the image tag in k8s/deployment.yaml and apply
kubectl apply -f k8s/
```

The Workload Identity webhook injects `AZURE_CLIENT_ID` and `AZURE_FEDERATED_TOKEN_FILE` into the pod automatically. Set `AZURE_TENANT_ID` in `k8s/configmap.yaml`.

The `ServiceAccount` `internal-mcp-sa` must be annotated with the managed identity client ID:

```yaml
annotations:
  azure.workload.identity/client-id: "<managed-identity-client-id>"
```
