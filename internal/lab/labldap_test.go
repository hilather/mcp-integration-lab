package lab

import (
	"strings"
	"testing"
)

func TestLabldapOneShotArgsAvoidWaitRace(t *testing.T) {
	args := labldapOneShotArgs("native-secret-prep")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--abort-on-container-exit", "--exit-code-from", "native-secret-prep"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	for _, a := range args {
		if a == "wait" || a == "-d" {
			t.Errorf("one-shot must not detach or wait (race): %v", args)
		}
	}
}

func TestLabldapComposeArgsUsesNativeEngine(t *testing.T) {
	r := &Runner{Root: "/repo"}
	args := r.labldapComposeArgs("up", "-d")
	joined := strings.Join(args, "\n")
	for _, want := range []string{
		"compose.native.yaml",
		"compose.native-ephemeral.yaml",
		"labldap.overlay.yaml",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	for _, a := range args {
		if strings.HasSuffix(a, "/compose.ephemeral.yaml") {
			t.Fatalf("389 ephemeral overlay still selected: %s", a)
		}
	}
}
