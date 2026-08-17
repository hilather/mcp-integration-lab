package lab

import (
	"path/filepath"
	"testing"
)

// Regression: the fixture archive we generate must contain the members the
// NFS smoke test depends on, and member checksums must be recoverable so the
// byte-exact NFS comparison stays meaningful.
func TestFixtureArchiveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixtures.tar.gz")
	if err := writeFixtureArchive(path); err != nil {
		t.Fatal(err)
	}

	for _, member := range []string{
		"fixtures/hello.txt",
		"fixtures/nested/notes.md",
		"fixtures/random-1mib.bin",
	} {
		sum, err := tarGzMemberMD5(path, member)
		if err != nil {
			t.Errorf("%s: %v", member, err)
			continue
		}
		if len(sum) != 32 {
			t.Errorf("%s: md5 hex length = %d, want 32", member, len(sum))
		}
	}

	if _, err := tarGzMemberMD5(path, "fixtures/does-not-exist"); err == nil {
		t.Error("expected error for missing member")
	}
}
