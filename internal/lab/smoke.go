package lab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hilather/mcp-integration-lab/internal/labinfo"
	"github.com/hilather/mcp-integration-lab/internal/mcpout"
	"github.com/hilather/mcp-integration-lab/internal/profile"
	"github.com/hilather/mcp-integration-lab/internal/radius"
)

// Smoke runs the end-to-end scenario: drives DNS, LDAP, TACACS+/RADIUS,
// mail, and LabMITM state through the MCP gateway the way an agent would,
// then verifies every result on the real data plane (DNS query, LDAPS bind,
// kernel NFS mount, RADIUS PAP auth, SMTP delivery into the receive-only
// mail sink, HTTP intercept via the published proxy port).
func (r *Runner) Smoke() error {
	s := &smokeState{r: r}

	s.step("gateway health")
	port := r.Prof.Get("MCP_GATEWAY_PORT", "8080")
	s.check(waitHealthy("http://127.0.0.1:"+port+"/health", 10*time.Second) == nil, "gateway /health")

	s.dnsScenario()
	s.ldapScenario()
	s.labldapHostAllowListMerge()
	s.nfsScenario()
	s.tacacsScenario()
	s.taclabTokenEncoding()
	s.maildevScenario()
	s.labmitmScenario()
	s.labinfoScenario()
	s.devCatalogScenario()

	fmt.Printf("\n== smoke summary: %d passed, %d failed\n", s.pass, s.fail)
	if s.fail > 0 {
		return fmt.Errorf("%d smoke check(s) failed", s.fail)
	}
	return nil
}

type smokeState struct {
	r          *Runner
	pass, fail int
}

func (s *smokeState) devMode() bool {
	return profile.IsTrue(s.r.Prof.Get("LAB_DEV_MODE", "false"))
}

func (s *smokeState) step(name string) { fmt.Printf("\n== %s\n", name) }

func (s *smokeState) check(ok bool, what string) bool {
	if ok {
		s.pass++
		fmt.Printf("   OK: %s\n", what)
	} else {
		s.fail++
		fmt.Printf("   FAIL: %s\n", what)
	}
	return ok
}

// invoke calls a tool through the gateway via the registrar CLI service and
// returns the JSON text content.
func (s *smokeState) invoke(tool, input string) (string, error) {
	tokens, err := s.r.loadTokens()
	if err != nil {
		return "", err
	}
	env := s.r.registrarEnv(tokens)
	out, err := s.r.captureWithEnv(env, "docker",
		"compose", "run", "--rm", "-T", "--no-deps", "--quiet-pull",
		"registrar", "--registry", "http://mcpjungle:8080",
		"invoke", tool, "--input", input)
	if err != nil {
		return "", err
	}
	return mcpout.ExtractText(out)
}

func (s *smokeState) ldapsearch(script string) (string, error) {
	secrets := filepath.Join(s.r.Root, "third_party", "go-lab-ldap-mcp", "secrets")
	return s.r.capture(".", "docker", "run", "--rm", "--network", "mcplab-shared",
		"-v", secrets+":/s:ro", "-e", "LDAPTLS_CACERT=/s/tls/ca.crt",
		"--entrypoint", "sh", "labldap-bootstrap:dev", "-c", script)
}

// lookup resolves name against the lab DNS on its published host port.
func (s *smokeState) lookup(name string) ([]string, error) {
	dnsPort := s.r.Prof.Get("LABDNS_DNS_PORT", "10053")
	res := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", dnsPort))
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return res.LookupHost(ctx, name)
}

func (s *smokeState) dnsScenario() {
	s.step("DNS scenario: agent adds a record over MCP, resolver sees it, reset wipes it")

	stateOut, err := s.invoke("labdns__dns_state_get", "{}")
	var state struct {
		RuntimeRevision string `json:"runtimeRevision"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(stateOut), &state)
	}
	if !s.check(err == nil && state.RuntimeRevision != "", "read runtime revision") {
		return
	}

	apply := fmt.Sprintf(`{"expectedRevision":%q,"reason":"smoke: add smoke-a","operations":[{"op":"add","target":{"kind":"record","id":"smoke-a","zoneId":"lab-zone"},"value":{"id":"smoke-a","owner":"smoke","type":"A","values":["10.42.0.99"]}}]}`, state.RuntimeRevision)
	_, err = s.invoke("labdns__dns_change_apply", apply)
	s.check(err == nil, "dns_change_apply smoke-a")

	addrs, err := s.lookup("smoke.lab.test")
	s.check(err == nil && len(addrs) == 1 && addrs[0] == "10.42.0.99",
		fmt.Sprintf("resolve smoke.lab.test -> %v", addrs))

	_, err = s.invoke("labdns__dns_state_reset", `{"reason":"smoke: back to bootstrap"}`)
	s.check(err == nil, "dns_state_reset")

	addrs, err = s.lookup("smoke.lab.test")
	s.check(err != nil || len(addrs) == 0,
		fmt.Sprintf("smoke.lab.test gone after reset (got %v err=%v)", addrs, err))
}

func (s *smokeState) ldapScenario() {
	s.step("LDAP scenario: agent creates a user over MCP, real LDAPS bind sees it")

	// Unique per run: the directory is ephemeral (wiped on restart), and this
	// keeps repeat smoke runs independent.
	user := fmt.Sprintf("smoke%d", time.Now().Unix())
	pwBytes := make([]byte, 12)
	if _, err := rand.Read(pwBytes); err != nil {
		s.check(false, "generate password")
		return
	}
	password := "smoke-" + hex.EncodeToString(pwBytes)

	createOut, err := s.invoke("labldap__ldap_create_user",
		fmt.Sprintf(`{"id":%q,"password":%q}`, user, password))
	var created struct {
		ID       string `json:"id"`
		Revision string `json:"revision"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(createOut), &created)
	}
	if !s.check(err == nil && created.ID == user, "ldap_create_user "+user) {
		return
	}

	acctOut, err := s.invoke("labldap__ldap_get_account_state", fmt.Sprintf(`{"id":%q}`, user))
	s.check(err == nil && strings.Contains(acctOut, `"enabled":true`), "ldap_get_account_state for "+user)

	// Data-plane checks from inside the docker network (the lab LDAPS cert
	// always has DNS SAN directory; LAB_PUBLIC_HOST is an extra SAN for
	// remote clients). Alice has suffix read via the staff ACL.
	out, err := s.ldapsearch(fmt.Sprintf(
		`ldapsearch -x -H ldaps://directory:3636 -D "uid=alice,ou=people,dc=example,dc=test" -w "$(cat /s/user-alice)" -b "dc=example,dc=test" "(uid=%s)" uid`, user))
	s.check(err == nil && strings.Contains(out, "uid: "+user), "ldapsearch found "+user+" via LDAPS")

	_, err = s.ldapsearch(fmt.Sprintf(
		`ldapsearch -x -H ldaps://directory:3636 -D "uid=%s,ou=people,dc=example,dc=test" -w "%s" -b "uid=%s,ou=people,dc=example,dc=test" -s base dn`,
		user, password, user))
	s.check(err == nil, user+" can bind with the agent-set password")

	if created.Revision != "" {
		_, err = s.invoke("labldap__ldap_delete_user",
			fmt.Sprintf(`{"id":%q,"confirm":true,"revision":%q}`, user, created.Revision))
		s.check(err == nil, "ldap_delete_user cleanup")
	} else {
		fmt.Printf("   note: no revision in create response; leaving %s (ephemeral store)\n", user)
	}
}

func (s *smokeState) labldapHostAllowListMerge() {
	s.step("LabLDAP overlay Host allow-list merge")

	env, err := s.r.labldapMergedControlEnv()
	if !s.check(err == nil, "labldap compose config (stacked files)") {
		if err != nil {
			fmt.Printf("   %v\n", err)
		}
		return
	}
	for _, k := range []string{"LABLDAP_LDAP_URL", "LABLDAP_DIRECTORY_HOST", "LABLDAP_DIRECTORY_CA_FILE"} {
		s.check(env[k] != "", "merged control.environment keeps "+k)
	}
	want := s.r.Prof.Get("LAB_PUBLIC_HOST", "localhost")
	s.check(env[labldapAllowedHostsEnv] == want,
		fmt.Sprintf("merged %s=%s (localhost is already a LoopbackHost; not extra-hostname proof)", labldapAllowedHostsEnv, env[labldapAllowedHostsEnv]))
}

func (s *smokeState) nfsScenario() {
	s.step("NFS scenario: kernel client mounts the empty-root tar.zst export and writes via the overlay")

	if _, err := s.r.capture(".", "docker", "build", "-q", "-t", "mcplab-nfs-client", "docker/nfs-client"); err != nil {
		s.check(false, "build nfs client image: "+err.Error())
		return
	}
	out, err := s.r.capture(".", "docker", "run", "--rm", "--privileged", "--network", "mcplab_default",
		"mcplab-nfs-client", "bash", "-c",
		`mkdir -p /m && mount -t nfs -o vers=3,tcp,nolock,port=20490,mountport=20490 nfs:/ /m >/dev/null 2>&1 && echo nfs-write-ok > /m/smoke-write.txt && cat /m/smoke-write.txt && umount /m`)
	wrote := strings.TrimSpace(out)
	if i := strings.LastIndex(wrote, "\n"); i >= 0 {
		wrote = strings.TrimSpace(wrote[i+1:])
	}
	s.check(err == nil && wrote == "nfs-write-ok", fmt.Sprintf("NFS overlay write (%s)", wrote))
}

func (s *smokeState) tacacsScenario() {
	s.step("TacLab scenario: agent reads AAA state over MCP, real RADIUS PAP auth succeeds")

	out, err := s.invoke("labtacacs__taclab.users.list", "{}")
	if !s.check(err == nil && strings.Contains(out, "lab-admin"), "taclab.users.list shows lab-admin") {
		return
	}

	statusOut, err := s.invoke("labtacacs__taclab.system.status.get", "{}")
	s.check(err == nil && (strings.Contains(statusOut, "legacy") || strings.Contains(statusOut, "radius") || strings.Contains(statusOut, "listener")),
		"taclab.system.status.get reports listeners")

	// Data plane: PAP Access-Request on the published RADIUS port with the
	// labgen shared secret and lab-admin's generated password.
	secretsDir := taclabDir + "/deployments/compose/secrets"
	secret, err := readTrimmed(s.r.path(secretsDir + "/lab_switches_radius_secret"))
	if err != nil {
		s.check(false, "read RADIUS shared secret: "+err.Error())
		return
	}
	password, err := labgenPassword(s.r.path(secretsDir+"/PASSWORDS.txt"), "lab-admin")
	if err != nil {
		s.check(false, "read lab-admin password: "+err.Error())
		return
	}

	port := s.r.Prof.Get("TACLAB_RADIUS_ACCESS_PORT", "1812")
	code, err := radiusAuth("127.0.0.1:"+port, "lab-admin", password, secret)
	s.check(err == nil && code == radius.CodeAccessAccept,
		fmt.Sprintf("RADIUS Access-Accept for lab-admin on udp/%s (code=%d, err=%v)", port, code, err))

	code, err = radiusAuth("127.0.0.1:"+port, "lab-admin", "wrong-password", secret)
	s.check(err == nil && code == radius.CodeAccessReject, "RADIUS rejects a wrong password")
}

const (
	tokenEncodingUnpadded = "unpadded-base64url"
	tokenEncodingCaller   = "caller-supplied"
)

// expectTokenEncoding checks labgen's manifest token_encoding.
// Non-dev requires unpadded-base64url. CI first-mint-in-dev (GITHUB_ACTIONS
// + PROFILE=ci-dev) requires caller-supplied. Other dev smokes accept either
// known encoding (enter-dev on an existing random baseline skips labgen).
func expectTokenEncoding(dev, ciDevSmoke bool, got string) error {
	switch {
	case !dev:
		if got != tokenEncodingUnpadded {
			return fmt.Errorf("token_encoding = %q, want %s (non-dev)", got, tokenEncodingUnpadded)
		}
	case ciDevSmoke:
		if got != tokenEncodingCaller {
			return fmt.Errorf("token_encoding = %q, want %s (CI first-mint-in-dev)", got, tokenEncodingCaller)
		}
	default:
		if got != tokenEncodingCaller && got != tokenEncodingUnpadded {
			return fmt.Errorf("token_encoding = %q, want %s or %s", got, tokenEncodingCaller, tokenEncodingUnpadded)
		}
	}
	return nil
}

func (s *smokeState) taclabTokenEncoding() {
	s.step("TacLab labgen token_encoding")
	b, err := os.ReadFile(s.r.path(taclabDir + "/deployments/compose/manifest.json"))
	if !s.check(err == nil, fmt.Sprintf("read TacLab manifest.json (err=%v)", err)) {
		return
	}
	var man struct {
		TokenEncoding string `json:"token_encoding"`
	}
	err = json.Unmarshal(b, &man)
	if !s.check(err == nil && man.TokenEncoding != "", fmt.Sprintf("decode token_encoding (err=%v value=%q)", err, man.TokenEncoding)) {
		return
	}
	dev := s.devMode()
	ciDev := os.Getenv("GITHUB_ACTIONS") == "true" && s.r.Prof.Name == "ci-dev"
	err = expectTokenEncoding(dev, ciDev, man.TokenEncoding)
	s.check(err == nil, fmt.Sprintf("token_encoding=%s (dev=%v ci-dev=%v err=%v)", man.TokenEncoding, dev, ciDev, err))
}

func (s *smokeState) maildevScenario() {
	s.step("maildev scenario: SMTP ingest on the published port, REST shows the mail, web auth is on")

	smtpPort := s.r.Prof.Get("MAILDEV_SMTP_PORT", "1025")
	webPort := s.r.Prof.Get("MAILDEV_WEB_PORT", "1080")
	subject := fmt.Sprintf("mcplab smoke %d", time.Now().UnixNano())

	msg := strings.Join([]string{
		"From: smoke@lab.test",
		"To: inbox@lab.test",
		"Subject: " + subject,
		"",
		"delivered through the receive-only lab sink",
	}, "\r\n")
	err := smtp.SendMail("127.0.0.1:"+smtpPort, nil, "smoke@lab.test", []string{"inbox@lab.test"}, []byte(msg))
	if !s.check(err == nil, fmt.Sprintf("SMTP accepted mail on :%s (err=%v)", smtpPort, err)) {
		return
	}

	user := s.r.Prof.Get("MAILDEV_WEB_USER", "admin")
	pass, err := readTrimmed(s.r.path("secrets/maildev-web-password"))
	if err != nil {
		s.check(false, "read maildev web password: "+err.Error())
		return
	}

	// Without credentials the web/REST surface must refuse.
	resp, err := http.Get("http://127.0.0.1:" + webPort + "/email")
	if err == nil {
		resp.Body.Close()
	}
	s.check(err == nil && resp.StatusCode == http.StatusUnauthorized,
		"REST API requires basic auth")

	// With credentials the captured mail shows up (poll briefly: delivery to
	// the store is asynchronous).
	found := false
	deadline := time.Now().Add(10 * time.Second)
	for !found && time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:"+webPort+"/email", nil)
		req.SetBasicAuth(user, pass)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var mails []struct {
				Subject string `json:"subject"`
			}
			if json.NewDecoder(resp.Body).Decode(&mails) == nil {
				for _, m := range mails {
					if m.Subject == subject {
						found = true
					}
				}
			}
			resp.Body.Close()
		}
		if !found {
			time.Sleep(500 * time.Millisecond)
		}
	}
	s.check(found, "captured mail visible via authenticated REST")

	// MCP wait is additive: keep the three REST/SMTP assertions above
	// unchanged (TestMaildevScenarioCompat twin in go-lab-maildev).
	waitIn := fmt.Sprintf(`{"filter":{"subjectContains":%q},"timeout":"10s"}`, subject)
	waitOut, err := s.invoke("labmail__mail_messages_wait", waitIn)
	s.check(err == nil && strings.Contains(waitOut, subject),
		fmt.Sprintf("mail_messages_wait sees captured mail (err=%v)", err))
}

// flowListJSON is LabMITM native GET /v1/flows — an object with items, not a
// raw array (the opt-in compat spelling).
type flowListJSON struct {
	Items []json.RawMessage `json:"items"`
}

func (s *smokeState) labmitmScenario() {
	s.step("labmitm scenario: host-side ProxyURL GET is captured; management is bearer-only")

	webPort := s.r.Prof.Get("LABMITM_WEB_PORT", "18088")
	proxyPort := s.r.Prof.Get("LABMITM_PROXY_PORT", "18888")

	// Direct checks must not inherit HTTP_PROXY/HTTPS_PROXY.
	direct := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		},
	}

	resp, err := direct.Get("http://127.0.0.1:" + webPort + "/v1/flows")
	if err == nil {
		resp.Body.Close()
	}
	s.check(err == nil && resp.StatusCode == http.StatusUnauthorized,
		"REST /v1/flows requires bearer")

	token, err := readTrimmed(s.r.path("secrets/labmitm-token"))
	if err != nil {
		s.check(false, "read labmitm token: "+err.Error())
		return
	}
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+webPort+"/v1/flows", nil)
	if err != nil {
		s.check(false, "build bearer request: "+err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = direct.Do(req)
	var list flowListJSON
	if err == nil {
		decErr := json.NewDecoder(resp.Body).Decode(&list)
		resp.Body.Close()
		if decErr != nil {
			err = decErr
		}
	}
	s.check(err == nil && resp.StatusCode == http.StatusOK,
		"bearer GET /v1/flows returns {items:...}")

	proxyURL, err := url.Parse("http://127.0.0.1:" + proxyPort)
	if err != nil {
		s.check(false, "parse proxy URL: "+err.Error())
		return
	}
	viaProxy := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	live, err := viaProxy.Get("http://labdns:8080/v1/health/live")
	if err == nil {
		live.Body.Close()
	}
	if !s.check(err == nil && live.StatusCode == http.StatusOK,
		fmt.Sprintf("proxied GET labdns /v1/health/live via :%s (err=%v)", proxyPort, err)) {
		return
	}

	waitOut, err := s.invoke("labmitm__mitm_flows_wait", `{"filter":{"host":"labdns"},"timeout":"10s"}`)
	s.check(err == nil && strings.Contains(waitOut, "labdns"),
		fmt.Sprintf("mitm_flows_wait sees labdns flow (err=%v)", err))

	// 1.3 hop/accept catalog is discovered at Register() during make up.
	// make reload APP=labmitm does not re-register (gateway SQLite is tmpfs
	// — only mcpjungle reload / make up / make register refresh the tool list).
	featOut, err := s.invoke("labmitm__mitm_features_list", `{}`)
	s.check(err == nil,
		fmt.Sprintf("mitm_features_list is registered (err=%v out=%q)", err, featOut))
}

// radiusAuth sends one PAP Access-Request and returns the verified reply code.
func radiusAuth(addr, user, password, secret string) (byte, error) {
	var authenticator [16]byte
	if _, err := rand.Read(authenticator[:]); err != nil {
		return 0, err
	}
	id := make([]byte, 1)
	if _, err := rand.Read(id); err != nil {
		return 0, err
	}
	req, err := radius.BuildAccessRequest(user, password, secret, id[0], authenticator)
	if err != nil {
		return 0, err
	}

	conn, err := net.Dial("udp", addr)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	if _, err := conn.Write(req); err != nil {
		return 0, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return 0, err
	}
	return radius.VerifyResponse(buf[:n], req, secret)
}

// labgenPassword pulls one plaintext lab credential ("name=value" lines)
// from labgen's PASSWORDS.txt.
func labgenPassword(path, name string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	pw := parseLabgenPasswords(b)
	v, ok := pw[name]
	if !ok {
		return "", fmt.Errorf("%s: no entry for %q", path, name)
	}
	return v, nil
}

func readTrimmed(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (s *smokeState) labinfoScenario() {
	s.step("labinfo scenario: agents can look up user-facing service URLs")

	out, err := s.invoke("labinfo__endpoints_list", "{}")
	var eps struct {
		DevMode  bool `json:"devMode"`
		Services []struct {
			ID   string `json:"id"`
			URLs []struct {
				URL string `json:"url"`
			} `json:"urls"`
			Credential *struct {
				Secret string `json:"secret"`
			} `json:"credential"`
		} `json:"services"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(out), &eps)
	}
	if !s.check(err == nil && len(eps.Services) > 0, "endpoints_list returns the catalog") {
		return
	}

	devMode := s.devMode()
	s.check(eps.DevMode == devMode, fmt.Sprintf("devMode flag matches profile (%v)", devMode))

	gatewayPort := s.r.Prof.Get("MCP_GATEWAY_PORT", "8080")
	mitmWebPort := s.r.Prof.Get("LABMITM_WEB_PORT", "18088")
	foundGateway := false
	foundMITM := false
	credentialsRevealed := false
	for _, svc := range eps.Services {
		if svc.ID == "gateway" {
			for _, u := range svc.URLs {
				if strings.Contains(u.URL, ":"+gatewayPort+"/mcp") {
					foundGateway = true
				}
			}
		}
		if svc.ID == "labmitm" {
			for _, u := range svc.URLs {
				if strings.Contains(u.URL, ":"+mitmWebPort) {
					foundMITM = true
				}
			}
		}
		if svc.Credential != nil && svc.Credential.Secret != "" {
			credentialsRevealed = true
		}
	}
	s.check(foundGateway, "gateway URL carries the profile's public port")
	s.check(foundMITM, "labmitm catalog URL carries the profile's web port")
	s.check(credentialsRevealed == devMode,
		fmt.Sprintf("credentials revealed only in dev mode (revealed=%v)", credentialsRevealed))

	// Protocol-level connection details: every cataloged service must tell
	// agents how to point a client at it (endpoints + parameters), with
	// connection secrets gated on dev mode exactly like the web credentials.
	out, err = s.invoke("labinfo__connections_list", "{}")
	var conns struct {
		Services []struct {
			ID        string `json:"id"`
			Endpoints []struct {
				Protocol string `json:"protocol"`
				Address  string `json:"address"`
			} `json:"endpoints"`
			Parameters  map[string]string `json:"parameters"`
			Credentials []struct {
				Name   string `json:"name"`
				Usage  string `json:"usage"`
				Secret string `json:"secret"`
			} `json:"credentials"`
		} `json:"services"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(out), &conns)
	}
	if !s.check(err == nil && len(conns.Services) == len(eps.Services),
		"connections_list covers every cataloged service") {
		return
	}

	smtpPort := s.r.Prof.Get("MAILDEV_SMTP_PORT", "1025")
	allHaveEndpoints := true
	foundSMTP := false
	foundMailMCP := false
	foundBaseDN := false
	connSecretsRevealed := false
	for _, svc := range conns.Services {
		if len(svc.Endpoints) == 0 {
			allHaveEndpoints = false
		}
		for _, e := range svc.Endpoints {
			if svc.ID == "maildev" && e.Protocol == "smtp" && strings.HasSuffix(e.Address, ":"+smtpPort) {
				foundSMTP = true
			}
			if svc.ID == "maildev" && e.Protocol == "mcp-streamable-http" && strings.Contains(e.Address, "/mcp") {
				foundMailMCP = true
			}
		}
		if svc.ID == "labldap" && strings.HasPrefix(svc.Parameters["base_dn"], "dc=") {
			foundBaseDN = true
		}
		for _, cr := range svc.Credentials {
			if cr.Secret != "" {
				connSecretsRevealed = true
			}
		}
	}
	s.check(allHaveEndpoints, "every service documents at least one protocol endpoint")
	s.check(foundSMTP, "maildev SMTP endpoint carries the profile's port")
	s.check(foundMailMCP, "maildev MCP endpoint is cataloged")
	s.check(foundBaseDN, "labldap parameters include the base DN")
	s.check(connSecretsRevealed == devMode,
		fmt.Sprintf("connection secrets revealed only in dev mode (revealed=%v)", connSecretsRevealed))
}

// devCatalogScenario runs only when LAB_DEV_MODE is on so default-profile
// smoke stays random secrets and redaction.
func (s *smokeState) devCatalogScenario() {
	if !s.devMode() {
		return
	}
	s.step("dev catalog on the wire")

	doc, err := LoadDevCredentials(filepath.Join(s.r.Prof.Dir, "dev-credentials.yaml"))
	if !s.check(err == nil, fmt.Sprintf("load profile dev-credentials.yaml (err=%v)", err)) {
		return
	}

	mism := checkCatalogFiles(s.r.Root, doc)
	s.check(len(mism) == 0, fmt.Sprintf("every catalog value equals disk (%d mismatch(es))", len(mism)))
	for _, m := range mism {
		fmt.Printf("      %s\n", m)
	}

	// File-backed password: checkCatalogFiles already required user-alice
	// equal the catalog; interpolating the catalog string into sh -c would
	// mis-quote team values with $, backticks, or quotes.
	out, err := s.ldapsearch(
		`ldapsearch -x -H ldaps://directory:3636 -D "uid=alice,ou=people,dc=example,dc=test" -w "$(cat /s/user-alice)" -b "uid=alice,ou=people,dc=example,dc=test" -s base dn`)
	s.check(err == nil && strings.Contains(out, "uid=alice,ou=people,dc=example,dc=test"),
		fmt.Sprintf("Alice LDAPS bind uses catalog password (err=%v)", err))

	port := s.r.Prof.Get("TACLAB_RADIUS_ACCESS_PORT", "1812")
	code, err := radiusAuth("127.0.0.1:"+port, "lab-admin",
		doc.Spec.Passwords.TaclabAdmin, doc.Spec.SharedSecrets.RadiusLabSwitches)
	s.check(err == nil && code == radius.CodeAccessAccept,
		fmt.Sprintf("RADIUS Access-Accept for catalog taclabAdmin (code=%d, err=%v)", code, err))

	s.checkConnectionsEqualDisk()
}

func (s *smokeState) checkConnectionsEqualDisk() {
	out, err := s.invoke("labinfo__connections_list", "{}")
	var conns struct {
		DevMode  bool `json:"devMode"`
		Services []struct {
			Credentials []struct {
				Name   string `json:"name"`
				Secret string `json:"secret"`
			} `json:"credentials"`
		} `json:"services"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(out), &conns)
	}
	if !s.check(err == nil && conns.DevMode, fmt.Sprintf("connections_list devMode=true (err=%v)", err)) {
		return
	}
	cat, err := labinfo.Load(filepath.Join(s.r.Prof.Dir, "labinfo", "services.yaml"))
	if !s.check(err == nil, fmt.Sprintf("load labinfo catalog (err=%v)", err)) {
		return
	}
	var revealed []revealedSecret
	for _, svc := range conns.Services {
		for _, cr := range svc.Credentials {
			revealed = append(revealed, revealedSecret{Name: cr.Name, Secret: cr.Secret})
		}
	}
	mism := matchConnSecretsToDisk(revealed, s.r.path("secrets/labinfo-creds"), connCredIndex(cat))
	s.check(len(mism) == 0, fmt.Sprintf("connections_list secrets equal disk files (%d mismatch(es))", len(mism)))
	for _, m := range mism {
		fmt.Printf("      %s\n", m)
	}
}

// catalogFileExpect is one catalog key and the file that must equal it
// after reconcile. PasswordsKey selects a PASSWORDS.txt entry.
type catalogFileExpect struct {
	Key          string
	Rel          string
	Want         string
	PasswordsKey string
}

func catalogFileExpects(doc *DevCredentials) []catalogFileExpect {
	ll := "third_party/go-lab-ldap-mcp/secrets/"
	ts := taclabDir + "/deployments/compose/secrets/"
	return []catalogFileExpect{
		{"spec.tokens.labdns", "secrets/labdns-token", doc.Spec.Tokens.LabDNS, ""},
		{"spec.tokens.labinfo", "secrets/labinfo-token", doc.Spec.Tokens.Labinfo, ""},
		{"spec.tokens.labmail", "secrets/labmail-token", doc.Spec.Tokens.Labmail, ""},
		{"spec.tokens.labmitm", "secrets/labmitm-token", doc.Spec.Tokens.LabMITM, ""},
		{"spec.tokens.mcpClient", "secrets/mcp-client-token", doc.Spec.Tokens.MCPClient, ""},
		{"spec.tokens.labldapAdmin", ll + "token-admin", doc.Spec.Tokens.LabLDAPAdmin, ""},
		{"spec.tokens.labtacacsAdmin", ts + "api_admin_token", doc.Spec.Tokens.LabTacacsAdmin, ""},
		{"spec.passwords.maildevWeb", "secrets/maildev-web-password", doc.Spec.Passwords.MaildevWeb, ""},
		{"spec.passwords.labldapAlice", ll + "user-alice", doc.Spec.Passwords.LabLDAPAlice, ""},
		{"spec.passwords.labldapRuntime", ll + "runtime-ldap", doc.Spec.Passwords.LabLDAPRuntime, ""},
		{"spec.passwords.labldapDM", ll + "dm.pw", doc.Spec.Passwords.LabLDAPDM, ""},
		{"spec.passwords.labldapDM(directory.env)", ll + "directory.env", "DS_DM_PASSWORD=" + doc.Spec.Passwords.LabLDAPDM, ""},
		{"spec.passwords.taclabAdmin", ts + "PASSWORDS.txt", doc.Spec.Passwords.TaclabAdmin, "lab-admin"},
		{"spec.passwords.taclabAdminEnable", ts + "PASSWORDS.txt", doc.Spec.Passwords.TaclabAdminEnable, "lab-admin-enable"},
		{"spec.passwords.taclabReadonly", ts + "PASSWORDS.txt", doc.Spec.Passwords.TaclabReadonly, "lab-readonly"},
		{"spec.passwords.taclabDisabled", ts + "PASSWORDS.txt", doc.Spec.Passwords.TaclabDisabled, "lab-disabled"},
		{"spec.passwords.taclabChallenge", ts + "lab_admin_challenge_secret", doc.Spec.Passwords.TaclabChallenge, ""},
		{"spec.sharedSecrets.tacacsLabSwitches", ts + "lab_switches_tacacs_secret", doc.Spec.SharedSecrets.TacacsLabSwitches, ""},
		{"spec.sharedSecrets.radiusLabSwitches", ts + "lab_switches_radius_secret", doc.Spec.SharedSecrets.RadiusLabSwitches, ""},
	}
}

func checkCatalogFiles(root string, doc *DevCredentials) []string {
	var mismatches []string
	for _, e := range catalogFileExpects(doc) {
		path := filepath.Join(root, e.Rel)
		var got string
		var err error
		if e.PasswordsKey != "" {
			got, err = labgenPassword(path, e.PasswordsKey)
		} else {
			got, err = readTrimmed(path)
		}
		if err != nil {
			mismatches = append(mismatches, e.Key+": "+err.Error())
			continue
		}
		if got != e.Want {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s = %q, want %q", e.Key, e.Rel, got, e.Want))
		}
	}
	sort.Strings(mismatches)
	return mismatches
}

type connCredMeta struct {
	Service  string
	Basename string
	Optional bool
}

func connCredIndex(cat *labinfo.Catalog) map[string]connCredMeta {
	out := make(map[string]connCredMeta)
	if cat == nil {
		return out
	}
	for _, svc := range cat.Services {
		if svc.Connection == nil {
			continue
		}
		for _, cr := range svc.Connection.Credentials {
			out[cr.Name] = connCredMeta{
				Service:  svc.ID,
				Basename: filepath.Base(cr.File),
				Optional: cr.Optional,
			}
		}
	}
	return out
}

type revealedSecret struct {
	Name   string
	Secret string
}

func matchConnSecretsToDisk(revealed []revealedSecret, stagedDir string, idx map[string]connCredMeta) []string {
	seen := map[string]bool{}
	var mismatches []string
	for _, r := range revealed {
		seen[r.Name] = true
		meta, ok := idx[r.Name]
		if !ok {
			mismatches = append(mismatches, "unknown connections_list credential "+r.Name)
			continue
		}
		got, err := readTrimmed(filepath.Join(stagedDir, meta.Basename))
		if err != nil {
			if meta.Optional && os.IsNotExist(err) {
				if r.Secret != "" {
					mismatches = append(mismatches, r.Name+": optional file missing but secret revealed")
				}
				continue
			}
			mismatches = append(mismatches, r.Name+": "+err.Error())
			continue
		}
		if r.Secret != got {
			mismatches = append(mismatches, fmt.Sprintf("%s: connections_list %q != disk %q", r.Name, r.Secret, got))
		}
	}
	for name, meta := range idx {
		if seen[name] || meta.Optional {
			continue
		}
		mismatches = append(mismatches, "missing connections_list credential "+name)
	}
	sort.Strings(mismatches)
	return mismatches
}
