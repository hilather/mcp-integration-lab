package taclabcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const labgenSnippet = `
schema_version: 1
api:
  mode: lab_static_bearer
  mcp:
    allowed_origins: []
    require_origin: false
  bootstrap_tokens: []
`

func TestEnableLegacyClientsSetsKnob(t *testing.T) {
	out, err := EnableLegacyClients([]byte(labgenSnippet))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		API struct {
			MCP struct {
				AllowLegacyClients bool `yaml:"allow_legacy_clients"`
				RequireOrigin      bool `yaml:"require_origin"`
			} `yaml:"mcp"`
		} `yaml:"api"`
	}
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !got.API.MCP.AllowLegacyClients {
		t.Fatalf("allow_legacy_clients not set:\n%s", out)
	}
	if got.API.MCP.RequireOrigin {
		t.Fatal("require_origin must stay false")
	}
}

func TestEnableLegacyClientsIdempotent(t *testing.T) {
	once, err := EnableLegacyClients([]byte(labgenSnippet))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := EnableLegacyClients(once)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(twice), "allow_legacy_clients: true") {
		t.Fatalf("second pass lost the knob:\n%s", twice)
	}
}

func TestEnableLegacyClientsRejectsMissingMCP(t *testing.T) {
	if _, err := EnableLegacyClients([]byte("schema_version: 1\n")); err == nil {
		t.Fatal("expected error for missing api.mcp")
	}
}

func TestEnableLegacyClientsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "taclab.combined.yaml"), []byte(labgenSnippet), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnableLegacyClientsDir(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "taclab.combined.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "allow_legacy_clients: true") {
		t.Fatalf("dir rewrite missed the knob:\n%s", b)
	}
}
