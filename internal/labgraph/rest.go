package labgraph

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// REST mounts /v1 adapters over Service. It does not call MCP.
func REST(mux *http.ServeMux, svc *Service, sess *SessionStore) {
	mux.HandleFunc("/v1/health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
	})
	mux.HandleFunc("/v1/health/ready", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("/v1/session", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			sess.Create(w, r)
		case http.MethodGet:
			sess.Get(w, r)
		case http.MethodDelete:
			sess.Delete(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v1/scenarios", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		names, err := svc.List(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": names})
	})
	mux.HandleFunc("/v1/scenarios/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/v1/scenarios/")
		name, verb := splitAction(rest)
		if name == "" {
			writeErr(w, http.StatusNotFound, "missing name")
			return
		}
		switch {
		case verb == "" && r.Method == http.MethodGet:
			doc, err := svc.Get(r.Context(), name)
			if err != nil {
				writeErr(w, http.StatusNotFound, err.Error())
				return
			}
			view, err := scenarioView(doc)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, view)
		case verb == "status" && r.Method == http.MethodGet:
			st, err := svc.Status(r.Context(), name)
			if err != nil {
				writeErr(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, st)
		case verb == "validate" && r.Method == http.MethodPost:
			res, err := svc.Validate(r.Context(), name)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, statusOK(res), res)
		case verb == "plan" && r.Method == http.MethodPost:
			var req ApplyRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			res, err := svc.Plan(r.Context(), name, req)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, statusOK(res), res)
		case verb == "apply" && r.Method == http.MethodPost:
			var req ApplyRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			res, err := svc.Apply(r.Context(), name, req)
			if err != nil {
				code := http.StatusBadRequest
				if strings.Contains(err.Error(), "generation") {
					code = http.StatusConflict
				}
				writeErr(w, code, err.Error())
				return
			}
			writeJSON(w, statusOK(res), res)
		case verb == "reset" && r.Method == http.MethodPost:
			var req ResetRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			res, err := svc.Reset(r.Context(), name, req)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, statusOK(res), res)
		default:
			writeErr(w, http.StatusNotFound, "unknown scenario route")
		}
	})
	mux.HandleFunc("/v1/fixtures/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/v1/fixtures/")
		id, verb := splitAction(rest)
		if id == "" {
			writeErr(w, http.StatusNotFound, "missing id")
			return
		}
		switch {
		case verb == "" && r.Method == http.MethodGet:
			view, err := svc.GetFixture(r.Context(), id)
			if err != nil {
				writeErr(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, view)
		case verb == "apply" && r.Method == http.MethodPost:
			var req ApplyRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			res, err := svc.ApplyFixture(r.Context(), id, req)
			if err != nil {
				code := http.StatusBadRequest
				if strings.Contains(err.Error(), "generation") {
					code = http.StatusConflict
				}
				if strings.Contains(err.Error(), errNotFixture) {
					code = http.StatusNotFound
				}
				writeErr(w, code, err.Error())
				return
			}
			writeJSON(w, statusOK(res), res)
		default:
			writeErr(w, http.StatusNotFound, "unknown fixture route")
		}
	})
}

func statusOK(res *GraphResult) int {
	if res != nil && !res.OK {
		return http.StatusConflict
	}
	return http.StatusOK
}

func splitAction(rest string) (name, verb string) {
	// name, name:validate, name/status
	if i := strings.Index(rest, ":"); i >= 0 {
		return rest[:i], rest[i+1:]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i], rest[i+1:]
	}
	return rest, ""
}
