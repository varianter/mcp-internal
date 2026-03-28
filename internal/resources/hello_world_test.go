package resources

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/strowk/foxy-contexts/pkg/mcp"
)

func TestNewHelloWorldResource_read(t *testing.T) {
	resource := NewHelloWorldResource()
	if resource == nil {
		t.Fatal("NewHelloWorldResource returned nil")
	}

	const uri = "variant-internal://hello-world"
	result, err := resource.ReadResource(context.Background(), uri)
	if err != nil {
		t.Fatalf("ReadResource returned unexpected error: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Contents))
	}

	contents, ok := result.Contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatalf("expected mcp.TextResourceContents, got %T", result.Contents[0])
	}

	var payload helloWorldPayload
	if err := json.Unmarshal([]byte(contents.Text), &payload); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if payload.Organisation != "Variant" {
		t.Errorf("Organisation = %q, want %q", payload.Organisation, "Variant")
	}
	if payload.Message == "" {
		t.Error("Message is empty")
	}
	if contents.Uri != uri {
		t.Errorf("Uri = %q, want %q", contents.Uri, uri)
	}
}

func TestNewHelloWorldResource_metadata(t *testing.T) {
	resource := NewHelloWorldResource()
	meta := resource.GetResource(context.Background())

	if meta.Name != "hello-world" {
		t.Errorf("Name = %q, want %q", meta.Name, "hello-world")
	}
	if meta.Uri != "variant-internal://hello-world" {
		t.Errorf("Uri = %q, want %q", meta.Uri, "variant-internal://hello-world")
	}
	if meta.MimeType == nil || *meta.MimeType != "application/json" {
		t.Errorf("MimeType = %v, want %q", meta.MimeType, "application/json")
	}
}
