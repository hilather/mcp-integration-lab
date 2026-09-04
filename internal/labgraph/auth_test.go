package labgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiredTokenRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadRequiredToken(""); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("empty path: %v", err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRequiredToken(empty); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("whitespace file: %v", err)
	}
	ok := filepath.Join(dir, "ok")
	if err := os.WriteFile(ok, []byte("lab-dev-labgraph-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRequiredToken(ok)
	if err != nil || got != "lab-dev-labgraph-token" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestParseLDAPControlOpsAllowlist(t *testing.T) {
	ops, err := parseLDAPControlOps(map[string]any{
		"operations": []any{map[string]any{"op": "disableUser", "id": "alice"}},
		"reason":     "fixture",
	})
	if err != nil || len(ops) != 1 || ops[0].ID != "alice" {
		t.Fatalf("reason allowed: %v %+v", err, ops)
	}
	if _, err := parseLDAPControlOps(map[string]any{
		"operations": []any{map[string]any{"op": "disableUser", "id": "alice"}},
		"spec":       map[string]any{"users": []any{map[string]any{"id": "bob"}}},
	}); err == nil || !strings.Contains(err.Error(), errNoLDAPApply) {
		t.Fatalf("spec must fail: %v", err)
	}
}
