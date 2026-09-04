package labgraph

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "labgraph_session"
	csrfHeader    = "X-LabGraph-CSRF"
	sessionTTL    = 8 * time.Hour
)

type session struct {
	CSRF    string
	Expires time.Time
}

type SessionStore struct {
	token string
	mu    sync.Mutex
	m     map[string]session
}

func NewSessionStore(token string) *SessionStore {
	return &SessionStore{token: token, m: map[string]session{}}
}

// LoadRequiredToken reads a bearer token file. An empty path or
// whitespace-only file is an error — empty used to disable auth on the
// published management port (Docker bind-mount of a missing file creates
// an empty host path; writeTokenIfMissing then only chmods it).
func LoadRequiredToken(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("token-file is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("token-file %s is empty", path)
	}
	return tok, nil
}

func (s *SessionStore) BearerOK(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

func (s *SessionStore) CookieOK(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.m[c.Value]
	if !ok || time.Now().After(sess.Expires) {
		return session{}, false
	}
	return sess, true
}

func mutating(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// Protect /v1 (except live/ready). Bearer always works (no Origin required).
// Cookie sessions need CSRF on mutating requests.
func (s *SessionStore) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1") {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/health/") {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/v1/session" && r.Method == http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		if s.BearerOK(r) {
			next.ServeHTTP(w, r)
			return
		}
		sess, ok := s.CookieOK(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if mutating(r) && subtle.ConstantTimeCompare([]byte(r.Header.Get(csrfHeader)), []byte(sess.CSRF)) != 1 {
			http.Error(w, "csrf", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *SessionStore) Create(w http.ResponseWriter, r *http.Request) {
	if !s.BearerOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := randHex(16)
	csrf := randHex(16)
	s.mu.Lock()
	s.m[id] = session{CSRF: csrf, Expires: time.Now().Add(sessionTTL)}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // HTTP management listener (LabDNS family)
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"csrfToken": csrf})
}

func (s *SessionStore) Get(w http.ResponseWriter, r *http.Request) {
	if s.BearerOK(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"auth": "bearer"})
		return
	}
	sess, ok := s.CookieOK(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"csrfToken": sess.CSRF})
}

func (s *SessionStore) Delete(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.mu.Lock()
		delete(s.m, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	w.WriteHeader(http.StatusNoContent)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func originAllowed(origin string, allow []string) bool {
	if origin == "" {
		return true // non-browser
	}
	switch strings.ToLower(origin) {
	case "http://127.0.0.1", "http://localhost", "http://[::1]",
		"https://127.0.0.1", "https://localhost", "https://[::1]":
		return true
	}
	// loopback with port
	for _, p := range []string{"http://127.0.0.1:", "http://localhost:", "http://[::1]:",
		"https://127.0.0.1:", "https://localhost:", "https://[::1]:"} {
		if strings.HasPrefix(origin, p) {
			return true
		}
	}
	for _, a := range allow {
		if origin == a {
			return true
		}
	}
	return false
}

// SPAOrigins gates cookie/SPA asset Origin only. Bearer /v1 and /mcp skip this.
func SPAOrigins(allow []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			next.ServeHTTP(w, r)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !originAllowed(o, allow) {
			http.Error(w, "origin is not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
