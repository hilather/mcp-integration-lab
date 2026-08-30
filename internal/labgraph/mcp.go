package labgraph

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer registers scenario_* tools that call Service directly (not REST).
func MCPServer(svc *Service) *server.MCPServer {
	s := server.NewMCPServer("labgraph", "0.1.0",
		server.WithToolCapabilities(false),
		server.WithInstructions("LabScenario orchestrator. list/get/validate/plan/apply/reset/status. Apply is sequential DNS→MITM→mail→LDAP→TacLab; partial failure is honest; omitted appliances are left alone. LabLDAP and TacLab have no native file-level apply."),
	)
	add := func(name, desc string, fn func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), opts ...mcp.ToolOption) {
		opts = append([]mcp.ToolOption{mcp.WithDescription(desc)}, opts...)
		s.AddTool(mcp.NewTool(name, opts...), fn)
	}
	add("scenario_list", "List LabScenario names from the profile scenarios directory.",
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			names, err := svc.List(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return marshalTool(names)
		}, mcp.WithReadOnlyHintAnnotation(true))
	add("scenario_get", "Get one LabScenario by metadata.name.",
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			doc, err := svc.Get(ctx, req.GetString("name", "default"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return marshalTool(map[string]any{"name": doc.Metadata.Name, "kind": doc.Kind, "apiVersion": doc.APIVersion})
		}, mcp.WithString("name", mcp.Description("Scenario metadata.name")), mcp.WithReadOnlyHintAnnotation(true))
	add("scenario_validate", "Validate a LabScenario. Family sections call native POST /v1/state:validate. A present labldap/labtacacs section fails closed.",
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			res, err := svc.Validate(ctx, req.GetString("name", "default"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return marshalTool(res)
		}, mcp.WithString("name", mcp.Description("Scenario metadata.name")))
	add("scenario_plan", "Dry-run apply. Family operations call POST /v1/changes:plan with expectedRevision from GET /v1/state when omitted.",
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			res, err := svc.Plan(ctx, req.GetString("name", "default"), applyReqFromMCP(req))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return marshalTool(res)
		}, mcp.WithString("name", mcp.Description("Scenario metadata.name")),
		mcp.WithString("expectedRevision", mcp.Description("Optional JSON object of appliance id → expectedRevision")),
		mcp.WithNumber("generation", mcp.Description("Optional labgraph journal generation for OCC")))
	add("scenario_apply", "Apply in order DNS→MITM→mail→LDAP→TacLab. Stop on first failure; no auto-rollback. Empty default is a no-op.",
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			res, err := svc.Apply(ctx, req.GetString("name", "default"), applyReqFromMCP(req))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return marshalTool(res)
		}, mcp.WithString("name", mcp.Description("Scenario metadata.name")),
		mcp.WithString("expectedRevision", mcp.Description("Optional JSON object of appliance id → expectedRevision")),
		mcp.WithNumber("generation", mcp.Description("Optional labgraph journal generation for OCC")))
	add("scenario_reset", "Native reset of named appliances or all five (family :reset, LabLDAP POST /api/v1/reset, TacLab runtime.reset). Empty omit = all five — a lab-wide mutation even on the empty default scenario. Do not use that on default in smoke.",
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var apps []string
			if raw := req.GetString("appliances", ""); raw != "" {
				if err := json.Unmarshal([]byte(raw), &apps); err != nil {
					apps = splitCSV(raw)
				}
			}
			res, err := svc.Reset(ctx, req.GetString("name", "default"), ResetRequest{Appliances: apps})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return marshalTool(res)
		},
		mcp.WithString("name", mcp.Description("Scenario metadata.name")),
		mcp.WithString("appliances", mcp.Description("Optional comma-separated appliance ids; omit = all five")))
	add("scenario_status", "Last apply/reset journal plus native revisions when reachable.",
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			st, err := svc.Status(ctx, req.GetString("name", "default"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return marshalTool(st)
		}, mcp.WithString("name", mcp.Description("Scenario metadata.name")), mcp.WithReadOnlyHintAnnotation(true))
	return s
}

func applyReqFromMCP(req mcp.CallToolRequest) ApplyRequest {
	out := ApplyRequest{Reason: req.GetString("reason", "")}
	if raw := req.GetString("expectedRevision", ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out.ExpectedRevision)
	}
	if v, err := req.RequireInt("generation"); err == nil {
		g := int64(v)
		out.Generation = &g
	}
	return out
}

func marshalTool(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range splitComma(s) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			out = append(out, trimSpace(s[start:i]))
			start = i + 1
		}
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
