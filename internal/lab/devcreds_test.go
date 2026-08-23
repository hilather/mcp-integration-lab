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
	if got := doc.Spec.Passwords.LabLDAPAlice; got != "lab-dev-alice-12" {
		t.Fatalf("labldapAlice = %q, want lab-dev-alice-12", got)
	}
	if got := doc.Spec.SharedSecrets.RadiusLabSwitches; got != "LabDev-Switches-Radius-01" {
		t.Fatalf("radiusLabSwitches = %q, want LabDev-Switches-Radius-01", got)
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
