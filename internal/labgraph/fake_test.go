package labgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type call struct {
	Op   string
	Body json.RawMessage
}

type fakeFamily struct {
	mu       sync.Mutex
	Calls    []call
	State    FamilyState
	ValidateFn func(json.RawMessage) (json.RawMessage, error)
	PlanFn   func(json.RawMessage) (json.RawMessage, error)
	ApplyFn  func(json.RawMessage) (json.RawMessage, error)
	ResetFn  func(string) (json.RawMessage, error)
}

func (f *fakeFamily) rec(op string, body json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append(json.RawMessage(nil), body...)
	f.Calls = append(f.Calls, call{Op: op, Body: cp})
}

func (f *fakeFamily) GetState(context.Context) (FamilyState, error) {
	f.rec("getState", nil)
	return f.State, nil
}

func (f *fakeFamily) Validate(_ context.Context, body json.RawMessage) (json.RawMessage, error) {
	f.rec("validate", body)
	if f.ValidateFn != nil {
		return f.ValidateFn(body)
	}
	return []byte(`{"ok":true}`), nil
}

func (f *fakeFamily) Plan(_ context.Context, body json.RawMessage) (json.RawMessage, error) {
	f.rec("plan", body)
	if f.PlanFn != nil {
		return f.PlanFn(body)
	}
	return []byte(`{"ok":true}`), nil
}

func (f *fakeFamily) Apply(_ context.Context, body json.RawMessage) (json.RawMessage, error) {
	f.rec("apply", body)
	if f.ApplyFn != nil {
		return f.ApplyFn(body)
	}
	return []byte(`{"ok":true}`), nil
}

func (f *fakeFamily) Reset(_ context.Context, reason string) (json.RawMessage, error) {
	f.rec("reset", json.RawMessage(reason))
	if f.ResetFn != nil {
		return f.ResetFn(reason)
	}
	return []byte(`{"ok":true}`), nil
}

type fakeLDAP struct {
	mu        sync.Mutex
	Calls     []string
	Baseline  LDAPBaseline
	PostCode  int
	PostState LDAPResetStatus
	GetState  LDAPResetStatus
	GetSeq    []LDAPResetStatus
	getN      int
	PostErr   error
	FailGet   bool
}

func (f *fakeLDAP) GetBaseline(context.Context) (LDAPBaseline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "baseline")
	return f.Baseline, nil
}

func (f *fakeLDAP) PostReset(_ context.Context, name, expectedRevision string) (int, LDAPResetStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "post:"+name+":"+expectedRevision)
	code := f.PostCode
	if code == 0 {
		code = 202
	}
	return code, f.PostState, f.PostErr
}

func (f *fakeLDAP) GetReset(context.Context) (LDAPResetStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "getReset")
	if f.FailGet {
		return LDAPResetStatus{}, fmt.Errorf("get reset failed")
	}
	if f.getN < len(f.GetSeq) {
		st := f.GetSeq[f.getN]
		f.getN++
		return st, nil
	}
	st := f.GetState
	if st.State == "" && len(f.GetSeq) == 0 {
		st.State = LDAPReady
	}
	return st, nil
}

type fakeTac struct {
	mu    sync.Mutex
	Calls []string
	Err   error
}

func (f *fakeTac) Status(context.Context) (json.RawMessage, error) {
	return []byte(`{}`), nil
}

func (f *fakeTac) RuntimeReset(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "runtime.reset")
	return f.Err
}
