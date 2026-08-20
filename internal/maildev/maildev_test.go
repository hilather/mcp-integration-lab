package maildev

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeProfile(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const validBootstrap = `
apiVersion: labmail.dev/v1alpha1
kind: LabMail
spec:
  smtp:
    tls:
      mode: off
  management:
    auth:
      tokens:
        - id: admin
          secretFile: /run/secrets/labmail-token
      basic:
        username: admin
        passwordFile: /run/secrets/maildev-web-password
    mcp:
      allowLegacyClients: true
`

func TestValidateProfileAcceptsValidBootstrap(t *testing.T) {
	dir := writeProfile(t, map[string]string{"labmail/bootstrap.yaml": validBootstrap})
	if err := ValidateProfile(dir); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDefaultProfileBootstrap(t *testing.T) {
	dir := filepath.Join("..", "..", "profiles", "default")
	if err := ValidateProfile(dir); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultProfileOriginAllowlist(t *testing.T) {
	path := filepath.Join("..", "..", "profiles", "default", "labmail", "bootstrap.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBootstrap(path); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Spec struct {
			Management struct {
				OriginAllowlist []string `yaml:"originAllowlist"`
			} `yaml:"management"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range doc.Spec.Management.OriginAllowlist {
		if v == "*" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("profiles/default/labmail/bootstrap.yaml originAllowlist = %v, want \"*\" (LabMail rc.3 remote SPA hatch)", doc.Spec.Management.OriginAllowlist)
	}
}

func TestValidateProfileRejectsLeftoverMaildevYAML(t *testing.T) {
	dir := writeProfile(t, map[string]string{
		"labmail/bootstrap.yaml": validBootstrap,
		"maildev/maildev.yaml":   "flags: {}\n",
	})
	err := ValidateProfile(dir)
	if err == nil || !strings.Contains(err.Error(), "replaced by labmail/bootstrap.yaml") {
		t.Fatalf("expected leftover rejection, got %v", err)
	}
}

func TestValidateBootstrapMissingFile(t *testing.T) {
	if err := ValidateProfile(t.TempDir()); err == nil {
		t.Fatal("expected missing bootstrap error")
	}
}

func TestValidateBootstrapRejectsRelayKeys(t *testing.T) {
	for _, key := range []string{
		"outgoing-host",
		"outgoingHost",
		"auto-relay",
		"autoRelay",
		"--auto-relay",
		"relay",
		"smarthost",
		"forwardTo",
		"deliver",
		"environment",
		"flags",
		"incoming-secure",
		"mail-directory",
		"base-pathname",
	} {
		body := "apiVersion: labmail.dev/v1alpha1\nkind: LabMail\n" + key + ": true\n"
		p := filepath.Join(t.TempDir(), "bootstrap.yaml")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		err := ValidateBootstrap(p)
		if err == nil || !strings.Contains(err.Error(), "receive-only") {
			t.Fatalf("key %q: expected receive-only rejection, got %v", key, err)
		}
	}
}

func TestValidateBootstrapRejectsImplicitSMTPS(t *testing.T) {
	body := `
apiVersion: labmail.dev/v1alpha1
kind: LabMail
spec:
  smtp:
    tls:
      mode: implicit
  management:
    auth:
      tokens:
        - secretFile: /run/secrets/labmail-token
      basic:
        username: admin
        passwordFile: /run/secrets/maildev-web-password
    mcp:
      allowLegacyClients: true
`
	p := filepath.Join(t.TempDir(), "bootstrap.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "implicit") {
		t.Fatalf("expected implicit SMTPS rejection, got %v", err)
	}
}

func TestValidateBootstrapRejectsLegacyClientsOff(t *testing.T) {
	body := strings.Replace(validBootstrap, "allowLegacyClients: true", "allowLegacyClients: false", 1)
	p := filepath.Join(t.TempDir(), "bootstrap.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "allowLegacyClients") {
		t.Fatalf("expected allowLegacyClients rejection, got %v", err)
	}
}

func TestValidateBootstrapRejectsWrongBasicUser(t *testing.T) {
	body := strings.Replace(validBootstrap, "username: admin", "username: other", 1)
	p := filepath.Join(t.TempDir(), "bootstrap.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "MAILDEV_WEB_USER") {
		t.Fatalf("expected frozen username rejection, got %v", err)
	}
}

func TestValidateBootstrapRejectsWrongSecretPaths(t *testing.T) {
	body := strings.Replace(validBootstrap, "/run/secrets/labmail-token", "/etc/wrong-token", 1)
	p := filepath.Join(t.TempDir(), "bootstrap.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "labmail-token") {
		t.Fatalf("expected token path rejection, got %v", err)
	}
}

func TestNormalizeKeyStripsDashes(t *testing.T) {
	if got, want := normalizeKey("--Auto_Relay"), "autorelay"; got != want {
		t.Fatalf("normalizeKey = %q, want %q", got, want)
	}
}
