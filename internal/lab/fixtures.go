package lab

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Fixtures prepares the NFS container's storage: the read-only fixture
// archive it serves and the writable work directory (indexes, scratch disk).
// Both locations are profile-definable (NFS_ARCHIVE_DIR / NFS_DATA_DIR).
func (r *Runner) Fixtures() error {
	archiveDir := r.path(r.Prof.Get("NFS_ARCHIVE_DIR", ".data/nfs"))
	workDir := r.path(r.Prof.Get("NFS_DATA_DIR", ".data/nfs-work"))

	if err := os.MkdirAll(workDir, 0o777); err != nil {
		return err
	}
	// The container runs unprivileged (uid 65532) and must write indexes here.
	if err := os.Chmod(workDir, 0o777); err != nil {
		return err
	}

	target := filepath.Join(archiveDir, "fixtures.tar.gz")
	if _, err := os.Stat(target); err == nil {
		fmt.Printf("fixtures: %s already present\n", target)
		return nil
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return err
	}
	if err := writeFixtureArchive(target); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", target)
	return nil
}

func writeFixtureArchive(path string) error {
	random := make([]byte, 1<<20)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	members := []struct {
		Name string
		Body []byte
	}{
		{"fixtures/hello.txt", []byte("Hello from the MCP integration lab NFS fixture.\n")},
		{"fixtures/nested/notes.md", []byte("# NFS fixture\n\nServed by ratarmount-rs (userspace NFSv3) from a read-only tar.gz archive.\n")},
		{"fixtures/random-1mib.bin", random},
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	now := time.Now()
	for _, m := range members {
		hdr := &tar.Header{
			Name:    m.Name,
			Mode:    0o644,
			Size:    int64(len(m.Body)),
			ModTime: now,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(m.Body); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Close()
}
