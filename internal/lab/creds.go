package lab

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hilather/mcp-integration-lab/internal/labinfo"
	"github.com/hilather/mcp-integration-lab/internal/profile"
)

const credsErrNonDev = "credentials are not printed outside LAB_DEV_MODE"

// Creds prints a copy-pasteable credentials sheet. Source of truth is
// staged files on disk (what labinfo would reveal), not the YAML catalog
// in isolation. Fail-closed outside LAB_DEV_MODE. TLS private keys are
// never printed.
func (r *Runner) Creds() error {
	sheet, err := r.credsSheet()
	if err != nil {
		return err
	}
	fmt.Print(sheet)
	return nil
}

func (r *Runner) credsSheet() (string, error) {
	if !profile.IsTrue(r.Prof.Get("LAB_DEV_MODE", "false")) {
		return "", fmt.Errorf("%s", credsErrNonDev)
	}
	cat, err := labinfo.Load(filepath.Join(r.Prof.Dir, "labinfo", "services.yaml"))
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("# MCP lab credentials (LAB_DEV_MODE)\n\n")
	b.WriteString("Source: secrets/labinfo-creds after `mcplab secrets` (files on disk, not the YAML catalog in isolation).\n")
	b.WriteString("TLS private keys are not printed.\n\n")

	b.WriteString("## Host\n\n")
	b.WriteString("PROFILE=" + r.Prof.Name + "\n")
	for _, line := range r.credsEnvLines() {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	for _, s := range cat.Services {
		var body strings.Builder
		seen := map[string]bool{}
		if s.Credential != nil {
			name := filepath.Base(s.Credential.File)
			if err := r.writeCredKV(&body, name, s.Credential.File, false); err != nil {
				return "", fmt.Errorf("service %s: %w", s.ID, err)
			}
			seen[name] = true
		}
		if s.Connection != nil {
			for _, cr := range s.Connection.Credentials {
				if seen[cr.Name] {
					continue
				}
				if err := r.writeCredKV(&body, cr.Name, cr.File, cr.Optional); err != nil {
					return "", fmt.Errorf("service %s: credential %s: %w", s.ID, cr.Name, err)
				}
				seen[cr.Name] = true
			}
		}
		if body.Len() == 0 {
			continue
		}
		b.WriteString("## " + s.ID + "\n\n")
		b.WriteString(body.String())
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func (r *Runner) credsEnvLines() []string {
	keys := make([]string, 0, len(r.Prof.Values))
	for k := range r.Prof.Values {
		if k == "LAB_PUBLIC_HOST" || strings.HasSuffix(k, "_PORT") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+r.Prof.Values[k])
	}
	return out
}

func (r *Runner) writeCredKV(b *strings.Builder, name, catalogFile string, optional bool) error {
	if isPrivateKeyFile(catalogFile) {
		return nil
	}
	path := r.stagedCredPath(catalogFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if optional && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s (run mcplab secrets first): %w", path, err)
	}
	value := strings.TrimSpace(string(raw))
	if strings.Contains(value, "-----BEGIN ") || strings.Contains(value, "\n") {
		b.WriteString(name + ":\n```\n" + value + "\n```\n")
		return nil
	}
	b.WriteString(name + "=" + value + "\n")
	return nil
}

func (r *Runner) stagedCredPath(catalogFile string) string {
	base := filepath.Base(catalogFile)
	return r.path(filepath.Join("secrets/labinfo-creds", base))
}

func isPrivateKeyFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, ".key") || strings.HasSuffix(base, "key.pem")
}
