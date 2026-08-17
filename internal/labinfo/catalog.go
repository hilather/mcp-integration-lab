// Package labinfo implements the lab service directory: a YAML catalog of
// every lab service's user-facing web/REST endpoints and protocol-level
// connection details (hosts/ports, protocol parameters like LDAP DNs or NFS
// mount options, and connection credentials), rendered for agents so they can
// direct users to the right URL and help configure clients against the lab.
// Credentials are included only when the profile enables dev mode
// (LAB_DEV_MODE).
package labinfo

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Catalog is the permanent (profile-owned) YAML description of the lab's
// user-facing endpoints. URL values may reference profile variables with
// ${VAR} syntax; they are expanded from the service environment.
type Catalog struct {
	Services []Service `yaml:"services"`
}

// Service is one lab service and its user-facing surfaces.
type Service struct {
	ID          string      `yaml:"id"`
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	URLs        []URL       `yaml:"urls"`
	Note        string      `yaml:"note,omitempty"`
	Credential  *Credential `yaml:"credential,omitempty"`
	Connection  *Connection `yaml:"connection"`
}

// URL is a named user-facing endpoint.
type URL struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// Credential describes how to authenticate against the service's web/REST
// surface. The secret itself is read from File and only ever revealed when
// dev mode is enabled.
type Credential struct {
	File  string `yaml:"file"`
	Usage string `yaml:"usage"`
}

// Connection is the protocol-level client configuration for a service's data
// plane: what an agent needs to point a system under test (or a client) at
// the lab. Every cataloged service must carry one — that requirement is the
// guard that keeps new services from shipping without connection details.
type Connection struct {
	// Endpoints are the protocol sockets (SMTP, LDAP, DNS, NFS, ...).
	Endpoints []ConnEndpoint `yaml:"endpoints"`
	// Parameters are protocol-specific client settings that are not
	// secrets: base/bind DNs, DNS zones, mount options, AAA specifics, ...
	// Values may reference ${VAR}.
	Parameters map[string]string `yaml:"parameters,omitempty"`
	// Credentials are the secrets a client needs on the wire (bind
	// passwords, shared secrets). Revealed only in dev mode.
	Credentials []ConnCredential `yaml:"credentials,omitempty"`
}

// ConnEndpoint is one protocol socket of a service.
type ConnEndpoint struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"`
	Address  string `yaml:"address"`
	Note     string `yaml:"note,omitempty"`
}

// ConnCredential is a named connection secret (e.g. an LDAP bind password or
// a RADIUS shared secret). Usage must say what the secret is for; File points
// at the staged copy under /run/lab-secrets.
type ConnCredential struct {
	Name  string `yaml:"name"`
	File  string `yaml:"file"`
	Usage string `yaml:"usage"`
}

// Load parses a catalog YAML file.
func Load(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(c.Services) == 0 {
		return nil, fmt.Errorf("%s: catalog has no services", path)
	}
	for i, s := range c.Services {
		if s.ID == "" || len(s.URLs) == 0 {
			return nil, fmt.Errorf("%s: services[%d] needs id and at least one url", path, i)
		}
		// Fail closed: a service without connection details would leave
		// agents unable to configure clients against it.
		if s.Connection == nil || len(s.Connection.Endpoints) == 0 {
			return nil, fmt.Errorf("%s: services[%d] (%s) needs a connection block with at least one endpoint", path, i, s.ID)
		}
		for j, e := range s.Connection.Endpoints {
			if e.Protocol == "" || e.Address == "" {
				return nil, fmt.Errorf("%s: services[%d] (%s) connection.endpoints[%d] needs protocol and address", path, i, s.ID, j)
			}
		}
		for j, cr := range s.Connection.Credentials {
			if cr.Name == "" || cr.File == "" || cr.Usage == "" {
				return nil, fmt.Errorf("%s: services[%d] (%s) connection.credentials[%d] needs name, file, and usage", path, i, s.ID, j)
			}
		}
	}
	return &c, nil
}

// Endpoints is the rendered, agent-facing directory.
type Endpoints struct {
	DevMode  bool           `json:"devMode"`
	Note     string         `json:"note,omitempty"`
	Services []EndpointInfo `json:"services"`
}

// EndpointInfo is one rendered service entry.
type EndpointInfo struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	URLs        []URL          `json:"urls"`
	Note        string         `json:"note,omitempty"`
	Credential  *RevealedCred  `json:"credential,omitempty"`
	Auth        string         `json:"auth,omitempty"`
}

// RevealedCred carries the secret; only present in dev mode.
type RevealedCred struct {
	Usage  string `json:"usage"`
	Secret string `json:"secret"`
}

// Render expands ${VAR} references via lookup, and includes credentials
// (read via readSecret) only when devMode is true. Otherwise the Auth field
// carries the usage description so agents can still explain how to connect.
func (c *Catalog) Render(devMode bool, lookup func(string) string, readSecret func(string) (string, error)) (*Endpoints, error) {
	out := &Endpoints{DevMode: devMode}
	if !devMode {
		out.Note = "credentials are only revealed when the active profile enables dev mode (LAB_DEV_MODE=true)"
	}
	for _, s := range c.Services {
		info := EndpointInfo{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Note:        expand(s.Note, lookup),
		}
		for _, u := range s.URLs {
			info.URLs = append(info.URLs, URL{Name: u.Name, URL: expand(u.URL, lookup)})
		}
		if s.Credential != nil {
			usage := expand(s.Credential.Usage, lookup)
			if devMode {
				secret, err := readSecret(s.Credential.File)
				if err != nil {
					return nil, fmt.Errorf("service %s: credential: %w", s.ID, err)
				}
				info.Credential = &RevealedCred{
					Usage:  usage,
					Secret: strings.TrimSpace(secret),
				}
			} else {
				info.Auth = usage
			}
		}
		out.Services = append(out.Services, info)
	}
	return out, nil
}

// Connections is the rendered, agent-facing connection directory.
type Connections struct {
	DevMode  bool             `json:"devMode"`
	Note     string           `json:"note,omitempty"`
	Services []ConnectionInfo `json:"services"`
}

// ConnectionInfo is one rendered service connection entry.
type ConnectionInfo struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Endpoints   []ConnEndpoint     `json:"endpoints"`
	Parameters  map[string]string  `json:"parameters,omitempty"`
	Credentials []RenderedConnCred `json:"credentials,omitempty"`
}

// RenderedConnCred is a connection credential as served to agents. Secret is
// populated only in dev mode; otherwise Usage alone tells the agent what the
// secret is and how a human obtains it.
type RenderedConnCred struct {
	Name   string `json:"name"`
	Usage  string `json:"usage"`
	Secret string `json:"secret,omitempty"`
}

// RenderConnections expands ${VAR} references and renders the protocol-level
// connection details. serviceID filters to one service ("" for all; unknown
// IDs are an error so agents get a corrective message instead of an empty
// list). Secrets are read (via readSecret) only when devMode is true.
func (c *Catalog) RenderConnections(devMode bool, lookup func(string) string, readSecret func(string) (string, error), serviceID string) (*Connections, error) {
	out := &Connections{DevMode: devMode}
	if !devMode {
		out.Note = "credential secrets are only revealed when the active profile enables dev mode (LAB_DEV_MODE=true); each credential's usage explains what it is for"
	}
	for _, s := range c.Services {
		if serviceID != "" && s.ID != serviceID {
			continue
		}
		info := ConnectionInfo{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
		}
		for _, e := range s.Connection.Endpoints {
			info.Endpoints = append(info.Endpoints, ConnEndpoint{
				Name:     e.Name,
				Protocol: e.Protocol,
				Address:  expand(e.Address, lookup),
				Note:     expand(e.Note, lookup),
			})
		}
		if len(s.Connection.Parameters) > 0 {
			info.Parameters = make(map[string]string, len(s.Connection.Parameters))
			for k, v := range s.Connection.Parameters {
				info.Parameters[k] = expand(v, lookup)
			}
		}
		for _, cr := range s.Connection.Credentials {
			rc := RenderedConnCred{
				Name:  cr.Name,
				Usage: expand(cr.Usage, lookup),
			}
			if devMode {
				secret, err := readSecret(cr.File)
				if err != nil {
					return nil, fmt.Errorf("service %s: connection credential %s: %w", s.ID, cr.Name, err)
				}
				rc.Secret = strings.TrimSpace(secret)
			}
			info.Credentials = append(info.Credentials, rc)
		}
		out.Services = append(out.Services, info)
	}
	if serviceID != "" && len(out.Services) == 0 {
		return nil, fmt.Errorf("unknown service %q; known services: %s", serviceID, strings.Join(c.serviceIDs(), ", "))
	}
	return out, nil
}

func (c *Catalog) serviceIDs() []string {
	ids := make([]string, 0, len(c.Services))
	for _, s := range c.Services {
		ids = append(ids, s.ID)
	}
	return ids
}

// expand substitutes ${VAR} (and $VAR) using lookup; unknown vars expand to
// the empty string, matching os.Expand semantics.
func expand(s string, lookup func(string) string) string {
	if s == "" {
		return ""
	}
	return os.Expand(s, lookup)
}

// ReadSecretFile is the production readSecret: it reads and trims a mounted
// secret file.
func ReadSecretFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
