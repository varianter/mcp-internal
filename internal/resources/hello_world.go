package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/strowk/foxy-contexts/pkg/fxctx"
	"github.com/strowk/foxy-contexts/pkg/mcp"
)

func ptr[T any](v T) *T { return &v }

type helloWorldPayload struct {
	Message      string `json:"message"`
	Organisation string `json:"organisation"`
}

// NewHelloWorldResource is a placeholder resource that proves the server works
// end-to-end. Replace the handler body with real data as resources are added.
func NewHelloWorldResource() fxctx.Resource {
	return fxctx.NewResource(
		mcp.Resource{
			Name:        "hello-world",
			Uri:         "variant-internal://hello-world",
			MimeType:    ptr("application/json"),
			Description: ptr("Hello World placeholder for the Variant internal MCP server"),
			Annotations: &mcp.ResourceAnnotations{
				Audience: []mcp.Role{mcp.RoleAssistant, mcp.RoleUser},
			},
		},
		func(_ context.Context, uri string) (*mcp.ReadResourceResult, error) {
			payload := helloWorldPayload{
				Message:      "Hello from Variant's internal MCP server!",
				Organisation: "Variant",
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("marshal hello-world payload: %w", err)
			}
			return &mcp.ReadResourceResult{
				Contents: []any{
					mcp.TextResourceContents{
						MimeType: ptr("application/json"),
						Text:     string(raw),
						Uri:      uri,
					},
				},
			}, nil
		},
	)
}
