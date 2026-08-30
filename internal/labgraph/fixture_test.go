package labgraph

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePacks(t *testing.T, dir string) {
	t.Helper()
	root := filepath.Join("..", "..", "profiles", "default", "scenarios")
	for _, id := range FixtureIDs {
		b, err := os.ReadFile(filepath.Join(root, id+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".yaml"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "default.yaml"), []byte(emptyYAML), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultProfilePacksNoPrivateKey(t *testing.T) {
	dir := filepath.Join("..", "..", "profiles", "default", "scenarios")
	if err := scanScenarioDirPrivateKeys(dir); err != nil {
		t.Fatal(err)
	}
	for _, id := range FixtureIDs {
		doc, err := LoadScenario(filepath.Join(dir, id+".yaml"))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if doc.Metadata.Name != id {
			t.Fatalf("%s metadata.name = %q", id, doc.Metadata.Name)
		}
	}
}

func TestApplyFixtureRejectsDefault(t *testing.T) {
	s := testService(t, "default.yaml", emptyYAML, nil, nil, nil)
	_, err := s.ApplyFixture(context.Background(), "default", ApplyRequest{})
	if err == nil || !strings.Contains(err.Error(), errNotFixture) {
		t.Fatalf("got %v", err)
	}
}

func TestExpiredCertApplyIsNoopAndMaterialIsExpiredPublic(t *testing.T) {
	dir := t.TempDir()
	writePacks(t, dir)
	dns := &fakeFamily{State: FamilyState{RuntimeRevision: "r"}}
	ldap := &fakeLDAP{}
	s := NewService(dir, Clients{Family: map[string]FamilyClient{"labdns": dns}, LDAP: ldap})
	res, err := s.ApplyFixture(context.Background(), "expired-cert", ApplyRequest{})
	if err != nil || !res.OK {
		t.Fatalf("apply: %v %+v", err, res)
	}
	if len(dns.Calls) != 0 || len(ldap.Calls) != 0 {
		t.Fatalf("expired-cert apply must not call appliances: dns=%v ldap=%v", dns.Calls, ldap.Calls)
	}
	view, err := s.GetFixture(context.Background(), "expired-cert")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(view)
	if ContainsPrivateKey(b) {
		t.Fatal("GetFixture leaked PRIVATE KEY")
	}
	if view.Material == nil || view.Material.Kind != expiredTLSLeafKind {
		t.Fatalf("material %+v", view.Material)
	}
	block, _ := pem.Decode([]byte(view.Material.CertPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("pem type %v", block)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.NotAfter.Before(time.Now()) {
		t.Fatalf("NotAfter %s is not in the past", cert.NotAfter)
	}
}

func TestBrokenBindDisableUserIfMatch(t *testing.T) {
	dir := t.TempDir()
	writePacks(t, dir)
	ldap := &fakeLDAP{UserETag: `"etag-1"`}
	s := NewService(dir, Clients{LDAP: ldap})
	res, err := s.ApplyFixture(context.Background(), "broken-bind", ApplyRequest{})
	if err != nil || !res.OK {
		t.Fatalf("apply: %v %+v", err, res)
	}
	if ldap.LastMatch != `"etag-1"` {
		t.Fatalf("If-Match = %q", ldap.LastMatch)
	}
	if strings.Join(ldap.Calls, ",") != "getUser:alice,disable:alice" {
		t.Fatalf("calls %v", ldap.Calls)
	}
}

func TestBrokenBindFlattenStillFailClosed(t *testing.T) {
	ldap := &fakeLDAP{}
	s := testService(t, "x.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: x
spec:
  labldap:
    users: [{id: bob}]
`, nil, ldap, nil)
	res, err := s.Apply(context.Background(), "x", ApplyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Results[3].Detail, "no native file-level") {
		t.Fatalf("%+v", res)
	}
	if len(ldap.Calls) != 0 {
		t.Fatalf("flatten must not call ldap %v", ldap.Calls)
	}
}

func TestSplitHorizonAndMITMPackEnvelopes(t *testing.T) {
	dir := t.TempDir()
	writePacks(t, dir)
	var dnsBody, mitmBody json.RawMessage
	dns := &fakeFamily{
		State: FamilyState{RuntimeRevision: "dns-r"},
		ApplyFn: func(body json.RawMessage) (json.RawMessage, error) {
			dnsBody = append(json.RawMessage(nil), body...)
			return []byte(`{}`), nil
		},
	}
	mitm := &fakeFamily{
		State: FamilyState{RuntimeRevision: "mitm-r"},
		ApplyFn: func(body json.RawMessage) (json.RawMessage, error) {
			mitmBody = append(json.RawMessage(nil), body...)
			return []byte(`{}`), nil
		},
	}
	s := NewService(dir, Clients{Family: map[string]FamilyClient{"labdns": dns, "labmitm": mitm}})
	res, err := s.ApplyFixture(context.Background(), "split-horizon-dns", ApplyRequest{})
	if err != nil || !res.OK {
		t.Fatalf("dns pack: %v %+v", err, res)
	}
	if !strings.Contains(string(dnsBody), `"split-horizon"`) || !strings.Contains(string(dnsBody), `"overlay"`) || !strings.Contains(string(dnsBody), `"internal-horizon"`) {
		t.Fatalf("dns body %s", dnsBody)
	}
	res, err = s.ApplyFixture(context.Background(), "mitm-intercept-extra-port", ApplyRequest{})
	if err != nil || !res.OK {
		t.Fatalf("mitm pack: %v %+v", err, res)
	}
	var env struct {
		Operations []struct {
			TLS struct {
				Ports []int `json:"ports"`
			} `json:"tls"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(mitmBody, &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Operations) != 1 || len(env.Operations[0].TLS.Ports) != 2 || env.Operations[0].TLS.Ports[0] != 443 || env.Operations[0].TLS.Ports[1] != 9443 {
		t.Fatalf("ports %+v", env.Operations)
	}
}

func TestMCPResourcesAndFixtureApplyTool(t *testing.T) {
	dir := t.TempDir()
	writePacks(t, dir)
	s := NewService(dir, Clients{})
	srv := MCPServer(s)
	if srv == nil {
		t.Fatal("nil server")
	}
	view, err := s.GetFixture(context.Background(), "broken-bind")
	if err != nil {
		t.Fatal(err)
	}
	if view.ID != "broken-bind" || !strings.Contains(string(view.Spec), "disableUser") {
		t.Fatalf("%+v", view)
	}
}

func TestScenarioGetIncludesSpec(t *testing.T) {
	dir := t.TempDir()
	writePacks(t, dir)
	s := NewService(dir, Clients{})
	doc, err := s.Get(context.Background(), "broken-bind")
	if err != nil {
		t.Fatal(err)
	}
	view, err := scenarioView(doc)
	if err != nil {
		t.Fatal(err)
	}
	if view["spec"] == nil {
		t.Fatal("spec missing")
	}
}
