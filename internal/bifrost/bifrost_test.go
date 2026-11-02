package bifrost_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bifrost"
	"github.com/Rumeasiyan/rosetta/internal/standin"
)

func stand(t *testing.T) (*httptest.Server, func(mode string, params string)) {
	t.Helper()
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	return ts, func(mode, params string) {
		t.Helper()
		body := `{"mode":"` + mode + `"` + params + `}`
		res, err := http.Post(ts.URL+"/control/bifrost/mode", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal(mode, res.StatusCode)
		}
	}
}

const person = "someone-invented@example.invalid"

func TestFoundAndNotFound(t *testing.T) {
	ts, mode := stand(t)
	c := bifrost.Client{URL: ts.URL + "/bifrost", Key: standin.BifrostKey}
	ctx := context.Background()
	a, err := c.Lookup(ctx, person)
	if err != nil || !a.Found || len(a.Offices) != 1 || a.Offices[0].Office != "EP-I-1" || a.Offices[0].Post != "Invented Clerk" {
		t.Fatal(a, err)
	}
	mode("found-no-offices", "")
	if a, err = c.Lookup(ctx, person); err != nil || !a.Found || len(a.Offices) != 0 {
		t.Fatal(a, err)
	}
	mode("not-found", "")
	if a, err = c.Lookup(ctx, person); err != nil || a.Found {
		t.Fatal(a, err)
	}
}

// Every other answer is a failure, and says nothing about the person (EXCHANGE.md), no_list_held included.
func TestEverythingElseIsAFailedLookUp(t *testing.T) {
	ts, mode := stand(t)
	c := bifrost.Client{URL: ts.URL + "/bifrost", Key: standin.BifrostKey}
	for _, m := range []string{"no_key", "key_withdrawn", "no_agreement", "not_this_kind_of_fact", "no_list_held", "agreement_must_be_named", "bad_request", "too_many", "agreement_cannot_be_checked", "list_unavailable", "not_recorded"} {
		mode(m, "")
		_, err := c.Lookup(context.Background(), person)
		var refused *bifrost.Refused
		if !errors.As(err, &refused) || refused.Code != m {
			t.Errorf("%s: %v", m, err)
		}
		if err != nil && strings.Contains(err.Error(), person) {
			t.Errorf("%s: the address is in the error", m)
		}
		if m == "too_many" && (!refused.TooMany() || refused.Wait != 60*time.Second) {
			t.Errorf("too_many: %+v", refused)
		}
	}
}

func TestSilenceAndNonsenseAreFailures(t *testing.T) {
	ts, mode := stand(t)
	c := bifrost.Client{URL: ts.URL + "/bifrost", Key: standin.BifrostKey}
	mode("silent", "")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.Lookup(ctx, person); err == nil {
		t.Fatal("silence read as an answer")
	}
	for _, body := range []string{`{}`, `not json`, `{"found":"no"}`, `{"offices":[]}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) }))
		if _, err := (bifrost.Client{URL: srv.URL, Key: "k"}).Lookup(context.Background(), person); err == nil {
			t.Errorf("%s read as an answer", body)
		}
		srv.Close()
	}
}

func TestAWrongKeyIsRefused(t *testing.T) {
	ts, _ := stand(t)
	_, err := (bifrost.Client{URL: ts.URL + "/bifrost", Key: "not-one"}).Lookup(context.Background(), person)
	var refused *bifrost.Refused
	if !errors.As(err, &refused) || refused.Code != "no_key" {
		t.Fatal(err)
	}
}
