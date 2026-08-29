package lab

import (
	"fmt"
	"strings"
	"unicode"
)

// mainHealthcheckedServices are the main compose project services with a
// HEALTHCHECK. On --wait failure we inspect these by container ID.
var mainHealthcheckedServices = []string{"labdns", "maildev", "labmitm", "labinfo"}

// isMainUpWait reports compose argv that wait on main-project health
// (Up and reloadMain). down / other verbs must not dump.
func isMainUpWait(args []string) bool {
	hasUp, hasWait := false, false
	for _, a := range args {
		if a == "down" {
			return false
		}
		if a == "up" {
			hasUp = true
		}
		if a == "--wait" {
			hasWait = true
		}
	}
	return hasUp && hasWait
}

func (r *Runner) composeMaybeDumpWait(args []string, err error) error {
	if err != nil && isMainUpWait(args) {
		if r.waitDump != nil {
			r.waitDump(err)
		} else {
			r.dumpMainWaitHealth(err)
		}
	}
	return err
}

func (r *Runner) dumpMainWaitHealth(cause error) {
	fmt.Printf("compose --wait failed (%v); dumping main-project health\n", cause)
	if err := r.run(".", "docker", "compose", "ps", "-a"); err != nil {
		fmt.Printf("compose ps -a: %v\n", err)
	}
	for _, name := range mainHealthcheckedServices {
		raw, capErr := r.capture(".", "docker", "compose", "ps", "-aq", name)
		if capErr != nil {
			fmt.Printf("%s: compose ps -aq: %v\n%s\n", name, capErr, raw)
			continue
		}
		id := extractComposeContainerID(raw)
		if id == "" {
			fmt.Printf("%s: no container id in: %q\n", name, strings.TrimSpace(raw))
			continue
		}
		insp, inspErr := r.capture(".", "docker", "inspect", "-f", "{{json .State}} {{json .Config.Healthcheck}}", id)
		if inspErr != nil {
			fmt.Printf("%s: inspect %s: %v\n%s\n", name, id, inspErr, insp)
		} else {
			fmt.Printf("%s %s: %s\n", name, id, strings.TrimSpace(insp))
		}
		if err := r.run(".", "docker", "compose", "logs", "--tail=80", name); err != nil {
			fmt.Printf("%s: logs: %v\n", name, err)
		}
	}
}

func extractComposeContainerID(out string) string {
	var last string
	for _, tok := range strings.Fields(out) {
		if isDockerID(tok) {
			last = tok
		}
	}
	return last
}

func isDockerID(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, r := range s {
		if !unicode.Is(unicode.ASCII_Hex_Digit, r) {
			return false
		}
	}
	return true
}
