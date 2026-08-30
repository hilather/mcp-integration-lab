package lab

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/hilather/mcp-integration-lab/internal/profile"
)

const (
	sharedNetworkName        = "mcplab-shared"
	legacyDefaultNetworkName = "mcplab_default"
	defaultSharedSubnet      = "10.99.42.0/24"
)

type netAction int

const (
	netNop netAction = iota
	netCreate
	netRecreate
	netConflict
)

func sharedSubnet(p *profile.Profile) (string, error) {
	raw := defaultSharedSubnet
	if p != nil {
		if v := strings.TrimSpace(p.Get("LAB_DOCKER_SUBNET", "")); v != "" {
			raw = v
		}
	}
	return parseIPv4LabSubnet(raw)
}

// parseIPv4LabSubnet accepts IPv4 /24–/27.
func parseIPv4LabSubnet(raw string) (string, error) {
	ip, n, err := net.ParseCIDR(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid LAB_DOCKER_SUBNET=%q: %w", raw, err)
	}
	if ip.To4() == nil {
		return "", fmt.Errorf("LAB_DOCKER_SUBNET=%q must be IPv4", raw)
	}
	ones, bits := n.Mask.Size()
	if bits != 32 || ones < 24 || ones > 27 {
		return "", fmt.Errorf("LAB_DOCKER_SUBNET=%q: IPv4 prefix must be /24–/27 (default %s)", raw, defaultSharedSubnet)
	}
	return n.String(), nil
}

func normalizeCIDR(s string) string {
	_, n, err := net.ParseCIDR(strings.TrimSpace(s))
	if err != nil {
		return strings.TrimSpace(s)
	}
	return n.String()
}

func sharedNetworkCreateArgs(subnet string) []string {
	return []string{"network", "create", "--driver", "bridge", "--subnet", subnet, sharedNetworkName}
}

func decideSharedNetworkAction(exists bool, currentSubnet string, containers int, want string) netAction {
	if !exists {
		return netCreate
	}
	if normalizeCIDR(currentSubnet) == want {
		return netNop
	}
	if containers == 0 {
		return netRecreate
	}
	return netConflict
}

func parseNetworkInspect(out string) (subnet string, containers int, err error) {
	i := strings.Index(out, "{")
	if i < 0 {
		return "", 0, fmt.Errorf("no JSON in network inspect")
	}
	var n struct {
		IPAM struct {
			Config []struct {
				Subnet string `json:"Subnet"`
			} `json:"Config"`
		} `json:"IPAM"`
		Containers map[string]json.RawMessage `json:"Containers"`
	}
	if err := json.Unmarshal([]byte(out[i:]), &n); err != nil {
		return "", 0, err
	}
	for _, c := range n.IPAM.Config {
		ip, _, perr := net.ParseCIDR(c.Subnet)
		if perr == nil && ip.To4() != nil {
			subnet = c.Subnet
			break
		}
	}
	return subnet, len(n.Containers), nil
}

func isNoSuchNetwork(out string, err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(out + " " + err.Error())
	return strings.Contains(s, "no such network")
}

func (r *Runner) inspectSharedNetwork() (exists bool, subnet string, containers int, err error) {
	out, err := r.capture(".", "docker", "network", "inspect", "-f", "{{json .}}", sharedNetworkName)
	if err != nil {
		if isNoSuchNetwork(out, err) {
			return false, "", 0, nil
		}
		return false, "", 0, err
	}
	subnet, containers, err = parseNetworkInspect(out)
	if err != nil {
		return false, "", 0, err
	}
	return true, subnet, containers, nil
}

// EnsureNetwork creates mcplab-shared with LAB_DOCKER_SUBNET (default /24).
// An empty leftover /16 is replaced. Endpoints on the wrong subnet fail
// closed — `make down` then `make up`. Also drops unused mcplab_default
// (the old compose /16).
func (r *Runner) EnsureNetwork() error {
	want, err := sharedSubnet(r.Prof)
	if err != nil {
		return err
	}
	exists, subnet, ncont, err := r.inspectSharedNetwork()
	if err != nil {
		return err
	}
	switch decideSharedNetworkAction(exists, subnet, ncont, want) {
	case netNop:
	case netCreate:
		if err := r.run(".", "docker", sharedNetworkCreateArgs(want)...); err != nil {
			return err
		}
	case netRecreate:
		fmt.Printf("network: replacing %s (%s) with %s\n", sharedNetworkName, subnet, want)
		if err := r.run(".", "docker", "network", "rm", sharedNetworkName); err != nil {
			return err
		}
		if err := r.run(".", "docker", sharedNetworkCreateArgs(want)...); err != nil {
			return err
		}
	case netConflict:
		return fmt.Errorf("%s is %s with %d endpoint(s); want %s. run `make down` then `make up`",
			sharedNetworkName, subnet, ncont, want)
	default:
		return fmt.Errorf("internal: unknown network action")
	}
	r.releaseUnusedNetwork(legacyDefaultNetworkName)
	return nil
}

func (r *Runner) releaseUnusedNetworks() {
	r.releaseUnusedNetwork(sharedNetworkName)
	r.releaseUnusedNetwork(legacyDefaultNetworkName)
}

func (r *Runner) releaseUnusedNetwork(name string) {
	out, err := r.capture(".", "docker", "network", "inspect", "-f", "{{json .}}", name)
	if err != nil {
		return
	}
	_, n, err := parseNetworkInspect(out)
	if err != nil || n > 0 {
		return
	}
	_ = r.run(".", "docker", "network", "rm", name)
}
