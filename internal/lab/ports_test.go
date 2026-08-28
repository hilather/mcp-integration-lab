package lab

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
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

func TestIsPermissionDenied(t *testing.T) {
	denied := &net.OpError{
		Op:  "listen",
		Net: "tcp",
		Err: os.NewSyscallError("bind", syscall.EACCES),
	}
	if !isPermissionDenied(denied) {
		t.Fatal("EACCES must be permission denied")
	}
	perm := &net.OpError{
		Op:  "listen",
		Net: "tcp",
		Err: os.NewSyscallError("bind", syscall.EPERM),
	}
	if !isPermissionDenied(perm) {
		t.Fatal("EPERM must be permission denied")
	}
	inUse := &net.OpError{
		Op:  "listen",
		Net: "tcp",
		Err: os.NewSyscallError("bind", syscall.EADDRINUSE),
	}
	if isPermissionDenied(inUse) {
		t.Fatal("EADDRINUSE must not be treated as permission denied")
	}
	if isPermissionDenied(nil) {
		t.Fatal("nil must not be permission denied")
	}
}

func TestProbePortPermissionDeniedIsNotOccupied(t *testing.T) {
	// Default-profile TacLab ports. A non-root user cannot bind them; that is
	// not occupancy — dockerd can still publish them (CI smoke-dev).
	for _, port := range []int{49, 300} {
		addr := fmt.Sprintf("0.0.0.0:%d", port)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			ln.Close()
		} else if !isPermissionDenied(err) {
			t.Fatalf("listen tcp %s = %v, want success or permission denied", addr, err)
		}
		if err := probePort(portTCP, port); err != nil {
			t.Fatalf("probePort(tcp, %d) = %v, want nil (EACCES is not occupied)", port, err)
		}
	}
}

func TestPreflightPortsAvailablePrivilegedTacLabPorts(t *testing.T) {
	r := &Runner{
		Prof: &profile.Profile{
			Values: map[string]string{
				"TACLAB_LEGACY_PORT": "49",
				"TACLAB_TLS_PORT":    "300",
			},
		},
	}
	if err := r.preflightPortsAvailable(); err != nil {
		t.Fatalf("preflightPortsAvailable() on privileged TacLab ports: %v", err)
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
