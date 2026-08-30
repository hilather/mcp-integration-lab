package labgraph

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testService(t *testing.T, yamlName, body string, fam map[string]*fakeFamily, ldap *fakeLDAP, tac *fakeTac) *Service {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, yamlName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Clients{Family: map[string]FamilyClient{}}
	for k, v := range fam {
		c.Family[k] = v
	}
	if ldap != nil {
		c.LDAP = ldap
	}
	if tac != nil {
		c.TacLab = tac
	}
	s := NewService(dir, c)
	s.LDAPResetWait = 2 * time.Second
	return s
}

const emptyYAML = `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: default
spec: {}
`

func TestEmptyScenarioCallsZeroClients(t *testing.T) {
	dns := &fakeFamily{State: FamilyState{RuntimeRevision: "r1"}}
	s := testService(t, "default.yaml", emptyYAML, map[string]*fakeFamily{"labdns": dns}, nil, nil)
	ctx := context.Background()
	for _, fn := range []func() (*GraphResult, error){
		func() (*GraphResult, error) { return s.Validate(ctx, "default") },
		func() (*GraphResult, error) { return s.Plan(ctx, "default", ApplyRequest{}) },
		func() (*GraphResult, error) { return s.Apply(ctx, "default", ApplyRequest{}) },
	} {
		res, err := fn()
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK {
			t.Fatalf("empty spec should succeed: %+v", res)
		}
	}
	if len(dns.Calls) != 0 {
		t.Fatalf("empty apply/plan/validate called family: %+v", dns.Calls)
	}
}

func TestValidateOperationsNoRevision(t *testing.T) {
	var got json.RawMessage
	dns := &fakeFamily{
		State: FamilyState{RuntimeRevision: "rev-live"},
		ValidateFn: func(body json.RawMessage) (json.RawMessage, error) {
			got = append(json.RawMessage(nil), body...)
			return []byte(`{}`), nil
		},
	}
	s := testService(t, "ops.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: ops
spec:
  labdns:
    operations:
      - op: add
        target: {kind: record, id: a}
`, map[string]*fakeFamily{"labdns": dns}, nil, nil)
	res, err := s.Validate(context.Background(), "ops")
	if err != nil || !res.OK {
		t.Fatalf("validate: %v %+v", err, res)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["expectedRevision"]; ok {
		t.Fatalf("validate must not invent expectedRevision: %s", got)
	}
	if _, ok := m["operations"]; !ok {
		t.Fatalf("validate body = %s", got)
	}
	for _, c := range dns.Calls {
		if c.Op == "plan" || c.Op == "getState" {
			t.Fatalf("validate must not call %s", c.Op)
		}
	}
}

func TestValidateDocumentWrapsState(t *testing.T) {
	var got json.RawMessage
	dns := &fakeFamily{
		ValidateFn: func(body json.RawMessage) (json.RawMessage, error) {
			got = append(json.RawMessage(nil), body...)
			return []byte(`{}`), nil
		},
	}
	s := testService(t, "doc.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: doc
spec:
  labdns:
    apiVersion: labdns.dev/v1alpha1
    kind: LabDNS
    metadata: {name: x}
    spec: {}
`, map[string]*fakeFamily{"labdns": dns}, nil, nil)
	if _, err := s.Validate(context.Background(), "doc"); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	st, ok := m["state"].(map[string]any)
	if !ok || st["kind"] != "LabDNS" {
		t.Fatalf("want {state: document}, got %s", got)
	}
	if _, rootKind := m["kind"]; rootKind {
		t.Fatalf("document must not be posted at root: %s", got)
	}
}

func TestApplyFetchesExpectedRevisionKey(t *testing.T) {
	var got json.RawMessage
	dns := &fakeFamily{
		State: FamilyState{RuntimeRevision: "live-rev"},
		ApplyFn: func(body json.RawMessage) (json.RawMessage, error) {
			got = append(json.RawMessage(nil), body...)
			return []byte(`{}`), nil
		},
	}
	s := testService(t, "ops.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: ops
spec:
  labdns:
    operations:
      - op: add
`, map[string]*fakeFamily{"labdns": dns}, nil, nil)
	res, err := s.Apply(context.Background(), "ops", ApplyRequest{})
	if err != nil || !res.OK {
		t.Fatalf("apply: %v %+v", err, res)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	if m["expectedRevision"] != "live-rev" {
		t.Fatalf("expectedRevision = %v, want live-rev from GET /v1/state; body %s", m["expectedRevision"], got)
	}
	if _, ok := m["runtimeRevision"]; ok {
		t.Fatalf("must not send runtimeRevision key: %s", got)
	}
	ops := []string{}
	for _, c := range dns.Calls {
		ops = append(ops, c.Op)
	}
	if strings.Join(ops, ",") != "getState,apply" {
		t.Fatalf("call order %v", ops)
	}
}

func TestApplyDocumentFailClosed(t *testing.T) {
	dns := &fakeFamily{State: FamilyState{RuntimeRevision: "r"}}
	s := testService(t, "doc.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: doc
spec:
  labdns:
    apiVersion: labdns.dev/v1alpha1
    kind: LabDNS
    spec: {}
`, map[string]*fakeFamily{"labdns": dns}, nil, nil)
	res, err := s.Apply(context.Background(), "doc", ApplyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Stopped != "labdns" {
		t.Fatalf("%+v", res)
	}
	for _, c := range dns.Calls {
		if c.Op == "apply" {
			t.Fatal("document apply must not call :apply")
		}
	}
}

func TestLDAPAndTaclabApplyFailClosed(t *testing.T) {
	ldap := &fakeLDAP{}
	tac := &fakeTac{}
	s := testService(t, "x.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: x
spec:
  labldap:
    users: [{id: bob}]
`, map[string]*fakeFamily{}, ldap, tac)
	res, err := s.Apply(context.Background(), "x", ApplyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Results[3].Detail, "no native file-level") {
		t.Fatalf("%+v", res)
	}
	if len(ldap.Calls) != 0 {
		t.Fatalf("ldap apply called %v", ldap.Calls)
	}

	s = testService(t, "y.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: y
spec:
  labtacacs:
    users: [{id: bob}]
`, map[string]*fakeFamily{}, ldap, tac)
	res, err = s.Apply(context.Background(), "y", ApplyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Stopped != "labtacacs" {
		t.Fatalf("%+v", res)
	}
	if len(tac.Calls) != 0 {
		t.Fatal("tac apply must not call runtime reset")
	}
}

func TestPartialApplyStops(t *testing.T) {
	dns := &fakeFamily{State: FamilyState{RuntimeRevision: "a"}}
	mitm := &fakeFamily{
		State: FamilyState{RuntimeRevision: "b"},
		ApplyFn: func(json.RawMessage) (json.RawMessage, error) {
			return nil, errBoom
		},
	}
	mail := &fakeFamily{State: FamilyState{RuntimeRevision: "c"}}
	s := testService(t, "p.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: p
spec:
  labdns:
    operations: [{op: add}]
  labmitm:
    operations: [{op: setFeature}]
  maildev:
    operations: [{op: replaceStoreCaps}]
`, map[string]*fakeFamily{"labdns": dns, "labmitm": mitm, "maildev": mail}, nil, nil)
	res, err := s.Apply(context.Background(), "p", ApplyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Stopped != "labmitm" {
		t.Fatalf("%+v", res)
	}
	if len(mail.Calls) != 0 {
		t.Fatalf("mail must not be called after mitm fail: %v", mail.Calls)
	}
	var applied, failed bool
	for _, r := range res.Results {
		if r.Appliance == "labdns" && r.Status == "applied" {
			applied = true
		}
		if r.Appliance == "labmitm" && r.Status == "failed" {
			failed = true
		}
		if r.Appliance == "maildev" && r.Status == "applied" {
			t.Fatal("maildev applied after stop")
		}
	}
	if !applied || !failed {
		t.Fatalf("%+v", res.Results)
	}
}

var errBoom = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }

func TestApplyOrder(t *testing.T) {
	want := []string{"labdns", "labmitm", "maildev", "labldap", "labtacacs"}
	if strings.Join(ApplyOrder, ",") != strings.Join(want, ",") {
		t.Fatalf("%v", ApplyOrder)
	}
}

func TestResetLDAPUsesCompiledRevision(t *testing.T) {
	ldap := &fakeLDAP{
		Baseline: LDAPBaseline{
			ExpectedRevision: "compiled",
			AppliedRevision:  "stale",
			Match:            false,
		},
		PostCode: 202,
		GetSeq: []LDAPResetStatus{
			{State: LDAPResetting},
			{State: LDAPReady},
		},
	}
	s := testService(t, "default.yaml", emptyYAML, nil, ldap, &fakeTac{})
	res, err := s.Reset(context.Background(), "default", ResetRequest{Appliances: []string{"labldap"}})
	if err != nil || !res.OK {
		t.Fatalf("reset: %v %+v", err, res)
	}
	found := false
	for _, c := range ldap.Calls {
		if c == "post:mcp-integration-lab:compiled" {
			found = true
		}
		if strings.Contains(c, "stale") {
			t.Fatalf("must not send appliedRevision: %v", ldap.Calls)
		}
	}
	if !found {
		t.Fatalf("calls %v", ldap.Calls)
	}
}

func TestResetLDAPIdleReadyIsNotSuccess(t *testing.T) {
	ldap := &fakeLDAP{
		Baseline: LDAPBaseline{ExpectedRevision: "compiled"},
		PostCode: 202,
		GetState: LDAPResetStatus{State: LDAPReady},
	}
	s := testService(t, "default.yaml", emptyYAML, nil, ldap, &fakeTac{})
	s.LDAPResetWait = 200 * time.Millisecond
	res, err := s.Reset(context.Background(), "default", ResetRequest{Appliances: []string{"labldap"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("idle Ready without PreparingReset/Resetting/Verifying must not count as this reset")
	}
}

func TestStatusIncludesTacLab(t *testing.T) {
	s := testService(t, "default.yaml", emptyYAML, nil, nil, &fakeTac{})
	st, err := s.Status(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range st.Appliances {
		if a.Appliance == "labtacacs" && a.RuntimeRevision == "ok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("status missing TacLab: %+v", st.Appliances)
	}
}

func TestResetStopsOnFailure(t *testing.T) {
	dns := &fakeFamily{
		ResetFn: func(string) (json.RawMessage, error) { return nil, errBoom },
	}
	mitm := &fakeFamily{}
	s := testService(t, "default.yaml", emptyYAML, map[string]*fakeFamily{"labdns": dns, "labmitm": mitm}, nil, nil)
	res, err := s.Reset(context.Background(), "default", ResetRequest{Appliances: []string{"labdns", "labmitm"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Stopped != "labdns" {
		t.Fatalf("%+v", res)
	}
	if len(mitm.Calls) != 0 {
		t.Fatal("mitm reset after dns fail")
	}
}

func TestGenerationConflict(t *testing.T) {
	s := testService(t, "default.yaml", emptyYAML, nil, nil, nil)
	gen := int64(5)
	_, err := s.Apply(context.Background(), "default", ApplyRequest{Generation: &gen})
	if err == nil || !strings.Contains(err.Error(), "generation") {
		t.Fatalf("want generation conflict, got %v", err)
	}
}

func TestMITMDocumentValidateFailClosed(t *testing.T) {
	mitm := &fakeFamily{}
	s := testService(t, "m.yaml", `
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: m
spec:
  labmitm:
    apiVersion: labmitm.dev/v1alpha1
    kind: LabMITM
    spec: {}
`, map[string]*fakeFamily{"labmitm": mitm}, nil, nil)
	res, err := s.Validate(context.Background(), "m")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("labmitm document validate must fail closed")
	}
	for _, c := range mitm.Calls {
		if c.Op == "validate" {
			t.Fatal("must not POST {state:} to LabMITM")
		}
	}
}
