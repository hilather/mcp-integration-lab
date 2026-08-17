package lab

import (
	"archive/tar"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
)

// nfsFixtureArchive is the empty-root .tar.zst ratarmount serves. Live overlay
// commit accepts uncompressed TAR or .tar.zst; gzip is rejected. An archive
// that contains only the root directory (`./`) is the writable starting point.
const nfsFixtureArchive = "fixtures.tar.zst"

// rootDirMember is the TAR directory entry for the archive/NFS root. TAR
// members must be relative; an absolute "/" would be skipped by many readers.
const rootDirMember = "./"

// Fixtures prepares the NFS container's storage: the fixture archive it
// serves, the writable work directory (indexes, overlay), and the durable
// write-overlay folder live commit requires (not :temp:). Both locations are
// profile-definable (NFS_ARCHIVE_DIR / NFS_DATA_DIR).
func (r *Runner) Fixtures() error {
	archiveDir := r.path(r.Prof.Get("NFS_ARCHIVE_DIR", ".data/nfs"))
	workDir := r.path(r.Prof.Get("NFS_DATA_DIR", ".data/nfs-work"))
	overlayDir := filepath.Join(workDir, "overlay")

	// The container runs unprivileged (uid 65532) and must write indexes,
	// overlay files, and (on commit) a sibling copy of the archive.
	for _, dir := range []string{workDir, overlayDir, archiveDir} {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o777); err != nil {
			return err
		}
	}

	target := filepath.Join(archiveDir, nfsFixtureArchive)
	if _, err := os.Stat(target); err == nil {
		fmt.Printf("fixtures: %s already present\n", target)
		return nil
	}
	if err := writeEmptyRootTarZst(target); err != nil {
		return err
	}
	if err := os.Chmod(target, 0o666); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", target)
	return nil
}

// writeEmptyRootTarZst writes a zstd-compressed TAR whose only member is the
// root directory. That is the starting tree the NFS export presents at `/`.
func writeEmptyRootTarZst(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc, err := zstd.NewWriter(f)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(enc)
	hdr := &tar.Header{
		Typeflag: tar.TypeDir,
		Name:     rootDirMember,
		Mode:     0o755,
		ModTime:  time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		_ = enc.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		_ = enc.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return f.Close()
}
