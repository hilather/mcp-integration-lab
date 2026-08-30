package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveReloadAliases(t *testing.T) {
	cases := map[string]string{
		"labdns":    "labdns",
		"DNS":       "labdns",
		"maildev":   "maildev",
		"labmail":   "maildev",
		"mail":      "maildev",
		"nfs":       "nfs",
		"labinfo":   "labinfo",
		"mcpjungle": "mcpjungle",
		"gateway":   "mcpjungle",
		"labldap":   "labldap",
		"ldap":      "labldap",
		"labtacacs": "labtacacs",
		"taclab":    "labtacacs",
		"tacacs":    "labtacacs",
		"labmitm":   "labmitm",
		"mitm":      "labmitm",
		"labgraph":  "labgraph",
		"graph":     "labgraph",
		"scenario":  "labgraph",
	}
	for in, want := range cases {
		got, err := ResolveReload(in)
		if err != nil {
			t.Fatalf("ResolveReload(%q): %v", in, err)
		}
		if got.canonical != want {
			t.Errorf("ResolveReload(%q).canonical = %q, want %q", in, got.canonical, want)
		}
	}
}

func TestResolveReloadUnknown(t *testing.T) {
	_, err := ResolveReload("not-a-service")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown app") {
		t.Errorf("error = %v", err)
	}
	for _, name := range CanonicalReloadApps {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error should list %s: %v", name, err)
		}
	}
}

func TestResolveReloadComposeService(t *testing.T) {
	got, err := ResolveReload("labmail")
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != reloadMainCompose || got.composeService != "maildev" {
		t.Fatalf("labmail should reload compose service maildev, got %+v", got)
	}
	gw, err := ResolveReload("gateway")
	if err != nil {
		t.Fatal(err)
	}
	if gw.kind != reloadGateway {
		t.Fatalf("gateway should re-register after recreate, kind=%v", gw.kind)
	}
}

func TestReloadMainArgs(t *testing.T) {
	args := reloadMainArgs("labdns")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--no-deps", "--force-recreate", "--wait", "--build", "labdns"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
}

func TestReloadLabtacacsRequiresSecrets(t *testing.T) {
	r := &Runner{Root: t.TempDir()}
	err := r.requireTaclabSecrets()
	if err == nil || !strings.Contains(err.Error(), "mcplab secrets") {
		t.Fatalf("got %v", err)
	}
}

func TestCanonicalReloadAppsAllResolve(t *testing.T) {
	for _, name := range CanonicalReloadApps {
		got, err := ResolveReload(name)
		if err != nil {
			t.Fatalf("ResolveReload(%q): %v", name, err)
		}
		if got.canonical != name {
			t.Errorf("ResolveReload(%q).canonical = %q", name, got.canonical)
		}
	}
}

func TestMakefileReloadUsageMentionsLabgraph(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "labgraph") {
		t.Fatal("Makefile reload usage missing labgraph")
	}
}

func TestDocsMentionCanonicalReloadApps(t *testing.T) {
	root := filepath.Join("..", "..")
	files := []string{
		"AGENTS.md",
		"CONTRIBUTING.md",
		"README.md",
		"docs/guides/quickstart.md",
		"docs/guides/configuration.md",
		"docs/architecture.md",
	}
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		body := string(b)
		for _, app := range CanonicalReloadApps {
			if !strings.Contains(body, app) {
				t.Errorf("%s missing reload app %s", rel, app)
			}
		}
	}
}

func TestReloadLabldapForceRecreatesAndBootstraps(t *testing.T) {
	dir := strings.Join(reloadLabldapDirectoryArgs(), " ")
	ctrl := strings.Join(reloadLabldapControlArgs(), " ")
	boot := strings.Join(labldapOneShotArgs("bootstrap"), " ")
	for _, want := range []string{"--force-recreate", "directory"} {
		if !strings.Contains(dir, want) {
			t.Errorf("directory args missing %s: %s", want, dir)
		}
	}
	for _, want := range []string{"--force-recreate", "control"} {
		if !strings.Contains(ctrl, want) {
			t.Errorf("control args missing %s: %s", want, ctrl)
		}
	}
	for _, want := range []string{"--no-deps", "--force-recreate", "--exit-code-from", "bootstrap"} {
		if !strings.Contains(boot, want) {
			t.Errorf("bootstrap one-shot missing %s: %s", want, boot)
		}
	}
}
