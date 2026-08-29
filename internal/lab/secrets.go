package lab

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hilather/mcp-integration-lab/internal/profile"
	"github.com/hilather/mcp-integration-lab/internal/taclabcfg"
)

const (
	devModeMarkerRel = "secrets/.lab-dev-mode"
	reloadsPending   = "pending"
	reloadsDone      = "done"
	// Gitignored labgen -secrets-from input. Unlinked on leave-dev.
	taclabSecretsFromRel = "secrets/taclab-secrets-from.yaml"
	// Sidecar so a failed directory recreate is retried even after SANs
	// already match (same class as enter-dev reloads=pending).
	labldapTLSReloadPendingRel = "third_party/go-lab-ldap-mcp/secrets/tls/.reload-pending"
)

type devModeMarker struct {
	profile    string
	catalogSHA string
	reloads    string
}

// secretsDeps overrides subprocesses and docker for tests without a daemon.
type secretsDeps struct {
	setupsecrets    func(force bool) error
	ensureTLS       func() (labTLSResult, error)
	ensureTaclab    func(force bool, secretsFromAbs string) error
	containerExists func(name string) (bool, error)
	reloadMain      func(service string) error
	reloadGateway   func() error
	reloadLabLDAP   func() error
	reloadLabTacacs func() error
	register        func() error

	taclabArgon2Params *taclabcfg.Argon2Params
	taclabEntropy      io.Reader
}

type secretChanges struct {
	labdnsToken    bool
	labinfoToken   bool
	labmailToken   bool
	maildevWeb     bool
	mcpClientToken bool
	labmitmToken   bool
	labldapAlice   bool
	labldapRuntime bool
	labldapDM      bool
	labldapAdmin   bool
	labtacacs      bool
	labtacacsAdmin bool
	labldapTLS     bool
}

func (c secretChanges) labldap() bool {
	return c.labldapAlice || c.labldapRuntime || c.labldapDM || c.labldapAdmin || c.labldapTLS
}

func (c secretChanges) labtacacsReload() bool {
	return c.labtacacs || c.labtacacsAdmin
}

func (c secretChanges) registrarEnv() bool {
	return c.labdnsToken || c.labinfoToken || c.labmailToken || c.mcpClientToken || c.labmitmToken || c.labldapAdmin || c.labtacacsAdmin
}

func (c secretChanges) count() int {
	n := 0
	for _, v := range []bool{
		c.labdnsToken, c.labinfoToken, c.labmailToken, c.maildevWeb, c.mcpClientToken, c.labmitmToken,
		c.labldapAlice, c.labldapRuntime, c.labldapDM, c.labldapAdmin,
		c.labtacacs, c.labtacacsAdmin,
	} {
		if v {
			n++
		}
	}
	return n
}

// Full consumer set for an unfinished enter-dev (marker missing or
// reloads!=done), so leftover LabLDAP /data cannot keep a pre-catalog hash.
func enterDevReloadChanges() secretChanges {
	return secretChanges{
		labdnsToken:    true,
		labinfoToken:   true,
		labmailToken:   true,
		maildevWeb:     true,
		mcpClientToken: true,
		labmitmToken:   true,
		labldapAlice:   true,
		labldapRuntime: true,
		labldapDM:      true,
		labldapAdmin:   true,
		labtacacs:      true,
		labtacacsAdmin: true,
	}
}

// Secrets generates or reconciles lab secrets. Non-dev mints random files
// if missing. Dev mode (LAB_DEV_MODE only — never MCPJUNGLE_MODE) writes
// the active profile's dev-credentials.yaml. Leaving dev mode remints.
// Both modes run labtlsEnsure so LabLDAP leaves include LAB_PUBLIC_HOST.
// When containers already exist, Secrets() reloads them; Up skips those
// names via reloadedThisRun.
func (r *Runner) Secrets() error {
	r.reloadedThisRun = map[string]bool{}
	if err := os.MkdirAll(r.path("secrets"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(r.path("secrets/mcpjungle-home"), 0o755); err != nil {
		return err
	}

	devMode := profile.IsTrue(r.Prof.Get("LAB_DEV_MODE", "false"))
	marker := r.path(devModeMarkerRel)
	_, markerErr := os.Stat(marker)
	markerExists := markerErr == nil

	if devMode {
		return r.secretsEnterDev(marker)
	}
	if markerExists {
		return r.secretsLeaveDev(marker)
	}
	return r.secretsRandomMint()
}

func (r *Runner) secretsEnterDev(marker string) error {
	doc, raw, err := r.loadActiveDevCredentials()
	if err != nil {
		return err
	}
	prev, prevErr := parseDevModeMarkerFile(marker)
	if prevErr != nil && !os.IsNotExist(prevErr) {
		return prevErr
	}
	ch, err := r.applyDevCatalog(doc)
	if err != nil {
		return err
	}
	if err := r.labldapSetupsecrets(false); err != nil {
		return err
	}
	if err := os.MkdirAll(r.path("secrets"), 0o755); err != nil {
		return err
	}
	sf := r.path(taclabSecretsFromRel)
	if err := taclabcfg.WriteSecretsFrom(sf, taclabCatalog(doc)); err != nil {
		return err
	}
	fmt.Println("wrote secrets/taclab-secrets-from.yaml")
	if err := r.generateTaclabLab(false, sf); err != nil {
		return err
	}
	tac, err := r.applyDevTaclabSecrets(doc)
	if err != nil {
		return err
	}
	ch.labtacacs = tac.Changed
	ch.labtacacsAdmin = tac.APIAdminChanged

	newHash := sha256Hex(raw)
	if prev.catalogSHA != "" && prev.catalogSHA != newHash {
		fmt.Printf("dev-credentials.yaml changed; reconciled %d files\n", ch.count())
	}
	// Sample before flipping the marker to pending so a finished enter-dev
	// stays a no-op when plaintext already matches.
	forceConsumers := os.IsNotExist(prevErr) || prev.reloads != reloadsDone
	if err := writeDevModeMarker(marker, r.Prof.Name, newHash, reloadsPending); err != nil {
		return err
	}

	tls, err := r.ensureLabLDAPTLS()
	if err != nil {
		return err
	}
	if err := r.persistLabLDAPTLSReloadIf(tls.leavesRewritten()); err != nil {
		return err
	}
	if err := r.stageLabinfoCreds(); err != nil {
		return err
	}
	reload := ch
	if forceConsumers {
		reload = enterDevReloadChanges()
	}
	if tls.leavesRewritten() || r.labldapTLSReloadPending() {
		reload.labldapTLS = true
	}
	if err := r.applySecretReloads(reload, false); err != nil {
		return err
	}
	if err := writeDevModeMarker(marker, r.Prof.Name, newHash, reloadsDone); err != nil {
		return err
	}
	fmt.Println("secrets ready")
	return nil
}

func (r *Runner) secretsLeaveDev(marker string) error {
	if err := r.leaveDevRemint(); err != nil {
		return err
	}
	tls, err := r.ensureLabLDAPTLS()
	if err != nil {
		return err
	}
	if err := r.persistLabLDAPTLSReloadIf(tls.leavesRewritten()); err != nil {
		return err
	}
	if err := r.stageLabinfoCreds(); err != nil {
		return err
	}
	if err := r.applySecretReloads(secretChanges{labldapTLS: tls.leavesRewritten() || r.labldapTLSReloadPending()}, true); err != nil {
		return err
	}
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("secrets ready")
	return nil
}

func (r *Runner) secretsRandomMint() error {
	if err := writeTokenIfMissing(r.path("secrets/labdns-token"), 0o644); err != nil {
		return err
	}
	if err := writeTokenIfMissing(r.path("secrets/mcp-client-token"), 0o600); err != nil {
		return err
	}
	if err := writeTokenIfMissing(r.path("secrets/labinfo-token"), 0o644); err != nil {
		return err
	}
	if err := writeTokenIfMissing(r.path("secrets/labmail-token"), 0o644); err != nil {
		return err
	}
	if err := writeTokenIfMissing(r.path("secrets/maildev-web-password"), 0o644); err != nil {
		return err
	}
	if err := writeTokenIfMissing(r.path("secrets/labmitm-token"), 0o644); err != nil {
		return err
	}
	if err := r.labldapSetupsecrets(false); err != nil {
		return err
	}
	tls, err := r.ensureLabLDAPTLS()
	if err != nil {
		return err
	}
	if err := r.persistLabLDAPTLSReloadIf(tls.leavesRewritten()); err != nil {
		return err
	}
	if err := r.generateTaclabLab(false, ""); err != nil {
		return err
	}
	if err := r.stageLabinfoCreds(); err != nil {
		return err
	}
	if tls.leavesRewritten() || r.labldapTLSReloadPending() {
		if err := r.applySecretReloads(secretChanges{labldapTLS: true}, false); err != nil {
			return err
		}
	}
	fmt.Println("secrets ready")
	return nil
}

func (r *Runner) loadActiveDevCredentials() (*DevCredentials, []byte, error) {
	path := filepath.Join(r.Prof.Dir, "dev-credentials.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("LAB_DEV_MODE=true requires %s: %w", path, err)
	}
	doc, err := LoadDevCredentials(path)
	if err != nil {
		return nil, nil, fmt.Errorf("LAB_DEV_MODE=true: %w", err)
	}
	return doc, b, nil
}

func (r *Runner) applyDevCatalog(doc *DevCredentials) (secretChanges, error) {
	var ch secretChanges
	type item struct {
		rel  string
		val  string
		mode os.FileMode
		flag *bool
	}
	ll := "third_party/go-lab-ldap-mcp/secrets/"
	items := []item{
		{"secrets/labdns-token", doc.Spec.Tokens.LabDNS, 0o644, &ch.labdnsToken},
		{"secrets/labinfo-token", doc.Spec.Tokens.Labinfo, 0o644, &ch.labinfoToken},
		{"secrets/labmail-token", doc.Spec.Tokens.Labmail, 0o644, &ch.labmailToken},
		{"secrets/mcp-client-token", doc.Spec.Tokens.MCPClient, 0o600, &ch.mcpClientToken},
		{"secrets/labmitm-token", doc.Spec.Tokens.LabMITM, 0o644, &ch.labmitmToken},
		{"secrets/maildev-web-password", doc.Spec.Passwords.MaildevWeb, 0o644, &ch.maildevWeb},
		{ll + "token-admin", doc.Spec.Tokens.LabLDAPAdmin, 0o600, &ch.labldapAdmin},
		{ll + "user-alice", doc.Spec.Passwords.LabLDAPAlice, 0o600, &ch.labldapAlice},
		{ll + "runtime-ldap", doc.Spec.Passwords.LabLDAPRuntime, 0o600, &ch.labldapRuntime},
		{ll + "dm.pw", doc.Spec.Passwords.LabLDAPDM, 0o600, &ch.labldapDM},
		{ll + "directory.env", "DS_DM_PASSWORD=" + doc.Spec.Passwords.LabLDAPDM, 0o600, &ch.labldapDM},
	}
	for _, it := range items {
		changed, err := writeSecretBytes(r.path(it.rel), it.val, it.mode)
		if err != nil {
			return ch, err
		}
		if changed {
			*it.flag = true
		}
	}
	return ch, nil
}

func (r *Runner) leaveDevRemint() error {
	for _, it := range []struct {
		rel  string
		mode os.FileMode
	}{
		{"secrets/labdns-token", 0o644},
		{"secrets/labinfo-token", 0o644},
		{"secrets/labmail-token", 0o644},
		{"secrets/maildev-web-password", 0o644},
		{"secrets/mcp-client-token", 0o600},
		{"secrets/labmitm-token", 0o644},
	} {
		if err := writeTokenAlways(r.path(it.rel), it.mode); err != nil {
			return err
		}
	}
	if err := r.labldapSetupsecrets(true); err != nil {
		return err
	}
	if err := os.Remove(r.path(taclabSecretsFromRel)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return r.generateTaclabLab(true, "")
}

func writeDevModeMarker(path, profileName, catalogSHA, reloads string) error {
	body := fmt.Sprintf("profile=%s\ncatalog-sha256=%s\ntimestamp=%s\nreloads=%s\n",
		profileName, catalogSHA, time.Now().UTC().Format(time.RFC3339), reloads)
	return os.WriteFile(path, []byte(body), 0o644)
}

func parseDevModeMarkerFile(path string) (devModeMarker, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return devModeMarker{}, err
	}
	return parseDevModeMarker(b), nil
}

func parseDevModeMarker(b []byte) devModeMarker {
	var m devModeMarker
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "profile":
			m.profile = v
		case "catalog-sha256":
			m.catalogSHA = v
		case "reloads":
			m.reloads = v
		}
	}
	return m
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (r *Runner) labldapSetupsecrets(force bool) error {
	if r.deps != nil && r.deps.setupsecrets != nil {
		return r.deps.setupsecrets(force)
	}
	args := []string{"run", "./tools/setupsecrets", "--dir", "secrets"}
	if force {
		args = append(args, "--force")
	}
	return r.run("third_party/go-lab-ldap-mcp", "go", args...)
}

func (r *Runner) ensureLabLDAPTLS() (labTLSResult, error) {
	if r.deps != nil && r.deps.ensureTLS != nil {
		return r.deps.ensureTLS()
	}
	dir := r.path("third_party/go-lab-ldap-mcp/secrets/tls")
	host := ""
	if r.Prof != nil {
		host = r.Prof.Get("LAB_PUBLIC_HOST", "localhost")
	}
	return labtlsEnsure(dir, host)
}

func (r *Runner) labldapTLSReloadPendingPath() string {
	return r.path(labldapTLSReloadPendingRel)
}

func (r *Runner) labldapTLSReloadPending() bool {
	_, err := os.Stat(r.labldapTLSReloadPendingPath())
	return err == nil
}

func (r *Runner) persistLabLDAPTLSReloadIf(rewritten bool) error {
	if !rewritten {
		return nil
	}
	path := r.labldapTLSReloadPendingPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("pending\n"), 0o644)
}

func (r *Runner) clearLabLDAPTLSReloadPending() error {
	if err := os.Remove(r.labldapTLSReloadPendingPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (r *Runner) generateTaclabLab(force bool, secretsFromAbs string) error {
	if r.deps != nil && r.deps.ensureTaclab != nil {
		return r.deps.ensureTaclab(force, secretsFromAbs)
	}
	return r.ensureTaclabLab(force, secretsFromAbs)
}

// labgenArgs is the argv after "go" for tools/labgen.
func labgenArgs(force bool, secretsFromAbs string) []string {
	args := []string{"run", "./tools/labgen"}
	if force {
		args = append(args, "-force")
	}
	if secretsFromAbs != "" {
		args = append(args, "-secrets-from", secretsFromAbs)
	}
	return append(args, "deployments/compose")
}

// ensureTaclabLab materializes TacLab's labgen bundle (configs, PKI, secrets)
// and turns on api.mcp.allow_legacy_clients so MCPJungle can connect. labgen
// is rerun with -force when the vendored checkout moves to a new tag, so a
// pin bump cannot leave a stale baseline behind. force also covers leave-dev.
// secretsFromAbs is -secrets-from when labgen actually runs in dev mode.
// Dev-mode catalog pinning still runs after this from secretsEnterDev.
func (r *Runner) ensureTaclabLab(force bool, secretsFromAbs string) error {
	ref, err := r.taclabVendorRef()
	if err != nil {
		return err
	}
	marker := r.path(taclabDir + "/" + taclabLabgenMarker)
	prev, _ := os.ReadFile(marker)
	need := force || strings.TrimSpace(string(prev)) != ref
	tokenPath := r.path(taclabDir + "/deployments/compose/secrets/api_admin_token")
	if _, err := os.Stat(tokenPath); err != nil {
		need = true
	}
	if need {
		labForce := force
		if !force {
			if _, err := os.Stat(tokenPath); err == nil {
				labForce = true
			}
		}
		if err := r.run(taclabDir, "go", labgenArgs(labForce, secretsFromAbs)...); err != nil {
			return err
		}
		if err := os.WriteFile(marker, []byte(ref+"\n"), 0o644); err != nil {
			return err
		}
	}
	if err := taclabcfg.EnableLegacyClientsDir(r.path(taclabDir + "/deployments/compose/config")); err != nil {
		return fmt.Errorf("enable TacLab MCP legacy clients: %w", err)
	}
	return nil
}

func taclabCatalog(doc *DevCredentials) taclabcfg.Catalog {
	return taclabcfg.Catalog{
		APIAdminToken:    doc.Spec.Tokens.LabTacacsAdmin,
		TacacsSecret:     doc.Spec.SharedSecrets.TacacsLabSwitches,
		RadiusSecret:     doc.Spec.SharedSecrets.RadiusLabSwitches,
		AdminPassword:    doc.Spec.Passwords.TaclabAdmin,
		AdminEnable:      doc.Spec.Passwords.TaclabAdminEnable,
		ReadonlyPassword: doc.Spec.Passwords.TaclabReadonly,
		DisabledPassword: doc.Spec.Passwords.TaclabDisabled,
		ChallengeSecret:  doc.Spec.Passwords.TaclabChallenge,
	}
}

func (r *Runner) applyDevTaclabSecrets(doc *DevCredentials) (taclabcfg.ApplyResult, error) {
	params := taclabcfg.DefaultParams
	entropy := rand.Reader
	if r.deps != nil && r.deps.taclabArgon2Params != nil {
		params = *r.deps.taclabArgon2Params
	}
	if r.deps != nil && r.deps.taclabEntropy != nil {
		entropy = r.deps.taclabEntropy
	}
	return taclabcfg.ApplyDevSecrets(r.path(taclabDir+"/deployments/compose/secrets"), taclabCatalog(doc), params, entropy)
}

const taclabLabgenMarker = "deployments/compose/.mcplab-labgen-ref"

func (r *Runner) taclabVendorRef() (string, error) {
	out, err := r.capture(".", "git", "-C", r.path(taclabDir), "describe", "--tags", "--always")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// labinfoStageCopy is one always-on copy into secrets/labinfo-creds/.
// Optional sources (TacLab client certs) are skipped if missing; required
// sources fail closed so catalog file: entries cannot dangle.
type labinfoStageCopy struct {
	src      string
	dst      string
	optional bool
}

// labinfoCredFiles is the copy table for stageLabinfoCreds. Dest names
// must stay lockstep with catalog file: basenames (plus labinfo-token,
// which is inbound auth for the labinfo container, not a catalog service).
var labinfoCredFiles = []labinfoStageCopy{
	{src: "secrets/labinfo-token", dst: "labinfo-token"},
	{src: "secrets/labdns-token", dst: "labdns-token"},
	{src: "secrets/mcp-client-token", dst: "mcp-client-token"},
	{src: "secrets/labmail-token", dst: "labmail-token"},
	{src: "secrets/maildev-web-password", dst: "maildev-web-password"},
	{src: "secrets/labmitm-token", dst: "labmitm-token"},
	{src: "third_party/go-lab-ldap-mcp/secrets/token-admin", dst: "labldap-token-admin"},
	{src: "third_party/go-lab-ldap-mcp/secrets/user-alice", dst: "labldap-user-alice"},
	{src: "third_party/go-lab-ldap-mcp/secrets/tls/ca.crt", dst: "labldap-ca.crt"},
	{src: taclabDir + "/deployments/compose/secrets/api_admin_token", dst: "labtacacs-token-admin"},
	{src: taclabDir + "/deployments/compose/secrets/lab_switches_radius_secret", dst: "labtacacs-radius-secret"},
	{src: taclabDir + "/deployments/compose/secrets/lab_switches_tacacs_secret", dst: "labtacacs-tacacs-secret"},
	{src: taclabDir + "/deployments/compose/certs-public/client-ca.pem", dst: "tacacs-client-ca.pem", optional: true},
	{src: taclabDir + "/deployments/compose/certs-public/client-ok.pem", dst: "tacacs-client-ok.pem", optional: true},
}

const labinfoPasswordsSrc = taclabDir + "/deployments/compose/secrets/PASSWORDS.txt"

// labinfoPasswordKeys splits labgen PASSWORDS.txt (written in both modes)
// into staged files. Catalog file: entries use lab-admin, lab-admin-enable,
// and lab-readonly.
var labinfoPasswordKeys = []struct{ key, dst string }{
	{"lab-admin", "labtacacs-lab-admin"},
	{"lab-admin-enable", "labtacacs-lab-admin-enable"},
	{"lab-readonly", "labtacacs-lab-readonly"},
	{"lab-disabled", "labtacacs-lab-disabled"},
	{"lab-admin-challenge", "labtacacs-lab-admin-challenge"},
}

// stageLabinfoCreds copies credentials labinfo may reveal into
// secrets/labinfo-creds/ (0o644, uid 65532). Always-on so catalog file:
// paths exist after mcplab secrets in both modes; reveal stays gated on
// LAB_DEV_MODE inside labinfo.
func (r *Runner) stageLabinfoCreds() error {
	dir := r.path("secrets/labinfo-creds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range labinfoCredFiles {
		b, err := os.ReadFile(r.path(f.src))
		if err != nil {
			if f.optional && os.IsNotExist(err) {
				dst := filepath.Join(dir, f.dst)
				if rmErr := os.Remove(dst); rmErr != nil && !os.IsNotExist(rmErr) {
					return fmt.Errorf("stage labinfo creds: %w", rmErr)
				}
				continue
			}
			return fmt.Errorf("stage labinfo creds: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.dst), b, 0o644); err != nil {
			return err
		}
	}
	return r.stageLabinfoPasswords(dir)
}

func (r *Runner) stageLabinfoPasswords(dir string) error {
	src := r.path(labinfoPasswordsSrc)
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("stage labinfo creds: %w", err)
	}
	pw := parseLabgenPasswords(b)
	for _, k := range labinfoPasswordKeys {
		v, ok := pw[k.key]
		if !ok {
			return fmt.Errorf("stage labinfo creds: %s: no entry for %q", src, k.key)
		}
		if err := os.WriteFile(filepath.Join(dir, k.dst), []byte(v+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// parseLabgenPasswords reads labgen PASSWORDS.txt `name=value` lines.
func parseLabgenPasswords(b []byte) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func writeTokenIfMissing(path string, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return os.Chmod(path, mode)
	}
	return mintTokenFile(path, mode, "wrote %s\n")
}

func writeTokenAlways(path string, mode os.FileMode) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return mintTokenFile(path, mode, "rotated %s (left dev mode)\n")
}

func mintTokenFile(path string, mode os.FileMode, msg string) error {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(buf)+"\n"), mode); err != nil {
		return err
	}
	fmt.Printf(msg, path)
	return nil
}

func writeSecretBytes(path, value string, mode os.FileMode) (bool, error) {
	desired := []byte(value + "\n")
	existing, err := os.ReadFile(path)
	switch {
	case err == nil && bytes.Equal(existing, desired):
		if err := os.Chmod(path, mode); err != nil {
			return false, err
		}
		fmt.Printf("skipped %s (exists)\n", path)
		return false, nil
	case err != nil && !os.IsNotExist(err):
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, desired, mode); err != nil {
		return false, err
	}
	if err := os.Chmod(path, mode); err != nil {
		return false, err
	}
	if os.IsNotExist(err) {
		fmt.Printf("wrote %s\n", path)
	} else {
		fmt.Printf("reconciled %s (dev catalog)\n", path)
	}
	return true, nil
}

func (r *Runner) alreadyReloaded(name string) bool {
	return r.reloadedThisRun[name]
}

func (r *Runner) markReloaded(name string) {
	if r.reloadedThisRun == nil {
		r.reloadedThisRun = map[string]bool{}
	}
	r.reloadedThisRun[name] = true
}

func (r *Runner) serviceExists(name string) (bool, error) {
	if r.deps != nil && r.deps.containerExists != nil {
		return r.deps.containerExists(name)
	}
	var (
		out string
		err error
	)
	switch name {
	case "labdns", "maildev", "labinfo", "mcpjungle", "labmitm":
		out, err = r.capture(".", "docker", "compose", "ps", "-aq", name)
	case "labldap":
		out, err = r.capture(".", "docker", r.labldapComposeArgs("ps", "-aq", "directory")...)
	case "labtacacs":
		out, err = r.capture(".", "docker", r.labtacacsComposeArgs("ps", "-aq")...)
	default:
		return false, fmt.Errorf("internal: unknown service %q", name)
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", name, err)
	}
	return strings.TrimSpace(out) != "", nil
}

func (r *Runner) applySecretReloads(ch secretChanges, leaveDev bool) error {
	if err := r.reloadMainIf("labdns", leaveDev || ch.labdnsToken); err != nil {
		return err
	}
	if err := r.reloadMainIf("maildev", leaveDev || ch.labmailToken || ch.maildevWeb); err != nil {
		return err
	}
	if err := r.reloadMainIf("labinfo", leaveDev || ch.labinfoToken); err != nil {
		return err
	}
	if err := r.reloadMainIf("labmitm", leaveDev || ch.labmitmToken); err != nil {
		return err
	}

	registrar := leaveDev || ch.registrarEnv()
	if registrar {
		ok, err := r.serviceExists("mcpjungle")
		if err != nil {
			return err
		}
		if ok {
			if leaveDev || ch.mcpClientToken {
				if err := r.doReloadGateway(); err != nil {
					return err
				}
			} else if err := r.doRegister(); err != nil {
				return err
			}
		}
	}

	if leaveDev || ch.labldap() || r.labldapTLSReloadPending() {
		ok, err := r.serviceExists("labldap")
		if err != nil {
			return err
		}
		if ok {
			if err := r.doReloadLabLDAP(); err != nil {
				return err
			}
		}
		if err := r.clearLabLDAPTLSReloadPending(); err != nil {
			return err
		}
	}
	if leaveDev || ch.labtacacsReload() {
		ok, err := r.serviceExists("labtacacs")
		if err != nil {
			return err
		}
		if ok {
			if err := r.doReloadLabTacacs(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Runner) reloadMainIf(service string, needed bool) error {
	if !needed {
		return nil
	}
	ok, err := r.serviceExists(service)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return r.doReloadMain(service)
}

func (r *Runner) doReloadMain(service string) error {
	fmt.Printf("secrets: reload %s\n", service)
	var err error
	if r.deps != nil && r.deps.reloadMain != nil {
		err = r.deps.reloadMain(service)
	} else {
		err = r.reloadMain(service)
	}
	if err != nil {
		return err
	}
	r.markReloaded(service)
	return nil
}

func (r *Runner) doReloadGateway() error {
	fmt.Println("secrets: reload mcpjungle")
	var err error
	if r.deps != nil && r.deps.reloadGateway != nil {
		err = r.deps.reloadGateway()
	} else {
		if err = r.EnsureNetwork(); err == nil {
			if err = r.reloadMain("mcpjungle"); err == nil {
				err = r.Register()
			}
		}
	}
	if err != nil {
		return err
	}
	r.markReloaded("mcpjungle")
	r.markReloaded("register")
	return nil
}

func (r *Runner) doRegister() error {
	if r.alreadyReloaded("register") || r.alreadyReloaded("mcpjungle") {
		return nil
	}
	fmt.Println("secrets: register (registrarEnv tokens changed)")
	var err error
	if r.deps != nil && r.deps.register != nil {
		err = r.deps.register()
	} else {
		err = r.Register()
	}
	if err != nil {
		return err
	}
	r.markReloaded("register")
	return nil
}

func (r *Runner) doReloadLabLDAP() error {
	fmt.Println("secrets: reload labldap")
	var err error
	if r.deps != nil && r.deps.reloadLabLDAP != nil {
		err = r.deps.reloadLabLDAP()
	} else {
		err = r.reloadLabLDAP()
	}
	if err != nil {
		return err
	}
	r.markReloaded("labldap")
	return nil
}

func (r *Runner) doReloadLabTacacs() error {
	fmt.Println("secrets: reload labtacacs")
	var err error
	if r.deps != nil && r.deps.reloadLabTacacs != nil {
		err = r.deps.reloadLabTacacs()
	} else {
		err = r.reloadLabTacacs()
	}
	if err != nil {
		return err
	}
	r.markReloaded("labtacacs")
	return nil
}
