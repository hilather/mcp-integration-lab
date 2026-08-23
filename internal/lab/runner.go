// Package lab implements the mcplab CLI commands: docker orchestration for
// the MCP integration lab. Pure logic (env resolution, output parsing) lives
// in sibling packages so it can be unit/regression tested without docker.
package lab

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hilather/mcp-integration-lab/internal/maildev"
	"github.com/hilather/mcp-integration-lab/internal/profile"
)

// Runner executes lab commands against a repo root with a resolved profile.
type Runner struct {
	Root string
	Prof *profile.Profile
	Env  []string

	// reloadedThisRun records apps Secrets() already reloaded so Up does
	// not LabLDAPUp / LabTacacsUp them again this process.
	reloadedThisRun map[string]bool

	// deps overrides subprocesses and docker. Production leaves it nil.
	deps *secretsDeps
}

// New resolves the active profile and prepares the process environment used
// for every child process (docker compose interpolates from it).
func New(root string) (*Runner, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	prof, err := profile.Load(abs, env)
	if err != nil {
		return nil, err
	}
	r := &Runner{Root: abs, Prof: prof, Env: prof.Environ(os.Environ())}
	if err := r.refreshDerivedEnv(); err != nil {
		return nil, err
	}
	return r, nil
}

// refreshDerivedEnv re-validates the active profile's LabMail bootstrap
// (receive-only guard, leftover maildev.yaml rejection). Secrets are
// bind-mounted as files; do not inject MAILDEV_ARGS / MAILDEV_WEB_PASS.
func (r *Runner) refreshDerivedEnv() error {
	return maildev.ValidateProfile(r.Prof.Dir)
}

func (r *Runner) path(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(r.Root, rel)
}

// run streams a child process to the terminal.
func (r *Runner) run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = r.path(dir)
	cmd.Env = r.Env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// runWithEnv streams a child process with an explicit environment.
func (r *Runner) runWithEnv(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = r.Root
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// captureWithEnv returns combined output with an explicit environment.
func (r *Runner) captureWithEnv(env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = r.Root
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// capture returns combined output of a child process.
func (r *Runner) capture(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = r.path(dir)
	cmd.Env = r.Env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// compose runs docker compose for the main (mcplab) project.
func (r *Runner) compose(args ...string) error {
	return r.run(".", "docker", append([]string{"compose"}, args...)...)
}

// labldapComposeArgs is the vendored LabLDAP compose bundle plus this repo's
// overlay. As of LabLDAP v0.3.0, compose.yaml is already native labldapd;
// compose.ephemeral.yaml is the tmpfs /data overlay sized for bbolt.
// compose.native.yaml is an upstream one-release alias and is not stacked.
func (r *Runner) labldapComposeArgs(args ...string) []string {
	ll := filepath.Join(r.Root, "third_party", "go-lab-ldap-mcp")
	base := []string{
		"compose", "-p", "labldap",
		"-f", filepath.Join(ll, "deploy", "compose", "compose.yaml"),
		"-f", filepath.Join(ll, "deploy", "compose", "compose.ephemeral.yaml"),
		"-f", filepath.Join(r.Root, "compose", "labldap.overlay.yaml"),
	}
	return append(base, args...)
}

func (r *Runner) labldapCompose(args ...string) error {
	return r.run(".", "docker", r.labldapComposeArgs(args...)...)
}

// EnsureNetwork creates the shared external network both projects join.
func (r *Runner) EnsureNetwork() error {
	out, err := r.capture(".", "docker", "network", "inspect", "mcplab-shared")
	if err == nil && strings.Contains(out, "mcplab-shared") {
		return nil
	}
	return r.run(".", "docker", "network", "create", "mcplab-shared")
}
