package taclabcfg

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	secretFileMode    os.FileMode = 0o444
	passwordsFileMode os.FileMode = 0o600

	passwordsHeader = "# Lab-only plaintext. Mode 0600. Do not commit. labgen never logs these values."
)

// Catalog is the TacLab subset of a profile's dev-credentials.yaml.
type Catalog struct {
	APIAdminToken    string
	TacacsSecret     string
	RadiusSecret     string
	AdminPassword    string
	AdminEnable      string
	ReadonlyPassword string
	DisabledPassword string
	ChallengeSecret  string
}

// ApplyResult reports which plaintext files were rewritten. PHC rewrite
// alone never sets Changed.
type ApplyResult struct {
	Changed         bool
	APIAdminChanged bool
}

// ApplyDevSecrets pins labgen secret files to catalog values. dir is
// deployments/compose/secrets. PKI, certs-public, tacacs_server_key.pem,
// and generated YAML are left alone.
func ApplyDevSecrets(dir string, cat Catalog, params Argon2Params, entropy io.Reader) (ApplyResult, error) {
	var res ApplyResult
	if params == (Argon2Params{}) {
		params = DefaultParams
	}
	if entropy == nil {
		entropy = rand.Reader
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}

	type plain struct {
		name string
		val  string
		flag *bool
	}
	for _, p := range []plain{
		{"api_admin_token", cat.APIAdminToken, &res.APIAdminChanged},
		{"lab_switches_tacacs_secret", cat.TacacsSecret, nil},
		{"lab_switches_radius_secret", cat.RadiusSecret, nil},
		{"lab_admin_challenge_secret", cat.ChallengeSecret, nil},
	} {
		changed, err := writeRaw(filepath.Join(dir, p.name), []byte(p.val), secretFileMode)
		if err != nil {
			return res, err
		}
		if changed {
			res.Changed = true
			if p.flag != nil {
				*p.flag = true
			}
		}
	}

	for _, h := range []struct{ name, password string }{
		{"lab_admin_argon2id", cat.AdminPassword},
		{"lab_admin_enable_argon2id", cat.AdminEnable},
		{"lab_readonly_argon2id", cat.ReadonlyPassword},
		{"lab_disabled_argon2id", cat.DisabledPassword},
	} {
		if err := applyPHC(filepath.Join(dir, h.name), h.password, params, entropy); err != nil {
			return res, err
		}
	}

	pwChanged, err := applyPasswords(filepath.Join(dir, "PASSWORDS.txt"), cat)
	if err != nil {
		return res, err
	}
	if pwChanged {
		res.Changed = true
	}
	return res, nil
}

func applyPHC(path, password string, params Argon2Params, entropy io.Reader) error {
	existing, err := os.ReadFile(path)
	if err == nil && VerifyArgon2id(existing, []byte(password)) == nil {
		if err := os.Chmod(path, secretFileMode); err != nil {
			return err
		}
		fmt.Printf("skipped %s (exists)\n", path)
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	phc, err := DeriveArgon2id([]byte(password), params, entropy)
	if err != nil {
		return err
	}
	_, err = writeRaw(path, phc, secretFileMode)
	return err
}

func applyPasswords(path string, cat Catalog) (bool, error) {
	want := map[string]string{
		"lab-admin":           cat.AdminPassword,
		"lab-admin-enable":    cat.AdminEnable,
		"lab-readonly":        cat.ReadonlyPassword,
		"lab-disabled":        cat.DisabledPassword,
		"lab-admin-challenge": cat.ChallengeSecret,
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err == nil && passwordEntriesEqual(existing, want) {
		if err := os.Chmod(path, passwordsFileMode); err != nil {
			return false, err
		}
		fmt.Printf("skipped %s (exists)\n", path)
		return false, nil
	}
	body := passwordsBody(cat)
	return writeRaw(path, body, passwordsFileMode)
}

func passwordsBody(cat Catalog) []byte {
	return []byte(strings.Join([]string{
		passwordsHeader,
		"lab-admin=" + cat.AdminPassword,
		"lab-admin-enable=" + cat.AdminEnable,
		"lab-readonly=" + cat.ReadonlyPassword,
		"lab-disabled=" + cat.DisabledPassword,
		"lab-admin-challenge=" + cat.ChallengeSecret,
		"",
	}, "\n"))
}

func passwordEntriesEqual(existing []byte, want map[string]string) bool {
	got := parsePasswordFile(existing)
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func parsePasswordFile(b []byte) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m[k] = v
	}
	return m
}

func writeRaw(path string, data []byte, mode os.FileMode) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		if err := os.Chmod(path, mode); err != nil {
			return false, err
		}
		fmt.Printf("skipped %s (exists)\n", path)
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	existed := err == nil
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return false, err
	}
	if err := os.Chmod(path, mode); err != nil {
		return false, err
	}
	if !existed {
		fmt.Printf("wrote %s\n", path)
	} else {
		fmt.Printf("reconciled %s (dev catalog)\n", path)
	}
	return true, nil
}
