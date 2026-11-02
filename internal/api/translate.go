package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/translate"
)

// Translator is the piece pipeline.
type Translator interface {
	Translate(ctx context.Context, q translate.Request) (translate.Answer, *reasons.Failure)
}

// translateBody is what a caller sends (R-TRN-01). A field left out reads as empty, and
// is refused as that; a field not named here is ignored, so later additions do not
// break an older ROSETTA (R-ADM-07).
type translateBody struct {
	From        string    `json:"from"`
	To          string    `json:"to"`
	Sensitivity string    `json:"sensitivity"`
	Passages    []*string `json:"passages"`
}

// parseTranslate reads the body. Nothing of it reaches the failure: a JSON error can
// quote what it read, so it is dropped, never logged or answered.
func parseTranslate(ref string, body []byte) (translate.Request, *reasons.Failure) {
	var in translateBody
	if json.Unmarshal(body, &in) != nil {
		return translate.Request{}, reasons.New(reasons.Invalid)
	}
	q := translate.Request{Ref: ref, From: in.From, To: in.To, Sensitivity: in.Sensitivity, Passages: make([]string, len(in.Passages))}
	for i, p := range in.Passages {
		if p == nil {
			return translate.Request{}, reasons.New(reasons.Invalid).At(i)
		}
		q.Passages[i] = *p
	}
	return q, nil
}

// passageAnswer is one passage in the answer.
type passageAnswer struct {
	Text string `json:"text"`
}

// translateAnswer is the answer to a request (R-TRN-01, R-TRN-06).
type translateAnswer struct {
	RequestRef string          `json:"request_ref"`
	Passages   []passageAnswer `json:"passages"`
}

func (s *Server) translate(w http.ResponseWriter, r *http.Request) {
	q := requestOf(r)
	if s.deps.Translator == nil {
		s.refuse(w, r, reasons.New(reasons.NoProvider))
		return
	}
	req, fail := parseTranslate(q.Ref, q.Body)
	if fail == nil {
		var a translate.Answer
		if a, fail = s.deps.Translator.Translate(r.Context(), req); fail == nil {
			out := translateAnswer{RequestRef: q.Ref, Passages: make([]passageAnswer, len(a.Passages))}
			for i, p := range a.Passages {
				out.Passages[i] = passageAnswer{Text: p.Text}
			}
			writeJSON(w, out)
			return
		}
	}
	s.deps.Logger.Info("a request was not answered", "request_ref", q.Ref, "system", q.Caller.Reference, "reason", string(fail.Reason))
	s.refuse(w, r, fail)
}
