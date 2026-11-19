package labgraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestMCPStreamableServerDiscoverVersions checks that server/discover
// advertises only the pinned version when legacy clients are refused, and
// the SDK's full list when they are allowed.
func TestMCPStreamableServerDiscoverVersions(t *testing.T) {
	const discover = `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientCapabilities":{},` +
		`"io.modelcontextprotocol/clientInfo":{"name":"test","version":"0"}}}}`
	for _, allowLegacy := range []bool{false, true} {
		svc := NewService(t.TempDir(), Clients{})
		ts := httptest.NewServer(MCPVersion(allowLegacy, MCPStreamableServer(svc, allowLegacy)))
		req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(discover))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", MCPProtocolVersion)
		req.Header.Set("Mcp-Method", "server/discover")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Result struct {
				SupportedVersions []string `json:"supportedVersions"`
			} `json:"result"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		ts.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("allowLegacy=%v: status %d, decode err %v", allowLegacy, resp.StatusCode, err)
		}
		got := out.Result.SupportedVersions
		if !allowLegacy {
			if !slices.Equal(got, []string{MCPProtocolVersion}) {
				t.Fatalf("strict supportedVersions = %v, want [%s]", got, MCPProtocolVersion)
			}
			continue
		}
		if !slices.Equal(got, mcp.ValidProtocolVersions) {
			t.Fatalf("legacy-allowed supportedVersions = %v, want SDK default %v", got, mcp.ValidProtocolVersions)
		}
	}
}
