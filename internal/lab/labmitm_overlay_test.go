package lab

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Allowlists are the v1.5.0 published schema property names
// (https://raw.githubusercontent.com/hilather/go-lab-mitmproxy/v1.5.0/api/jsonschema/labmitm.dev.v1alpha1.json).
// Pin is third_party/go-lab-mitmproxy @ v1.5.0 (internal/lab/vendor.go).
var labmitmV15Allow = map[string][]string{
	"":                    {"apiVersion", "kind", "metadata", "spec"},
	"metadata":            {"name", "labels"},
	"spec":                {"listeners", "proxy", "tls", "rules", "store", "ui", "management", "observability", "protocols", "compat"},
	"spec.listeners":      {"proxy", "management", "originalDestination"},
	"spec.listeners.proxy": {
		"address", "acceptSOCKS5", "acceptSOCKS4", "acceptBind",
		"acceptUDPAssociate", "acceptUserPass", "userPass",
	},
	"spec.listeners.proxy.userPass": {"users"},
	"spec.listeners.management":     {"address", "restPath", "mcpPath", "tls"},
	"spec.listeners.management.tls": {"enabled", "certFile", "keyFile"},
	"spec.listeners.originalDestination": {"enabled", "address"},
	"spec.proxy": {"hostname", "admission", "targets", "httpAuth"},
	"spec.proxy.admission": {
		"maxSessions", "maxSessionsPerIP", "maxInFlight", "maxInFlightBytes",
		"sessionTimeout", "idleTimeout", "headerTimeout", "dialTimeout",
		"upstreamTimeout", "maxConcurrentStreams",
	},
	"spec.proxy.targets":  {"denyCloudMetadata", "denyLinkLocal", "allowLoopback", "allowHosts", "denyHosts"},
	"spec.proxy.httpAuth": {"enabled", "realm", "users"},
	"spec.tls":            {"intercept", "hosts", "ports", "ca", "upstream"},
	"spec.tls.ca":         {"mode", "certFile", "keyFile"},
	"spec.tls.upstream":   {"insecureSkipVerify", "extraCAFiles"},
	"spec.rules":          {"enabled", "items"},
	"spec.store": {
		"maxFlows", "maxBytes", "maxBodyBytes", "fullPolicy",
		"maxWait", "spillDirectory", "spillThreshold",
	},
	"spec.ui":         {"enabled"},
	"spec.management": {"auth", "mcp", "originAllowlist", "bodyLimit", "requestsPerSecond", "burst", "maxConcurrent"},
	"spec.management.auth": {"mode", "tokens"},
	"spec.management.mcp":  {"allowLegacyClients"},
	"spec.protocols":       {"http2", "websocket", "connect", "absoluteForm"},
	"spec.protocols.http2": {
		"enabled", "clientCleartext", "origin", "extendedConnect",
		"capturePush", "grpcDecode",
	},
	"spec.protocols.websocket":    {"enabled", "inspectFrames"},
	"spec.protocols.connect":      {"enabled"},
	"spec.protocols.absoluteForm": {"enabled"},
	"spec.compat":                 {"flowREST"},
	"spec.compat.flowREST":        {"enabled", "pathPrefix"},
	"spec.observability":          {"logLevel", "metrics", "audit"},
	"spec.observability.metrics":  {"listen", "publicPath"},
	"spec.observability.audit":    {"ring"},
}

func requireLabMITM15Knobs(raw []byte) error {
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return err
	}
	if err := rejectUnknownLabMITM(root, ""); err != nil {
		return err
	}
	falsePaths := []string{
		"spec.listeners.proxy.acceptSOCKS5",
		"spec.listeners.proxy.acceptSOCKS4",
		"spec.listeners.proxy.acceptBind",
		"spec.listeners.proxy.acceptUDPAssociate",
		"spec.listeners.proxy.acceptUserPass",
		"spec.listeners.originalDestination.enabled",
		"spec.proxy.httpAuth.enabled",
		"spec.protocols.http2.enabled",
		"spec.protocols.http2.clientCleartext",
		"spec.protocols.http2.origin",
		"spec.protocols.http2.extendedConnect",
		"spec.protocols.http2.capturePush",
		"spec.protocols.http2.grpcDecode",
		"spec.protocols.websocket.inspectFrames",
		"spec.compat.flowREST.enabled",
		"spec.rules.enabled",
	}
	truePaths := []string{
		"spec.protocols.websocket.enabled",
		"spec.protocols.connect.enabled",
		"spec.protocols.absoluteForm.enabled",
		"spec.ui.enabled",
	}
	for _, p := range falsePaths {
		if err := requireBoolPath(root, p, false); err != nil {
			return err
		}
	}
	for _, p := range truePaths {
		if err := requireBoolPath(root, p, true); err != nil {
			return err
		}
	}
	items, err := lookupPath(root, "spec.rules.items")
	if err != nil {
		return err
	}
	arr, ok := items.([]any)
	if !ok {
		return fmt.Errorf("spec.rules.items = %#v, want empty sequence", items)
	}
	if len(arr) != 0 {
		return fmt.Errorf("spec.rules.items = %#v, want empty", items)
	}
	return nil
}

func requireBoolPath(root map[string]any, path string, want bool) error {
	v, err := lookupPath(root, path)
	if err != nil {
		return err
	}
	got, ok := v.(bool)
	if !ok {
		return fmt.Errorf("%s = %#v, want bool %v", path, v, want)
	}
	if got != want {
		return fmt.Errorf("%s = %v, want %v", path, got, want)
	}
	return nil
}

func lookupPath(root map[string]any, path string) (any, error) {
	cur := any(root)
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: parent is not a mapping", path)
		}
		next, ok := m[key]
		if !ok {
			return nil, fmt.Errorf("%s: missing", path)
		}
		cur = next
	}
	return cur, nil
}

func rejectUnknownLabMITM(v any, path string) error {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	allowed, ok := labmitmV15Allow[path]
	if !ok {
		return nil
	}
	set := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		set[k] = true
	}
	for k, child := range m {
		if !set[k] {
			if path == "" {
				return fmt.Errorf("unknown field %q", k)
			}
			return fmt.Errorf("unknown field %s.%s", path, k)
		}
		childPath := k
		if path != "" {
			childPath = path + "." + k
		}
		if err := rejectUnknownLabMITM(child, childPath); err != nil {
			return err
		}
	}
	return nil
}

func TestDefaultLabMITMBootstrapRequiresV15Knobs(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(defaultProfileDir(t), "labmitm", "bootstrap.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := requireLabMITM15Knobs(b); err != nil {
		t.Fatal(err)
	}
}

func TestOmitStyleLabMITMOverlayFailsV15Knobs(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "labmitm", "omit-12-14.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	err = requireLabMITM15Knobs(b)
	if err == nil {
		t.Fatal("omit-style overlay must fail requireLabMITM15Knobs (httpAuth / 1.2 paths absent)")
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing") {
		t.Fatalf("error = %v, want a missing 1.2/httpAuth path", err)
	}
}

func TestLabMITM15KnobsRejectsMisnestedAndUnknown(t *testing.T) {
	good, err := os.ReadFile(filepath.Join(defaultProfileDir(t), "labmitm", "bootstrap.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := requireLabMITM15Knobs(good); err != nil {
		t.Fatal(err)
	}

	var root map[string]any
	if err := yaml.Unmarshal(good, &root); err != nil {
		t.Fatal(err)
	}
	spec := root["spec"].(map[string]any)
	spec["protcols"] = map[string]any{"http2": map[string]any{"enabled": false}}
	b, err := yaml.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireLabMITM15Knobs(b); err == nil || !strings.Contains(err.Error(), "protcols") {
		t.Fatalf("typo spec.protcols must fail, got %v", err)
	}

	if err := yaml.Unmarshal(good, &root); err != nil {
		t.Fatal(err)
	}
	listeners := root["spec"].(map[string]any)["listeners"].(map[string]any)
	listeners["acceptBind"] = false
	delete(listeners["proxy"].(map[string]any), "acceptBind")
	b, err = yaml.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireLabMITM15Knobs(b); err == nil {
		t.Fatal("mis-nested spec.listeners.acceptBind must fail")
	}
}
