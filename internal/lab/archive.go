package lab

import (
	"archive/tar"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// tarZstMembers returns the member names inside a .tar.zst. Used to prove
// the NFS fixture is an empty-root starting point (only `./`).
func tarZstMembers(archive string) ([]string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	tr := tar.NewReader(dec)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
		names = append(names, hdr.Name)
	}
}

func tarZstContains(archive, member string) error {
	names, err := tarZstMembers(archive)
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == member {
			return nil
		}
	}
	return fmt.Errorf("member %q not found in %s (have %v)", member, archive, names)
}
