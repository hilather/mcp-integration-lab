package labgraph

import (
	"net/http"
	"strings"
)

const MCPProtocolVersion = "2026-07-28"

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
