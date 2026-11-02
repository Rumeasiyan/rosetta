package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// atOnce is how long "fails at once" may take here: far under any source's time, with room for a
// busy machine, and the stand-in told to say nothing so a call out would show as a wait.
const atOnce = 2 * time.Second

func reasonIn(t *testing.T, out map[string]any) string {
	t.Helper()
	e, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("no failure in %v", out)
	}
	return e["reason"].(string)
}

// R-NFR-09: while BUMBLEBEE is down the glossary and memory still answer, and everything else fails at
// once with its reason.
func TestWithBumblebeeDownTheGlossaryAndMemoryStillAnswer(t *testing.T) {
	w := newPathWorld(t)
	w.open("en", "ta")
	w.publish("post", "Continuity Registrar", "අඛණ්ඩ ලේඛකාධිකාරී", "தொடர் பதிவாளர்")
	raw, _ := json.Marshal(map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "approver_post": "An invented post",
		"passages": []map[string]string{{"source": "An approved invented sentence.", "approved": "அங்கீகரிக்கப்பட்ட புனைந்த வாக்கியம்."}}})
	q := httptest.NewRequest(http.MethodPost, "/api/v1/memory", bytes.NewReader(raw))
	q.Header.Set("Authorization", "Bearer "+w.key)
	rec := httptest.NewRecorder()
	w.mux.ServeHTTP(rec, q)
	if rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	// Down: the list goes past its age, and the stand-in would hold any call for minutes.
	w.mode("silent")
	w.maxAge = time.Nanosecond
	for _, text := range []string{"Continuity Registrar", "An approved invented sentence."} {
		if code, out := w.translate("ordinary", text); code != 200 {
			t.Fatalf("%q: %v", text, out)
		}
	}
	at := time.Now()
	code, out := w.translate("ordinary", "Something nobody has approved.")
	if took := time.Since(at); took > atOnce {
		t.Fatalf("took %v", took)
	}
	if code != 503 || reasonIn(t, out) != "no provider" {
		t.Fatalf("%d %v", code, out)
	}
	// A check fails the same way, at once.
	at = time.Now()
	form := url.Values{"language": {"en-GB"}, "text": {"We recieve this."}}
	c := httptest.NewRequest(http.MethodPost, "/api/v2/check", strings.NewReader(form.Encode()))
	c.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.Header.Set("Authorization", "Bearer "+w.key)
	rec = httptest.NewRecorder()
	w.mux.ServeHTTP(rec, c)
	if took := time.Since(at); took > atOnce || rec.Code != 503 {
		t.Fatalf("%v %d %s", took, rec.Code, rec.Body)
	}
	// Nothing was sent: the list is past its age, so nothing may go.
	if strings.Contains(w.received(), "Something nobody has approved") || strings.Contains(w.received(), "We recieve this") {
		t.Fatal("text went out while BUMBLEBEE was down")
	}
}

// R-NFR-09 again: every source for a direction down, with BUMBLEBEE answering. The earliest time one
// is expected back comes with the failure (R-PRV-06).
func TestWithEverySourceDownTheGlossaryAndMemoryStillAnswer(t *testing.T) {
	w := newPathWorld(t)
	w.open("en", "ta")
	w.publish("post", "Continuity Clerk", "අඛණ්ඩ ලිපිකරු", "தொடர் எழுத்தர்")
	w.mode("blocked")
	if err := w.book.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if code, out := w.translate("ordinary", "Continuity Clerk"); code != 200 {
		t.Fatal(out)
	}
	at := time.Now()
	code, out := w.translate("personal", "Another invented sentence for nobody.")
	if took := time.Since(at); took > atOnce {
		t.Fatalf("took %v", took)
	}
	if code != 503 || reasonIn(t, out) != "no provider" {
		t.Fatalf("%d %v", code, out)
	}
	if e := out["error"].(map[string]any); e["retry_at"] == nil {
		t.Fatalf("no time to try again: %v", out)
	}
	if strings.Contains(w.received(), "Another invented sentence") {
		t.Fatal("text went to a resting source")
	}
}
