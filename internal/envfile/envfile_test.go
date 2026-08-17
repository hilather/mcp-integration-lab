package envfile

import (
	"strings"
	"testing"
)

func TestParseBasics(t *testing.T) {
	in := `
# comment
FOO=bar
QUOTED="hello world"
SINGLE='x'
EMPTY=
SPACED = padded value
`
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"FOO":    "bar",
		"QUOTED": "hello world",
		"SINGLE": "x",
		"EMPTY":  "",
		"SPACED": "padded value",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("parsed %d keys, want %d: %v", len(got), len(want), got)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(strings.NewReader("not a pair\n")); err == nil {
		t.Fatal("expected error for line without =")
	}
	if _, err := Parse(strings.NewReader("=value\n")); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestParseFileMissingIsEmpty(t *testing.T) {
	m, err := ParseFile("/nonexistent/definitely-not-here.env")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("want empty map, got %v", m)
	}
}
