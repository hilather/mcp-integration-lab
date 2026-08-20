package lab

import (
	"fmt"
	"strings"
)

// LabLDAPUp brings up the LabLDAP stack (separate compose project `labldap`)
// on the native Go engine (`labldapd`), mirroring upstream
// `make compose-up` (native is the default as of v0.3.0) with this repo's
// overlay (shared network, management TLS staging, external ports) and the
// active profile's scenario. Idempotent; use Reload("labldap") to
// force-recreate directory + control after a scenario edit.
func (r *Runner) LabLDAPUp() error {
	if err := r.EnsureNetwork(); err != nil {
		return err
	}

	ll := "third_party/go-lab-ldap-mcp"
	// Native engine image first, then bootstrap/control. Bootstrap still
	// applies the scenario over LDAPS; control is unchanged.
	if err := r.run(ll, "make", "image-native", "image-bootstrap", "image"); err != nil {
		return err
	}

	// Switching from 389 DS is a re-bootstrap, not a live change. Leftover
	// 389 /data (uid 389 tmpfs) fail-closes labldapd, so wipe when we see it.
	if r.labldapNeeds389Wipe() {
		fmt.Println("labldap: dropping leftover 389 DS volume (engine is native)")
		if err := r.LabLDAPDown(true); err != nil {
			return err
		}
	}

	// 1. Stage DM password + lab-CA directory cert/key for labldapd.
	if err := r.labldapOneShot("native-secret-prep"); err != nil {
		return err
	}
	// 2. Native directory (self-applies the engine plan; TLS is the lab CA).
	if err := r.labldapCompose("up", "-d", "--wait", "--remove-orphans", "directory"); err != nil {
		return err
	}
	// 3. Stage control-plane secrets (incl. management cert/key).
	if err := r.labldapOneShot("secret-prep"); err != nil {
		return err
	}
	// 4. Bootstrap the suffix over LDAPS, then start the control plane.
	if err := r.labldapCompose("up", "-d", "--wait", "--remove-orphans", "--force-recreate", "control"); err != nil {
		return err
	}

	fmt.Printf("labldap up (native): UI/REST/MCP on https://<host>:%s (token: third_party/go-lab-ldap-mcp/secrets/token-admin)\n",
		r.Prof.Get("LABLDAP_HTTPS_PORT", "8443"))
	return nil
}

// LabLDAPDown tears the LabLDAP project down; wipe also removes volumes.
func (r *Runner) LabLDAPDown(wipe bool) error {
	args := []string{"down", "--remove-orphans"}
	if wipe {
		args = append(args, "-v")
	}
	return r.labldapCompose(args...)
}

// labldapOneShot runs a restart: "no" prep container to completion.
// `compose wait` races when the copy finishes before wait attaches
// ("no containers for project"), so we stay in the foreground and
// take the container's exit code instead.
func (r *Runner) labldapOneShot(service string) error {
	return r.labldapCompose(labldapOneShotArgs(service)...)
}

func labldapOneShotArgs(service string) []string {
	return []string{
		"up", "--no-deps", "--force-recreate",
		"--abort-on-container-exit", "--exit-code-from", service, service,
	}
}

func (r *Runner) labldapNeeds389Wipe() bool {
	if r.labldapDirectoryIs389() {
		return true
	}
	opts, err := r.capture(".", "docker", "volume", "inspect", "-f", `{{index .Options "o"}}`, "labldap_directory-data")
	if err != nil {
		return false
	}
	return strings.Contains(opts, "uid=389")
}

func (r *Runner) labldapDirectoryIs389() bool {
	id, err := r.capture(".", "docker", r.labldapComposeArgs("ps", "-aq", "directory")...)
	if err != nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	img, err := r.capture(".", "docker", "inspect", "-f", "{{.Config.Image}}", id)
	if err != nil {
		return false
	}
	img = strings.ToLower(img)
	return strings.Contains(img, "dirsrv") || strings.Contains(img, "389")
}
