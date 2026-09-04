package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVendorPinsLatestReleases(t *testing.T) {
	got := map[string]string{}
	for _, repo := range vendorRepos {
		got[repo.Dest] = repo.Ref
	}
	if got["third_party/go-lab-ldap-mcp"] != "v0.5.0" {
		t.Fatalf("labldap pin = %q, want v0.5.0", got["third_party/go-lab-ldap-mcp"])
	}
	if got["third_party/go-lab-tacacs-mcp"] != "v1.5.0" {
		t.Fatalf("taclab pin = %q, want v1.5.0", got["third_party/go-lab-tacacs-mcp"])
	}
	if got["third_party/go-lab-dns"] != "v1.3.0" {
		t.Fatalf("labdns pin = %q, want v1.3.0", got["third_party/go-lab-dns"])
	}
	if got["third_party/go-lab-maildev"] != "v1.0.0-rc.4" {
		t.Fatalf("labmail pin = %q, want v1.0.0-rc.4", got["third_party/go-lab-maildev"])
	}
	if got["third_party/go-lab-mitmproxy"] != "v1.6.0" {
		t.Fatalf("labmitm pin = %q, want v1.6.0", got["third_party/go-lab-mitmproxy"])
	}
	if got["third_party/go-lab-ntp"] != "v1.0.0-rc.3" {
		t.Fatalf("labntp pin = %q, want v1.0.0-rc.3", got["third_party/go-lab-ntp"])
	}
	if got["third_party/go-lab-sso"] != "v1.0.0-rc.1" {
		t.Fatalf("labsso pin = %q, want v1.0.0-rc.1", got["third_party/go-lab-sso"])
	}
}

func TestRatarmountDebPin(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docker", "ratarmount", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ARG RATARMOUNT_VERSION=0.1.28") {
		t.Fatalf("ratarmount Dockerfile pin is not 0.1.28:\n%s", b)
	}
}

func TestMCPJungleImagePin(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docker-compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "ghcr.io/mcpjungle/mcpjungle:${MCPJUNGLE_IMAGE_TAG:-0.4.6}"
	images := map[string]string{}
	var current string
	for _, line := range strings.Split(string(b), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(trim, ":") && !strings.HasPrefix(trim, "#") {
			current = strings.TrimSuffix(trim, ":")
			continue
		}
		if !strings.HasPrefix(trim, "image:") {
			continue
		}
		if current == "mcpjungle" || current == "registrar" {
			images[current] = strings.TrimSpace(strings.TrimPrefix(trim, "image:"))
		}
	}
	for _, name := range []string{"mcpjungle", "registrar"} {
		got := images[name]
		if got != want {
			t.Errorf("%s image = %q, want %q", name, got, want)
		}
		if strings.Contains(got, ":latest") {
			t.Errorf("%s image uses :latest: %s", name, got)
		}
	}
	env, err := os.ReadFile(filepath.Join("..", "..", "profiles", "default", "profile.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "MCPJUNGLE_IMAGE_TAG=0.4.6") {
		t.Fatal("profiles/default/profile.env must set MCPJUNGLE_IMAGE_TAG=0.4.6")
	}
}

func TestVendorPatchesEmpty(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "patches", "go-lab-*-*.patch"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("appliance patches = %v, want none", matches)
	}
}

func TestLabDNSPatch(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "patches", "go-lab-dns-*.patch"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("labdns patches = %v, want none (MCP pin is allowLegacyClients in profile YAML)", matches)
	}
}
