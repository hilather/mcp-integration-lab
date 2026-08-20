package lab

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

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
	{"LABINFO_PORT", []portProto{portTCP}},
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

func probePort(proto portProto, port int) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
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

var labContainerPrefixes = []string{"mcplab-", "labldap-", "labtacacs-"}

func isLabContainer(name string) bool {
	for _, prefix := range labContainerPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (r *Runner) dockerContainersPublishingPort(port int) ([]string, error) {
	out, err := r.capture(".", "docker", "ps",
		"--filter", fmt.Sprintf("publish=%d", port),
		"--format", "{{.Names}}")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
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
	return fmt.Errorf("preflight failed: required host ports are not available:\n%s\nhint: stop the conflicting service or change the profile port",
		strings.Join(conflicts, "\n"))
}
