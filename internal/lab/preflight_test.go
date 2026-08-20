package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/profile"
)

const testProfileName = "teamx"

func writeProfileEnv(t *testing.T, dir, body string) {
	t.Helper()
	profDir := filepath.Join(dir, "profiles", testProfileName)
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "profile.env"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testProfileDir(root string) string {
	return filepath.Join(root, "profiles", testProfileName)
}

func TestPreflightOKWhenNoCriticalDrift(t *testing.T) {
	root := t.TempDir()
	writeProfileEnv(t, root, "LAB_PUBLIC_HOST=10.0.0.9\nLAB_DEV_MODE=true\n")
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LAB_PUBLIC_HOST": "10.0.0.9",
				"LAB_DEV_MODE":    "true",
			},
		},
	}
	if err := r.Preflight(); err != nil {
		t.Fatalf("Preflight() unexpected error: %v", err)
	}
}

func TestPreflightFailsOnDNSPortDrift(t *testing.T) {
	root := t.TempDir()
	writeProfileEnv(t, root, "LABDNS_DNS_PORT=53\n")
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LABDNS_DNS_PORT": "10053",
			},
		},
	}
	err := r.Preflight()
	if err == nil {
		t.Fatal("Preflight() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "LABDNS_DNS_PORT") {
		t.Fatalf("Preflight() missing key in error: %v", err)
	}
}

func TestPreflightFailsOnCriticalDrift(t *testing.T) {
	root := t.TempDir()
	writeProfileEnv(t, root, "LAB_PUBLIC_HOST=10.0.0.9\nLAB_DEV_MODE=true\n")
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LAB_PUBLIC_HOST": "localhost",
				"LAB_DEV_MODE":    "true",
			},
		},
	}
	err := r.Preflight()
	if err == nil {
		t.Fatal("Preflight() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "LAB_PUBLIC_HOST") {
		t.Fatalf("Preflight() missing key in error: %v", err)
	}
}

func TestPreflightAllowsDriftWithBypassFlag(t *testing.T) {
	root := t.TempDir()
	writeProfileEnv(t, root, "LAB_PUBLIC_HOST=10.0.0.9\n")
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LAB_PUBLIC_HOST":                "localhost",
				"MCPLAB_ALLOW_PROFILE_OVERRIDES": "true",
			},
		},
	}
	if err := r.Preflight(); err != nil {
		t.Fatalf("Preflight() unexpected error with bypass: %v", err)
	}
}
