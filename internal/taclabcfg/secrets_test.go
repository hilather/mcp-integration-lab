package taclabcfg

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func testCatalog() Catalog {
	return Catalog{
		APIAdminToken:    "lab-dev-labtacacs-token-admin",
		TacacsSecret:     "LabDev-Switches-Tacacs-01",
		RadiusSecret:     "LabDev-Switches-Radius-01",
		AdminPassword:    "LabAdmin-Dev-Pass-01!",
		AdminEnable:      "LabEnable-Dev-Pass-01!",
		ReadonlyPassword: "LabReadonly-Dev-Pass-01!",
		DisabledPassword: "LabDisabled-Dev-Pass-01!",
		ChallengeSecret:  "LabChallenge-Dev-Pass-01!",
	}
}

func TestPHCRoundTripNewlineFree(t *testing.T) {
	entropy := bytes.Repeat([]byte{0x02}, 16)
	phc, err := DeriveArgon2id([]byte("LabAdmin-Dev-Pass-01!"), TestParams, bytes.NewReader(entropy))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(phc, []byte("\n")) {
		t.Fatalf("PHC has newline: %q", phc)
	}
	if !bytes.HasPrefix(phc, []byte("$argon2id$v=19$m=8,t=1,p=1$")) {
		t.Fatalf("PHC format: %s", phc)
	}
	if err := VerifyArgon2id(phc, []byte("LabAdmin-Dev-Pass-01!")); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := VerifyArgon2id(phc, []byte("wrong")); err == nil {
		t.Fatal("expected mismatch")
	}
	withNL := append(append([]byte{}, phc...), '\n')
	if err := VerifyArgon2id(withNL, []byte("LabAdmin-Dev-Pass-01!")); err == nil {
		t.Fatal("trailing newline must fail parsePHC")
	}
}

func TestDefaultParamsGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("DefaultParams Argon2id")
	}
	entropy := bytes.Repeat([]byte{0x01}, 16)
	phc, err := DeriveArgon2id([]byte("LabAdmin-Dev-Pass-01!"), DefaultParams, bytes.NewReader(entropy))
	if err != nil {
		t.Fatal(err)
	}
	const want = "$argon2id$v=19$m=65536,t=3,p=1$AQEBAQEBAQEBAQEBAQEBAQ$C/mlNnz+Itw3r+2quxgOERCPRr1mI1HkHY8LMHUnR0w"
	if string(phc) != want {
		t.Fatalf("got %s\nwant %s", phc, want)
	}
	if bytes.Contains(phc, []byte("\n")) {
		t.Fatal("DefaultParams PHC has newline")
	}
	if err := VerifyArgon2id(phc, []byte("LabAdmin-Dev-Pass-01!")); err != nil {
		t.Fatal(err)
	}
}

func TestApplyDevSecretsPasswordsTxtFormat(t *testing.T) {
	dir := t.TempDir()
	res, err := ApplyDevSecrets(dir, testCatalog(), TestParams, bytes.NewReader(bytes.Repeat([]byte{0x03}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || !res.APIAdminChanged {
		t.Fatalf("first apply: %+v", res)
	}
	body := mustRead(t, filepath.Join(dir, "PASSWORDS.txt"))
	want := passwordsBody(testCatalog())
	if !bytes.Equal(body, want) {
		t.Fatalf("PASSWORDS.txt\ngot:\n%s\nwant:\n%s", body, want)
	}
	info, err := os.Stat(filepath.Join(dir, "PASSWORDS.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("PASSWORDS.txt mode %o", info.Mode().Perm())
	}
}

func TestApplyDevSecretsNewlineFreePlaintext(t *testing.T) {
	dir := t.TempDir()
	cat := testCatalog()
	if _, err := ApplyDevSecrets(dir, cat, TestParams, repeatingEntropy(0x04)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"api_admin_token",
		"lab_switches_tacacs_secret",
		"lab_switches_radius_secret",
		"lab_admin_challenge_secret",
		"lab_admin_argon2id",
		"lab_admin_enable_argon2id",
		"lab_readonly_argon2id",
		"lab_disabled_argon2id",
	} {
		b := mustRead(t, filepath.Join(dir, name))
		if bytes.Contains(b, []byte("\n")) {
			t.Errorf("%s has newline: %q", name, b)
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o444 {
			t.Errorf("%s mode %o, want 0444", name, info.Mode().Perm())
		}
	}
	if got := string(mustRead(t, filepath.Join(dir, "api_admin_token"))); got != cat.APIAdminToken {
		t.Fatalf("token %q", got)
	}
	if err := VerifyArgon2id(mustRead(t, filepath.Join(dir, "lab_admin_argon2id")), []byte(cat.AdminPassword)); err != nil {
		t.Fatal(err)
	}
}

func TestApplyDevSecretsDoesNotRehashWhenVerifySucceeds(t *testing.T) {
	dir := t.TempDir()
	cat := testCatalog()
	if _, err := ApplyDevSecrets(dir, cat, TestParams, repeatingEntropy(0x05)); err != nil {
		t.Fatal(err)
	}
	phc := mustRead(t, filepath.Join(dir, "lab_admin_argon2id"))
	res, err := ApplyDevSecrets(dir, cat, TestParams, repeatingEntropy(0x06))
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.APIAdminChanged {
		t.Fatalf("second apply must not flag plaintext: %+v", res)
	}
	if !bytes.Equal(phc, mustRead(t, filepath.Join(dir, "lab_admin_argon2id"))) {
		t.Fatal("PHC rewritten even though verify succeeded")
	}
}

func TestApplyDevSecretsPHCRewriteDoesNotFlagWhenPlaintextMatches(t *testing.T) {
	dir := t.TempDir()
	cat := testCatalog()
	if _, err := ApplyDevSecrets(dir, cat, TestParams, repeatingEntropy(0x07)); err != nil {
		t.Fatal(err)
	}
	overwrite0444(t, filepath.Join(dir, "lab_admin_argon2id"), []byte("not-a-phc"))
	res, err := ApplyDevSecrets(dir, cat, TestParams, repeatingEntropy(0x08))
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("PHC rewrite must not flag recreate: %+v", res)
	}
	if err := VerifyArgon2id(mustRead(t, filepath.Join(dir, "lab_admin_argon2id")), []byte(cat.AdminPassword)); err != nil {
		t.Fatal(err)
	}
}

func TestApplyDevSecretsFlagsPlaintextChange(t *testing.T) {
	dir := t.TempDir()
	cat := testCatalog()
	if _, err := ApplyDevSecrets(dir, cat, TestParams, repeatingEntropy(0x09)); err != nil {
		t.Fatal(err)
	}
	overwrite0444(t, filepath.Join(dir, "lab_switches_radius_secret"), []byte("old-radius-secret"))
	res, err := ApplyDevSecrets(dir, cat, TestParams, repeatingEntropy(0x0a))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("radius secret change must flag Changed")
	}
	if res.APIAdminChanged {
		t.Fatal("radius-only change must not flag APIAdminChanged")
	}

	overwrite0444(t, filepath.Join(dir, "api_admin_token"), []byte("stale-token"))
	res, err = ApplyDevSecrets(dir, cat, TestParams, repeatingEntropy(0x0b))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || !res.APIAdminChanged {
		t.Fatalf("token change: %+v", res)
	}
}

func TestApplyDevSecretsLeavesOtherMaterial(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "tacacs_server_key.pem")
	if err := os.WriteFile(key, []byte("KEEP-KEY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pki := filepath.Join(filepath.Dir(dir), "pki", "ca.pem")
	if err := os.MkdirAll(filepath.Dir(pki), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pki, []byte("KEEP-PKI\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := filepath.Join(filepath.Dir(dir), "config", "taclab.yaml")
	if err := os.MkdirAll(filepath.Dir(yaml), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yaml, []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyDevSecrets(dir, testCatalog(), TestParams, repeatingEntropy(0x0c)); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, key)); got != "KEEP-KEY\n" {
		t.Fatalf("server key mutated: %q", got)
	}
	if got := string(mustRead(t, pki)); got != "KEEP-PKI\n" {
		t.Fatalf("pki mutated: %q", got)
	}
	if got := string(mustRead(t, yaml)); got != "schema_version: 1\n" {
		t.Fatalf("yaml mutated: %q", got)
	}
}

func overwrite0444(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
}

func repeatingEntropy(seed byte) *bytes.Reader {
	buf := bytes.Repeat([]byte{seed}, 256)
	return bytes.NewReader(buf)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
