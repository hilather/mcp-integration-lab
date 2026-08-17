package lab

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// zstd magic: https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md
var zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

// Regression: the NFS starting archive must be zstd (gzip is rejected for
// live overlay commit) and contain only the root directory so the export
// presents an empty `/` for clients to write into.
func TestEmptyRootTarZst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixtures.tar.zst")
	if err := writeEmptyRootTarZst(path); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 4 || !bytes.Equal(raw[:4], zstdMagic) {
		t.Fatalf("missing zstd magic, got %x", raw[:min(4, len(raw))])
	}
	if _, err := gzip.NewReader(bytes.NewReader(raw)); err == nil {
		t.Fatal("fixture archive is gzip-compressed; live overlay commit rejects gzip")
	}

	names, err := tarZstMembers(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != rootDirMember {
		t.Fatalf("members = %v, want only %q", names, rootDirMember)
	}

	if err := tarZstContains(path, rootDirMember); err != nil {
		t.Fatal(err)
	}
	if err := tarZstContains(path, "fixtures/hello.txt"); err == nil {
		t.Error("expected error for a member that is not in the empty-root archive")
	}
}
