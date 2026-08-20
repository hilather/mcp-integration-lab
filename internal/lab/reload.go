package lab

import (
	"fmt"
	"strings"
)

// CanonicalReloadApps are the names operators pass to `mcplab reload`.
var CanonicalReloadApps = []string{
	"labdns", "maildev", "nfs", "labinfo", "mcpjungle", "labldap", "labtacacs",
}

type reloadKind int

const (
	reloadMainCompose reloadKind = iota
	reloadGateway
	reloadLabLDAP
	reloadLabTacacs
)

type reloadTarget struct {
	kind           reloadKind
	composeService string
	canonical      string
}

// ResolveReload maps an operator-facing name (and aliases) onto a reload
// target. Unknown names fail closed so a typo cannot `compose up` an
// unexpected service.
func ResolveReload(name string) (reloadTarget, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "labdns", "dns":
		return reloadTarget{kind: reloadMainCompose, composeService: "labdns", canonical: "labdns"}, nil
	case "maildev", "labmail", "mail":
		return reloadTarget{kind: reloadMainCompose, composeService: "maildev", canonical: "maildev"}, nil
	case "nfs":
		return reloadTarget{kind: reloadMainCompose, composeService: "nfs", canonical: "nfs"}, nil
	case "labinfo":
		return reloadTarget{kind: reloadMainCompose, composeService: "labinfo", canonical: "labinfo"}, nil
	case "mcpjungle", "gateway":
		return reloadTarget{kind: reloadGateway, composeService: "mcpjungle", canonical: "mcpjungle"}, nil
	case "labldap", "ldap":
		return reloadTarget{kind: reloadLabLDAP, canonical: "labldap"}, nil
	case "labtacacs", "taclab", "tacacs":
		return reloadTarget{kind: reloadLabTacacs, canonical: "labtacacs"}, nil
	default:
		return reloadTarget{}, fmt.Errorf("unknown app %q (want one of: %s)", name, strings.Join(CanonicalReloadApps, ", "))
	}
}

// Reload rebuilds and recreates one lab application without taking the rest
// of the stack down. Bind-mounted profile YAML is re-read; in-process
// runtime state for that app is discarded. mcpjungle reload also re-runs
// gateway registration because the SQLite lives on tmpfs.
func (r *Runner) Reload(name string) error {
	target, err := ResolveReload(name)
	if err != nil {
		return err
	}
	fmt.Printf("reload: %s\n", target.canonical)
	switch target.kind {
	case reloadMainCompose:
		return r.reloadMain(target.composeService)
	case reloadGateway:
		if err := r.EnsureNetwork(); err != nil {
			return err
		}
		if err := r.reloadMain("mcpjungle"); err != nil {
			return err
		}
		return r.Register()
	case reloadLabLDAP:
		return r.reloadLabLDAP()
	case reloadLabTacacs:
		return r.reloadLabTacacs()
	default:
		return fmt.Errorf("internal: unhandled reload kind for %s", target.canonical)
	}
}

func reloadMainArgs(service string) []string {
	// --no-deps keeps sibling mcplab services running. --force-recreate
	// re-reads bind-mounted YAML even when the compose spec is unchanged.
	return []string{"up", "-d", "--build", "--wait", "--no-deps", "--force-recreate", service}
}

func (r *Runner) reloadMain(service string) error {
	return r.compose(reloadMainArgs(service)...)
}

func (r *Runner) reloadLabLDAP() error {
	if err := r.EnsureNetwork(); err != nil {
		return err
	}
	ll := "third_party/go-lab-ldap-mcp"
	if err := r.run(ll, "make", "image-native", "image-bootstrap", "image"); err != nil {
		return err
	}
	if r.labldapNeeds389Wipe() {
		fmt.Println("labldap: dropping leftover 389 DS volume (engine is native)")
		if err := r.LabLDAPDown(true); err != nil {
			return err
		}
	}
	if err := r.labldapOneShot("native-secret-prep"); err != nil {
		return err
	}
	if err := r.labldapCompose(reloadLabldapDirectoryArgs()...); err != nil {
		return err
	}
	if err := r.labldapOneShot("secret-prep"); err != nil {
		return err
	}
	// Directory recreate remounts ephemeral tmpfs empty; bootstrap must
	// re-seed the suffix. Directory is already up, so --no-deps is safe.
	if err := r.labldapOneShot("bootstrap"); err != nil {
		return err
	}
	return r.labldapCompose(reloadLabldapControlArgs()...)
}

func reloadLabldapDirectoryArgs() []string {
	return []string{"up", "-d", "--wait", "--remove-orphans", "--force-recreate", "directory"}
}

func reloadLabldapControlArgs() []string {
	return []string{"up", "-d", "--wait", "--remove-orphans", "--force-recreate", "control"}
}

func (r *Runner) reloadLabTacacs() error {
	if err := r.EnsureNetwork(); err != nil {
		return err
	}
	if err := r.requireTaclabSecrets(); err != nil {
		return err
	}
	return r.labtacacsCompose("up", "-d", "--build", "--wait", "--remove-orphans", "--force-recreate")
}
