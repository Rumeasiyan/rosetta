package audit

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

	"github.com/Rumeasiyan/rosetta/internal/standin"
)

func pensieveStandIn(t *testing.T) (*httptest.Server, Pensieve) {
	t.Helper()
	s := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { s.Close(); ts.Close() })
	return ts, Pensieve{URL: ts.URL + "/pensieve", Key: standin.PensieveKey}
}

func setMode(t *testing.T, ts *httptest.Server, mode string, params map[string]string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"mode": mode, "params": params})
	res, err := http.Post(ts.URL+"/control/pensieve/mode", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("%d", res.StatusCode)
	}
}

// rowsOf makes rows as Record would write them.
func rowsOf(t *testing.T, events ...Event) []row {
	t.Helper()
	var out []row
	for i, e := range events {
		id := "rosetta-test-" + time.Now().Format("150405.000000") + "-" + string(rune('a'+i))
		built, err := build(e, id, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(built)
		out = append(out, row{id: int64(i + 1), entry: id, body: raw})
	}
	return out
}

// The entries Record writes are ones PENSIEVE's contract keeps: a person's, with
// exactly "not known", and automatic work, with exactly "system".
func TestEntriesPassPensievesChecks(t *testing.T) {
	_, p := pensieveStandIn(t)
	sys := good()
	sys.Actor = System
	deny := good()
	deny.Action, deny.Result, deny.Reason, deny.Details = ActionDenial, ResultDenied, "Role does not allow this", nil
	set := good()
	set.Action, set.ObjectType, set.ObjectRef = ActionSettingChange, "setting", "caller_idle_days"
	set.Details = map[string]any{"old": "90", "new": "45"}
	set.Visitor = "192.0.2.1"
	rows := rowsOf(t, good(), sys, deny, set)
	got, stopped := deliver(context.Background(), p, rows, slog.New(slog.DiscardHandler))
	if stopped {
		t.Fatal("stopped")
	}
	for _, r := range rows {
		if !got[r.id].kept {
			t.Errorf("%s: %+v: %s", r.entry, got[r.id], r.body)
		}
	}
	// Sent again: the same answer, so a crash between sending and marking costs nothing.
	if again, _ := deliver(context.Background(), p, rows, slog.New(slog.DiscardHandler)); !again[1].kept {
		t.Fatalf("%+v", again)
	}
}

func TestRefusedForGoodIsMarkedAndNotKeptWaits(t *testing.T) {
	ts, p := pensieveStandIn(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	rows := rowsOf(t, good(), good())
	// A blank post, as a fault in code would write it.
	var e map[string]any
	json.Unmarshal(rows[1].body, &e)
	e["post"] = ""
	rows[1].body, _ = json.Marshal(e)
	got, stopped := deliver(context.Background(), p, rows, logger)
	if stopped || !got[1].kept || got[2].refusal != "missing post" {
		t.Fatalf("%v %+v", stopped, got)
	}
	if !strings.Contains(logs.String(), rows[1].entry) || !strings.Contains(logs.String(), "missing post") {
		t.Fatalf("%s", logs.String())
	}
	setMode(t, ts, "not_kept", nil)
	more := rowsOf(t, good())
	more[0].tries = 2
	got, stopped = deliver(context.Background(), p, more, logger)
	if stopped || got[1].kept || got[1].refusal != "" || got[1].wait != 4*time.Minute {
		t.Fatalf("%v %+v", stopped, got)
	}
}

func TestWholeRequestFailuresWait(t *testing.T) {
	ts, p := pensieveStandIn(t)
	discard := slog.New(slog.DiscardHandler)
	rows := rowsOf(t, good(), good())
	setMode(t, ts, "unavailable", nil)
	got, stopped := deliver(context.Background(), p, rows, discard)
	if !stopped || got[1].kept || got[1].wait != time.Minute {
		t.Fatalf("503: %v %+v", stopped, got)
	}
	setMode(t, ts, "too_many", map[string]string{"retry_seconds": "7"})
	got, stopped = deliver(context.Background(), p, rows, discard)
	if !stopped || got[1].wait != 7*time.Second || got[2].wait != 7*time.Second {
		t.Fatalf("429: %v %+v", stopped, got)
	}
	setMode(t, ts, "normal", nil)
	for _, key := range []string{"", "pv_nobody", standin.PensieveOffKey} {
		off := Pensieve{URL: p.URL, Key: key}
		_, err := off.Send(context.Background(), []json.RawMessage{rows[0].body})
		if err == nil || (key != "" && strings.Contains(err.Error(), key)) {
			t.Errorf("key %q: %v", key, err)
		}
		got, stopped = deliver(context.Background(), off, rows, discard)
		if !stopped || got[1].kept {
			t.Errorf("key %q: %v %+v", key, stopped, got)
		}
	}
	// Out of reach.
	far := Pensieve{URL: "http://127.0.0.1:1", Key: standin.PensieveKey}
	if _, err := far.Send(context.Background(), nil); err == nil || strings.Contains(err.Error(), standin.PensieveKey) {
		t.Fatalf("%v", err)
	}
	got, stopped = deliver(context.Background(), far, rows, discard)
	if !stopped || got[1].wait == 0 {
		t.Fatalf("%v %+v", stopped, got)
	}
}

// Silence must not hold a person's work: the client gives up.
func TestSilenceEndsAtTheDeadline(t *testing.T) {
	ts, p := pensieveStandIn(t)
	setMode(t, ts, "silent", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	began := time.Now()
	got, stopped := deliver(ctx, p, rowsOf(t, good()), slog.New(slog.DiscardHandler))
	if !stopped || got[1].kept || time.Since(began) > 5*time.Second {
		t.Fatalf("%v %+v", stopped, got)
	}
}

func TestABatchTooLargeIsSplit(t *testing.T) {
	ts, p := pensieveStandIn(t)
	setMode(t, ts, "normal", map[string]string{"limit": "2"})
	rows := rowsOf(t, good(), good(), good(), good(), good())
	got, stopped := deliver(context.Background(), p, rows, slog.New(slog.DiscardHandler))
	if stopped || len(got) != 5 {
		t.Fatalf("%v %+v", stopped, got)
	}
	for id, o := range got {
		if !o.kept {
			t.Errorf("%d: %+v", id, o)
		}
	}
}

func TestBackoffGrowsToItsLimit(t *testing.T) {
	for tries, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 7: time.Hour, 50: time.Hour} {
		if got := backoff(tries); got != want {
			t.Errorf("%d: %s", tries, got)
		}
	}
}

type failing struct{}

func (failing) Send(context.Context, []json.RawMessage) ([]Result, error) {
	return nil, errors.New("no")
}

// A row PENSIEVE's answer does not mention waits; it is never taken as kept.
type silentAboutOne struct{}

func (silentAboutOne) Send(context.Context, []json.RawMessage) ([]Result, error) {
	return []Result{}, nil
}

func TestAnAnswerWithoutTheEntryIsNotKept(t *testing.T) {
	got, _ := deliver(context.Background(), silentAboutOne{}, rowsOf(t, good()), slog.New(slog.DiscardHandler))
	if got[1].kept || got[1].wait == 0 {
		t.Fatalf("%+v", got)
	}
	if got, stopped := deliver(context.Background(), failing{}, rowsOf(t, good()), slog.New(slog.DiscardHandler)); !stopped || got[1].kept {
		t.Fatalf("%+v", got)
	}
}
