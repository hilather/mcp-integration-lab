// Package maildev validates a profile's LabMail bootstrap YAML. The lab's
// mail server is a receive-only sink: leftover maildev flag files, relay /
// outbound keys, and implicit SMTPS are rejected here before a container
// starts. LabMail itself also fail-closes on those keys; this is the
// orchestrator's copy of that guard so a bad profile never reaches compose.
package maildev

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	tokenSecretFile    = "/run/secrets/labmail-token"
	passwordSecretFile = "/run/secrets/maildev-web-password"
	basicUsername      = "admin"
)

// reservedKeys are LabMail's reserved outbound/relay names after
// normalizeKey (strip dashes/underscores/case). Matching the appliance's
// fail-closed list so a profile cannot smuggle a relay through YAML.
var reservedKeys = map[string]string{
	"outgoing":       "configures an outgoing SMTP relay",
	"outgoinghost":   "configures an outgoing SMTP relay",
	"outgoingport":   "configures an outgoing SMTP relay",
	"outgoinguser":   "configures an outgoing SMTP relay",
	"outgoingpass":   "configures an outgoing SMTP relay",
	"outgoingsecure": "configures an outgoing SMTP relay",
	"autorelay":      "auto-forwards received mail outward",
	"autorelayrules": "auto-forwards received mail outward",
	"relay":          "configures outbound delivery",
	"smarthost":      "configures outbound delivery",
	"forwardto":      "configures outbound delivery",
	"mx":             "configures outbound delivery",
	"deliver":        "configures outbound delivery",
	"environment":    "inline env secrets are rejected; use secretFile references",
	"flags":          "maildev flag bag; use labmail.dev/v1alpha1 (profiles/<name>/labmail/bootstrap.yaml)",
	"incomingsecure": "implicit SMTPS is 1.1; do not silently downgrade to STARTTLS",
	"incomingcert":   "implicit SMTPS is 1.1; do not silently downgrade to STARTTLS",
	"incomingkey":    "implicit SMTPS is 1.1; do not silently downgrade to STARTTLS",
	"maildirectory":  "captured mail is ephemeral; durable mail-directory is rejected",
	"basepathname":   "non-goal for this lab overlay",
}

type bootstrap struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Spec       struct {
		SMTP struct {
			TLS struct {
				Mode string `yaml:"mode"`
			} `yaml:"tls"`
		} `yaml:"smtp"`
		Management struct {
			Auth struct {
				Tokens []struct {
					SecretFile string `yaml:"secretFile"`
				} `yaml:"tokens"`
				Basic struct {
					Username     string `yaml:"username"`
					PasswordFile string `yaml:"passwordFile"`
				} `yaml:"basic"`
			} `yaml:"auth"`
			MCP struct {
				AllowLegacyClients bool `yaml:"allowLegacyClients"`
			} `yaml:"mcp"`
		} `yaml:"management"`
	} `yaml:"spec"`
}

// ValidateProfile fail-closes on a leftover maildev/maildev.yaml and on a
// LabMail bootstrap that would relay, skip MCPJungle compatibility, or
// point secrets at the wrong bind-mounts.
func ValidateProfile(profileDir string) error {
	legacy := filepath.Join(profileDir, "maildev", "maildev.yaml")
	if _, err := os.Stat(legacy); err == nil {
		return fmt.Errorf("%s: replaced by labmail/bootstrap.yaml — remove the maildev flag file; the Node maildev image is gone", legacy)
	}
	return ValidateBootstrap(filepath.Join(profileDir, "labmail", "bootstrap.yaml"))
}

// ValidateBootstrap checks one LabMail YAML file.
func ValidateBootstrap(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: %w (LabMail desired state lives at profiles/<name>/labmail/bootstrap.yaml)", path, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := rejectReserved(&root, path); err != nil {
		return err
	}

	var cfg bootstrap
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if cfg.APIVersion != "labmail.dev/v1alpha1" {
		return fmt.Errorf("%s: apiVersion %q, want labmail.dev/v1alpha1", path, cfg.APIVersion)
	}
	if cfg.Kind != "LabMail" {
		return fmt.Errorf("%s: kind %q, want LabMail", path, cfg.Kind)
	}
	if !cfg.Spec.Management.MCP.AllowLegacyClients {
		return fmt.Errorf("%s: spec.management.mcp.allowLegacyClients must be true so MCPJungle can register", path)
	}
	if cfg.Spec.Management.Auth.Basic.Username != basicUsername {
		return fmt.Errorf("%s: spec.management.auth.basic.username %q is frozen at %q (LabMail YAML does not interpolate MAILDEV_WEB_USER)", path, cfg.Spec.Management.Auth.Basic.Username, basicUsername)
	}
	if cfg.Spec.Management.Auth.Basic.PasswordFile != passwordSecretFile {
		return fmt.Errorf("%s: spec.management.auth.basic.passwordFile %q, want %s (bind-mounted by compose)", path, cfg.Spec.Management.Auth.Basic.PasswordFile, passwordSecretFile)
	}
	foundToken := false
	for _, tok := range cfg.Spec.Management.Auth.Tokens {
		if tok.SecretFile == tokenSecretFile {
			foundToken = true
			break
		}
	}
	if !foundToken {
		return fmt.Errorf("%s: spec.management.auth.tokens must include secretFile %s (bind-mounted by compose)", path, tokenSecretFile)
	}
	if mode := cfg.Spec.SMTP.TLS.Mode; mode == "implicit" {
		return fmt.Errorf("%s: smtp.tls.mode: implicit SMTPS is 1.1; do not silently downgrade to STARTTLS", path)
	}
	return nil
}

func rejectReserved(n *yaml.Node, path string) error {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			if err := rejectReserved(c, path); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			norm := normalizeKey(key)
			if why, ok := reservedKeys[norm]; ok {
				return fmt.Errorf("%s: key %q %s — this lab's mail server is receive-only", path, key, why)
			}
			if err := rejectReserved(n.Content[i+1], path); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeKey(s string) string {
	s = strings.ToLower(strings.TrimLeft(s, "-"))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '-' || r == '_' || unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
