package standin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// BifrostKey is the key BIFROST's stand-in takes; BifrostWithdrawnKey is one it
// knows as withdrawn.
const (
	BifrostKey          = "bf_standin"
	BifrostWithdrawnKey = "bf_withdrawn"
)

// bifrostRefusal is one refusal in EXCHANGE.md.
type bifrostRefusal struct {
	status int
	extra  map[string]any
}

var bifrostRefusals = map[string]bifrostRefusal{
	"no_key":                      {status: 401},
	"key_withdrawn":               {status: 401},
	"no_agreement":                {status: 403},
	"not_this_kind_of_fact":       {status: 403},
	"no_list_held":                {status: 404},
	"agreement_must_be_named":     {status: 409, extra: map[string]any{"agreements": []string{"AGR-TEST-1", "AGR-TEST-2"}}},
	"bad_request":                 {status: 400},
	"too_many":                    {status: 429},
	"agreement_cannot_be_checked": {status: 503},
	"list_unavailable":            {status: 503},
	"not_recorded":                {status: 503},
}

// bifrostModes is found, found with no offices, not found, each refusal, or no
// answer (E-ENB-03).
func bifrostModes() []string {
	modes := []string{"found", "found-no-offices", "not-found"}
	for _, m := range []string{"no_key", "key_withdrawn", "no_agreement", "not_this_kind_of_fact", "no_list_held",
		"agreement_must_be_named", "bad_request", "too_many", "agreement_cannot_be_checked", "list_unavailable", "not_recorded"} {
		modes = append(modes, m)
	}
	return append(modes, "slow", "silent")
}

func (s *Server) mountBifrost(mux *http.ServeMux) {
	mux.HandleFunc("POST /bifrost/api/facts/{name}/lookup", s.serve("bifrost", s.bifrostLookup))
}

func (s *Server) bifrostLookup(w http.ResponseWriter, r *http.Request) {
	refuse := func(code string) {
		ref := bifrostRefusals[code]
		out := map[string]any{"refusal": code}
		for k, v := range ref.extra {
			out[k] = v
		}
		if code == "too_many" {
			w.Header().Set("Retry-After", "60")
		}
		writeJSON(w, ref.status, out)
	}
	mode, p := s.mode("bifrost")
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case !ok || (key != BifrostKey && key != BifrostWithdrawnKey):
		refuse("no_key")
		return
	case key == BifrostWithdrawnKey:
		refuse("key_withdrawn")
		return
	case r.PathValue("name") != "scroll.staff-lookup":
		refuse("no_agreement")
		return
	}
	if _, refused := bifrostRefusals[mode]; refused {
		refuse(mode)
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Email) == "" {
		refuse("bad_request")
		return
	}
	written := time.Now().UTC().Format(time.RFC3339)
	switch mode {
	case "not-found":
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "written_at": written})
	case "found-no-offices":
		writeJSON(w, http.StatusOK, map[string]any{"found": true, "written_at": written, "offices": []any{}})
	default:
		office, post := p["office"], p["post"]
		if office == "" {
			office = "EP-I-1"
		}
		if post == "" {
			post = "Invented Clerk"
		}
		writeJSON(w, http.StatusOK, map[string]any{"found": true, "written_at": written,
			"offices": []map[string]string{{"office": office, "post": post}}})
	}
}
