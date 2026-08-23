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
	if got["third_party/go-lab-ldap-mcp"] != "v0.3.0" {
		t.Fatalf("labldap pin = %q, want v0.3.0", got["third_party/go-lab-ldap-mcp"])
	}
	if got["third_party/go-lab-tacacs-mcp"] != "v1.3.0" {
		t.Fatalf("taclab pin = %q, want v1.3.0", got["third_party/go-lab-tacacs-mcp"])
	}
	if got["third_party/go-lab-dns"] != "v1.1.0" {
		t.Fatalf("labdns pin = %q, want v1.1.0", got["third_party/go-lab-dns"])
	}
	if got["third_party/go-lab-maildev"] != "v1.0.0-rc.3" {
		t.Fatalf("labmail pin = %q, want v1.0.0-rc.3", got["third_party/go-lab-maildev"])
	}
	if got["third_party/go-lab-mitmproxy"] != "v1.1.0" {
		t.Fatalf("labmitm pin = %q, want v1.1.0", got["third_party/go-lab-mitmproxy"])
	}
}

func TestRatarmountDebPin(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docker", "ratarmount", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ARG RATARMOUNT_VERSION=0.1.24") {
		t.Fatalf("ratarmount Dockerfile pin is not 0.1.24:\n%s", b)
	}
}

func TestLabDNSPatchIsPinRelaxation(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "patches", "go-lab-dns-*.patch"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || !strings.HasSuffix(matches[0], "go-lab-dns-relax-mcp-pin.patch") {
		t.Fatalf("labdns patches = %v, want go-lab-dns-relax-mcp-pin.patch only", matches)
	}
}
