package lab

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	devCredentialsAPIVersion = "mcplab.dev/v1alpha1"
	devCredentialsKind       = "DevCredentials"

	// LabLDAP scenario passwordPolicy.minLength (profiles/default).
	labldapPasswordMinLength = 12

	// TacLab v1.3.0 labgen YAML: security.*.minimum_length_characters /
	// minimum_character_classes. Copied with isKnownWeakSecret from
	// internal/config/secretpolicy.go — config.Validate does not read secret files.
	taclabSharedSecretMinLen     = 16
	taclabSharedSecretMinClasses = 3
)

// DevCredentials is the mcplab.dev/v1alpha1 catalog at
// profiles/<name>/dev-credentials.yaml. Used iff LAB_DEV_MODE=true.
type DevCredentials struct {
	APIVersion string             `yaml:"apiVersion"`
	Kind       string             `yaml:"kind"`
	Metadata   DevCredentialsMeta `yaml:"metadata"`
	Spec       DevCredentialsSpec `yaml:"spec"`
}

type DevCredentialsMeta struct {
	Name string `yaml:"name"`
}

// DevCredentialsSpec holds every catalog secret. Every key is required;
// there is no spec.defaults.password fallback.
type DevCredentialsSpec struct {
	Tokens        DevTokens        `yaml:"tokens"`
	Passwords     DevPasswords     `yaml:"passwords"`
	SharedSecrets DevSharedSecrets `yaml:"sharedSecrets"`
}

// DevTokens are opaque bearers. Charset is not constrained (hex vs
// base64url vs lab-dev-…).
type DevTokens struct {
	LabDNS         string `yaml:"labdns"`
	Labinfo        string `yaml:"labinfo"`
	Labmail        string `yaml:"labmail"`
	MCPClient      string `yaml:"mcpClient"`
	LabLDAPAdmin   string `yaml:"labldapAdmin"`
	LabTacacsAdmin string `yaml:"labtacacsAdmin"`
}

type DevPasswords struct {
	MaildevWeb        string `yaml:"maildevWeb"`
	LabLDAPAlice      string `yaml:"labldapAlice"`
	LabLDAPRuntime    string `yaml:"labldapRuntime"`
	LabLDAPDM         string `yaml:"labldapDM"`
	TaclabAdmin       string `yaml:"taclabAdmin"`
	TaclabAdminEnable string `yaml:"taclabAdminEnable"`
	TaclabReadonly    string `yaml:"taclabReadonly"`
	TaclabDisabled    string `yaml:"taclabDisabled"`
	TaclabChallenge   string `yaml:"taclabChallenge"`
}

type DevSharedSecrets struct {
	TacacsLabSwitches string `yaml:"tacacsLabSwitches"`
	RadiusLabSwitches string `yaml:"radiusLabSwitches"`
}

// LoadDevCredentials parses path with KnownFields(true) and Validate.
func LoadDevCredentials(path string) (*DevCredentials, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	doc, err := parseDevCredentials(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

func parseDevCredentials(r io.Reader) (*DevCredentials, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var doc DevCredentials
	if err := dec.Decode(&doc); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("empty document")
		}
		return nil, err
	}
	return &doc, nil
}

// Validate fail-closes on wrong apiVersion/kind, missing/empty required
// keys, LabLDAP passwords shorter than minLength, and TacLab shared secrets
// that would crash the appliance at boot.
func (d *DevCredentials) Validate() error {
	if d == nil {
		return fmt.Errorf("dev credentials document is nil")
	}
	if d.APIVersion != devCredentialsAPIVersion {
		return fmt.Errorf("apiVersion %q, want %s", d.APIVersion, devCredentialsAPIVersion)
	}
	if d.Kind != devCredentialsKind {
		return fmt.Errorf("kind %q, want %s", d.Kind, devCredentialsKind)
	}
	if strings.TrimSpace(d.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}

	required := []struct{ path, value string }{
		{"spec.tokens.labdns", d.Spec.Tokens.LabDNS},
		{"spec.tokens.labinfo", d.Spec.Tokens.Labinfo},
		{"spec.tokens.labmail", d.Spec.Tokens.Labmail},
		{"spec.tokens.mcpClient", d.Spec.Tokens.MCPClient},
		{"spec.tokens.labldapAdmin", d.Spec.Tokens.LabLDAPAdmin},
		{"spec.tokens.labtacacsAdmin", d.Spec.Tokens.LabTacacsAdmin},
		{"spec.passwords.maildevWeb", d.Spec.Passwords.MaildevWeb},
		{"spec.passwords.labldapAlice", d.Spec.Passwords.LabLDAPAlice},
		{"spec.passwords.labldapRuntime", d.Spec.Passwords.LabLDAPRuntime},
		{"spec.passwords.labldapDM", d.Spec.Passwords.LabLDAPDM},
		{"spec.passwords.taclabAdmin", d.Spec.Passwords.TaclabAdmin},
		{"spec.passwords.taclabAdminEnable", d.Spec.Passwords.TaclabAdminEnable},
		{"spec.passwords.taclabReadonly", d.Spec.Passwords.TaclabReadonly},
		{"spec.passwords.taclabDisabled", d.Spec.Passwords.TaclabDisabled},
		{"spec.passwords.taclabChallenge", d.Spec.Passwords.TaclabChallenge},
		{"spec.sharedSecrets.tacacsLabSwitches", d.Spec.SharedSecrets.TacacsLabSwitches},
		{"spec.sharedSecrets.radiusLabSwitches", d.Spec.SharedSecrets.RadiusLabSwitches},
	}
	for _, f := range required {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("%s is required", f.path)
		}
	}

	for _, f := range []struct{ path, value string }{
		{"spec.passwords.labldapAlice", d.Spec.Passwords.LabLDAPAlice},
		{"spec.passwords.labldapRuntime", d.Spec.Passwords.LabLDAPRuntime},
		{"spec.passwords.labldapDM", d.Spec.Passwords.LabLDAPDM},
	} {
		if len(f.value) < labldapPasswordMinLength {
			return fmt.Errorf("%s is shorter than LabLDAP passwordPolicy.minLength (%d)", f.path, labldapPasswordMinLength)
		}
	}

	if err := checkSharedSecret(d.Spec.SharedSecrets.TacacsLabSwitches, "spec.sharedSecrets.tacacsLabSwitches"); err != nil {
		return err
	}
	if err := checkSharedSecret(d.Spec.SharedSecrets.RadiusLabSwitches, "spec.sharedSecrets.radiusLabSwitches"); err != nil {
		return err
	}
	return nil
}

// checkSharedSecret copies TacLab v1.3.0 CheckSharedSecret
// (internal/config/secretpolicy.go): length, unicode character classes, and
// exact-match known-weak list. Not a substring match.
func checkSharedSecret(secret, path string) error {
	raw := []byte(secret)
	if taclabSharedSecretMinLen > 0 && len(raw) < taclabSharedSecretMinLen {
		return fmt.Errorf("%s: shared secret is shorter than the configured minimum", path)
	}
	if taclabSharedSecretMinClasses > 0 && characterClasses(raw) < taclabSharedSecretMinClasses {
		return fmt.Errorf("%s: shared secret does not meet the character-class policy", path)
	}
	if isKnownWeakSecret(raw) {
		return fmt.Errorf("%s: shared secret is a known-weak value", path)
	}
	return nil
}

func characterClasses(b []byte) int {
	var lower, upper, digit, other bool
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			other = true
			b = b[1:]
			continue
		}
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
		b = b[size:]
	}
	n := 0
	if lower {
		n++
	}
	if upper {
		n++
	}
	if digit {
		n++
	}
	if other {
		n++
	}
	return n
}

func isKnownWeakSecret(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	s := stringsToLowerASCII(b)
	switch s {
	case "password", "secret", "changeme", "tacacs", "tacacs+", "admin",
		"test", "testing", "lab", "default", "cisco", "public", "private",
		"123456", "12345678", "qwerty":
		return true
	default:
		return false
	}
}

func stringsToLowerASCII(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		} else {
			out[i] = c
		}
	}
	return string(out)
}
