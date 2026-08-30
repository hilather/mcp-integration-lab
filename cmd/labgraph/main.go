// labgraph serves LabScenario orchestration over REST, MCP, and an embedded SPA.
//
//	labgraph serve --config=/etc/labgraph/config.yaml --scenarios=/etc/labgraph/scenarios --listen=:8080 --token-file=/run/lab-secrets/labgraph-token
//	labgraph healthcheck --url=http://127.0.0.1:8080/v1/health/ready
package main

import (
	"crypto/subtle"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/hilather/mcp-integration-lab/internal/labgraph"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: labgraph serve|healthcheck [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		os.Exit(serve(os.Args[2:]))
	case "healthcheck":
		os.Exit(healthcheck(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "labgraph: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "/etc/labgraph/config.yaml", "LabGraph bootstrap YAML")
	scenarios := fs.String("scenarios", "/etc/labgraph/scenarios", "LabScenario directory")
	listen := fs.String("listen", ":8080", "listen address")
	tokenFile := fs.String("token-file", "", "bearer token file")
	_ = fs.Parse(args)

	cfg, err := labgraph.LoadLabGraph(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "labgraph: %v\n", err)
		return 1
	}
	token, err := readToken(*tokenFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "labgraph: token: %v\n", err)
		return 1
	}

	clients, err := applianceClients()
	if err != nil {
		fmt.Fprintf(os.Stderr, "labgraph: clients: %v\n", err)
		return 1
	}
	svc := labgraph.NewService(*scenarios, clients)
	if v := strings.TrimSpace(os.Getenv("LABGRAPH_LABLDAP_SCENARIO_NAME")); v != "" {
		svc.LDAPName = v
	}
	sess := labgraph.NewSessionStore(token)

	mux := http.NewServeMux()
	labgraph.REST(mux, svc, sess)
	mcpH := server.NewStreamableHTTPServer(labgraph.MCPServer(svc))
	allowLegacy := cfg.Spec.Management.MCP.AllowLegacyClients
	mux.Handle("/mcp", labgraph.MCPVersion(allowLegacy, bearerOnly(token, mcpH)))
	if cfg.Spec.UI.Enabled {
		mux.Handle("/", labgraph.SPAOrigins(cfg.Spec.Management.OriginAllowlist, labgraph.SPA()))
	}

	hs := &http.Server{
		Addr:              *listen,
		Handler:           sess.Protect(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Printf("labgraph: listening on %s\n", *listen)
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "labgraph: %v\n", err)
		return 1
	}
	return 0
}

func applianceClients() (labgraph.Clients, error) {
	read := func(env, def string) string {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
		return def
	}
	tok := func(basename string) string {
		b, err := os.ReadFile("/run/lab-secrets/" + basename)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	c := labgraph.Clients{Family: map[string]labgraph.FamilyClient{}}
	c.Family["labdns"] = &labgraph.HTTPFamily{Base: read("LABGRAPH_LABDNS_URL", "http://labdns:8080"), Token: tok("labdns-token")}
	c.Family["labmitm"] = &labgraph.HTTPFamily{Base: read("LABGRAPH_LABMITM_URL", "http://labmitm:8088"), Token: tok("labmitm-token")}
	c.Family["maildev"] = &labgraph.HTTPFamily{Base: read("LABGRAPH_MAILDEV_URL", "http://maildev:1080"), Token: tok("labmail-token")}
	ca := read("LABGRAPH_LABLDAP_CA_FILE", "/run/lab-secrets/labldap-ca.crt")
	ldap, err := labgraph.NewHTTPLDAP(read("LABGRAPH_LABLDAP_URL", "https://control:8443"), tok("labldap-token-admin"), ca)
	if err != nil {
		// CA may be missing at unit-test of the binary; serve still starts if we skip LDAP.
		fmt.Fprintf(os.Stderr, "labgraph: labldap client: %v (reset/status for labldap will fail)\n", err)
	} else {
		c.LDAP = ldap
	}
	c.TacLab = &labgraph.HTTPTacLab{Base: read("LABGRAPH_LABTACACS_URL", "http://taclab:8080"), Token: tok("labtacacs-token-admin")}
	return c, nil
}

func bearerOnly(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readToken(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func healthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	url := fs.String("url", "http://127.0.0.1:8080/v1/health/ready", "health endpoint")
	_ = fs.Parse(args)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(*url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "labgraph healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "labgraph healthcheck: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
