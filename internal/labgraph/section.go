package labgraph

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

type sectionKind int

const (
	sectionEmpty sectionKind = iota
	sectionOperations
	sectionDocument
	sectionUnknown
)

func classifySection(n *yaml.Node) (sectionKind, map[string]any, error) {
	if !sectionNonEmpty(n) {
		return sectionEmpty, nil, nil
	}
	var m map[string]any
	if err := n.Decode(&m); err != nil {
		return sectionUnknown, nil, err
	}
	if m == nil {
		return sectionEmpty, nil, nil
	}
	if _, ok := m["operations"]; ok {
		return sectionOperations, m, nil
	}
	_, hasAPI := m["apiVersion"]
	_, hasKind := m["kind"]
	if hasAPI && hasKind {
		return sectionDocument, m, nil
	}
	return sectionUnknown, m, nil
}

func jsonRaw(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func validateEnvelope(kind sectionKind, m map[string]any) (json.RawMessage, error) {
	switch kind {
	case sectionOperations:
		body := map[string]any{"operations": m["operations"]}
		if r, ok := m["reason"]; ok {
			body["reason"] = r
		}
		return jsonRaw(body)
	case sectionDocument:
		return jsonRaw(map[string]any{"state": m})
	default:
		return nil, fmt.Errorf("section is not a native operations payload or desired-state document")
	}
}

func changeEnvelope(m map[string]any, expectedRevision string) (json.RawMessage, error) {
	body := map[string]any{"operations": m["operations"]}
	if r, ok := m["reason"]; ok {
		body["reason"] = r
	}
	if expectedRevision != "" {
		body["expectedRevision"] = expectedRevision
	}
	return jsonRaw(body)
}

func revisionFromSection(m map[string]any) string {
	if m == nil {
		return ""
	}
	if v, ok := m["expectedRevision"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
