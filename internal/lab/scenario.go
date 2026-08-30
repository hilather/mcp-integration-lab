package lab

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hilather/mcp-integration-lab/internal/labgraph"
)

const scenarioUsage = `usage: mcplab scenario validate|plan|apply|reset [name] [--appliances=a,b]
  name defaults to "default". --appliances is reset-only.
  Named fixture packs: broken-bind, expired-cert, split-horizon-dns,
  mitm-intercept-extra-port (also: mcplab fixture apply <id>).
  Talks to the running labgraph service (LAB_PUBLIC_HOST:LABGRAPH_PORT)
  with secrets/labgraph-token. Not a second appliance fan-out.`

const fixtureUsage = `usage: mcplab fixture apply <id>
  Closed ids: broken-bind, expired-cert, split-horizon-dns, mitm-intercept-extra-port.
  HTTP client of labgraph POST /v1/fixtures/{id}:apply (secrets/labgraph-token).
  Rejects default. Not a second appliance fan-out.`

// Scenario is the dedicated mcplab scenario parser. Do not share
// reload's exact-count check — validate|plan|apply take an optional name.
func (r *Runner) Scenario(args []string) error {
	op, name, appliances, err := parseScenarioArgs(args)
	if err != nil {
		return err
	}
	c, err := r.labgraphClient()
	if err != nil {
		return err
	}
	var res *labgraph.GraphResult
	switch op {
	case "validate":
		res, err = c.Validate(name)
	case "plan":
		res, err = c.Plan(name)
	case "apply":
		res, err = c.Apply(name)
	case "reset":
		res, err = c.Reset(name, appliances)
	default:
		return fmt.Errorf("internal: unknown scenario op %q", op)
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

func (r *Runner) labgraphClient() (*labgraph.Client, error) {
	host := strings.TrimSpace(r.Prof.Get("LAB_PUBLIC_HOST", "localhost"))
	port := strings.TrimSpace(r.Prof.Get("LABGRAPH_PORT", "18091"))
	base := "http://" + host + ":" + port
	return labgraph.NewClient(base, r.path("secrets/labgraph-token"))
}

// Fixture is mcplab fixture apply <id> — same labgraph token, fixture REST route.
func (r *Runner) Fixture(args []string) error {
	id, err := parseFixtureArgs(args)
	if err != nil {
		return err
	}
	c, err := r.labgraphClient()
	if err != nil {
		return err
	}
	res, err := c.ApplyFixture(id)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

func parseFixtureArgs(args []string) (id string, err error) {
	if len(args) == 0 {
		return "", fmt.Errorf("%s", fixtureUsage)
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	if op != "apply" {
		return "", fmt.Errorf("unknown fixture operation %q\n%s", args[0], fixtureUsage)
	}
	if len(args) != 2 {
		return "", fmt.Errorf("%s", fixtureUsage)
	}
	id = strings.TrimSpace(args[1])
	if !labgraph.IsFixture(id) {
		return "", fmt.Errorf("%s: not a fixture pack\n%s", id, fixtureUsage)
	}
	return id, nil
}

func parseScenarioArgs(args []string) (op, name string, appliances []string, err error) {
	if len(args) == 0 {
		return "", "", nil, fmt.Errorf("%s", scenarioUsage)
	}
	op = strings.ToLower(strings.TrimSpace(args[0]))
	switch op {
	case "validate", "plan", "apply", "reset":
	default:
		return "", "", nil, fmt.Errorf("unknown scenario operation %q\n%s", args[0], scenarioUsage)
	}
	name = "default"
	sawName := false
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "--appliances=") {
			if op != "reset" {
				return "", "", nil, fmt.Errorf("--appliances is valid only on scenario reset\n%s", scenarioUsage)
			}
			raw := strings.TrimPrefix(a, "--appliances=")
			for _, p := range strings.Split(raw, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					appliances = append(appliances, p)
				}
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			return "", "", nil, fmt.Errorf("unknown flag %s\n%s", a, scenarioUsage)
		}
		if sawName {
			return "", "", nil, fmt.Errorf("unexpected argument %q\n%s", a, scenarioUsage)
		}
		name = a
		sawName = true
	}
	return op, name, appliances, nil
}
