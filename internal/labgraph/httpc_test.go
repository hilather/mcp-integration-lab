package labgraph

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewClientSendsAuthorizationBearer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labgraph-token")
	if err := os.WriteFile(path, []byte("unit-tok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		io.WriteString(w, `{"name":"default","ok":true,"order":[],"results":[]}`)
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Validate("default")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("result %+v", res)
	}
	if gotAuth != "Bearer unit-tok" {
		t.Fatalf("Authorization = %q, want Bearer unit-tok", gotAuth)
	}
	if gotPath != "/v1/scenarios/default:validate" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestNewClientMissingTokenFailClosed(t *testing.T) {
	_, err := NewClient("http://127.0.0.1:1", filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "labgraph token") {
		t.Fatalf("want fail-closed missing token, got %v", err)
	}
}
