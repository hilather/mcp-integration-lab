package labgraph

// Disposition is derived: RESTOnly → REST_ONLY_PROTOCOL, else PARITY_REQUIRED.
type Disposition string

const (
	ParityRequired    Disposition = "PARITY_REQUIRED"
	RESTOnlyProtocol  Disposition = "REST_ONLY_PROTOCOL"
)

type Capability struct {
	ID             string
	Title          string
	Description    string
	RESTOnly       bool
	REST           []RESTBinding
	MCP            *MCPBinding
	UI             *UIBinding
	ServiceMethods []string
}

type RESTBinding struct {
	Method string
	Path   string
}

type MCPBinding struct {
	Tool string
}

type UIBinding struct {
	Route  string
	Action string
}

func (c Capability) Disposition() Disposition {
	if c.RESTOnly {
		return RESTOnlyProtocol
	}
	return ParityRequired
}

// Registry is the single operation catalog. REST and MCP adapters bind
// ServiceMethods by name. Adapters must not call each other.
func Registry() []Capability {
	return []Capability{
		{
			ID: "scenario.list", Title: "List scenarios",
			REST: []RESTBinding{{"GET", "/v1/scenarios"}},
			MCP:  &MCPBinding{Tool: "scenario_list"},
			UI:   &UIBinding{Route: "/", Action: "view"},
			ServiceMethods: []string{"List"},
		},
		{
			ID: "scenario.get", Title: "Get scenario",
			REST: []RESTBinding{{"GET", "/v1/scenarios/{name}"}},
			MCP:  &MCPBinding{Tool: "scenario_get"},
			UI:   &UIBinding{Route: "/scenarios/:name", Action: "view"},
			ServiceMethods: []string{"Get"},
		},
		{
			ID: "scenario.validate", Title: "Validate scenario",
			REST: []RESTBinding{{"POST", "/v1/scenarios/{name}:validate"}},
			MCP:  &MCPBinding{Tool: "scenario_validate"},
			UI:   &UIBinding{Route: "/scenarios/:name", Action: "mutate"},
			ServiceMethods: []string{"Validate"},
		},
		{
			ID: "scenario.plan", Title: "Plan scenario",
			REST: []RESTBinding{{"POST", "/v1/scenarios/{name}:plan"}},
			MCP:  &MCPBinding{Tool: "scenario_plan"},
			UI:   &UIBinding{Route: "/scenarios/:name", Action: "mutate"},
			ServiceMethods: []string{"Plan"},
		},
		{
			ID: "scenario.apply", Title: "Apply scenario",
			REST: []RESTBinding{{"POST", "/v1/scenarios/{name}:apply"}},
			MCP:  &MCPBinding{Tool: "scenario_apply"},
			UI:   &UIBinding{Route: "/scenarios/:name", Action: "mutate"},
			ServiceMethods: []string{"Apply"},
		},
		{
			ID: "scenario.reset", Title: "Reset appliances",
			REST: []RESTBinding{{"POST", "/v1/scenarios/{name}:reset"}},
			MCP:  &MCPBinding{Tool: "scenario_reset"},
			UI:   &UIBinding{Route: "/scenarios/:name", Action: "mutate"},
			ServiceMethods: []string{"Reset"},
		},
		{
			ID: "scenario.status", Title: "Scenario status",
			REST: []RESTBinding{{"GET", "/v1/scenarios/{name}/status"}},
			MCP:  &MCPBinding{Tool: "scenario_status"},
			UI:   &UIBinding{Route: "/scenarios/:name", Action: "view"},
			ServiceMethods: []string{"Status"},
		},
		{
			ID: "health.live", Title: "Liveness", RESTOnly: true,
			REST: []RESTBinding{{"GET", "/v1/health/live"}},
			UI:   &UIBinding{Route: "/", Action: "view"},
			ServiceMethods: []string{"Live"},
		},
		{
			ID: "health.ready", Title: "Readiness", RESTOnly: true,
			REST: []RESTBinding{{"GET", "/v1/health/ready"}},
			UI:   &UIBinding{Route: "/", Action: "view"},
			ServiceMethods: []string{"Ready"},
		},
		{
			ID: "session.create", Title: "Create session", RESTOnly: true,
			REST: []RESTBinding{{"POST", "/v1/session"}},
			UI:   &UIBinding{Route: "/", Action: "mutate"},
			ServiceMethods: []string{"CreateSession"},
		},
		{
			ID: "session.get", Title: "Get session", RESTOnly: true,
			REST: []RESTBinding{{"GET", "/v1/session"}},
			UI:   &UIBinding{Route: "/", Action: "view"},
			ServiceMethods: []string{"GetSession"},
		},
		{
			ID: "session.delete", Title: "Delete session", RESTOnly: true,
			REST: []RESTBinding{{"DELETE", "/v1/session"}},
			UI:   &UIBinding{Route: "/", Action: "mutate"},
			ServiceMethods: []string{"DeleteSession"},
		},
	}
}
