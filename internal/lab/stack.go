package lab

import "fmt"

// Up brings the whole lab up under the active profile: vendored repos,
// secrets, fixtures, LabLDAP and TacLab projects, main compose project, and
// gateway registration. Idempotent.
func (r *Runner) Up() error {
	if err := r.Preflight(); err != nil {
		return err
	}
	fmt.Printf("profile: %s (%s)\n", r.Prof.Name, r.Prof.Dir)
	for _, step := range []func() error{
		r.Vendor,
		r.Secrets,
		r.refreshDerivedEnv, // pick up secrets generated a moment ago
		r.Fixtures,
		r.EnsureNetwork,
	} {
		if err := step(); err != nil {
			return err
		}
	}
	if r.alreadyReloaded("labldap") {
		fmt.Println("up: skip labldap (reloaded this process)")
	} else if err := r.LabLDAPUp(); err != nil {
		return err
	}
	if r.alreadyReloaded("labtacacs") {
		fmt.Println("up: skip labtacacs (reloaded this process)")
	} else if err := r.LabTacacsUp(); err != nil {
		return err
	}
	if err := r.compose("up", "-d", "--build", "--wait"); err != nil {
		return err
	}
	return r.Register()
}

// Down stops both compose projects. Persistent state (none by design, apart
// from bind-mounted storage dirs) survives.
func (r *Runner) Down() error {
	if err := r.compose("down", "--remove-orphans"); err != nil {
		return err
	}
	if err := r.LabLDAPDown(false); err != nil {
		return err
	}
	return r.LabTacacsDown(false)
}

// Reset stops everything and wipes all runtime state (volumes included).
func (r *Runner) Reset() error {
	if err := r.compose("down", "--remove-orphans", "-v"); err != nil {
		return err
	}
	if err := r.LabLDAPDown(true); err != nil {
		return err
	}
	return r.LabTacacsDown(true)
}
