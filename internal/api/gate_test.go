package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/sources"
)

func TestSystemsReadTheGate(t *testing.T) {
	b := newBench(t)
	on := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	b.srv.deps.Snapshot = func(context.Context) sources.Snapshot {
		return sources.Snapshot{List: bumblebee.List{Sources: []bumblebee.Source{{Name: "a", Version: "1", State: "on", Offers: []bumblebee.Offer{{Operation: "translate", From: "en", To: "ta"}}}}}}
	}
	b.srv.deps.Policy = func(context.Context) sources.Policy {
		return sources.Policy{Gates: map[sources.GateKey]sources.Gate{{Source: "a", From: "en", To: "ta"}: {State: "open", SourceVersion: "1", EvaluatedOn: on}}}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/gate", nil)
	r.Header.Set("Authorization", "Bearer rst_here")
	rec := b.send(r)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out struct {
		Ref        string       `json:"request_ref"`
		Directions []gateAnswer `json:"directions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Directions) != 6 || out.Ref == "" {
		t.Fatalf("%+v", out)
	}
	for _, d := range out.Directions {
		if d.From == "en" && d.To == "ta" {
			if d.State != "open" || d.EvaluatedOn != "2030-01-01" || len(d.Sources) != 1 || d.Sources[0].Name != "a" {
				t.Fatalf("%+v", d)
			}
		} else if d.State != "closed" || len(d.Sources) != 0 || d.EvaluatedOn != "" {
			t.Fatalf("%+v", d)
		}
	}
	// With a key, like every other call.
	if rec := b.send(httptest.NewRequest(http.MethodGet, "/api/v1/gate", nil)); rec.Code != 401 {
		t.Fatal(rec.Code)
	}
}
