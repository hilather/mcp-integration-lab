package lab

import (
	"fmt"
	"os"

	"github.com/hilather/mcp-integration-lab/internal/labjenkins"
	"github.com/hilather/mcp-integration-lab/internal/profile"
)

const (
	labjenkinsComposeRel = "third_party/go-jenkins-mcp/testdata/jwt-rs-lab/docker-compose.yml"
	labjenkinsOverlayRel = "compose/labjenkins.overlay.yaml"
)

// LabJenkinsEnabled reports whether the active profile starts jwt-rs Jenkins.
func (r *Runner) LabJenkinsEnabled() bool {
	if r.Prof == nil {
		return false
	}
	return profile.IsTrue(r.Prof.Get("LABJENKINS_ENABLED", "false"))
}

func (r *Runner) labjenkinsComposeFile() string {
	return r.path(labjenkinsComposeRel)
}

func (r *Runner) labjenkinsComposeArgs(args ...string) []string {
	base := []string{
		"compose", "-p", "labjenkins",
		"-f", r.labjenkinsComposeFile(),
		"-f", r.path(labjenkinsOverlayRel),
	}
	return append(base, args...)
}

func (r *Runner) labjenkinsCompose(args ...string) error {
	return r.run(".", "docker", r.labjenkinsComposeArgs(args...)...)
}

// applyLabJenkinsEnv overwrites JWT_RS_JWKS_URL / JWT_RS_AUDIENCE /
// LABJENKINS_IDP on Prof.Values and rebuilds r.Env. No-op when Jenkins
// is disabled so teardown never fail-closes on half-filled Entra IDs.
func (r *Runner) applyLabJenkinsEnv() error {
	if r.Prof == nil || r.Prof.Values == nil {
		return nil
	}
	if !r.LabJenkinsEnabled() {
		return nil
	}
	out, err := labjenkins.Resolve(labjenkins.Input{
		Enabled:      true,
		TenantID:     r.Prof.Get("ENTRA_TENANT_ID", ""),
		APIAppID:     r.Prof.Get("ENTRA_API_APP_ID", ""),
		GatewayAppID: r.Prof.Get("ENTRA_GATEWAY_APP_ID", ""),
	})
	if err != nil {
		return fmt.Errorf("labjenkins: %w", err)
	}
	r.Prof.Values["JWT_RS_JWKS_URL"] = out.JWKSURL
	r.Prof.Values["JWT_RS_AUDIENCE"] = out.Audience
	r.Prof.Values["LABJENKINS_IDP"] = out.IDP
	r.Env = r.Prof.Environ(os.Environ())
	return nil
}

// LabJenkinsUp brings up Keycloak + Jenkins jwt-rs on mcplab-shared.
func (r *Runner) LabJenkinsUp() error {
	if !r.LabJenkinsEnabled() {
		return fmt.Errorf("labjenkins: LABJENKINS_ENABLED is not true (copy profiles/default and set the flag in that profile.env)")
	}
	if err := r.applyLabJenkinsEnv(); err != nil {
		return err
	}
	if err := r.EnsureNetwork(); err != nil {
		return err
	}
	if err := r.labjenkinsCompose("up", "-d", "--build", "--wait", "--remove-orphans"); err != nil {
		return err
	}
	fmt.Printf("labjenkins up: Jenkins on http://<host>:%s (IdP %s)\n",
		r.Prof.Get("JWT_RS_JENKINS_PORT", "18092"),
		r.Prof.Get("LABJENKINS_IDP", labjenkins.IDPKeycloak))
	return nil
}

// LabJenkinsDown tears the jwt-rs project down. Missing vendor tree is a
// no-op so make down works before the first vendor of go-jenkins-mcp.
func (r *Runner) LabJenkinsDown(wipe bool) error {
	if _, err := os.Stat(r.labjenkinsComposeFile()); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	args := []string{"down", "--remove-orphans"}
	if wipe {
		args = append(args, "-v")
	}
	return r.labjenkinsCompose(args...)
}

// syncLabJenkins starts the project when enabled and stops leftovers when not.
func (r *Runner) syncLabJenkins() error {
	if r.LabJenkinsEnabled() {
		return r.LabJenkinsUp()
	}
	return r.LabJenkinsDown(false)
}

func (r *Runner) reloadLabJenkins() error {
	if !r.LabJenkinsEnabled() {
		return fmt.Errorf("labjenkins: LABJENKINS_ENABLED is not true; use labjenkins-down for leftovers")
	}
	if err := r.applyLabJenkinsEnv(); err != nil {
		return err
	}
	if err := r.EnsureNetwork(); err != nil {
		return err
	}
	return r.labjenkinsCompose("up", "-d", "--build", "--wait", "--remove-orphans", "--force-recreate")
}
