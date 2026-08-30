package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/labinfo"
)

func copyDefaultLabinfoCatalog(t *testing.T, r *Runner) {
	t.Helper()
	src := filepath.Join("..", "..", "profiles", "default", "labinfo", "services.yaml")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r.Prof.Dir, "labinfo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "services.yaml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCredsRefusesNonDev(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=false\nLAB_PUBLIC_HOST=lab.example.com\n", validCatalogBytes(t))
	copyDefaultLabinfoCatalog(t, r)
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	_, err := r.credsSheet()
	if err == nil || !strings.Contains(err.Error(), credsErrNonDev) {
		t.Fatalf("want %q, got %v", credsErrNonDev, err)
	}
}

func TestCredsPrintsStagedFilesAndHost(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nLAB_PUBLIC_HOST=lab.example.com\nLABDNS_DNS_PORT=10053\n", validCatalogBytes(t))
	copyDefaultLabinfoCatalog(t, r)
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	sheet, err := r.credsSheet()
	if err != nil {
		t.Fatal(err)
	}
	cat, err := labinfo.Load(filepath.Join(r.Prof.Dir, "labinfo", "services.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range cat.Services {
		if s.Credential != nil {
			name := filepath.Base(s.Credential.File)
			if !seen[name] {
				if !sheetHasCred(sheet, name) {
					t.Errorf("sheet missing web credential %q\n%s", name, sheet)
				}
				seen[name] = true
			}
		}
		if s.Connection == nil {
			continue
		}
		for _, cr := range s.Connection.Credentials {
			if cr.Optional || seen[cr.Name] {
				continue
			}
			if !sheetHasCred(sheet, cr.Name) {
				t.Errorf("sheet missing required connection credential %q\n%s", cr.Name, sheet)
			}
			seen[cr.Name] = true
		}
	}
	for _, want := range []string{
		"LAB_PUBLIC_HOST=lab.example.com",
		"LABDNS_DNS_PORT=10053",
		"PROFILE=default",
		"bind-password-alice=lab-dev-alice-12",
		"labdns-token=lab-dev-labdns-token",
		"labmitm-token=lab-dev-labmitm-token-32b-minimum",
		"labgraph-token=lab-dev-labgraph-token",
		"mcp-client-token=lab-dev-mcp-client-token",
		"maildev-web-password=lab-dev-mail-admin-1",
		"labmail-token=lab-dev-labmail-token-32b-minimum",
		"labldap-token-admin=lab-dev-labldap-token-admin",
		"lab-admin-password=LabAdmin-Dev-Pass-01!",
		"lab-admin-enable=LabEnable-Dev-Pass-01!",
		"lab-readonly-password=LabReadonly-Dev-Pass-01!",
		"lab-ca:",
		"-----BEGIN CERTIFICATE-----",
		"radius-shared-secret=",
		"tacacs-shared-secret=",
		"labtacacs-token-admin=",
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("sheet missing %q\n%s", want, sheet)
		}
	}
	if strings.Contains(sheet, "labinfo-token") {
		t.Error("sheet must not invent a labinfo catalog service/token")
	}
}

func sheetHasCred(sheet, name string) bool {
	return strings.Contains(sheet, name+"=") || strings.Contains(sheet, name+":\n")
}

func TestCredsSkipsOptionalMissingPEMs(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nLAB_PUBLIC_HOST=lab.example.com\n", validCatalogBytes(t))
	copyDefaultLabinfoCatalog(t, r)
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	sheet, err := r.credsSheet()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sheet, "tacacs-client-ca=") || strings.Contains(sheet, "tacacs-client-ca:") {
		t.Error("missing optional client CA should be omitted, not fail")
	}
}

func TestCredsIncludesOptionalPEMsWhenStaged(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nLAB_PUBLIC_HOST=lab.example.com\n", validCatalogBytes(t))
	copyDefaultLabinfoCatalog(t, r)
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	dir := r.path("secrets/labinfo-creds")
	pem := "-----BEGIN CERTIFICATE-----\nOPTIONALCA\n-----END CERTIFICATE-----\n"
	if err := os.WriteFile(filepath.Join(dir, "tacacs-client-ca.pem"), []byte(pem), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tacacs-client-ok.pem"), []byte("-----BEGIN CERTIFICATE-----\nOKCERT\n-----END CERTIFICATE-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sheet, err := r.credsSheet()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sheet, "tacacs-client-ca:") || !strings.Contains(sheet, "OPTIONALCA") {
		t.Errorf("sheet missing staged client CA PEM:\n%s", sheet)
	}
	if !strings.Contains(sheet, "OKCERT") {
		t.Errorf("sheet missing staged client-ok PEM:\n%s", sheet)
	}
}

func TestCredsOmitsPrivateKeys(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nLAB_PUBLIC_HOST=lab.example.com\n", validCatalogBytes(t))
	writeCredsCatalog(t, r, `services:
  - id: labldap
    name: LabLDAP
    urls:
      - name: UI
        url: https://x/
    connection:
      endpoints:
        - name: LDAPS
          protocol: ldaps
          address: ldaps://x:3636
      credentials:
        - name: lab-ca
          file: /run/lab-secrets/labldap-ca.crt
          usage: trust this CA
        - name: lab-ca-key
          file: /run/lab-secrets/ca.key
          usage: must not print
        - name: server-key
          file: /run/lab-secrets/tacacs_server_key.pem
          usage: must not print
`)
	dir := r.path("secrets/labinfo-creds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"labldap-ca.crt":        "-----BEGIN CERTIFICATE-----\nKEEPCA\n-----END CERTIFICATE-----\n",
		"ca.key":                "-----BEGIN PRIVATE KEY-----\nNOPE\n-----END PRIVATE KEY-----\n",
		"tacacs_server_key.pem": "-----BEGIN PRIVATE KEY-----\nNOPE2\n-----END PRIVATE KEY-----\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sheet, err := r.credsSheet()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sheet, "lab-ca:") || !strings.Contains(sheet, "KEEPCA") {
		t.Errorf("sheet missing public CA PEM:\n%s", sheet)
	}
	for _, leak := range []string{"NOPE", "PRIVATE KEY", "lab-ca-key", "server-key"} {
		if strings.Contains(sheet, leak) {
			t.Errorf("private key leaked %q:\n%s", leak, sheet)
		}
	}
}

func writeCredsCatalog(t *testing.T, r *Runner, body string) {
	t.Helper()
	dir := filepath.Join(r.Prof.Dir, "labinfo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "services.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCredsFailsIfRequiredFileMissing(t *testing.T) {
	r := scaffoldSecretsRunner(t, "LAB_DEV_MODE=true\nLAB_PUBLIC_HOST=lab.example.com\n", validCatalogBytes(t))
	copyDefaultLabinfoCatalog(t, r)
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.path("secrets/labinfo-creds/labldap-user-alice")); err != nil {
		t.Fatal(err)
	}
	_, err := r.credsSheet()
	if err == nil || !strings.Contains(err.Error(), "labldap-user-alice") {
		t.Fatalf("want missing required staged file, got %v", err)
	}
}

func TestIsPrivateKeyFile(t *testing.T) {
	for path, want := range map[string]bool{
		"ca.key":                  true,
		"secrets/tls/ca.key":      true,
		"tacacs_server_key.pem":   true,
		"labldap-ca.crt":          false,
		"tacacs-client-ca.pem":    false,
		"tacacs-client-ok.pem":    false,
		"labtacacs-tacacs-secret": false,
	} {
		if got := isPrivateKeyFile(path); got != want {
			t.Errorf("isPrivateKeyFile(%q)=%v, want %v", path, got, want)
		}
	}
}
