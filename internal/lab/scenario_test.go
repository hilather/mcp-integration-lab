package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilather/mcp-integration-lab/internal/labgraph"
	"github.com/hilather/mcp-integration-lab/internal/profile"
)

func TestParseScenarioArgs(t *testing.T) {
	op, name, apps, err := parseScenarioArgs([]string{"validate"})
	if err != nil || op != "validate" || name != "default" || len(apps) != 0 {
		t.Fatalf("validate default: op=%s name=%s apps=%v err=%v", op, name, apps, err)
	}
	op, name, apps, err = parseScenarioArgs([]string{"plan", "broken-bind"})
	if err != nil || op != "plan" || name != "broken-bind" || len(apps) != 0 {
		t.Fatalf("plan named: op=%s name=%s apps=%v err=%v", op, name, apps, err)
	}
	op, name, apps, err = parseScenarioArgs([]string{"reset", "default", "--appliances=labdns,labmitm"})
	if err != nil || op != "reset" || name != "default" || strings.Join(apps, ",") != "labdns,labmitm" {
		t.Fatalf("reset appliances: op=%s name=%s apps=%v err=%v", op, name, apps, err)
	}
	if _, _, _, err := parseScenarioArgs([]string{"validate", "a", "b"}); err == nil {
		t.Fatal("extra positional must fail (do not copy reload's exact-count check)")
	}
	if _, _, _, err := parseScenarioArgs([]string{"apply", "--appliances=labdns"}); err == nil {
		t.Fatal("--appliances on apply must fail")
	}
	if _, _, _, err := parseScenarioArgs([]string{"nope"}); err == nil {
		t.Fatal("unknown op must fail")
	}
	if _, _, _, err := parseScenarioArgs(nil); err == nil {
		t.Fatal("empty args must fail")
	}
}

func TestParseFixtureArgs(t *testing.T) {
	id, err := parseFixtureArgs([]string{"apply", "broken-bind"})
	if err != nil || id != "broken-bind" {
		t.Fatalf("got id=%q err=%v", id, err)
	}
	if _, err := parseFixtureArgs([]string{"apply", "default"}); err == nil {
		t.Fatal("default must be rejected")
	}
	if _, err := parseFixtureArgs([]string{"apply"}); err == nil {
		t.Fatal("missing id must fail")
	}
	if _, err := parseFixtureArgs([]string{"list"}); err == nil {
		t.Fatal("list is not a fixture op")
	}
	if _, err := parseFixtureArgs([]string{"apply", "broken-bind", "--appliances=labldap"}); err == nil {
		t.Fatal("--appliances on fixture apply must fail")
	}
}

func TestLabgraphClientRequiresTokenFile(t *testing.T) {
	r := &Runner{
		Root: t.TempDir(),
		Prof: &profile.Profile{Values: map[string]string{
			"LAB_PUBLIC_HOST": "127.0.0.1",
			"LABGRAPH_PORT":   "18091",
		}},
	}
	_, err := r.labgraphClient()
	if err == nil || !strings.Contains(err.Error(), "labgraph token") {
		t.Fatalf("missing token must fail closed, got %v", err)
	}
}

func TestLabgraphClientSendsBearer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secrets", "labgraph-token"), []byte("cli-tok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Runner{
		Root: root,
		Prof: &profile.Profile{Values: map[string]string{
			"LAB_PUBLIC_HOST": "lab.example.com",
			"LABGRAPH_PORT":   "18091",
		}},
	}
	c, err := r.labgraphClient()
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "cli-tok" {
		t.Fatalf("token = %q, want cli-tok", c.Token)
	}
	if c.Base != "http://lab.example.com:18091" {
		t.Fatalf("Base = %q", c.Base)
	}
	_ = labgraph.Client{}
}
