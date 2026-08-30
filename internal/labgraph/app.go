package labgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	errNoLDAPApply   = "labldap has no native file-level plan/apply; omit the section or use scenario.reset"
	errNoTacApply    = "labtacacs has no native plan/apply; omit the section or use scenario.reset"
	errDocApply      = "desired-state documents are validate-only; apply requires native operations[]"
	errUnknownSect   = "section is not a native operations payload or desired-state document"
	errMITMDocument  = "labmitm desired-state documents are not applied; use operations[] (ValidateIn state field is not pinned on LabMITM v1.5.0)"
	defaultLDAPName  = "mcp-integration-lab"
	ldapResetTimeout = 2 * time.Minute
	ldapResetPoll    = 500 * time.Millisecond
)

type ApplianceResult struct {
	Appliance string          `json:"appliance"`
	Status    string          `json:"status"` // skipped, validated, planned, applied, failed, reset
	Detail    string          `json:"detail,omitempty"`
	Native    json.RawMessage `json:"native,omitempty"`
}

type GraphResult struct {
	Name     string            `json:"name"`
	Order    []string          `json:"order"`
	Results  []ApplianceResult `json:"results"`
	Stopped  string            `json:"stopped,omitempty"`
	OK       bool              `json:"ok"`
	Generation int64           `json:"generation,omitempty"`
}

type ApplyRequest struct {
	ExpectedRevision map[string]string `json:"expectedRevision"`
	Generation       *int64            `json:"generation"`
	Reason           string            `json:"reason"`
}

type ResetRequest struct {
	Appliances []string `json:"appliances"`
	Reason     string   `json:"reason"`
}

type ScenarioStatus struct {
	Name           string            `json:"name"`
	Generation     int64             `json:"generation"`
	LastApplied    string            `json:"lastApplied,omitempty"`
	LastOp         string            `json:"lastOp,omitempty"`
	Appliances     []ApplianceStatus `json:"appliances"`
}

type ApplianceStatus struct {
	Appliance       string `json:"appliance"`
	LastStatus      string `json:"lastStatus,omitempty"`
	Stuck           bool   `json:"stuck"`
	RuntimeRevision string `json:"runtimeRevision,omitempty"`
	Drifted         bool   `json:"drifted,omitempty"`
	Detail          string `json:"detail,omitempty"`
}

type journal struct {
	Name       string
	Generation int64
	LastOp     string
	LastOK     bool
	ByApp      map[string]ApplianceResult
}

type Clients struct {
	Family map[string]FamilyClient // labdns, labmitm, maildev
	LDAP   LDAPClient
	TacLab TacLabClient
}

type Service struct {
	Dir            string
	LDAPName       string
	Clients        Clients
	LDAPResetWait  time.Duration
	mu             sync.Mutex
	j              journal
}

func NewService(dir string, c Clients) *Service {
	return &Service{
		Dir:           dir,
		LDAPName:      defaultLDAPName,
		Clients:       c,
		LDAPResetWait: ldapResetTimeout,
		j:             journal{ByApp: map[string]ApplianceResult{}},
	}
}

func (s *Service) List(_ context.Context) ([]string, error) {
	docs, err := LoadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(docs))
	for _, d := range docs {
		names = append(names, d.Metadata.Name)
	}
	return names, nil
}

func (s *Service) Get(_ context.Context, name string) (*LabScenario, error) {
	docs, err := LoadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	doc := FindByName(docs, name)
	if doc == nil {
		return nil, fmt.Errorf("scenario %q not found", name)
	}
	return doc, nil
}

func (s *Service) Validate(ctx context.Context, name string) (*GraphResult, error) {
	doc, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return s.walk(ctx, doc, walkValidate, ApplyRequest{})
}

func (s *Service) Plan(ctx context.Context, name string, req ApplyRequest) (*GraphResult, error) {
	doc, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return s.walk(ctx, doc, walkPlan, req)
}

func (s *Service) Apply(ctx context.Context, name string, req ApplyRequest) (*GraphResult, error) {
	doc, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if req.Generation != nil {
		s.mu.Lock()
		cur := s.j.Generation
		s.mu.Unlock()
		if *req.Generation != cur {
			return nil, fmt.Errorf("generation conflict: have %d, want %d", cur, *req.Generation)
		}
	}
	res, err := s.walk(ctx, doc, walkApply, req)
	if err != nil {
		return nil, err
	}
	s.record("apply", name, res)
	if res.OK {
		s.mu.Lock()
		s.j.Generation++
		res.Generation = s.j.Generation
		s.mu.Unlock()
	}
	return res, nil
}

func (s *Service) Reset(ctx context.Context, name string, req ResetRequest) (*GraphResult, error) {
	if _, err := s.Get(ctx, name); err != nil {
		return nil, err
	}
	targets := req.Appliances
	if len(targets) == 0 {
		targets = append([]string{}, ApplyOrder...)
	}
	out := &GraphResult{Name: name, Order: targets, OK: true}
	reason := req.Reason
	if reason == "" {
		reason = "labgraph scenario.reset"
	}
	for _, id := range targets {
		ar, stop := s.resetOne(ctx, id, reason)
		out.Results = append(out.Results, ar)
		if stop {
			out.OK = false
			out.Stopped = id
			break
		}
	}
	s.record("reset", name, out)
	return out, nil
}

func (s *Service) Status(ctx context.Context, name string) (*ScenarioStatus, error) {
	if _, err := s.Get(ctx, name); err != nil {
		return nil, err
	}
	s.mu.Lock()
	j := s.j
	s.mu.Unlock()
	st := &ScenarioStatus{Name: name, Generation: j.Generation, LastOp: j.LastOp}
	if j.LastOK && j.Name == name {
		st.LastApplied = j.Name
	}
	for _, id := range ApplyOrder {
		as := ApplianceStatus{Appliance: id}
		if last, ok := j.ByApp[id]; ok {
			as.LastStatus = last.Status
			as.Stuck = last.Status == "failed"
			as.Detail = last.Detail
		}
		switch id {
		case "labdns", "labmitm", "maildev":
			if c := s.Clients.Family[id]; c != nil {
				if fs, err := c.GetState(ctx); err == nil {
					as.RuntimeRevision = fs.RuntimeRevision
					as.Drifted = fs.Drifted
				} else {
					as.Detail = err.Error()
				}
			}
		case "labldap":
			if s.Clients.LDAP != nil {
				if b, err := s.Clients.LDAP.GetBaseline(ctx); err == nil {
					as.RuntimeRevision = b.AppliedRevision
					as.Drifted = !b.Match
				} else {
					as.Detail = err.Error()
				}
			}
		case "labtacacs":
			if s.Clients.TacLab != nil {
				if raw, err := s.Clients.TacLab.Status(ctx); err == nil {
					as.RuntimeRevision = "ok"
					if as.Detail == "" {
						as.Detail = strings.TrimSpace(string(raw))
					}
				} else {
					as.Detail = err.Error()
				}
			}
		}
		st.Appliances = append(st.Appliances, as)
	}
	return st, nil
}

type walkMode int

const (
	walkValidate walkMode = iota
	walkPlan
	walkApply
)

func (s *Service) walk(ctx context.Context, doc *LabScenario, mode walkMode, req ApplyRequest) (*GraphResult, error) {
	out := &GraphResult{Name: doc.Metadata.Name, Order: append([]string{}, ApplyOrder...), OK: true}
	for _, id := range ApplyOrder {
		n := doc.Spec.node(id)
		ar := ApplianceResult{Appliance: id, Status: "skipped"}
		if !sectionPresent(n) || !sectionNonEmpty(n) {
			out.Results = append(out.Results, ar)
			continue
		}
		if id == "labldap" {
			kind, m, err := classifySection(n)
			if err != nil || kind != sectionOperations {
				ar.Status = "failed"
				if err != nil {
					ar.Detail = err.Error()
				} else {
					ar.Detail = errNoLDAPApply
				}
				out.Results = append(out.Results, ar)
				out.OK = false
				out.Stopped = id
				return out, nil
			}
			ar = s.walkLDAP(ctx, m, mode)
			out.Results = append(out.Results, ar)
			if ar.Status == "failed" {
				out.OK = false
				out.Stopped = id
				return out, nil
			}
			continue
		}
		if id == "labtacacs" {
			ar.Status = "failed"
			ar.Detail = errNoTacApply
			out.Results = append(out.Results, ar)
			out.OK = false
			out.Stopped = id
			return out, nil
		}
		kind, m, err := classifySection(n)
		if err != nil {
			ar.Status = "failed"
			ar.Detail = err.Error()
			out.Results = append(out.Results, ar)
			out.OK = false
			out.Stopped = id
			return out, nil
		}
		if kind == sectionUnknown {
			ar.Status = "failed"
			ar.Detail = errUnknownSect
			out.Results = append(out.Results, ar)
			out.OK = false
			out.Stopped = id
			return out, nil
		}
		fc := s.Clients.Family[id]
		if fc == nil {
			ar.Status = "failed"
			ar.Detail = "no client configured"
			out.Results = append(out.Results, ar)
			out.OK = false
			out.Stopped = id
			return out, nil
		}
		switch mode {
		case walkValidate:
			if id == "labmitm" && kind == sectionDocument {
				ar.Status = "failed"
				ar.Detail = errMITMDocument
				out.OK = false
				out.Stopped = id
				out.Results = append(out.Results, ar)
				return out, nil
			}
			body, err := validateEnvelope(kind, m)
			if err != nil {
				ar.Status = "failed"
				ar.Detail = err.Error()
				out.OK = false
				out.Stopped = id
				out.Results = append(out.Results, ar)
				return out, nil
			}
			native, err := fc.Validate(ctx, body)
			if err != nil {
				ar.Status = "failed"
				ar.Detail = err.Error()
				out.OK = false
				out.Stopped = id
				out.Results = append(out.Results, ar)
				return out, nil
			}
			ar.Status = "validated"
			ar.Native = native
		case walkPlan, walkApply:
			if kind == sectionDocument {
				if mode == walkPlan {
					ar.Status = "skipped"
					ar.Detail = "document-shaped section is validate-only"
					out.Results = append(out.Results, ar)
					continue
				}
				ar.Status = "failed"
				ar.Detail = errDocApply
				out.OK = false
				out.Stopped = id
				out.Results = append(out.Results, ar)
				return out, nil
			}
			rev := ""
			if req.ExpectedRevision != nil {
				rev = req.ExpectedRevision[id]
			}
			if rev == "" {
				rev = revisionFromSection(m)
			}
			if rev == "" {
				st, err := fc.GetState(ctx)
				if err != nil {
					ar.Status = "failed"
					ar.Detail = err.Error()
					out.OK = false
					out.Stopped = id
					out.Results = append(out.Results, ar)
					return out, nil
				}
				rev = st.RuntimeRevision
			}
			body, err := changeEnvelope(m, rev)
			if err != nil {
				ar.Status = "failed"
				ar.Detail = err.Error()
				out.OK = false
				out.Stopped = id
				out.Results = append(out.Results, ar)
				return out, nil
			}
			var native json.RawMessage
			if mode == walkPlan {
				native, err = fc.Plan(ctx, body)
				ar.Status = "planned"
			} else {
				native, err = fc.Apply(ctx, body)
				ar.Status = "applied"
			}
			if err != nil {
				ar.Status = "failed"
				ar.Detail = err.Error()
				ar.Native = native
				out.OK = false
				out.Stopped = id
				out.Results = append(out.Results, ar)
				return out, nil
			}
			ar.Native = native
		}
		out.Results = append(out.Results, ar)
	}
	return out, nil
}

func (s *Service) resetOne(ctx context.Context, id, reason string) (ApplianceResult, bool) {
	ar := ApplianceResult{Appliance: id, Status: "reset"}
	switch id {
	case "labdns", "labmitm", "maildev":
		fc := s.Clients.Family[id]
		if fc == nil {
			ar.Status = "failed"
			ar.Detail = "no client configured"
			return ar, true
		}
		native, err := fc.Reset(ctx, reason)
		if err != nil {
			ar.Status = "failed"
			ar.Detail = err.Error()
			return ar, true
		}
		ar.Native = native
		return ar, false
	case "labldap":
		if s.Clients.LDAP == nil {
			ar.Status = "failed"
			ar.Detail = "no client configured"
			return ar, true
		}
		base, err := s.Clients.LDAP.GetBaseline(ctx)
		if err != nil {
			ar.Status = "failed"
			ar.Detail = err.Error()
			return ar, true
		}
		name := s.LDAPName
		if name == "" {
			name = defaultLDAPName
		}
		code, st, err := s.Clients.LDAP.PostReset(ctx, name, base.ExpectedRevision)
		if err != nil && code != 202 {
			ar.Status = "failed"
			ar.Detail = err.Error()
			return ar, true
		}
		if code != 202 && st.State != LDAPPreparingReset && st.State != LDAPResetting && st.State != LDAPVerifying {
			// Idle Ready without a 202/in-progress transition is not this reset.
			if code != 202 {
				ar.Status = "failed"
				ar.Detail = fmt.Sprintf("labldap reset HTTP %d (need 202 or in-progress state)", code)
				return ar, true
			}
		}
		if err := s.waitLDAPReset(ctx); err != nil {
			ar.Status = "failed"
			ar.Detail = err.Error()
			return ar, true
		}
		return ar, false
	case "labtacacs":
		if s.Clients.TacLab == nil {
			ar.Status = "failed"
			ar.Detail = "no client configured"
			return ar, true
		}
		if err := s.Clients.TacLab.RuntimeReset(ctx); err != nil {
			ar.Status = "failed"
			ar.Detail = err.Error()
			return ar, true
		}
		return ar, false
	default:
		ar.Status = "failed"
		ar.Detail = "unknown appliance"
		return ar, true
	}
}

func (s *Service) waitLDAPReset(ctx context.Context) error {
	deadline := s.LDAPResetWait
	if deadline == 0 {
		deadline = ldapResetTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	t := time.NewTicker(ldapResetPoll)
	defer t.Stop()
	// Idle Ready on GET is not this reset. Require a non-idle observation
	// (PreparingReset / Resetting / Verifying) before treating Ready as success.
	sawProgress := false
	for {
		st, err := s.Clients.LDAP.GetReset(ctx)
		if err != nil {
			return err
		}
		switch st.State {
		case LDAPPreparingReset, LDAPResetting, LDAPVerifying:
			sawProgress = true
		case LDAPReady:
			if sawProgress {
				return nil
			}
		case LDAPFailed:
			return fmt.Errorf("labldap reset failed")
		case "":
			// under-specified after 202 — keep polling
		default:
			return fmt.Errorf("labldap reset unknown state %q", st.State)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("labldap reset timeout")
		case <-t.C:
		}
	}
}

func (s *Service) record(op, name string, res *GraphResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.j.ByApp == nil {
		s.j.ByApp = map[string]ApplianceResult{}
	}
	s.j.Name = name
	s.j.LastOp = op
	s.j.LastOK = res.OK
	for _, ar := range res.Results {
		s.j.ByApp[ar.Appliance] = ar
	}
}
