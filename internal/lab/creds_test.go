package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	for _, want := range []string{
		"LAB_PUBLIC_HOST=lab.example.com",
		"LABDNS_DNS_PORT=10053",
		"PROFILE=default",
		"bind-password-alice=lab-dev-alice-12",
		"labdns-token=lab-dev-labdns-token",
		"mcp-client-token=lab-dev-mcp-client-token",
		"lab-admin-password=fake-lab-admin",
		"lab-admin-enable=fake-lab-admin-enable",
		"lab-readonly-password=fake-lab-readonly",
		"lab-ca:",
		"-----BEGIN CERTIFICATE-----",
		"TESTCA",
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("sheet missing %q\n%s", want, sheet)
		}
	}
	if strings.Contains(sheet, "labinfo-token") {
		t.Error("sheet must not invent a labinfo catalog service/token")
	}
	if strings.Contains(sheet, "ca.key") || strings.Contains(sheet, "BEGIN PRIVATE KEY") {
		t.Error("sheet must not print TLS private keys")
	}
	if !strings.Contains(sheet, "tacacs-shared-secret=") {
		t.Error("sheet missing tacacs-shared-secret")
	}
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
	copyDefaultLabinfoCatalog(t, r)
	if err := r.Secrets(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.path("secrets/labinfo-creds/ca.key"), []byte("-----BEGIN PRIVATE KEY-----\nNOPE\n-----END PRIVATE KEY-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.path("secrets/labinfo-creds/tacacs_server_key.pem"), []byte("-----BEGIN PRIVATE KEY-----\nNOPE2\n-----END PRIVATE KEY-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sheet, err := r.credsSheet()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sheet, "NOPE") || strings.Contains(sheet, "PRIVATE KEY") {
		t.Errorf("private keys leaked:\n%s", sheet)
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
