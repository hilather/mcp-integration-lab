package lab

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hilather/mcp-integration-lab/internal/envfile"
	"github.com/hilather/mcp-integration-lab/internal/profile"
	"gopkg.in/yaml.v3"
)

var preflightKeys = []string{
	"LAB_PUBLIC_HOST",
	"MCP_GATEWAY_PORT",
	"LABDNS_DNS_PORT",
	"LABDNS_REST_PORT",
	"LABLDAP_HTTPS_PORT",
	"TACLAB_HTTP_PORT",
	"MAILDEV_WEB_PORT",
	"LABMITM_PROXY_PORT",
	"LABMITM_WEB_PORT",
	"LABINFO_PORT",
	"LAB_DEV_MODE",
	"MCPJUNGLE_MODE",
	"LAB_DOCKER_SUBNET",
	"LABJENKINS_ENABLED",
}

// Preflight fails fast when effective env values drift from profile.env for
// critical endpoint and mode keys, and when required host ports are not free.
func (r *Runner) Preflight() error {
	if err := r.preflightEnvDrift(); err != nil {
		return err
	}
	if _, err := sharedSubnet(r.Prof); err != nil {
		return fmt.Errorf("preflight failed: %w", err)
	}
	if r.LabJenkinsEnabled() {
		if err := r.applyLabJenkinsEnv(); err != nil {
			return err
		}
	}
	r.warnLabmitmOrigin()
	return r.preflightPortsAvailable()
}

func isLoopbackPublicHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	default:
		return false
	}
}

// warnLabmitmOrigin prints a stdout warning when a remote inspector Origin
// is missing from bootstrap originAllowlist. It never returns an error —
// make up must not fail-close on this.
func (r *Runner) warnLabmitmOrigin() {
	host := strings.TrimSpace(r.Prof.Get("LAB_PUBLIC_HOST", "localhost"))
	if isLoopbackPublicHost(host) {
		return
	}
	path := filepath.Join(r.Prof.Dir, "labmitm", "bootstrap.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc struct {
		Spec struct {
			Management struct {
				OriginAllowlist []string `yaml:"originAllowlist"`
			} `yaml:"management"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		fmt.Printf("warning: labmitm/bootstrap.yaml: %v\n", err)
		return
	}
	if len(doc.Spec.Management.OriginAllowlist) > 0 {
		return
	}
	webPort := r.Prof.Get("LABMITM_WEB_PORT", "18088")
	fmt.Printf("warning: LAB_PUBLIC_HOST=%s is not loopback; add http://%s:%s to\nlabmitm originAllowlist or the inspector SPA will 403 /v1\n",
		host, host, webPort)
}

// preflightEnvDrift catches stale .env/process overrides before slow bring-up
// or gateway registration runs.
func (r *Runner) preflightEnvDrift() error {
	if profile.IsTrue(r.Prof.Get("MCPLAB_ALLOW_PROFILE_OVERRIDES", "")) {
		return nil
	}
	profileEnv, err := envfile.ParseFile(filepath.Join(r.Prof.Dir, "profile.env"))
	if err != nil {
		return err
	}
	var drift []string
	for _, k := range preflightKeys {
		want := strings.TrimSpace(profileEnv[k])
		if want == "" {
			continue
		}
		got := strings.TrimSpace(r.Prof.Get(k, ""))
		if got == "" || got == want {
			continue
		}
		drift = append(drift, fmt.Sprintf("%s: profile.env=%q effective=%q", k, want, got))
	}
	sort.Strings(drift)
	if len(drift) == 0 {
		return nil
	}
	return fmt.Errorf("preflight failed: critical profile overrides detected:\n%s\nhint: remove conflicting values from .env/process env or set MCPLAB_ALLOW_PROFILE_OVERRIDES=true",
		strings.Join(drift, "\n"))
}
