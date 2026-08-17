package lab

import (
	"archive/tar"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// tarGzMemberMD5 returns the md5 hex digest of one member inside a tar.gz.
// Used by the smoke test to prove NFS reads are byte-exact against source.
func tarGzMemberMD5(archive, member string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return "", fmt.Errorf("member %q not found in %s", member, archive)
		}
		if err != nil {
			return "", err
		}
		if hdr.Name == member {
			h := md5.New()
			if _, err := io.Copy(h, tr); err != nil {
				return "", err
			}
			return hex.EncodeToString(h.Sum(nil)), nil
		}
	}
}
