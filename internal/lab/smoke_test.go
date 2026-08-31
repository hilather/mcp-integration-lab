package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/labinfo"
	"gopkg.in/yaml.v3"
)

func TestExpectTokenEncoding(t *testing.T) {
	cases := []struct {
		name       string
		dev, ciDev bool
		got        string
		wantErr    bool
	}{
		{"non-dev unpadded", false, false, tokenEncodingUnpadded, false},
		{"non-dev caller", false, false, tokenEncodingCaller, true},
		{"non-dev unknown", false, false, "hex", true},
		{"non-dev empty", false, false, "", true},
		{"ci-dev caller", true, true, tokenEncodingCaller, false},
		{"ci-dev unpadded", true, true, tokenEncodingUnpadded, true},
		{"ci-dev unknown", true, true, "hex", true},
		{"other-dev caller", true, false, tokenEncodingCaller, false},
		{"other-dev unpadded", true, false, tokenEncodingUnpadded, false},
		{"other-dev unknown", true, false, "hex", true},
		{"other-dev empty", true, false, "", true},
		{"non-dev ignores ciDev flag", false, true, tokenEncodingUnpadded, false},
		{"non-dev ignores ciDev flag caller", false, true, tokenEncodingCaller, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := expectTokenEncoding(tc.dev, tc.ciDev, tc.got)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

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
		{"spec.tokens.labmitm", doc.Spec.Tokens.LabMITM, ""},
		{"spec.tokens.labgraph", doc.Spec.Tokens.Labgraph, ""},
		{"spec.tokens.labntp", doc.Spec.Tokens.LabNTP, ""},
		{"spec.tokens.labsso", doc.Spec.Tokens.LabSSO, ""},
		{"spec.tokens.mcpClient", doc.Spec.Tokens.MCPClient, ""},
		{"spec.tokens.labldapAdmin", doc.Spec.Tokens.LabLDAPAdmin, ""},
		{"spec.tokens.labtacacsAdmin", doc.Spec.Tokens.LabTacacsAdmin, ""},
		{"spec.passwords.maildevWeb", doc.Spec.Passwords.MaildevWeb, ""},
		{"spec.passwords.labssoAlice", doc.Spec.Passwords.LabSSOAlice, ""},
		{"spec.passwords.labldapAlice", doc.Spec.Passwords.LabLDAPAlice, ""},
		{"spec.passwords.labldapRuntime", doc.Spec.Passwords.LabLDAPRuntime, ""},
		{"spec.passwords.labldapDM", doc.Spec.Passwords.LabLDAPDM, ""},
		{"spec.passwords.labldapDM(directory.env)", "DS_DM_PASSWORD=" + doc.Spec.Passwords.LabLDAPDM, ""},
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

	writeCatalogDisk(t, root, doc)
	env := filepath.Join(root, "third_party", "go-lab-ldap-mcp", "secrets", "directory.env")
	if err := os.WriteFile(env, []byte("DS_DM_PASSWORD=drifted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mism = checkCatalogFiles(root, doc)
	if len(mism) != 1 || !strings.Contains(mism[0], "directory.env") {
		t.Fatalf("directory.env mismatch = %v", mism)
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

	required := revealed
	t.Run("optional missing empty secret", func(t *testing.T) {
		got := append([]revealedSecret{}, required...)
		got = append(got, revealedSecret{Name: "tacacs-client-ca", Secret: ""})
		if mism := matchConnSecretsToDisk(got, dir, idx); len(mism) != 0 {
			t.Fatalf("missing optional + empty secret: %v", mism)
		}
	})
	t.Run("optional missing nonempty secret", func(t *testing.T) {
		got := append([]revealedSecret{}, required...)
		got = append(got, revealedSecret{Name: "tacacs-client-ca", Secret: "-----BEGIN CERTIFICATE-----\nX\n-----END CERTIFICATE-----"})
		mism := matchConnSecretsToDisk(got, dir, idx)
		if len(mism) != 1 || !strings.Contains(mism[0], "tacacs-client-ca") {
			t.Fatalf("missing optional + revealed secret: %v", mism)
		}
	})
	t.Run("optional present equal", func(t *testing.T) {
		pem := "-----BEGIN CERTIFICATE-----\nCLIENTCA\n-----END CERTIFICATE-----"
		mustWrite("tacacs-client-ca.pem", pem+"\n")
		t.Cleanup(func() { os.Remove(filepath.Join(dir, "tacacs-client-ca.pem")) })
		got := append([]revealedSecret{}, required...)
		got = append(got, revealedSecret{Name: "tacacs-client-ca", Secret: pem})
		if mism := matchConnSecretsToDisk(got, dir, idx); len(mism) != 0 {
			t.Fatalf("optional PEM equal: %v", mism)
		}
	})
	t.Run("optional present unequal", func(t *testing.T) {
		mustWrite("tacacs-client-ca.pem", "-----BEGIN CERTIFICATE-----\nCLIENTCA\n-----END CERTIFICATE-----\n")
		t.Cleanup(func() { os.Remove(filepath.Join(dir, "tacacs-client-ca.pem")) })
		got := append([]revealedSecret{}, required...)
		got = append(got, revealedSecret{Name: "tacacs-client-ca", Secret: "other-pem"})
		mism := matchConnSecretsToDisk(got, dir, idx)
		if len(mism) != 1 || !strings.Contains(mism[0], "tacacs-client-ca") {
			t.Fatalf("optional PEM unequal: %v", mism)
		}
	})
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
		"labmitm-token":         "labmitm-token",
		"labgraph-token":        "labgraph-token",
		"labntp-token":          "labntp-token",
		"labsso-token":          "labsso-token",
		"labsso-alice":          "labsso-alice",
		"labsso-ca.crt":         "labsso-ca.crt",
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
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "LAB_DEV_MODE:") {
			t.Fatalf("ci.yml must not set LAB_DEV_MODE as a YAML env key: %s", strings.TrimSpace(line))
		}
	}
	var wf struct {
		Jobs map[string]struct {
			Env map[string]string `yaml:"env"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatal(err)
	}
	smokeEnv := wf.Jobs["smoke-dev"].Env
	if smokeEnv["PROFILE"] != "ci-dev" {
		t.Fatalf("smoke-dev env PROFILE=%q, want ci-dev", smokeEnv["PROFILE"])
	}
	if _, ok := smokeEnv["LAB_DEV_MODE"]; ok {
		t.Fatal("smoke-dev must not set LAB_DEV_MODE as job env")
	}
	if len(smokeEnv) != 1 {
		t.Fatalf("smoke-dev env = %v, want only PROFILE", smokeEnv)
	}
	if _, ok := wf.Jobs["test"].Env["LAB_DEV_MODE"]; ok {
		t.Fatal("test job must not set LAB_DEV_MODE")
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
		"secrets/labmitm-token":                                               doc.Spec.Tokens.LabMITM + "\n",
		"secrets/labgraph-token":                                              doc.Spec.Tokens.Labgraph + "\n",
		"secrets/labntp-token":                                                doc.Spec.Tokens.LabNTP + "\n",
		"secrets/labsso-token":                                                doc.Spec.Tokens.LabSSO + "\n",
		"secrets/labsso-users/alice.password":                                 doc.Spec.Passwords.LabSSOAlice + "\n",
		"secrets/mcp-client-token":                                            doc.Spec.Tokens.MCPClient + "\n",
		"secrets/maildev-web-password":                                        doc.Spec.Passwords.MaildevWeb + "\n",
		"third_party/go-lab-ldap-mcp/secrets/token-admin":                     doc.Spec.Tokens.LabLDAPAdmin + "\n",
		"third_party/go-lab-ldap-mcp/secrets/user-alice":                      doc.Spec.Passwords.LabLDAPAlice + "\n",
		"third_party/go-lab-ldap-mcp/secrets/runtime-ldap":                    doc.Spec.Passwords.LabLDAPRuntime + "\n",
		"third_party/go-lab-ldap-mcp/secrets/dm.pw":                           doc.Spec.Passwords.LabLDAPDM + "\n",
		"third_party/go-lab-ldap-mcp/secrets/directory.env":                   "DS_DM_PASSWORD=" + doc.Spec.Passwords.LabLDAPDM + "\n",
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
