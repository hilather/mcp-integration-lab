package labgraph

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSPAServesIndex(t *testing.T) {
	h := SPA()
	for _, path := range []string{"/", "/index.html"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rr.Code)
		}
		ct := rr.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("%s: Content-Type %q", path, ct)
		}
		if !strings.Contains(rr.Body.String(), "LabGraph") {
			t.Fatalf("%s: missing LabGraph title chrome", path)
		}
	}
}

func TestSPAChromeContract(t *testing.T) {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, need := range []string{
		"labgraph_session",
		"X-LabGraph-CSRF",
		"Reset (all five)",
		"Reset all five",
		"labdns → labmitm → maildev → labldap → labtacacs",
		"Empty spec does not skip",
		"No auto-rollback",
		"Skip to main content",
		":focus-visible",
		"#0b0c0e",
		"#d4a06a",
		"#c45c5c",
		"IBM Plex",
		"r.status === 204",
	} {
		if !strings.Contains(src, need) {
			t.Errorf("SPA missing %q", need)
		}
	}
	for _, ban := range []string{"localStorage", "sessionStorage", "/mcp", "ws4", "jenkins", "Jenkins"} {
		if strings.Contains(src, ban) {
			t.Errorf("SPA must not contain %q", ban)
		}
	}
	if !strings.Contains(src, `method: "DELETE"`) && !strings.Contains(src, `"DELETE", "/v1/session"`) && !strings.Contains(src, `api("DELETE", "/v1/session")`) {
		t.Error("SPA must DELETE /v1/session on sign-out")
	}
}
