package lab

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	labssoCAName           = "LabSSO-Lab-CA"
	labssoTLSValid         = 365 * 24 * time.Hour
	labssoLeafDNS          = "labsso"
	labssoTLSRel           = "secrets/labsso-tls"
	labssoTLSReloadPending = "secrets/labsso-tls/.reload-pending"
	labssoSigningRel       = "secrets/labsso-oidc/signing.pem"
	labssoAliceRel         = "secrets/labsso-users/alice.password"
	labssoTokenRel         = "secrets/labsso-token"
)

type labssoTLSResult struct {
	Rewritten bool
}

func (r *Runner) labssoTLSDir() string {
	return r.path(labssoTLSRel)
}

func (r *Runner) labssoTLSReloadPendingPath() string {
	return r.path(labssoTLSReloadPending)
}

func (r *Runner) labssoTLSReloadPending() bool {
	_, err := os.Stat(r.labssoTLSReloadPendingPath())
	return err == nil
}

func (r *Runner) persistLabssoTLSReloadIf(rewritten bool) error {
	if !rewritten {
		return nil
	}
	path := r.labssoTLSReloadPendingPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("pending\n"), 0o644)
}

func (r *Runner) clearLabssoTLSReloadPending() error {
	if err := os.Remove(r.labssoTLSReloadPendingPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (r *Runner) ensureLabssoTLS() (labssoTLSResult, error) {
	host := "localhost"
	if r.Prof != nil {
		host = strings.TrimSpace(r.Prof.Get("LAB_PUBLIC_HOST", "localhost"))
	}
	return labssoTLSEnsure(r.labssoTLSDir(), host)
}

func labssoTLSEnsure(dir, publicHost string) (labssoTLSResult, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return labssoTLSResult{}, err
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return labssoTLSResult{}, err
	}

	dns, ips := requiredLabssoSANs(publicHost)
	caKeyPath := filepath.Join(dir, "ca.key")
	var (
		ca    *x509.Certificate
		caKey *rsa.PrivateKey
		mint  bool
	)
	if _, err := os.Stat(caKeyPath); err != nil {
		if !os.IsNotExist(err) {
			return labssoTLSResult{}, err
		}
		var err error
		ca, caKey, err = mintLabssoCA(dir)
		if err != nil {
			return labssoTLSResult{}, err
		}
		mint = true
	} else {
		var err error
		ca, caKey, err = loadLabssoCA(dir)
		if err != nil {
			return labssoTLSResult{}, err
		}
	}

	leafCert := filepath.Join(dir, "tls.crt")
	leafKey := filepath.Join(dir, "tls.key")
	if mint || leafNeedsRewrite(leafCert, leafKey, dns, ips) {
		if err := signLabssoLeaf(leafCert, leafKey, labssoLeafDNS, ca, caKey, dns, ips); err != nil {
			return labssoTLSResult{}, err
		}
		if mint {
			fmt.Printf("wrote %s\n", leafCert)
			fmt.Printf("wrote %s\n", leafKey)
		} else {
			fmt.Printf("re-signed %s (LabSSO TLS SAN)\n", leafCert)
		}
		return labssoTLSResult{Rewritten: true}, nil
	}
	return labssoTLSResult{}, nil
}

func requiredLabssoSANs(publicHost string) ([]string, []net.IP) {
	return appendPublicHostSAN([]string{labssoLeafDNS, "localhost"}, []net.IP{net.ParseIP("127.0.0.1")}, publicHost)
}

func mintLabssoCA(dir string) (*x509.Certificate, *rsa.PrivateKey, error) {
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
		Subject:               pkix.Name{CommonName: labssoCAName, Organization: []string{"LabSSO"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(labssoTLSValid),
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
	if err := writeLabssoPEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", der, 0o644); err != nil {
		return nil, nil, err
	}
	if err := writeLabssoPEM(filepath.Join(dir, "ca.key"), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(caKey), 0o644); err != nil {
		return nil, nil, err
	}
	fmt.Printf("wrote %s\n", filepath.Join(dir, "ca.crt"))
	fmt.Printf("wrote %s\n", filepath.Join(dir, "ca.key"))
	return cert, caKey, nil
}

func signLabssoLeaf(certPath, keyPath, cn string, ca *x509.Certificate, caKey *rsa.PrivateKey, dns []string, ips []net.IP) error {
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
		NotAfter:     time.Now().Add(labssoTLSValid),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  ipAddrs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writeLabssoPEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	return writeLabssoPEM(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), 0o644)
}

func loadLabssoCA(dir string) (*x509.Certificate, *rsa.PrivateKey, error) {
	cert, err := loadCert(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, nil, fmt.Errorf("labsso tls: load ca.crt: %w", err)
	}
	key, err := loadRSAKey(filepath.Join(dir, "ca.key"))
	if err != nil {
		return nil, nil, fmt.Errorf("labsso tls: load ca.key: %w", err)
	}
	return cert, key, nil
}

func writeLabssoPEM(path, typ string, der []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b := pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
	if err := os.WriteFile(path, b, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func (r *Runner) ensureLabssoSigningKey() error {
	path := r.path(labssoSigningRel)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := writeLabssoPEM(path, "PRIVATE KEY", der, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

func deriveLabssoIssuer(publicHost, httpsPort string) string {
	host := strings.TrimSpace(publicHost)
	if host == "" {
		return ""
	}
	port := strings.TrimSpace(httpsPort)
	if port == "" || port == "443" {
		return "https://" + host
	}
	return "https://" + host + ":" + port
}
