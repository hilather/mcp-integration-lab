package labinfo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/envfile"
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

func TestDefaultCatalogLabmitm(t *testing.T) {
	root := filepath.Join("..", "..")
	cat, err := Load(filepath.Join(root, "profiles", "default", "labinfo", "services.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var svc *Service
	for i := range cat.Services {
		if cat.Services[i].ID == "labmitm" {
			svc = &cat.Services[i]
			break
		}
	}
	if svc == nil {
		t.Fatal("default catalog missing labmitm")
	}
	if strings.Contains(strings.ToLower(svc.Description), "follow-on") {
		t.Fatalf("live catalog must not say compose-in is a follow-on: %q", svc.Description)
	}
	if svc.Connection == nil || len(svc.Connection.Endpoints) == 0 {
		t.Fatal("labmitm must have a connection block")
	}
	if svc.Credential == nil || svc.Credential.File != "/run/lab-secrets/labmitm-token" {
		t.Fatalf("credential file = %#v, want /run/lab-secrets/labmitm-token", svc.Credential)
	}
	if !strings.Contains(svc.Note, "exact Origins") || !strings.Contains(svc.Note, "${LABMITM_WEB_PORT}") {
		t.Fatalf("note missing exact-Origin / remote inspector sentence: %q", svc.Note)
	}
	tls := svc.Connection.Parameters["tls"]
	if !strings.Contains(tls, ":443") || !strings.Contains(tls, "tunnel-not-decrypt") {
		t.Fatalf("tls parameter missing intercept-:443 / CONNECT-tunnel sentence: %q", tls)
	}
	hosts := svc.Connection.Parameters["hosts"]
	for _, name := range []string{"*.lab", "labdns", "labinfo", "maildev", "mcpjungle", "control", "taclab"} {
		if !strings.Contains(hosts, name) {
			t.Fatalf("hosts = %q, missing %q", hosts, name)
		}
	}
	auth := svc.Connection.Parameters["auth"]
	if !strings.Contains(auth, "407") || !strings.Contains(auth, "off here") {
		t.Fatalf("auth parameter missing HTTP 407 opt-in/off sentence: %q", auth)
	}
	features := svc.Connection.Parameters["features"]
	if !strings.Contains(features, "11-row hop/accept") || !strings.Contains(features, "31") || !strings.Contains(features, "features.get") {
		t.Fatalf("features parameter missing native /v1 catalog 31 vs 11-row hop/accept: %q", features)
	}
	flags := svc.Connection.Parameters["flags"]
	if !strings.Contains(flags, "inspectFrames") || !strings.Contains(flags, "off") {
		t.Fatalf("flags parameter missing SOCKS/h2/inspectFrames off: %q", flags)
	}
}

func TestDefaultCatalogLabntp(t *testing.T) {
	root := filepath.Join("..", "..")
	cat, err := Load(filepath.Join(root, "profiles", "default", "labinfo", "services.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var svc *Service
	for i := range cat.Services {
		if cat.Services[i].ID == "labntp" {
			svc = &cat.Services[i]
			break
		}
	}
	if svc == nil {
		t.Fatal("default catalog missing labntp")
	}
	if svc.Connection == nil || len(svc.Connection.Endpoints) == 0 {
		t.Fatal("labntp must have a connection block")
	}
	foundUDP := false
	for _, e := range svc.Connection.Endpoints {
		if e.Protocol == "ntp-udp" && strings.Contains(e.Address, "${LABNTP_NTP_PORT}") {
			foundUDP = true
		}
	}
	if !foundUDP {
		t.Fatalf("labntp must catalog ntp-udp at ${LABNTP_NTP_PORT}: %+v", svc.Connection.Endpoints)
	}
	if svc.Credential == nil || svc.Credential.File != "/run/lab-secrets/labntp-token" {
		t.Fatalf("credential file = %#v, want /run/lab-secrets/labntp-token", svc.Credential)
	}
	if !strings.Contains(svc.Note, "10123") || !strings.Contains(svc.Note, "userland-proxy") {
		t.Fatalf("note missing dest-port / NAT-collision sentence: %q", svc.Note)
	}
	dest := svc.Connection.Parameters["dest_port"]
	if !strings.Contains(dest, "10123") || !strings.Contains(dest, "123") {
		t.Fatalf("dest_port parameter missing 10123 / 123 opt-in: %q", dest)
	}
}

func TestDefaultCatalogOperatorConsole(t *testing.T) {
	root := filepath.Join("..", "..")
	cat, err := Load(filepath.Join(root, "profiles", "default", "labinfo", "services.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	env, err := defaultProfileLookup(t)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cat.Render(false, env, func(string) (string, error) {
		t.Fatal("readSecret must not be called outside dev mode")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var dns *EndpointInfo
	for i := range got.Services {
		if got.Services[i].ID == "labdns" {
			dns = &got.Services[i]
			break
		}
	}
	if dns == nil {
		t.Fatal("default catalog missing labdns")
	}
	host := env("LAB_PUBLIC_HOST")
	port := env("LABDNS_REST_PORT")
	want := "http://" + host + ":" + port + "/"
	found := false
	for _, u := range dns.URLs {
		if u.Name == "Operator console" && u.URL == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("labdns URLs = %+v, want Operator console %s", dns.URLs, want)
	}
}

func defaultProfileLookup(t *testing.T) (func(string) string, error) {
	t.Helper()
	m, err := envfile.ParseFile(filepath.Join("..", "..", "profiles", "default", "profile.env"))
	if err != nil {
		return nil, err
	}
	return func(k string) string { return m[k] }, nil
}

func TestDefaultCatalogConnectionCredentialsRedactOutsideDev(t *testing.T) {
	root := filepath.Join("..", "..")
	cat, err := Load(filepath.Join(root, "profiles", "default", "labinfo", "services.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	env, err := defaultProfileLookup(t)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cat.RenderConnections(false, env, func(string) (string, error) {
		t.Fatal("readSecret must not be called outside dev mode")
		return "", nil
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"gateway": {
			"mcp-client-token",
		},
		"labdns": nil,
		"labldap": {
			"bind-password-alice",
			"lab-ca",
		},
		"labtacacs": {
			"radius-shared-secret",
			"tacacs-shared-secret",
			"lab-admin-password",
			"lab-admin-enable",
			"lab-readonly-password",
			"tacacs-client-ca",
			"tacacs-client-ok-cert",
		},
		"maildev": {
			"labmail-token",
		},
		"labmitm": {
			"labmitm-token",
		},
		"labgraph": {
			"labgraph-token",
		},
		"labntp": {
			"labntp-token",
		},
		"nfs": nil,
	}
	if len(got.Services) != len(want) {
		t.Fatalf("services = %d, want %d complete catalog (got ids %v)", len(got.Services), len(want), serviceIDs(got))
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), `"secret"`) {
		t.Errorf("wire format carries a secret field outside dev mode: %s", raw)
	}
	seen := map[string]bool{}
	for _, s := range got.Services {
		if s.ID == "labinfo" {
			t.Fatal("do not add a labinfo catalog service for labinfo-token")
		}
		seen[s.ID] = true
		exp, ok := want[s.ID]
		if !ok {
			t.Errorf("unexpected catalog service %q", s.ID)
			continue
		}
		gotNames := make([]string, 0, len(s.Credentials))
		for _, c := range s.Credentials {
			gotNames = append(gotNames, c.Name)
			if c.Secret != "" || c.Usage == "" {
				t.Errorf("%s credential %s: secret=%q usage=%q", s.ID, c.Name, c.Secret, c.Usage)
			}
		}
		if exp == nil {
			exp = []string{}
		}
		if !slices.Equal(gotNames, exp) {
			t.Errorf("%s credentials = %v, want complete set %v", s.ID, gotNames, exp)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("default catalog missing service %q", id)
		}
	}
}

func serviceIDs(got *Connections) []string {
	ids := make([]string, 0, len(got.Services))
	for _, s := range got.Services {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestConnectionsOptionalMissingFileInDevMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.yaml")
	body := `services:
  - id: labtacacs
    name: TacLab
    urls:
      - name: UI
        url: http://x/
    connection:
      endpoints:
        - name: TACACS+
          protocol: tacacs+
          address: x:49
      credentials:
        - name: tacacs-client-ca
          file: /run/lab-secrets/tacacs-client-ca.pem
          usage: client CA
          optional: true
        - name: radius-shared-secret
          file: /run/lab-secrets/labtacacs-radius-secret
          usage: RADIUS secret
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.RenderConnections(true, func(string) string { return "" }, func(p string) (string, error) {
		if p == "/run/lab-secrets/tacacs-client-ca.pem" {
			return "", os.ErrNotExist
		}
		if p == "/run/lab-secrets/labtacacs-radius-secret" {
			return "rad-secret\n", nil
		}
		t.Errorf("unexpected secret path %q", p)
		return "", os.ErrNotExist
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	creds := got.Services[0].Credentials
	if len(creds) != 2 {
		t.Fatalf("credentials = %+v", creds)
	}
	if creds[0].Name != "tacacs-client-ca" || creds[0].Secret != "" {
		t.Errorf("optional missing cert: %+v", creds[0])
	}
	if creds[1].Name != "radius-shared-secret" || creds[1].Secret != "rad-secret" {
		t.Errorf("required secret: %+v", creds[1])
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
