package mcpout

import (
	"encoding/json"
	"strings"
	"testing"
)

// Regression fixture: verbatim shape of `mcpjungle invoke` output as of
// mcpjungle:latest (2026-08). If upstream changes its CLI framing, this test
// pins what our parser must keep handling (or be consciously updated).
const invokeFixture = `TIP: You can set ` + "`registry_url: http://mcpjungle:8080`" + ` in /cli-home/.mcpjungle.conf to avoid setting the --registry flag every time.

Response from tool:

** Content [text] **
{"zones":[{"id":"lab-zone","mode":"authoritative","name":"lab.test."}]}


** Structured Content **
{
  "zones": [
    {
      "id": "lab-zone"
    }
  ]
}
`

func TestExtractTextFromInvokeOutput(t *testing.T) {
	got, err := ExtractText(invokeFixture)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Zones []struct {
			ID string `json:"id"`
		} `json:"zones"`
	}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("extracted content is not JSON: %v\n%s", err, got)
	}
	if len(parsed.Zones) != 1 || parsed.Zones[0].ID != "lab-zone" {
		t.Fatalf("unexpected parse result: %+v", parsed)
	}
}

func TestExtractTextWithComposeNoise(t *testing.T) {
	noisy := " Container mcplab-registrar-run-1  Creating\n" + invokeFixture
	got, err := ExtractText(noisy)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "{") {
		t.Fatalf("expected JSON, got %q", got)
	}
}

func TestExtractTextErrorsWhenAbsent(t *testing.T) {
	if _, err := ExtractText("Error: tool not found\n"); err == nil {
		t.Fatal("expected error when no content block present")
	}
}

func TestExtractTextWithoutStructuredTail(t *testing.T) {
	out := "** Content [text] **\n{\"ok\":true}\n"
	got, err := ExtractText(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"ok":true}` {
		t.Fatalf("got %q", got)
	}
}
