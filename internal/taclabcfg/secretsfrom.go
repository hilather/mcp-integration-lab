package taclabcfg

import (
	"os"
	"path/filepath"
	"strings"
)

const secretsFromFileMode os.FileMode = 0o600

// secretsFromHeader is committed on the golden and emitted on disk so a
// TacLab pin bump knows to re-copy validSecretsFromYAML under it.
const secretsFromHeader = "# Re-sync from TacLab validSecretsFromYAML when bumping the TacLab vendor pin."

// WriteSecretsFrom writes labgen -secrets-from YAML for cat. Hand-built so
// key order and quoting stay stable (labgen KnownFields).
func WriteSecretsFrom(path string, cat Catalog) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, secretsFromYAML(cat), secretsFromFileMode); err != nil {
		return err
	}
	return os.Chmod(path, secretsFromFileMode)
}

func secretsFromYAML(cat Catalog) []byte {
	return []byte(strings.Join([]string{
		secretsFromHeader,
		"api_admin_token: " + cat.APIAdminToken,
		"lab_switches_tacacs_secret: " + cat.TacacsSecret,
		"lab_switches_radius_secret: " + cat.RadiusSecret,
		"passwords:",
		"  lab-admin: " + cat.AdminPassword,
		"  lab-admin-enable: " + cat.AdminEnable,
		"  lab-readonly: " + cat.ReadonlyPassword,
		"  lab-disabled: " + cat.DisabledPassword,
		"  lab-admin-challenge: " + cat.ChallengeSecret,
		"",
	}, "\n"))
}
