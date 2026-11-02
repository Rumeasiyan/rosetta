package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rumeasiyan/paradox/sign"
	"github.com/Rumeasiyan/rosetta/internal/apisign"
	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
)

const marker = "CALLERTEXTMARKER"

var t0 = time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)

// fakeKeys is a few keys, as the database would hold them.
type fakeKeys struct {
	mu    sync.Mutex
	byKey map[string]callers.Holder
	err   error
	used  int
}

func (f *fakeKeys) Find(_ context.Context, hash string) (callers.Holder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return callers.Holder{}, f.err
	}
	h, ok := f.byKey[hash]
	if !ok {
		return h, callers.ErrNoSuchKey
	}
	return h, nil
}

func (f *fakeKeys) Used(context.Context, callers.Holder) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.used++
	return nil
}

func (f *fakeKeys) add(key string, h callers.Holder) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byKey == nil {
		f.byKey = map[string]callers.Holder{}
	}
	f.byKey[callers.HashKey(key)] = h
}

type bench struct {
	t       *testing.T
	keys    *fakeKeys
	srv     *Server
	logs    *bytes.Buffer
	denials []Denial
	handler http.Handler
	now     time.Time
	public  ed25519.PublicKey
	private ed25519.PrivateKey
}

func newBench(t *testing.T) *bench {
	t.Helper()
	pubText, privText, err := sign.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := sign.ParsePublicKey(pubText)
	priv, _ := sign.ParsePrivateKey(privText)
	b := &bench{t: t, keys: &fakeKeys{}, logs: &bytes.Buffer{}, now: t0, public: pub, private: priv}
	system := func(ref, status string, atOnce int) callers.Caller {
		return callers.Caller{ID: 1, Reference: ref, Name: ref, TVAStatus: status, PerMinute: 60, AtOnce: atOnce}
	}
	b.keys.add("rst_here", callers.Holder{KeyID: 1, Prefix: "rst_here", Caller: system("TEST-1", "live", 5)})
	b.keys.add("rst_signs", callers.Holder{KeyID: 2, Prefix: "rst_signs", PublicKey: pubText, Caller: system("TEST-2", "may_join", 5)})
	b.keys.add("rst_off", callers.Holder{KeyID: 3, Prefix: "rst_off", TurnedOff: true, Caller: system("TEST-3", "live", 5)})
	b.keys.add("rst_retired", callers.Holder{KeyID: 4, Prefix: "rst_retired", Caller: system("TEST-4", "retired", 5)})
	b.srv = New(Deps{
		Keys:   b.keys,
		Logger: slog.New(slog.NewJSONHandler(b.logs, nil)),
		Now:    func() time.Time { return b.now },
		Denied: func(_ context.Context, d Denial) error { b.denials = append(b.denials, d); return nil },
	})
	b.srv.routes.HandleFunc("POST /api/v1/echo", func(w http.ResponseWriter, r *http.Request) {
		q := requestOf(r)
		w.Write([]byte(q.Caller.Reference + " " + string(q.Body)))
	})
	mux := http.NewServeMux()
	b.srv.Mount(mux)
	b.handler = mux
	return b
}

func (b *bench) send(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, req)
	return rec
}

func request(target, key, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	return r
}

func (b *bench) signed(target, key, body, nonce string, at time.Time) *http.Request {
	r := request(target, key, body)
	apisign.Sign(r, []byte(body), key, nonce, b.private, at)
	return r
}

func reasonOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Ref   string `json:"request_ref"`
		Error struct {
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %v", rec.Body, err)
	}
	if out.Ref == "" {
		t.Fatalf("no request reference: %s", rec.Body)
	}
	return out.Error.Reason
}

// A caller on this server has no public key and sends no signature.
func TestACallerOnThisServerNeedsOnlyItsKey(t *testing.T) {
	b := newBench(t)
	rec := b.send(request("/api/v1/echo", "rst_here", "hello"))
	if rec.Code != 200 || rec.Body.String() != "TEST-1 hello" || b.keys.used != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// With a query too: only a signed call may not have one.
	if rec := b.send(request("/api/v1/echo?x=1", "rst_here", "hello")); rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
}

func TestRefusedKeysAreNoKeyAndDenied(t *testing.T) {
	for name, c := range map[string]struct {
		req    func(b *bench) *http.Request
		system string
		why    string
	}{
		"none": {func(*bench) *http.Request { return request("/api/v1/echo", "", marker) }, "", "No key was sent"},
		"not bearer": {func(*bench) *http.Request {
			r := request("/api/v1/echo", "", marker)
			r.Header.Set("Authorization", "Basic rst_here")
			return r
		}, "", "No key was sent"},
		"unknown":  {func(*bench) *http.Request { return request("/api/v1/echo", marker, marker) }, "", "Not a key ROSETTA knows"},
		"off":      {func(*bench) *http.Request { return request("/api/v1/echo", "rst_off", marker) }, "TEST-3", "The key is turned off"},
		"retired":  {func(*bench) *http.Request { return request("/api/v1/echo", "rst_retired", marker) }, "TEST-4", "The system is neither live nor may join"},
		"unsigned": {func(*bench) *http.Request { return request("/api/v1/echo", "rst_signs", marker) }, "TEST-2", "The signature did not pass"},
		"bad query": {func(b *bench) *http.Request {
			return b.signed("/api/v1/echo?x=1", "rst_signs", marker, "a-nonce-of-sixteen-or-more", t0)
		}, "TEST-2", "The signature did not pass"},
		"old": {func(b *bench) *http.Request {
			return b.signed("/api/v1/echo", "rst_signs", marker, "a-nonce-of-sixteen-or-more", t0.Add(-6*time.Minute))
		}, "TEST-2", "The signature did not pass"},
	} {
		b := newBench(t)
		rec := b.send(c.req(b))
		if rec.Code != 401 || reasonOf(t, rec) != "no key" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
		if b.keys.used != 0 {
			t.Errorf("%s: counted as used", name)
		}
		if len(b.denials) != 1 || b.denials[0].System != c.system || b.denials[0].Why != c.why {
			t.Errorf("%s: %+v", name, b.denials)
		}
		if got := b.srv.Counts().Today()[reasons.NoKey]; got != 1 {
			t.Errorf("%s: counted %d", name, got)
		}
		for _, place := range []string{b.logs.String(), rec.Body.String()} {
			if strings.Contains(place, marker) || strings.Contains(place, "rst_") {
				t.Errorf("%s: %s", name, place)
			}
		}
	}
}

func TestASignedRequest(t *testing.T) {
	b := newBench(t)
	const nonce = "a-nonce-of-sixteen-or-more"
	rec := b.send(b.signed("/api/v1/echo", "rst_signs", "hello", nonce, t0))
	if rec.Code != 200 || rec.Body.String() != "TEST-2 hello" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// The same request again is refused: the nonce has gone.
	again := b.send(b.signed("/api/v1/echo", "rst_signs", "hello", nonce, t0))
	if again.Code != 401 || reasonOf(t, again) != "no key" {
		t.Fatalf("%d %s", again.Code, again.Body)
	}
	if !strings.Contains(b.logs.String(), "nonce seen") {
		t.Fatal(b.logs.String())
	}
	// A body changed on the way is refused.
	r := b.signed("/api/v1/echo", "rst_signs", "hello", nonce+"-2", t0)
	r.Body = io.NopCloser(strings.NewReader("changed"))
	if rec := b.send(r); rec.Code != 401 {
		t.Fatalf("%d", rec.Code)
	}
}

// With no public key a signature is simply not asked for; with one it always is, so
// a key alone from another machine is no use.
func TestAKeyHoldingAPublicKeyMustSign(t *testing.T) {
	b := newBench(t)
	if rec := b.send(request("/api/v1/echo", "rst_signs", "hello")); rec.Code != 401 {
		t.Fatalf("%d", rec.Code)
	}
	b.keys.add("rst_badpub", callers.Holder{KeyID: 9, Prefix: "rst_badpub", PublicKey: "not a key", Caller: callers.Caller{Reference: "TEST-9", TVAStatus: "live"}})
	if rec := b.send(b.signed("/api/v1/echo", "rst_badpub", "hello", "a-nonce-of-sixteen-or-more", t0)); rec.Code != 401 {
		t.Fatalf("a stored key that cannot be read passes nothing: %d", rec.Code)
	}
}

// ROSETTA losing its database is not the caller's key at fault.
func TestAKeyThatCannotBeCheckedIsNotNoKey(t *testing.T) {
	b := newBench(t)
	b.keys.err = errors.New("connection to the database lost")
	rec := b.send(request("/api/v1/echo", "rst_here", marker))
	if rec.Code != 503 || reasonOf(t, rec) != "no provider" || len(b.denials) != 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if strings.Contains(b.logs.String(), marker) || strings.Contains(rec.Body.String(), marker) {
		t.Fatal("text escaped")
	}
}

func TestALongBodyIsTooLong(t *testing.T) {
	b := newBench(t)
	rec := b.send(request("/api/v1/echo", "rst_here", strings.Repeat(marker, MaxBody/len(marker)+1)))
	if rec.Code != 413 || reasonOf(t, rec) != "too long" || strings.Contains(rec.Body.String(), marker) {
		t.Fatalf("%d", rec.Code)
	}
}

func TestAnUnknownPathIsInvalid(t *testing.T) {
	b := newBench(t)
	rec := b.send(request("/api/v1/nothing", "rst_here", marker))
	if rec.Code != 400 || reasonOf(t, rec) != "invalid request" || strings.Contains(rec.Body.String(), marker) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := b.send(request("/api/v1/nothing", "", marker)); rec.Code != 401 {
		t.Fatalf("no key comes first: %d", rec.Code)
	}
}

func TestCountsRollOverAtMidnightInSriLanka(t *testing.T) {
	now := time.Date(2030, 1, 1, 18, 29, 0, 0, time.UTC) // 23:59 in Sri Lanka
	c := NewCounts(func() time.Time { return now })
	c.Add(reasons.NoKey)
	c.Add(reasons.NoKey)
	if c.Today()[reasons.NoKey] != 2 {
		t.Fatal(c.Today())
	}
	now = now.Add(time.Minute)
	if c.Today()[reasons.NoKey] != 0 {
		t.Fatal(c.Today())
	}
}

// The entry PENSIEVE gets for a refused key passes the audit's own checks, says who
// where it can, and holds nothing sent.
func TestDenialEventIsWhatTheAuditTakes(t *testing.T) {
	e := DenialEvent(Denial{Why: "Not a key ROSETTA knows", Visitor: "192.0.2.1"})
	if e.Actor.Subject != unidentified || e.Actor.Post != audit.PostNotKnown || e.Action != audit.ActionDenial || e.Result != audit.ResultDenied || e.ObjectRef != Prefix {
		t.Fatalf("%+v", e)
	}
	e = DenialEvent(Denial{System: "TEST-1", Why: "The key is turned off"})
	if e.Actor.Subject != "TEST-1" || e.Actor.Post != audit.PostSystem {
		t.Fatalf("%+v", e)
	}
}

// ---- limits (R-CAL-07) --------------------------------------------------------

func TestOverLimitSaysWhenAndIsNoDenial(t *testing.T) {
	b := newBench(t)
	b.keys.add("rst_small", callers.Holder{KeyID: 7, Prefix: "rst_small", Caller: callers.Caller{ID: 7, Reference: "TEST-7", TVAStatus: "live", PerMinute: 2, AtOnce: 5}})
	for range 2 {
		if rec := b.send(request("/api/v1/echo", "rst_small", marker)); rec.Code != 200 {
			t.Fatal(rec.Code)
		}
	}
	b.now = t0.Add(15 * time.Second)
	rec := b.send(request("/api/v1/echo", "rst_small", marker))
	if rec.Code != 429 || reasonOf(t, rec) != "over limit" || rec.Header().Get("Retry-After") != "45" || !strings.Contains(rec.Body.String(), "retry_at") {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	if strings.Contains(rec.Body.String(), marker) || len(b.denials) != 0 {
		t.Fatalf("%+v", b.denials)
	}
	if b.srv.Counts().Today()[reasons.OverLimit] != 1 {
		t.Fatal(b.srv.Counts().Today())
	}
	b.now = t0.Add(time.Minute)
	if rec := b.send(request("/api/v1/echo", "rst_small", marker)); rec.Code != 200 {
		t.Fatalf("after the wait: %d", rec.Code)
	}
}

func TestRequestsAtOnceAreHeldAndReleased(t *testing.T) {
	b := newBench(t)
	b.keys.add("rst_one", callers.Holder{KeyID: 8, Prefix: "rst_one", Caller: callers.Caller{ID: 8, Reference: "TEST-8", TVAStatus: "live", PerMinute: 100, AtOnce: 1}})
	started, finish := make(chan struct{}), make(chan struct{})
	b.srv.routes.HandleFunc("POST /api/v1/hold", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-finish
	})
	done := make(chan int)
	go func() { done <- b.send(request("/api/v1/hold", "rst_one", "")).Code }()
	<-started
	rec := b.send(request("/api/v1/echo", "rst_one", ""))
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("%d", rec.Code)
	}
	close(finish)
	if code := <-done; code != 200 {
		t.Fatal(code)
	}
	if rec := b.send(request("/api/v1/echo", "rst_one", "")); rec.Code != 200 {
		t.Fatalf("after the first ended: %d", rec.Code)
	}
}

// Refused keys never touch a system's limits.
func TestARefusedKeyUsesNoLimit(t *testing.T) {
	b := newBench(t)
	b.keys.add("rst_tiny", callers.Holder{KeyID: 6, Prefix: "rst_tiny", TurnedOff: true, Caller: callers.Caller{ID: 6, Reference: "TEST-6", TVAStatus: "live", PerMinute: 1, AtOnce: 1}})
	for range 3 {
		if rec := b.send(request("/api/v1/echo", "rst_tiny", "")); rec.Code != 401 {
			t.Fatal(rec.Code)
		}
	}
}
