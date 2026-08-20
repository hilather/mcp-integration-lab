// mcplab orchestrates the MCP integration lab: vendored service repos,
// secrets, fixtures, the two docker compose projects, gateway registration,
// and the end-to-end smoke test. Configuration comes from the active profile
// (profiles/<name>, selected via PROFILE in .env or the environment).
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/hilather/mcp-integration-lab/internal/lab"
)

const usage = `mcplab — MCP integration lab orchestrator

Usage: mcplab <command> [args]

  up             vendor + secrets + fixtures + full stack + gateway registration
  down           stop everything (bind-mounted storage survives)
  reset          down -v for all compose projects: wipe all runtime state
  register       (re)apply gateway config from the active profile
  preflight      fail fast on profile drift and unavailable host ports
  smoke          end-to-end DNS/LDAP/NFS/TACACS+RADIUS/mail scenario through the gateway
  reload <app>   rebuild/recreate one app (not a full redeploy). Apps:
                 labdns, maildev, nfs, labinfo, mcpjungle, labldap, labtacacs

  vendor         clone/update pinned service repos into third_party/ and apply patches/
  secrets        generate tokens, LabLDAP secrets, lab CA, TacLab lab dir
  fixtures       build the NFS fixture archive + work dir
  labldap-up     bring up only the LabLDAP compose project (idempotent)
  labldap-down   stop only the LabLDAP compose project
  labtacacs-up   bring up only the TacLab compose project (idempotent)
  labtacacs-down stop only the TacLab compose project

Profile selection: PROFILE=<name> (env or .env), directories under profiles/.
Use reload when a single service's YAML or image changed; use up after a
vendor pin bump, profile switch, or first bring-up.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	r, err := lab.New(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcplab: %v\n", err)
		os.Exit(1)
	}

	cmd := os.Args[1]
	if cmd == "reload" {
		if len(os.Args) != 3 {
			fmt.Fprintf(os.Stderr, "mcplab reload: need an app name (%s)\n\n%s",
				strings.Join(lab.CanonicalReloadApps, ", "), usage)
			os.Exit(2)
		}
		if err := r.Reload(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "mcplab reload: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) != 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	commands := map[string]func() error{
		"up":             r.Up,
		"down":           r.Down,
		"reset":          r.Reset,
		"register":       r.Register,
		"preflight":      r.Preflight,
		"smoke":          r.Smoke,
		"vendor":         r.Vendor,
		"secrets":        r.Secrets,
		"fixtures":       r.Fixtures,
		"labldap-up":     r.LabLDAPUp,
		"labldap-down":   func() error { return r.LabLDAPDown(false) },
		"labtacacs-up":   r.LabTacacsUp,
		"labtacacs-down": func() error { return r.LabTacacsDown(false) },
	}
	fn, ok := commands[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "mcplab: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err := fn(); err != nil {
		fmt.Fprintf(os.Stderr, "mcplab %s: %v\n", cmd, err)
		os.Exit(1)
	}
}
