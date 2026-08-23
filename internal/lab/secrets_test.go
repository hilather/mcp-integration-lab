package lab

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTokenIfMissingCreates0644(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labmitm-token")
	if err := writeTokenIfMissing(path, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.TrimSpace(string(b))
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		t.Fatalf("token is not hex: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("token length = %d, want 32 bytes", len(decoded))
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %04o, want 0644", fi.Mode().Perm())
	}
}

func TestWriteTokenIfMissingChmodsExistingTo0644(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labmitm-token")
	const body = "keep-me\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeTokenIfMissing(path, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("content = %q, want %q", got, body)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %04o, want 0644", fi.Mode().Perm())
	}
}
