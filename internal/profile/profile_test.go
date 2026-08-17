package profile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// scaffold creates a fake repo root with the given profiles and .env content.
func scaffold(t *testing.T, dotenv string, profiles map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if dotenv != "" {
		if err := os.WriteFile(filepath.Join(root, ".env"), []byte(dotenv), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, env := range profiles {
		dir := filepath.Join(root, "profiles", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "profile.env"), []byte(env), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestPrecedenceProfileThenDotenvThenProcess(t *testing.T) {
	root := scaffold(t,
		"PROFILE=teamx\nLABDNS_DNS_PORT=20053\n",
		map[string]string{
			"teamx": "LABDNS_DNS_PORT=10053\nMCP_GATEWAY_PORT=9090\nNFS_PORT=20490\n",
		})

	p, err := Load(root, map[string]string{"NFS_PORT": "30000"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "teamx" {
		t.Fatalf("profile name = %q, want teamx", p.Name)
	}
	// .env overrides profile.env
	if got := p.Get("LABDNS_DNS_PORT", ""); got != "20053" {
		t.Errorf("LABDNS_DNS_PORT = %q, want 20053 (.env wins over profile)", got)
	}
	// profile.env supplies values .env does not
	if got := p.Get("MCP_GATEWAY_PORT", ""); got != "9090" {
		t.Errorf("MCP_GATEWAY_PORT = %q, want 9090", got)
	}
	// process env wins over both
	if got := p.Get("NFS_PORT", ""); got != "30000" {
		t.Errorf("NFS_PORT = %q, want 30000 (process env wins)", got)
	}
}

func TestProcessEnvSelectsProfile(t *testing.T) {
	root := scaffold(t, "PROFILE=teamx\n", map[string]string{
		"teamx": "A=1\n",
		"teamy": "A=2\n",
	})
	p, err := Load(root, map[string]string{"PROFILE": "teamy"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "teamy" || p.Get("A", "") != "2" {
		t.Fatalf("got profile %q A=%q, want teamy A=2", p.Name, p.Get("A", ""))
	}
}

func TestMissingProfileFails(t *testing.T) {
	root := scaffold(t, "", map[string]string{"default": ""})
	if _, err := Load(root, map[string]string{"PROFILE": "nope"}); err == nil {
		t.Fatal("expected error for unknown profile")
	}
}

func TestDerivedPathsAndFallback(t *testing.T) {
	root := scaffold(t, "", map[string]string{"default": ""})
	p, err := Load(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "default" {
		t.Fatalf("default profile expected, got %q", p.Name)
	}
	if p.Values["MCPLAB_PROFILE_DIR"] != p.Dir {
		t.Errorf("MCPLAB_PROFILE_DIR = %q, want %q", p.Values["MCPLAB_PROFILE_DIR"], p.Dir)
	}
	want := filepath.Join(p.Dir, "labldap", "scenario.yaml")
	if p.Values["LABLDAP_SCENARIO_FILE"] != want {
		t.Errorf("LABLDAP_SCENARIO_FILE = %q, want %q", p.Values["LABLDAP_SCENARIO_FILE"], want)
	}
	if !strings.HasSuffix(p.Values["LABLDAP_TLS_CA"], "secrets/tls/ca.crt") {
		t.Errorf("LABLDAP_TLS_CA = %q, want lab CA ca.crt", p.Values["LABLDAP_TLS_CA"])
	}
	if !strings.HasSuffix(p.Values["LABLDAP_TLS_DIR"], "secrets/tls") {
		t.Errorf("LABLDAP_TLS_DIR = %q", p.Values["LABLDAP_TLS_DIR"])
	}
	if !strings.HasSuffix(p.Values["LABLDAP_SECRETS_DIR"], "secrets") {
		t.Errorf("LABLDAP_SECRETS_DIR = %q", p.Values["LABLDAP_SECRETS_DIR"])
	}
	if !strings.HasSuffix(p.Values["LABLDAP_DM_PASSWORD_FILE"], "secrets/dm.pw") {
		t.Errorf("LABLDAP_DM_PASSWORD_FILE = %q", p.Values["LABLDAP_DM_PASSWORD_FILE"])
	}
	if got := p.Get("MISSING", "fb"); got != "fb" {
		t.Errorf("fallback = %q, want fb", got)
	}
}

func TestDevModeDrivesGatewayMode(t *testing.T) {
	cases := []struct {
		name    string
		profile string
		process map[string]string
		wantDev string
		wantMcp string
	}{
		{"default is hardened", "", nil, "false", "enterprise"},
		{"dev mode opens gateway", "LAB_DEV_MODE=true\n", nil, "true", "development"},
		{"explicit gateway mode wins", "LAB_DEV_MODE=true\nMCPJUNGLE_MODE=enterprise\n", nil, "true", "enterprise"},
		{"process env can flip dev mode", "", map[string]string{"LAB_DEV_MODE": "yes"}, "yes", "development"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := scaffold(t, "", map[string]string{"default": tc.profile})
			p, err := Load(root, tc.process)
			if err != nil {
				t.Fatal(err)
			}
			if got := p.Get("LAB_DEV_MODE", ""); got != tc.wantDev {
				t.Errorf("LAB_DEV_MODE = %q, want %q", got, tc.wantDev)
			}
			if got := p.Get("MCPJUNGLE_MODE", ""); got != tc.wantMcp {
				t.Errorf("MCPJUNGLE_MODE = %q, want %q", got, tc.wantMcp)
			}
		})
	}
}

func TestIsTrue(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", " yes ", "on"} {
		if !IsTrue(v) {
			t.Errorf("IsTrue(%q) = false", v)
		}
	}
	for _, v := range []string{"", "0", "false", "off", "nope"} {
		if IsTrue(v) {
			t.Errorf("IsTrue(%q) = true", v)
		}
	}
}

func TestEnvironMergesDeterministically(t *testing.T) {
	root := scaffold(t, "", map[string]string{"default": "B=profile\n"})
	p, err := Load(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := p.Environ([]string{"A=base", "B=base"})
	if !slices.Contains(env, "A=base") {
		t.Error("base-only key lost")
	}
	if !slices.Contains(env, "B=profile") {
		t.Error("profile did not override base")
	}
	if !slices.IsSorted(env) {
		t.Error("Environ output should be sorted for determinism")
	}
}
