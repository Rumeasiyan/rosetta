package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/grammar"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/standin"
)

// withChecks gives the bench the whole check against BUMBLEBEE's stand-in.
func withChecks(t *testing.T, b *bench) *standin.Server {
	t.Helper()
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	client := bumblebee.Client{URL: ts.URL + "/bumblebee", Key: standin.BumblebeeKey}
	book := &sources.Book{Read: client.Sources, MaxAge: func(context.Context) time.Duration { return time.Minute }}
	if err := book.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.srv.deps.Grammar = &grammar.Service{
		Sources:  &sources.Fallback{Book: book, Provider: client, Grammars: client, Policy: func(context.Context) sources.Policy { return sources.Policy{} }},
		Settings: func(context.Context) settings.Values { return settings.Defaults() },
	}
	return sd
}

// form is the check as the plug-in sends it (docs/plugin-findings.md).
func form(text, language string) string {
	return url.Values{"text": {text}, "language": {language}, "enabledOnly": {"false"}}.Encode()
}

func checkRequest(key, body string) *http.Request {
	r := request(checkPath, key, body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	return r
}

func TestACheckWithAKey(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	rec := b.send(checkRequest("rst_here", form("We recieve "+marker+".", "en-GB")))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	m := out["matches"].([]any)[0].(map[string]any)
	if m["offset"] != float64(3) || m["length"] != float64(7) || m["message"] != "Possible spelling mistake" || m["source"] != "standin-grammar" {
		t.Fatalf("%v", m)
	}
	if out["language"].(map[string]any)["name"] != "English" || out["request_ref"] == "" || out["fallback_used"] != false {
		t.Fatalf("%v", out)
	}
	if strings.Contains(b.logs.String(), marker) {
		t.Fatal(b.logs)
	}
	// Nothing to say is an empty list.
	rec = b.send(checkRequest("rst_here", form("Nothing to say.", "auto")))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"matches":[]`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestCheckRefusals(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	for name, c := range map[string]struct {
		req    *http.Request
		status int
		reason string
	}{
		"no language": {checkRequest("rst_here", url.Values{"text": {marker}}.Encode()), 400, "invalid request"},
		"no text":     {checkRequest("rst_here", url.Values{"language": {"en"}}.Encode()), 400, "invalid request"},
		"not a form":  {request(checkPath, "rst_here", marker), 400, "invalid request"},
		"bad form":    {checkRequest("rst_here", "text=%zz"+marker+"&language=en"), 400, "invalid request"},
		"Sinhala":     {checkRequest("rst_here", form(marker, "si-LK")), 422, "unsupported"},
		"Tamil":       {checkRequest("rst_here", form(marker, "ta")), 422, "unsupported"},
		"too long":    {checkRequest("rst_here", form(strings.Repeat(marker, 2000), "en")), 413, "too long"},
		"no key":      {checkRequest("", form(marker, "en")), 401, "no key"},
		"a wrong way": {httptest.NewRequest(http.MethodPost, GrammarPrefix+"other", nil), 401, "no key"},
	} {
		rec := b.send(c.req)
		if rec.Code != c.status || reasonOf(t, rec) != c.reason {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
		for _, place := range []string{rec.Body.String(), b.logs.String()} {
			if strings.Contains(place, marker) {
				t.Errorf("%s: the text escaped: %s", name, place)
			}
		}
	}
}

func TestCheckFollowsTheKeysRules(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	// A key with a public key signs, as for translation.
	if rec := b.send(checkRequest("rst_signs", form("Some text.", "en"))); rec.Code != 401 {
		t.Fatalf("unsigned: %d", rec.Code)
	}
	body := form("Some text.", "en")
	r := b.signed(checkPath, "rst_signs", body, "a-nonce-of-sixteen-or-more", t0)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := b.send(r); rec.Code != 200 {
		t.Fatalf("signed: %d %s", rec.Code, rec.Body)
	}
	// A key with none is refused from outside, as for translation.
	r = checkRequest("rst_here", form("Some text.", "en"))
	r.Header.Set("Cf-Connecting-Ip", "192.0.2.1")
	if rec := b.send(r); rec.Code != 401 {
		t.Fatalf("from outside: %d", rec.Code)
	}
}

func TestLanguagesAreThePluginsForm(t *testing.T) {
	b := newBench(t)
	r := httptest.NewRequest(http.MethodGet, languagesPath, nil)
	r.Header.Set("Authorization", "Bearer rst_here")
	rec := b.send(r)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0]["name"] != "English" || list[0]["code"] != "en" || list[0]["longCode"] != "en-GB" || list[0]["offered"] != true {
		t.Fatalf("%v", list)
	}
	for _, l := range list[1:] {
		if l["offered"] != false || !strings.Contains(l["name"].(string), "not offered") {
			t.Fatalf("%v", l)
		}
		if f := grammar.CheckLanguage(l["longCode"].(string)); f == nil || f.Reason != "unsupported" {
			t.Fatalf("%v: %v", l, f)
		}
	}
}

// Without a check behind it, a key still gets the fixed answer.
func TestNoCheckerIsNoProvider(t *testing.T) {
	b := newBench(t)
	if rec := b.send(checkRequest("rst_here", form(marker, "en"))); rec.Code != 503 || reasonOf(t, rec) != "no provider" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
