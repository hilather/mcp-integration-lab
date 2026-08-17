// Package profile resolves the active lab profile and its effective
// environment. Precedence (lowest to highest): profiles/<name>/profile.env,
// repo .env, the parent process environment. The winner for PROFILE itself is
// process env, then .env, then "default".
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hilather/mcp-integration-lab/internal/envfile"
)

// IsTrue interprets boolean-ish env values ("1", "true", "yes", "on").
func IsTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

const DefaultName = "default"

// Profile is the resolved configuration environment for one lab profile.
type Profile struct {
	Name string
	// Dir is the absolute path of profiles/<name>.
	Dir string
	// Values is the effective KEY=VALUE set after precedence merging,
	// including derived MCPLAB_* keys consumed by the compose files.
	Values map[string]string
}

// Load resolves the active profile for a repo root. processEnv is typically
// os.Environ() parsed into a map; it has the highest precedence.
func Load(root string, processEnv map[string]string) (*Profile, error) {
	dotenv, err := envfile.ParseFile(filepath.Join(root, ".env"))
	if err != nil {
		return nil, err
	}

	name := DefaultName
	if v := dotenv["PROFILE"]; v != "" {
		name = v
	}
	if v := processEnv["PROFILE"]; v != "" {
		name = v
	}

	dir := filepath.Join(root, "profiles", name)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("profile %q not found at %s", name, dir)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	profEnv, err := envfile.ParseFile(filepath.Join(dir, "profile.env"))
	if err != nil {
		return nil, err
	}

	values := map[string]string{}
	for k, v := range profEnv {
		values[k] = v
	}
	for k, v := range dotenv {
		values[k] = v
	}
	for k, v := range processEnv {
		values[k] = v
	}
	values["PROFILE"] = name

	// Dev mode is the single security knob: unless a profile (or override)
	// pins MCPJUNGLE_MODE explicitly, dev mode opens the gateway (development
	// = no client auth) and non-dev hardens it (enterprise = client tokens +
	// ACLs). Dev mode also makes the labinfo service reveal web credentials.
	if values["LAB_DEV_MODE"] == "" {
		values["LAB_DEV_MODE"] = "false"
	}
	if values["MCPJUNGLE_MODE"] == "" {
		if IsTrue(values["LAB_DEV_MODE"]) {
			values["MCPJUNGLE_MODE"] = "development"
		} else {
			values["MCPJUNGLE_MODE"] = "enterprise"
		}
	}

	// Derived paths consumed by docker-compose.yaml and the LabLDAP overlay.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	ll := filepath.Join(absRoot, "third_party", "go-lab-ldap-mcp")
	values["MCPLAB_PROFILE_DIR"] = absDir
	values["MCPLAB_LDAP_TLS"] = filepath.Join(ll, "secrets", "tls")
	values["LABLDAP_SCENARIO_FILE"] = filepath.Join(absDir, "labldap", "scenario.yaml")
	values["LABLDAP_SECRETS_DIR"] = filepath.Join(ll, "secrets")
	values["LABLDAP_DM_PASSWORD_FILE"] = filepath.Join(ll, "secrets", "dm.pw")
	// Native engine serves the lab CA directory cert; bootstrap/clients trust
	// ca.crt. instance-ca.crt is a 389-only publish step we no longer run.
	values["LABLDAP_TLS_DIR"] = filepath.Join(ll, "secrets", "tls")
	values["LABLDAP_TLS_CA"] = filepath.Join(ll, "secrets", "tls", "ca.crt")

	return &Profile{Name: name, Dir: absDir, Values: values}, nil
}

// Get returns the effective value for key, or fallback when unset/empty.
func (p *Profile) Get(key, fallback string) string {
	if v := p.Values[key]; v != "" {
		return v
	}
	return fallback
}

// Environ merges the profile values over base (typically os.Environ()) into
// a slice suitable for exec.Cmd.Env. Deterministic order for testability.
func (p *Profile) Environ(base []string) []string {
	merged := map[string]string{}
	for _, kv := range base {
		if k, v, ok := cut(kv); ok {
			merged[k] = v
		}
	}
	for k, v := range p.Values {
		merged[k] = v
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+merged[k])
	}
	return out
}

func cut(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}
