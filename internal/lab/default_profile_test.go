package lab

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/envfile"
	"github.com/hilather/mcp-integration-lab/internal/profile"
	"gopkg.in/yaml.v3"
)

func defaultProfileDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "profiles", "default")
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDefaultLabDNSBootstrapEnablesOperatorConsole(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(defaultProfileDir(t), "labdns", "bootstrap.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Spec       struct {
			UI struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"ui"`
			Management struct {
				MCP struct {
					AllowLegacyClients bool `yaml:"allowLegacyClients"`
				} `yaml:"mcp"`
			} `yaml:"management"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.APIVersion != "labdns.dev/v1alpha1" || doc.Kind != "LabDNS" {
		t.Fatalf("apiVersion=%q kind=%q", doc.APIVersion, doc.Kind)
	}
	if !doc.Spec.UI.Enabled {
		t.Fatal("profiles/default/labdns/bootstrap.yaml: spec.ui.enabled must be true (LabDNS operator console)")
	}
	if !doc.Spec.Management.MCP.AllowLegacyClients {
		t.Fatal("profiles/default/labdns/bootstrap.yaml: spec.management.mcp.allowLegacyClients must be true so MCPJungle can register")
	}
}

func TestDefaultLabMITMBootstrap(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(defaultProfileDir(t), "labmitm", "bootstrap.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(b)
	if !strings.Contains(raw, "lab-owned copy") || !strings.Contains(raw, "do not recopy from the upstream examples tree") {
		t.Fatal("bootstrap header must say lab-owned copy; do not recopy from the upstream examples tree")
	}
	for _, stale := range []string{
		"KnownFields on a v1.1.1",
		"omit spec.proxy.httpAuth",
		"must stay omitted",
		"httpAuth must stay omitted",
	} {
		if strings.Contains(raw, stale) {
			t.Fatalf("bootstrap must not keep stale omit wording %q", stale)
		}
	}
	var doc struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Spec       struct {
			Listeners struct {
				Proxy struct {
					Address string `yaml:"address"`
				} `yaml:"proxy"`
				Management struct {
					Address string `yaml:"address"`
				} `yaml:"management"`
			} `yaml:"listeners"`
			Proxy struct {
				Targets struct {
					AllowHosts []string `yaml:"allowHosts"`
				} `yaml:"targets"`
			} `yaml:"proxy"`
			TLS struct {
				Intercept bool  `yaml:"intercept"`
				Ports     []int `yaml:"ports"`
			} `yaml:"tls"`
			Management struct {
				Auth struct {
					Tokens []struct {
						SecretFile string `yaml:"secretFile"`
					} `yaml:"tokens"`
				} `yaml:"auth"`
				MCP struct {
					AllowLegacyClients bool `yaml:"allowLegacyClients"`
				} `yaml:"mcp"`
				OriginAllowlist []string `yaml:"originAllowlist"`
			} `yaml:"management"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.APIVersion != "labmitm.dev/v1alpha1" || doc.Kind != "LabMITM" {
		t.Fatalf("apiVersion=%q kind=%q", doc.APIVersion, doc.Kind)
	}
	if doc.Spec.Listeners.Proxy.Address != ":8888" || doc.Spec.Listeners.Management.Address != ":8088" {
		t.Fatalf("listeners proxy=%q management=%q", doc.Spec.Listeners.Proxy.Address, doc.Spec.Listeners.Management.Address)
	}
	if !doc.Spec.Management.MCP.AllowLegacyClients {
		t.Fatal("spec.management.mcp.allowLegacyClients must be true")
	}
	if len(doc.Spec.Management.Auth.Tokens) != 1 || doc.Spec.Management.Auth.Tokens[0].SecretFile != "/run/secrets/labmitm-token" {
		t.Fatalf("token secretFile = %#v, want /run/secrets/labmitm-token", doc.Spec.Management.Auth.Tokens)
	}
	if !doc.Spec.TLS.Intercept || len(doc.Spec.TLS.Ports) != 1 || doc.Spec.TLS.Ports[0] != 443 {
		t.Fatalf("tls intercept=%v ports=%v, want intercept :443", doc.Spec.TLS.Intercept, doc.Spec.TLS.Ports)
	}
	wantHosts := []string{"*.lab", "labdns", "labinfo", "maildev", "mcpjungle", "control", "taclab"}
	if len(doc.Spec.Proxy.Targets.AllowHosts) != len(wantHosts) {
		t.Fatalf("allowHosts = %v, want %v", doc.Spec.Proxy.Targets.AllowHosts, wantHosts)
	}
	for i, h := range wantHosts {
		if doc.Spec.Proxy.Targets.AllowHosts[i] != h {
			t.Fatalf("allowHosts = %v, want %v", doc.Spec.Proxy.Targets.AllowHosts, wantHosts)
		}
	}
	forbidden := map[string]bool{"labldap": true, "labtacacs": true, "nfs": true, "directory": true, "labmitm": true}
	for _, h := range doc.Spec.Proxy.Targets.AllowHosts {
		if forbidden[h] {
			t.Fatalf("allowHosts must not list %q", h)
		}
	}
	for _, o := range doc.Spec.Management.OriginAllowlist {
		if o == "*" || o == "private" {
			t.Fatalf("originAllowlist must not contain %q", o)
		}
	}
	if len(doc.Spec.Management.OriginAllowlist) != 0 {
		t.Fatalf("originAllowlist = %v, want empty", doc.Spec.Management.OriginAllowlist)
	}

	if err := requireLabMITM15Knobs(b); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultLabLDAPScenarioInsecureLabMode(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(defaultProfileDir(t), "labldap", "scenario.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Spec struct {
			Transport struct {
				InsecureLabMode bool `yaml:"insecureLabMode"`
			} `yaml:"transport"`
			Management map[string]any `yaml:"management"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Spec.Transport.InsecureLabMode {
		t.Fatal("profiles/default/labldap/scenario.yaml: transport.insecureLabMode must stay true (lab-grade)")
	}
	if _, ok := doc.Spec.Management["allowedHosts"]; ok {
		t.Fatal("spec.management.allowedHosts must stay omitted; Host extras come from overlay LABLDAP_MANAGEMENT_ALLOWED_HOSTS")
	}
}

func TestDefaultLabMailOmitsSMTPBehavior(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(defaultProfileDir(t), "labmail", "bootstrap.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Spec map[string]any `yaml:"spec"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	smtp, _ := doc.Spec["smtp"].(map[string]any)
	if smtp == nil {
		t.Fatal("spec.smtp missing")
	}
	if _, ok := smtp["behavior"]; ok {
		t.Fatal("spec.smtp.behavior must stay omitted")
	}
}

func TestDefaultProfileDevCredentials(t *testing.T) {
	dir := defaultProfileDir(t)
	env, err := os.ReadFile(filepath.Join(dir, "profile.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(env, []byte("LAB_DEV_MODE=false")) {
		t.Fatal("default profile must keep LAB_DEV_MODE=false")
	}
	if !bytes.Contains(env, []byte("LAB_DOCKER_SUBNET=10.99.42.0/24")) {
		t.Fatal("default profile must pin LAB_DOCKER_SUBNET to a /24")
	}
	for _, line := range strings.Split(string(env), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "LAB_DEV_MODE=") && line != "LAB_DEV_MODE=false" {
			t.Fatalf("LAB_DEV_MODE must stay false, got %q", line)
		}
	}

	got, err := LoadDevCredentials(filepath.Join(dir, "dev-credentials.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	want, err := LoadDevCredentials(testdataDevcreds("valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if *got != *want {
		t.Fatalf("profiles/default/dev-credentials.yaml drifted from testdata/devcreds/valid.yaml\ngot  %+v\nwant %+v", got, want)
	}
}

func TestDefaultProfileLabJenkinsDisabled(t *testing.T) {
	vals, err := envfile.ParseFile(filepath.Join(defaultProfileDir(t), "profile.env"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.IsTrue(vals["LABJENKINS_ENABLED"]) {
		t.Fatal("default profile must keep LABJENKINS_ENABLED=false")
	}
	for _, k := range []string{"ENTRA_TENANT_ID", "ENTRA_API_APP_ID", "ENTRA_GATEWAY_APP_ID"} {
		if v := strings.TrimSpace(vals[k]); v != "" {
			t.Fatalf("%s must be empty in default, got %q", k, v)
		}
	}
	for _, v := range vals {
		switch v {
		case "{tenant-id}", "{api-app-id}", "{gateway-app-id}":
			t.Fatalf("placeholder %q must not be a profile.env value", v)
		}
	}
}
