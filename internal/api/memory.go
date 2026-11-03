package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Rumeasiyan/rosetta/internal/memory"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
)

// MemoryAdder keeps approved text (R-MEM-01).
type MemoryAdder interface {
	Add(ctx context.Context, q memory.AddRequest) (memory.Added, *reasons.Failure)
}

// memoryBody is what a calling system sends. A field left out reads as empty, and is refused as that;
// sensitivity has no default, and any but ordinary is refused (R-MEM-03).
type memoryBody struct {
	From         string `json:"from"`
	To           string `json:"to"`
	Sensitivity  string `json:"sensitivity"`
	ApproverPost string `json:"approver_post"`
	ApprovedOn   string `json:"approved_on"`
	Passages     []*struct {
		Source   *string `json:"source"`
		Approved *string `json:"approved"`
	} `json:"passages"`
}

// parseMemory reads the body. Nothing of it reaches the failure: a JSON error can quote what it read.
func parseMemory(q *Request, r *http.Request) (memory.AddRequest, *reasons.Failure) {
	var in memoryBody
	if json.Unmarshal(q.Body, &in) != nil {
		return memory.AddRequest{}, reasons.New(reasons.Invalid)
	}
	out := memory.AddRequest{Ref: q.Ref, CallerID: q.Caller.ID, System: q.Caller.Reference, From: in.From, To: in.To, Sensitivity: in.Sensitivity,
		ApproverPost: in.ApproverPost, ApprovedOn: in.ApprovedOn, Visitor: visitor(r), Passages: make([]memory.Passage, len(in.Passages))}
	for i, p := range in.Passages {
		if p == nil || p.Source == nil || p.Approved == nil {
			return memory.AddRequest{}, reasons.New(reasons.Invalid).At(i)
		}
		out.Passages[i] = memory.Passage{Source: *p.Source, Approved: *p.Approved}
	}
	return out, nil
}

// memoryAnswer says what joined (R-MEM-02); not_kept names each passage whose pieces did not line up.
type memoryAnswer struct {
	RequestRef      string `json:"request_ref"`
	From            string `json:"from"`
	To              string `json:"to"`
	GlossaryVersion int    `json:"glossary_version"`
	Pieces          int    `json:"pieces"`
	NotKept         []int  `json:"not_kept"`
}

func (s *Server) memoryAdd(w http.ResponseWriter, r *http.Request) {
	q := requestOf(r)
	if s.deps.Memory == nil {
		s.refuse(w, r, reasons.New(reasons.NoProvider))
		return
	}
	req, fail := parseMemory(q, r)
	if fail == nil {
		var a memory.Added
		a, fail = s.deps.Memory.Add(r.Context(), req)
		if a.Mismatch {
			s.counts.AddMismatch(q.Caller.Reference)
		}
		if fail == nil {
			writeJSON(w, memoryAnswer{RequestRef: q.Ref, From: req.From, To: req.To, GlossaryVersion: a.GlossaryVersion, Pieces: a.Pieces, NotKept: a.NotKept})
			return
		}
	}
	s.deps.Logger.Info("approved text was not taken", "request_ref", q.Ref, "system", q.Caller.Reference, "reason", string(fail.Reason))
	s.refuse(w, r, fail)
}
