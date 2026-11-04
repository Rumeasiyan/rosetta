package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/grammar"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/Rumeasiyan/rosetta/internal/tva"
	"github.com/Rumeasiyan/rosetta/internal/usage"
)

// TestRequest is a test sent from the screens for a system (R-ADM-04): the same pipeline and the
// same answer, with invented text that nothing keeps. Grammar says it is a check, of Text alone.
type TestRequest struct {
	Caller      callers.Caller
	Officer     string // who sent it
	Grammar     bool
	From, To    string
	Sensitivity string
	Text        string
}

// TestAnswer is the full answer, in the one published shape, for a person to read.
type TestAnswer struct {
	Ref      string
	Reason   string // "" when it was answered
	Sources  []string
	Fallback bool
	Label    map[string]string // the machine label, by language, when a machine made any of it
	JSON     string
}

// Test runs a test through the system's own limits, and records it as operation test (E-ADM-05).
// Annex A says test requests are never stored, so the translator here keeps nothing in the cache.
func (s *Server) Test(ctx context.Context, q TestRequest) TestAnswer {
	started := s.deps.Now()
	ref := NewRef()
	rec := &usage.Record{Ref: ref, CallerID: q.Caller.ID, Officer: q.Officer, Operation: usage.OpTest, Outcome: usage.Answered}
	var out any
	var label map[string]string
	var fail *reasons.Failure
	release, wait, allowed := func() (func(), time.Duration, bool) {
		if !tva.MayCall(q.Caller.TVAStatus) {
			return func() {}, 0, true
		}
		return s.limiter.Acquire(q.Caller.ID, q.Caller.PerMinute, q.Caller.AtOnce)
	}()
	defer release()
	switch {
	case !tva.MayCall(q.Caller.TVAStatus):
		fail = reasons.New(reasons.NoKey)
	case !allowed:
		fail = reasons.New(reasons.OverLimit).Until(s.deps.Now().Add(wait))
	case q.Grammar && s.deps.Grammar == nil, !q.Grammar && s.deps.TestTranslator == nil:
		fail = reasons.New(reasons.NoProvider)
	case q.Grammar:
		req := grammar.Request{Ref: ref, Language: "en-GB", Text: q.Text}
		noteCheck(rec, req)
		var a grammar.Answer
		if a, fail = s.deps.Grammar.Check(ctx, req); fail == nil {
			noteChecked(rec, a)
			out = shapeCheck(ref, q.Text, a)
		}
	default:
		var tally translate.Tally
		req := translate.Request{Ref: ref, From: q.From, To: q.To, Sensitivity: q.Sensitivity, Passages: []string{q.Text}, Tally: &tally}
		noteRequest(rec, req)
		var a translate.Answer
		a, fail = s.deps.TestTranslator.Translate(ctx, req)
		noteTally(rec, &tally)
		if fail == nil {
			out = shape(ref, a)
			for _, p := range a.Passages {
				if p.Machine() {
					label = p.Label()
				}
			}
		}
	}
	answer := TestAnswer{Ref: ref, Label: label}
	if fail != nil {
		rec.Outcome, answer.Reason = string(fail.Reason), string(fail.Reason)
		out = reasons.Body(ref, fail)
	}
	answer.Sources, answer.Fallback = rec.Sources, rec.Fallback
	if raw, err := json.MarshalIndent(out, "", "  "); err == nil {
		answer.JSON = string(raw)
	}
	if s.deps.Usage != nil {
		rec.DurationMS = s.deps.Now().Sub(started).Milliseconds()
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), denialTimeout)
		defer cancel()
		if err := s.deps.Usage(cctx, *rec); err != nil {
			s.deps.Logger.Error("a usage record was not kept", "request_ref", ref)
		}
	}
	return answer
}
