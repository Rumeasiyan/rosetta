package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/runs"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/Rumeasiyan/rosetta/internal/usage"
)

// runsPath is where background runs are started; each has its own address below it (R-TRN-10).
const runsPath = "/api/v1/translate/runs"

// RunStarter checks a run as a direct request would be checked, and answers what it can without a
// source, inside the time of one request.
type RunStarter interface {
	Start(ctx context.Context, q translate.Request) (*translate.Work, *reasons.Failure)
}

// askingAfterRun says this call asks after a run or gives it up, which counts against neither
// requests a minute nor requests at once (R-CAL-07). A HEAD is neither, and is refused.
func askingAfterRun(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodDelete) && strings.HasPrefix(r.URL.Path, runsPath+"/")
}

// runStarted is the answer to a run taken on (R-TRN-10). There is no text here.
type runStarted struct {
	Run        string `json:"run"`
	State      string `json:"state"`
	AskAgainAt string `json:"ask_again_at"`
}

// runState is the answer while a run works: never any text (R-TRN-11).
type runState struct {
	State      string `json:"state"`
	PiecesDone int    `json:"pieces_done"`
	PiecesAll  int    `json:"pieces_total"`
	AskAgainAt string `json:"ask_again_at"`
}

// runTask is one run's work. Its text and masks are inside work, in this process's memory alone
// (R-TRN-13). What it logs or records is fixed words, reasons and counts.
type runTask struct {
	s       *Server
	work    *translate.Work
	tally   *translate.Tally
	rec     *usage.Record
	system  string
	started time.Time
}

func (t *runTask) Pieces() (done, total int) { return t.work.Pieces() }

// Work has the answer ready to send, as the same bytes a direct request would have sent.
func (t *runTask) Work(ctx context.Context) runs.Result {
	a, fail := t.work.Finish(ctx)
	if fail != nil {
		return runs.Result{Failure: fail}
	}
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(shape(t.rec.Ref, a)); err != nil {
		t.s.deps.Logger.Error("a run's answer could not be made", "request_ref", t.rec.Ref)
		return runs.Result{Failure: reasons.New(reasons.NoProvider)}
	}
	return runs.Result{Body: b.Bytes()}
}

// Ended writes the run's one record (R-REC-01), and notes how it ended, in fixed words.
func (t *runTask) Ended(r runs.Result) {
	rec := *t.rec
	noteTally(&rec, t.tally)
	switch {
	case r.GivenUp:
		rec.Outcome = usage.GivenUp
	case r.Failure != nil:
		rec.Outcome = string(r.Failure.Reason)
	default:
		rec.Outcome = usage.Answered
	}
	t.s.deps.Logger.Info("a run ended", "request_ref", rec.Ref, "system", t.system, "outcome", rec.Outcome)
	if t.s.deps.Usage == nil {
		return
	}
	rec.DurationMS = t.s.deps.Now().Sub(t.started).Milliseconds()
	ctx, cancel := context.WithTimeout(context.Background(), denialTimeout)
	defer cancel()
	if err := t.s.deps.Usage(ctx, rec); err != nil {
		t.s.deps.Logger.Error("a usage record was not kept", "request_ref", rec.Ref)
	}
}

// runStart takes a background run on, or refuses it at once as a direct request would be
// (R-TRN-10), with over limit besides (R-TRN-12). A refused run is recorded now; one taken on when
// it ends.
func (s *Server) runStart(w http.ResponseWriter, r *http.Request) {
	q := requestOf(r)
	if s.deps.Runner == nil || s.deps.Runs == nil {
		s.refuse(w, r, reasons.New(reasons.NoProvider))
		return
	}
	started := s.deps.Now()
	req, fail := parseTranslate(q.Ref, q.Body)
	if fail == nil {
		noteRequest(q.Use, req)
		slot, wait, err := s.deps.Runs.Reserve(q.Caller.ID)
		switch {
		case errors.Is(err, runs.ErrFull):
			fail = reasons.New(reasons.OverLimit).Until(s.deps.Now().Add(wait))
		case err != nil:
			fail = reasons.New(reasons.NoProvider)
		default:
			defer slot.Release()
			var tally translate.Tally
			req.Tally = &tally
			var work *translate.Work
			work, fail = s.deps.Runner.Start(r.Context(), req)
			noteTally(q.Use, &tally)
			if fail == nil {
				task := &runTask{s: s, work: work, tally: &tally, rec: q.Use, system: q.Caller.Reference, started: started}
				run, err := s.deps.Runs.Begin(slot, q.Ref, task)
				if err == nil {
					q.NoRecord = true
					s.deps.Logger.Info("a run was taken on", "request_ref", q.Ref, "system", q.Caller.Reference)
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Cache-Control", "no-store")
					w.WriteHeader(http.StatusAccepted)
					json.NewEncoder(w).Encode(runStarted{Run: run.Ref, State: run.State, AskAgainAt: run.AskAgainAt.Format(time.RFC3339)})
					return
				}
				fail = reasons.New(reasons.NoProvider)
			}
		}
	}
	s.deps.Logger.Info("a request was not answered", "request_ref", q.Ref, "system", q.Caller.Reference, "reason", string(fail.Reason))
	s.refuse(w, r, fail)
}

// runAsk tells the system that started a run how it goes. Once it has ended this is exactly what a
// direct request would have answered, handed over this once and forgotten (R-TRN-11, R-TRN-13). If
// the connection breaks during this, the answer is gone: the system sends it again (R-TRN-05).
func (s *Server) runAsk(w http.ResponseWriter, r *http.Request) {
	q := requestOf(r)
	// ServeMux sends HEAD here too, and a HEAD must not hand over an answer it cannot carry.
	if r.Method != http.MethodGet {
		s.refuse(w, r, reasons.New(reasons.Invalid))
		return
	}
	if s.deps.Runs == nil {
		s.refuse(w, r, reasons.New(reasons.RunNotKnown))
		return
	}
	v, ok := s.deps.Runs.Ask(q.Caller.ID, r.PathValue("run"))
	if !ok {
		s.refuse(w, r, reasons.New(reasons.RunNotKnown))
		return
	}
	q.NoRecord = true // the run is one request, with one record of its own (R-REC-01)
	switch {
	case v.Result == nil:
		writeJSON(w, runState{State: v.State, PiecesDone: v.Done, PiecesAll: v.Total, AskAgainAt: v.AskAgainAt.Format(time.RFC3339)})
	case v.Result.Failure != nil:
		// The reference is the run's, which its record and every source call carry.
		reasons.Write(w, v.Result.Ref, v.Result.Failure, s.deps.Now())
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(v.Result.Body)
	}
}

// runGiveUp forgets a run at once (R-TRN-11). The record says it was given up, unless it had already
// ended, when the record says how.
func (s *Server) runGiveUp(w http.ResponseWriter, r *http.Request) {
	q := requestOf(r)
	if s.deps.Runs == nil || !s.deps.Runs.GiveUp(q.Caller.ID, r.PathValue("run")) {
		s.refuse(w, r, reasons.New(reasons.RunNotKnown))
		return
	}
	q.NoRecord = true
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
