package lab

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/profile"
)

func TestParseIPv4LabSubnet(t *testing.T) {
	got, err := parseIPv4LabSubnet("10.99.42.0/24")
	if err != nil || got != "10.99.42.0/24" {
		t.Fatalf("default-shaped /24: got %q err=%v", got, err)
	}
	if _, err := parseIPv4LabSubnet("10.99.42.0/16"); err == nil || !strings.Contains(err.Error(), "/16") {
		t.Fatalf("want /16 rejected, got %v", err)
	}
	if _, err := parseIPv4LabSubnet("fd00::/64"); err == nil {
		t.Fatal("IPv6 must be rejected")
	}
	if _, err := parseIPv4LabSubnet("10.99.42.0/28"); err == nil {
		t.Fatal("/28 is too small for the lab")
	}
	got, err = parseIPv4LabSubnet("10.99.42.0/27")
	if err != nil || got != "10.99.42.0/27" {
		t.Fatalf("/27: got %q err=%v", got, err)
	}
	if _, err := parseIPv4LabSubnet("not-a-cidr"); err == nil {
		t.Fatal("want parse error")
	}
}

func TestSharedSubnetDefaultAndOverride(t *testing.T) {
	got, err := sharedSubnet(nil)
	if err != nil || got != defaultSharedSubnet {
		t.Fatalf("nil profile: got %q err=%v, want %s", got, err, defaultSharedSubnet)
	}
	got, err = sharedSubnet(&profile.Profile{Values: map[string]string{}})
	if err != nil || got != defaultSharedSubnet {
		t.Fatalf("empty values: got %q err=%v", got, err)
	}
	got, err = sharedSubnet(&profile.Profile{Values: map[string]string{"LAB_DOCKER_SUBNET": "10.88.0.0/24"}})
	if err != nil || got != "10.88.0.0/24" {
		t.Fatalf("override: got %q err=%v", got, err)
	}
}

func TestSharedNetworkCreateArgsPinsSubnet(t *testing.T) {
	args := sharedNetworkCreateArgs("10.99.42.0/24")
	want := []string{"network", "create", "--driver", "bridge", "--subnet", "10.99.42.0/24", sharedNetworkName}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("args=%v want %v", args, want)
	}
}

func TestDecideSharedNetworkAction(t *testing.T) {
	want := "10.99.42.0/24"
	if got := decideSharedNetworkAction(false, "", 0, want); got != netCreate {
		t.Fatalf("missing: %v", got)
	}
	if got := decideSharedNetworkAction(true, "10.99.42.0/24", 3, want); got != netNop {
		t.Fatalf("matching: %v", got)
	}
	if got := decideSharedNetworkAction(true, "172.18.0.0/16", 0, want); got != netRecreate {
		t.Fatalf("empty leftover /16: %v", got)
	}
	if got := decideSharedNetworkAction(true, "172.18.0.0/16", 2, want); got != netConflict {
		t.Fatalf("attached leftover /16: %v", got)
	}
}

func TestParseNetworkInspect(t *testing.T) {
	const body = `{"Name":"mcplab-shared","IPAM":{"Config":[{"Subnet":"172.18.0.0/16","Gateway":"172.18.0.1"}]},"Containers":{"abc":{"Name":"taclab"},"def":{"Name":"mcpjungle"}}}`
	subnet, n, err := parseNetworkInspect("WARNING: foo\n" + body)
	if err != nil {
		t.Fatal(err)
	}
	if subnet != "172.18.0.0/16" || n != 2 {
		t.Fatalf("subnet=%q n=%d", subnet, n)
	}
}

func TestIsNoSuchNetwork(t *testing.T) {
	err := fmt.Errorf("docker network inspect: exit 1")
	if !isNoSuchNetwork("Error: No such network: mcplab-shared\n", err) {
		t.Fatal("missing network must be detectable")
	}
	if isNoSuchNetwork("permission denied", err) {
		t.Fatal("other inspect errors must not look missing")
	}
}
