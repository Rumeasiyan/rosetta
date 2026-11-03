package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/standin"
)

const marker = "CALLERTEXTMARKER"

// script answers each source's calls in turn; the last answer repeats.
type script struct {
	answers map[string][]func(bumblebee.Request) (bumblebee.Answer, error)
	calls   []bumblebee.Request
}

func (s *script) Translate(_ context.Context, q bumblebee.Request) (bumblebee.Answer, error) {
	s.calls = append(s.calls, q)
	list := s.answers[q.Source]
	if len(list) == 0 {
		return bumblebee.Answer{Text: "[" + q.Source + "] " + q.Text, Source: q.Source, Version: "1"}, nil
	}
	next := list[0]
	if len(list) > 1 {
		s.answers[q.Source] = list[1:]
	}
	return next(q)
}

func (s *script) called() []string {
	var out []string
	for _, c := range s.calls {
		out = append(out, c.Source)
	}
	return out
}

func fails(reason string, at *time.Time) func(bumblebee.Request) (bumblebee.Answer, error) {
	return func(bumblebee.Request) (bumblebee.Answer, error) {
		return bumblebee.Answer{}, &bumblebee.Failure{Reason: reason, RetryAt: at}
	}
}

// bench is a list of sources a, b and c, every gate open, with a fixed clock.
type fbBench struct {
	now      time.Time
	provider *script
	f        *Fallback
	slept    []time.Duration
	logs     *bytes.Buffer
	list     bumblebee.List
}

func newFB(t *testing.T) *fbBench {
	t.Helper()
	b := &fbBench{now: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), provider: &script{answers: map[string][]func(bumblebee.Request) (bumblebee.Answer, error){}}, logs: &bytes.Buffer{}}
	b.list = bumblebee.List{MaxTextChars: 1000, Sources: []bumblebee.Source{offering("a", enTa), offering("b", enTa), offering("c", enTa)}}
	now := func() time.Time { return b.now }
	book := &Book{Now: now, MaxAge: age(time.Minute), Read: func(context.Context) (bumblebee.List, error) { return b.list, nil }}
	if err := book.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	gates := map[GateKey]Gate{}
	for _, s := range b.list.Sources {
		gates[GateKey{s.Name, "en", "ta"}] = openGate("1")
	}
	logger := slog.New(slog.NewJSONHandler(b.logs, nil))
	b.f = &Fallback{Book: book, Provider: b.provider, Now: now, Logger: logger,
		Policy: func(context.Context) Policy { return Policy{Gates: gates} },
		Sleep: func(_ context.Context, d time.Duration) error {
			b.slept = append(b.slept, d)
			b.now = b.now.Add(d)
			return nil
		}}
	return b
}

func (b *fbBench) send(ctx context.Context) (Result, error) {
	return b.f.Translate(ctx, Piece{Ref: "r-1", From: "en", To: "ta", Sensitivity: Ordinary, Text: marker})
}

func TestTheFirstSourceAnswers(t *testing.T) {
	b := newFB(t)
	r, err := b.send(context.Background())
	if err != nil || r.Source != "a" || r.Version != "1" || r.Fallback || r.Text != "[a] "+marker {
		t.Fatalf("%+v %v", r, err)
	}
	if got := b.provider.calls[0]; got.RequestID != "r-1" || got.From != "en" || got.To != "ta" {
		t.Fatalf("%+v", got)
	}
}

// Every reason but too-long goes to the next source, and the answer says fallback.
func TestEachFailureGoesToTheNext(t *testing.T) {
	for _, reason := range []string{"blocked", "changed", "timeout", "unsupported", "source-off", "unavailable", "busy"} {
		b := newFB(t)
		b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails(reason, nil)}
		r, err := b.send(context.Background())
		if err != nil || r.Source != "b" || !r.Fallback {
			t.Errorf("%s: %+v %v", reason, r, err)
		}
		if !slices.Equal(b.provider.called(), []string{"a", "b"}) {
			t.Errorf("%s: %v", reason, b.provider.called())
		}
		if strings.Contains(b.logs.String(), marker) || !strings.Contains(b.logs.String(), reason) {
			t.Errorf("%s: %s", reason, b.logs)
		}
	}
}

// R-PRV-06: all failed, no provider, and the earliest time one is expected back.
func TestEverySourceFailing(t *testing.T) {
	b := newFB(t)
	soon, later := b.now.Add(time.Minute), b.now.Add(time.Hour)
	b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails("blocked", &later)}
	b.provider.answers["b"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails("changed", &soon)}
	b.provider.answers["c"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails("timeout", nil)}
	_, err := b.send(context.Background())
	var e *Exhausted
	if !errors.As(err, &e) || !e.RetryAt.Equal(soon) || strings.Contains(err.Error(), marker) {
		t.Fatalf("%v", err)
	}
}

// A blocked source is not called before retry_at, by this request or any other
// (R-PRV-05), and is called again after it.
func TestABlockedSourceIsLeftAloneUntilRetryAt(t *testing.T) {
	b := newFB(t)
	at := b.now.Add(30 * time.Second)
	b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails("blocked", &at), func(q bumblebee.Request) (bumblebee.Answer, error) {
		return bumblebee.Answer{Text: "back", Source: "a", Version: "1"}, nil
	}}
	if r, err := b.send(context.Background()); err != nil || r.Source != "b" {
		t.Fatal(r, err)
	}
	b.provider.calls = nil
	b.now = at.Add(-time.Second)
	if r, err := b.send(context.Background()); err != nil || r.Source != "b" || !r.Fallback {
		t.Fatal(r, err)
	}
	if !slices.Equal(b.provider.called(), []string{"b"}) {
		t.Fatalf("called while held: %v", b.provider.called())
	}
	b.now = at
	if r, err := b.send(context.Background()); err != nil || r.Source != "a" || r.Fallback {
		t.Fatal(r, err)
	}
}

// Resting in the list counts the same, and all resting means no provider with the time.
func TestARestingSourceIsSkipped(t *testing.T) {
	b := newFB(t)
	rest := b.now.Add(time.Hour)
	for i := range b.list.Sources {
		b.list.Sources[i].State, b.list.Sources[i].RestUntil = "resting-block", &rest
	}
	b.f.Book.Refresh(context.Background())
	_, err := b.send(context.Background())
	var e *Exhausted
	if !errors.As(err, &e) || !e.RetryAt.Equal(rest) || len(b.provider.calls) != 0 {
		t.Fatalf("%v %v", err, b.provider.called())
	}
	// A source off gives no time, and is not called either.
	for i := range b.list.Sources {
		b.list.Sources[i].State, b.list.Sources[i].RestUntil = "off", nil
	}
	b.f.Book.Refresh(context.Background())
	if _, err = b.send(context.Background()); !errors.As(err, &e) || !e.RetryAt.IsZero() || len(b.provider.calls) != 0 {
		t.Fatal(err)
	}
}

// R-PRV-03: on busy with a wait shorter than the time left, wait, and try the same
// source once more first.
func TestBusyWaitsAndTriesOnceMore(t *testing.T) {
	b := newFB(t)
	at := b.now.Add(5 * time.Second)
	b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails("busy", &at), func(q bumblebee.Request) (bumblebee.Answer, error) {
		return bumblebee.Answer{Text: "after a wait", Source: "a", Version: "1"}, nil
	}}
	r, err := b.send(context.Background())
	if err != nil || r.Source != "a" || r.Fallback || !slices.Equal(b.slept, []time.Duration{5 * time.Second}) {
		t.Fatalf("%+v %v %v", r, err, b.slept)
	}
	if !slices.Equal(b.provider.called(), []string{"a", "a"}) {
		t.Fatal(b.provider.called())
	}
}

func TestBusyAgainMovesOnAndLeavesItAlone(t *testing.T) {
	b := newFB(t)
	first, second := b.now.Add(5*time.Second), b.now.Add(30*time.Second)
	b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails("busy", &first), fails("busy", &second)}
	r, err := b.send(context.Background())
	if err != nil || r.Source != "b" || !r.Fallback || !slices.Equal(b.provider.called(), []string{"a", "a", "b"}) {
		t.Fatalf("%+v %v %v", r, err, b.provider.called())
	}
	b.provider.calls = nil
	if r, _ := b.send(context.Background()); r.Source != "b" || slices.Contains(b.provider.called(), "a") {
		t.Fatal("called before its retry_at")
	}
}

// A wait longer than the time left is not waited: next source at once.
func TestBusyWithTooLongAWaitMovesOn(t *testing.T) {
	b := newFB(t)
	at := b.now.Add(time.Hour)
	b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails("busy", &at)}
	// The fixed clock and the real deadline must agree on the time left.
	b.f.Now = time.Now
	at = time.Now().Add(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, err := b.send(ctx)
	if err != nil || r.Source != "b" || len(b.slept) != 0 {
		t.Fatalf("%+v %v %v", r, err, b.slept)
	}
}

// too-long reads the list again and asks for a split before it counts as a failure.
func TestTooLongReadsTheListAndSplitsAgain(t *testing.T) {
	b := newFB(t)
	b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails("too-long", nil)}
	b.list.MaxTextChars = 5
	_, err := b.send(context.Background())
	var tl *TooLong
	if !errors.As(err, &tl) || tl.Limit != 5 || !slices.Equal(b.provider.called(), []string{"a"}) {
		t.Fatalf("%v %v", err, b.provider.called())
	}
	// With the list unchanged and the text inside it, it is a failure: next source.
	b = newFB(t)
	b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){fails("too-long", nil)}
	if r, err := b.send(context.Background()); err != nil || r.Source != "b" || !r.Fallback {
		t.Fatal(r, err)
	}
}

// The request's limit holds: no call starts with too little left, and running out
// inside a call is the request timing out rather than another source's turn.
func TestTheWholeRequestStaysInsideItsTime(t *testing.T) {
	b := newFB(t)
	b.f.Now = time.Now
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := b.send(ctx); !errors.Is(err, ErrTooLate) || len(b.provider.calls) != 0 {
		t.Fatalf("%v %v", err, b.provider.called())
	}
	b = newFB(t)
	ctx, cancel = context.WithCancel(context.Background())
	b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){func(bumblebee.Request) (bumblebee.Answer, error) {
		cancel()
		return bumblebee.Answer{}, context.Canceled
	}}
	if _, err := b.send(ctx); !errors.Is(err, context.Canceled) || !slices.Equal(b.provider.called(), []string{"a"}) {
		t.Fatalf("%v %v", err, b.provider.called())
	}
}

// A list older than its limit sends nothing, at once (R-NFR-09).
func TestAStaleListSendsNothing(t *testing.T) {
	b := newFB(t)
	b.now = b.now.Add(time.Minute)
	_, err := b.send(context.Background())
	var e *Exhausted
	if !errors.As(err, &e) || len(b.provider.calls) != 0 {
		t.Fatal(err)
	}
}

// R-TRN-09: the text sent back, or nothing, is a source failing, and the next is tried.
func TestAnEchoIsAFailure(t *testing.T) {
	for name, answer := range map[string]string{"the same": marker, "the same with spacing": " " + marker + "\n", "empty": "", "only spacing": " \n"} {
		b := newFB(t)
		b.provider.answers["a"] = []func(bumblebee.Request) (bumblebee.Answer, error){func(bumblebee.Request) (bumblebee.Answer, error) {
			return bumblebee.Answer{Text: answer, Source: "a", Version: "1"}, nil
		}}
		if r, err := b.send(context.Background()); err != nil || r.Source != "b" || !r.Fallback {
			t.Errorf("%s: %+v %v", name, r, err)
		}
	}
	if !acceptable("⟦M1⟧", "⟦M1⟧") || !acceptable("12.", "12.") {
		t.Fatal("a piece with no letters comes back the same in any language")
	}
	if acceptable("Call ⟦M1⟧", "Call ⟦M1⟧") {
		t.Fatal("an echo with a stand-in in it is still an echo")
	}
	if !acceptable("வணக்கம்", "Hello") {
		t.Fatal("a translation")
	}
	// The same text in another normal form is still the same text.
	if acceptable("\u0D9A\u0DDC", "\u0D9A\u0DD9\u0DCF") {
		t.Fatal("normalised")
	}
}

// The order, the switch and the gate all reach the choice.
func TestTheChoiceFollowsThePolicy(t *testing.T) {
	b := newFB(t)
	d := Translate("en", "ta")
	base := b.f.Policy(context.Background())
	base.Orders = map[Direction][]string{d: {"c", "b", "a"}}
	base.Off = map[string]bool{"c": true}
	b.f.Policy = func(context.Context) Policy { return base }
	if r, err := b.send(context.Background()); err != nil || r.Source != "b" {
		t.Fatal(r, err)
	}
	got, read := b.f.Allowed(context.Background(), "en", "ta", Ordinary)
	if !read || !slices.Equal(names(got), []string{"b", "a"}) {
		t.Fatal(names(got), read)
	}
	if got, _ := b.f.Allowed(context.Background(), "en", "ta", Confidential); len(got) != 0 {
		t.Fatal(names(got))
	}
}

// ---- against BUMBLEBEE's stand-in, over HTTP ---------------------------------

func TestFallbackAgainstTheStandIn(t *testing.T) {
	s := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { s.Close(); ts.Close() })
	mode := func(m string, params map[string]string) {
		raw, _ := json.Marshal(map[string]any{"mode": m, "params": params})
		res, err := http.Post(ts.URL+"/control/bumblebee/mode", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	client := bumblebee.Client{URL: ts.URL + "/bumblebee", Key: standin.BumblebeeKey}
	book := &Book{MaxAge: age(time.Minute), Read: client.Sources}
	var logs bytes.Buffer
	gates := map[GateKey]Gate{{"standin-translate", "en", "ta"}: openGate("1"), {"standin-translate-b", "en", "ta"}: openGate("1")}
	newFallback := func() *Fallback {
		return &Fallback{Book: book, Provider: client, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
			Policy: func(context.Context) Policy { return Policy{Gates: gates} },
			Sleep:  func(context.Context, time.Duration) error { return nil }}
	}
	ctx := context.Background()
	send := func(f *Fallback) (Result, error) {
		book.Refresh(ctx)
		return f.Translate(ctx, Piece{Ref: "r-standin", From: "en", To: "ta", Sensitivity: Ordinary, Text: "An invented letter " + marker})
	}
	if r, err := send(newFallback()); err != nil || r.Source != "standin-translate" || r.Fallback || !strings.HasPrefix(r.Text, "[ta] ") {
		t.Fatal(r, err)
	}
	// Each failure on the first source alone lands on the second.
	for _, reason := range []string{"busy", "timeout", "unsupported", "unavailable"} {
		mode(reason, map[string]string{"source": "standin-translate"})
		if r, err := send(newFallback()); err != nil || r.Source != "standin-translate-b" || !r.Fallback {
			t.Errorf("%s: %+v %v", reason, r, err)
		}
	}
	// Listed as resting or off, it is not called at all.
	for _, reason := range []string{"blocked", "changed", "source-off"} {
		mode(reason, map[string]string{"source": "standin-translate"})
		if r, err := send(newFallback()); err != nil || r.Source != "standin-translate-b" || !r.Fallback {
			t.Errorf("%s: %+v %v", reason, r, err)
		}
	}
	// Every source failing is no provider; when they say when, it says when.
	mode("busy", map[string]string{"retry_seconds": "3600"})
	var e *Exhausted
	if _, err := send(newFallback()); !errors.As(err, &e) || e.RetryAt.IsZero() {
		t.Fatalf("%v", err)
	}
	mode("timeout", nil)
	if _, err := send(newFallback()); !errors.As(err, &e) || !e.RetryAt.IsZero() {
		t.Fatalf("%v", err)
	}
	// The size in the list shrinking comes back as a split.
	mode("normal", map[string]string{"max_text_chars": "10"})
	f := newFallback()
	var tl *TooLong
	if _, err := send(f); !errors.As(err, &tl) || tl.Limit != 10 {
		t.Fatalf("%v", err)
	}
	if strings.Contains(logs.String(), marker) {
		t.Fatal("a log holds the text")
	}
}
