package labinfo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCatalog = `
services:
  - id: gateway
    name: MCP gateway
    description: Single MCP endpoint.
    urls:
      - name: MCP endpoint
        url: http://${LAB_PUBLIC_HOST}:${MCP_GATEWAY_PORT}/mcp
    credential:
      file: /run/lab-secrets/mcp-client-token
      usage: "Authorization: Bearer <secret> (host ${LAB_PUBLIC_HOST})"
    connection:
      endpoints:
        - name: MCP
          protocol: mcp-streamable-http
          address: http://${LAB_PUBLIC_HOST}:${MCP_GATEWAY_PORT}/mcp
      credentials:
        - name: client-token
          file: /run/lab-secrets/mcp-client-token
          usage: bearer token for enterprise mode
  - id: nfs
    name: NFS export
    urls:
      - name: NFS endpoint
        url: nfs://${LAB_PUBLIC_HOST}:${NFS_PORT}/
    note: "mount -o vers=3,tcp,nolock,port=${NFS_PORT},mountport=${NFS_PORT}"
    connection:
      endpoints:
        - name: NFSv3
          protocol: nfs-tcp
          address: ${LAB_PUBLIC_HOST}:${NFS_PORT}
          note: nfsd and mountd share the port
      parameters:
        mount_options: vers=3,tcp,nolock,port=${NFS_PORT},mountport=${NFS_PORT}
        auth: AUTH_SYS (no credential)
`

func loadTest(t *testing.T) *Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "services.yaml")
	if err := os.WriteFile(path, []byte(testCatalog), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testLookup(k string) string {
	return map[string]string{
		"LAB_PUBLIC_HOST":  "lab.example.com",
		"MCP_GATEWAY_PORT": "8080",
		"NFS_PORT":         "20490",
	}[k]
}

func TestRenderExpandsVariables(t *testing.T) {
	c := loadTest(t)
	got, err := c.Render(false, testLookup, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Services[0].URLs[0].URL != "http://lab.example.com:8080/mcp" {
		t.Errorf("gateway url = %q", got.Services[0].URLs[0].URL)
	}
	if want := "mount -o vers=3,tcp,nolock,port=20490,mountport=20490"; got.Services[1].Note != want {
		t.Errorf("nfs note = %q, want %q", got.Services[1].Note, want)
	}
}

// Regression: credentials must never appear in non-dev output, and the JSON
// wire format must not even carry an empty secret field.
func TestRenderRedactsWithoutDevMode(t *testing.T) {
	c := loadTest(t)
	got, err := c.Render(false, testLookup, func(string) (string, error) {
		t.Fatal("readSecret must not be called outside dev mode")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DevMode || got.Note == "" {
		t.Errorf("devMode=%v note=%q", got.DevMode, got.Note)
	}
	if got.Services[0].Credential != nil {
		t.Error("credential leaked outside dev mode")
	}
	if want := "Authorization: Bearer <secret> (host lab.example.com)"; got.Services[0].Auth != want {
		t.Errorf("auth usage = %q, want expanded %q", got.Services[0].Auth, want)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), `"credential"`) {
		t.Errorf("wire format carries a credential object outside dev mode: %s", raw)
	}
}

func TestRenderRevealsInDevMode(t *testing.T) {
	c := loadTest(t)
	got, err := c.Render(true, testLookup, func(path string) (string, error) {
		if path != "/run/lab-secrets/mcp-client-token" {
			t.Errorf("unexpected secret path %q", path)
		}
		return "tok-123\n", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cred := got.Services[0].Credential
	if cred == nil || cred.Secret != "tok-123" {
		t.Fatalf("credential = %+v, want trimmed tok-123", cred)
	}
	if got.Services[1].Credential != nil {
		t.Error("credential invented for service without one")
	}
}

func TestConnectionsExpandVariables(t *testing.T) {
	c := loadTest(t)
	got, err := c.RenderConnections(false, testLookup, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 2 {
		t.Fatalf("services = %d, want 2", len(got.Services))
	}
	gw := got.Services[0]
	if gw.Endpoints[0].Address != "http://lab.example.com:8080/mcp" {
		t.Errorf("gateway endpoint = %q", gw.Endpoints[0].Address)
	}
	if gw.Endpoints[0].Protocol != "mcp-streamable-http" {
		t.Errorf("gateway protocol = %q", gw.Endpoints[0].Protocol)
	}
	nfs := got.Services[1]
	if want := "vers=3,tcp,nolock,port=20490,mountport=20490"; nfs.Parameters["mount_options"] != want {
		t.Errorf("mount_options = %q, want %q", nfs.Parameters["mount_options"], want)
	}
}

// Regression: connection secrets must never appear (nor be read) outside dev
// mode, but the credential name/usage must still be served so agents can
// explain how to obtain them.
func TestConnectionsRedactWithoutDevMode(t *testing.T) {
	c := loadTest(t)
	got, err := c.RenderConnections(false, testLookup, func(string) (string, error) {
		t.Fatal("readSecret must not be called outside dev mode")
		return "", nil
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.DevMode || got.Note == "" {
		t.Errorf("devMode=%v note=%q", got.DevMode, got.Note)
	}
	creds := got.Services[0].Credentials
	if len(creds) != 1 || creds[0].Name != "client-token" || creds[0].Usage == "" {
		t.Fatalf("credentials = %+v, want name+usage without secret", creds)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), `"secret"`) {
		t.Errorf("wire format carries a secret field outside dev mode: %s", raw)
	}
}

func TestConnectionsRevealInDevMode(t *testing.T) {
	c := loadTest(t)
	got, err := c.RenderConnections(true, testLookup, func(path string) (string, error) {
		if path != "/run/lab-secrets/mcp-client-token" {
			t.Errorf("unexpected secret path %q", path)
		}
		return "tok-456\n", nil
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	creds := got.Services[0].Credentials
	if len(creds) != 1 || creds[0].Secret != "tok-456" {
		t.Fatalf("credentials = %+v, want trimmed tok-456", creds)
	}
	if len(got.Services[1].Credentials) != 0 {
		t.Error("credentials invented for service without any")
	}
}

func TestConnectionsFilterByService(t *testing.T) {
	c := loadTest(t)
	got, err := c.RenderConnections(false, testLookup, nil, "nfs")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 1 || got.Services[0].ID != "nfs" {
		t.Fatalf("filtered services = %+v, want just nfs", got.Services)
	}

	_, err = c.RenderConnections(false, testLookup, nil, "bogus")
	if err == nil || !strings.Contains(err.Error(), "gateway, nfs") {
		t.Errorf("unknown service error = %v, want list of known ids", err)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	valid := func(mutate string) string {
		base := `services:
  - id: x
    name: X
    urls:
      - name: u
        url: http://x/
    connection:
      endpoints:
        - name: e
          protocol: p
          address: x:1
`
		return base + mutate
	}
	for name, body := range map[string]string{
		"empty.yaml":  "services: []\n",
		"no-url.yaml": "services:\n  - id: x\n    name: X\n",
		// Fail-closed: every service must document its connection details.
		"no-connection.yaml": "services:\n  - id: x\n    name: X\n    urls:\n      - name: u\n        url: http://x/\n",
		"no-endpoints.yaml":  "services:\n  - id: x\n    name: X\n    urls:\n      - name: u\n        url: http://x/\n    connection:\n      parameters: {a: b}\n",
		"endpoint-no-protocol.yaml": `services:
  - id: x
    name: X
    urls:
      - name: u
        url: http://x/
    connection:
      endpoints:
        - name: e
          address: x:1
`,
		"conn-cred-no-file.yaml": valid(`      credentials:
        - name: c
          usage: something
`),
	} {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
