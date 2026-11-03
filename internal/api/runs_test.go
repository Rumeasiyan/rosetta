package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/apisign"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/runs"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/Rumeasiyan/rosetta/internal/usage"
)

// runBench is the bench with the pipeline against the stand-in, and background runs.
type runBench struct {
	*bench
	ts    *httptest.Server
	book  *runs.Book
	mu    sync.Mutex
	kept  []usage.Record
	wrote chan struct{}
}

func newRunBench(t *testing.T, l runs.Limits) *runBench {
	t.Helper()
	b := newBench(t)
	r := &runBench{bench: b, wrote: make(chan struct{}, 100)}
	r.ts = withPipeline(t, b)
	r.book = &runs.Book{Limits: func(context.Context) runs.Limits { return l }, Logger: b.srv.deps.Logger}
	t.Cleanup(r.book.Stop)
	b.srv.deps.Runner = b.srv.deps.Translator.(*translate.Service)
	b.srv.deps.Runs = r.book
	b.srv.deps.Usage = func(_ context.Context, rec usage.Record) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.kept = append(r.kept, rec)
		r.wrote <- struct{}{}
		return nil
	}
	return r
}

func wideRuns() runs.Limits {
	return runs.Limits{PerSystem: 5, InAll: 50, AtATime: 2, Longest: time.Minute, Uncollected: time.Minute}
}

func (r *runBench) records() []usage.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]usage.Record(nil), r.kept...)
}

// waitRecords waits for the records of runs, which are written as they end.
func (r *runBench) waitRecords(n int) []usage.Record {
	r.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if got := r.records(); len(got) >= n {
			return got
		}
		select {
		case <-r.wrote:
		case <-deadline:
			r.t.Fatalf("%d records, not %d", len(r.records()), n)
		}
	}
}

func (r *runBench) mode(mode string, delayMS int) {
	r.t.Helper()
	res, err := http.Post(r.ts.URL+"/control/bumblebee/mode", "application/json", strings.NewReader(fmt.Sprintf(`{"mode":%q,"params":{"delay_ms":"%d"}}`, mode, delayMS)))
	if err != nil {
		r.t.Fatal(err)
	}
	res.Body.Close()
}

// call sends a request with a key that signs nothing.
func (r *runBench) call(method, target, key, sent string) *httptest.ResponseRecorder {
	q := httptest.NewRequest(method, target, strings.NewReader(sent))
	q.Header.Set("Authorization", "Bearer "+key)
	return r.send(q)
}

func (r *runBench) start(key string, passages ...string) string {
	r.t.Helper()
	got := r.call("POST", runsPath, key, body(r.t, passages...))
	if got.Code != 202 {
		r.t.Fatalf("%d %s", got.Code, got.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(got.Body.Bytes(), &out); err != nil {
		r.t.Fatal(err)
	}
	if len(out) != 3 || (out["state"] != "waiting" && out["state"] != "working") || !strings.HasPrefix(out["run"].(string), "run-") {
		r.t.Fatalf("%v", out)
	}
	if _, err := time.Parse(time.RFC3339, out["ask_again_at"].(string)); err != nil {
		r.t.Fatal(err)
	}
	return out["run"].(string)
}

// collect asks until the run has ended, and gives the answer handed over.
func (r *runBench) collect(key, run string) *httptest.ResponseRecorder {
	r.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		got := r.call("GET", runsPath+"/"+run, key, "")
		if got.Code != 200 || !strings.Contains(got.Body.String(), `"state"`) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatal("the run never ended")
	return nil
}

func runReason(t *testing.T, got *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Error struct {
			Reason   string            `json:"reason"`
			Messages map[string]string `json:"messages"`
		} `json:"error"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &out); err != nil {
		t.Fatalf("%v: %s", err, got.Body)
	}
	return out.Error.Reason
}

var refs = regexp.MustCompile(`"request_ref":"[^"]*"`)

// R-TRN-11: the answer is exactly what a direct request would have answered, byte for byte but for its
// own reference; it is handed over once, then forgotten, and the run is one record.
func TestARunIsHandedOverOnceExactlyAsADirectAnswer(t *testing.T) {
	r := newRunBench(t, wideRuns())
	passages := []string{"  An invented " + marker + ".\n", "Call 0771234567 now.", "Call 0771234567 now."}
	direct := r.call("POST", "/api/v1/translate", "rst_here", body(t, passages...))
	if direct.Code != 200 {
		t.Fatalf("%d %s", direct.Code, direct.Body)
	}
	r.waitRecords(1)
	run := r.start("rst_here", passages...)
	if len(r.records()) != 1 {
		t.Fatal("taking a run on wrote a record")
	}
	got := r.collect("rst_here", run)
	if got.Code != 200 || got.Header().Get("Cache-Control") != "no-store" || got.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%d %v", got.Code, got.Header())
	}
	if refs.ReplaceAllString(got.Body.String(), "") != refs.ReplaceAllString(direct.Body.String(), "") {
		t.Fatalf("not the direct answer:\n%s\n%s", got.Body, direct.Body)
	}
	// Once only.
	again := r.call("GET", runsPath+"/"+run, "rst_here", "")
	if again.Code != 404 || runReason(t, again) != "run not known" {
		t.Fatalf("%d %s", again.Code, again.Body)
	}
	for _, lang := range []string{"si", "ta", "en"} {
		if !strings.Contains(again.Body.String(), `"`+lang+`":"`) {
			t.Fatalf("no %s in %s", lang, again.Body)
		}
	}
	// One record for the run: asking made none of its own; the one refused ask has its own.
	recs := r.waitRecords(3)
	if len(recs) != 3 {
		t.Fatalf("%+v", recs)
	}
	var out translateAnswer
	json.Unmarshal(got.Body.Bytes(), &out)
	var run1 *usage.Record
	for i := range recs {
		if recs[i].Ref == out.RequestRef {
			run1 = &recs[i]
		}
	}
	if run1 == nil || run1.Operation != usage.OpTranslate || run1.Outcome != usage.Answered || run1.Passages != 3 || run1.Pieces.Source != 3 || run1.CallerID != 1 || run1.Characters == 0 || run1.From != "en" {
		t.Fatalf("%+v", run1)
	}
	for _, rec := range recs {
		if rec.Outcome == "run not known" && rec.Operation != usage.OpRead {
			t.Fatalf("%+v", rec)
		}
		if strings.Contains(fmt.Sprintf("%+v", rec), marker) {
			t.Fatalf("%+v", rec)
		}
	}
	if strings.Contains(r.logs.String(), marker) || strings.Contains(r.logs.String(), "0771234567") {
		t.Fatal(r.logs)
	}
}

// R-TRN-11: while it works, its state and counts, and never any text, nor anything of its masks.
func TestWhileWorkingNoTextIsSaid(t *testing.T) {
	r := newRunBench(t, wideRuns())
	r.mode("slow", 150)
	run := r.start("rst_here", "An invented "+marker+".", "A second invented "+marker+" line, 0771234567.", "A third.")
	saw := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got := r.call("GET", runsPath+"/"+run, "rst_here", "")
		if !strings.Contains(got.Body.String(), `"state"`) {
			if got.Code != 200 || !strings.Contains(got.Body.String(), marker) {
				t.Fatalf("%d %s", got.Code, got.Body)
			}
			if !saw {
				t.Fatal("never seen with some pieces done")
			}
			return
		}
		var state map[string]any
		if err := json.Unmarshal(got.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if got.Code != 200 || len(state) != 4 || state["pieces_total"] != float64(3) || strings.Contains(got.Body.String(), marker) || strings.Contains(got.Body.String(), "⟦") {
			t.Fatalf("%d %s", got.Code, got.Body)
		}
		if _, err := time.Parse(time.RFC3339, state["ask_again_at"].(string)); err != nil {
			t.Fatal(err)
		}
		if state["pieces_done"].(float64) > 0 && state["state"] == "working" {
			saw = true
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("never ended")
}

// R-TRN-10: checked at once and refused as a direct request would be, with the same reasons and the
// same record at once.
func TestARunIsRefusedAtOnceAsADirectRequest(t *testing.T) {
	r := newRunBench(t, wideRuns())
	for name, sent := range map[string]string{
		"not JSON":       marker,
		"a null passage": `{"from":"en","to":"ta","sensitivity":"ordinary","passages":[null]}`,
		"no sensitivity": `{"from":"en","to":"ta","passages":["` + marker + `"]}`,
		"other language": `{"from":"en","to":"fr","sensitivity":"ordinary","passages":["` + marker + `"]}`,
		"closed":         `{"from":"si","to":"ta","sensitivity":"ordinary","passages":["` + marker + `"]}`,
		"confidential":   `{"from":"en","to":"ta","sensitivity":"confidential","passages":["` + marker + `"]}`,
		"too many":       body(t, make([]string, 201)...),
		"too much":       body(t, strings.Repeat("x", 5001)),
	} {
		started := r.call("POST", runsPath, "rst_here", sent)
		direct := r.call("POST", "/api/v1/translate", "rst_here", sent)
		if started.Code != direct.Code || started.Code == 202 || refs.ReplaceAllString(started.Body.String(), "") != refs.ReplaceAllString(direct.Body.String(), "") {
			t.Errorf("%s: %d %s, directly %d %s", name, started.Code, started.Body, direct.Code, direct.Body)
		}
		if strings.Contains(started.Body.String(), marker) {
			t.Errorf("%s: %s", name, started.Body)
		}
	}
	for _, rec := range r.waitRecords(16) {
		if rec.Operation != usage.OpTranslate || rec.Outcome == usage.Answered {
			t.Fatalf("%+v", rec)
		}
	}
	if w, g := r.book.Counts(nil, true); w+g != 0 {
		t.Fatal(w, g)
	}
	if strings.Contains(r.logs.String(), marker) {
		t.Fatal(r.logs)
	}
	// Room refused runs were kept in has come back.
	for i := 0; i < 5; i++ {
		r.start("rst_here", "An invented letter.")
	}
}

// R-TRN-10, R-NFR-09: while a direct request would fail at once for want of a source, so does a
// run, with when to try again.
func TestARunWithNoSourceToCallIsRefusedAtOnce(t *testing.T) {
	r := newRunBench(t, wideRuns())
	fallback := r.srv.deps.Translator.(*translate.Service).Sources.(*sources.Fallback)
	fallback.Book.MaxAge = func(context.Context) time.Duration { return 0 }
	sent := body(t, "An invented "+marker+".")
	got := r.call("POST", runsPath, "rst_here", sent)
	direct := r.call("POST", "/api/v1/translate", "rst_here", sent)
	if got.Code != 503 || runReason(t, got) != "no provider" || !strings.Contains(got.Body.String(), `"position":0`) || refs.ReplaceAllString(got.Body.String(), "") != refs.ReplaceAllString(direct.Body.String(), "") {
		t.Fatalf("%d %s, directly %d %s", got.Code, got.Body, direct.Code, direct.Body)
	}
}

// R-TRN-11: a run asked after, or given up, by another system is not known, and stays its own.
func TestAnotherSystemsKeyIsRunNotKnown(t *testing.T) {
	r := newRunBench(t, wideRuns())
	r.keys.add("rst_other", callers.Holder{KeyID: 9, Prefix: "rst_other", Caller: callers.Caller{ID: 2, Reference: "TEST-9", Name: "TEST-9", TVAStatus: "live", PerMinute: 60, AtOnce: 5}})
	run := r.start("rst_here", "An invented "+marker+".")
	nowhere := r.call("GET", runsPath+"/run-nothing", "rst_other", "")
	for _, got := range []*httptest.ResponseRecorder{r.call("GET", runsPath+"/"+run, "rst_other", ""), r.call("DELETE", runsPath+"/"+run, "rst_other", "")} {
		if got.Code != 404 || runReason(t, got) != "run not known" || refs.ReplaceAllString(got.Body.String(), "") != refs.ReplaceAllString(nowhere.Body.String(), "") {
			t.Fatalf("%d %s", got.Code, got.Body)
		}
	}
	if len(r.denials) != 0 {
		t.Fatalf("%+v", r.denials)
	}
	if got := r.collect("rst_here", run); got.Code != 200 || !strings.Contains(got.Body.String(), marker) {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
}

// R-TRN-11: giving up forgets at once; the record still says what became of it.
func TestGivingUpARun(t *testing.T) {
	r := newRunBench(t, wideRuns())
	r.mode("slow", 200)
	run := r.start("rst_here", "An invented "+marker+".", "A second.", "A third.")
	got := r.call("DELETE", runsPath+"/"+run, "rst_here", "")
	if got.Code != 204 || got.Body.Len() != 0 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	if again := r.call("GET", runsPath+"/"+run, "rst_here", ""); again.Code != 404 || runReason(t, again) != "run not known" {
		t.Fatalf("%d %s", again.Code, again.Body)
	}
	if again := r.call("DELETE", runsPath+"/"+run, "rst_here", ""); again.Code != 404 {
		t.Fatalf("%d %s", again.Code, again.Body)
	}
	recs := r.waitRecords(3)
	seen := 0
	for _, rec := range recs {
		if rec.Outcome == usage.GivenUp {
			seen++
			if rec.Operation != usage.OpTranslate || rec.Passages != 3 {
				t.Fatalf("%+v", rec)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("%+v", recs)
	}
}

// R-TRN-12: past a system's runs or all runs, over limit with when to try again; R-CAL-07: asking
// counts against neither of a system's request limits.
func TestRunLimitsAndAskingIsNotCounted(t *testing.T) {
	l := wideRuns()
	l.PerSystem, l.InAll = 2, 3
	r := newRunBench(t, l)
	r.mode("slow", 300)
	r.keys.add("rst_tight", callers.Holder{KeyID: 8, Prefix: "rst_tight", Caller: callers.Caller{ID: 3, Reference: "TEST-8", Name: "TEST-8", TVAStatus: "live", PerMinute: 3, AtOnce: 1}})
	r.keys.add("rst_third", callers.Holder{KeyID: 7, Prefix: "rst_third", Caller: callers.Caller{ID: 4, Reference: "TEST-7", Name: "TEST-7", TVAStatus: "live", PerMinute: 60, AtOnce: 5}})
	a := r.start("rst_tight", "One invented line.")
	r.start("rst_tight", "Two invented lines.")
	over := r.call("POST", runsPath, "rst_tight", body(t, "Three."))
	if over.Code != 429 || runReason(t, over) != "over limit" || over.Header().Get("Retry-After") == "" || !strings.Contains(over.Body.String(), `"retry_at"`) {
		t.Fatalf("%d %s", over.Code, over.Body)
	}
	// It used its third request of the minute, and is held to that; asking goes on.
	for i := 0; i < 20; i++ {
		if got := r.call("GET", runsPath+"/"+a, "rst_tight", ""); got.Code != 200 {
			t.Fatalf("%d: %d %s", i, got.Code, got.Body)
		}
	}
	if got := r.call("POST", "/api/v1/translate", "rst_tight", body(t, "Four.")); got.Code != 429 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	r.start("rst_third", "An invented line.")
	if got := r.call("POST", runsPath, "rst_third", body(t, "Another.")); got.Code != 429 || runReason(t, got) != "over limit" {
		t.Fatalf("all: %d %s", got.Code, got.Body)
	}
	// Giving up, with its requests a minute used, still works, and makes room.
	if got := r.call("DELETE", runsPath+"/"+a, "rst_tight", ""); got.Code != 204 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	if got := r.call("POST", runsPath, "rst_third", body(t, "Again.")); got.Code != 202 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
}

// A run takes a request's place only while it is started: it holds no request at once.
func TestARunHoldsNoRequestAtOnce(t *testing.T) {
	r := newRunBench(t, wideRuns())
	r.mode("slow", 300)
	r.keys.add("rst_one", callers.Holder{KeyID: 8, Prefix: "rst_one", Caller: callers.Caller{ID: 3, Reference: "TEST-8", Name: "TEST-8", TVAStatus: "live", PerMinute: 60, AtOnce: 1}})
	r.start("rst_one", "One invented line.")
	r.start("rst_one", "Two invented lines.")
}

// GET and DELETE sign as every call: the empty body's digest must come and is covered, and the
// address is covered too.
func TestAskingIsSignedAsEveryCall(t *testing.T) {
	r := newRunBench(t, wideRuns())
	r.mode("slow", 400)
	got := r.send(r.signed(runsPath, "rst_signs", body(t, "An invented "+marker+"."), "nonce-run-start-01", t0))
	if got.Code != 202 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	var out runStarted
	json.Unmarshal(got.Body.Bytes(), &out)
	address := runsPath + "/" + out.Run
	make := func(method, target, nonce string) *http.Request {
		q := httptest.NewRequest(method, target, nil)
		q.Header.Set("Authorization", "Bearer rst_signs")
		apisign.Sign(q, nil, "rst_signs", nonce, r.private, t0)
		return q
	}
	refused := func(name string, q *http.Request) {
		t.Helper()
		if got := r.send(q); got.Code != 401 || runReason(t, got) != "no key" {
			t.Errorf("%s: %d %s", name, got.Code, got.Body)
		}
	}
	// No signature at all.
	q := httptest.NewRequest("GET", address, nil)
	q.Header.Set("Authorization", "Bearer rst_signs")
	refused("unsigned", q)
	// The digest left out.
	q = make("GET", address, "nonce-run-ask-0001")
	q.Header.Del("Content-Digest")
	refused("no digest", q)
	// A body that is not the one signed.
	q = make("GET", address, "nonce-run-ask-0002")
	withBody := httptest.NewRequest("GET", address, strings.NewReader("x"))
	withBody.Header = q.Header
	refused("another body", withBody)
	// A signature for another address, and for the other method.
	q = make("GET", runsPath+"/run-another", "nonce-run-ask-0003")
	moved := httptest.NewRequest("GET", address, nil)
	moved.Header = q.Header
	refused("another address", moved)
	q = make("GET", address, "nonce-run-ask-0004")
	other := httptest.NewRequest("DELETE", address, nil)
	other.Header = q.Header
	refused("another method", other)
	// A nonce used again.
	q = make("GET", address, "nonce-run-ask-0005")
	if got := r.send(q); got.Code != 200 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	refused("used again", make("GET", address, "nonce-run-ask-0005"))
	// Rightly signed, it goes through, and the run was kept through all of that.
	deadline := time.Now().Add(10 * time.Second)
	for i := 0; ; i++ {
		got := r.send(make("GET", address, fmt.Sprintf("nonce-run-ask-1%04d", i)))
		if got.Code != 200 {
			t.Fatalf("%d %s", got.Code, got.Body)
		}
		if strings.Contains(got.Body.String(), marker) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never ended")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := r.send(make("DELETE", address, "nonce-run-give-up1")); got.Code != 404 || runReason(t, got) != "run not known" {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
}

// ServeMux sends HEAD to a GET: it must not take the answer away.
func TestAHeadHandsNothingOver(t *testing.T) {
	r := newRunBench(t, wideRuns())
	run := r.start("rst_here", "An invented "+marker+".")
	r.waitRecords(1)
	if got := r.call("HEAD", runsPath+"/"+run, "rst_here", ""); got.Code != 400 {
		t.Fatalf("%d", got.Code)
	}
	if got := r.collect("rst_here", run); got.Code != 200 || !strings.Contains(got.Body.String(), marker) {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
}

// R-TRN-12: past its limit a run fails with timed out, its position with it, and it was given far
// longer than the limit of a direct request would allow in the first place.
func TestARunPastItsLimitIsTimedOut(t *testing.T) {
	l := wideRuns()
	l.Longest = 2500 * time.Millisecond
	r := newRunBench(t, l)
	r.mode("slow", 400)
	run := r.start("rst_here", "An invented "+marker+".", "A second invented line.", "A third invented line.", "A fourth.", "A fifth.")
	got := r.collect("rst_here", run)
	if got.Code != 504 || runReason(t, got) != "timed out" || !strings.Contains(got.Body.String(), `"position":`) || !strings.Contains(got.Body.String(), `"request_ref":"r-`) || strings.Contains(got.Body.String(), marker) {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	recs := r.waitRecords(1)
	if recs[0].Outcome != "timed out" || recs[0].Pieces.Source < 1 {
		t.Fatalf("%+v", recs[0])
	}
}

// R-TRN-02, R-TRN-11: a failure is handed over as a direct request's, with its position and reason.
func TestAFailureIsHandedOverOnceWithItsPosition(t *testing.T) {
	r := newRunBench(t, wideRuns())
	res, err := http.Post(r.ts.URL+"/control/bumblebee/mode", "application/json", strings.NewReader(`{"mode":"lose-masking"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	run := r.start("rst_here", "An invented "+marker+".", "Call 0771234567 now.")
	got := r.collect("rst_here", run)
	if got.Code != 502 || runReason(t, got) != "masking lost" || !strings.Contains(got.Body.String(), `"position":1`) || strings.Contains(got.Body.String(), "0771234567") {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	if again := r.call("GET", runsPath+"/"+run, "rst_here", ""); again.Code != 404 {
		t.Fatal(again.Code)
	}
}

// R-TRN-13: an answer not collected is forgotten at its time; the record of the run stays.
func TestAnUncollectedAnswerIsForgotten(t *testing.T) {
	l := wideRuns()
	l.Uncollected = 50 * time.Millisecond
	r := newRunBench(t, l)
	run := r.start("rst_here", "An invented "+marker+".")
	r.waitRecords(1)
	time.Sleep(200 * time.Millisecond)
	if got := r.call("GET", runsPath+"/"+run, "rst_here", ""); got.Code != 404 || runReason(t, got) != "run not known" {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	if got := r.records(); got[0].Outcome != usage.Answered {
		t.Fatalf("%+v", got)
	}
}

// R-NFR-08, R-REC-01: a stop loses a run with no record; asked after, it is not known.
func TestAStopLosesARunWithNoRecord(t *testing.T) {
	r := newRunBench(t, wideRuns())
	r.mode("slow", 2000)
	run := r.start("rst_here", "An invented "+marker+".")
	r.book.Stop()
	r.book.Wait()
	if got := r.call("GET", runsPath+"/"+run, "rst_here", ""); got.Code != 404 || runReason(t, got) != "run not known" {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	if got := r.call("POST", runsPath, "rst_here", body(t, "Another.")); got.Code != 503 || runReason(t, got) != "no provider" {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	for _, rec := range r.waitRecords(2) {
		if rec.Operation == usage.OpTranslate && rec.Outcome != "no provider" {
			t.Fatalf("%+v", rec)
		}
	}
}

// An address under the path that is not a run's is no request ROSETTA can read, and a service with
// no runs says so as every path does.
func TestRunAddresses(t *testing.T) {
	r := newRunBench(t, wideRuns())
	for _, c := range []struct {
		method, target string
		status         int
		reason         string
	}{
		{"GET", runsPath, 400, "invalid request"},
		{"DELETE", runsPath, 400, "invalid request"},
		{"POST", runsPath + "/run-x", 400, "invalid request"},
		{"GET", runsPath + "/a/b", 400, "invalid request"},
		{"GET", runsPath + "/not-a-reference", 404, "run not known"},
	} {
		got := r.call(c.method, c.target, "rst_here", "")
		if got.Code != c.status || runReason(t, got) != c.reason {
			t.Errorf("%s %s: %d %s", c.method, c.target, got.Code, got.Body)
		}
	}
	bare := newBench(t)
	q := request(runsPath, "rst_here", body(t, "A line."))
	if got := bare.send(q); got.Code != 503 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	if got := bare.send(httptest.NewRequest("GET", runsPath+"/x", nil)); got.Code != 401 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	q = httptest.NewRequest("GET", runsPath+"/x", nil)
	q.Header.Set("Authorization", "Bearer rst_here")
	if got := bare.send(q); got.Code != 404 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
}
