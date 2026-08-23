package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/labinfo"
)

func TestCatalogFileExpectsCoversEveryRequiredKey(t *testing.T) {
	doc, err := LoadDevCredentials(testdataDevcreds("valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		key, want, passwordsKey string
	}{
		{"spec.tokens.labdns", doc.Spec.Tokens.LabDNS, ""},
		{"spec.tokens.labinfo", doc.Spec.Tokens.Labinfo, ""},
		{"spec.tokens.labmail", doc.Spec.Tokens.Labmail, ""},
		{"spec.tokens.mcpClient", doc.Spec.Tokens.MCPClient, ""},
		{"spec.tokens.labldapAdmin", doc.Spec.Tokens.LabLDAPAdmin, ""},
		{"spec.tokens.labtacacsAdmin", doc.Spec.Tokens.LabTacacsAdmin, ""},
		{"spec.passwords.maildevWeb", doc.Spec.Passwords.MaildevWeb, ""},
		{"spec.passwords.labldapAlice", doc.Spec.Passwords.LabLDAPAlice, ""},
		{"spec.passwords.labldapRuntime", doc.Spec.Passwords.LabLDAPRuntime, ""},
		{"spec.passwords.labldapDM", doc.Spec.Passwords.LabLDAPDM, ""},
		{"spec.passwords.taclabAdmin", doc.Spec.Passwords.TaclabAdmin, "lab-admin"},
		{"spec.passwords.taclabAdminEnable", doc.Spec.Passwords.TaclabAdminEnable, "lab-admin-enable"},
		{"spec.passwords.taclabReadonly", doc.Spec.Passwords.TaclabReadonly, "lab-readonly"},
		{"spec.passwords.taclabDisabled", doc.Spec.Passwords.TaclabDisabled, "lab-disabled"},
		{"spec.passwords.taclabChallenge", doc.Spec.Passwords.TaclabChallenge, ""},
		{"spec.sharedSecrets.tacacsLabSwitches", doc.Spec.SharedSecrets.TacacsLabSwitches, ""},
		{"spec.sharedSecrets.radiusLabSwitches", doc.Spec.SharedSecrets.RadiusLabSwitches, ""},
	}
	got := catalogFileExpects(doc)
	if len(got) != len(want) {
		t.Fatalf("catalogFileExpects returned %d entries, want %d (every required catalog key)", len(got), len(want))
	}
	seen := map[string]catalogFileExpect{}
	for _, e := range got {
		if e.Rel == "" || e.Want == "" {
			t.Errorf("%s: empty Rel or Want", e.Key)
		}
		if _, dup := seen[e.Key]; dup {
			t.Errorf("duplicate key %s", e.Key)
		}
		seen[e.Key] = e
	}
	for _, w := range want {
		e, ok := seen[w.key]
		if !ok {
			t.Errorf("missing catalog key %s", w.key)
			continue
		}
		if e.Want != w.want {
			t.Errorf("%s Want=%q, want %q", w.key, e.Want, w.want)
		}
		if e.PasswordsKey != w.passwordsKey {
			t.Errorf("%s PasswordsKey=%q, want %q", w.key, e.PasswordsKey, w.passwordsKey)
		}
	}
}

func TestCheckCatalogFiles(t *testing.T) {
	doc, err := LoadDevCredentials(testdataDevcreds("valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeCatalogDisk(t, root, doc)

	if mism := checkCatalogFiles(root, doc); len(mism) != 0 {
		t.Fatalf("matching disk: %v", mism)
	}

	alice := filepath.Join(root, "third_party", "go-lab-ldap-mcp", "secrets", "user-alice")
	if err := os.WriteFile(alice, []byte("not-the-catalog\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mism := checkCatalogFiles(root, doc)
	if len(mism) != 1 || !strings.Contains(mism[0], "spec.passwords.labldapAlice") {
		t.Fatalf("Alice mismatch = %v", mism)
	}
}

func TestMatchConnSecretsToDisk(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("labldap-user-alice", "lab-dev-alice-12\n")
	mustWrite("labtacacs-lab-admin", "LabAdmin-Dev-Pass-01!\n")
	mustWrite("labtacacs-radius-secret", "LabDev-Switches-Radius-01")

	idx := map[string]connCredMeta{
		"bind-password-alice":  {Service: "labldap", Basename: "labldap-user-alice"},
		"lab-admin-password":   {Service: "labtacacs", Basename: "labtacacs-lab-admin"},
		"radius-shared-secret": {Service: "labtacacs", Basename: "labtacacs-radius-secret"},
		"tacacs-client-ca":     {Service: "labtacacs", Basename: "tacacs-client-ca.pem", Optional: true},
	}

	revealed := []revealedSecret{
		{Name: "bind-password-alice", Secret: "lab-dev-alice-12"},
		{Name: "lab-admin-password", Secret: "LabAdmin-Dev-Pass-01!"},
		{Name: "radius-shared-secret", Secret: "LabDev-Switches-Radius-01"},
	}
	if mism := matchConnSecretsToDisk(revealed, dir, idx); len(mism) != 0 {
		t.Fatalf("matching staged files: %v", mism)
	}

	revealed[0].Secret = "wrong"
	mism := matchConnSecretsToDisk(revealed, dir, idx)
	if len(mism) != 1 || !strings.Contains(mism[0], "bind-password-alice") {
		t.Fatalf("Alice secret mismatch = %v", mism)
	}

	revealed[0].Secret = "lab-dev-alice-12"
	partial := revealed[:2]
	mism = matchConnSecretsToDisk(partial, dir, idx)
	if len(mism) != 1 || !strings.Contains(mism[0], "radius-shared-secret") {
		t.Fatalf("missing required cred = %v", mism)
	}
}

func TestConnCredIndexDefaultCatalog(t *testing.T) {
	cat, err := labinfo.Load(filepath.Join(defaultProfileDir(t), "labinfo", "services.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	idx := connCredIndex(cat)
	want := map[string]string{
		"mcp-client-token":      "mcp-client-token",
		"bind-password-alice":   "labldap-user-alice",
		"lab-ca":                "labldap-ca.crt",
		"radius-shared-secret":  "labtacacs-radius-secret",
		"tacacs-shared-secret":  "labtacacs-tacacs-secret",
		"lab-admin-password":    "labtacacs-lab-admin",
		"lab-admin-enable":      "labtacacs-lab-admin-enable",
		"lab-readonly-password": "labtacacs-lab-readonly",
		"labmail-token":         "labmail-token",
		"tacacs-client-ca":      "tacacs-client-ca.pem",
		"tacacs-client-ok-cert": "tacacs-client-ok.pem",
	}
	if len(idx) != len(want) {
		t.Fatalf("connCredIndex has %d names, want %d: %v", len(idx), len(want), idx)
	}
	for name, base := range want {
		meta, ok := idx[name]
		if !ok {
			t.Errorf("missing connections_list credential %s", name)
			continue
		}
		if meta.Basename != base {
			t.Errorf("%s basename=%q, want %q", name, meta.Basename, base)
		}
	}
	if !idx["tacacs-client-ca"].Optional || !idx["tacacs-client-ok-cert"].Optional {
		t.Fatal("TacLab client certs must be optional")
	}
}

func TestCIWorkflowWritesCiDevProfile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{
		"profiles/ci-dev",
		"profiles/default",
		"LAB_DEV_MODE=true",
		"profile.env",
		"make up",
		"make smoke",
		"PROFILE: ci-dev",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("ci.yml missing %q", needle)
		}
	}
	if strings.Contains(s, "LAB_DEV_MODE: true") || strings.Contains(s, "LAB_DEV_MODE: 'true'") {
		t.Fatal("ci.yml must not set LAB_DEV_MODE as process/job env (preflight on default)")
	}
	if !strings.Contains(s, "make test") {
		t.Fatal("default CI job must keep make test (non-dev)")
	}
}

func writeCatalogDisk(t *testing.T, root string, doc *DevCredentials) {
	t.Helper()
	files := map[string]string{
		"secrets/labdns-token":                                                doc.Spec.Tokens.LabDNS + "\n",
		"secrets/labinfo-token":                                               doc.Spec.Tokens.Labinfo + "\n",
		"secrets/labmail-token":                                               doc.Spec.Tokens.Labmail + "\n",
		"secrets/mcp-client-token":                                            doc.Spec.Tokens.MCPClient + "\n",
		"secrets/maildev-web-password":                                        doc.Spec.Passwords.MaildevWeb + "\n",
		"third_party/go-lab-ldap-mcp/secrets/token-admin":                     doc.Spec.Tokens.LabLDAPAdmin + "\n",
		"third_party/go-lab-ldap-mcp/secrets/user-alice":                      doc.Spec.Passwords.LabLDAPAlice + "\n",
		"third_party/go-lab-ldap-mcp/secrets/runtime-ldap":                    doc.Spec.Passwords.LabLDAPRuntime + "\n",
		"third_party/go-lab-ldap-mcp/secrets/dm.pw":                           doc.Spec.Passwords.LabLDAPDM + "\n",
		taclabDir + "/deployments/compose/secrets/api_admin_token":            doc.Spec.Tokens.LabTacacsAdmin,
		taclabDir + "/deployments/compose/secrets/lab_admin_challenge_secret": doc.Spec.Passwords.TaclabChallenge,
		taclabDir + "/deployments/compose/secrets/lab_switches_tacacs_secret": doc.Spec.SharedSecrets.TacacsLabSwitches,
		taclabDir + "/deployments/compose/secrets/lab_switches_radius_secret": doc.Spec.SharedSecrets.RadiusLabSwitches,
		taclabDir + "/deployments/compose/secrets/PASSWORDS.txt": "# header\n" +
			"lab-admin=" + doc.Spec.Passwords.TaclabAdmin + "\n" +
			"lab-admin-enable=" + doc.Spec.Passwords.TaclabAdminEnable + "\n" +
			"lab-readonly=" + doc.Spec.Passwords.TaclabReadonly + "\n" +
			"lab-disabled=" + doc.Spec.Passwords.TaclabDisabled + "\n",
	}
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
