// labinfo is the lab service directory: a tiny MCP (streamable HTTP) service
// that tells agents where every lab service's web/REST surface lives
// (endpoints_list) and how to connect clients to each service's data plane —
// hosts/ports, protocol parameters like LDAP DNs or NFS mount options, and
// connection credentials (connections_list). When the profile enables dev
// mode (LAB_DEV_MODE=true) it also reveals the credential secrets.
//
// Usage:
//
//	labinfo serve --config=/etc/labinfo/services.yaml --listen=:8080 --token-file=/run/lab-secrets/labinfo-token
//	labinfo healthcheck --url=http://127.0.0.1:8080/healthz
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/hilather/mcp-integration-lab/internal/labinfo"
	"github.com/hilather/mcp-integration-lab/internal/profile"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: labinfo serve|healthcheck [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		os.Exit(serve(os.Args[2:]))
	case "healthcheck":
		os.Exit(healthcheck(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "labinfo: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "/etc/labinfo/services.yaml", "endpoint catalog YAML")
	listen := fs.String("listen", ":8080", "listen address")
	tokenFile := fs.String("token-file", "", "bearer token required on /mcp (empty disables auth)")
	_ = fs.Parse(args)

	catalog, err := labinfo.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "labinfo: %v\n", err)
		return 1
	}
	devMode := profile.IsTrue(os.Getenv("LAB_DEV_MODE"))

	var token string
	if *tokenFile != "" {
		token, err = labinfo.ReadSecretFile(*tokenFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "labinfo: token file: %v\n", err)
			return 1
		}
	}

	mcpSrv := server.NewMCPServer("labinfo", "0.2.0",
		server.WithToolCapabilities(false),
		server.WithInstructions("Lab service directory. Call endpoints_list for the user-facing web/REST URLs of every lab service (to direct users to the right place), and connections_list for protocol-level client configuration — hosts/ports, parameters like LDAP base/bind DNs, DNS zones, NFS mount options, SMTP settings — plus connection credentials when the lab runs in dev mode."),
	)
	endpointsTool := mcp.NewTool("endpoints_list",
		mcp.WithDescription("List the user-facing web/REST endpoints of every lab service (gateway, DNS, LDAP, TACACS+/RADIUS, mail sink, NFS, ...): names, descriptions, URLs, and how to authenticate. In dev mode the response includes the actual credentials; otherwise it explains how credentials are obtained."),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	mcpSrv.AddTool(endpointsTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		eps, err := catalog.Render(devMode, os.Getenv, labinfo.ReadSecretFile)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return marshalResult(eps)
	})

	connectionsTool := mcp.NewTool("connections_list",
		mcp.WithDescription("Get the protocol-level connection details needed to configure a client or system under test against each lab service: protocol endpoints (SMTP, LDAP/LDAPS, DNS, NFS, TACACS+, RADIUS, MCP) with host:port, client parameters (LDAP base/bind DNs and OUs, DNS zones, NFS mount options, AAA specifics), and the connection credentials (bind passwords, shared secrets, tokens). Secrets are revealed only in dev mode; otherwise each credential's usage explains what it is for and how to obtain it."),
		mcp.WithString("service",
			mcp.Description("Optional service id to return a single service's connection details (e.g. maildev, labldap); omit for all services."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	mcpSrv.AddTool(connectionsTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		conns, err := catalog.RenderConnections(devMode, os.Getenv, labinfo.ReadSecretFile, req.GetString("service", ""))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return marshalResult(conns)
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", bearerAuth(token, server.NewStreamableHTTPServer(mcpSrv)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	hs := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Printf("labinfo: listening on %s (devMode=%v)\n", *listen, devMode)
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "labinfo: %v\n", err)
		return 1
	}
	return 0
}

func marshalResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

// bearerAuth requires "Authorization: Bearer <token>" when token is set.
func bearerAuth(token string, next http.Handler) http.Handler {
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

func healthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	url := fs.String("url", "http://127.0.0.1:8080/healthz", "health endpoint")
	_ = fs.Parse(args)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(*url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "labinfo healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "labinfo healthcheck: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
