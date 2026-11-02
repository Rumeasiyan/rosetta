package api

import (
	"context"
	"encoding/json"
	"net/http"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/Rumeasiyan/rosetta/internal/usage"
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

// pieceAnswer says where one piece came from (R-TRN-06).
type pieceAnswer struct {
	Origin   string `json:"origin"`
	Source   string `json:"source,omitempty"`
	Version  string `json:"version,omitempty"`
	Fallback bool   `json:"fallback_used"`
}

type sourceAnswer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// passageAnswer is one passage in the answer: every field R-TRN-06 and R-TRN-07 ask for,
// and the language of its text, for screen readers (R-NFR-05).
type passageAnswer struct {
	Text            string            `json:"text"`
	Language        string            `json:"language"`
	Origins         map[string]int    `json:"origins"`
	Pieces          []pieceAnswer     `json:"pieces"`
	Sources         []sourceAnswer    `json:"sources"`
	Machine         bool              `json:"machine"`
	CheckedByPerson bool              `json:"checked_by_person"`
	Label           map[string]string `json:"label,omitempty"`
	Fallback        bool              `json:"fallback_used"`
	GlossaryVersion int               `json:"glossary_version"`
	// TermsNotPlaced is each published term a source lost, flagged for the reader (R-GLO-03).
	TermsNotPlaced []string `json:"terms_not_placed,omitempty"`
}

// translateAnswer is the answer to a request (R-TRN-01, R-TRN-06).
type translateAnswer struct {
	RequestRef      string          `json:"request_ref"`
	From            string          `json:"from"`
	To              string          `json:"to"`
	Sensitivity     string          `json:"sensitivity"`
	Mismatch        bool            `json:"sensitivity_mismatch"`
	GlossaryVersion int             `json:"glossary_version"`
	Fallback        bool            `json:"fallback_used"`
	Passages        []passageAnswer `json:"passages"`
}

// shape turns the pipeline's answer into the one published shape (R-CAL-08).
func shape(ref string, a translate.Answer) translateAnswer {
	out := translateAnswer{RequestRef: ref, From: a.From, To: a.To, Sensitivity: a.Sensitivity, Mismatch: a.Mismatch,
		GlossaryVersion: a.GlossaryVersion, Passages: make([]passageAnswer, len(a.Passages))}
	for i, p := range a.Passages {
		x := passageAnswer{Text: p.Text, Language: a.To, Origins: p.Origins(), Pieces: make([]pieceAnswer, len(p.Pieces)), Sources: []sourceAnswer{},
			Machine: p.Machine(), CheckedByPerson: p.CheckedByPerson(), Fallback: p.Fallback(), GlossaryVersion: a.GlossaryVersion, TermsNotPlaced: p.TermsNotPlaced}
		for j, piece := range p.Pieces {
			x.Pieces[j] = pieceAnswer{Origin: piece.Origin, Source: piece.Source, Version: piece.Version, Fallback: piece.Fallback}
		}
		for _, u := range p.Sources() {
			x.Sources = append(x.Sources, sourceAnswer{Name: u.Name, Version: u.Version})
		}
		if x.Machine {
			x.Label = translate.Label()
		}
		out.Fallback = out.Fallback || x.Fallback
		out.Passages[i] = x
	}
	return out
}

// noteRequest keeps what a usage record holds of a request: languages and counts, never text.
// What is not on the fixed lists is left out when the record is written (usage.Keep).
func noteRequest(rec *usage.Record, q translate.Request) {
	rec.From, rec.To, rec.Sensitivity, rec.Passages = q.From, q.To, q.Sensitivity, len(q.Passages)
	for _, p := range q.Passages {
		rec.Characters += utf8.RuneCountInString(p)
	}
}

// noteTally keeps what a request did. Where it was refused before anything was read, the
// sensitivity stated stays.
func noteTally(rec *usage.Record, t *translate.Tally) {
	if t.Sensitivity != "" {
		rec.Sensitivity = t.Sensitivity
	}
	rec.Mismatch, rec.Fallback, rec.Sources = t.Mismatch, t.Fallback, t.Sources
	rec.Pieces = usage.Pieces{Glossary: t.Pieces[translate.OriginGlossary], Memory: t.Pieces[translate.OriginMemory],
		Cache: t.Pieces[translate.OriginCache], Source: t.Pieces[translate.OriginSource]}
}

func (s *Server) translate(w http.ResponseWriter, r *http.Request) {
	q := requestOf(r)
	if s.deps.Translator == nil {
		s.refuse(w, r, reasons.New(reasons.NoProvider))
		return
	}
	req, fail := parseTranslate(q.Ref, q.Body)
	if fail == nil {
		var tally translate.Tally
		req.Tally = &tally
		noteRequest(q.Use, req)
		var a translate.Answer
		a, fail = s.deps.Translator.Translate(r.Context(), req)
		noteTally(q.Use, &tally)
		if fail == nil {
			writeJSON(w, shape(q.Ref, a))
			return
		}
	}
	s.deps.Logger.Info("a request was not answered", "request_ref", q.Ref, "system", q.Caller.Reference, "reason", string(fail.Reason))
	s.refuse(w, r, fail)
}
