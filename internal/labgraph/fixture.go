package labgraph

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	errNotFixture     = "not a fixture pack (closed FixtureIDs; default is not a fixture)"
	privateKeyMarker  = "PRIVATE KEY"
	expiredTLSLeafKind = "expired-tls-leaf"
)

// FixtureView is the load payload for labgraph://fixtures/{id} and GET /v1/fixtures/{id}.
type FixtureView struct {
	ID          string           `json:"id"`
	APIVersion  string           `json:"apiVersion"`
	Kind        string           `json:"kind"`
	Description string           `json:"description,omitempty"`
	Spec        json.RawMessage  `json:"spec"`
	Material    *FixtureMaterial `json:"material,omitempty"`
}

// FixtureMaterial is public-only pack material. Never a private key.
type FixtureMaterial struct {
	Kind     string `json:"kind"`
	CertPEM  string `json:"certPem,omitempty"`
	NotAfter string `json:"notAfter,omitempty"`
	Subject  string `json:"subject,omitempty"`
}

func ContainsPrivateKey(b []byte) bool {
	return bytes.Contains(b, []byte(privateKeyMarker))
}

func (s *Service) ApplyFixture(ctx context.Context, id string, req ApplyRequest) (*GraphResult, error) {
	if !IsFixture(id) {
		return nil, fmt.Errorf("%s: %s", id, errNotFixture)
	}
	return s.Apply(ctx, id, req)
}

func (s *Service) GetFixture(ctx context.Context, id string) (*FixtureView, error) {
	if !IsFixture(id) {
		return nil, fmt.Errorf("%s: %s", id, errNotFixture)
	}
	doc, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	spec, err := specJSON(doc)
	if err != nil {
		return nil, err
	}
	view := &FixtureView{
		ID:          doc.Metadata.Name,
		APIVersion:  doc.APIVersion,
		Kind:        doc.Kind,
		Description: doc.Metadata.Description,
		Spec:        spec,
	}
	if id == "expired-cert" {
		mat, err := expiredLeafMaterial()
		if err != nil {
			return nil, err
		}
		view.Material = mat
	}
	out, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	if ContainsPrivateKey(out) {
		return nil, fmt.Errorf("fixture %q: refused payload containing PRIVATE KEY", id)
	}
	return view, nil
}

func specJSON(doc *LabScenario) (json.RawMessage, error) {
	out := map[string]any{}
	for _, id := range ApplyOrder {
		n := doc.Spec.node(id)
		if !sectionPresent(n) {
			continue
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func scenarioView(doc *LabScenario) (map[string]any, error) {
	spec, err := specJSON(doc)
	if err != nil {
		return nil, err
	}
	var specObj any
	if err := json.Unmarshal(spec, &specObj); err != nil {
		return nil, err
	}
	return map[string]any{
		"name":        doc.Metadata.Name,
		"kind":        doc.Kind,
		"apiVersion":  doc.APIVersion,
		"description": doc.Metadata.Description,
		"spec":        specObj,
	}, nil
}

func expiredLeafMaterial() (*FixtureMaterial, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "labgraph-expired-cert"},
		NotBefore:    now.Add(-2 * time.Hour),
		NotAfter:     now.Add(-1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	key = nil
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if ContainsPrivateKey(pemBytes) {
		return nil, fmt.Errorf("expired leaf PEM contained PRIVATE KEY")
	}
	return &FixtureMaterial{
		Kind:     expiredTLSLeafKind,
		CertPEM:  string(pemBytes),
		NotAfter: tmpl.NotAfter.Format(time.RFC3339),
		Subject:  tmpl.Subject.CommonName,
	}, nil
}

func scanScenarioDirPrivateKeys(dir string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		if ContainsPrivateKey(b) {
			return fmt.Errorf("%s: contains PRIVATE KEY", e.Name())
		}
	}
	return nil
}
