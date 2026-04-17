# internal-mcp

> [!IMPORTANT]  
> Archived and replaced with https://github.com/varianter/mcp-internal-ts
> It is ported to typescript to use a more mature and fully featured MCP implementation


MCP server exposing Variant internal data via the [Model Context Protocol](https://modelcontextprotocol.io). Built with [mcp-go](https://mcp-go.dev/).

Deployed in AKS behind oauth2-proxy, using Azure Workload Identity (managed identity) for Azure/Graph access.

## Tools

| Name                    | Description                                                                                                                                                              | Required secrets                   |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------- |
| `get-cv-for-consultant` | Fetches a consultant's full CV from FlowCase by name. Returns a Markdown summary with profile, skills, work history, projects, education, certifications, and languages. | `FLOWCASE_API_KEY`, `FLOWCASE_ORG` |
| `random-joke`           | A random IT, programming, or design joke.                                                                                                                                |

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

For Azure Graph/SharePoint data, use `azidentity.NewDefaultAzureCredential()` — it falls through to `az login` locally and uses the managed identity in AKS automatically.

## Deployment

Run workflow manually to deploy to Variant AKS.
