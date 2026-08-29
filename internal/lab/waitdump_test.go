package lab

import (
	"errors"
	"strings"
	"testing"
)

func TestIsMainUpWait(t *testing.T) {
	if !isMainUpWait([]string{"up", "-d", "--build", "--wait"}) {
		t.Fatal("Up argv must dump")
	}
	if !isMainUpWait(reloadMainArgs("labdns")) {
		t.Fatal("reloadMain argv must dump")
	}
	if isMainUpWait([]string{"down", "--remove-orphans"}) {
		t.Fatal("down must not dump")
	}
	if isMainUpWait([]string{"ps"}) {
		t.Fatal("ps must not dump")
	}
}

func TestExtractComposeContainerID(t *testing.T) {
	id := "a1b2c3d4e5f6789012345678"
	if got := extractComposeContainerID("WARNING: foo\n" + id + "\n"); got != id {
		t.Fatalf("got %q", got)
	}
	first, second := "aaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbb"
	if got := extractComposeContainerID(first + "\n" + second); got != second {
		t.Fatalf("last id = %q, want %q", got, second)
	}
	if got := extractComposeContainerID(id + "\nWARNING: after"); got != id {
		t.Fatalf("warning after id: %q", got)
	}
	if got := extractComposeContainerID("not-an-id"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestComposeMaybeDumpWaitInvokesHook(t *testing.T) {
	var dumped error
	r := &Runner{waitDump: func(err error) { dumped = err }}
	want := errors.New("wait")
	if err := r.composeMaybeDumpWait([]string{"up", "-d", "--build", "--wait"}, want); err != want {
		t.Fatalf("err = %v", err)
	}
	if dumped != want {
		t.Fatal("waitDump not called for Up argv")
	}
	dumped = nil
	if err := r.composeMaybeDumpWait([]string{"down", "--remove-orphans"}, want); err != want {
		t.Fatalf("err = %v", err)
	}
	if dumped != nil {
		t.Fatal("waitDump must not run on down")
	}
	dumped = nil
	if err := r.composeMaybeDumpWait([]string{"up", "-d", "--build", "--wait"}, nil); err != nil {
		t.Fatal(err)
	}
	if dumped != nil {
		t.Fatal("waitDump must not run on success")
	}
}

func TestMainHealthcheckedServices(t *testing.T) {
	want := []string{"labdns", "maildev", "labmitm", "labinfo"}
	if strings.Join(mainHealthcheckedServices, ",") != strings.Join(want, ",") {
		t.Fatalf("%v", mainHealthcheckedServices)
	}
}
