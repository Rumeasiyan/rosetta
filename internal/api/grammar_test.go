package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/grammar"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
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
		"a wrong way": {request(GrammarPrefix+"other", "rst_here", marker), 400, "invalid request"},
		"a bad key":   {checkRequest("rst_unknown", form(marker, "en")), 401, "no key"},
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

// ---- browser calls (R-CAL-06) -------------------------------------------------

const (
	listedOrigin = "https://writedocument.epc.test"
	otherOrigin  = "https://elsewhere.epc.test"
)

// fakeOrigins is the list as the database would hold it.
type fakeOrigins struct {
	bySite map[string]callers.Caller
	err    error
	used   []int64
}

func (f *fakeOrigins) Find(_ context.Context, origin string) (callers.Caller, error) {
	if f.err != nil {
		return callers.Caller{}, f.err
	}
	c, ok := f.bySite[origin]
	if !ok {
		return c, callers.ErrNoSuchOrigin
	}
	return c, nil
}

func (f *fakeOrigins) Used(_ context.Context, id int64) error {
	f.used = append(f.used, id)
	return nil
}

func withOrigins(t *testing.T, b *bench) *fakeOrigins {
	t.Helper()
	f := &fakeOrigins{bySite: map[string]callers.Caller{
		listedOrigin:               {ID: 21, Reference: "EDITOR-1", TVAStatus: "live", PerMinute: 60, AtOnce: 5},
		"https://retired.epc.test": {ID: 22, Reference: "OLD-1", TVAStatus: "retired", PerMinute: 60, AtOnce: 5},
	}}
	b.srv.deps.Origins = f
	return f
}

// browserCheck is what the plug-in sends, signed in or not.
func browserCheck(origin, account, body string) *http.Request {
	r := checkRequest("", body)
	r.Header.Set("Origin", origin)
	if account != "" {
		r.Header.Set(accountHeader, account)
	}
	return r
}

func wantCORS(t *testing.T, name string, rec *httptest.ResponseRecorder) {
	t.Helper()
	h := rec.Header()
	if h.Get("Access-Control-Allow-Origin") != listedOrigin || h.Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("%s: %v", name, h)
	}
	if !slices.Contains(h.Values("Vary"), "Origin") {
		t.Errorf("%s: no Vary: %v", name, h)
	}
}

func wantNoCORS(t *testing.T, name string, rec *httptest.ResponseRecorder) {
	t.Helper()
	for k := range rec.Header() {
		if strings.HasPrefix(k, "Access-Control-") {
			t.Errorf("%s: %s on an address not listed", name, k)
		}
	}
}

func TestABrowserCallFromAListedAddress(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	origins := withOrigins(t, b)
	var uses []GrammarUse
	b.srv.deps.Usage = func(_ context.Context, u GrammarUse) { uses = append(uses, u) }
	rec := b.send(browserCheck(listedOrigin, "officer-1", form("We recieve "+marker+".", "auto")))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"offset":3`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	wantCORS(t, "answer", rec)
	if !slices.Equal(origins.used, []int64{21}) {
		t.Fatalf("counted to %v", origins.used)
	}
	if len(uses) != 1 || uses[0].System != "EDITOR-1" || uses[0].CallerID != 21 || uses[0].Officer != "officer-1" || uses[0].Outcome != "answered" || uses[0].Characters == 0 || len(uses[0].Sources) != 1 {
		t.Fatalf("%+v", uses)
	}
	if strings.Contains(fmt.Sprintf("%+v", uses), marker) {
		t.Fatal("the hook holds text")
	}
	if len(b.denials) != 0 || strings.Contains(b.logs.String(), marker) || strings.Contains(b.logs.String(), "officer-1") {
		t.Fatalf("%+v %s", b.denials, b.logs)
	}
	// The list of languages comes the same way.
	r := httptest.NewRequest(http.MethodGet, languagesPath, nil)
	r.Header.Set("Origin", listedOrigin)
	r.Header.Set(accountHeader, "officer-1")
	rec = b.send(r)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "English") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	wantCORS(t, "languages", rec)
}

func TestNotSignedInIsRefusedAndReadable(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	origins := withOrigins(t, b)
	for name, account := range map[string]string{"absent": "", "too long": strings.Repeat("a", maxAccount+1), "blank": "   "} {
		b.denials = nil
		rec := b.send(browserCheck(listedOrigin, account, form(marker, "en")))
		if rec.Code != 401 || rec.Header().Get("X-Rosetta-Reason") != "no key" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
			t.Fatalf("%s: %d %v %s", name, rec.Code, rec.Header(), rec.Body)
		}
		if rec.Body.String() != "Please sign in to check grammar, then try again." {
			t.Fatalf("%s: %s", name, rec.Body)
		}
		wantCORS(t, name, rec)
		if len(b.denials) != 1 || b.denials[0].System != "EDITOR-1" || b.denials[0].Why != "Nobody is signed in" {
			t.Fatalf("%s: %+v", name, b.denials)
		}
	}
	if len(origins.used) != 0 {
		t.Fatal("a refused call counted")
	}
}

// ROSETTA's own session counts for nothing here: only the header says who is signed in.
func TestASessionCookieIsNotASignIn(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	withOrigins(t, b)
	r := browserCheck(listedOrigin, "", form("Some text.", "en"))
	r.AddCookie(&http.Cookie{Name: "rosetta_session", Value: "a-session"})
	if rec := b.send(r); rec.Code != 401 {
		t.Fatalf("%d", rec.Code)
	}
}

func TestAnAddressNotListedGetsNoHeadersAndARefusal(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	origins := withOrigins(t, b)
	for name, origin := range map[string]string{
		"other": otherOrigin, "null": "null", "star": "*", "http form": "http://writedocument.epc.test", "with a slash": listedOrigin + "/",
		"longer host": listedOrigin + ".evil.test", "upper case": "https://WRITEDOCUMENT.epc.test", "retired system": "https://retired.epc.test",
	} {
		b.denials = nil
		rec := b.send(browserCheck(origin, "officer-1", form(marker, "en")))
		if rec.Code != 401 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") || strings.Contains(rec.Body.String(), marker) {
			t.Errorf("%s: %d %v %s", name, rec.Code, rec.Header(), rec.Body)
		}
		wantNoCORS(t, name, rec)
		if len(b.denials) != 1 || b.denials[0].Why != "The browser's address is not listed" {
			t.Errorf("%s: %+v", name, b.denials)
		}
	}
	// Even with a good key.
	r := checkRequest("rst_here", form(marker, "en"))
	r.Header.Set("Origin", otherOrigin)
	rec := b.send(r)
	if rec.Code != 401 {
		t.Fatalf("%d", rec.Code)
	}
	wantNoCORS(t, "with a key", rec)
	// No key and no address has no system to count to.
	b.denials = nil
	r = checkRequest("", form(marker, "en"))
	r.Header.Set(accountHeader, "officer-1")
	rec = b.send(r)
	if rec.Code != 401 || len(b.denials) != 1 || b.denials[0].System != "" {
		t.Fatalf("%d %+v", rec.Code, b.denials)
	}
	if len(origins.used) != 0 {
		t.Fatal(origins.used)
	}
}

func TestAPreflight(t *testing.T) {
	b := newBench(t)
	withOrigins(t, b)
	r := httptest.NewRequest(http.MethodOptions, checkPath, nil)
	r.Header.Set("Origin", listedOrigin)
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := b.send(r)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Methods") == "" || rec.Body.Len() != 0 {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	wantCORS(t, "preflight", rec)
	r = httptest.NewRequest(http.MethodOptions, checkPath, nil)
	r.Header.Set("Origin", otherOrigin)
	rec = b.send(r)
	if rec.Code != 401 {
		t.Fatalf("%d", rec.Code)
	}
	wantNoCORS(t, "unlisted preflight", rec)
	if len(b.denials) != 0 {
		t.Fatalf("a preflight is no denial: %+v", b.denials)
	}
}

// A refusal reaches the panel as plain text in the reader's language, English if not known.
func TestARefusalIsInTheReadersLanguage(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	withOrigins(t, b)
	for accept, want := range map[string]string{
		"":               i18n.T(i18n.EN, "grammarNotSignedIn"),
		"fr-FR,fr;q=0.9": i18n.T(i18n.EN, "grammarNotSignedIn"),
		"ta-LK,ta;q=0.9": i18n.T(i18n.TA, "grammarNotSignedIn"),
		"si,en;q=0.5":    i18n.T(i18n.SI, "grammarNotSignedIn"),
		"en-GB,en;q=0.9": i18n.T(i18n.EN, "grammarNotSignedIn"),
	} {
		r := browserCheck(listedOrigin, "", form(marker, "en"))
		if accept != "" {
			r.Header.Set("Accept-Language", accept)
		}
		if rec := b.send(r); rec.Body.String() != want {
			t.Errorf("%q: %s", accept, rec.Body)
		}
	}
	// Each of ROSETTA's own refusals is readable, and none repeats the text.
	for name, c := range map[string]struct {
		body   string
		status int
		reason string
	}{
		"Sinhala":  {form(marker, "si-LK"), 422, "unsupported"},
		"too long": {form(strings.Repeat(marker, 2000), "en"), 413, "too long"},
		"no text":  {"language=en", 400, "invalid request"},
		"bad form": {"text=%zz" + marker, 400, "invalid request"},
	} {
		rec := b.send(browserCheck(listedOrigin, "officer-1", c.body))
		if rec.Code != c.status || rec.Header().Get("X-Rosetta-Reason") != c.reason || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") || strings.Contains(rec.Body.String(), marker) || rec.Body.Len() == 0 {
			t.Errorf("%s: %d %v %s", name, rec.Code, rec.Header(), rec.Body)
		}
		wantCORS(t, name, rec)
	}
}

func TestABrowserCallKeepsTheSystemsLimits(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	f := withOrigins(t, b)
	f.bySite[listedOrigin] = callers.Caller{ID: 31, Reference: "EDITOR-1", TVAStatus: "live", PerMinute: 1, AtOnce: 5}
	if rec := b.send(browserCheck(listedOrigin, "officer-1", form("Some text.", "en"))); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec := b.send(browserCheck(listedOrigin, "officer-1", form("Some text.", "en")))
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" || rec.Header().Get("X-Rosetta-Reason") != "over limit" || len(b.denials) != 0 {
		t.Fatalf("%d %v %+v", rec.Code, rec.Header(), b.denials)
	}
	wantCORS(t, "over limit", rec)
}

func TestAKeyFromAListedAddressGetsTheHeadersAndItsOwnCount(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	origins := withOrigins(t, b)
	r := checkRequest("rst_here", form("Some text.", "en"))
	r.Header.Set("Origin", listedOrigin)
	rec := b.send(r)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	wantCORS(t, "key", rec)
	if len(origins.used) != 0 || b.keys.used != 1 {
		t.Fatalf("%v %d", origins.used, b.keys.used)
	}
	// A key's refusal is the shape of the rest of /api/.
	r = checkRequest("rst_off", form(marker, "en"))
	r.Header.Set("Origin", listedOrigin)
	rec = b.send(r)
	if rec.Code != 401 || reasonOf(t, rec) != "no key" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	wantCORS(t, "key refused", rec)
}

// ROSETTA losing its database is no fault of the address.
func TestAnAddressThatCannotBeCheckedIsNoProvider(t *testing.T) {
	b := newBench(t)
	f := withOrigins(t, b)
	f.err = errors.New("connection lost")
	rec := b.send(browserCheck(listedOrigin, "officer-1", form(marker, "en")))
	if rec.Code != 503 || rec.Header().Get("X-Rosetta-Reason") != "no provider" || len(b.denials) != 0 {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	wantNoCORS(t, "a fault", rec)
}

// With no list at all, no browser gets in.
func TestNoListListsNobody(t *testing.T) {
	b := newBench(t)
	rec := b.send(browserCheck(listedOrigin, "officer-1", form(marker, "en")))
	if rec.Code != 401 {
		t.Fatalf("%d", rec.Code)
	}
	wantNoCORS(t, "no list", rec)
}
