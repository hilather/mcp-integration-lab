// Package labgraph is the LabScenario orchestrator: one operation registry
// behind REST, MCP, and the embedded SPA. It fans out to native appliance
// APIs and does not invent a second control protocol.
package labgraph

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "mcplab.dev/v1alpha1"
	Kind       = "LabScenario"
	GraphKind  = "LabGraph"
)

// ApplyOrder is the documented sequential fan-out. Do not reorder.
var ApplyOrder = []string{"labdns", "labmitm", "maildev", "labldap", "labtacacs"}

// LabScenario is mcplab.dev/v1alpha1 orchestration YAML. Appliance sections
// are opaque nodes. spec is a struct so KnownFields rejects unknown keys.
type LabScenario struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Metadata   Metadata       `yaml:"metadata"`
	Spec       Spec           `yaml:"spec"`
	SourcePath string         `yaml:"-"`
}

type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

// FixtureIDs is the closed set of named packs. default is not a fixture.
var FixtureIDs = []string{
	"broken-bind",
	"expired-cert",
	"split-horizon-dns",
	"mitm-intercept-extra-port",
}

func IsFixture(name string) bool {
	for _, id := range FixtureIDs {
		if id == name {
			return true
		}
	}
	return false
}

// RawYAML holds an opaque YAML subtree. UnmarshalYAML keeps KnownFields
// from walking yaml.Node's own exported fields.
type RawYAML struct {
	Node yaml.Node
	set  bool
}

func (r *RawYAML) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	r.Node = *value
	r.set = true
	return nil
}

type Spec struct {
	LabDNS    *RawYAML `yaml:"labdns,omitempty"`
	LabMITM   *RawYAML `yaml:"labmitm,omitempty"`
	Maildev   *RawYAML `yaml:"maildev,omitempty"`
	LabLDAP   *RawYAML `yaml:"labldap,omitempty"`
	LabTacacs *RawYAML `yaml:"labtacacs,omitempty"`
}

func (s Spec) node(id string) *yaml.Node {
	var r *RawYAML
	switch id {
	case "labdns":
		r = s.LabDNS
	case "labmitm":
		r = s.LabMITM
	case "maildev":
		r = s.Maildev
	case "labldap":
		r = s.LabLDAP
	case "labtacacs":
		r = s.LabTacacs
	default:
		return nil
	}
	if r == nil || !r.set {
		return nil
	}
	return &r.Node
}

// sectionPresent is true when the key is in the file (including {}).
func sectionPresent(n *yaml.Node) bool { return n != nil }

// sectionNonEmpty is true when the key is present and not an empty mapping.
func sectionNonEmpty(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.MappingNode && len(n.Content) == 0 {
		return false
	}
	return true
}

func LoadScenario(path string) (*LabScenario, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := parseScenario(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	doc.SourcePath = path
	return doc, nil
}

func parseScenario(r io.Reader) (*LabScenario, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var doc LabScenario
	if err := dec.Decode(&doc); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("empty document")
		}
		return nil, err
	}
	if doc.APIVersion != APIVersion {
		return nil, fmt.Errorf("apiVersion %q, want %s", doc.APIVersion, APIVersion)
	}
	if doc.Kind != Kind {
		return nil, fmt.Errorf("kind %q, want %s", doc.Kind, Kind)
	}
	if strings.TrimSpace(doc.Metadata.Name) == "" {
		return nil, fmt.Errorf("metadata.name is required")
	}
	return &doc, nil
}

func LoadDir(dir string) ([]*LabScenario, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []*LabScenario
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		doc, err := LoadScenario(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, nil
}

func FindByName(docs []*LabScenario, name string) *LabScenario {
	for _, d := range docs {
		if d.Metadata.Name == name {
			return d
		}
	}
	return nil
}

// LabGraph is the service bootstrap (not a LabScenario).
type LabGraph struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Metadata   Metadata     `yaml:"metadata"`
	Spec       LabGraphSpec `yaml:"spec"`
}

type LabGraphSpec struct {
	UI         LabGraphUI         `yaml:"ui"`
	Management LabGraphManagement `yaml:"management"`
}

type LabGraphUI struct {
	Enabled bool `yaml:"enabled"`
}

type LabGraphManagement struct {
	Auth            LabGraphAuth `yaml:"auth"`
	MCP             LabGraphMCP  `yaml:"mcp"`
	OriginAllowlist []string     `yaml:"originAllowlist"`
}

type LabGraphAuth struct {
	Profile   string `yaml:"profile"`
	SecretRef string `yaml:"secretRef"`
}

type LabGraphMCP struct {
	AllowLegacyClients bool `yaml:"allowLegacyClients"`
}

func LoadLabGraph(path string) (*LabGraph, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var doc LabGraph
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.APIVersion != APIVersion {
		return nil, fmt.Errorf("%s: apiVersion %q, want %s", path, doc.APIVersion, APIVersion)
	}
	if doc.Kind != GraphKind {
		return nil, fmt.Errorf("%s: kind %q, want %s", path, doc.Kind, GraphKind)
	}
	return &doc, nil
}
