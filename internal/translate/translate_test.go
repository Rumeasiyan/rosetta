package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/standin"
)

const marker = "CALLERTEXTMARKER"

// fakeSources marks what it is sent, so a test can tell it from the original.
type fakeSources struct {
	allowed []bumblebee.Source
	read    bool
	limit   int
	sent    []sources.Piece
	answer  func(p sources.Piece) (sources.Result, error)
}

func (f *fakeSources) Allowed(context.Context, string, string, string) ([]bumblebee.Source, bool) {
	return f.allowed, f.read
}

func (f *fakeSources) MaxText() int { return f.limit }

func (f *fakeSources) Translate(_ context.Context, p sources.Piece) (sources.Result, error) {
	f.sent = append(f.sent, p)
	if f.answer != nil {
		return f.answer(p)
	}
	return sources.Result{Text: "[" + p.To + "] " + p.Text, Source: "a", Version: "1"}, nil
}

func newService(f *fakeSources) (*Service, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	if f.limit == 0 {
		f.limit = 1000
	}
	if f.allowed == nil {
		f.allowed, f.read = []bumblebee.Source{{Name: "a"}}, true
	}
	return &Service{Sources: f, Logger: slog.New(slog.NewJSONHandler(logs, nil)),
		Settings: func(context.Context) settings.Values { return settings.Defaults() }}, logs
}

func request(passages ...string) Request {
	return Request{Ref: "r-1", From: "en", To: "ta", Sensitivity: sources.Ordinary, Passages: passages}
}

func TestEveryPassageBackInOrderWithItsSpacing(t *testing.T) {
	f := &fakeSources{}
	s, _ := newService(f)
	a, fail := s.Translate(context.Background(), request("One. Two.", "  Three.\n\n  Four.  \n", "", " \n "))
	if fail != nil {
		t.Fatal(fail)
	}
	want := []string{"[ta] One. [ta] Two.", "  [ta] Three.\n\n  [ta] Four.  \n", "", " \n "}
	for i, p := range a.Passages {
		if p.Text != want[i] {
			t.Errorf("%d: %q", i, p.Text)
		}
	}
	if len(a.Passages) != 4 || len(a.Passages[0].Pieces) != 2 || len(a.Passages[2].Pieces) != 0 || a.Passages[0].Pieces[0].Origin != OriginSource {
		t.Fatalf("%+v", a)
	}
	for _, p := range f.sent {
		if p.Ref != "r-1" || p.From != "en" || p.To != "ta" {
			t.Fatalf("%+v", p)
		}
	}
}

func TestMaskedBeforeItLeavesAndBackInTheAnswer(t *testing.T) {
	f := &fakeSources{}
	s, _ := newService(f)
	a, fail := s.Translate(context.Background(), request("Call 077 123 4567 now. Card 123456789V. Then 0719999999 and 0718888888."))
	if fail != nil {
		t.Fatal(fail)
	}
	if got := a.Passages[0].Text; got != "[ta] Call 077 123 4567 now. [ta] Card 123456789V. [ta] Then 0719999999 and 0718888888." {
		t.Fatal(got)
	}
	for _, p := range f.sent {
		if strings.ContainsAny(p.Text, "0123456789") && !strings.Contains(p.Text, "⟦M") {
			t.Fatalf("a number left: %q", p.Text)
		}
		if strings.Contains(p.Text, "4567") || strings.Contains(p.Text, "123456789V") || strings.Contains(p.Text, "9999") {
			t.Fatalf("a value left: %q", p.Text)
		}
	}
	// Numbered in each piece from one.
	if f.sent[2].Text != "Then ⟦M1⟧ and ⟦M2⟧." {
		t.Fatal(f.sent[2].Text)
	}
}

// A number with spaces at a word break is masked first, so it cannot go in two parts.
func TestAWordBreakNeverSplitsANumber(t *testing.T) {
	f := &fakeSources{limit: 12}
	s, _ := newService(f)
	a, fail := s.Translate(context.Background(), request("Please call 077 123 4567 today"))
	if fail != nil {
		t.Fatal(fail)
	}
	for _, p := range f.sent {
		if strings.Contains(p.Text, "077") || strings.Contains(p.Text, "4567") {
			t.Fatalf("%q", p.Text)
		}
	}
	if !strings.Contains(a.Passages[0].Text, "077 123 4567") {
		t.Fatal(a.Passages[0].Text)
	}
}

func TestTheSamePieceGoesOnceAndKeepsItsOwnValues(t *testing.T) {
	f := &fakeSources{}
	s, _ := newService(f)
	a, fail := s.Translate(context.Background(), request("Call 0771111111 now.", "Call 0772222222 now."))
	if fail != nil {
		t.Fatal(fail)
	}
	if len(f.sent) != 1 {
		t.Fatalf("%d calls", len(f.sent))
	}
	if a.Passages[0].Text != "[ta] Call 0771111111 now." || a.Passages[1].Text != "[ta] Call 0772222222 now." {
		t.Fatalf("%+v", a)
	}
}

// R-TRN-02: one passage failing fails them all, with its position and reason.
func TestAllOrNothing(t *testing.T) {
	retry := time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		err  error
		text string
		want reasons.Reason
	}{
		"no provider":  {&sources.Exhausted{RetryAt: retry}, "", reasons.NoProvider},
		"too late":     {sources.ErrTooLate, "", reasons.TimedOut},
		"out of time":  {context.DeadlineExceeded, "", reasons.TimedOut},
		"given up":     {context.Canceled, "", reasons.TimedOut},
		"another":      {errors.New(marker), "", reasons.NoProvider},
		"masking lost": {nil, "[ta] Call now.", reasons.MaskingLost},
	} {
		f := &fakeSources{answer: func(p sources.Piece) (sources.Result, error) {
			if !strings.Contains(p.Text, "Bad") && !strings.Contains(p.Text, "⟦") {
				return sources.Result{Text: "[ok] " + p.Text, Source: "a"}, nil
			}
			return sources.Result{Text: c.text, Source: "a"}, c.err
		}}
		s, logs := newService(f)
		second := "Bad " + marker
		if c.err == nil {
			second = "Call 0771234567 " + marker
		}
		a, fail := s.Translate(context.Background(), request("Fine "+marker, second, "Also fine"))
		if fail == nil || fail.Reason != c.want || fail.Position == nil || *fail.Position != 1 || a.Passages != nil {
			t.Errorf("%s: %+v %+v", name, fail, a)
			continue
		}
		if name == "no provider" && (fail.RetryAt == nil || !fail.RetryAt.Equal(retry)) {
			t.Errorf("%s: %+v", name, fail)
		}
		if strings.Contains(logs.String(), marker) || strings.Contains(logs.String(), "0771234567") || strings.Contains(fail.Error(), marker) {
			t.Errorf("%s: %s", name, logs)
		}
	}
}

// R-TRN-03 and R-TRN-01: what is refused before anything is done.
func TestWhatIsRefused(t *testing.T) {
	v := settings.Values{settings.PassagesMax.Name: 2, settings.PassageCharsMax.Name: 5, settings.RequestCharsMax.Name: 8}
	for name, c := range map[string]struct {
		q        Request
		want     reasons.Reason
		position int
	}{
		"no from":        {Request{To: "ta", Sensitivity: "ordinary", Passages: []string{"a"}}, reasons.Invalid, -1},
		"other language": {Request{From: "fr", To: "ta", Sensitivity: "ordinary", Passages: []string{"a"}}, reasons.Invalid, -1},
		"upper case":     {Request{From: "EN", To: "ta", Sensitivity: "ordinary", Passages: []string{"a"}}, reasons.Invalid, -1},
		"same languages": {Request{From: "en", To: "en", Sensitivity: "ordinary", Passages: []string{"a"}}, reasons.Invalid, -1},
		"no sensitivity": {Request{From: "en", To: "ta", Passages: []string{"a"}}, reasons.Invalid, -1},
		"other":          {Request{From: "en", To: "ta", Sensitivity: "secret", Passages: []string{"a"}}, reasons.Invalid, -1},
		"no passages":    {Request{From: "en", To: "ta", Sensitivity: "ordinary"}, reasons.Invalid, -1},
		"too many":       {Request{From: "en", To: "ta", Sensitivity: "ordinary", Passages: []string{"a", "b", "c"}}, reasons.TooLong, -1},
		"one long":       {Request{From: "en", To: "ta", Sensitivity: "ordinary", Passages: []string{"a", "abcdef"}}, reasons.TooLong, 1},
		"long in all":    {Request{From: "en", To: "ta", Sensitivity: "ordinary", Passages: []string{"abcde", "abcde"}}, reasons.TooLong, -1},
	} {
		got := check(c.q, v)
		if got == nil || got.Reason != c.want {
			t.Errorf("%s: %v", name, got)
			continue
		}
		if (got.Position == nil) != (c.position < 0) || (got.Position != nil && *got.Position != c.position) {
			t.Errorf("%s: position %v", name, got.Position)
		}
	}
	// Counted in code points: five Tamil letters and an emoji each fit.
	ok := Request{From: "en", To: "ta", Sensitivity: "ordinary", Passages: []string{"அஆஇஈஉ", "😀😀😀"}}
	if f := check(ok, v); f != nil {
		t.Fatal(f)
	}
}

// R-GAT-03: nothing goes out, and the terms found go back to help the translator.
func TestAClosedDirectionNeedsAPerson(t *testing.T) {
	f := &fakeSources{allowed: []bumblebee.Source{}, read: true}
	s, _ := newService(f)
	s.Terms = func(context.Context, string, []string) []string { return []string{"Chief Secretary"} }
	_, fail := s.Translate(context.Background(), request("A piece."))
	if fail == nil || fail.Reason != reasons.HumanRequired || len(f.sent) != 0 || len(fail.Terms) != 1 || *fail.Position != 0 {
		t.Fatalf("%+v %v", fail, f.sent)
	}
	// A list never read says nothing of the gate.
	f = &fakeSources{allowed: []bumblebee.Source{}, read: false, answer: func(sources.Piece) (sources.Result, error) { return sources.Result{}, &sources.Exhausted{} }}
	s, _ = newService(f)
	if _, fail := s.Translate(context.Background(), request("A piece.")); fail == nil || fail.Reason != reasons.NoProvider {
		t.Fatalf("%+v", fail)
	}
}

// Whatever the gate says, a passage with nothing to translate is answered.
func TestAClosedDirectionWithNothingToSend(t *testing.T) {
	f := &fakeSources{allowed: []bumblebee.Source{}, read: true}
	s, _ := newService(f)
	if a, fail := s.Translate(context.Background(), request("", "  ")); fail != nil || a.Passages[1].Text != "  " {
		t.Fatal(fail)
	}
}

// fixedAnswer knows every piece.
type fixedAnswer struct {
	origin string
	asked  *[]string
	err    error
}

func (a fixedAnswer) Answer(_ context.Context, l Lookup) (Hit, bool, error) {
	*a.asked = append(*a.asked, a.origin)
	if a.origin == "" || a.err != nil {
		return Hit{}, false, a.err
	}
	return Hit{Text: a.origin + ": " + l.Text, Origin: a.origin}, true, nil
}

// R-TRN-04: glossary, memory, cache, then a source, and each hook is asked in that order.
// Served under any gate state (R-GAT-02).
func TestTheOrderOfAnswers(t *testing.T) {
	var asked []string
	f := &fakeSources{allowed: []bumblebee.Source{}, read: true}
	s, _ := newService(f)
	s.Glossary = fixedAnswer{"", &asked, nil}
	s.Memory = fixedAnswer{OriginMemory, &asked, nil}
	s.Cache = fixedAnswer{OriginCache, &asked, nil}
	a, fail := s.Translate(context.Background(), request("Call 0771234567 now."))
	if fail != nil {
		t.Fatal(fail)
	}
	if a.Passages[0].Text != "memory: Call 0771234567 now." || a.Passages[0].Pieces[0].Origin != OriginMemory || len(f.sent) != 0 {
		t.Fatalf("%+v", a)
	}
	if strings.Join(asked, ",") != ",memory" {
		t.Fatal(asked)
	}
	// With none knowing it, the source; one that fails counts as not knowing.
	var again []string
	f = &fakeSources{}
	s, logs := newService(f)
	s.Glossary, s.Memory, s.Cache = None{}, fixedAnswer{"", &again, errors.New(marker)}, None{}
	a, fail = s.Translate(context.Background(), request("A piece."))
	if fail != nil || a.Passages[0].Pieces[0].Origin != OriginSource {
		t.Fatalf("%+v %+v", fail, a)
	}
	if strings.Contains(logs.String(), marker) || !strings.Contains(logs.String(), "could not be looked up") {
		t.Fatal(logs)
	}
}

// R-PRV-04: "too long" means the list was read again; the piece goes in parts, joined
// as it was written.
func TestTooLongSplitsAgain(t *testing.T) {
	f := &fakeSources{}
	first := true
	f.answer = func(p sources.Piece) (sources.Result, error) {
		if first {
			first = false
			return sources.Result{}, &sources.TooLong{Limit: 12}
		}
		return sources.Result{Text: "[ta] " + p.Text, Source: "a", Version: "1", Fallback: true}, nil
	}
	s, _ := newService(f)
	a, fail := s.Translate(context.Background(), request("Please call 077 123 4567 today, kind  friends"))
	if fail != nil {
		t.Fatal(fail)
	}
	p := a.Passages[0]
	if !strings.Contains(p.Text, "077 123 4567") || !strings.Contains(p.Text, "kind") || strings.Count(p.Text, "[ta]") < 3 {
		t.Fatalf("%q", p.Text)
	}
	if !strings.Contains(p.Text, "  [ta] friends") {
		t.Fatalf("spacing between parts: %q", p.Text)
	}
	if !p.Pieces[0].Fallback || p.Pieces[0].Origin != OriginSource {
		t.Fatalf("%+v", p.Pieces)
	}
	// Too long again, with the list changing under it, is a failure.
	f = &fakeSources{answer: func(sources.Piece) (sources.Result, error) { return sources.Result{}, &sources.TooLong{Limit: 3} }}
	s, _ = newService(f)
	if _, fail := s.Translate(context.Background(), request("Long enough a piece")); fail == nil || fail.Reason != reasons.TooLong {
		t.Fatalf("%+v", fail)
	}
}

// R-ERR-03: the request's own limit reaches the source call.
func TestTheRequestHasALimit(t *testing.T) {
	f := &fakeSources{}
	var left time.Duration
	f.answer = func(sources.Piece) (sources.Result, error) { return sources.Result{Text: "x", Source: "a"}, nil }
	s, _ := newService(f)
	s.Settings = func(context.Context) settings.Values {
		return settings.Values{settings.RequestLimitSeconds.Name: 7, settings.PassagesMax.Name: 5, settings.PassageCharsMax.Name: 50, settings.RequestCharsMax.Name: 50}
	}
	seen := &deadlineSources{fakeSources: f, left: &left}
	s.Sources = seen
	if _, fail := s.Translate(context.Background(), request("A piece.")); fail != nil {
		t.Fatal(fail)
	}
	if left <= 0 || left > 7*time.Second {
		t.Fatal(left)
	}
}

type deadlineSources struct {
	*fakeSources
	left *time.Duration
}

func (d *deadlineSources) Translate(ctx context.Context, p sources.Piece) (sources.Result, error) {
	if dl, ok := ctx.Deadline(); ok {
		*d.left = time.Until(dl)
	}
	return d.fakeSources.Translate(ctx, p)
}

// ---- the whole pipeline against BUMBLEBEE's stand-in over HTTP -----------------

type world struct {
	t       *testing.T
	ts      *httptest.Server
	service *Service
	logs    *bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	client := bumblebee.Client{URL: ts.URL + "/bumblebee", Key: standin.BumblebeeKey}
	book := &sources.Book{Read: client.Sources, MaxAge: func(context.Context) time.Duration { return time.Minute }}
	if err := book.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	gates := map[sources.GateKey]sources.Gate{}
	for _, name := range []string{"standin-translate", "standin-translate-b"} {
		gates[sources.GateKey{Source: name, From: "en", To: "ta"}] = sources.Gate{State: sources.GateOpen, SourceVersion: "1"}
	}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	fb := &sources.Fallback{Book: book, Provider: client, Logger: logger,
		Policy: func(context.Context) sources.Policy { return sources.Policy{Gates: gates} },
		Sleep:  func(context.Context, time.Duration) error { return nil }}
	return &world{t: t, ts: ts, logs: logs, service: &Service{Sources: fb, Logger: logger, Settings: func(context.Context) settings.Values { return settings.Defaults() }}}
}

func (w *world) mode(name string, params map[string]string) {
	w.t.Helper()
	raw, _ := json.Marshal(map[string]any{"mode": name, "params": params})
	res, err := http.Post(w.ts.URL+"/control/bumblebee/mode", "application/json", bytes.NewReader(raw))
	if err != nil {
		w.t.Fatal(err)
	}
	res.Body.Close()
}

func (w *world) received() string {
	w.t.Helper()
	res, err := http.Get(w.ts.URL + "/control/bumblebee/received")
	if err != nil {
		w.t.Fatal(err)
	}
	defer res.Body.Close()
	var b bytes.Buffer
	b.ReadFrom(res.Body)
	return b.String()
}

func TestAgainstTheStandIn(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	const value = "0771234567"
	a, fail := w.service.Translate(ctx, request("  Call "+value+" now.\n\nAn invented "+marker+" letter.  ", "Second passage."))
	if fail != nil {
		t.Fatal(fail)
	}
	want := "  [ta] Call " + value + " now.\n\n[ta] An invented " + marker + " letter.  "
	if a.Passages[0].Text != want || a.Passages[1].Text != "[ta] Second passage." {
		t.Fatalf("%q", a.Passages[0].Text)
	}
	if got := w.received(); strings.Contains(got, value) || !strings.Contains(got, "⟦M1⟧") || !strings.Contains(got, `\"request_id\":\"r-1\"`) {
		t.Fatal(got)
	}
	// Only one source down: the other answers, and the piece says so.
	w.mode("timeout", map[string]string{"source": "standin-translate"})
	if a, fail = w.service.Translate(ctx, request("Third.")); fail != nil || a.Passages[0].Pieces[0].Source != "standin-translate-b" || !a.Passages[0].Pieces[0].Fallback {
		t.Fatalf("%+v %+v", fail, a)
	}
	// A source that echoes is a source failing: R-TRN-09. Every source echoing is no
	// provider, never the original as if it were translated.
	w.mode("echo", nil)
	if a, fail = w.service.Translate(ctx, request("Fourth.")); fail == nil || fail.Reason != reasons.NoProvider || a.Passages != nil {
		t.Fatalf("%+v %+v", fail, a)
	}
	// Each source failing the same way.
	w.mode("unavailable", nil)
	if _, fail = w.service.Translate(ctx, request("Fifth.")); fail == nil || fail.Reason != reasons.NoProvider {
		t.Fatalf("%+v", fail)
	}
	// A stand-in lost or changed fails the request: never an answer without the value.
	w.mode("lose-masking", nil)
	if a, fail = w.service.Translate(ctx, request("Call "+value+" now.")); fail == nil || fail.Reason != reasons.MaskingLost || a.Passages != nil {
		t.Fatalf("%+v %+v", fail, a)
	}
	w.mode("change-term", map[string]string{"from": "⟦M1⟧", "to": "⟦M2⟧"})
	if _, fail = w.service.Translate(ctx, request("Call "+value+" now.")); fail == nil || fail.Reason != reasons.MaskingLost {
		t.Fatalf("%+v", fail)
	}
	// A size smaller than the list said: read again, split again, joined.
	w.mode("normal", map[string]string{"max_text_chars": "12"})
	a, fail = w.service.Translate(ctx, request("One two three four five"))
	if fail != nil || a.Passages[0].Text != "[ta] One two [ta] three four [ta] five" {
		t.Fatalf("%+v %+v", fail, a)
	}
	// R-TRN-02 from a source that gives way midway through the list is one failure.
	for _, place := range []string{w.logs.String()} {
		if strings.Contains(place, marker) || strings.Contains(place, value) {
			t.Fatal(place)
		}
	}
}

// R-ERR-03 against a silent stand-in: the request stops at its limit with the reason.
func TestASilentSourceTimesOut(t *testing.T) {
	w := newWorld(t)
	w.mode("silent", nil)
	w.service.Settings = func(context.Context) settings.Values {
		v := settings.Defaults()
		v[settings.RequestLimitSeconds.Name] = 3 // the setting's own floor is higher; here it is the shape that counts
		return v
	}
	began := time.Now()
	_, fail := w.service.Translate(context.Background(), request("A piece."))
	if fail == nil || fail.Reason != reasons.TimedOut || time.Since(began) > 10*time.Second {
		t.Fatalf("%+v %v", fail, time.Since(began))
	}
}
