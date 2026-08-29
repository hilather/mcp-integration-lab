package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/labjenkins"
	"github.com/hilather/mcp-integration-lab/internal/profile"
	"gopkg.in/yaml.v3"
)

func TestApplyLabJenkinsEnvOverridesKeycloakDefaults(t *testing.T) {
	const (
		tenant  = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		api     = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
		gateway = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	)
	r := &Runner{
		Prof: &profile.Profile{
			Values: map[string]string{
				"LABJENKINS_ENABLED":   "true",
				"JWT_RS_JWKS_URL":      labjenkins.KeycloakJWKS,
				"JWT_RS_AUDIENCE":      labjenkins.KeycloakAudience,
				"LABJENKINS_IDP":       labjenkins.IDPKeycloak,
				"ENTRA_TENANT_ID":      tenant,
				"ENTRA_API_APP_ID":     api,
				"ENTRA_GATEWAY_APP_ID": gateway,
			},
		},
	}
	if err := r.applyLabJenkinsEnv(); err != nil {
		t.Fatal(err)
	}
	wantJWKS := "https://login.microsoftonline.com/" + tenant + "/discovery/v2.0/keys"
	if r.Prof.Values["JWT_RS_JWKS_URL"] != wantJWKS {
		t.Fatalf("Values JWKS = %q", r.Prof.Values["JWT_RS_JWKS_URL"])
	}
	if r.Prof.Values["JWT_RS_AUDIENCE"] != api {
		t.Fatalf("Values audience = %q", r.Prof.Values["JWT_RS_AUDIENCE"])
	}
	if r.Prof.Values["LABJENKINS_IDP"] != labjenkins.IDPEntra {
		t.Fatalf("idp = %q", r.Prof.Values["LABJENKINS_IDP"])
	}
	joined := strings.Join(r.Env, "\n")
	if !strings.Contains(joined, "JWT_RS_JWKS_URL="+wantJWKS) {
		t.Fatalf("r.Env missing Entra JWKS:\n%s", joined)
	}
	if strings.Contains(joined, "JWT_RS_JWKS_URL="+labjenkins.KeycloakJWKS) {
		t.Fatal("r.Env still has Keycloak JWKS")
	}
}

func TestApplyLabJenkinsEnvDisabledIsNoop(t *testing.T) {
	r := &Runner{
		Prof: &profile.Profile{
			Values: map[string]string{
				"LABJENKINS_ENABLED": "false",
				"JWT_RS_JWKS_URL":    labjenkins.KeycloakJWKS,
				"ENTRA_TENANT_ID":    "{tenant-id}",
			},
		},
	}
	if err := r.applyLabJenkinsEnv(); err != nil {
		t.Fatal(err)
	}
	if r.Prof.Values["JWT_RS_JWKS_URL"] != labjenkins.KeycloakJWKS {
		t.Fatal("disabled must not rewrite JWKS")
	}
}

func TestLabJenkinsDownMissingVendorIsNoop(t *testing.T) {
	r := &Runner{Root: t.TempDir()}
	if err := r.LabJenkinsDown(true); err != nil {
		t.Fatalf("missing vendor tree: %v", err)
	}
}

func TestLabJenkinsComposeArgs(t *testing.T) {
	r := &Runner{Root: "/repo"}
	args := r.labjenkinsComposeArgs("up", "-d")
	joined := strings.Join(args, "\n")
	for _, want := range []string{"-p", "labjenkins", labjenkinsComposeRel, "labjenkins.overlay.yaml"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
}

func TestLabJenkinsOverlayContract(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "compose", "labjenkins.overlay.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	services := yamlMappingValue(&root, "services")
	for _, name := range []string{"jenkins", "keycloak"} {
		svc := yamlMappingValue(services, name)
		if svc == nil {
			t.Fatalf("missing services.%s", name)
		}
		if yamlMappingValue(svc, "network_mode") != nil {
			t.Fatalf("services.%s must not set network_mode", name)
		}
		ports := yamlMappingValue(svc, "ports")
		if ports == nil || ports.Tag != "!override" {
			t.Fatalf("services.%s.ports tag = %v, want !override", name, ports)
		}
		if yamlSubtreeContains(ports, "127.0.0.1") {
			t.Fatalf("services.%s.ports bind loopback", name)
		}
	}
	if yamlMappingValue(services, "labjenkins") != nil {
		t.Fatal("overlay must use upstream keys jenkins/keycloak, not labjenkins")
	}
	nets := yamlMappingValue(&root, "networks")
	def := yamlMappingValue(nets, "default")
	if def == nil {
		t.Fatal("networks.default missing")
	}
	name := yamlMappingValue(def, "name")
	if name == nil || name.Value != "mcplab-shared" {
		t.Fatalf("networks.default.name = %#v, want mcplab-shared", name)
	}
}

func TestLabJenkinsUpRequiresEnabled(t *testing.T) {
	r := &Runner{Prof: &profile.Profile{Values: map[string]string{"LABJENKINS_ENABLED": "false"}}}
	if err := r.LabJenkinsUp(); err == nil {
		t.Fatal("expected error when disabled")
	}
}

func TestReloadLabJenkinsRequiresEnabled(t *testing.T) {
	r := &Runner{Prof: &profile.Profile{Values: map[string]string{"LABJENKINS_ENABLED": "false"}}}
	if err := r.reloadLabJenkins(); err == nil {
		t.Fatal("expected error when disabled")
	}
	if err := r.Reload("labjenkins"); err == nil {
		t.Fatal("Reload(labjenkins) must fail when disabled")
	}
}

func yamlSubtreeContains(n *yaml.Node, needle string) bool {
	if n == nil {
		return false
	}
	if strings.Contains(n.Value, needle) {
		return true
	}
	for _, c := range n.Content {
		if yamlSubtreeContains(c, needle) {
			return true
		}
	}
	return false
}

func TestSyncLabJenkinsDisabledDoesNotRequireVendor(t *testing.T) {
	r := &Runner{
		Root: t.TempDir(),
		Prof: &profile.Profile{Values: map[string]string{"LABJENKINS_ENABLED": "false"}},
	}
	if err := r.syncLabJenkins(); err != nil {
		t.Fatal(err)
	}
}
