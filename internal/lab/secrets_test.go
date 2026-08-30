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

	"github.com/hilather/mcp-integration-lab/internal/labinfo"
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
		ensureTaclab: func(force bool, secretsFromAbs string) error {
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

func fakeSetuptls(root string) error {
	dir := filepath.Join(root, "third_party", "go-lab-ldap-mcp", "secrets", "tls")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "ca.crt")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte("-----BEGIN CERTIFICATE-----\nTESTCA\n-----END CERTIFICATE-----\n"), 0o644)
}

func fakeLabgen(root string, force bool) error {
	dir := filepath.Join(root, taclabDir, "deployments", "compose", "secrets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"api_admin_token", "lab_switches_radius_secret", "lab_switches_tacacs_secret"} {
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
	pwPath := filepath.Join(dir, "PASSWORDS.txt")
	if force {
		if err := os.Remove(pwPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if _, err := os.Stat(pwPath); err != nil {
		body := "# labgen PASSWORDS.txt\n" +
			"lab-admin=fake-lab-admin\n" +
			"lab-admin-enable=fake-lab-admin-enable\n" +
			"lab-readonly=fake-lab-readonly\n" +
			"lab-disabled=fake-lab-disabled\n" +
			"lab-admin-challenge=fake-lab-admin-challenge\n"
		if force {
			body = "# labgen PASSWORDS.txt\n" +
				"lab-admin=rotated-lab-admin\n" +
				"lab-admin-enable=rotated-lab-admin-enable\n" +
				"lab-readonly=rotated-lab-readonly\n" +
				"lab-disabled=rotated-lab-disabled\n" +
				"lab-admin-challenge=rotated-lab-admin-challenge\n"
		}
		if err := os.WriteFile(pwPath, []byte(body), 0o600); err != nil {
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

func TestServiceExistsKnowsLabMITM(t *testing.T) {
	// applySecretReloads calls reloadMainIf("labmitm") without a mock in
	// production. The inspect switch must know that name; mocked
	// containerExists tests cannot catch a missing case.
	_, err := (&Runner{}).serviceExists("labmitm")
	if err != nil && strings.Contains(err.Error(), `unknown service "labmitm"`) {
		t.Fatal(`serviceExists must inspect main compose labmitm`)
	}
}

func TestServiceExistsKnowsLabgraph(t *testing.T) {
	_, err := (&Runner{}).serviceExists("labgraph")
	if err != nil && strings.Contains(err.Error(), `unknown service "labgraph"`) {
		t.Fatal(`serviceExists must inspect main compose labgraph`)
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
		"secrets/labmail-token":                             "lab-dev-labmail-token-32b-minimum",
		"secrets/labmitm-token":                             "lab-dev-labmitm-token-32b-minimum",
		"secrets/labgraph-token":                            "lab-dev-labgraph-token",
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
	var gotAbs string
	r.deps.ensureTaclab = func(force bool, secretsFromAbs string) error {
		gotAbs = secretsFromAbs
		return fakeLabgen(r.Root, force)
	}
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"secrets/labdns-token",
		"secrets/labinfo-token",
		"secrets/labmail-token",
		"secrets/labmitm-token",
		"secrets/labgraph-token",
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
	if _, err := os.Stat(r.path(taclabSecretsFromRel)); !os.IsNotExist(err) {
		t.Fatalf("non-dev must not write secrets-from YAML: %v", err)
	}
	if gotAbs != "" {
		t.Fatalf("secretsFromAbs=%q, want empty", gotAbs)
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

func TestSecretsDevFailsClosedShortApplianceToken(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", mustRead(t, testdataDevcreds("labmail-short.yaml")))
	err := r.Secrets()
	if err == nil || !strings.Contains(err.Error(), "spec.tokens.labmail") {
		t.Fatalf("want enter-dev fail-closed on short labmail token, got %v", err)
	}
	if !strings.Contains(err.Error(), "MinTokenBytes") {
		t.Fatalf("expected MinTokenBytes in error, got %v", err)
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
	caKey := mustRead(t, r.path("third_party/go-lab-ldap-mcp/secrets/tls/ca.key"))

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
	if got := mustRead(t, r.path("third_party/go-lab-ldap-mcp/secrets/tls/ca.key")); string(got) != string(caKey) {
		t.Fatal("leave-dev must not rotate LabLDAP CA")
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
	exists := map[string]bool{"labldap": true, "mcpjungle": true, "labdns": true, "labgraph": true}
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
	foundGraph := false
	for _, s := range mains {
		if s == "labgraph" {
			foundGraph = true
			continue
		}
		t.Errorf("unexpected main reload %s", s)
	}
	if !foundGraph {
		t.Fatal("token-admin change must reload labgraph (appliance token copy)")
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

func TestSecretsEnterDevArmsPendingBeforeSubprocesses(t *testing.T) {
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

	// Catalog change lands on disk in applyDevCatalog, which used to run
	// before the pending marker. A later setupsecrets error then left
	// reloads=done; retry saw a plaintext match and skipped LabLDAP.
	mutated := bytes.Replace(validCatalogBytes(t),
		[]byte("lab-dev-alice-12"),
		[]byte("lab-dev-alice-99"),
		1)
	if err := os.WriteFile(filepath.Join(r.Prof.Dir, "dev-credentials.yaml"), mutated, 0o644); err != nil {
		t.Fatal(err)
	}

	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	r.deps.setupsecrets = func(bool) error {
		return errors.New("setupsecrets boom")
	}
	err = r.Secrets()
	if err == nil || !strings.Contains(err.Error(), "setupsecrets boom") {
		t.Fatalf("want setupsecrets failure, got %v", err)
	}
	mid, err := parseDevModeMarkerFile(r.path(devModeMarkerRel))
	if err != nil {
		t.Fatal(err)
	}
	if mid.reloads != reloadsPending {
		t.Fatalf("after catalog write + subprocess fail, reloads=%q, want %s", mid.reloads, reloadsPending)
	}
	if got := readTrim(t, r, "third_party/go-lab-ldap-mcp/secrets/user-alice"); got != "lab-dev-alice-99" {
		t.Fatalf("Alice on disk = %q, want catalog write to have landed", got)
	}

	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	var labldap bool
	r.deps.reloadLabLDAP = func() error { labldap = true; return nil }
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if !labldap || !r.alreadyReloaded("labldap") {
		t.Fatal("retry after catalog-write-then-fail must reloadLabLDAP (disk already matches)")
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

func TestApplySecretReloadsLabgraphOnLeaveDev(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\n", nil)
	installTestSecretsDeps(r, map[string]bool{"labgraph": true})
	var mains []string
	r.deps.reloadMain = func(s string) error { mains = append(mains, s); return nil }
	if err := r.applySecretReloads(secretChanges{}, true); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range mains {
		if s == "labgraph" {
			found = true
		}
	}
	if !found {
		t.Fatalf("leave-dev must reload labgraph; mains=%v", mains)
	}
}

func TestApplySecretReloadsLabgraphOnLabdnsTokenOnly(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\n", nil)
	installTestSecretsDeps(r, map[string]bool{"labgraph": true, "labdns": true})
	var mains []string
	r.deps.reloadMain = func(s string) error { mains = append(mains, s); return nil }
	if err := r.applySecretReloads(secretChanges{labdnsToken: true}, false); err != nil {
		t.Fatal(err)
	}
	foundDNS, foundGraph := false, false
	for _, s := range mains {
		switch s {
		case "labdns":
			foundDNS = true
		case "labgraph":
			foundGraph = true
		}
	}
	if !foundDNS || !foundGraph {
		t.Fatalf("labdns-token-only must bounce labdns and labgraph; mains=%v", mains)
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

func TestLabgenArgsSecretsFrom(t *testing.T) {
	path := "/abs/secrets/taclab-secrets-from.yaml"
	cases := []struct {
		name, secretsFrom string
		force             bool
		want              []string
	}{
		{
			name:        "force+path",
			force:       true,
			secretsFrom: path,
			want:        []string{"run", "./tools/labgen", "-force", "-secrets-from", path, "deployments/compose"},
		},
		{
			name:        "first-mint+path",
			force:       false,
			secretsFrom: path,
			want:        []string{"run", "./tools/labgen", "-secrets-from", path, "deployments/compose"},
		},
		{
			name:        "force+empty",
			force:       true,
			secretsFrom: "",
			want:        []string{"run", "./tools/labgen", "-force", "deployments/compose"},
		},
		{
			name:        "neither",
			force:       false,
			secretsFrom: "",
			want:        []string{"run", "./tools/labgen", "deployments/compose"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := labgenArgs(tc.force, tc.secretsFrom)
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d want %d: %v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("args=%v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestEnterDevWritesSecretsFromYAML(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	var sawYAML bool
	var gotAbs string
	wantAbs := r.path(taclabSecretsFromRel)
	r.deps.ensureTaclab = func(force bool, secretsFromAbs string) error {
		gotAbs = secretsFromAbs
		b, err := os.ReadFile(wantAbs)
		if err != nil {
			t.Errorf("YAML missing when ensureTaclab ran: %v", err)
			return fakeLabgen(r.Root, force)
		}
		sawYAML = true
		golden, err := os.ReadFile(filepath.Join("..", "taclabcfg", "testdata", "secrets-from.golden.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b, golden) {
			t.Errorf("written YAML != golden\ngot:\n%s", b)
		}
		if secretsFromAbs != wantAbs {
			t.Errorf("secretsFromAbs=%q, want %q", secretsFromAbs, wantAbs)
		}
		return fakeLabgen(r.Root, force)
	}
	if err := r.secretsEnterDev(r.path(devModeMarkerRel)); err != nil {
		t.Fatal(err)
	}
	if !sawYAML {
		t.Fatal("ensureTaclab ran before YAML write")
	}
	if gotAbs != wantAbs {
		t.Fatalf("secretsFromAbs=%q, want %q", gotAbs, wantAbs)
	}
	assertTaclabCatalog(t, r)
}

func TestLeaveDevUnlinksSecretsFromYAML(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\n", validCatalogBytes(t))
	path := r.path(taclabSecretsFromRel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("leftover\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotAbs string
	r.deps.ensureTaclab = func(force bool, secretsFromAbs string) error {
		gotAbs = secretsFromAbs
		return fakeLabgen(r.Root, force)
	}
	if err := r.leaveDevRemint(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("leftover YAML still present: %v", err)
	}
	if gotAbs != "" {
		t.Fatalf("secretsFromAbs=%q, want empty", gotAbs)
	}
}

func TestSecretsDevPinsTaclabAfterForcedLabgen(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\n", validCatalogBytes(t))
	r.deps.ensureTaclab = func(force bool, secretsFromAbs string) error {
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
	exists := map[string]bool{"labtacacs": true, "mcpjungle": true, "labgraph": true}
	installTestSecretsDeps(r, exists)
	var registered, gateway, labtacacs, graph bool
	r.deps.register = func() error { registered = true; return nil }
	r.deps.reloadGateway = func() error { gateway = true; return nil }
	r.deps.reloadLabTacacs = func() error { labtacacs = true; return nil }
	r.deps.reloadLabLDAP = func() error { t.Fatal("unexpected labldap reload"); return nil }
	r.deps.reloadMain = func(s string) error {
		if s != "labgraph" {
			t.Fatalf("unexpected main reload %s", s)
		}
		graph = true
		return nil
	}

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
	if !graph {
		t.Fatal("api_admin_token change must reload labgraph")
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

func TestSecretsPublicHostChangeReloadsLabLDAP(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nLAB_PUBLIC_HOST=lab.example.test\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	assertVendorTLS(t, r, "lab.example.test")
	tlsDir := r.path("third_party/go-lab-ldap-mcp/secrets/tls")
	caKey := mustRead(t, filepath.Join(tlsDir, "ca.key"))

	r.Prof.Values["LAB_PUBLIC_HOST"] = "203.0.113.10"
	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	var labldap, registered, gateway bool
	var mains []string
	r.deps.reloadLabLDAP = func() error { labldap = true; return nil }
	r.deps.register = func() error { registered = true; return nil }
	r.deps.reloadGateway = func() error { gateway = true; return nil }
	r.deps.reloadMain = func(s string) error { mains = append(mains, s); return nil }

	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if !labldap || !r.alreadyReloaded("labldap") {
		t.Fatalf("leaf re-sign must reloadLabLDAP; reloadedThisRun=%v", r.reloadedThisRun)
	}
	if registered || gateway || len(mains) != 0 {
		t.Fatalf("TLS re-sign is not a registrarEnv change: register=%v gateway=%v mains=%v", registered, gateway, mains)
	}
	if !bytes.Equal(caKey, mustRead(t, filepath.Join(tlsDir, "ca.key"))) {
		t.Fatal("must not rotate ca.key")
	}
	assertVendorTLS(t, r, "203.0.113.10")
	if r.labldapTLSReloadPending() {
		t.Fatal("successful reload must clear TLS pending")
	}
}

func TestSecretsNonDevSANChangeReloadsLabLDAP(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\nLAB_PUBLIC_HOST=lab.example.test\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	tlsDir := r.path("third_party/go-lab-ldap-mcp/secrets/tls")
	caKey := mustRead(t, filepath.Join(tlsDir, "ca.key"))

	r.Prof.Values["LAB_PUBLIC_HOST"] = "2001:db8::10"
	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	var labldap bool
	var hits []string
	r.deps.reloadLabLDAP = func() error { labldap = true; return nil }
	r.deps.reloadMain = func(s string) error { hits = append(hits, s); return nil }
	r.deps.reloadGateway = func() error { hits = append(hits, "gateway"); return nil }

	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if !labldap || !r.alreadyReloaded("labldap") {
		t.Fatal("non-dev leaf re-sign must reloadLabLDAP")
	}
	if len(hits) != 0 {
		t.Fatalf("TLS re-sign must not bounce other apps: %v", hits)
	}
	if !bytes.Equal(caKey, mustRead(t, filepath.Join(tlsDir, "ca.key"))) {
		t.Fatal("must not rotate ca.key")
	}
	assertVendorTLS(t, r, "2001:db8::10")
	if r.labldapTLSReloadPending() {
		t.Fatal("successful reload must clear TLS pending")
	}
}

func TestSecretsNonDevTLSReloadRetriedWhenPending(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\nLAB_PUBLIC_HOST=lab.example.test\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	r.Prof.Values["LAB_PUBLIC_HOST"] = "203.0.113.10"
	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	r.deps.reloadLabLDAP = func() error { return errors.New("reload failed") }
	err := r.Secrets()
	if err == nil || !strings.Contains(err.Error(), "reload failed") {
		t.Fatalf("want reload failure, got %v", err)
	}
	if !r.labldapTLSReloadPending() {
		t.Fatal("pending TLS reload marker must remain after failure")
	}
	assertVendorTLS(t, r, "203.0.113.10")

	var n int
	r.deps.reloadLabLDAP = func() error { n++; return nil }
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("second Secrets with complete SANs must still reloadLabLDAP, n=%d", n)
	}
	if !r.alreadyReloaded("labldap") {
		t.Fatal("retry must mark labldap reloaded")
	}
	if r.labldapTLSReloadPending() {
		t.Fatal("pending marker must clear after successful reload")
	}
}

func TestSecretsNonDevTLSReloadPendingWhenInspectFails(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\nLAB_PUBLIC_HOST=lab.example.test\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	r.Prof.Values["LAB_PUBLIC_HOST"] = "203.0.113.10"
	installTestSecretsDeps(r, nil)
	r.deps.containerExists = func(string) (bool, error) {
		return false, errors.New("compose ps failed")
	}
	err := r.Secrets()
	if err == nil || !strings.Contains(err.Error(), "compose ps failed") {
		t.Fatalf("want inspect failure, got %v", err)
	}
	if !r.labldapTLSReloadPending() {
		t.Fatal("inspect failure must not drop the TLS reload pending marker")
	}

	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	var n int
	r.deps.reloadLabLDAP = func() error { n++; return nil }
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("retry after inspect failure must reloadLabLDAP, n=%d", n)
	}
	if r.labldapTLSReloadPending() {
		t.Fatal("pending marker must clear after successful reload")
	}
}

func TestSecretsNonDevTLSPendingClearedWhenDirectoryAbsent(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\nLAB_PUBLIC_HOST=lab.example.test\n", nil)
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if r.labldapTLSReloadPending() {
		t.Fatal("first mint with no directory must not leave a pending reload")
	}
}

func TestSecretsDevSANChangeReloadRetriedWhenPending(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nLAB_PUBLIC_HOST=lab.example.test\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	r.Prof.Values["LAB_PUBLIC_HOST"] = "203.0.113.10"
	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	r.deps.reloadLabLDAP = func() error { return errors.New("reload failed") }
	if err := r.Secrets(); err == nil {
		t.Fatal("expected reload failure")
	}
	if !r.labldapTLSReloadPending() {
		t.Fatal("dev SAN rewrite must persist TLS reload pending")
	}

	var n int
	r.deps.reloadLabLDAP = func() error { n++; return nil }
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("dev retry with complete SANs must still reloadLabLDAP, n=%d", n)
	}
	if r.labldapTLSReloadPending() {
		t.Fatal("pending marker must clear after successful reload")
	}
}

func TestSecretsLeaveDevSANResignReloadsLabLDAP(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nLAB_PUBLIC_HOST=lab.example.test\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	tlsDir := r.path("third_party/go-lab-ldap-mcp/secrets/tls")
	caKey := mustRead(t, filepath.Join(tlsDir, "ca.key"))

	r.Prof.Values["LAB_DEV_MODE"] = "false"
	r.Prof.Values["LAB_PUBLIC_HOST"] = "203.0.113.10"
	installTestSecretsDeps(r, map[string]bool{"labldap": true})
	var labldap bool
	r.deps.reloadLabLDAP = func() error { labldap = true; return nil }

	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if !labldap {
		t.Fatal("leave-dev leaf re-sign must still reloadLabLDAP")
	}
	if !bytes.Equal(caKey, mustRead(t, filepath.Join(tlsDir, "ca.key"))) {
		t.Fatal("leave-dev must not rotate ca.key")
	}
	assertVendorTLS(t, r, "203.0.113.10")
}

func assertVendorTLS(t *testing.T, r *Runner, publicHost string) {
	t.Helper()
	tlsDir := r.path("third_party/go-lab-ldap-mcp/secrets/tls")
	assertDirectorySANs(t, mustCert(t, filepath.Join(tlsDir, "directory.crt")), publicHost)
	assertManagementSANs(t, mustCert(t, filepath.Join(tlsDir, "management.crt")), publicHost)
	if _, err := os.Stat(r.path("secrets/tls/ca.crt")); !os.IsNotExist(err) {
		t.Fatalf("must not write repo-root secrets/tls: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tlsDir, "ca.key")); err != nil {
		t.Fatal(err)
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

func TestParseLabgenPasswords(t *testing.T) {
	got := parseLabgenPasswords([]byte(`# header
lab-admin=Alpha
lab-admin-enable=Bravo
lab-readonly=Charlie

lab-disabled=Delta
lab-admin-challenge=Echo
lab-spaced = value with spaces
`))
	want := map[string]string{
		"lab-admin":           "Alpha",
		"lab-admin-enable":    "Bravo",
		"lab-readonly":        "Charlie",
		"lab-disabled":        "Delta",
		"lab-admin-challenge": "Echo",
		"lab-spaced":          "value with spaces",
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %#v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s=%q, want %q", k, got[k], v)
		}
	}
}

func TestStageLabinfoCredsLockstepWithCatalog(t *testing.T) {
	cat, err := labinfo.Load(filepath.Join("..", "..", "profiles", "default", "labinfo", "services.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	staged := map[string]bool{}
	for _, f := range labinfoCredFiles {
		staged[f.dst] = true
	}
	for _, k := range labinfoPasswordKeys {
		staged[k.dst] = true
	}
	for _, s := range cat.Services {
		if s.ID == "labinfo" {
			t.Fatal("do not add a labinfo catalog service for labinfo-token")
		}
		if s.Credential != nil {
			dst := filepath.Base(s.Credential.File)
			if !staged[dst] {
				t.Errorf("catalog %s web credential file %s is not staged", s.ID, dst)
			}
		}
		if s.Connection == nil {
			continue
		}
		for _, c := range s.Connection.Credentials {
			dst := filepath.Base(c.File)
			if !staged[dst] {
				t.Errorf("catalog %s connection credential %s file %s is not staged", s.ID, c.Name, dst)
			}
		}
	}
}

func TestStageLabinfoCredsSplitsPasswordsWithoutApplyDev(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\n", nil)
	if err := writeStageSources(r, false); err != nil {
		t.Fatal(err)
	}
	if err := r.stageLabinfoCreds(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"labtacacs-lab-admin":           "split-admin",
		"labtacacs-lab-admin-enable":    "split-enable",
		"labtacacs-lab-readonly":        "split-readonly",
		"labtacacs-lab-disabled":        "split-disabled",
		"labtacacs-lab-admin-challenge": "split-challenge",
		"labtacacs-tacacs-secret":       "tacacs-secret-bytes",
		"labldap-ca.crt":                "-----BEGIN CERTIFICATE-----\nLABCA\n-----END CERTIFICATE-----",
	}
	for dst, exp := range want {
		got := strings.TrimSpace(string(mustRead(t, r.path("secrets/labinfo-creds/"+dst))))
		if got != exp {
			t.Errorf("%s = %q, want %q", dst, got, exp)
		}
	}
	if _, err := os.Stat(r.path("secrets/labinfo-creds/tacacs-client-ca.pem")); !os.IsNotExist(err) {
		t.Fatalf("optional missing client CA should not be staged: %v", err)
	}
}

func TestStageLabinfoCredsFailsClosedOnMissingRequired(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\n", nil)
	if err := writeStageSources(r, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.path("third_party/go-lab-ldap-mcp/secrets/tls/ca.crt")); err != nil {
		t.Fatal(err)
	}
	err := r.stageLabinfoCreds()
	if err == nil || !strings.Contains(err.Error(), "ca.crt") {
		t.Fatalf("want fail-closed missing ca.crt, got %v", err)
	}
}

func TestStageLabinfoCredsCopiesOptionalClientCerts(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\n", nil)
	if err := writeStageSources(r, true); err != nil {
		t.Fatal(err)
	}
	if err := r.stageLabinfoCreds(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(mustRead(t, r.path("secrets/labinfo-creds/tacacs-client-ca.pem")))); got != "-----BEGIN CERTIFICATE-----\nCLIENTCA\n-----END CERTIFICATE-----" {
		t.Fatalf("client-ca = %q", got)
	}
	if got := strings.TrimSpace(string(mustRead(t, r.path("secrets/labinfo-creds/tacacs-client-ok.pem")))); got != "-----BEGIN CERTIFICATE-----\nCLIENTOK\n-----END CERTIFICATE-----" {
		t.Fatalf("client-ok = %q", got)
	}
}

func TestStageLabinfoCredsRemovesStaleOptionalDest(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\n", nil)
	if err := writeStageSources(r, true); err != nil {
		t.Fatal(err)
	}
	if err := r.stageLabinfoCreds(); err != nil {
		t.Fatal(err)
	}
	for _, dst := range []string{"tacacs-client-ca.pem", "tacacs-client-ok.pem"} {
		if _, err := os.Stat(r.path("secrets/labinfo-creds/" + dst)); err != nil {
			t.Fatalf("precondition %s: %v", dst, err)
		}
	}
	for _, src := range []string{
		taclabDir + "/deployments/compose/certs-public/client-ca.pem",
		taclabDir + "/deployments/compose/certs-public/client-ok.pem",
	} {
		if err := os.Remove(r.path(src)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.stageLabinfoCreds(); err != nil {
		t.Fatal(err)
	}
	for _, dst := range []string{"tacacs-client-ca.pem", "tacacs-client-ok.pem"} {
		if _, err := os.Stat(r.path("secrets/labinfo-creds/" + dst)); !os.IsNotExist(err) {
			t.Errorf("stale optional dest %s still present: %v", dst, err)
		}
	}
}

func TestLabgenPasswordUsesSharedParser(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "PASSWORDS.txt")
	if err := os.WriteFile(path, []byte("# header\nlab-admin = spaced-admin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := labgenPassword(path, "lab-admin")
	if err != nil {
		t.Fatal(err)
	}
	if got != "spaced-admin" {
		t.Fatalf("labgenPassword = %q, want trimmed shared parse", got)
	}
}

func TestSecretsNonDevStagesPasswordsSplit(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\n", validCatalogBytes(t))
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(mustRead(t, r.path("secrets/labinfo-creds/labtacacs-lab-admin"))))
	if got != "fake-lab-admin" {
		t.Fatalf("staged lab-admin = %q, want split from PASSWORDS.txt without applyDev", got)
	}
	if _, err := os.Stat(r.path("secrets/labinfo-creds/labtacacs-tacacs-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.path("secrets/labinfo-creds/labldap-ca.crt")); err != nil {
		t.Fatal(err)
	}
}

func writeStageSources(r *Runner, withOptionalCerts bool) error {
	files := map[string]string{
		"secrets/labinfo-token":                                               "info-token\n",
		"secrets/labdns-token":                                                "dns-token\n",
		"secrets/mcp-client-token":                                            "mcp-token\n",
		"secrets/labmail-token":                                               "mail-token\n",
		"secrets/maildev-web-password":                                        "mail-pass\n",
		"secrets/labmitm-token":                                               "mitm-token\n",
		"secrets/labgraph-token":                                              "graph-token\n",
		"third_party/go-lab-ldap-mcp/secrets/token-admin":                     "ldap-admin\n",
		"third_party/go-lab-ldap-mcp/secrets/user-alice":                      "alice-pw\n",
		"third_party/go-lab-ldap-mcp/secrets/tls/ca.crt":                      "-----BEGIN CERTIFICATE-----\nLABCA\n-----END CERTIFICATE-----\n",
		taclabDir + "/deployments/compose/secrets/api_admin_token":            "tac-admin",
		taclabDir + "/deployments/compose/secrets/lab_switches_radius_secret": "radius-secret-bytes",
		taclabDir + "/deployments/compose/secrets/lab_switches_tacacs_secret": "tacacs-secret-bytes",
		taclabDir + "/deployments/compose/secrets/PASSWORDS.txt": "# header\n" +
			"lab-admin=split-admin\n" +
			"lab-admin-enable=split-enable\n" +
			"lab-readonly=split-readonly\n" +
			"lab-disabled=split-disabled\n" +
			"lab-admin-challenge=split-challenge\n",
	}
	if withOptionalCerts {
		files[taclabDir+"/deployments/compose/certs-public/client-ca.pem"] = "-----BEGIN CERTIFICATE-----\nCLIENTCA\n-----END CERTIFICATE-----\n"
		files[taclabDir+"/deployments/compose/certs-public/client-ok.pem"] = "-----BEGIN CERTIFICATE-----\nCLIENTOK\n-----END CERTIFICATE-----\n"
	}
	for rel, body := range files {
		path := r.path(rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
