package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/usage"
)

// Every request with a known system leaves a record, also when it is refused; one with no
// known system has nowhere to be kept, so it is counted (R-REC-01, R-CAL-04).
func TestEveryRefusalWithAKnownSystemLeavesARecord(t *testing.T) {
	b := newBench(t)
	withPipeline(t, b)
	const marker = "USAGEMARKER"
	send := func(path, key, body string) {
		t.Helper()
		b.send(request(path, key, body))
	}
	send("/api/v1/translate", "rst_off", marker)                                    // a key turned off
	send("/api/v1/translate", "rst_retired", marker)                                // a system no longer live
	send("/api/v1/translate", "rst_here", `{"from":"`+marker+`","passages":["x"]}`) // refused as invalid
	send("/api/v1/nowhere", "rst_here", marker)                                     // no such address
	if len(b.uses) != 4 {
		t.Fatalf("%d records: %+v", len(b.uses), b.uses)
	}
	want := []struct{ op, outcome string }{
		{usage.OpTranslate, "no key"}, {usage.OpTranslate, "no key"}, {usage.OpTranslate, "invalid request"}, {usage.OpRead, "invalid request"},
	}
	for i, w := range want {
		if b.uses[i].Operation != w.op || b.uses[i].Outcome != w.outcome || b.uses[i].CallerID == 0 || b.uses[i].Ref == "" {
			t.Errorf("%d: %+v", i, b.uses[i])
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", b.uses), "\"") {
		t.Errorf("a record holds a quote from a body: %+v", b.uses)
	}

	send("/api/v1/translate", "rst_unknown", marker)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/translate", strings.NewReader(marker))
	b.send(r)
	if len(b.uses) != 4 || b.srv.Counts().Today()[reasons.NoKey] != 2 {
		t.Fatalf("%d records, counted %v", len(b.uses), b.srv.Counts().Today())
	}
}

// A record that cannot be written changes no answer, and the log holds fixed words.
func TestARecordThatFailsChangesNoAnswer(t *testing.T) {
	b := newBench(t)
	b.srv.deps.Usage = func(_ context.Context, _ usage.Record) error { return fmt.Errorf("USAGEMARKER") }
	rec := b.send(request("/api/v1/echo", "rst_here", "fine"))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(b.logs.String(), "a usage record was not kept") || strings.Contains(b.logs.String(), "USAGEMARKER") {
		t.Fatal(b.logs)
	}
}
