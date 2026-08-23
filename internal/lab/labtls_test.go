package lab

import (
	"bytes"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestLabTLSEnsureFirstMintIncludesPublicHostSAN(t *testing.T) {
	dir := t.TempDir()
	res := mustLabTLSEnsure(t, dir, "lab.example.test")
	if !res.Directory || !res.Management {
		t.Fatalf("first mint must write both leaves: %+v", res)
	}
	ca := mustCert(t, filepath.Join(dir, "ca.crt"))
	if ca.Subject.CommonName != labLDAPCAName {
		t.Fatalf("CA CN = %q, want %s", ca.Subject.CommonName, labLDAPCAName)
	}
	dirCert := mustCert(t, filepath.Join(dir, "directory.crt"))
	mgmt := mustCert(t, filepath.Join(dir, "management.crt"))
	assertLeafChain(t, ca, dirCert)
	assertLeafChain(t, ca, mgmt)
	assertDirectorySANs(t, dirCert, "lab.example.test")
	assertManagementSANs(t, mgmt, "lab.example.test")
	if dirCert.Subject.CommonName != labLDAPDirDNS {
		t.Fatalf("directory CN = %q (must stay %s, not LAB_PUBLIC_HOST)", dirCert.Subject.CommonName, labLDAPDirDNS)
	}
}

func TestLabTLSEnsureHostnameIsDNSAndIPIsIP(t *testing.T) {
	t.Run("hostname", func(t *testing.T) {
		dir := t.TempDir()
		mustLabTLSEnsure(t, dir, "lab.team.example")
		c := mustCert(t, filepath.Join(dir, "directory.crt"))
		assertDirectorySANs(t, c, "lab.team.example")
		if containsIP(c.IPAddresses, net.ParseIP("lab.team.example")) {
			t.Fatal("hostname must not be parsed as IP")
		}
	})
	t.Run("ipv4", func(t *testing.T) {
		dir := t.TempDir()
		mustLabTLSEnsure(t, dir, "203.0.113.10")
		c := mustCert(t, filepath.Join(dir, "directory.crt"))
		assertDirectorySANs(t, c, "203.0.113.10")
		m := mustCert(t, filepath.Join(dir, "management.crt"))
		assertManagementSANs(t, m, "203.0.113.10")
	})
	t.Run("ipv6", func(t *testing.T) {
		dir := t.TempDir()
		mustLabTLSEnsure(t, dir, "2001:db8::10")
		c := mustCert(t, filepath.Join(dir, "directory.crt"))
		assertDirectorySANs(t, c, "2001:db8::10")
		m := mustCert(t, filepath.Join(dir, "management.crt"))
		assertManagementSANs(t, m, "2001:db8::10")
	})
}

func TestLabTLSEnsureResignsLeavesKeepsCA(t *testing.T) {
	dir := t.TempDir()
	mustLabTLSEnsure(t, dir, "")
	caKey := mustRead(t, filepath.Join(dir, "ca.key"))
	caCrt := mustRead(t, filepath.Join(dir, "ca.crt"))
	before := mustCert(t, filepath.Join(dir, "directory.crt"))
	if containsDNS(before.DNSNames, "lab.example.test") {
		t.Fatal("empty public host must not add extra DNS")
	}

	res := mustLabTLSEnsure(t, dir, "lab.example.test")
	if !res.Directory || !res.Management {
		t.Fatalf("hostname SAN missing on setuptls-like leaves must re-sign: %+v", res)
	}
	if !bytes.Equal(caKey, mustRead(t, filepath.Join(dir, "ca.key"))) {
		t.Fatal("re-sign must keep ca.key")
	}
	if !bytes.Equal(caCrt, mustRead(t, filepath.Join(dir, "ca.crt"))) {
		t.Fatal("re-sign must keep ca.crt")
	}
	dirCert := mustCert(t, filepath.Join(dir, "directory.crt"))
	assertDirectorySANs(t, dirCert, "lab.example.test")
	assertLeafChain(t, mustCert(t, filepath.Join(dir, "ca.crt")), dirCert)
	assertManagementSANs(t, mustCert(t, filepath.Join(dir, "management.crt")), "lab.example.test")
}

func TestLabTLSEnsureHostToIPResignsLeavesOnly(t *testing.T) {
	dir := t.TempDir()
	mustLabTLSEnsure(t, dir, "lab.example.test")
	caKey := mustRead(t, filepath.Join(dir, "ca.key"))

	res := mustLabTLSEnsure(t, dir, "203.0.113.10")
	if !res.Directory || !res.Management {
		t.Fatalf("public host change hostname→IP must re-sign: %+v", res)
	}
	if !bytes.Equal(caKey, mustRead(t, filepath.Join(dir, "ca.key"))) {
		t.Fatal("must not rotate ca.key")
	}
	c := mustCert(t, filepath.Join(dir, "directory.crt"))
	assertDirectorySANs(t, c, "203.0.113.10")
	if containsDNS(c.DNSNames, "lab.example.test") {
		t.Fatalf("re-sign must use the required SAN set, got DNS %v", c.DNSNames)
	}
}

func TestLabTLSEnsureIdempotentWhenSANsPresent(t *testing.T) {
	dir := t.TempDir()
	mustLabTLSEnsure(t, dir, "lab.example.test")
	dirPEM := mustRead(t, filepath.Join(dir, "directory.crt"))
	caKey := mustRead(t, filepath.Join(dir, "ca.key"))
	res := mustLabTLSEnsure(t, dir, "lab.example.test")
	if res.leavesRewritten() {
		t.Fatalf("second ensure must not rewrite: %+v", res)
	}
	if !bytes.Equal(dirPEM, mustRead(t, filepath.Join(dir, "directory.crt"))) {
		t.Fatal("directory.crt bytes changed")
	}
	if !bytes.Equal(caKey, mustRead(t, filepath.Join(dir, "ca.key"))) {
		t.Fatal("ca.key changed")
	}
}

func TestLabTLSEnsureIPLiteralInDNSNamesIsNotEnough(t *testing.T) {
	dir := t.TempDir()
	mustLabTLSEnsure(t, dir, "")
	ca, caKey, err := loadLabLDAPCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	ip := "203.0.113.10"
	if err := signLabLDAPLeaf(
		filepath.Join(dir, "directory.crt"),
		filepath.Join(dir, "directory.key"),
		labLDAPDirDNS, ca, caKey,
		[]string{labLDAPDirDNS, "localhost", ip},
		[]net.IP{net.ParseIP("127.0.0.1")},
	); err != nil {
		t.Fatal(err)
	}
	res := mustLabTLSEnsure(t, dir, ip)
	if !res.Directory {
		t.Fatal("IPv4 in DNSNames must not satisfy the IP SAN requirement")
	}
	c := mustCert(t, filepath.Join(dir, "directory.crt"))
	assertDirectorySANs(t, c, ip)
}

func TestLabTLSEnsureMissingManagementOnly(t *testing.T) {
	dir := t.TempDir()
	mustLabTLSEnsure(t, dir, "lab.example.test")
	caKey := mustRead(t, filepath.Join(dir, "ca.key"))
	if err := os.Remove(filepath.Join(dir, "management.crt")); err != nil {
		t.Fatal(err)
	}
	res := mustLabTLSEnsure(t, dir, "lab.example.test")
	if res.Directory {
		t.Fatal("directory leaf was complete")
	}
	if !res.Management {
		t.Fatal("missing management leaf must be minted with existing CA")
	}
	if !bytes.Equal(caKey, mustRead(t, filepath.Join(dir, "ca.key"))) {
		t.Fatal("must not rotate ca.key")
	}
	assertManagementSANs(t, mustCert(t, filepath.Join(dir, "management.crt")), "lab.example.test")
}

func TestLabTLSEnsureDoesNotWriteRepoRootTLS(t *testing.T) {
	root := t.TempDir()
	vendor := filepath.Join(root, "third_party", "go-lab-ldap-mcp", "secrets", "tls")
	mustLabTLSEnsure(t, vendor, "lab.example.test")
	if _, err := os.Stat(filepath.Join(vendor, "ca.crt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "secrets", "tls", "ca.crt")); !os.IsNotExist(err) {
		t.Fatalf("must not invent repo-root secrets/tls: %v", err)
	}
}

func mustLabTLSEnsure(t *testing.T, dir, host string) labTLSResult {
	t.Helper()
	res, err := labtlsEnsure(dir, host)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func mustCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	c, err := loadCert(path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func assertLeafChain(t *testing.T, ca, leaf *x509.Certificate) {
	t.Helper()
	if err := leaf.CheckSignatureFrom(ca); err != nil {
		t.Fatalf("leaf not signed by lab CA: %v", err)
	}
}

func assertDirectorySANs(t *testing.T, cert *x509.Certificate, publicHost string) {
	t.Helper()
	if !containsDNS(cert.DNSNames, labLDAPDirDNS) {
		t.Fatalf("missing directory SAN: %v", cert.DNSNames)
	}
	assertPublicHostSAN(t, cert, publicHost)
}

func assertManagementSANs(t *testing.T, cert *x509.Certificate, publicHost string) {
	t.Helper()
	if !containsDNS(cert.DNSNames, labLDAPMgmtDNS) {
		t.Fatalf("missing control SAN: %v", cert.DNSNames)
	}
	assertPublicHostSAN(t, cert, publicHost)
}

func assertPublicHostSAN(t *testing.T, cert *x509.Certificate, publicHost string) {
	t.Helper()
	if !containsDNS(cert.DNSNames, "localhost") {
		t.Fatalf("missing localhost SAN: %v", cert.DNSNames)
	}
	if !containsIP(cert.IPAddresses, net.ParseIP("127.0.0.1")) {
		t.Fatalf("missing 127.0.0.1: %v", cert.IPAddresses)
	}
	host := publicHost
	if host == "" {
		return
	}
	if ip := net.ParseIP(host); ip != nil {
		if !containsIP(cert.IPAddresses, ip) {
			t.Fatalf("missing IP SAN %s in %v", host, cert.IPAddresses)
		}
		if containsDNS(cert.DNSNames, host) {
			t.Fatalf("IP literal %s must not be a DNS SAN: %v", host, cert.DNSNames)
		}
		return
	}
	if !containsDNS(cert.DNSNames, host) {
		t.Fatalf("missing DNS SAN %s: %v", host, cert.DNSNames)
	}
}
