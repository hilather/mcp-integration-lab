package labgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeYAML(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadScenarioEmptySpec(t *testing.T) {
	dir := t.TempDir()
	p := writeYAML(t, dir, "default.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: default
spec: {}
`)
	doc, err := LoadScenario(p)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Metadata.Name != "default" {
		t.Fatalf("name = %q", doc.Metadata.Name)
	}
	if sectionNonEmpty(doc.Spec.node("labdns")) {
		t.Fatal("empty spec must skip labdns")
	}
}

func TestLoadScenarioUnknownField(t *testing.T) {
	dir := t.TempDir()
	p := writeYAML(t, dir, "bad.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: bad
spec:
  jenkins: {}
`)
	if _, err := LoadScenario(p); err == nil || !strings.Contains(err.Error(), "jenkins") && !strings.Contains(err.Error(), "field") {
		t.Fatalf("want KnownFields reject, got %v", err)
	}
}

func TestLoadScenarioWrongKind(t *testing.T) {
	dir := t.TempDir()
	p := writeYAML(t, dir, "bad.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: DevCredentials
metadata:
  name: x
spec: {}
`)
	if _, err := LoadScenario(p); err == nil {
		t.Fatal("expected kind error")
	}
}

func TestRegistryParity(t *testing.T) {
	byID := map[string]Capability{}
	for _, c := range Registry() {
		byID[c.ID] = c
		if c.Disposition() == ParityRequired {
			if c.MCP == nil || c.MCP.Tool == "" {
				t.Errorf("%s missing MCP", c.ID)
			}
			if c.UI == nil {
				t.Errorf("%s missing UI", c.ID)
			}
			if len(c.REST) == 0 {
				t.Errorf("%s missing REST", c.ID)
			}
		}
		if c.Disposition() == RESTOnlyProtocol && c.MCP != nil {
			t.Errorf("%s REST_ONLY must not have MCP", c.ID)
		}
	}
	apply, ok := byID["fixture.apply"]
	if !ok || apply.MCP == nil || apply.MCP.Tool != "fixture_apply" || apply.RESTOnly {
		t.Fatalf("fixture.apply = %+v", apply)
	}
	if apply.REST[0].Path != "/v1/fixtures/{id}:apply" {
		t.Fatalf("fixture.apply REST %v", apply.REST)
	}
	get, ok := byID["fixture.get"]
	if !ok || !get.RESTOnly || get.MCP != nil {
		t.Fatalf("fixture.get must be REST_ONLY: %+v", get)
	}
	if _, ok := byID["fixture.list"]; ok {
		t.Fatal("fixture.list must not be in the registry")
	}
}
