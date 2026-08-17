package maildev

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "maildev.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestArgsMissingFileEmitsBase(t *testing.T) {
	got, err := Args(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "--smtp 1025 --web 1080" {
		t.Fatalf("base args = %q", got)
	}
}

func TestArgsRendersFlagShapes(t *testing.T) {
	p := write(t, `
flags:
  verbose: true
  silent: false
  mail-directory: /tmp/mail
  hide-extensions: [STARTTLS, SMTPUTF8]
  ip: 0.0.0.0
`)
	got, err := Args(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "--smtp 1025 --web 1080 --hide-extensions STARTTLS SMTPUTF8 --ip 0.0.0.0 --mail-directory /tmp/mail --verbose"
	if got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestArgsRejectsRelayFlags(t *testing.T) {
	for _, flag := range []string{
		"outgoing-host: smtp.example.com",
		"outgoing-port: 25",
		"outgoing-user: bob",
		"outgoing-pass: hunter2",
		"outgoing-secure: true",
		"auto-relay: true",
		"auto-relay-rules: /rules.json",
	} {
		p := write(t, "flags:\n  "+flag+"\n")
		_, err := Args(p)
		if err == nil || !strings.Contains(err.Error(), "receive-only") {
			t.Fatalf("flag %q: expected receive-only rejection, got %v", flag, err)
		}
	}
}

func TestArgsRejectsManagedFlags(t *testing.T) {
	for _, flag := range []string{"smtp: 2525", "web: 9999", "web-user: x", "web-pass: y"} {
		p := write(t, "flags:\n  "+flag+"\n")
		_, err := Args(p)
		if err == nil || !strings.Contains(err.Error(), "managed by the lab") {
			t.Fatalf("flag %q: expected managed rejection, got %v", flag, err)
		}
	}
}

func TestArgsRejectsLeadingDashesAlias(t *testing.T) {
	// Sneaking the flag in with dashes must not bypass the guard.
	p := write(t, "flags:\n  --auto-relay: true\n")
	if _, err := Args(p); err == nil {
		t.Fatal("expected rejection of --auto-relay")
	}
}

func TestArgsRejectsInjectableValues(t *testing.T) {
	p := write(t, `flags:
  mail-directory: "/tmp/x --auto-relay"
`)
	if _, err := Args(p); err == nil {
		t.Fatal("expected rejection of value containing whitespace")
	}
}
