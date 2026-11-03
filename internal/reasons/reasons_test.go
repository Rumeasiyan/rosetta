package reasons

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The list is exactly R-ERR-01's, in its words.
func TestTheListIsTheOneInThePRD(t *testing.T) {
	want := "no key, over limit, invalid request, too long, unsupported, human translation required, no provincial provider, masking lost, no provider, timed out, run not known"
	var got []string
	for _, r := range All {
		got = append(got, string(r))
	}
	if strings.Join(got, ", ") != want {
		t.Fatal(got)
	}
}

// Every reason has a status, and a message in each language in its own script.
func TestEveryReasonHasAStatusAndThreeMessages(t *testing.T) {
	sinhala, tamil := regexp.MustCompile(`\p{Sinhala}`), regexp.MustCompile(`\p{Tamil}`)
	seen := map[string]Reason{}
	for _, r := range All {
		if r.Status() < 400 {
			t.Errorf("%s: %d", r, r.Status())
		}
		m := r.Messages()
		if len(m) != 3 || m["en"] == "" || m["en"] == r.Word() || !sinhala.MatchString(m["si"]) || !tamil.MatchString(m["ta"]) {
			t.Errorf("%s: %v", r, m)
		}
		if other, dup := seen[m["en"]]; dup {
			t.Errorf("%s and %s say the same", r, other)
		}
		seen[m["en"]] = r
	}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out["error"].(map[string]any)
}

func TestWriteGivesReasonMessagesPositionAndWhen(t *testing.T) {
	now := time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)
	rec := httptest.NewRecorder()
	f := New(NoProvider).At(3).Until(now.Add(90 * time.Second).In(time.FixedZone("x", 3600)))
	Write(rec, "r-1", f, now)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "90" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	e := decode(t, rec)
	if e["reason"] != "no provider" || e["position"] != float64(3) || e["retry_at"] != "2030-01-01T04:31:30Z" || e["messages"].(map[string]any)["ta"] == "" {
		t.Fatalf("%v", e)
	}
	if !strings.Contains(rec.Body.String(), `"request_ref":"r-1"`) {
		t.Fatal(rec.Body.String())
	}
}

// Position 0 is a position; no position, no time and no terms say nothing.
func TestWriteLeavesOutWhatItDoesNotKnow(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, "", New(Invalid), time.Now())
	e := decode(t, rec)
	for _, k := range []string{"position", "retry_at", "glossary_terms"} {
		if _, ok := e[k]; ok {
			t.Errorf("%s: %v", k, e)
		}
	}
	if rec.Header().Get("Retry-After") != "" || strings.Contains(rec.Body.String(), "request_ref") {
		t.Fatal(rec.Body.String())
	}
	rec = httptest.NewRecorder()
	Write(rec, "r", New(MaskingLost).At(0), time.Now())
	if decode(t, rec)["position"] != float64(0) {
		t.Fatal(rec.Body.String())
	}
}

// A time already past still tells the caller to wait, rather than sending nothing.
func TestRetryAfterIsNeverBelowOneSecond(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	rec := httptest.NewRecorder()
	Write(rec, "r", New(OverLimit).Until(now.Add(-time.Minute)), now)
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatal(rec.Header())
	}
	var f *Failure = New(OverLimit).Until(time.Time{})
	if f.RetryAt != nil {
		t.Fatal("the zero time says nothing")
	}
}

// A failure is the reason alone: logging one can carry no text.
func TestErrorIsTheReasonAlone(t *testing.T) {
	var err error = New(TooLong).At(4)
	if err.Error() != "too long" {
		t.Fatal(err)
	}
	var f *Failure
	if !errors.As(err, &f) || f.Reason != TooLong {
		t.Fatal(f)
	}
}
