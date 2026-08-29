package lab

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Vendored service repos live in third_party/ (not vendor/: that name is
// reserved by the Go toolchain for module vendoring). Ref is a release tag
// checked out on every `mcplab vendor`.
var vendorRepos = []struct{ URL, Dest, Ref string }{
	{"https://github.com/hilather/go-lab-dns", "third_party/go-lab-dns", "v1.3.0"},
	{"https://github.com/hilather/go-lab-ldap-mcp", "third_party/go-lab-ldap-mcp", "v0.5.0"},
	{"https://github.com/hilather/go-lab-tacacs-mcp", "third_party/go-lab-tacacs-mcp", "v1.5.0"},
	{"https://github.com/hilather/go-lab-maildev", "third_party/go-lab-maildev", "v1.0.0-rc.4"},
	{"https://github.com/hilather/go-lab-mitmproxy", "third_party/go-lab-mitmproxy", "v1.5.0"},
}

// Vendor clones or updates the service repos to their pinned refs and
// applies this repo's patches.
func (r *Runner) Vendor() error {
	if err := os.MkdirAll(r.path("third_party"), 0o755); err != nil {
		return err
	}
	for _, repo := range vendorRepos {
		if err := r.vendorCheckout(repo.URL, repo.Dest, repo.Ref); err != nil {
			return err
		}
	}

	// Apply local patches (patches/<repo-name>-*.patch) idempotently: skip a
	// patch that already reverse-applies cleanly (i.e. is present).
	for _, repo := range vendorRepos {
		name := filepath.Base(repo.Dest)
		matches, err := filepath.Glob(r.path("patches/" + name + "-*.patch"))
		if err != nil {
			return err
		}
		for _, patch := range matches {
			dest := r.path(repo.Dest)
			if _, err := r.capture(".", "git", "-C", dest, "apply", "--reverse", "--check", patch); err == nil {
				fmt.Printf("vendor: %s already applied\n", filepath.Base(patch))
				continue
			}
			if err := r.run(".", "git", "-C", dest, "apply", patch); err != nil {
				return err
			}
			fmt.Printf("vendor: applied %s\n", filepath.Base(patch))
		}
	}
	return nil
}

func (r *Runner) vendorCheckout(url, dest, ref string) error {
	gitDir := filepath.Join(r.path(dest), ".git")
	if _, err := os.Stat(gitDir); err != nil {
		args := []string{"clone", "--depth", "1"}
		if ref != "" {
			args = append(args, "--branch", ref)
		}
		args = append(args, url, dest)
		return r.run(".", "git", args...)
	}
	if ref == "" {
		fmt.Printf("vendor: %s already present\n", dest)
		return nil
	}

	// Drop leftover local patches; they are reapplied from patches/.
	if err := r.run(".", "git", "-C", r.path(dest), "reset", "--hard"); err != nil {
		return err
	}
	if err := r.run(".", "git", "-C", r.path(dest), "clean", "-fd"); err != nil {
		return err
	}

	if at, err := r.capture(".", "git", "-C", r.path(dest), "describe", "--tags", "--exact-match"); err == nil && strings.TrimSpace(at) == ref {
		fmt.Printf("vendor: %s at %s\n", dest, ref)
		return nil
	}
	if err := r.run(".", "git", "-C", r.path(dest), "fetch", "--depth", "1", "origin", "tag", ref); err != nil {
		return err
	}
	if err := r.run(".", "git", "-C", r.path(dest), "checkout", "--detach", "FETCH_HEAD"); err != nil {
		return err
	}
	fmt.Printf("vendor: %s checked out %s\n", dest, ref)
	return nil
}
