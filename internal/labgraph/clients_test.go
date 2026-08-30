package labgraph

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewHTTPLDAPLoadsCAPool(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "labldap-ca.crt")
	if err := os.WriteFile(caPath, testCAPEM(t), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := NewHTTPLDAP("https://control:8443", "tok", caPath)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.Client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type %T, want *http.Transport", c.Client.Transport)
	}
	cfg := tr.TLSClientConfig
	if cfg == nil {
		t.Fatal("missing TLSClientConfig")
	}
	if cfg.InsecureSkipVerify {
		t.Fatal("must not set InsecureSkipVerify")
	}
	if cfg.RootCAs == nil {
		t.Fatal("RootCAs must be the lab CA pool")
	}
}

func TestNewHTTPLDAPMissingCAFailClosed(t *testing.T) {
	_, err := NewHTTPLDAP("https://control:8443", "tok", filepath.Join(t.TempDir(), "missing.crt"))
	if err == nil {
		t.Fatal("missing CA must fail closed")
	}
}

func testCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "labgraph-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
