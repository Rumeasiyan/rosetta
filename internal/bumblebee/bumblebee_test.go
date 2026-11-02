package bumblebee_test

import (
	"bytes"
	"context"
	"encoding/json"
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
