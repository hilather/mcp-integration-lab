package lab

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hilather/mcp-integration-lab/internal/envfile"
	"github.com/hilather/mcp-integration-lab/internal/profile"
)

var preflightKeys = []string{
	"LAB_PUBLIC_HOST",
	"MCP_GATEWAY_PORT",
	"LABDNS_DNS_PORT",
	"LABDNS_REST_PORT",
	"LABLDAP_HTTPS_PORT",
	"TACLAB_HTTP_PORT",
	"MAILDEV_WEB_PORT",
	"LABINFO_PORT",
	"LAB_DEV_MODE",
	"MCPJUNGLE_MODE",
}

// Preflight fails fast when effective env values drift from profile.env for
// critical endpoint and mode keys, and when required host ports are not free.
func (r *Runner) Preflight() error {
	if err := r.preflightEnvDrift(); err != nil {
		return err
	}
	return r.preflightPortsAvailable()
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
