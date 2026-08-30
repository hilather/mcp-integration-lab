package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/lab"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s: %v", root, err)
	}
	return root
}

func runMcplab(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "./cmd/mcplab"}, args...)...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCLIUsageDocumentsReload(t *testing.T) {
	out, err := runMcplab(t)
	if err == nil {
		t.Fatal("expected non-zero exit for usage")
	}
	if !strings.Contains(out, "reload") {
		t.Fatalf("usage missing reload:\n%s", out)
	}
	for _, app := range lab.CanonicalReloadApps {
		if !strings.Contains(out, app) {
			t.Errorf("usage missing app %s:\n%s", app, out)
		}
	}
}

func TestCLIUsageDocumentsCreds(t *testing.T) {
	out, err := runMcplab(t)
	if err == nil {
		t.Fatal("expected non-zero exit for usage")
	}
	if !strings.Contains(out, "creds") {
		t.Fatalf("usage missing creds:\n%s", out)
	}
	if !strings.Contains(out, "dev mode only") {
		t.Fatalf("usage missing creds restriction:\n%s", out)
	}
}

func TestCLIUsageDocumentsScenario(t *testing.T) {
	out, err := runMcplab(t)
	if err == nil {
		t.Fatal("expected non-zero exit for usage")
	}
	if !strings.Contains(out, "scenario") {
		t.Fatalf("usage missing scenario:\n%s", out)
	}
	for _, op := range []string{"validate", "plan", "apply", "reset"} {
		if !strings.Contains(out, op) {
			t.Errorf("usage missing scenario op %s:\n%s", op, out)
		}
	}
	if !strings.Contains(out, "fixture") || !strings.Contains(out, "broken-bind") {
		t.Fatalf("usage missing fixture packs:\n%s", out)
	}
}

func TestCLIReloadRequiresApp(t *testing.T) {
	out, err := runMcplab(t, "reload")
	if err == nil {
		t.Fatal("expected non-zero exit for reload without app")
	}
	if !strings.Contains(out, "need an app name") {
		t.Errorf("missing 'need an app name':\n%s", out)
	}
	for _, app := range lab.CanonicalReloadApps {
		if !strings.Contains(out, app) {
			t.Errorf("reload usage missing %s:\n%s", app, out)
		}
	}
}

func TestCLIReloadUnknownApp(t *testing.T) {
	out, err := runMcplab(t, "reload", "not-a-service")
	if err == nil {
		t.Fatal("expected non-zero exit for unknown app")
	}
	if !strings.Contains(out, "unknown app") {
		t.Errorf("missing unknown-app error:\n%s", out)
	}
	for _, app := range lab.CanonicalReloadApps {
		if !strings.Contains(out, app) {
			t.Errorf("unknown-app error missing %s:\n%s", app, out)
		}
	}
}
