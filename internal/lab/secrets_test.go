package lab

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/profile"
)

func validCatalogBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(testdataDevcreds("valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func scaffoldSecretsRunner(t *testing.T, profileEnv string, catalog []byte) *Runner {
	t.Helper()
	root := t.TempDir()
	profDir := filepath.Join(root, "profiles", "default")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "profile.env"), []byte(profileEnv), 0o644); err != nil {
		t.Fatal(err)
	}
	if catalog != nil {
		if err := os.WriteFile(filepath.Join(profDir, "dev-credentials.yaml"), catalog, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prof, err := profile.Load(root, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{Root: root, Prof: prof, Env: prof.Environ(nil)}
	installTestSecretsDeps(r, nil)
	return r
}

func installTestSecretsDeps(r *Runner, exists map[string]bool) {
	if exists == nil {
		exists = map[string]bool{}
	}
	r.deps = &secretsDeps{
		setupsecrets: func(force bool) error {
			return fakeSetupsecrets(r.Root, force)
		},
		setuptls: func() error { return nil },
		ensureTaclab: func(force bool) error {
			return fakeLabgen(r.Root, force)
		},
		containerExists: func(name string) (bool, error) {
			return exists[name], nil
		},
		reloadMain:      func(string) error { return nil },
		reloadGateway:   func() error { return nil },
		reloadLabLDAP:   func() error { return nil },
		reloadLabTacacs: func() error { return nil },
		register:        func() error { return nil },
	}
}

func fakeSetupsecrets(root string, force bool) error {
	dir := filepath.Join(root, "third_party", "go-lab-ldap-mcp", "secrets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	writeRand := func(name string, n int) error {
		path := filepath.Join(dir, name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				return nil
			}
		}
		return writeRandHexFile(path, n, 0o600, true)
	}
	envPath := filepath.Join(dir, "directory.env")
	dmPath := filepath.Join(dir, "dm.pw")
	_, envErr := os.Stat(envPath)
	_, dmErr := os.Stat(dmPath)
	if force || os.IsNotExist(envErr) || os.IsNotExist(dmErr) {
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		pw := hex.EncodeToString(buf)
		if err := os.WriteFile(envPath, []byte("DS_DM_PASSWORD="+pw+"\n"), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(dmPath, []byte(pw+"\n"), 0o600); err != nil {
			return err
		}
	}
	if err := writeRand("runtime-ldap", 24); err != nil {
		return err
	}
	if err := writeRand("user-alice", 24); err != nil {
		return err
	}
	return writeRand("token-admin", 32)
}

func fakeLabgen(root string, force bool) error {
	dir := filepath.Join(root, taclabDir, "deployments", "compose", "secrets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"api_admin_token", "lab_switches_radius_secret"} {
		path := filepath.Join(dir, name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				continue
			}
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := writeRandHexFile(path, 32, 0o444, false); err != nil {
			return err
		}
	}
	return nil
}

func writeRandHexFile(path string, n int, mode os.FileMode, newline bool) error {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	body := hex.EncodeToString(buf)
	if newline {
		body += "\n"
	}
	return os.WriteFile(path, []byte(body), mode)
}

func readTrim(t *testing.T, r *Runner, rel string) string {
	t.Helper()
	b, err := os.ReadFile(r.path(rel))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func mustHexToken(t *testing.T, got, label string) {
	t.Helper()
	if len(got) != 64 {
		t.Fatalf("%s: len=%d want 64 hex, got %q", label, len(got), got)
	}
	if _, err := hex.DecodeString(got); err != nil {
		t.Fatalf("%s: not hex: %q", label, got)
	}
}

func TestSecretsDevWritesCatalog(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"secrets/labdns-token":                              "lab-dev-labdns-token",
		"secrets/labinfo-token":                             "lab-dev-labinfo-token",
		"secrets/labmail-token":                             "lab-dev-labmail-token",
		"secrets/mcp-client-token":                          "lab-dev-mcp-client-token",
		"secrets/maildev-web-password":                      "lab-dev-mail-admin-1",
		"third_party/go-lab-ldap-mcp/secrets/token-admin":   "lab-dev-labldap-token-admin",
		"third_party/go-lab-ldap-mcp/secrets/user-alice":    "lab-dev-alice-12",
		"third_party/go-lab-ldap-mcp/secrets/runtime-ldap":  "lab-dev-runtime-12",
		"third_party/go-lab-ldap-mcp/secrets/dm.pw":         "lab-dev-dm-password-12",
		"third_party/go-lab-ldap-mcp/secrets/directory.env": "DS_DM_PASSWORD=lab-dev-dm-password-12",
	}
	for rel, exp := range want {
		if got := readTrim(t, r, rel); got != exp {
			t.Errorf("%s = %q, want %q", rel, got, exp)
		}
	}
	if _, err := os.Stat(r.path(devModeMarkerRel)); err != nil {
		t.Fatalf("dev-mode marker missing: %v", err)
	}
}

func TestSecretsNonDevNeverReadsCatalog(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\nMCPJUNGLE_MODE=development\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"secrets/labdns-token",
		"secrets/labinfo-token",
		"secrets/labmail-token",
		"secrets/mcp-client-token",
		"secrets/maildev-web-password",
	} {
		mustHexToken(t, readTrim(t, r, rel), rel)
	}
	alice := readTrim(t, r, "third_party/go-lab-ldap-mcp/secrets/user-alice")
	if alice == "lab-dev-alice-12" {
		t.Fatal("non-dev must not write catalog Alice")
	}
	if _, err := os.Stat(r.path(devModeMarkerRel)); err == nil {
		t.Fatal("non-dev must not write the dev-mode marker")
	}
}

func TestSecretsNonDevIgnoresInvalidCatalog(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\n", []byte("not: a: catalog: {{"))
	if err := r.Secrets(); err != nil {
		t.Fatalf("non-dev must not parse the catalog: %v", err)
	}
	mustHexToken(t, readTrim(t, r, "secrets/labdns-token"), "labdns-token")
}

func TestSecretsDevFailsClosedWithoutCatalog(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", nil)
	err := r.Secrets()
	if err == nil || !strings.Contains(err.Error(), "dev-credentials.yaml") {
		t.Fatalf("want fail-closed missing catalog, got %v", err)
	}
}

func TestSecretsDevFailsClosedIncompleteCatalog(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nMCPJUNGLE_MODE=enterprise\n", []byte(`
apiVersion: mcplab.dev/v1alpha1
kind: DevCredentials
metadata:
  name: default
spec:
  tokens:
    labdns: "x"
`))
	err := r.Secrets()
	if err == nil {
		t.Fatal("expected incomplete catalog to fail")
	}
	if !strings.Contains(err.Error(), "required") && !strings.Contains(err.Error(), "LAB_DEV_MODE") {
		t.Fatalf("expected fail-closed required-key error, got %v", err)
	}
}

func TestSecretsDevEnterpriseGatewayStillWritesCatalog(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nMCPJUNGLE_MODE=enterprise\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if got := readTrim(t, r, "third_party/go-lab-ldap-mcp/secrets/user-alice"); got != "lab-dev-alice-12" {
		t.Fatalf("Alice = %q, want catalog (LAB_DEV_MODE only)", got)
	}
}

func TestSecretsLeaveDevRotatesCatalogAlice(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if got := readTrim(t, r, "third_party/go-lab-ldap-mcp/secrets/user-alice"); got != "lab-dev-alice-12" {
		t.Fatalf("precondition Alice = %q", got)
	}
	tlsDir := r.path("third_party/go-lab-ldap-mcp/secrets/tls")
	if err := os.MkdirAll(tlsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(tlsDir, "ca.crt")
	if err := os.WriteFile(ca, []byte("keep-me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.Prof.Values["LAB_DEV_MODE"] = "false"
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	alice := readTrim(t, r, "third_party/go-lab-ldap-mcp/secrets/user-alice")
	if alice == "lab-dev-alice-12" {
		t.Fatal("leave-dev must not keep catalog Alice")
	}
	mustHexToken(t, readTrim(t, r, "secrets/labdns-token"), "labdns-token")
	if readTrim(t, r, "secrets/labdns-token") == "lab-dev-labdns-token" {
		t.Fatal("leave-dev must not keep catalog labdns token")
	}
	if _, err := os.Stat(r.path(devModeMarkerRel)); !os.IsNotExist(err) {
		t.Fatalf("marker should be removed last, still present: %v", err)
	}
	kept, err := os.ReadFile(ca)
	if err != nil || string(kept) != "keep-me\n" {
		t.Fatalf("leave-dev must not rotate LabLDAP TLS: %v %q", err, kept)
	}
}

func TestSecretsLeaveDevKeepsMarkerIfReloadFails(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	r.Prof.Values["LAB_DEV_MODE"] = "false"
	installTestSecretsDeps(r, map[string]bool{
		"labdns": true, "maildev": true, "labinfo": true,
		"mcpjungle": true, "labldap": true, "labtacacs": true,
	})
	r.deps.reloadLabTacacs = func() error { return errors.New("reload failed") }
	err := r.Secrets()
	if err == nil {
		t.Fatal("expected reload failure")
	}
	if _, statErr := os.Stat(r.path(devModeMarkerRel)); statErr != nil {
		t.Fatalf("marker must remain when rotate aborts before the last step: %v", statErr)
	}
}

func TestSecretsTokenAdminChangeFlagsRegister(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	admin := r.path("third_party/go-lab-ldap-mcp/secrets/token-admin")
	if err := os.WriteFile(admin, []byte("stale-admin-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists := map[string]bool{"labldap": true, "mcpjungle": true, "labdns": true}
	installTestSecretsDeps(r, exists)
	var registered, labldap, gateway bool
	var mains []string
	r.deps.register = func() error { registered = true; return nil }
	r.deps.reloadLabLDAP = func() error { labldap = true; return nil }
	r.deps.reloadGateway = func() error { gateway = true; return nil }
	r.deps.reloadMain = func(service string) error {
		mains = append(mains, service)
		return nil
	}

	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if got := readTrim(t, r, "third_party/go-lab-ldap-mcp/secrets/token-admin"); got != "lab-dev-labldap-token-admin" {
		t.Fatalf("token-admin = %q", got)
	}
	if !r.alreadyReloaded("register") && !r.alreadyReloaded("mcpjungle") {
		t.Fatalf("reloadedThisRun=%v; changing only token-admin must flag register (or mcpjungle), not merely labldap", r.reloadedThisRun)
	}
	if !r.alreadyReloaded("labldap") {
		t.Fatal("token-admin change must reload labldap")
	}
	if r.alreadyReloaded("mcpjungle") || gateway {
		t.Fatal("token-admin alone must Register(), not reloadGateway")
	}
	if !registered {
		t.Fatal("expected Register() for registrarEnv token-admin")
	}
	if !labldap {
		t.Fatal("expected reloadLabLDAP")
	}
	for _, s := range mains {
		t.Errorf("unexpected main reload %s", s)
	}
}

func TestSecretsMatchingCatalogDoesNotReload(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	exists := map[string]bool{
		"labdns": true, "maildev": true, "labinfo": true,
		"mcpjungle": true, "labldap": true, "labtacacs": true,
	}
	installTestSecretsDeps(r, exists)
	var hits []string
	r.deps.reloadMain = func(s string) error { hits = append(hits, "main:"+s); return nil }
	r.deps.reloadGateway = func() error { hits = append(hits, "gateway"); return nil }
	r.deps.reloadLabLDAP = func() error { hits = append(hits, "labldap"); return nil }
	r.deps.reloadLabTacacs = func() error { hits = append(hits, "labtacacs"); return nil }
	r.deps.register = func() error { hits = append(hits, "register"); return nil }
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("unchanged catalog must not reload: %v", hits)
	}
	if len(r.reloadedThisRun) != 0 {
		t.Fatalf("reloadedThisRun=%v, want empty", r.reloadedThisRun)
	}
}

func TestAlreadyReloaded(t *testing.T) {
	r := &Runner{}
	if r.alreadyReloaded("labldap") {
		t.Fatal("nil map must not skip")
	}
	r.markReloaded("labldap")
	r.markReloaded("register")
	if !r.alreadyReloaded("labldap") || !r.alreadyReloaded("register") {
		t.Fatal("expected marks")
	}
	if r.alreadyReloaded("labtacacs") {
		t.Fatal("unmarked name")
	}
}

func TestWriteTokenAlwaysUnlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labdns-token")
	if err := os.WriteFile(path, []byte("lab-dev-labdns-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTokenAlways(path, 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(mustRead(t, path)))
	if got == "lab-dev-labdns-token" {
		t.Fatal("writeTokenAlways left catalog value")
	}
	if len(got) != 64 {
		t.Fatalf("got %q", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWriteTokenIfMissingCreates0644(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labmitm-token")
	if err := writeTokenIfMissing(path, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.TrimSpace(string(b))
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		t.Fatalf("token is not hex: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("token length = %d, want 32 bytes", len(decoded))
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %04o, want 0644", fi.Mode().Perm())
	}
}

func TestWriteTokenIfMissingChmodsExistingTo0644(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labmitm-token")
	const body = "keep-me\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeTokenIfMissing(path, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("content = %q, want %q", got, body)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %04o, want 0644", fi.Mode().Perm())
	}
}
