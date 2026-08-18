package lab

import (
	"os"
	"path/filepath"
	"testing"
)

func writeServers(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestServerNamesDiscovery(t *testing.T) {
	dir := writeServers(t, map[string]string{
		"labdns.json":  `{"name":"labdns","url":"http://labdns:8080/mcp"}`,
		"labinfo.json": `{"name":"labinfo","url":"http://labinfo:8080/mcp"}`,
		"notes.txt":    "ignored",
	})
	got, err := serverNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"labdns", "labinfo"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("serverNames = %v, want %v", got, want)
	}
}

func TestServerNamesRejectsBadConfigs(t *testing.T) {
	cases := map[string]map[string]string{
		"missing name":      {"a.json": `{"url":"http://a/mcp"}`},
		"filename mismatch": {"a.json": `{"name":"b"}`},
		"invalid json":      {"a.json": `{`},
		"empty dir":         {},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := serverNames(writeServers(t, files)); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestDefaultProfileRegistersLabmail(t *testing.T) {
	dir := filepath.Join("..", "..", "profiles", "default", "mcpjungle", "servers")
	got, err := serverNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"labdns": true, "labldap": true, "labtacacs": true, "labinfo": true, "labmail": true}
	for _, name := range got {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("default profile servers = %v, missing %v", got, want)
	}
}
