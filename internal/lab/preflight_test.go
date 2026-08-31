package lab

import (
	"io"
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

func TestPreflightKeysIncludesLabSSO(t *testing.T) {
	want := map[string]bool{"LABSSO_HTTPS_PORT": false, "LABSSO_REST_PORT": false}
	for _, k := range preflightKeys {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("preflightKeys missing %s", k)
		}
	}
}

func TestPreflightKeysIncludesLabNTP(t *testing.T) {
	want := map[string]bool{"LABNTP_NTP_PORT": false, "LABNTP_REST_PORT": false}
	for _, k := range preflightKeys {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("preflightKeys missing %s", k)
		}
	}
}

func TestPreflightKeysIncludesLabMITM(t *testing.T) {
	want := map[string]bool{"LABMITM_PROXY_PORT": false, "LABMITM_WEB_PORT": false, "LAB_DOCKER_SUBNET": false}
	for _, k := range preflightKeys {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("preflightKeys missing %s", k)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	b, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
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

func TestPreflightSkipsMissingLabmitmBootstrap(t *testing.T) {
	root := t.TempDir()
	writeProfileEnv(t, root, "LAB_PUBLIC_HOST=10.0.0.9\n")
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LAB_PUBLIC_HOST": "10.0.0.9",
			},
		},
	}
	if _, err := os.Stat(filepath.Join(testProfileDir(root), "labmitm", "bootstrap.yaml")); !os.IsNotExist(err) {
		t.Fatalf("bootstrap should be missing, stat: %v", err)
	}
	out := captureStdout(t, func() {
		if err := r.Preflight(); err != nil {
			t.Errorf("Preflight() unexpected error: %v", err)
		}
	})
	if strings.Contains(out, "originAllowlist") {
		t.Fatalf("missing bootstrap must not warn: %q", out)
	}
}

func TestPreflightOKWhenLabmitmOriginAllowlistEmpty(t *testing.T) {
	root := t.TempDir()
	writeProfileEnv(t, root, "LAB_PUBLIC_HOST=10.0.0.9\n")
	mitmDir := filepath.Join(testProfileDir(root), "labmitm")
	if err := os.MkdirAll(mitmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("apiVersion: labmitm.dev/v1alpha1\nkind: LabMITM\nspec:\n  management:\n    originAllowlist: []\n")
	if err := os.WriteFile(filepath.Join(mitmDir, "bootstrap.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LAB_PUBLIC_HOST": "10.0.0.9",
			},
		},
	}
	out := captureStdout(t, func() {
		if err := r.Preflight(); err != nil {
			t.Errorf("Preflight() unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "warning: LAB_PUBLIC_HOST=10.0.0.9 is not loopback; add http://10.0.0.9:18088 to") {
		t.Fatalf("missing origin warning: %q", out)
	}
	if !strings.Contains(out, "labmitm originAllowlist") {
		t.Fatalf("warning missing originAllowlist: %q", out)
	}
}

func TestPreflightOKWhenLabntpAllowedOriginsEmpty(t *testing.T) {
	root := t.TempDir()
	writeProfileEnv(t, root, "LAB_PUBLIC_HOST=10.0.0.9\n")
	ntpDir := filepath.Join(testProfileDir(root), "labntp")
	if err := os.MkdirAll(ntpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("apiVersion: labntp.dev/v1alpha1\nkind: LabNTP\nspec:\n  management:\n    allowedOrigins: []\n    originAllowlist: [\"http://ignore.example\"]\n")
	if err := os.WriteFile(filepath.Join(ntpDir, "bootstrap.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LAB_PUBLIC_HOST": "10.0.0.9",
			},
		},
	}
	out := captureStdout(t, func() {
		if err := r.Preflight(); err != nil {
			t.Errorf("Preflight() unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "warning: LAB_PUBLIC_HOST=10.0.0.9 is not loopback; add http://10.0.0.9:18123 to") {
		t.Fatalf("missing origin warning: %q", out)
	}
	if !strings.Contains(out, "labntp allowedOrigins") {
		t.Fatalf("warning missing allowedOrigins: %q", out)
	}
	if strings.Contains(out, "labntp originAllowlist") {
		t.Fatalf("LabNTP warn must parse allowedOrigins, not originAllowlist: %q", out)
	}
}

func TestPreflightFailsOnInvalidDockerSubnet(t *testing.T) {
	root := t.TempDir()
	writeProfileEnv(t, root, "LAB_DOCKER_SUBNET=10.99.42.0/16\n")
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LAB_DOCKER_SUBNET": "10.99.42.0/16",
			},
		},
	}
	err := r.Preflight()
	if err == nil {
		t.Fatal("Preflight() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "LAB_DOCKER_SUBNET") {
		t.Fatalf("Preflight() missing subnet in error: %v", err)
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

func TestPreflightSkipsMissingLabssoBootstrap(t *testing.T) {
	// Do not put LABSSO_HTTPS_PORT in Values. Preflight() always ends in
	// preflightPortsAvailable(); a dest-443 probe fails make test when a
	// non-lab process holds 443. checkLabssoIssuer returns before derive
	// when the bootstrap file is missing — the skip does not need the port.
	root := t.TempDir()
	writeProfileEnv(t, root, "LAB_PUBLIC_HOST=localhost\n")
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LAB_PUBLIC_HOST": "localhost",
			},
		},
	}
	if _, err := os.Stat(filepath.Join(testProfileDir(root), "labsso", "bootstrap.yaml")); !os.IsNotExist(err) {
		t.Fatalf("bootstrap should be missing, stat: %v", err)
	}
	if err := r.Preflight(); err != nil {
		t.Fatalf("missing labsso bootstrap must skip issuer check: %v", err)
	}
}

func TestPreflightLabssoIssuerMismatch(t *testing.T) {
	root := t.TempDir()
	writeProfileEnv(t, root, "LAB_PUBLIC_HOST=localhost\nLABSSO_HTTPS_PORT=443\n")
	dir := filepath.Join(testProfileDir(root), "labsso")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("apiVersion: labsso.dev/v1alpha1\nkind: LabSSO\nspec:\n  issuer: https://localhost:443\n")
	if err := os.WriteFile(filepath.Join(dir, "bootstrap.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Runner{
		Prof: &profile.Profile{
			Name: testProfileName,
			Dir:  testProfileDir(root),
			Values: map[string]string{
				"LAB_PUBLIC_HOST":   "localhost",
				"LABSSO_HTTPS_PORT": "443",
			},
		},
	}
	err := r.Preflight()
	if err == nil || !strings.Contains(err.Error(), "spec.issuer") || !strings.Contains(err.Error(), "https://localhost") {
		t.Fatalf("want issuer mismatch naming YAML edit, got %v", err)
	}
}
