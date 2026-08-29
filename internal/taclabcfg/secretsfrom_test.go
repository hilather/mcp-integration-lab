package taclabcfg

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// secretsFromFile mirrors TacLab labgen's unexported decode struct.
type secretsFromFile struct {
	APIAdminToken           string `yaml:"api_admin_token"`
	LabSwitchesTacacsSecret string `yaml:"lab_switches_tacacs_secret"`
	LabSwitchesRadiusSecret string `yaml:"lab_switches_radius_secret"`
	Passwords               struct {
		LabAdmin          string `yaml:"lab-admin"`
		LabAdminEnable    string `yaml:"lab-admin-enable"`
		LabReadonly       string `yaml:"lab-readonly"`
		LabDisabled       string `yaml:"lab-disabled"`
		LabAdminChallenge string `yaml:"lab-admin-challenge"`
	} `yaml:"passwords"`
}

func TestWriteSecretsFromDefaultCatalog(t *testing.T) {
	src, err := os.ReadFile("secretsfrom.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(src, []byte("yaml.Marshal")) {
		t.Fatal("WriteSecretsFrom must hand-build YAML, not yaml.Marshal")
	}
	if bytes.Contains(src, []byte("third_party")) {
		t.Fatal("WriteSecretsFrom must not read third_party/")
	}

	golden, err := os.ReadFile(filepath.Join("testdata", "secrets-from.golden.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(golden, []byte("\n")) {
		t.Fatal("golden must end with a trailing newline")
	}

	path := filepath.Join(t.TempDir(), "secrets-from.yaml")
	cat := testCatalog()
	if err := WriteSecretsFrom(path, cat); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, golden) {
		t.Fatalf("WriteSecretsFrom output != golden\ngot:\n%s\nwant:\n%s", got, golden)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o, want 0600", info.Mode().Perm())
	}

	dec := yaml.NewDecoder(bytes.NewReader(got))
	dec.KnownFields(true)
	var decoded secretsFromFile
	if err := dec.Decode(&decoded); err != nil {
		t.Fatalf("tagged-struct decode: %v", err)
	}
	if decoded.APIAdminToken != cat.APIAdminToken ||
		decoded.LabSwitchesTacacsSecret != cat.TacacsSecret ||
		decoded.LabSwitchesRadiusSecret != cat.RadiusSecret ||
		decoded.Passwords.LabAdmin != cat.AdminPassword ||
		decoded.Passwords.LabAdminEnable != cat.AdminEnable ||
		decoded.Passwords.LabReadonly != cat.ReadonlyPassword ||
		decoded.Passwords.LabDisabled != cat.DisabledPassword ||
		decoded.Passwords.LabAdminChallenge != cat.ChallengeSecret {
		t.Fatalf("decoded %+v does not match catalog", decoded)
	}
}
