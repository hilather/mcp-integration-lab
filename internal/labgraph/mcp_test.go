package labgraph

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestApplyReqFromMCP(t *testing.T) {
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"name":             "ops",
		"expectedRevision": `{"labdns":"r1"}`,
		"generation":       3,
	}}}
	got := applyReqFromMCP(req)
	if got.ExpectedRevision["labdns"] != "r1" {
		t.Fatalf("expectedRevision = %#v", got.ExpectedRevision)
	}
	if got.Generation == nil || *got.Generation != 3 {
		t.Fatalf("generation = %#v", got.Generation)
	}
	empty := applyReqFromMCP(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"name": "default"}}})
	if empty.Generation != nil || empty.ExpectedRevision != nil {
		t.Fatalf("omitted OCC fields must stay unset: %#v", empty)
	}
}
