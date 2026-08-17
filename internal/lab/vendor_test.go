package lab

import "testing"

func TestVendorPinsLatestReleases(t *testing.T) {
	got := map[string]string{}
	for _, repo := range vendorRepos {
		got[repo.Dest] = repo.Ref
	}
	if got["third_party/go-lab-ldap-mcp"] != "v0.2.2" {
		t.Fatalf("labldap pin = %q, want v0.2.2", got["third_party/go-lab-ldap-mcp"])
	}
	if got["third_party/go-lab-tacacs-mcp"] != "v1.3.0" {
		t.Fatalf("taclab pin = %q, want v1.3.0", got["third_party/go-lab-tacacs-mcp"])
	}
	if got["third_party/go-lab-dns"] != "" {
		t.Fatalf("labdns should stay unpinned until MCP wiring lands upstream, got %q", got["third_party/go-lab-dns"])
	}
}
