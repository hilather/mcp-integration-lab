package lab

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/profile"
)

func TestPublishedPortBindingsDedupesDNS(t *testing.T) {
	p := &profile.Profile{
		Values: map[string]string{
			"LABDNS_DNS_PORT": "53",
			"NFS_PORT":        "2049",
		},
	}
	got, err := publishedPortBindings(p)
	if err != nil {
		t.Fatal(err)
	}
	var tcp53, udp53 int
	for _, b := range got {
		if b.Port == 53 {
			for _, proto := range b.Protos {
				switch proto {
				case portTCP:
					tcp53++
				case portUDP:
					udp53++
				}
			}
		}
	}
	if tcp53 != 1 || udp53 != 1 {
		t.Fatalf("LABDNS_DNS_PORT bindings = tcp:%d udp:%d, want 1 each", tcp53, udp53)
	}
}

func TestPublishedPortSpecsIncludesLabMITM(t *testing.T) {
	want := map[string]bool{"LABMITM_PROXY_PORT": false, "LABMITM_WEB_PORT": false}
	for _, spec := range publishedPortSpecs {
		if _, ok := want[spec.EnvKey]; ok {
			want[spec.EnvKey] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("publishedPortSpecs missing %s", k)
		}
	}
}

func TestIsLabContainer(t *testing.T) {
	if !isLabContainer("mcplab-labdns-1") {
		t.Fatal("expected mcplab container")
	}
	if isLabContainer("other-nginx-1") {
		t.Fatal("did not expect non-lab container")
	}
}

func TestPreflightPortsAvailableOnFreePort(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	r := &Runner{
		Prof: &profile.Profile{
			Values: map[string]string{
				"MCP_GATEWAY_PORT": strconv.Itoa(port),
			},
		},
	}
	if err := r.preflightPortsAvailable(); err != nil {
		t.Fatalf("preflightPortsAvailable() unexpected error: %v", err)
	}
}

func TestPreflightPortsAvailableBlocksConflict(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	r := &Runner{
		Prof: &profile.Profile{
			Values: map[string]string{
				"MCP_GATEWAY_PORT": strconv.Itoa(port),
			},
		},
	}
	err = r.preflightPortsAvailable()
	if err == nil {
		t.Fatal("preflightPortsAvailable() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "MCP_GATEWAY_PORT") {
		t.Fatalf("error = %v, want MCP_GATEWAY_PORT conflict", err)
	}
}
