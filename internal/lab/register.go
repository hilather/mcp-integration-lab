package lab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Register applies the active profile's MCPJungle configuration to the
// running gateway. Idempotent: deregister/recreate is the supported "update"
// path upstream, and gateway state is ephemeral by design (tmpfs SQLite).
func (r *Runner) Register() error {
	if err := r.Preflight(); err != nil {
		return err
	}
	port := r.Prof.Get("MCP_GATEWAY_PORT", "8080")
	mode := r.Prof.Get("MCPJUNGLE_MODE", "development")

	tokens, err := r.loadTokens()
	if err != nil {
		return err
	}
	regEnv := r.registrarEnv(tokens)

	servers, err := serverNames(filepath.Join(r.Prof.Dir, "mcpjungle", "servers"))
	if err != nil {
		return err
	}

	fmt.Println("waiting for gateway health...")
	if err := waitHealthy("http://127.0.0.1:"+port+"/health", 60*time.Second); err != nil {
		return err
	}

	reg := func(args ...string) error {
		full := append([]string{"compose", "run", "--rm", "-T", "--no-deps", "--quiet-pull",
			"registrar", "--registry", "http://mcpjungle:8080"}, args...)
		return r.runWithEnv(regEnv, "docker", full...)
	}

	if mode == "enterprise" {
		// Creates the admin user; its token lands in the mounted CLI home.
		if err := reg("init-server"); err != nil {
			fmt.Println("init-server: already initialized (ok)")
		}
	}

	for _, server := range servers {
		_ = reg("deregister", server) // tolerated: may not exist yet
		if err := reg("register", "-c", "/config/servers/"+server+".json"); err != nil {
			return err
		}
	}

	_ = reg("delete", "group", "integration") // tolerated: may not exist yet
	if err := reg("create", "group", "-c", "/config/groups/integration.json"); err != nil {
		return err
	}

	if mode == "enterprise" {
		// Recreate so the allow-list always matches the discovered servers.
		_ = reg("delete", "mcp-client", "integration-client")
		if err := reg("create", "mcp-client", "integration-client",
			"--allow", strings.Join(servers, ", "),
			"--access-token", tokens["client"]); err != nil {
			return err
		}
		fmt.Println("\nenterprise mode: clients must send 'Authorization: Bearer $(cat secrets/mcp-client-token)'")
	}

	fmt.Printf("\ngateway ready: http://<host>:%s/mcp\n", port)
	fmt.Printf("tool group:    http://<host>:%s/v0/groups/integration/mcp\n", port)
	return nil
}

func (r *Runner) loadTokens() (map[string]string, error) {
	read := func(rel string) (string, error) {
		b, err := os.ReadFile(r.path(rel))
		if err != nil {
			return "", fmt.Errorf("missing secret (run `mcplab secrets` first): %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	out := map[string]string{}
	var err error
	if out["labdns"], err = read("secrets/labdns-token"); err != nil {
		return nil, err
	}
	if out["labldap"], err = read("third_party/go-lab-ldap-mcp/secrets/token-admin"); err != nil {
		return nil, err
	}
	if out["labtacacs"], err = read(taclabDir + "/deployments/compose/secrets/api_admin_token"); err != nil {
		return nil, err
	}
	if out["labinfo"], err = read("secrets/labinfo-token"); err != nil {
		return nil, err
	}
	if out["labmail"], err = read("secrets/labmail-token"); err != nil {
		return nil, err
	}
	if out["labmitm"], err = read("secrets/labmitm-token"); err != nil {
		return nil, err
	}
	if out["client"], err = read("secrets/mcp-client-token"); err != nil {
		return nil, err
	}
	return out, nil
}

// registrarEnv is the process environment for `docker compose run registrar`:
// compose interpolates ${LAB*_TOKEN} into the server JSON files.
func (r *Runner) registrarEnv(tokens map[string]string) []string {
	return append(append([]string{}, r.Env...),
		"LABDNS_TOKEN="+tokens["labdns"],
		"LABLDAP_TOKEN="+tokens["labldap"],
		"LABTACACS_TOKEN="+tokens["labtacacs"],
		"LABINFO_TOKEN="+tokens["labinfo"],
		"LABMAIL_TOKEN="+tokens["labmail"],
		"LABMITM_TOKEN="+tokens["labmitm"],
		"MCP_CLIENT_TOKEN="+tokens["client"],
	)
}

// serverNames discovers the MCP servers a profile registers by parsing the
// "name" field of every JSON file in its mcpjungle/servers directory. The
// same list drives registration and the enterprise client allow-list, so
// adding a server to a profile is a single-file change.
func serverNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("profile servers dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var cfg struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if cfg.Name == "" {
			return nil, fmt.Errorf("%s: missing \"name\"", e.Name())
		}
		// Registration references /config/servers/<name>.json inside the
		// registrar container, so the filename must match the server name.
		if base := strings.TrimSuffix(e.Name(), ".json"); base != cfg.Name {
			return nil, fmt.Errorf("%s: filename must match server name %q", e.Name(), cfg.Name)
		}
		names = append(names, cfg.Name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no server configs in %s", dir)
	}
	sort.Strings(names)
	return names, nil
}

func waitHealthy(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("gateway not healthy at %s after %s", url, timeout)
		}
		time.Sleep(time.Second)
	}
}
