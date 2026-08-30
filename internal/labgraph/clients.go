package labgraph

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type FamilyState struct {
	RuntimeRevision string `json:"runtimeRevision"`
	Generation      int64  `json:"generation"`
	Drifted         bool   `json:"drifted"`
}

type LDAPBaseline struct {
	ExpectedRevision string `json:"expectedRevision"`
	AppliedRevision  string `json:"appliedRevision"`
	ControlRevision  string `json:"controlRevision"`
	Match            bool   `json:"match"`
}

// LDAP reset states from go-lab-ldap-mcp v0.5.0 internal/reset.
const (
	LDAPReady          = "Ready"
	LDAPPreparingReset = "PreparingReset"
	LDAPResetting      = "Resetting"
	LDAPVerifying      = "Verifying"
	LDAPFailed         = "Failed"
)

type LDAPResetStatus struct {
	Phase            string `json:"phase"`
	State            string `json:"state"`
	ExpectedRevision string `json:"expectedRevision"`
	AppliedRevision  string `json:"appliedRevision"`
}

type FamilyClient interface {
	GetState(ctx context.Context) (FamilyState, error)
	Validate(ctx context.Context, body json.RawMessage) (json.RawMessage, error)
	Plan(ctx context.Context, body json.RawMessage) (json.RawMessage, error)
	Apply(ctx context.Context, body json.RawMessage) (json.RawMessage, error)
	Reset(ctx context.Context, reason string) (json.RawMessage, error)
}

type LDAPClient interface {
	GetBaseline(ctx context.Context) (LDAPBaseline, error)
	PostReset(ctx context.Context, name, expectedRevision string) (statusCode int, st LDAPResetStatus, err error)
	GetReset(ctx context.Context) (LDAPResetStatus, error)
	GetUser(ctx context.Context, id string) (etag string, body json.RawMessage, err error)
	DisableUser(ctx context.Context, id, etag string) (json.RawMessage, error)
}

type TacLabClient interface {
	Status(ctx context.Context) (json.RawMessage, error)
	RuntimeReset(ctx context.Context) error
}

// HTTPFamily talks to LabDNS / LabMITM / LabMail native /v1.
type HTTPFamily struct {
	Base   string
	Token  string
	Client *http.Client
}

func (c *HTTPFamily) GetState(ctx context.Context) (FamilyState, error) {
	var st FamilyState
	if err := c.doJSON(ctx, http.MethodGet, "/v1/state", nil, &st); err != nil {
		return FamilyState{}, err
	}
	return st, nil
}

func (c *HTTPFamily) Validate(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	return c.doRaw(ctx, http.MethodPost, "/v1/state:validate", body)
}

func (c *HTTPFamily) Plan(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	return c.doRaw(ctx, http.MethodPost, "/v1/changes:plan", body)
}

func (c *HTTPFamily) Apply(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	return c.doRaw(ctx, http.MethodPost, "/v1/changes:apply", body)
}

func (c *HTTPFamily) Reset(ctx context.Context, reason string) (json.RawMessage, error) {
	b, _ := json.Marshal(map[string]string{"reason": reason})
	return c.doRaw(ctx, http.MethodPost, "/v1/state:reset", b)
}

func (c *HTTPFamily) doJSON(ctx context.Context, method, path string, body []byte, out any) error {
	raw, err := c.doRaw(ctx, method, path, body)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *HTTPFamily) doRaw(ctx context.Context, method, path string, body []byte) (json.RawMessage, error) {
	return doHTTP(ctx, c.client(), c.Base, c.Token, method, path, body)
}

func (c *HTTPFamily) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// HTTPLDAP talks to LabLDAP control REST. TLS must use the lab CA.
type HTTPLDAP struct {
	Base   string
	Token  string
	CAFile string
	Client *http.Client
}

func NewHTTPLDAP(base, token, caFile string) (*HTTPLDAP, error) {
	c, err := tlsClient(caFile)
	if err != nil {
		return nil, err
	}
	return &HTTPLDAP{Base: base, Token: token, CAFile: caFile, Client: c}, nil
}

func tlsClient(caFile string) (*http.Client, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("labldap CA %s: %w", caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("labldap CA %s: no PEM certificates", caFile)
	}
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    pool,
			},
		},
	}, nil
}

func (c *HTTPLDAP) GetBaseline(ctx context.Context) (LDAPBaseline, error) {
	var b LDAPBaseline
	raw, err := doHTTP(ctx, c.Client, c.Base, c.Token, http.MethodGet, "/api/v1/baseline", nil)
	if err != nil {
		return LDAPBaseline{}, err
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return LDAPBaseline{}, err
	}
	return b, nil
}

func (c *HTTPLDAP) PostReset(ctx context.Context, name, expectedRevision string) (int, LDAPResetStatus, error) {
	body, _ := json.Marshal(map[string]string{
		"name":              name,
		"expectedRevision": expectedRevision,
	})
	code, raw, err := doHTTPCode(ctx, c.Client, c.Base, c.Token, http.MethodPost, "/api/v1/reset", body)
	var st LDAPResetStatus
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &st)
	}
	return code, st, err
}

func (c *HTTPLDAP) GetReset(ctx context.Context) (LDAPResetStatus, error) {
	raw, err := doHTTP(ctx, c.Client, c.Base, c.Token, http.MethodGet, "/api/v1/reset", nil)
	if err != nil {
		return LDAPResetStatus{}, err
	}
	var st LDAPResetStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return LDAPResetStatus{}, err
	}
	return st, nil
}

func (c *HTTPLDAP) GetUser(ctx context.Context, id string) (string, json.RawMessage, error) {
	hdr, raw, err := doHTTPHeader(ctx, c.Client, c.Base, c.Token, http.MethodGet, "/api/v1/users/"+id, nil)
	if err != nil {
		return "", raw, err
	}
	etag := strings.TrimSpace(hdr.Get("ETag"))
	if etag == "" {
		var u struct {
			Revision string `json:"revision"`
		}
		if json.Unmarshal(raw, &u) == nil && u.Revision != "" {
			etag = `"` + u.Revision + `"`
		}
	}
	if etag == "" {
		return "", raw, fmt.Errorf("labldap GET /api/v1/users/%s: missing ETag", id)
	}
	return etag, raw, nil
}

func (c *HTTPLDAP) DisableUser(ctx context.Context, id, etag string) (json.RawMessage, error) {
	_, _, raw, err := doHTTPCodeHeader(ctx, c.Client, c.Base, c.Token, http.MethodPost, "/api/v1/users/"+id+"/disable", nil, etag)
	return raw, err
}

type HTTPTacLab struct {
	Base   string
	Token  string
	Client *http.Client
}

func (c *HTTPTacLab) Status(ctx context.Context) (json.RawMessage, error) {
	return doHTTP(ctx, c.client(), c.Base, c.Token, http.MethodGet, "/api/v1/status", nil)
}

func (c *HTTPTacLab) RuntimeReset(ctx context.Context) error {
	_, err := doHTTP(ctx, c.client(), c.Base, c.Token, http.MethodPost, "/api/v1/runtime/reset", []byte(`{}`))
	return err
}

func (c *HTTPTacLab) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func doHTTP(ctx context.Context, hc *http.Client, base, token, method, path string, body []byte) (json.RawMessage, error) {
	_, raw, err := doHTTPCode(ctx, hc, base, token, method, path, body)
	return raw, err
}

func doHTTPHeader(ctx context.Context, hc *http.Client, base, token, method, path string, body []byte) (http.Header, json.RawMessage, error) {
	_, hdr, raw, err := doHTTPCodeHeader(ctx, hc, base, token, method, path, body, "")
	return hdr, raw, err
}

func doHTTPCode(ctx context.Context, hc *http.Client, base, token, method, path string, body []byte) (int, json.RawMessage, error) {
	code, _, raw, err := doHTTPCodeHeader(ctx, hc, base, token, method, path, body, "")
	return code, raw, err
}

func doHTTPCodeHeader(ctx context.Context, hc *http.Client, base, token, method, path string, body []byte, ifMatch string) (int, http.Header, json.RawMessage, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, rdr)
	if err != nil {
		return 0, nil, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, resp.Header, nil, err
	}
	if resp.StatusCode >= 300 {
		return resp.StatusCode, resp.Header, raw, fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return resp.StatusCode, resp.Header.Clone(), raw, nil
}

func readTokenFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
