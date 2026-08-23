package lab

import (
	"path/filepath"
	"strings"
	"testing"
)

func testdataDevcreds(name string) string {
	return filepath.Join("testdata", "devcreds", name)
}

func TestLoadDevCredentialsValid(t *testing.T) {
	doc, err := LoadDevCredentials(testdataDevcreds("valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ path, got, want string }{
		{"spec.tokens.labdns", doc.Spec.Tokens.LabDNS, "lab-dev-labdns-token"},
		{"spec.tokens.labinfo", doc.Spec.Tokens.Labinfo, "lab-dev-labinfo-token"},
		{"spec.tokens.labmail", doc.Spec.Tokens.Labmail, "lab-dev-labmail-token"},
		{"spec.tokens.mcpClient", doc.Spec.Tokens.MCPClient, "lab-dev-mcp-client-token"},
		{"spec.tokens.labldapAdmin", doc.Spec.Tokens.LabLDAPAdmin, "lab-dev-labldap-token-admin"},
		{"spec.tokens.labtacacsAdmin", doc.Spec.Tokens.LabTacacsAdmin, "lab-dev-labtacacs-token-admin"},
		{"spec.passwords.maildevWeb", doc.Spec.Passwords.MaildevWeb, "lab-dev-mail-admin-1"},
		{"spec.passwords.labldapAlice", doc.Spec.Passwords.LabLDAPAlice, "lab-dev-alice-12"},
		{"spec.passwords.labldapRuntime", doc.Spec.Passwords.LabLDAPRuntime, "lab-dev-runtime-12"},
		{"spec.passwords.labldapDM", doc.Spec.Passwords.LabLDAPDM, "lab-dev-dm-password-12"},
		{"spec.passwords.taclabAdmin", doc.Spec.Passwords.TaclabAdmin, "LabAdmin-Dev-Pass-01!"},
		{"spec.passwords.taclabAdminEnable", doc.Spec.Passwords.TaclabAdminEnable, "LabEnable-Dev-Pass-01!"},
		{"spec.passwords.taclabReadonly", doc.Spec.Passwords.TaclabReadonly, "LabReadonly-Dev-Pass-01!"},
		{"spec.passwords.taclabDisabled", doc.Spec.Passwords.TaclabDisabled, "LabDisabled-Dev-Pass-01!"},
		{"spec.passwords.taclabChallenge", doc.Spec.Passwords.TaclabChallenge, "LabChallenge-Dev-Pass-01!"},
		{"spec.sharedSecrets.tacacsLabSwitches", doc.Spec.SharedSecrets.TacacsLabSwitches, "LabDev-Switches-Tacacs-01"},
		{"spec.sharedSecrets.radiusLabSwitches", doc.Spec.SharedSecrets.RadiusLabSwitches, "LabDev-Switches-Radius-01"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.path, tc.got, tc.want)
		}
	}
}

func TestLoadDevCredentialsLabDevRadiusPasses(t *testing.T) {
	if err := checkSharedSecret("LabDev-Switches-Radius-01", "radius"); err != nil {
		t.Fatalf("LabDev-Switches-Radius-01 must pass TacLab policy: %v", err)
	}
	if _, err := LoadDevCredentials(testdataDevcreds("valid.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDevCredentialsUnknownField(t *testing.T) {
	_, err := LoadDevCredentials(testdataDevcreds("unknown-field.yaml"))
	if err == nil {
		t.Fatal("expected unknown field to fail KnownFields")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not found") && !strings.Contains(msg, "unknown") {
		t.Fatalf("expected KnownFields rejection, got %v", err)
	}
}

func TestLoadDevCredentialsMissingKey(t *testing.T) {
	_, err := LoadDevCredentials(testdataDevcreds("missing-key.yaml"))
	if err == nil || !strings.Contains(err.Error(), "spec.tokens.mcpClient is required") {
		t.Fatalf("expected missing mcpClient, got %v", err)
	}
}

func TestLoadDevCredentialsEmptyValue(t *testing.T) {
	_, err := LoadDevCredentials(testdataDevcreds("empty-value.yaml"))
	if err == nil || !strings.Contains(err.Error(), "spec.tokens.labdns is required") {
		t.Fatalf("expected empty labdns, got %v", err)
	}
}

func TestLoadDevCredentialsAliceTooShort(t *testing.T) {
	_, err := LoadDevCredentials(testdataDevcreds("alice-short.yaml"))
	if err == nil || !strings.Contains(err.Error(), "spec.passwords.labldapAlice") {
		t.Fatalf("expected Alice minLength failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "minLength") {
		t.Fatalf("expected minLength in error, got %v", err)
	}
}

func TestLoadDevCredentialsRadiusAllLowercase(t *testing.T) {
	_, err := LoadDevCredentials(testdataDevcreds("radius-lowercase.yaml"))
	if err == nil || !strings.Contains(err.Error(), "character-class") {
		t.Fatalf("expected RADIUS character-class failure, got %v", err)
	}
}

func TestLoadDevCredentialsRadiusExactPassword(t *testing.T) {
	_, err := LoadDevCredentials(testdataDevcreds("radius-password.yaml"))
	if err == nil {
		t.Fatal("expected exact password to fail")
	}
	if !isKnownWeakSecret([]byte("password")) {
		t.Fatal("password must be a known-weak value")
	}
}

func TestIsKnownWeakSecretExactMatchNotSubstring(t *testing.T) {
	weak := []string{
		"", "password", "PASSWORD", "secret", "changeme", "tacacs", "tacacs+",
		"admin", "test", "testing", "lab", "default", "cisco", "public",
		"private", "123456", "12345678", "qwerty",
	}
	for _, s := range weak {
		if !isKnownWeakSecret([]byte(s)) {
			t.Errorf("%q: want known-weak", s)
		}
	}
	if isKnownWeakSecret([]byte("LabDev-Switches-Radius-01")) {
		t.Fatal("substring radius must not be known-weak")
	}
	if err := checkSharedSecret("LabDev-Switches-Radius-01", "radius"); err != nil {
		t.Fatalf("substring radius must pass policy: %v", err)
	}
	if isKnownWeakSecret([]byte("not-the-word-password")) {
		t.Fatal("substring password must not be known-weak")
	}
}

func TestCharacterClassesLabDevRadius(t *testing.T) {
	got := characterClasses([]byte("LabDev-Switches-Radius-01"))
	if got != 4 {
		t.Fatalf("characterClasses(LabDev-Switches-Radius-01) = %d, want 4", got)
	}
}
