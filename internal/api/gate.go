package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/sources"
)

// gateAnswer is one direction as a system reads it (R-GAT-04), so a closed direction
// can offer human translation instead of a broken button.
type gateAnswer struct {
	From        string       `json:"from"`
	To          string       `json:"to"`
	State       string       `json:"state"`
	Sources     []gateSource `json:"sources"`
	EvaluatedOn string       `json:"evaluated_on,omitempty"`
}

type gateSource struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	EvaluatedOn string `json:"evaluated_on"`
}

// gate answers every direction's state. A signed call carries no query, so there is
// nothing to choose: a system reads all six.
func (s *Server) gate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if s.deps.Snapshot == nil || s.deps.Policy == nil {
		s.refuse(w, r, reasons.New(reasons.NoProvider))
		return
	}
	var out []gateAnswer
	for _, st := range sources.States(s.deps.Snapshot(ctx).List, s.deps.Policy(ctx)) {
		a := gateAnswer{From: st.From, To: st.To, State: sources.GateClosed, Sources: []gateSource{}}
		if st.Open {
			a.State = sources.GateOpen
		}
		for _, x := range st.Sources {
			a.Sources = append(a.Sources, gateSource{Name: x.Name, Version: x.Version, EvaluatedOn: x.EvaluatedOn.Format(time.DateOnly)})
		}
		if st.EvaluatedOn != nil {
			a.EvaluatedOn = st.EvaluatedOn.Format(time.DateOnly)
		}
		out = append(out, a)
	}
	writeJSON(w, map[string]any{"request_ref": requestOf(r).Ref, "directions": out})
}

func writeJSON(w http.ResponseWriter, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
