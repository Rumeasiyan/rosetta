package hedwig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/standin"
)

func newStandin(t *testing.T) (*httptest.Server, Client) {
	t.Helper()
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	return ts, Client{URL: ts.URL + "/hedwig", Key: standin.HedwigTestKey}
}

func mode(t *testing.T, ts *httptest.Server, m string) {
	t.Helper()
	res, err := http.Post(ts.URL+"/control/hedwig/mode", "application/json", strings.NewReader(`{"mode":"`+m+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

func notice(key string) Notice {
	return Notice{Type: "glossary_proposal", RequestKey: key, Details: map[string]string{"entry": "1"},
		People: []Person{{Ref: "rosetta-person-1", Email: "keeper@example.invalid", Language: "ta"}}}
}

// As API.md gives it: a test key's notice is recorded as test, and the same key sends nothing again.
func TestSendAndSendAgain(t *testing.T) {
	ts, c := newStandin(t)
	ctx := context.Background()
	first, err := c.Send(ctx, notice("one"))
	if err != nil || first.Status != "test" || first.RequestID == 0 {
		t.Fatalf("%+v %v", first, err)
	}
	again, err := c.Send(ctx, notice("one"))
	if err != nil || again.RequestID != first.RequestID {
		t.Fatalf("%+v %v", again, err)
	}
	if other, err := c.Send(ctx, notice("two")); err != nil || other.RequestID == first.RequestID {
		t.Fatalf("%+v %v", other, err)
	}
	res, err := http.Get(ts.URL + "/control/hedwig/received")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got struct {
		Received []standin.Received `json:"received"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Received) != 3 || !strings.Contains(got.Received[0].Body, `"ref":"rosetta-person-1"`) {
		t.Fatalf("%+v", got.Received)
	}
}

// Each refusal is its code alone; a message could name a detail.
func TestEachRefusalIsItsCode(t *testing.T) {
	ts, c := newStandin(t)
	for m, status := range map[string]int{"unauthorized": 401, "unknown_notice_type": 404, "missing_detail": 422, "rate_limited": 429, "internal": 500} {
		mode(t, ts, m)
		_, err := c.Send(context.Background(), notice(m))
		var r *Refused
		if !errors.As(err, &r) || r.Status != status || r.Code != m {
			t.Errorf("%s: %+v", m, err)
		}
		if m == "rate_limited" && r.Wait != time.Minute {
			t.Errorf("wait %v", r.Wait)
		}
		if strings.Contains(err.Error(), "keeper@example.invalid") {
			t.Error(err)
		}
	}
}

func TestOutOfReachSaysNothingOfTheKey(t *testing.T) {
	c := Client{URL: "http://127.0.0.1:1", Key: "hw_test_secret"}
	_, err := c.Send(context.Background(), notice("x"))
	if err == nil || strings.Contains(err.Error(), "hw_test_secret") {
		t.Fatal(err)
	}
}
