// Package api is ROSETTA's server-to-server path under /api/: every request here
// carries its system's key, a caller holding a public key signs it too, and a key with
// none is refused if the request came from outside (R-CAL-03, R-CAL-04; STACK section 5). There are no screens here and no scopes: a key allows
// what R-CAL and R-TRN allow any system.
//
// Caller text never reaches a log, record, error or the outbox (AGENTS C4). Nothing
// here logs a body, a key or a header's value; a refusal logs fixed words.
package api

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/apisign"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/limits"
	"github.com/Rumeasiyan/rosetta/internal/oidc"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/runs"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/tva"
	"github.com/Rumeasiyan/rosetta/internal/usage"
)

// Prefix is the path this package answers under.
const Prefix = "/api/"

// MaxBody is the most read from one request. It is well above what the settings
// allow, in bytes of JSON, and any request past it is too long whatever they say.
const MaxBody = 4 << 20

// denialTimeout is the longest a refused request waits for its entry to be kept.
const denialTimeout = 5 * time.Second

// Keys is how a request's key is found.
type Keys interface {
	Find(ctx context.Context, hash string) (callers.Holder, error)
	Used(ctx context.Context, h callers.Holder) error
}

// Denial is one refused key, for PENSIEVE (P-ING-02; E-REC-02's note). It holds the
// system if the key told which, fixed words why, and where it came from; never the
// key or anything else sent.
type Denial struct {
	System  string // TVA's reference, or "" when nobody was identified
	Why     string // fixed words
	Visitor string
}

// Deps is what the path needs.
type Deps struct {
	Keys Keys
	// Denied keeps a refused key as an audit entry. Nil keeps none.
	Denied func(ctx context.Context, d Denial) error
	// Snapshot and Policy are the source list and what the administrator has set.
	Snapshot func(ctx context.Context) sources.Snapshot
	Policy   func(ctx context.Context) sources.Policy
	// Translator answers translation requests. Nil answers "no provider".
	Translator Translator
	// TestTranslator answers tests from the screens: the same pipeline as Translator, keeping
	// nothing (Annex A). Nil answers "no provider".
	TestTranslator Translator
	// Runner and Runs start background runs and hold them in memory (R-TRN-10, R-TRN-13). Nil
	// answers "no provider".
	Runner RunStarter
	Runs   *runs.Book
	// Memory keeps approved text. Nil answers "no provider".
	Memory MemoryAdder
	// Grammar answers grammar checks. Nil answers "no provider".
	Grammar GrammarChecker
	// Origins finds who lists a browser's address (R-CAL-06). Nil lists none.
	Origins Origins
	// Officers says whether a signed-in account was added (R-ADM-08). Nil refuses every browser.
	Officers Officers
	// Usage keeps each request's record (R-REC-01). Nil keeps nothing. A failure here is logged
	// as fixed words and changes no answer.
	Usage func(ctx context.Context, r usage.Record) error
	// Counts is where requests with no known system are counted. Nil makes its own.
	Counts *Counts
	Logger *slog.Logger
	Now    func() time.Time // time.Now when nil
}

// Server is the path.
type Server struct {
	deps     Deps
	verifier apisign.Verifier
	counts   *Counts
	limiter  *limits.Limiter
	routes   *http.ServeMux
	checks   *http.ServeMux
}

// New makes the path.
func New(deps Deps) *Server {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.DiscardHandler)
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Counts == nil {
		deps.Counts = NewCounts(deps.Now)
	}
	s := &Server{deps: deps, counts: deps.Counts, limiter: &limits.Limiter{Now: deps.Now}, routes: http.NewServeMux(), checks: http.NewServeMux()}
	s.verifier = apisign.Verifier{Nonces: &apisign.Nonces{}, Now: deps.Now}
	s.routes.HandleFunc("GET /api/v1/gate", s.gate)
	s.routes.HandleFunc("POST /api/v1/translate", s.translate)
	s.routes.HandleFunc("POST "+runsPath, s.runStart)
	s.routes.HandleFunc("GET "+runsPath+"/{run}", s.runAsk)
	s.routes.HandleFunc("DELETE "+runsPath+"/{run}", s.runGiveUp)
	s.routes.HandleFunc("POST /api/v1/memory", s.memoryAdd)
	s.routes.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.refuse(w, r, reasons.New(reasons.Invalid))
	})
	s.checks.HandleFunc("GET "+languagesPath, s.languages)
	s.checks.HandleFunc("POST "+checkPath, s.check)
	s.checks.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.refuse(w, r, reasons.New(reasons.Invalid))
	})
	return s
}

// Mount adds the path to mux. Grammar's (B 9) takes its own longer pattern, so a mark on
// the translation paths in CERBERUS never covers it.
func (s *Server) Mount(mux *http.ServeMux) {
	mux.Handle(Prefix, s.withKey(s.routes))
	mux.Handle(GrammarPrefix, s.grammarEntry(s.checks))
}

// Counts is the refusals and failures by reason today, for the status screen (R-CAL-04,
// R-ADM-02).
func (s *Server) Counts() *Counts { return s.counts }

type requestKey struct{}

// Request is what the key check found, for the handlers.
type Request struct {
	Ref    string // the request reference (R-TRN-06, R-REC-01)
	Caller callers.Caller
	Body   []byte
	// Browser says this came from an officer's browser (R-CAL-06), whose refusals are plain
	// text, and Officer is who is signed in there.
	Browser bool
	Officer string
	// Use is this request's usage record, written when it has been answered (R-REC-01), unless
	// NoRecord says it is part of a background run, which is one request, recorded when it ends.
	Use      *usage.Record
	NoRecord bool
}

func requestOf(r *http.Request) *Request { return r.Context().Value(requestKey{}).(*Request) }

// NewRef is a request reference, in the form BUMBLEBEE takes as a request_id. It says
// nothing of the request.
func NewRef() string { return "r-" + oidc.Random() }

// operationOf names the work the address stands for, whatever came of the request.
func operationOf(r *http.Request) string {
	if r.URL.Path == runsPath && r.Method == http.MethodPost {
		return usage.OpTranslate
	}
	switch r.URL.Path {
	case "/api/v1/translate":
		return usage.OpTranslate
	case "/api/v1/memory":
		return usage.OpMemoryAdd
	case checkPath:
		return usage.OpGrammar
	}
	return usage.OpRead
}

// keepUse writes a request's record once it has been answered, whatever its caller did with
// the answer. A request with no known system cannot have one (Annex A: caller_id), and is
// counted instead (R-CAL-04).
func (s *Server) keepUse(r *http.Request, rec *usage.Record, started time.Time) {
	if rec.CallerID == 0 {
		if rec.Outcome != usage.Answered {
			s.counts.Add(reasons.Reason(rec.Outcome))
		}
		return
	}
	if s.deps.Usage == nil {
		return
	}
	rec.DurationMS = s.deps.Now().Sub(started).Milliseconds()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), denialTimeout)
	defer cancel()
	if err := s.deps.Usage(ctx, *rec); err != nil {
		s.deps.Logger.Error("a usage record was not kept", "request_ref", rec.Ref)
	}
}

// withKey checks the key, then the signature if this key has a public key; a key with
// none works only from this server. A failure is "no key" with 401 (R-CAL-04), counted,
// and kept as a denial.
func (s *Server) withKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ref := NewRef()
		rec := &usage.Record{Ref: ref, Operation: operationOf(r), Outcome: usage.Answered}
		q := &Request{Ref: ref, Use: rec}
		started := s.deps.Now()
		defer func() {
			if !q.NoRecord {
				s.keepUse(r, rec, started)
			}
		}()
		key, ok := bearer(r)
		if !ok {
			s.refuseKey(w, r, rec, "", "No key was sent")
			return
		}
		h, err := s.deps.Keys.Find(ctx, callers.HashKey(key))
		switch {
		case errors.Is(err, callers.ErrNoSuchKey):
			s.refuseKey(w, r, rec, "", "Not a key ROSETTA knows")
			return
		case err != nil:
			s.deps.Logger.Error("a key could not be checked", "request_ref", ref)
			s.answer(w, rec, reasons.New(reasons.NoProvider))
			return
		}
		rec.CallerID = h.Caller.ID
		switch {
		case h.TurnedOff:
			s.refuseKey(w, r, rec, h.Caller.Reference, "The key is turned off")
			return
		case !tva.MayCall(h.Caller.TVAStatus):
			s.refuseKey(w, r, rec, h.Caller.Reference, "The system is neither live nor may join")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxBody+1))
		if err != nil {
			s.answer(w, rec, reasons.New(reasons.Invalid))
			return
		}
		if len(body) > MaxBody {
			s.answer(w, rec, reasons.New(reasons.TooLong))
			return
		}
		switch {
		case h.PublicKey != "":
			if err := s.checkSignature(r, body, h); err != nil {
				s.deps.Logger.Warn("a signature was refused", "request_ref", ref, "system", h.Caller.Reference, "why", err.Error())
				s.refuseKey(w, r, rec, h.Caller.Reference, "The signature did not pass")
				return
			}
		case throughFrontDoor(r):
			s.deps.Logger.Warn("an unsigned key came from outside", "request_ref", ref, "system", h.Caller.Reference)
			s.refuseKey(w, r, rec, h.Caller.Reference, "A key with no public key came from outside")
			return
		}
		if err := s.deps.Keys.Used(ctx, h); err != nil {
			s.deps.Logger.Error("a key's use was not noted", "request_ref", ref)
		}
		// Over a limit is no denial (E-REC-02's note): the key was good. Limits are read
		// with the key, so a change is in force at once. Asking after a run counts against
		// neither (R-CAL-07).
		if !askingAfterRun(r) {
			release, wait, allowed := s.limiter.Acquire(h.Caller.ID, h.Caller.PerMinute, h.Caller.AtOnce)
			if !allowed {
				s.answer(w, rec, reasons.New(reasons.OverLimit).Until(s.deps.Now().Add(wait)))
				return
			}
			defer release()
		}
		q.Caller, q.Body = h.Caller, body
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, requestKey{}, q)))
	})
}

func (s *Server) checkSignature(r *http.Request, body []byte, h callers.Holder) error {
	var pub ed25519.PublicKey
	pub, err := callers.PublicKey(h.PublicKey)
	if err != nil {
		return apisign.ErrBadSignature
	}
	return s.verifier.Verify(r, body, h.Prefix, pub)
}

// throughFrontDoor says the request came through Cloudflare and Caddy, not directly on
// the platform network (B 3, B 7). Caddy sets Cf-Connecting-Ip on every site and its
// reverse proxy adds X-Forwarded-For, whatever the caller sent, so a caller outside
// cannot take them away; one who forges them refuses only itself. Being there at all
// counts, even empty.
func throughFrontDoor(r *http.Request) bool {
	return len(r.Header.Values("Cf-Connecting-Ip")) > 0 || len(r.Header.Values("X-Forwarded-For")) > 0
}

// bearer is the key from the Authorization header.
func bearer(r *http.Request) (string, bool) {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	key = strings.TrimSpace(key)
	return key, ok && key != ""
}

// refuseKey answers "no key", and keeps the denial: that keeping never changes the
// answer. The caller's request does not wait long for it.
func (s *Server) refuseKey(w http.ResponseWriter, r *http.Request, rec *usage.Record, system, why string) {
	s.answer(w, rec, reasons.New(reasons.NoKey))
	s.keepDenial(r, rec.Ref, system, why)
}

// keepDenial keeps a refused call as an audit entry, after it has been answered.
func (s *Server) keepDenial(r *http.Request, ref, system, why string) {
	if s.deps.Denied == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), denialTimeout)
	defer cancel()
	if err := s.deps.Denied(ctx, Denial{System: system, Why: why, Visitor: visitor(r)}); err != nil {
		s.deps.Logger.Error("a refused key was not kept as a denial", "request_ref", ref)
	}
}

// answer writes a failure, and notes it for the request's record.
func (s *Server) answer(w http.ResponseWriter, rec *usage.Record, f *reasons.Failure) {
	rec.Outcome = string(f.Reason)
	reasons.Write(w, rec.Ref, f, s.deps.Now())
}

// refuse answers a request that passed the key check.
func (s *Server) refuse(w http.ResponseWriter, r *http.Request, f *reasons.Failure) {
	q := requestOf(r)
	if q.Browser {
		s.answerText(w, r, q.Use, f, plainWord(f.Reason))
		return
	}
	s.answer(w, q.Use, f)
}

// Counts holds how many of each reason today for requests with no known system, in memory:
// they have no calling system, so no usage record (Annex A) can hold them. The status screen
// shows these beside the records (R-CAL-04, R-ADM-02). A restart starts them again.
type Counts struct {
	mu  sync.Mutex
	now func() time.Time
	day string
	by  map[reasons.Reason]int
}

// NewCounts starts counting.
func NewCounts(now func() time.Time) *Counts {
	return &Counts{now: now, by: map[reasons.Reason]int{}}
}

var colombo = func() *time.Location {
	l, err := time.LoadLocation("Asia/Colombo")
	if err != nil {
		return time.FixedZone("Asia/Colombo", 5*3600+1800)
	}
	return l
}()

// roll starts a new count when the day in Sri Lanka has changed. Called under mu.
func (c *Counts) roll() {
	if d := c.now().In(colombo).Format(time.DateOnly); d != c.day {
		c.day, c.by = d, map[reasons.Reason]int{}
	}
}

// Add counts one.
func (c *Counts) Add(r reasons.Reason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roll()
	c.by[r]++
}

// Today is a copy of today's counts.
func (c *Counts) Today() map[reasons.Reason]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roll()
	out := make(map[reasons.Reason]int, len(c.by))
	for k, v := range c.by {
		out[k] = v
	}
	return out
}
