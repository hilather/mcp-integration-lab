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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hilather/mcp-integration-lab/internal/mcpout"
	"github.com/hilather/mcp-integration-lab/internal/profile"
	"github.com/hilather/mcp-integration-lab/internal/radius"
)

// Smoke runs the end-to-end scenario: drives DNS, LDAP, and TACACS+/RADIUS
// state through the MCP gateway the way an agent would, then verifies every
// result on the real data plane (DNS query, LDAPS bind, kernel NFS mount,
// RADIUS PAP auth, SMTP delivery into the receive-only mail sink).
func (r *Runner) Smoke() error {
	s := &smokeState{r: r}

	s.step("gateway health")
	port := r.Prof.Get("MCP_GATEWAY_PORT", "8080")
	s.check(waitHealthy("http://127.0.0.1:"+port+"/health", 10*time.Second) == nil, "gateway /health")

	s.dnsScenario()
	s.ldapScenario()
	s.nfsScenario()
	s.tacacsScenario()
	s.maildevScenario()
	s.labinfoScenario()

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
	s.check(err != nil || len(addrs) == 0, "smoke.lab.test gone after reset")
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

	// Data-plane checks from inside the docker network (the lab LDAPS cert's
	// SAN is the compose hostname). Alice has suffix read via the staff ACL.
	secrets := filepath.Join(s.r.Root, "third_party", "go-lab-ldap-mcp", "secrets")
	ldapsearch := func(script string) (string, error) {
		return s.r.capture(".", "docker", "run", "--rm", "--network", "mcplab-shared",
			"-v", secrets+":/s:ro", "-e", "LDAPTLS_CACERT=/s/tls/ca.crt",
			"--entrypoint", "sh", "labldap-bootstrap:dev", "-c", script)
	}

	out, err := ldapsearch(fmt.Sprintf(
		`ldapsearch -x -H ldaps://directory:3636 -D "uid=alice,ou=people,dc=example,dc=test" -w "$(cat /s/user-alice)" -b "dc=example,dc=test" "(uid=%s)" uid`, user))
	s.check(err == nil && strings.Contains(out, "uid: "+user), "ldapsearch found "+user+" via LDAPS")

	_, err = ldapsearch(fmt.Sprintf(
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
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, name+"="); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("%s: no entry for %q", path, name)
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

	devMode := profile.IsTrue(s.r.Prof.Get("LAB_DEV_MODE", "false"))
	s.check(eps.DevMode == devMode, fmt.Sprintf("devMode flag matches profile (%v)", devMode))

	gatewayPort := s.r.Prof.Get("MCP_GATEWAY_PORT", "8080")
	foundGateway := false
	credentialsRevealed := false
	for _, svc := range eps.Services {
		if svc.ID == "gateway" {
			for _, u := range svc.URLs {
				if strings.Contains(u.URL, ":"+gatewayPort+"/mcp") {
					foundGateway = true
				}
			}
		}
		if svc.Credential != nil && svc.Credential.Secret != "" {
			credentialsRevealed = true
		}
	}
	s.check(foundGateway, "gateway URL carries the profile's public port")
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
