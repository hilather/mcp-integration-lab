package lab

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveLabssoIssuer(t *testing.T) {
	cases := []struct {
		host, port, want string
	}{
		{"localhost", "443", "https://localhost"},
		{"localhost", "", "https://localhost"},
		{"lab.example.test", "443", "https://lab.example.test"},
		{"lab.example.test", "8443", "https://lab.example.test:8443"},
		{"", "443", ""},
	}
	for _, tc := range cases {
		if got := deriveLabssoIssuer(tc.host, tc.port); got != tc.want {
			t.Errorf("deriveLabssoIssuer(%q, %q) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestLabssoTLSEnsureModesAndSANs(t *testing.T) {
	dir := t.TempDir()
	res, err := labssoTLSEnsure(dir, "lab.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Rewritten {
		t.Fatal("first mint must rewrite")
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("dir mode = %04o, want 0755", fi.Mode().Perm())
	}
	for _, name := range []string{"ca.crt", "ca.key", "tls.crt", "tls.key"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o644 {
			t.Fatalf("%s mode = %04o, want 0644", name, st.Mode().Perm())
		}
	}
	cert := mustCert(t, filepath.Join(dir, "tls.crt"))
	if !containsDNS(cert.DNSNames, "labsso") || !containsDNS(cert.DNSNames, "localhost") || !containsDNS(cert.DNSNames, "lab.example.test") {
		t.Fatalf("SANs = %v", cert.DNSNames)
	}
	again, err := labssoTLSEnsure(dir, "lab.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if again.Rewritten {
		t.Fatal("matching SANs must not rewrite")
	}
	changed, err := labssoTLSEnsure(dir, "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	if !changed.Rewritten {
		t.Fatal("missing IP SAN must rewrite")
	}
}

func TestLabssoSigningKeyPKCS8(t *testing.T) {
	r := &Runner{Root: t.TempDir()}
	if err := r.ensureLabssoSigningKey(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(r.path(labssoSigningRel))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "PRIVATE KEY" {
		t.Fatalf("want PKCS#8 PRIVATE KEY PEM, got %#v", block)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := key.(*rsa.PrivateKey); !ok {
		t.Fatalf("signing key type %T, want *rsa.PrivateKey", key)
	}
	if err := r.ensureLabssoSigningKey(); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(r.path(labssoSigningRel))
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(b) {
		t.Fatal("mint-if-missing must not rotate a good signing key")
	}
}
