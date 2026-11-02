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
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/tva"
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
	// Grammar answers grammar checks. Nil answers "no provider".
	Grammar GrammarChecker
	Logger  *slog.Logger
	Now     func() time.Time // time.Now when nil
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
	s := &Server{deps: deps, counts: NewCounts(deps.Now), limiter: &limits.Limiter{Now: deps.Now}, routes: http.NewServeMux(), checks: http.NewServeMux()}
	s.verifier = apisign.Verifier{Nonces: &apisign.Nonces{}, Now: deps.Now}
	s.routes.HandleFunc("GET /api/v1/gate", s.gate)
	s.routes.HandleFunc("POST /api/v1/translate", s.translate)
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
	mux.Handle(GrammarPrefix, s.withKey(s.checks))
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
}

func requestOf(r *http.Request) *Request { return r.Context().Value(requestKey{}).(*Request) }

// NewRef is a request reference, in the form BUMBLEBEE takes as a request_id. It says
// nothing of the request.
func NewRef() string { return "r-" + oidc.Random() }

// withKey checks the key, then the signature if this key has a public key; a key with
// none works only from this server. A failure is "no key" with 401 (R-CAL-04), counted,
// and kept as a denial.
func (s *Server) withKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ref := NewRef()
		key, ok := bearer(r)
		if !ok {
			s.refuseKey(w, r, ref, "", "No key was sent")
			return
		}
		h, err := s.deps.Keys.Find(ctx, callers.HashKey(key))
		switch {
		case errors.Is(err, callers.ErrNoSuchKey):
			s.refuseKey(w, r, ref, "", "Not a key ROSETTA knows")
			return
		case err != nil:
			s.deps.Logger.Error("a key could not be checked", "request_ref", ref)
			s.answer(w, ref, reasons.New(reasons.NoProvider))
			return
		case h.TurnedOff:
			s.refuseKey(w, r, ref, h.Caller.Reference, "The key is turned off")
			return
		case !tva.MayCall(h.Caller.TVAStatus):
			s.refuseKey(w, r, ref, h.Caller.Reference, "The system is neither live nor may join")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxBody+1))
		if err != nil {
			s.answer(w, ref, reasons.New(reasons.Invalid))
			return
		}
		if len(body) > MaxBody {
			s.answer(w, ref, reasons.New(reasons.TooLong))
			return
		}
		switch {
		case h.PublicKey != "":
			if err := s.checkSignature(r, body, h); err != nil {
				s.deps.Logger.Warn("a signature was refused", "request_ref", ref, "system", h.Caller.Reference, "why", err.Error())
				s.refuseKey(w, r, ref, h.Caller.Reference, "The signature did not pass")
				return
			}
		case throughFrontDoor(r):
			s.deps.Logger.Warn("an unsigned key came from outside", "request_ref", ref, "system", h.Caller.Reference)
			s.refuseKey(w, r, ref, h.Caller.Reference, "A key with no public key came from outside")
			return
		}
		if err := s.deps.Keys.Used(ctx, h); err != nil {
			s.deps.Logger.Error("a key's use was not noted", "request_ref", ref)
		}
		// Over a limit is no denial (E-REC-02's note): the key was good. Limits are read
		// with the key, so a change is in force at once.
		release, wait, allowed := s.limiter.Acquire(h.Caller.ID, h.Caller.PerMinute, h.Caller.AtOnce)
		if !allowed {
			s.answer(w, ref, reasons.New(reasons.OverLimit).Until(s.deps.Now().Add(wait)))
			return
		}
		defer release()
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, requestKey{}, &Request{Ref: ref, Caller: h.Caller, Body: body})))
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
func (s *Server) refuseKey(w http.ResponseWriter, r *http.Request, ref, system, why string) {
	s.answer(w, ref, reasons.New(reasons.NoKey))
	if s.deps.Denied == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), denialTimeout)
	defer cancel()
	if err := s.deps.Denied(ctx, Denial{System: system, Why: why, Visitor: visitor(r)}); err != nil {
		s.deps.Logger.Error("a refused key was not kept as a denial", "request_ref", ref)
	}
}

// answer writes a failure and counts it.
func (s *Server) answer(w http.ResponseWriter, ref string, f *reasons.Failure) {
	s.counts.Add(f.Reason)
	reasons.Write(w, ref, f, s.deps.Now())
}

// refuse answers a request that passed the key check.
func (s *Server) refuse(w http.ResponseWriter, r *http.Request, f *reasons.Failure) {
	s.answer(w, requestOf(r).Ref, f)
}

// Counts holds how many of each reason today, in memory. A request with no known key
// has no calling system, so no usage record (Annex A) can hold it; E-ADM-04 shows these.
type Counts struct {
	mu         sync.Mutex
	now        func() time.Time
	day        string
	by         map[reasons.Reason]int
	mismatches map[string]int // by system (R-MSK-04), until E-REC-02 keeps them in usage records
}

// NewCounts starts counting.
func NewCounts(now func() time.Time) *Counts {
	return &Counts{now: now, by: map[reasons.Reason]int{}, mismatches: map[string]int{}}
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
		c.day, c.by, c.mismatches = d, map[reasons.Reason]int{}, map[string]int{}
	}
}

// Add counts one.
func (c *Counts) Add(r reasons.Reason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roll()
	c.by[r]++
}

// AddMismatch counts one request of ordinary text holding an identity card number.
func (c *Counts) AddMismatch(system string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roll()
	c.mismatches[system]++
}

// Mismatches is a copy of today's mismatches, by system.
func (c *Counts) Mismatches() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roll()
	out := make(map[string]int, len(c.mismatches))
	for k, v := range c.mismatches {
		out[k] = v
	}
	return out
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
