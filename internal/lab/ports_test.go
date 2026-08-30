package lab

import (
	"errors"
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
	if !isLabContainer("labtacacs-taclab-1") {
		t.Fatal("expected labtacacs- project prefix")
	}
	if !isLabContainer("taclab") {
		t.Fatal("expected vendored container_name taclab")
	}
	if !isLabContainer("/taclab") {
		t.Fatal("docker inspect Name is /taclab")
	}
	if isLabContainer("other-nginx-1") {
		t.Fatal("did not expect non-lab container")
	}
	if isLabContainer("taclab-proxy") {
		t.Fatal("taclab is exact, not a prefix")
	}
}

func TestPublishedPortHoldersFindsUDPAndTCP(t *testing.T) {
	const ps = "" +
		"taclab\t0.0.0.0:49->49/tcp, 0.0.0.0:300->300/tcp, 0.0.0.0:18049->8080/tcp, 0.0.0.0:1812->1812/udp, 0.0.0.0:1813->1813/udp, 0.0.0.0:2083->2083/tcp, 0.0.0.0:3799->3799/udp\n" +
		"mcplab-labdns-1\t0.0.0.0:10053->53/tcp, 0.0.0.0:10053->53/udp\n" +
		"other-nginx-1\t0.0.0.0:8080->80/tcp\n"
	got := publishedPortHolders(ps, 1812)
	if len(got) != 1 || got[0] != "taclab" {
		t.Fatalf("1812/udp holders = %v, want [taclab]", got)
	}
	got = publishedPortHolders(ps, 49)
	if len(got) != 1 || got[0] != "taclab" {
		t.Fatalf("49/tcp holders = %v, want [taclab]", got)
	}
	if got := publishedPortHolders(ps, 18120); len(got) != 0 {
		t.Fatalf("18120 must not match :1812->, got %v", got)
	}
}

func TestInspectPortBindingHoldersFindsRADIUSUDP(t *testing.T) {
	// Exact HostConfig.PortBindings shape from `docker inspect` (CI
	// smoke-dev: ps Ports omitted 1812/1813 UDP; TCP still listed).
	const out = `/taclab	{"49/tcp":[{"HostIp":"0.0.0.0","HostPort":"49"}],"300/tcp":[{"HostIp":"0.0.0.0","HostPort":"300"}],"8080/tcp":[{"HostIp":"0.0.0.0","HostPort":"18049"}],"1812/udp":[{"HostIp":"0.0.0.0","HostPort":"1812"}],"1813/udp":[{"HostIp":"0.0.0.0","HostPort":"1813"}],"2083/tcp":[{"HostIp":"0.0.0.0","HostPort":"2083"}]` + "}\n"
	got := inspectPortBindingHolders(out, 1812)
	if len(got) != 1 || got[0] != "taclab" {
		t.Fatalf("1812/udp holders = %v, want [taclab]", got)
	}
	got = inspectPortBindingHolders(out, 1813)
	if len(got) != 1 || got[0] != "taclab" {
		t.Fatalf("1813/udp holders = %v, want [taclab]", got)
	}
	got = inspectPortBindingHolders(out, 49)
	if len(got) != 1 || got[0] != "taclab" {
		t.Fatalf("49/tcp holders = %v, want [taclab]", got)
	}
	if got := inspectPortBindingHolders(out, 3799); len(got) != 0 {
		t.Fatalf("unpublished 3799 holders = %v", got)
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

func TestProcNetHasBoundPortTCPListenAndUDP(t *testing.T) {
	const tcpTable = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0031 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0000000000000000 100 0 0 10 0
   1: 0100007F:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2 1 0000000000000000 100 0 0 10 0
   2: 00000000:0031 0100007F:ABCD 01 00000000:00000000 00:00000000 00000000     0        0 3 1 0000000000000000 100 0 0 10 0
`
	const tcp6Table = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:012C 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 4 1 0000000000000000 100 0 0 10 0
`
	const udpTable = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 5 1 0000000000000000 100 0 0 10 0
`
	if !procNetHasBoundPort(tcpTable, 49, true) {
		t.Fatal("IPv4 0.0.0.0:49 LISTEN")
	}
	if !procNetHasBoundPort(tcpTable, 443, true) {
		t.Fatal("127.0.0.1:443 LISTEN")
	}
	if procNetHasBoundPort(tcpTable, 50, true) {
		t.Fatal("port 50 is not in the table")
	}
	if !procNetHasBoundPort(tcp6Table, 300, true) {
		t.Fatal("IPv6 [::]:300 LISTEN")
	}
	if !procNetHasBoundPort(udpTable, 53, false) {
		t.Fatal("UDP 0.0.0.0:53 bound")
	}
	if procNetHasBoundPort(udpTable, 53, true) {
		t.Fatal("UDP 07 is not TCP LISTEN")
	}
}

func TestProbePortPermissionDeniedIsNotOccupied(t *testing.T) {
	port, ok := findFreePrivilegedTCP(t)
	if !ok {
		return
	}
	if err := probePort(portTCP, port); err != nil {
		t.Fatalf("probePort(tcp, %d) = %v, want nil when bind is EACCES and nothing is listening", port, err)
	}
}

func TestPreflightPortsAvailablePrivilegedTacLabPorts(t *testing.T) {
	port, ok := findFreePrivilegedTCP(t)
	if !ok {
		return
	}
	r := &Runner{
		Prof: &profile.Profile{
			Values: map[string]string{
				"TACLAB_LEGACY_PORT": strconv.Itoa(port),
			},
		},
	}
	if err := r.preflightPortsAvailable(); err != nil {
		t.Fatalf("preflight treated privileged free port %d as occupied: %v", port, err)
	}
}

func TestProbePortOccupiedPrivilegedTCP(t *testing.T) {
	start := unprivilegedPortStart()
	found := false
	for p := 1; p < start; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(p)))
		if err == nil {
			ln.Close()
			t.Skip("process can bind privileged TCP ports")
		}
		if !isPermissionDenied(err) {
			continue
		}
		occupied, oerr := linuxProcPortOccupied(portTCP, p)
		if oerr != nil || !occupied {
			continue
		}
		if err := probePort(portTCP, p); err == nil {
			t.Fatalf("probePort(tcp, %d) = nil, want occupied (/proc LISTEN)", p)
		}
		found = true
		break
	}
	if !found {
		t.Skip("no privileged TCP LISTEN socket to assert occupied-after-EACCES")
	}
}

func unprivilegedPortStart() int {
	b, err := os.ReadFile("/proc/sys/net/ipv4/ip_unprivileged_port_start")
	if err != nil {
		return 1024
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
	if convErr != nil || n < 1 {
		return 1024
	}
	return n
}

func findFreePrivilegedTCP(t *testing.T) (int, bool) {
	t.Helper()
	start := unprivilegedPortStart()
	sawEACCES := false
	for p := 1; p < start; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(p)))
		if err == nil {
			ln.Close()
			t.Skip("process can bind privileged TCP ports")
		}
		if !isPermissionDenied(err) {
			continue
		}
		sawEACCES = true
		occupied, oerr := linuxProcPortOccupied(portTCP, p)
		if oerr != nil {
			if errors.Is(oerr, errNoProcNet) {
				t.Skip("no /proc/net tables; cannot assert privileged-port occupancy")
			}
			t.Fatalf("linuxProcPortOccupied(%d): %v", p, oerr)
		}
		if !occupied {
			return p, true
		}
	}
	if !sawEACCES {
		t.Fatal("no privileged TCP port returned EACCES; cannot regress CI smoke-dev")
	}
	t.Skip("every privileged TCP port that returned EACCES is listening")
	return 0, false
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
