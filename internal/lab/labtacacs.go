package lab

import (
	"fmt"
	"os"
	"path/filepath"
)

// taclabDir is the vendored TacLab checkout; labgen materializes its compose
// bundle (configs, secrets, PKI) under deployments/compose.
const taclabDir = "third_party/go-lab-tacacs-mcp"

// labtacacsComposeArgs is the vendored TacLab compose bundle in its combined
// (TACACS+ + RADIUS/UDP) shape, plus this repo's overlay (shared network;
// published ports come from the profile's TACLAB_* variables, which upstream's
// compose already interpolates).
func (r *Runner) labtacacsComposeArgs(args ...string) []string {
	tc := filepath.Join(r.Root, taclabDir, "deployments", "compose")
	base := []string{
		"compose", "-p", "labtacacs",
		"-f", filepath.Join(tc, "compose.yaml"),
		"-f", filepath.Join(tc, "compose.combined.yaml"),
		"-f", filepath.Join(r.Root, "compose", "labtacacs.overlay.yaml"),
	}
	return append(base, args...)
}

func (r *Runner) labtacacsCompose(args ...string) error {
	return r.run(".", "docker", r.labtacacsComposeArgs(args...)...)
}

func (r *Runner) requireTaclabSecrets() error {
	if _, err := os.Stat(r.path(taclabDir + "/deployments/compose/secrets/api_admin_token")); err != nil {
		return fmt.Errorf("TacLab lab directory not generated (run `mcplab secrets` first): %w", err)
	}
	return nil
}

// LabTacacsUp brings up the TacLab stack (separate compose project
// `labtacacs`). labgen must have generated the lab directory first
// (`mcplab secrets` does this).
func (r *Runner) LabTacacsUp() error {
	if err := r.EnsureNetwork(); err != nil {
		return err
	}
	if err := r.requireTaclabSecrets(); err != nil {
		return err
	}
	if err := r.labtacacsCompose("up", "-d", "--build", "--wait", "--remove-orphans"); err != nil {
		return err
	}
	fmt.Printf("labtacacs up: UI/REST/MCP on http://<host>:%s (token: %s/deployments/compose/secrets/api_admin_token)\n",
		r.Prof.Get("TACLAB_HTTP_PORT", "18049"), taclabDir)
	return nil
}

// LabTacacsDown tears the TacLab project down; wipe also removes volumes.
func (r *Runner) LabTacacsDown(wipe bool) error {
	args := []string{"down", "--remove-orphans"}
	if wipe {
		args = append(args, "-v")
	}
	return r.labtacacsCompose(args...)
}
