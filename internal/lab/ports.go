package lab

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hilather/mcp-integration-lab/internal/profile"
)

type portProto string

const (
	portTCP portProto = "tcp"
	portUDP portProto = "udp"
)

type portBinding struct {
	EnvKey string
	Port   int
	Protos []portProto
}

// publishedPortSpecs lists every host port the three compose projects may
// publish from the active profile. Keep in sync with docker-compose.yaml and
// the LabLDAP / TacLab overlays.
var publishedPortSpecs = []struct {
	EnvKey string
	Protos []portProto
}{
	{"LABDNS_DNS_PORT", []portProto{portTCP, portUDP}},
	{"LABDNS_REST_PORT", []portProto{portTCP}},
	{"NFS_PORT", []portProto{portTCP}},
	{"MAILDEV_SMTP_PORT", []portProto{portTCP}},
	{"MAILDEV_WEB_PORT", []portProto{portTCP}},
	{"LABMITM_PROXY_PORT", []portProto{portTCP}},
	{"LABMITM_WEB_PORT", []portProto{portTCP}},
	{"LABINFO_PORT", []portProto{portTCP}},
	{"LABGRAPH_PORT", []portProto{portTCP}},
	{"LABNTP_NTP_PORT", []portProto{portUDP}},
	{"LABNTP_REST_PORT", []portProto{portTCP}},
	{"LABSSO_HTTPS_PORT", []portProto{portTCP}},
	{"LABSSO_REST_PORT", []portProto{portTCP}},
	{"MCP_GATEWAY_PORT", []portProto{portTCP}},
	{"LABLDAP_LDAP_PORT", []portProto{portTCP}},
	{"LABLDAP_LDAPS_PORT", []portProto{portTCP}},
	{"LABLDAP_HTTPS_PORT", []portProto{portTCP}},
	{"TACLAB_LEGACY_PORT", []portProto{portTCP}},
	{"TACLAB_TLS_PORT", []portProto{portTCP}},
	{"TACLAB_HTTP_PORT", []portProto{portTCP}},
	{"TACLAB_RADIUS_ACCESS_PORT", []portProto{portUDP}},
	{"TACLAB_RADIUS_ACCT_PORT", []portProto{portUDP}},
	{"TACLAB_RADIUS_RADSEC_PORT", []portProto{portTCP}},
	{"TACLAB_RADIUS_DYNAUTH_PORT", []portProto{portUDP}},
}

func publishedPortBindings(p *profile.Profile) ([]portBinding, error) {
	seen := map[string]bool{}
	var out []portBinding
	for _, spec := range publishedPortSpecs {
		raw := strings.TrimSpace(p.Get(spec.EnvKey, ""))
		if raw == "" {
			continue
		}
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid %s=%q", spec.EnvKey, raw)
		}
		for _, proto := range spec.Protos {
			key := fmt.Sprintf("%d/%s", port, proto)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, portBinding{
				EnvKey: spec.EnvKey,
				Port:   port,
				Protos: []portProto{proto},
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Protos[0] < out[j].Protos[0]
	})
	return out, nil
}

// isPermissionDenied reports bind/listen failures that mean "this process
// cannot open the port", not "something else is already listening". Privileged
// ports (TacLab 49/300) return EACCES to an unprivileged orchestrator; dockerd
// can still publish them.
func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "permission denied")
}

func probePort(proto portProto, port int) error {
	err := bindProbe(proto, port)
	if err == nil {
		return nil
	}
	if !isPermissionDenied(err) {
		return err
	}
	// Unprivileged Listen on ports below ip_unprivileged_port_start
	// (1024 on GitHub-hosted runners) returns EACCES even when nothing is
	// bound. Dockerd still publishes those ports. Classify occupancy
	// without requiring the current uid to bind — do not skip TacLab 49/300.
	occupied, oerr := portOccupiedWithoutBind(proto, port)
	if oerr != nil {
		return oerr
	}
	if occupied {
		return fmt.Errorf("%s port %d is in use", proto, port)
	}
	return nil
}

func bindProbe(proto portProto, port int) error {
	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	switch proto {
	case portTCP:
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		return ln.Close()
	case portUDP:
		conn, err := net.ListenPacket("udp", addr)
		if err != nil {
			return err
		}
		return conn.Close()
	default:
		return fmt.Errorf("unsupported protocol %q", proto)
	}
}

var errNoProcNet = errors.New("no /proc/net tables")

func portOccupiedWithoutBind(proto portProto, port int) (bool, error) {
	occupied, err := linuxProcPortOccupied(proto, port)
	if err == nil {
		return occupied, nil
	}
	if !errors.Is(err, errNoProcNet) {
		return false, err
	}
	if proto == portTCP {
		return tcpDialOccupied(port)
	}
	// UDP and no /proc: cannot distinguish. Assume free so dockerd can bind.
	return false, nil
}

func linuxProcPortOccupied(proto portProto, port int) (bool, error) {
	var files []string
	listenOnly := false
	switch proto {
	case portTCP:
		files = []string{"/proc/net/tcp", "/proc/net/tcp6"}
		listenOnly = true
	case portUDP:
		files = []string{"/proc/net/udp", "/proc/net/udp6"}
	default:
		return false, fmt.Errorf("unsupported protocol %q", proto)
	}
	sawTable := false
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}
		sawTable = true
		if procNetHasBoundPort(string(b), port, listenOnly) {
			return true, nil
		}
	}
	if !sawTable {
		return false, errNoProcNet
	}
	return false, nil
}

// tcpListenState is /proc/net/tcp st=0A (LISTEN). UDP sockets use 07
// (CLOSE) when bound; occupancy is "any row with this local port".
const tcpListenState = "0A"

func procNetHasBoundPort(table string, port int, listenOnly bool) bool {
	want := fmt.Sprintf("%04X", port)
	for i, line := range strings.Split(table, "\n") {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		local := fields[1]
		colon := strings.LastIndex(local, ":")
		if colon < 0 {
			continue
		}
		if !strings.EqualFold(local[colon+1:], want) {
			continue
		}
		if listenOnly && !strings.EqualFold(fields[3], tcpListenState) {
			continue
		}
		return true
	}
	return false
}

func tcpDialOccupied(port int) (bool, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 250*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return false, nil
	}
	// Timeout or unexpected: fail closed so compose does not race a
	// maybe-listener. Listen already told us this uid cannot bind.
	return true, nil
}

var labContainerPrefixes = []string{"mcplab-", "labldap-", "labtacacs-"}

// labContainerExact are vendored compose container_name values that do not
// use the project-prefixed default (TacLab labgen: `taclab`).
var labContainerExact = []string{"taclab"}

func isLabContainer(name string) bool {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	for _, exact := range labContainerExact {
		if name == exact {
			return true
		}
	}
	for _, prefix := range labContainerPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (r *Runner) dockerContainersPublishingPort(port int) ([]string, error) {
	// HostConfig.PortBindings is the source of truth for TCP and UDP.
	// `docker ps` Ports (and --filter publish=) omit RADIUS UDP on
	// GH-hosted Engine, so Register-after-LabTacacsUp saw 1812/1813 as
	// non-lab listeners while 49/300/18049/2083 showed holder taclab.
	idsOut, err := r.capture(".", "docker", "ps", "-q")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(idsOut)
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"inspect", "--format", "{{.Name}}\t{{json .HostConfig.PortBindings}}"}, ids...)
	out, err := r.capture(".", "docker", args...)
	if err != nil {
		return nil, err
	}
	return inspectPortBindingHolders(out, port), nil
}

type hostPortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// inspectPortBindingHolders reads `docker inspect --format '{{.Name}}\t{{json .HostConfig.PortBindings}}'` lines.
func inspectPortBindingHolders(out string, port int) []string {
	want := strconv.Itoa(port)
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, raw, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if name == "" || raw == "" || raw == "null" {
			continue
		}
		var bindings map[string][]hostPortBinding
		if err := json.Unmarshal([]byte(raw), &bindings); err != nil {
			continue
		}
		for _, binds := range bindings {
			for _, b := range binds {
				if b.HostPort != want {
					continue
				}
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
	}
	return names
}

// publishedPortHolders returns container names whose Ports column maps
// host port (":N->" in `docker ps` form, TCP or UDP).
func publishedPortHolders(psOut string, port int) []string {
	needle := fmt.Sprintf(":%d->", port)
	var names []string
	for _, line := range strings.Split(psOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, ports, ok := strings.Cut(line, "\t")
		if !ok {
			name, ports, ok = strings.Cut(line, " ")
			if !ok {
				continue
			}
		}
		name = strings.TrimSpace(name)
		if name == "" || !strings.Contains(ports, needle) {
			continue
		}
		names = append(names, name)
	}
	return names
}

func (r *Runner) preflightPortsAvailable() error {
	bindings, err := publishedPortBindings(r.Prof)
	if err != nil {
		return err
	}
	var conflicts []string
	for _, b := range bindings {
		for _, proto := range b.Protos {
			if err := probePort(proto, b.Port); err == nil {
				continue
			}
			names, derr := r.dockerContainersPublishingPort(b.Port)
			if derr == nil && len(names) > 0 {
				allLab := true
				for _, name := range names {
					if !isLabContainer(name) {
						allLab = false
						break
					}
				}
				if allLab {
					continue
				}
			}
			msg := fmt.Sprintf("%s=%d (%s) already in use", b.EnvKey, b.Port, proto)
			if len(names) > 0 {
				msg += fmt.Sprintf(" (holders: %s)", strings.Join(names, ", "))
			} else if derr != nil {
				msg += " (non-lab listener; docker inspect unavailable)"
			} else {
				msg += " (non-lab listener)"
			}
			conflicts = append(conflicts, msg)
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	sort.Strings(conflicts)
	return fmt.Errorf("preflight failed: required host ports are not available:\n%s\n%s",
		strings.Join(conflicts, "\n"), occupiedPortHint(conflicts))
}

func occupiedPortHint(conflicts []string) string {
	var hints []string
	for _, c := range conflicts {
		if strings.Contains(c, "LABNTP_NTP_PORT=123") {
			hints = append(hints, "hint: stop/disable systemd-timesyncd (lab host clock may drift; LabNTP never settimeofday) or extra IP for LAB_PUBLIC_HOST or keep LABNTP_NTP_PORT=10123 (timesyncd/W32Time cannot follow a non-123 dest)")
		}
		if strings.Contains(c, "LABSSO_HTTPS_PORT=443") {
			hints = append(hints, "hint: stop/disable occupant (nginx/caddy/apache/another compose stack) or extra IP for LAB_PUBLIC_HOST or escape LABSSO_HTTPS_PORT (SUTs that hardcode dest 443 cannot follow)")
		}
	}
	if len(hints) == 0 {
		return "hint: stop the conflicting service or change the profile port"
	}
	return strings.Join(hints, "\n")
}
