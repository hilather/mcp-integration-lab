package labgraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testMux(t *testing.T, token string) (*http.ServeMux, *Service) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "default.yaml"), []byte(emptyYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewService(dir, Clients{Family: map[string]FamilyClient{}})
	sess := NewSessionStore(token)
	mux := http.NewServeMux()
	REST(mux, svc, sess)
	mux.Handle("/", sess.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// wrap REST already registered — Protect applied in NewHandler tests via wrap
		http.NotFound(w, r)
	})))
	return mux, svc
}

func TestRESTValidateEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "default.yaml"), []byte(emptyYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewService(dir, Clients{Family: map[string]FamilyClient{}})
	sess := NewSessionStore("tok")
	mux := http.NewServeMux()
	REST(mux, svc, sess)
	h := sess.Protect(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/scenarios/default:validate", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var res GraphResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("%+v", res)
	}
}

func TestSessionCSRFRequired(t *testing.T) {
	sess := NewSessionStore("tok")
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/scenarios/default:apply", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := sess.Protect(mux)

	// login
	req := httptest.NewRequest(http.MethodPost, "/v1/session", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	sess.Create(rr, req)
	var body map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	cookie := rr.Result().Cookies()[0]
	if cookie.HttpOnly != true || cookie.Secure {
		t.Fatalf("cookie flags HttpOnly=%v Secure=%v", cookie.HttpOnly, cookie.Secure)
	}

	// cookie POST without CSRF
	req = httptest.NewRequest(http.MethodPost, "/v1/scenarios/default:apply", nil)
	req.AddCookie(cookie)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403 without CSRF, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/scenarios/default:apply", nil)
	req.AddCookie(cookie)
	req.Header.Set(csrfHeader, body["csrfToken"])
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200 with CSRF, got %d", rr.Code)
	}
}

func TestBearerNoOrigin(t *testing.T) {
	sess := NewSessionStore("tok")
	mux := http.NewServeMux()
	REST(mux, NewService(t.TempDir(), Clients{}), sess)
	h := SPAOrigins(nil, sess.Protect(mux))
	req := httptest.NewRequest(http.MethodGet, "/v1/health/ready", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ready without auth: %d", rr.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/scenarios", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		// empty dir 500 is ok; unauthorized is not
		if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
			t.Fatalf("bearer without Origin rejected: %d", rr.Code)
		}
	}
}

func TestClientSetsBearer(t *testing.T) {
	dir := t.TempDir()
	tok := filepath.Join(dir, "tok")
	if err := os.WriteFile(tok, []byte("secret-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := NewClient("http://127.0.0.1:18091", tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "secret-token" || !strings.HasPrefix(c.authHeader(), "Bearer ") {
		t.Fatalf("token %q header %q", c.Token, c.authHeader())
	}
}

func TestNewHTTPLDAPRequiresCA(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewHTTPLDAP("https://control:8443", "t", filepath.Join(dir, "missing.crt")); err == nil {
		t.Fatal("missing CA must fail")
	}
	pem := filepath.Join(dir, "ca.crt")
	// minimal invalid PEM
	if err := os.WriteFile(pem, []byte("not-a-cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHTTPLDAP("https://control:8443", "t", pem); err == nil {
		t.Fatal("invalid PEM must fail")
	}
}
