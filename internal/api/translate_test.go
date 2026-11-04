package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/standin"
	"github.com/Rumeasiyan/rosetta/internal/translate"
)

// withPipeline gives the bench the whole pipeline against BUMBLEBEE's stand-in.
func withPipeline(t *testing.T, b *bench) *httptest.Server {
	t.Helper()
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	client := bumblebee.Client{URL: ts.URL + "/bumblebee", Key: standin.BumblebeeKey}
	book := &sources.Book{Read: client.Sources, MaxAge: func(context.Context) time.Duration { return time.Minute }}
	if err := book.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	policy := sources.Policy{Gates: map[sources.GateKey]sources.Gate{{Source: "standin-translate", From: "en", To: "ta"}: {State: "open", SourceVersion: "1"}}}
	b.srv.deps.Translator = &translate.Service{
		Sources:  &sources.Fallback{Book: book, Provider: client, Policy: func(context.Context) sources.Policy { return policy }},
		Settings: func(context.Context) settings.Values { return settings.Defaults() },
	}
	return ts
}

func body(t *testing.T, passages ...string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "passages": passages})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestTranslateAListThroughTheStandIn(t *testing.T) {
	b := newBench(t)
	withPipeline(t, b)
	rec := b.send(request("/api/v1/translate", "rst_here", body(t, "  An invented "+marker+".\n", "Call 0771234567 now.")))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out translateAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.RequestRef == "" || len(out.Passages) != 2 || out.Passages[0].Text != "  [ta] An invented "+marker+".\n" || out.Passages[1].Text != "[ta] Call 0771234567 now." {
		t.Fatalf("%+v", out)
	}
	if strings.Contains(b.logs.String(), marker) || strings.Contains(b.logs.String(), "0771234567") {
		t.Fatal(b.logs)
	}
}

func TestTranslateRefusals(t *testing.T) {
	b := newBench(t)
	withPipeline(t, b)
	for name, c := range map[string]struct {
		body   string
		status int
		reason string
	}{
		"not JSON":       {marker, 400, "invalid request"},
		"wrong type":     {`{"from":"en","to":"ta","sensitivity":"ordinary","passages":"` + marker + `"}`, 400, "invalid request"},
		"a null passage": {`{"from":"en","to":"ta","sensitivity":"ordinary","passages":[null]}`, 400, "invalid request"},
		"no sensitivity": {`{"from":"en","to":"ta","passages":["` + marker + `"]}`, 400, "invalid request"},
		"other language": {`{"from":"en","to":"fr","sensitivity":"ordinary","passages":["` + marker + `"]}`, 400, "invalid request"},
		"closed":         {`{"from":"si","to":"ta","sensitivity":"ordinary","passages":["` + marker + `"]}`, 409, "human translation required"},
		"one too long":   {body(t, "short", strings.Repeat(marker, 1000)), 413, "too long"},
	} {
		rec := b.send(request("/api/v1/translate", "rst_here", c.body))
		if rec.Code != c.status || reasonOf(t, rec) != c.reason {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
		for _, place := range []string{rec.Body.String(), b.logs.String()} {
			if strings.Contains(place, marker) {
				t.Errorf("%s: the text escaped: %s", name, place)
			}
		}
	}
	if b.outcomes(reasons.Invalid) != 5 {
		t.Fatal(b.uses)
	}
	if len(b.denials) != 0 {
		t.Fatal("a bad request is no denial")
	}
	// The position goes back with a passage at fault (R-TRN-02).
	rec := b.send(request("/api/v1/translate", "rst_here", body(t, "short", strings.Repeat("a", 6000))))
	if !strings.Contains(rec.Body.String(), `"position":1`) {
		t.Fatal(rec.Body)
	}
}

func TestTranslateNeedsAKeyAndPost(t *testing.T) {
	b := newBench(t)
	withPipeline(t, b)
	if rec := b.send(request("/api/v1/translate", "", body(t, marker))); rec.Code != 401 || strings.Contains(rec.Body.String(), marker) {
		t.Fatal(rec.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/translate", nil)
	r.Header.Set("Authorization", "Bearer rst_here")
	if rec := b.send(r); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
}

// A signed call goes through the same way.
func TestASignedTranslation(t *testing.T) {
	b := newBench(t)
	withPipeline(t, b)
	rec := b.send(b.signed("/api/v1/translate", "rst_signs", body(t, "An invented letter."), "a-nonce-of-sixteen-or-more", t0))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "[ta] An invented letter.") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestSensitivityInTheAnswer(t *testing.T) {
	b := newBench(t)
	withPipeline(t, b)
	send := func(sensitivity string, passages ...string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"from": "en", "to": "ta", "sensitivity": sensitivity, "passages": passages})
		return b.send(request("/api/v1/translate", "rst_here", string(raw)))
	}
	rec := send("confidential", marker)
	if rec.Code != 403 || reasonOf(t, rec) != "no provincial provider" || strings.Contains(rec.Body.String(), marker) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = send("ordinary", "Card 123456789V here.")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"sensitivity":"personal"`) || !strings.Contains(rec.Body.String(), `"sensitivity_mismatch":true`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if last := b.uses[len(b.uses)-1]; !last.Mismatch || last.Sensitivity != "personal" || last.Outcome != "answered" {
		t.Fatalf("%+v", last)
	}
	if strings.Contains(b.logs.String(), "123456789V") {
		t.Fatal(b.logs)
	}
	rec = send("ordinary", "Call 0771234567 now.")
	if !strings.Contains(rec.Body.String(), `"sensitivity":"ordinary"`) || !strings.Contains(rec.Body.String(), `"sensitivity_mismatch":false`) {
		t.Fatal(rec.Body)
	}
}

// R-TRN-06, R-TRN-07, R-NFR-05: every field, on what the wire carries.
func TestEveryAnswerField(t *testing.T) {
	b := newBench(t)
	withPipeline(t, b)
	b.srv.deps.Translator.(*translate.Service).Version = func(context.Context) (int, error) { return 3, nil }
	rec := b.send(request("/api/v1/translate", "rst_here", body(t, "One. Two.", "")))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["request_ref"] == "" || out["from"] != "en" || out["to"] != "ta" || out["glossary_version"] != float64(3) || out["fallback_used"] != false {
		t.Fatalf("%v", out)
	}
	first := out["passages"].([]any)[0].(map[string]any)
	for k, want := range map[string]any{"language": "ta", "machine": true, "checked_by_person": false, "fallback_used": false, "glossary_version": float64(3)} {
		if first[k] != want {
			t.Errorf("%s: %v", k, first[k])
		}
	}
	if first["label"].(map[string]any)["en"] != "Machine translation through standin-translate, check before issue" || len(first["label"].(map[string]any)) != 3 {
		t.Fatalf("%v", first["label"])
	}
	if first["origins"].(map[string]any)["source"] != float64(2) {
		t.Fatalf("%v", first["origins"])
	}
	src := first["sources"].([]any)
	if len(src) != 1 || src[0].(map[string]any)["name"] != "standin-translate" || src[0].(map[string]any)["version"] != "1" {
		t.Fatalf("%v", src)
	}
	piece := first["pieces"].([]any)[1].(map[string]any)
	if piece["origin"] != "source" || piece["source"] != "standin-translate" || piece["version"] != "1" {
		t.Fatalf("%v", piece)
	}
	empty := out["passages"].([]any)[1].(map[string]any)
	if _, has := empty["label"]; has || empty["machine"] != false || empty["checked_by_person"] != false || len(empty["sources"].([]any)) != 0 {
		t.Fatalf("%v", empty)
	}
}

// What came wholly from people carries no label and is marked as checked (R-TRN-07).
func TestShapeOfAPassageFromPeople(t *testing.T) {
	a := translate.Answer{From: "en", To: "si", GlossaryVersion: 2, Passages: []translate.Passage{
		{Text: "x", Pieces: []translate.PieceInfo{{Origin: translate.OriginGlossary}, {Origin: translate.OriginMemory}}},
		{Text: "y", Pieces: []translate.PieceInfo{{Origin: translate.OriginCache, Source: "a", Version: "1", Fallback: true}}},
	}}
	out := shape("r-1", a)
	people, machine := out.Passages[0], out.Passages[1]
	if people.Label != nil || people.Machine || !people.CheckedByPerson || people.Language != "si" || len(people.Sources) != 0 {
		t.Fatalf("%+v", people)
	}
	if machine.Label == nil || !machine.Machine || machine.CheckedByPerson || !machine.Fallback || !out.Fallback {
		t.Fatalf("%+v", machine)
	}
}
