package lab

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/hilather/mcp-integration-lab/internal/taclabcfg"
)

// Secrets generates all lab secrets. Idempotent: existing files are kept.
//
//	secrets/labdns-token       bearer token the gateway presents to LabDNS
//	secrets/mcp-client-token   client token for MCPJungle enterprise mode
//	third_party/go-lab-ldap-mcp/secrets/...   minted by LabLDAP's own tools
//	third_party/go-lab-ldap-mcp/secrets/tls/  lab CA + directory & management certs
func (r *Runner) Secrets() error {
	if err := os.MkdirAll(r.path("secrets"), 0o755); err != nil {
		return err
	}
	// Containers run as uid 65532; the LabDNS token file must be readable
	// there. Lab-grade tradeoff, documented.
	if err := writeTokenIfMissing(r.path("secrets/labdns-token"), 0o644); err != nil {
		return err
	}
	if err := writeTokenIfMissing(r.path("secrets/mcp-client-token"), 0o600); err != nil {
		return err
	}
	// Inbound bearer for the labinfo service (gateway -> labinfo).
	if err := writeTokenIfMissing(r.path("secrets/labinfo-token"), 0o644); err != nil {
		return err
	}
	// The mcpjungle CLI stores its (enterprise) admin config here.
	if err := os.MkdirAll(r.path("secrets/mcpjungle-home"), 0o755); err != nil {
		return err
	}

	// Web UI basic-auth password for the receive-only maildev sink; injected
	// as MAILDEV_WEB_PASS at compose time, never written into the profile.
	if err := writeTokenIfMissing(r.path("secrets/maildev-web-password"), 0o600); err != nil {
		return err
	}

	// LabLDAP secrets + lab CA. The --management cert is what lets the gateway
	// verify the control plane's TLS. Native directory TLS is the same lab CA
	// (`directory` cert); there is no 389 instance-CA publish step.
	ll := "third_party/go-lab-ldap-mcp"
	if err := r.run(ll, "go", "run", "./tools/setupsecrets", "--dir", "secrets"); err != nil {
		return err
	}
	if err := r.run(ll, "go", "run", "./tools/setuptls", "generate",
		"--dir", "secrets/tls", "--host", "directory", "--management"); err != nil {
		return err
	}

	if err := r.ensureTaclabLab(); err != nil {
		return err
	}

	if err := r.stageLabinfoCreds(); err != nil {
		return err
	}
	fmt.Println("secrets ready")
	return nil
}

const taclabLabgenMarker = "deployments/compose/.mcplab-labgen-ref"

// ensureTaclabLab materializes TacLab's labgen bundle (configs, PKI, secrets)
// and turns on api.mcp.allow_legacy_clients so MCPJungle can connect. labgen
// is rerun with -force when the vendored checkout moves to a new tag, so a
// pin bump cannot leave a stale baseline behind.
func (r *Runner) ensureTaclabLab() error {
	ref, err := r.taclabVendorRef()
	if err != nil {
		return err
	}
	marker := r.path(taclabDir + "/" + taclabLabgenMarker)
	prev, _ := os.ReadFile(marker)
	need := strings.TrimSpace(string(prev)) != ref
	if _, err := os.Stat(r.path(taclabDir + "/deployments/compose/secrets/api_admin_token")); err != nil {
		need = true
	}
	if need {
		args := []string{"run", "./tools/labgen", "deployments/compose"}
		if _, err := os.Stat(r.path(taclabDir + "/deployments/compose/secrets/api_admin_token")); err == nil {
			args = []string{"run", "./tools/labgen", "-force", "deployments/compose"}
		}
		if err := r.run(taclabDir, "go", args...); err != nil {
			return err
		}
		if err := os.WriteFile(marker, []byte(ref+"\n"), 0o644); err != nil {
			return err
		}
	}
	if err := taclabcfg.EnableLegacyClientsDir(r.path(taclabDir + "/deployments/compose/config")); err != nil {
		return fmt.Errorf("enable TacLab MCP legacy clients: %w", err)
	}
	return nil
}

func (r *Runner) taclabVendorRef() (string, error) {
	out, err := r.capture(".", "git", "-C", r.path(taclabDir), "describe", "--tags", "--always")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// stageLabinfoCreds copies the credentials the labinfo service may reveal
// (dev mode only) into secrets/labinfo-creds/, world-readable so the
// unprivileged container (uid 65532) can read them: web/REST surface tokens
// plus the connection credentials served by connections_list (LDAP bind
// password, RADIUS shared secret). Lab-grade tradeoff: these are static lab
// secrets, the directory is gitignored, and exposure via the MCP tools is
// still gated on LAB_DEV_MODE.
func (r *Runner) stageLabinfoCreds() error {
	dir := r.path("secrets/labinfo-creds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for src, dst := range map[string]string{
		"secrets/labinfo-token":                                               "labinfo-token",
		"secrets/labdns-token":                                                "labdns-token",
		"secrets/mcp-client-token":                                            "mcp-client-token",
		"secrets/maildev-web-password":                                        "maildev-web-password",
		"third_party/go-lab-ldap-mcp/secrets/token-admin":                     "labldap-token-admin",
		"third_party/go-lab-ldap-mcp/secrets/user-alice":                      "labldap-user-alice",
		taclabDir + "/deployments/compose/secrets/api_admin_token":            "labtacacs-token-admin",
		taclabDir + "/deployments/compose/secrets/lab_switches_radius_secret": "labtacacs-radius-secret",
	} {
		b, err := os.ReadFile(r.path(src))
		if err != nil {
			return fmt.Errorf("stage labinfo creds: %w", err)
		}
		if err := os.WriteFile(dir+"/"+dst, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeTokenIfMissing(path string, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return os.Chmod(path, mode)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(buf)+"\n"), mode); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}
