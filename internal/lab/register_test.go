package lab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	want := map[string]bool{"labdns": true, "labldap": true, "labtacacs": true, "labinfo": true, "labmail": true, "labmitm": true, "labgraph": true, "labntp": true}
	for _, name := range got {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("default profile servers = %v, missing %v", got, want)
	}
}

func TestDefaultProfileIntegrationGroupIncludesLabmitm(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "profiles", "default", "mcpjungle", "groups", "integration.json"))
	if err != nil {
		t.Fatal(err)
	}
	var group struct {
		IncludedServers []string `json:"included_servers"`
	}
	if err := json.Unmarshal(b, &group); err != nil {
		t.Fatal(err)
	}
	foundMITM, foundGraph, foundNTP := false, false, false
	for _, name := range group.IncludedServers {
		if name == "labmitm" {
			foundMITM = true
		}
		if name == "labgraph" {
			foundGraph = true
		}
		if name == "labntp" {
			foundNTP = true
		}
	}
	if !foundMITM || !foundGraph || !foundNTP {
		t.Fatalf("included_servers = %v, want labmitm, labgraph, and labntp", group.IncludedServers)
	}
}

func TestLoadTokensAndRegistrarEnvIncludeLabmitm(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"secrets/labdns-token":                                                      "dns-tok",
		"third_party/go-lab-ldap-mcp/secrets/token-admin":                           "ldap-tok",
		"third_party/go-lab-tacacs-mcp/deployments/compose/secrets/api_admin_token": "tac-tok",
		"secrets/labinfo-token":                                                     "info-tok",
		"secrets/labmail-token":                                                     "mail-tok",
		"secrets/labmitm-token":                                                     "mitm-tok",
		"secrets/labgraph-token":                                                    "graph-tok",
		"secrets/labntp-token":                                                      "ntp-tok",
		"secrets/mcp-client-token":                                                  "client-tok",
	}
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := &Runner{Root: root, Env: []string{"FOO=bar"}}
	tokens, err := r.loadTokens()
	if err != nil {
		t.Fatal(err)
	}
	if tokens["labmitm"] != "mitm-tok" {
		t.Fatalf("labmitm token = %q, want mitm-tok", tokens["labmitm"])
	}
	if tokens["labgraph"] != "graph-tok" {
		t.Fatalf("labgraph token = %q, want graph-tok", tokens["labgraph"])
	}
	if tokens["labntp"] != "ntp-tok" {
		t.Fatalf("labntp token = %q, want ntp-tok", tokens["labntp"])
	}
	env := r.registrarEnv(tokens)
	foundMITM, foundGraph, foundNTP := false, false, false
	for _, kv := range env {
		if kv == "LABMITM_TOKEN=mitm-tok" {
			foundMITM = true
		}
		if kv == "LABGRAPH_TOKEN=graph-tok" {
			foundGraph = true
		}
		if kv == "LABNTP_TOKEN=ntp-tok" {
			foundNTP = true
		}
	}
	if !foundMITM || !foundGraph || !foundNTP {
		t.Fatalf("registrarEnv missing LABMITM_TOKEN, LABGRAPH_TOKEN, or LABNTP_TOKEN: %v", env)
	}
}

func TestLoadTokensRequiresLabmitm(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"secrets/labdns-token":                                                      "dns-tok",
		"third_party/go-lab-ldap-mcp/secrets/token-admin":                           "ldap-tok",
		"third_party/go-lab-tacacs-mcp/deployments/compose/secrets/api_admin_token": "tac-tok",
		"secrets/labinfo-token":                                                     "info-tok",
		"secrets/labmail-token":                                                     "mail-tok",
		"secrets/mcp-client-token":                                                  "client-tok",
	}
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := &Runner{Root: root}
	_, err := r.loadTokens()
	if err == nil || !strings.Contains(err.Error(), "labmitm-token") {
		t.Fatalf("error = %v, want missing labmitm-token", err)
	}
}
