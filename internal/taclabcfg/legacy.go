// Package taclabcfg mutates TacLab's labgen-generated YAML. labgen owns the
// baseline (PKI, secrets, users); this lab only turns on the upstream MCP
// compatibility knob so MCPJungle can connect.
package taclabcfg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const legacyKey = "allow_legacy_clients"

// EnableLegacyClients sets api.mcp.allow_legacy_clients: true on a TacLab
// YAML document. Fail-closed if the api.mcp mapping is missing — we must
// not silently serve a config the gateway cannot speak to.
func EnableLegacyClients(in []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(in, &doc); err != nil {
		return nil, err
	}
	mcp := mapValue(&doc, "api", "mcp")
	if mcp == nil {
		return nil, fmt.Errorf("missing api.mcp (cannot enable allow_legacy_clients)")
	}
	setBool(mcp, legacyKey, true)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	outNode := &doc
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		outNode = doc.Content[0]
	}
	if err := enc.Encode(outNode); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EnableLegacyClientsDir rewrites every *.yaml in a labgen config directory.
func EnableLegacyClientsDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, err := EnableLegacyClients(b)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("no yaml configs in %s", dir)
	}
	return nil
}

func mapValue(n *yaml.Node, keys ...string) *yaml.Node {
	cur := n
	if cur.Kind == yaml.DocumentNode && len(cur.Content) > 0 {
		cur = cur.Content[0]
	}
	for _, key := range keys {
		if cur == nil || cur.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i < len(cur.Content)-1; i += 2 {
			if cur.Content[i].Value == key {
				next = cur.Content[i+1]
				break
			}
		}
		cur = next
	}
	return cur
}

func setBool(m *yaml.Node, key string, v bool) {
	val := "false"
	if v {
		val = "true"
	}
	for i := 0; i < len(m.Content)-1; i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1].Kind = yaml.ScalarNode
			m.Content[i+1].Tag = "!!bool"
			m.Content[i+1].Value = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: val},
	)
}
