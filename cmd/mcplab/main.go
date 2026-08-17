// mcplab orchestrates the MCP integration lab: vendored service repos,
// secrets, fixtures, the two docker compose projects, gateway registration,
// and the end-to-end smoke test. Configuration comes from the active profile
// (profiles/<name>, selected via PROFILE in .env or the environment).
package main

import (
	"fmt"
	"os"

	"github.com/hilather/mcp-integration-lab/internal/lab"
)

const usage = `mcplab — MCP integration lab orchestrator

Usage: mcplab <command>

  up             vendor + secrets + fixtures + full stack + gateway registration
  down           stop everything (bind-mounted storage survives)
  reset          down -v for all compose projects: wipe all runtime state
  register       (re)apply gateway config from the active profile
  smoke          end-to-end DNS/LDAP/NFS/TACACS+RADIUS/mail scenario through the gateway

  vendor         clone/update pinned service repos into third_party/ and apply patches/
  secrets        generate tokens, LabLDAP secrets, lab CA, TacLab lab dir
  fixtures       build the NFS fixture archive + work dir
  labldap-up     (re)start only the LabLDAP compose project
  labldap-down   stop only the LabLDAP compose project
  labtacacs-up   (re)start only the TacLab compose project
  labtacacs-down stop only the TacLab compose project

Profile selection: PROFILE=<name> (env or .env), directories under profiles/.
`

func main() {
	if len(os.Args) != 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	r, err := lab.New(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcplab: %v\n", err)
		os.Exit(1)
	}

	commands := map[string]func() error{
		"up":           r.Up,
		"down":         r.Down,
		"reset":        r.Reset,
		"register":     r.Register,
		"smoke":        r.Smoke,
		"vendor":       r.Vendor,
		"secrets":      r.Secrets,
		"fixtures":     r.Fixtures,
		"labldap-up":     r.LabLDAPUp,
		"labldap-down":   func() error { return r.LabLDAPDown(false) },
		"labtacacs-up":   r.LabTacacsUp,
		"labtacacs-down": func() error { return r.LabTacacsDown(false) },
	}
	cmd, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "mcplab: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err := cmd(); err != nil {
		fmt.Fprintf(os.Stderr, "mcplab %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}
