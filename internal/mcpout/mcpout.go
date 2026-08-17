// Package mcpout parses human-oriented output of the mcpjungle CLI so the lab
// can drive tools programmatically through the gateway.
package mcpout

import (
	"fmt"
	"strings"
)

const (
	textMarker       = "** Content [text] **"
	structuredMarker = "** Structured Content **"
)

// ExtractText returns the JSON text content of an `mcpjungle invoke` output:
// the lines between "** Content [text] **" and "** Structured Content **"
// (or end of output), with blank lines dropped.
func ExtractText(cliOutput string) (string, error) {
	lines := strings.Split(cliOutput, "\n")
	var body []string
	in := false
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		switch {
		case strings.Contains(trimmed, textMarker):
			in = true
		case strings.Contains(trimmed, structuredMarker):
			in = false
		case in && trimmed != "":
			body = append(body, trimmed)
		}
	}
	if len(body) == 0 {
		return "", fmt.Errorf("no text content found in tool response:\n%s", strings.TrimSpace(cliOutput))
	}
	return strings.Join(body, "\n"), nil
}
