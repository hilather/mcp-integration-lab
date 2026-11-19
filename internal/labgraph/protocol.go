package labgraph

import (
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/server"
)

const MCPProtocolVersion = "2026-07-28"

// MCPStreamableServer builds the streamable-HTTP MCP handler for svc. When
// legacy clients are not allowed, MCPVersion admits only MCPProtocolVersion,
// so server/discover advertises only that version too (mcp-go v1.x
// advertises every version it implements by default).
func MCPStreamableServer(svc *Service, allowLegacy bool) *server.StreamableHTTPServer {
	var opts []server.StreamableHTTPOption
	if !allowLegacy {
		opts = append(opts, server.WithStreamableHTTPProtocolVersions(MCPProtocolVersion))
	}
	return server.NewStreamableHTTPServer(MCPServer(svc), opts...)
}

// MCPVersion requires MCP-Protocol-Version unless allowLegacy is set
// (MCPJungle cannot send the pin).
func MCPVersion(allowLegacy bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowLegacy {
			next.ServeHTTP(w, r)
			return
		}
		got := strings.TrimSpace(r.Header.Get("MCP-Protocol-Version"))
		if got != MCPProtocolVersion {
			http.Error(w, "MCP-Protocol-Version required: "+MCPProtocolVersion, http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}
