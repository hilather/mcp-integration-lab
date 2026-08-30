package labgraph

import (
	"context"
	"encoding/json"
	"fmt"
)

const ldapOpDisableUser = "disableUser"

type ldapControlOp struct {
	Op string `json:"op"`
	ID string `json:"id"`
}

func parseLDAPControlOps(m map[string]any) ([]ldapControlOp, error) {
	for _, flatten := range []string{"users", "groups", "suffix", "apiVersion", "kind"} {
		if _, ok := m[flatten]; ok {
			return nil, fmt.Errorf("%s", errNoLDAPApply)
		}
	}
	raw, err := json.Marshal(m["operations"])
	if err != nil {
		return nil, err
	}
	var ops []ldapControlOp
	if err := json.Unmarshal(raw, &ops); err != nil {
		return nil, fmt.Errorf("labldap operations: %w", err)
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("labldap operations: empty")
	}
	for i, op := range ops {
		if op.Op != ldapOpDisableUser {
			return nil, fmt.Errorf("%s: %s", errNoLDAPApply, op.Op)
		}
		if op.ID == "" {
			return nil, fmt.Errorf("labldap operations[%d]: id is required", i)
		}
	}
	return ops, nil
}

func (s *Service) walkLDAP(ctx context.Context, m map[string]any, mode walkMode) ApplianceResult {
	ar := ApplianceResult{Appliance: "labldap"}
	ops, err := parseLDAPControlOps(m)
	if err != nil {
		ar.Status = "failed"
		ar.Detail = err.Error()
		return ar
	}
	if s.Clients.LDAP == nil {
		ar.Status = "failed"
		ar.Detail = "no client configured"
		return ar
	}
	switch mode {
	case walkValidate, walkPlan:
		for _, op := range ops {
			_, native, err := s.Clients.LDAP.GetUser(ctx, op.ID)
			if err != nil {
				ar.Status = "failed"
				ar.Detail = err.Error()
				ar.Native = native
				return ar
			}
			ar.Native = native
		}
		if mode == walkValidate {
			ar.Status = "validated"
		} else {
			ar.Status = "planned"
		}
		return ar
	case walkApply:
		var last json.RawMessage
		for _, op := range ops {
			etag, native, err := s.Clients.LDAP.GetUser(ctx, op.ID)
			if err != nil {
				ar.Status = "failed"
				ar.Detail = err.Error()
				ar.Native = native
				return ar
			}
			last, err = s.Clients.LDAP.DisableUser(ctx, op.ID, etag)
			if err != nil {
				ar.Status = "failed"
				ar.Detail = err.Error()
				ar.Native = last
				return ar
			}
		}
		ar.Status = "applied"
		ar.Native = last
		return ar
	default:
		ar.Status = "failed"
		ar.Detail = "unknown walk mode"
		return ar
	}
}
