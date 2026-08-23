package lab

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	labLDAPCAName   = "LabLDAP-Lab-CA"
	labLDAPTLSValid = 365 * 24 * time.Hour
	labLDAPDirDNS   = "directory"
	labLDAPMgmtDNS  = "control"
)

// labTLSResult reports which leaves labtlsEnsure wrote or re-signed.
type labTLSResult struct {
	Directory  bool
	Management bool
}

func (r labTLSResult) leavesRewritten() bool {
	return r.Directory || r.Management
}

// labtlsEnsure is the single LabLDAP TLS path in both modes. It writes only
// dir (the vendored checkout's secrets/tls): ca.crt/key, directory.crt/key,
// management.crt/key. First mint and re-sign use the same SAN set, including
// publicHost. An IPv4/IPv6 literal must be an IP SAN — a DNSName of that
// literal does not make ldaps://203.0.113.10 verify. Existing ca.key is
// kept; leaves are re-signed when a required SAN is missing.
func labtlsEnsure(dir, publicHost string) (labTLSResult, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return labTLSResult{}, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return labTLSResult{}, err
	}

	dirDNS, dirIPs := requiredDirectorySANs(publicHost)
	mgmtDNS, mgmtIPs := requiredManagementSANs(publicHost)

	caKeyPath := filepath.Join(dir, "ca.key")
	var (
		ca    *x509.Certificate
		caKey *rsa.PrivateKey
		mint  bool
	)
	if _, err := os.Stat(caKeyPath); err != nil {
		if !os.IsNotExist(err) {
			return labTLSResult{}, err
		}
		var err error
		ca, caKey, err = mintLabLDAPCA(dir)
		if err != nil {
			return labTLSResult{}, err
		}
		mint = true
	} else {
		var err error
		ca, caKey, err = loadLabLDAPCA(dir)
		if err != nil {
			return labTLSResult{}, err
		}
	}

	var out labTLSResult
	dirCert := filepath.Join(dir, "directory.crt")
	dirKey := filepath.Join(dir, "directory.key")
	if mint || leafNeedsRewrite(dirCert, dirKey, dirDNS, dirIPs) {
		if err := signLabLDAPLeaf(dirCert, dirKey, labLDAPDirDNS, ca, caKey, dirDNS, dirIPs); err != nil {
			return labTLSResult{}, err
		}
		out.Directory = true
		printLabTLSWrite(mint, dirCert)
	}

	mgmtCert := filepath.Join(dir, "management.crt")
	mgmtKey := filepath.Join(dir, "management.key")
	if mint || leafNeedsRewrite(mgmtCert, mgmtKey, mgmtDNS, mgmtIPs) {
		if err := signLabLDAPLeaf(mgmtCert, mgmtKey, labLDAPMgmtDNS, ca, caKey, mgmtDNS, mgmtIPs); err != nil {
			return labTLSResult{}, err
		}
		out.Management = true
		printLabTLSWrite(mint, mgmtCert)
	}
	return out, nil
}

func printLabTLSWrite(mint bool, certPath string) {
	if mint {
		fmt.Printf("wrote %s\n", certPath)
		fmt.Printf("wrote %s\n", strings.TrimSuffix(certPath, ".crt")+".key")
		return
	}
	fmt.Printf("re-signed %s (LabLDAP TLS SAN)\n", certPath)
}

func requiredDirectorySANs(publicHost string) ([]string, []net.IP) {
	return appendPublicHostSAN([]string{labLDAPDirDNS, "localhost"}, []net.IP{net.ParseIP("127.0.0.1")}, publicHost)
}

func requiredManagementSANs(publicHost string) ([]string, []net.IP) {
	return appendPublicHostSAN([]string{labLDAPMgmtDNS, "localhost"}, []net.IP{net.ParseIP("127.0.0.1")}, publicHost)
}

func appendPublicHostSAN(dns []string, ips []net.IP, publicHost string) ([]string, []net.IP) {
	host := strings.TrimSpace(publicHost)
	if host == "" {
		return dns, ips
	}
	if ip := net.ParseIP(host); ip != nil {
		if !containsIP(ips, ip) {
			ips = append(ips, ip)
		}
		return dns, ips
	}
	if !containsDNS(dns, host) {
		dns = append(dns, host)
	}
	return dns, ips
}

func leafNeedsRewrite(certPath, keyPath string, dns []string, ips []net.IP) bool {
	if _, err := os.Stat(keyPath); err != nil {
		return true
	}
	cert, err := loadCert(certPath)
	if err != nil {
		return true
	}
	return !certHasRequiredSANs(cert, dns, ips)
}

func certHasRequiredSANs(cert *x509.Certificate, dns []string, ips []net.IP) bool {
	for _, n := range dns {
		if !containsDNS(cert.DNSNames, n) {
			return false
		}
	}
	for _, ip := range ips {
		if !containsIP(cert.IPAddresses, ip) {
			return false
		}
	}
	return true
}

func containsDNS(names []string, want string) bool {
	for _, n := range names {
		if strings.EqualFold(n, want) {
			return true
		}
	}
	return false
}

func containsIP(ips []net.IP, want net.IP) bool {
	if want == nil {
		return false
	}
	for _, ip := range ips {
		if ip != nil && ip.Equal(want) {
			return true
		}
	}
	return false
}

func mintLabLDAPCA(dir string) (*x509.Certificate, *rsa.PrivateKey, error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: labLDAPCAName, Organization: []string{"LabLDAP"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(labLDAPTLSValid),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	if err := writePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", der, 0o644); err != nil {
		return nil, nil, err
	}
	if err := writePEM(filepath.Join(dir, "ca.key"), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(caKey), 0o600); err != nil {
		return nil, nil, err
	}
	fmt.Printf("wrote %s\n", filepath.Join(dir, "ca.crt"))
	fmt.Printf("wrote %s\n", filepath.Join(dir, "ca.key"))
	return cert, caKey, nil
}

func signLabLDAPLeaf(certPath, keyPath, cn string, ca *x509.Certificate, caKey *rsa.PrivateKey, dns []string, ips []net.IP) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	var ipAddrs []net.IP
	for _, ip := range ips {
		if ip != nil {
			ipAddrs = append(ipAddrs, ip)
		}
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(labLDAPTLSValid),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  ipAddrs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	return writePEM(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), 0o600)
}

func loadLabLDAPCA(dir string) (*x509.Certificate, *rsa.PrivateKey, error) {
	cert, err := loadCert(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, nil, fmt.Errorf("labtls: load ca.crt: %w", err)
	}
	key, err := loadRSAKey(filepath.Join(dir, "ca.key"))
	if err != nil {
		return nil, nil, fmt.Errorf("labtls: load ca.key: %w", err)
	}
	return cert, key, nil
}

func loadCert(path string) (*x509.Certificate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("no PEM in %s", path)
	}
	return x509.ParseCertificate(block.Bytes)
}

func loadRSAKey(path string) (*rsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("no PEM in %s", path)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	rsaKey, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an RSA private key", path)
	}
	return rsaKey, nil
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b := pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
	if err := os.WriteFile(path, b, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
}
