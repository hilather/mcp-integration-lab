package lab

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/profile"
	"github.com/hilather/mcp-integration-lab/internal/taclabcfg"
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
	p := taclabcfg.TestParams
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
		reloadMain:         func(string) error { return nil },
		reloadGateway:      func() error { return nil },
		reloadLabLDAP:      func() error { return nil },
		reloadLabTacacs:    func() error { return nil },
		register:           func() error { return nil },
		taclabArgon2Params: &p,
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

// fakeLabgenAlways is a pin-bump stand-in: labgen -force always rewrites
// random TacLab secrets, even if catalog files already exist.
func fakeLabgenAlways(root string) error {
	dir := filepath.Join(root, taclabDir, "deployments", "compose", "secrets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
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
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := writeRandHexFile(path, 32, 0o444, false); err != nil {
			return err
		}
	}
	pw := strings.Join([]string{
		"lab-admin=rand-admin",
		"lab-admin-enable=rand-enable",
		"lab-readonly=rand-readonly",
		"lab-disabled=rand-disabled",
		"lab-admin-challenge=rand-challenge",
		"",
	}, "\n")
	return os.WriteFile(filepath.Join(dir, "PASSWORDS.txt"), []byte(pw), 0o600)
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
	assertTaclabCatalog(t, r)
	m, err := parseDevModeMarkerFile(r.path(devModeMarkerRel))
	if err != nil {
		t.Fatalf("dev-mode marker missing: %v", err)
	}
	if m.reloads != reloadsDone {
		t.Fatalf("reloads=%q, want %s", m.reloads, reloadsDone)
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
	if readTrim(t, r, taclabSecretRel("api_admin_token")) == "lab-dev-labtacacs-token-admin" {
		t.Fatal("non-dev must not write catalog TacLab token")
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
	if readTrim(t, r, taclabSecretRel("api_admin_token")) == "lab-dev-labtacacs-token-admin" {
		t.Fatal("leave-dev must not keep catalog TacLab token")
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

func TestSecretsEnterDevRetriesReloadsWhenPending(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	prev, err := parseDevModeMarkerFile(r.path(devModeMarkerRel))
	if err != nil {
		t.Fatal(err)
	}
	if prev.reloads != reloadsDone {
		t.Fatalf("precondition reloads=%q", prev.reloads)
	}
	if err := writeDevModeMarker(r.path(devModeMarkerRel), r.Prof.Name, prev.catalogSHA, reloadsPending); err != nil {
		t.Fatal(err)
	}

	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	var labldap bool
	r.deps.reloadLabLDAP = func() error { labldap = true; return nil }
	r.deps.reloadMain = func(string) error { return nil }
	r.deps.reloadGateway = func() error { return nil }
	r.deps.register = func() error { return nil }

	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if !labldap || !r.alreadyReloaded("labldap") {
		t.Fatal("pending enter-dev must reloadLabLDAP even when plaintext matches")
	}
	if got := readTrim(t, r, "third_party/go-lab-ldap-mcp/secrets/user-alice"); got != "lab-dev-alice-12" {
		t.Fatalf("Alice = %q, want catalog", got)
	}
	done, err := parseDevModeMarkerFile(r.path(devModeMarkerRel))
	if err != nil {
		t.Fatal(err)
	}
	if done.reloads != reloadsDone {
		t.Fatalf("reloads=%q after retry, want %s", done.reloads, reloadsDone)
	}
}

func TestSecretsEnterDevRetriesReloadsWhenMarkerMissing(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.path(devModeMarkerRel)); err != nil {
		t.Fatal(err)
	}
	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	var labldap bool
	r.deps.reloadLabLDAP = func() error { labldap = true; return nil }

	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if !labldap {
		t.Fatal("missing marker must reload running LabLDAP even when plaintext matches")
	}
	if got := readTrim(t, r, "third_party/go-lab-ldap-mcp/secrets/user-alice"); got != "lab-dev-alice-12" {
		t.Fatalf("Alice = %q, want catalog", got)
	}
}

func TestSecretsLeaveDevKeepsMarkerIfComposePsFails(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	r.Prof.Values["LAB_DEV_MODE"] = "false"
	installTestSecretsDeps(r, nil)
	r.deps.containerExists = func(string) (bool, error) {
		return false, errors.New("compose ps failed")
	}
	err := r.Secrets()
	if err == nil || !strings.Contains(err.Error(), "compose ps failed") {
		t.Fatalf("want compose ps failure, got %v", err)
	}
	if _, statErr := os.Stat(r.path(devModeMarkerRel)); statErr != nil {
		t.Fatalf("marker must remain when inspect fails: %v", statErr)
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

func TestSecretsDevPinsTaclabAfterForcedLabgen(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	r.deps.ensureTaclab = func(force bool) error {
		return fakeLabgenAlways(r.Root)
	}
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	assertTaclabCatalog(t, r)
}

func TestSecretsTaclabTokenChangeFlagsRegister(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	token := r.path(taclabSecretRel("api_admin_token"))
	if err := os.Chmod(token, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("stale-taclab-token"), 0o444); err != nil {
		t.Fatal(err)
	}
	exists := map[string]bool{"labtacacs": true, "mcpjungle": true}
	installTestSecretsDeps(r, exists)
	var registered, gateway, labtacacs bool
	r.deps.register = func() error { registered = true; return nil }
	r.deps.reloadGateway = func() error { gateway = true; return nil }
	r.deps.reloadLabTacacs = func() error { labtacacs = true; return nil }
	r.deps.reloadLabLDAP = func() error { t.Fatal("unexpected labldap reload"); return nil }
	r.deps.reloadMain = func(string) error { t.Fatal("unexpected main reload"); return nil }

	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if got := readTrim(t, r, taclabSecretRel("api_admin_token")); got != "lab-dev-labtacacs-token-admin" {
		t.Fatalf("api_admin_token = %q", got)
	}
	if !r.alreadyReloaded("labtacacs") || !labtacacs {
		t.Fatal("api_admin_token change must reloadLabTacacs")
	}
	if r.alreadyReloaded("mcpjungle") || gateway {
		t.Fatal("token alone must Register(), not reloadGateway")
	}
	if !r.alreadyReloaded("register") || !registered {
		t.Fatal("api_admin_token change must Register() (LABTACACS_TOKEN)")
	}
}

func TestSecretsTaclabPHCRewriteDoesNotReload(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	phcPath := r.path(taclabSecretRel("lab_admin_argon2id"))
	if err := os.Chmod(phcPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(phcPath, []byte("not-a-phc"), 0o444); err != nil {
		t.Fatal(err)
	}
	installTestSecretsDeps(r, map[string]bool{"labtacacs": true, "mcpjungle": true})
	var hits []string
	r.deps.reloadLabTacacs = func() error { hits = append(hits, "labtacacs"); return nil }
	r.deps.register = func() error { hits = append(hits, "register"); return nil }
	r.deps.reloadGateway = func() error { hits = append(hits, "gateway"); return nil }

	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if err := taclabcfg.VerifyArgon2id(mustRead(t, phcPath), []byte("LabAdmin-Dev-Pass-01!")); err != nil {
		t.Fatalf("PHC not rewritten: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("PHC rewrite must not bounce AAA: %v", hits)
	}
	if len(r.reloadedThisRun) != 0 {
		t.Fatalf("reloadedThisRun=%v", r.reloadedThisRun)
	}
}

func TestSecretsEnterDevRetriesLabTacacsWhenPending(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	prev, err := parseDevModeMarkerFile(r.path(devModeMarkerRel))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDevModeMarker(r.path(devModeMarkerRel), r.Prof.Name, prev.catalogSHA, reloadsPending); err != nil {
		t.Fatal(err)
	}
	installTestSecretsDeps(r, map[string]bool{"labtacacs": true})
	var labtacacs bool
	r.deps.reloadLabTacacs = func() error { labtacacs = true; return nil }
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if !labtacacs || !r.alreadyReloaded("labtacacs") {
		t.Fatal("pending enter-dev must reloadLabTacacs even when plaintext matches")
	}
}

func taclabSecretRel(name string) string {
	return taclabDir + "/deployments/compose/secrets/" + name
}

func assertTaclabCatalog(t *testing.T, r *Runner) {
	t.Helper()
	plain := map[string]string{
		"api_admin_token":            "lab-dev-labtacacs-token-admin",
		"lab_switches_tacacs_secret": "LabDev-Switches-Tacacs-01",
		"lab_switches_radius_secret": "LabDev-Switches-Radius-01",
		"lab_admin_challenge_secret": "LabChallenge-Dev-Pass-01!",
	}
	for name, exp := range plain {
		b := mustRead(t, r.path(taclabSecretRel(name)))
		if bytes.Contains(b, []byte("\n")) {
			t.Errorf("%s has newline", name)
		}
		if string(b) != exp {
			t.Errorf("%s = %q, want %q", name, b, exp)
		}
	}
	pw := string(mustRead(t, r.path(taclabSecretRel("PASSWORDS.txt"))))
	for _, line := range []string{
		"lab-admin=LabAdmin-Dev-Pass-01!",
		"lab-admin-enable=LabEnable-Dev-Pass-01!",
		"lab-readonly=LabReadonly-Dev-Pass-01!",
		"lab-disabled=LabDisabled-Dev-Pass-01!",
		"lab-admin-challenge=LabChallenge-Dev-Pass-01!",
	} {
		if !strings.Contains(pw, line+"\n") {
			t.Errorf("PASSWORDS.txt missing %q\\n:\n%s", line, pw)
		}
	}
	for _, h := range []struct{ file, password string }{
		{"lab_admin_argon2id", "LabAdmin-Dev-Pass-01!"},
		{"lab_admin_enable_argon2id", "LabEnable-Dev-Pass-01!"},
		{"lab_readonly_argon2id", "LabReadonly-Dev-Pass-01!"},
		{"lab_disabled_argon2id", "LabDisabled-Dev-Pass-01!"},
	} {
		b := mustRead(t, r.path(taclabSecretRel(h.file)))
		if bytes.Contains(b, []byte("\n")) {
			t.Errorf("%s has newline", h.file)
		}
		if err := taclabcfg.VerifyArgon2id(b, []byte(h.password)); err != nil {
			t.Errorf("%s: %v", h.file, err)
		}
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
