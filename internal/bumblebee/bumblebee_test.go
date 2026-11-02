package bumblebee_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/standin"
)

func stand(t *testing.T) (*httptest.Server, bumblebee.Client) {
	t.Helper()
	s := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { s.Close(); ts.Close() })
	return ts, bumblebee.Client{URL: ts.URL + "/bumblebee", Key: standin.BumblebeeKey}
}

func setMode(t *testing.T, ts *httptest.Server, mode string, params map[string]string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"mode": mode, "params": params})
	res, err := http.Post(ts.URL+"/control/bumblebee/mode", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

func TestReadsTheListFromTheStandIn(t *testing.T) {
	ts, c := stand(t)
	l, err := c.Sources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if l.MaxTextChars <= 0 || len(l.Sources) < 2 {
		t.Fatalf("%+v", l)
	}
	tr, ok := l.Find("standin-translate")
	if !ok || !tr.Offer(bumblebee.OpTranslate, "en", "ta") || tr.Offer(bumblebee.OpTranslate, "en", "en") || tr.Offer(bumblebee.OpGrammar, "en", "") {
		t.Fatalf("%+v", tr)
	}
	gr, ok := l.Find("standin-grammar")
	if !ok || !gr.Offer(bumblebee.OpGrammar, "en", "") || gr.Scores["en"] == 0 {
		t.Fatalf("%+v", gr)
	}
	// The size is read, never kept here: told another, the list says another.
	setMode(t, ts, "normal", map[string]string{"max_text_chars": "123"})
	if l, err = c.Sources(context.Background()); err != nil || l.MaxTextChars != 123 {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestStatesFromTheStandIn(t *testing.T) {
	ts, c := stand(t)
	now := time.Now()
	for mode, want := range map[string]string{"normal": bumblebee.StateOn, "blocked": bumblebee.StateRestingBlock, "changed": bumblebee.StateRestingChange, "source-off": bumblebee.StateOff} {
		setMode(t, ts, mode, nil)
		l, err := c.Sources(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		s := l.Sources[0]
		if s.State != want {
			t.Errorf("%s: %+v", mode, s)
		}
		if resting := strings.HasPrefix(want, "resting"); resting != (s.RestUntil != nil) {
			t.Errorf("%s: rest_until %v", mode, s.RestUntil)
		}
		if usable := want == bumblebee.StateOn; s.Usable(now) != usable {
			t.Errorf("%s: usable %v", mode, s.Usable(now))
		}
	}
}

func TestUsable(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	past, ahead := now.Add(-time.Second), now.Add(time.Second)
	for _, c := range []struct {
		s    bumblebee.Source
		want bool
	}{
		{bumblebee.Source{State: "on"}, true},
		{bumblebee.Source{State: "off"}, false},
		{bumblebee.Source{State: "resting-block", RestUntil: &ahead}, false},
		{bumblebee.Source{State: "resting-block", RestUntil: &now}, true},
		{bumblebee.Source{State: "resting-change", RestUntil: &past}, true},
		{bumblebee.Source{State: "resting-change"}, false},
		{bumblebee.Source{State: "from-a-later-version"}, false},
		{bumblebee.Source{}, false},
	} {
		if c.s.Usable(now) != c.want {
			t.Errorf("%+v", c.s)
		}
	}
}

func TestParseListFaults(t *testing.T) {
	for _, raw := range []string{`nope`, `{}`, `{"limits":{"max_text_chars":5}}`, `{"sources":[{"version":"1"}]}`, `{"sources":"x"}`} {
		if _, err := bumblebee.ParseList([]byte(raw)); err == nil {
			t.Errorf("%s", raw)
		}
	}
	// No size given, an empty list, a bad resume time and unknown fields all read.
	l, err := bumblebee.ParseList([]byte(`{"sources":[{"name":"a","state":"resting-block","rest_until":"soon","from_later":1}],"later":true}`))
	if err != nil || l.MaxTextChars != 0 || l.Sources[0].RestUntil != nil {
		t.Fatalf("%+v %v", l, err)
	}
	if l, err := bumblebee.ParseList([]byte(`{"limits":{"max_text_chars":-4},"sources":[]}`)); err != nil || l.MaxTextChars != 0 {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestSourcesFaults(t *testing.T) {
	ts, c := stand(t)
	bad := bumblebee.Client{URL: c.URL, Key: "bb_nobody"}
	if _, err := bad.Sources(context.Background()); err == nil || strings.Contains(err.Error(), "bb_nobody") {
		t.Fatal(err)
	}
	setMode(t, ts, "silent", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.Sources(ctx); err == nil {
		t.Fatal("a silent stand-in answered")
	}
}

func piece(text string) bumblebee.Request {
	return bumblebee.Request{RequestID: "r-1", Source: "standin-translate", From: "en", To: "ta", Text: text}
}

func TestTranslateAgainstTheStandIn(t *testing.T) {
	_, c := stand(t)
	a, err := c.Translate(context.Background(), piece("An invented letter."))
	if err != nil || a.Text != "[ta] An invented letter." || a.Source != "standin-translate" || a.Version != "1" {
		t.Fatalf("%+v %v", a, err)
	}
}

// Every failure in CALLERS.md reads as itself; those that say when, say when.
func TestEachReasonFromTheStandIn(t *testing.T) {
	ts, c := stand(t)
	for reason, retries := range map[string]bool{"busy": true, "blocked": true, "changed": true, "timeout": false, "too-long": false, "unsupported": false, "source-off": false, "unavailable": false} {
		setMode(t, ts, reason, nil)
		_, err := c.Translate(context.Background(), piece("An invented letter."))
		var f *bumblebee.Failure
		if !errors.As(err, &f) || f.Reason != reason || (f.RetryAt != nil) != retries {
			t.Errorf("%s: %v", reason, err)
		}
	}
}

// The reason decides, never the status: a failure under a 200 is a failure, and a
// reason it does not know is "unavailable". Nothing of the explanation is kept.
func TestReadsTheReasonAloneAndKeepsNoText(t *testing.T) {
	var answer string
	var status int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(answer))
	}))
	defer ts.Close()
	c := bumblebee.Client{URL: ts.URL, Key: "k"}
	for _, x := range []struct {
		status       int
		answer, want string
	}{
		{200, `{"failure":{"reason":"blocked","explanation":"CALLERTEXTMARKER","retry_at":"2030-01-01T04:30:00Z"}}`, "blocked"},
		{503, `{"text":"CALLERTEXTMARKER","failure":{"reason":"timeout","explanation":"CALLERTEXTMARKER"}}`, "timeout"},
		{200, `{"failure":{"reason":"from-a-later-version","explanation":"CALLERTEXTMARKER"}}`, "unavailable"},
		{200, `CALLERTEXTMARKER`, "unavailable"},
		{200, `{"source":"CALLERTEXTMARKER"}`, "unavailable"},
		{401, `{"error":"unknown_key"}`, "unavailable"},
	} {
		status, answer = x.status, x.answer
		_, err := c.Translate(context.Background(), piece("x"))
		var f *bumblebee.Failure
		if !errors.As(err, &f) || f.Reason != x.want || strings.Contains(err.Error(), "CALLERTEXTMARKER") {
			t.Errorf("%s: %v", x.answer, err)
		}
	}
	status, answer = 503, `{"text":"under another status"}`
	if a, err := c.Translate(context.Background(), piece("x")); err != nil || a.Text != "under another status" {
		t.Fatal(a, err)
	}
}

func TestSilenceIsTheTimeRunningOutOrUnavailable(t *testing.T) {
	ts, c := stand(t)
	setMode(t, ts, "silent", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Translate(ctx, piece("x")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	down := bumblebee.Client{URL: "http://127.0.0.1:1", Key: "k"}
	var f *bumblebee.Failure
	if _, err := down.Translate(context.Background(), piece("x")); !errors.As(err, &f) || f.Reason != "unavailable" {
		t.Fatal(err)
	}
}

func TestTheRequestCarriesTheReferenceAndTheKey(t *testing.T) {
	ts, c := stand(t)
	if _, err := c.Translate(context.Background(), piece("x")); err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(ts.URL + "/control/bumblebee/received")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var seen struct {
		Received []standin.Received `json:"received"`
	}
	json.NewDecoder(res.Body).Decode(&seen)
	last := seen.Received[len(seen.Received)-1]
	if last.Auth != "Bearer" || !strings.Contains(last.Body, `"request_id":"r-1"`) || !strings.Contains(last.Body, `"operation":"translate"`) {
		t.Fatalf("%+v", last)
	}
}

func TestGrammarCallAndAnswers(t *testing.T) {
	var sent map[string]string
	body := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/requests" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("%s %v", r.URL.Path, r.Header)
		}
		json.NewDecoder(r.Body).Decode(&sent)
		w.Write([]byte(body))
	}))
	defer ts.Close()
	c := bumblebee.Client{URL: ts.URL, Key: "k"}
	call := func() (bumblebee.GrammarAnswer, error) {
		return c.Grammar(context.Background(), bumblebee.GrammarRequest{RequestID: "r-1", Source: "g", Text: "We recieve."})
	}
	body = `{"request_id":"r-1","source":"g","version":"2","took_ms":3,"suggestions":[{"start":3,"length":7,"message":"m","replacements":["x"],"category":"spelling"}]}`
	a, err := call()
	if err != nil || len(a.Suggestions) != 1 || a.Source != "g" || a.Version != "2" || a.Suggestions[0].Start != 3 || a.Suggestions[0].Length != 7 || a.Suggestions[0].Replacements[0] != "x" {
		t.Fatalf("%+v %v", a, err)
	}
	if sent["operation"] != "grammar" || sent["language"] != "en" || sent["source"] != "g" || sent["request_id"] != "r-1" || sent["text"] != "We recieve." {
		t.Fatalf("%v", sent)
	}
	if _, has := sent["from"]; has {
		t.Fatalf("%v", sent)
	}
	body = `{"source":"g","suggestions":[]}`
	if a, err = call(); err != nil || a.Suggestions == nil || len(a.Suggestions) != 0 {
		t.Fatalf("%+v %v", a, err)
	}
	for _, b := range []string{`{"source":"g"}`, `not json`, `{"suggestions":"x"}`, `{"failure":{"reason":"made-up","explanation":"GRAMMARTEXT"}}`} {
		body = b
		_, err := call()
		var f *bumblebee.Failure
		if !errors.As(err, &f) || f.Reason != bumblebee.ReasonUnavailable || strings.Contains(err.Error(), "GRAMMARTEXT") {
			t.Errorf("%s: %v", b, err)
		}
	}
	body = `{"failure":{"reason":"busy","retry_at":"2030-01-01T04:30:00Z"}}`
	_, err = call()
	var f *bumblebee.Failure
	if !errors.As(err, &f) || f.Reason != bumblebee.ReasonBusy || f.RetryAt == nil {
		t.Fatal(err)
	}
}
