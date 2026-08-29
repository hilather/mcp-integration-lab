package lab

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLabldapOneShotArgsAvoidWaitRace(t *testing.T) {
	args := labldapOneShotArgs("native-secret-prep")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--abort-on-container-exit", "--exit-code-from", "native-secret-prep"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	for _, a := range args {
		if a == "wait" || a == "-d" {
			t.Errorf("one-shot must not detach or wait (race): %v", args)
		}
	}
}

func TestLabldapComposeArgsUsesNativeEngine(t *testing.T) {
	r := &Runner{Root: "/repo"}
	args := r.labldapComposeArgs("up", "-d")
	joined := strings.Join(args, "\n")
	for _, want := range []string{
		"compose.yaml",
		"compose.ephemeral.yaml",
		"labldap.overlay.yaml",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	for _, a := range args {
		if strings.HasSuffix(a, "/compose.native.yaml") ||
			strings.HasSuffix(a, "/compose.native-ephemeral.yaml") ||
			strings.HasSuffix(a, "/compose.389ds.yaml") ||
			strings.HasSuffix(a, "/compose.389ds-ephemeral.yaml") {
			t.Fatalf("v0.2 native alias or 389 overlay still selected: %s", a)
		}
	}
}

func TestLabldapOverlayMapsAllowedHostsFromPublicHost(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "compose", "labldap.overlay.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	control := yamlMappingValue(yamlMappingValue(&root, "services"), "control")
	if control == nil {
		t.Fatal("compose/labldap.overlay.yaml: missing services.control")
	}
	ports := yamlMappingValue(control, "ports")
	if ports == nil {
		t.Fatal("services.control.ports missing")
	}
	if ports.Tag != "!override" {
		t.Fatalf("services.control.ports tag = %q, want !override", ports.Tag)
	}
	env := yamlMappingValue(control, "environment")
	if env == nil {
		t.Fatal("services.control.environment missing")
	}
	if env.Kind != yaml.MappingNode {
		t.Fatalf("services.control.environment kind = %v, want mapping", env.Kind)
	}
	if strings.Contains(env.Tag, "override") {
		t.Fatalf("services.control.environment must not use !override (tag=%q)", env.Tag)
	}
	got, ok := yamlMappingString(env, labldapAllowedHostsEnv)
	if !ok {
		t.Fatalf("missing %s in control.environment", labldapAllowedHostsEnv)
	}
	const want = "${LAB_PUBLIC_HOST:-localhost}"
	if got != want {
		t.Fatalf("%s = %q, want %q", labldapAllowedHostsEnv, got, want)
	}
}

func TestParseComposeServiceEnvironment(t *testing.T) {
	const warningJSON = "time=\"2026-08-28T00:00:00Z\" level=warning msg=\"obsolete\"\n" +
		`{"services":{"control":{"environment":{"LABLDAP_LDAP_URL":"ldaps://directory:3636","LABLDAP_DIRECTORY_HOST":"directory","LABLDAP_DIRECTORY_CA_FILE":"/run/secrets/ca.crt","LABLDAP_MANAGEMENT_ALLOWED_HOSTS":"lab.example"}}}}`
	got, err := parseComposeServiceEnvironment(warningJSON, "control")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"LABLDAP_LDAP_URL", "LABLDAP_DIRECTORY_HOST", "LABLDAP_DIRECTORY_CA_FILE", labldapAllowedHostsEnv} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if got[labldapAllowedHostsEnv] != "lab.example" {
		t.Fatalf("%s = %q, want lab.example", labldapAllowedHostsEnv, got[labldapAllowedHostsEnv])
	}

	listJSON := `{"services":{"control":{"environment":["LABLDAP_LDAP_URL=ldaps://directory:3636","LABLDAP_MANAGEMENT_ALLOWED_HOSTS=localhost"]}}}`
	got, err = parseComposeServiceEnvironment(listJSON, "control")
	if err != nil {
		t.Fatal(err)
	}
	if got[labldapAllowedHostsEnv] != "localhost" {
		t.Fatalf("list form %s = %q", labldapAllowedHostsEnv, got[labldapAllowedHostsEnv])
	}

	if _, err := parseComposeServiceEnvironment(`{"services":{}}`, "control"); err == nil {
		t.Fatal("missing service must fail")
	}
}

func TestLabldapOverlayHostAllowListMergedConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("live compose merge needs docker")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	upstream := filepath.Join(root, "third_party", "go-lab-ldap-mcp", "deploy", "compose", "compose.yaml")
	if _, err := os.Stat(upstream); err != nil {
		t.Skip("vendored LabLDAP compose not present")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not in PATH")
	}
	r, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	env, err := r.labldapMergedControlEnv()
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "Cannot connect to the Docker daemon") ||
			strings.Contains(msg, "permission denied while trying to connect") {
			t.Skip(msg)
		}
		t.Fatal(err)
	}
	for _, k := range []string{"LABLDAP_LDAP_URL", "LABLDAP_DIRECTORY_HOST", "LABLDAP_DIRECTORY_CA_FILE"} {
		if env[k] == "" {
			t.Errorf("merged control.environment missing upstream %s", k)
		}
	}
	want := r.Prof.Get("LAB_PUBLIC_HOST", "localhost")
	if env[labldapAllowedHostsEnv] != want {
		t.Fatalf("merged %s = %q, want %q", labldapAllowedHostsEnv, env[labldapAllowedHostsEnv], want)
	}
}

func yamlMappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return yamlMappingValue(n.Content[0], key)
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func yamlMappingString(n *yaml.Node, key string) (string, bool) {
	v := yamlMappingValue(n, key)
	if v == nil {
		return "", false
	}
	return v.Value, true
}
