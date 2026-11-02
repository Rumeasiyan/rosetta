package standin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func start(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	s := New(Options{ClientID: "rosetta", ClientSecret: "local-stand-in-only"})
	ts := httptest.NewServer(s.Handler())
	s.SetPublicURL(ts.URL)
	t.Cleanup(func() { s.Close(); ts.Close() })
	return ts, s
}

type answer struct {
	status int
	header http.Header
	body   []byte
}

func (a answer) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(a.body, &m); err != nil {
		t.Fatalf("%s: %v", a.body, err)
	}
	return m
}

func call(t *testing.T, method, u, key, body string) answer {
	t.Helper()
	req, err := http.NewRequest(method, u, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return answer{res.StatusCode, res.Header, b}
}

func setMode(t *testing.T, base, service, mode string, params map[string]string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"mode": mode, "params": params})
	if a := call(t, http.MethodPost, base+"/control/"+service+"/mode", "", string(b)); a.status != 200 {
		t.Fatalf("set %s %s: %d %s", service, mode, a.status, a.body)
	}
}

func TestControlRefusesWhatItDoesNotKnow(t *testing.T) {
	ts, _ := start(t)
	if a := call(t, http.MethodPost, ts.URL+"/control/nobody/mode", "", `{"mode":"normal"}`); a.status != 404 {
		t.Fatalf("unknown service: %d", a.status)
	}
	if a := call(t, http.MethodPost, ts.URL+"/control/bumblebee/mode", "", `{"mode":"nonsense"}`); a.status != 400 {
		t.Fatalf("unknown mode: %d", a.status)
	}
	if a := call(t, http.MethodPost, ts.URL+"/control/bumblebee/mode", "", `not json`); a.status != 400 {
		t.Fatalf("bad body: %d", a.status)
	}
	a := call(t, http.MethodGet, ts.URL+"/control", "", "")
	if got := len(a.json(t)["services"].([]any)); got != 6 {
		t.Fatalf("%d services", got)
	}
}

func TestReadBackHoldsNoKeyOrSecret(t *testing.T) {
	ts, _ := start(t)
	call(t, http.MethodPost, ts.URL+"/pensieve/api/entries", PensieveKey, `{"entries":[]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mjolnir/oauth2/token", strings.NewReader("grant_type=refresh_token&refresh_token=a-secret-token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("rosetta", "local-stand-in-only")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	for _, svc := range []string{"pensieve", "mjolnir"} {
		a := call(t, http.MethodGet, ts.URL+"/control/"+svc+"/received", "", "")
		for _, secret := range []string{PensieveKey, "a-secret-token", "local-stand-in-only"} {
			if bytes.Contains(a.body, []byte(secret)) {
				t.Errorf("%s read-back holds %q: %s", svc, secret, a.body)
			}
		}
		if !bytes.Contains(a.body, []byte(`"received":[{`)) {
			t.Errorf("%s read back nothing: %s", svc, a.body)
		}
	}
}

func TestSilentAndSlowHoldTheRequest(t *testing.T) {
	ts, _ := start(t)
	setMode(t, ts.URL, "bumblebee", "silent", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/bumblebee/v1/sources", nil)
	if _, err := http.DefaultClient.Do(req); err == nil {
		t.Fatal("a silent stand-in answered")
	}
	setMode(t, ts.URL, "bumblebee", "slow", map[string]string{"delay_ms": "120"})
	began := time.Now()
	if a := call(t, http.MethodGet, ts.URL+"/bumblebee/v1/sources", BumblebeeKey, ""); a.status != 200 {
		t.Fatalf("slow: %d", a.status)
	}
	if time.Since(began) < 120*time.Millisecond {
		t.Fatal("slow did not wait")
	}
}

// ---- BUMBLEBEE, as docs/CALLERS.md in its repository ------------------------

func translate(text string) string {
	b, _ := json.Marshal(map[string]string{"request_id": "r-1", "source": "standin-translate", "operation": "translate", "from": "en", "to": "ta", "text": text})
	return string(b)
}

func TestBumblebeeKeyRefusals(t *testing.T) {
	ts, _ := start(t)
	for key, want := range map[string]string{"": "no_key", "bb_nobody": "unknown_key", BumblebeeOffKey: "key_turned_off"} {
		a := call(t, http.MethodPost, ts.URL+"/bumblebee/v1/requests", key, translate("An invented letter."))
		if a.status != 401 || a.json(t)["error"] != want {
			t.Errorf("key %q: %d %s", key, a.status, a.body)
		}
	}
}

func TestBumblebeeAnswersAndEveryFailureReason(t *testing.T) {
	ts, _ := start(t)
	u := ts.URL + "/bumblebee/v1/requests"
	a := call(t, http.MethodPost, u, BumblebeeKey, translate("An invented\nletter."))
	m := a.json(t)
	if a.status != 200 || m["text"] != "[ta] An invented\nletter." || m["version"] != "1" || m["request_id"] != "r-1" {
		t.Fatalf("%d %s", a.status, a.body)
	}
	if _, has := m["took_ms"]; !has {
		t.Fatal("no took_ms")
	}
	reasons := map[string]bool{}
	for reason, status := range reasonStatus {
		reasons[reason] = true
		setMode(t, ts.URL, "bumblebee", reason, nil)
		a := call(t, http.MethodPost, u, BumblebeeKey, translate("An invented letter."))
		f, _ := a.json(t)["failure"].(map[string]any)
		if a.status != status || f["reason"] != reason {
			t.Errorf("%s: %d %s", reason, a.status, a.body)
		}
		if _, has := f["retry_at"]; has != reasonRetry[reason] {
			t.Errorf("%s: retry_at %v", reason, f["retry_at"])
		}
	}
	if len(reasons) != 8 {
		t.Fatalf("%d reasons, CALLERS.md has eight", len(reasons))
	}
}

func TestBumblebeeSourceListLimitsAndStates(t *testing.T) {
	ts, _ := start(t)
	u := ts.URL + "/bumblebee/v1/sources"
	m := call(t, http.MethodGet, u, BumblebeeKey, "").json(t)
	if m["limits"].(map[string]any)["max_text_chars"] != float64(defaultMaxText) {
		t.Fatalf("%v", m)
	}
	first := m["sources"].([]any)[0].(map[string]any)
	if first["state"] != "on" || len(first["offers"].([]any)) != 6 {
		t.Fatalf("%v", first)
	}
	if call(t, http.MethodGet, u, "", "").status != 401 {
		t.Fatal("list without a key")
	}
	for mode, state := range map[string]string{"blocked": "resting-block", "changed": "resting-change", "source-off": "off"} {
		setMode(t, ts.URL, "bumblebee", mode, nil)
		src := call(t, http.MethodGet, u, BumblebeeKey, "").json(t)["sources"].([]any)[0].(map[string]any)
		if src["state"] != state {
			t.Errorf("%s: %v", mode, src)
		}
		if _, has := src["rest_until"]; has != (mode != "source-off") {
			t.Errorf("%s rest_until: %v", mode, src)
		}
	}
	// The limit is read from the list, and a text over it is refused, not cut.
	setMode(t, ts.URL, "bumblebee", "normal", map[string]string{"max_text_chars": "5"})
	if got := call(t, http.MethodGet, u, BumblebeeKey, "").json(t)["limits"].(map[string]any)["max_text_chars"]; got != float64(5) {
		t.Fatalf("limit %v", got)
	}
	// Code points, not bytes: five Tamil code points are 15 bytes and still fit.
	if a := call(t, http.MethodPost, ts.URL+"/bumblebee/v1/requests", BumblebeeKey, translate("தமிழ்")); a.status != 200 {
		t.Fatalf("code points: %d %s", a.status, a.body)
	}
	if a := call(t, http.MethodPost, ts.URL+"/bumblebee/v1/requests", BumblebeeKey, translate("aaaaaa")); a.status != 413 {
		t.Fatalf("over the limit: %d", a.status)
	}
	if a := call(t, http.MethodPost, ts.URL+"/bumblebee/v1/requests", BumblebeeKey, translate("aaaaa")); a.status != 200 {
		t.Fatalf("at the limit: %d", a.status)
	}
	if call(t, http.MethodGet, ts.URL+"/bumblebee/health", "", "").status != 200 {
		t.Fatal("health needs no key")
	}
	setMode(t, ts.URL, "bumblebee", "unavailable", nil)
	if call(t, http.MethodGet, ts.URL+"/bumblebee/health", "", "").status != 503 {
		t.Fatal("health while not ready")
	}
}

func TestBumblebeeLosesMaskingAndChangesTerms(t *testing.T) {
	ts, _ := start(t)
	u := ts.URL + "/bumblebee/v1/requests"
	setMode(t, ts.URL, "bumblebee", "lose-masking", nil)
	if got := call(t, http.MethodPost, u, BumblebeeKey, translate("Card ⟦1⟧ here.")).json(t)["text"]; got != "[ta] Card  here." {
		t.Fatalf("%v", got)
	}
	setMode(t, ts.URL, "bumblebee", "change-term", map[string]string{"from": "Chief Secretary", "to": "Head Clerk"})
	if got := call(t, http.MethodPost, u, BumblebeeKey, translate("The Chief Secretary.")).json(t)["text"]; got != "[ta] The Head Clerk." {
		t.Fatalf("%v", got)
	}
}

func TestBumblebeeRefusesWhatCallersDocumentRefuses(t *testing.T) {
	ts, _ := start(t)
	u := ts.URL + "/bumblebee/v1/requests"
	for name, body := range map[string]string{
		"not json":         `nope`,
		"bad request_id":   `{"request_id":"has a space","source":"standin-translate","operation":"translate","from":"en","to":"ta","text":"x"}`,
		"no such source":   `{"request_id":"r","source":"nobody","operation":"translate","from":"en","to":"ta","text":"x"}`,
		"same languages":   `{"request_id":"r","source":"standin-translate","operation":"translate","from":"en","to":"en","text":"x"}`,
		"no such language": `{"request_id":"r","source":"standin-translate","operation":"translate","from":"en","to":"fr","text":"x"}`,
		"tamil grammar":    `{"request_id":"r","source":"standin-grammar","operation":"grammar","language":"ta","text":"x"}`,
		"wrong source":     `{"request_id":"r","source":"standin-translate","operation":"grammar","language":"en","text":"x"}`,
		"no such op":       `{"request_id":"r","source":"standin-translate","operation":"guess","text":"x"}`,
	} {
		a := call(t, http.MethodPost, u, BumblebeeKey, body)
		if f, _ := a.json(t)["failure"].(map[string]any); a.status != 422 || f["reason"] != "unsupported" {
			t.Errorf("%s: %d %s", name, a.status, a.body)
		}
	}
}

func TestBumblebeeGrammarCountsUTF16(t *testing.T) {
	ts, _ := start(t)
	body, _ := json.Marshal(map[string]string{"request_id": "r", "source": "standin-grammar", "operation": "grammar", "language": "en", "text": "😀 We recieve."})
	a := call(t, http.MethodPost, ts.URL+"/bumblebee/v1/requests", BumblebeeKey, string(body))
	got := a.json(t)["suggestions"].([]any)
	if len(got) != 1 {
		t.Fatalf("%s", a.body)
	}
	one := got[0].(map[string]any)
	if one["start"] != float64(6) || one["length"] != float64(7) || one["category"] != "spelling" {
		t.Fatalf("%v", one)
	}
	body, _ = json.Marshal(map[string]string{"request_id": "r", "source": "standin-grammar", "operation": "grammar", "language": "en", "text": "Nothing to say."})
	a = call(t, http.MethodPost, ts.URL+"/bumblebee/v1/requests", BumblebeeKey, string(body))
	if !bytes.Contains(a.body, []byte(`"suggestions":[]`)) {
		t.Fatalf("an empty list is a list: %s", a.body)
	}
}

// ---- CERBERUS, as docs/JOINING.md in its repository --------------------------

func TestCerberusAllowsRefusesOrIsSilent(t *testing.T) {
	ts, _ := start(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	check := func(method string, origin string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/cerberus/check", nil)
		req.Header.Set("X-Forwarded-Method", method)
		req.Header.Set("X-Forwarded-Uri", "/people")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	res := check("GET", "")
	if res.StatusCode != 200 || res.Header.Get("X-Cerberus-Account") != "dev-admin" || res.Header.Get("X-Cerberus-Name") != "Test%20Administrator" {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	setMode(t, ts.URL, "cerberus", "refuse", map[string]string{"registered_origin": "https://writedocument.epc.test"})
	res = check("POST", "https://writedocument.epc.test")
	if res.StatusCode != 403 || res.Header.Get("Access-Control-Allow-Origin") != "https://writedocument.epc.test" || res.Header.Get("X-Cerberus-Account") != "" {
		t.Fatalf("refusal: %d %v", res.StatusCode, res.Header)
	}
	if res = check("POST", "https://elsewhere.epc.test"); res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("CORS named an address not registered: %v", res.Header)
	}
	setMode(t, ts.URL, "cerberus", "signed-out", nil)
	if res = check("GET", ""); res.StatusCode != 303 {
		t.Fatalf("page: %d", res.StatusCode)
	}
	if res = check("POST", ""); res.StatusCode != 401 || res.Header.Get("HX-Refresh") != "true" {
		t.Fatalf("form: %d %v", res.StatusCode, res.Header)
	}
}

// ---- BIFROST, as docs/EXCHANGE.md in its repository --------------------------

func TestBifrostEveryAnswer(t *testing.T) {
	ts, _ := start(t)
	u := ts.URL + "/bifrost/api/facts/scroll.staff-lookup/lookup"
	body := `{"email":"dev-admin@example.invalid"}`
	m := call(t, http.MethodPost, u, BifrostKey, body).json(t)
	if m["found"] != true || len(m["offices"].([]any)) != 1 {
		t.Fatalf("%v", m)
	}
	setMode(t, ts.URL, "bifrost", "found-no-offices", nil)
	m = call(t, http.MethodPost, u, BifrostKey, body).json(t)
	if m["found"] != true || len(m["offices"].([]any)) != 0 {
		t.Fatalf("%v", m)
	}
	setMode(t, ts.URL, "bifrost", "not-found", nil)
	if m = call(t, http.MethodPost, u, BifrostKey, body).json(t); m["found"] != false {
		t.Fatalf("%v", m)
	}
	for code, ref := range bifrostRefusals {
		setMode(t, ts.URL, "bifrost", code, nil)
		a := call(t, http.MethodPost, u, BifrostKey, body)
		if a.status != ref.status || a.json(t)["refusal"] != code {
			t.Errorf("%s: %d %s", code, a.status, a.body)
		}
		if code == "too_many" && a.header.Get("Retry-After") == "" {
			t.Error("too_many has no Retry-After")
		}
	}
	setMode(t, ts.URL, "bifrost", "found", nil)
	if a := call(t, http.MethodPost, u, "", body); a.status != 401 || a.json(t)["refusal"] != "no_key" {
		t.Fatalf("no key: %d %s", a.status, a.body)
	}
	if a := call(t, http.MethodPost, u, BifrostWithdrawnKey, body); a.status != 401 || a.json(t)["refusal"] != "key_withdrawn" {
		t.Fatalf("withdrawn: %d %s", a.status, a.body)
	}
	if a := call(t, http.MethodPost, ts.URL+"/bifrost/api/facts/other.fact/lookup", BifrostKey, body); a.status != 403 {
		t.Fatalf("another fact: %d", a.status)
	}
	if a := call(t, http.MethodPost, u, BifrostKey, `{"email":" "}`); a.status != 400 {
		t.Fatalf("blank email: %d", a.status)
	}
}

// ---- PENSIEVE, as docs/INTAKE.md in its repository ---------------------------

func entry(change map[string]any) map[string]any {
	e := map[string]any{"id": "rosetta-1", "occurred_at": time.Now().UTC().Format(time.RFC3339), "actor": "dev-admin",
		"post": "not known", "action": "role change", "object_type": "person", "object_ref": "dev-keeper", "result": "done"}
	for k, v := range change {
		if v == nil {
			delete(e, k)
		} else {
			e[k] = v
		}
	}
	return e
}

func send(t *testing.T, ts *httptest.Server, key string, entries ...map[string]any) answer {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"entries": entries})
	return call(t, http.MethodPost, ts.URL+"/pensieve/api/entries", key, string(b))
}

func TestPensieveKeepsOnceAndRefusesByItem(t *testing.T) {
	ts, _ := start(t)
	a := send(t, ts, PensieveKey, entry(nil))
	r := a.json(t)["results"].([]any)[0].(map[string]any)
	if a.status != 200 || r["status"] != "kept" || r["seq"] != float64(1) {
		t.Fatalf("%d %s", a.status, a.body)
	}
	r = send(t, ts, PensieveKey, entry(nil)).json(t)["results"].([]any)[0].(map[string]any)
	if r["repeat"] != true || r["seq"] != float64(1) {
		t.Fatalf("repeat: %v", r)
	}
	old := time.Now().Add(-31 * 24 * time.Hour).UTC().Format(time.RFC3339)
	ahead := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for name, c := range map[string]struct {
		change       map[string]any
		reason, item string
	}{
		"blank post":      {map[string]any{"post": ""}, "missing", "post"},
		"spaces for post": {map[string]any{"post": "  "}, "missing", "post"},
		"no post":         {map[string]any{"post": nil}, "missing", "post"},
		"no actor":        {map[string]any{"actor": nil}, "missing", "actor"},
		"action off list": {map[string]any{"action": "guess"}, "not_on_list", "action"},
		"result off list": {map[string]any{"result": "maybe"}, "not_on_list", "result"},
		"unknown item":    {map[string]any{"colour": "red"}, "unknown", "colour"},
		"long reason":     {map[string]any{"reason": strings.Repeat("x", 1001)}, "too_long", "reason"},
		"bad time":        {map[string]any{"occurred_at": "soon"}, "invalid", "occurred_at"},
		"too old":         {map[string]any{"occurred_at": old}, "too_old", "occurred_at"},
		"ahead":           {map[string]any{"occurred_at": ahead}, "in_future", "occurred_at"},
		"name a key":      {map[string]any{"details": map[string]any{"api_key": "x"}}, "secret", "details"},
		"value a key":     {map[string]any{"details": map[string]any{"a": []any{"Bearer abc"}}}, "secret", "details"},
		"name a card":     {map[string]any{"details": map[string]any{"NIC": "x"}}, "id_number", "details"},
		"old card":        {map[string]any{"details": map[string]any{"a": "123456789V"}}, "id_number", "details"},
		"new card":        {map[string]any{"details": map[string]any{"b": map[string]any{"c": "200012345678"}}}, "id_number", "details"},
		"details as text": {map[string]any{"details": "x"}, "invalid", "details"},
		"name elsewhere":  {map[string]any{"action_name": "x"}, "invalid", "action_name"},
	} {
		a := send(t, ts, PensieveKey, entry(c.change))
		r := a.json(t)["results"].([]any)[0].(map[string]any)
		if a.status != 422 || r["reason"] != c.reason || r["item"] != c.item {
			t.Errorf("%s: %d %s", name, a.status, a.body)
		}
	}
	mix := send(t, ts, PensieveKey, entry(map[string]any{"id": "rosetta-2"}), entry(map[string]any{"post": ""}))
	if mix.status != 207 {
		t.Fatalf("mixed: %d", mix.status)
	}
}

func TestPensieveRefusesWholeRequests(t *testing.T) {
	ts, _ := start(t)
	for key, want := range map[string]string{"": "no_key", "pv_nobody": "unknown_key", PensieveOffKey: "key_turned_off"} {
		if a := send(t, ts, key, entry(nil)); a.status != 401 || a.json(t)["error"] != want {
			t.Errorf("key %q: %d %s", key, a.status, a.body)
		}
	}
	if a := call(t, http.MethodPost, ts.URL+"/pensieve/api/entries", PensieveKey, `{"nothing":1}`); a.status != 400 {
		t.Fatalf("unreadable: %d", a.status)
	}
	setMode(t, ts.URL, "pensieve", "normal", map[string]string{"limit": "1"})
	if a := send(t, ts, PensieveKey, entry(nil), entry(nil)); a.status != 413 || a.json(t)["limit"] != float64(1) {
		t.Fatalf("batch: %d %s", a.status, a.body)
	}
	setMode(t, ts.URL, "pensieve", "unavailable", nil)
	if a := send(t, ts, PensieveKey, entry(nil)); a.status != 503 {
		t.Fatalf("unavailable: %d", a.status)
	}
	setMode(t, ts.URL, "pensieve", "too_many", map[string]string{"retry_seconds": "7"})
	if a := send(t, ts, PensieveKey, entry(nil)); a.status != 429 || a.header.Get("Retry-After") != "7" {
		t.Fatalf("too many: %d %v", a.status, a.header)
	}
	setMode(t, ts.URL, "pensieve", "not_kept", nil)
	if a := send(t, ts, PensieveKey, entry(nil)); a.status != 422 || !bytes.Contains(a.body, []byte("not_kept")) {
		t.Fatalf("not kept: %d %s", a.status, a.body)
	}
}

// ---- HEDWIG, as docs/API.md in its repository --------------------------------

func TestHedwigQueuesOnceAndRefuses(t *testing.T) {
	ts, _ := start(t)
	u := ts.URL + "/hedwig/v1/notices"
	body := `{"notice_type":"flag","request_key":"k-1","details":{"what":"x"},"people":[{"ref":"p1","email":"dev-admin@example.invalid","language":"ta"}]}`
	a := call(t, http.MethodPost, u, HedwigLiveKey, body)
	if a.status != 202 || !bytes.Contains(a.body, []byte(`"status":"queued"`)) {
		t.Fatalf("%d %s", a.status, a.body)
	}
	if again := call(t, http.MethodPost, u, HedwigLiveKey, body); !bytes.Equal(again.body, a.body) {
		t.Fatalf("a repeat is the first answer: %s", again.body)
	}
	if a := call(t, http.MethodPost, u, HedwigTestKey, body); !bytes.Contains(a.body, []byte(`"status":"test"`)) {
		t.Fatalf("test key: %s", a.body)
	}
	if a := call(t, http.MethodGet, u+"/1", HedwigLiveKey, ""); a.status != 200 || !bytes.Contains(a.body, []byte(`"notice_type":"flag"`)) {
		t.Fatalf("read: %d %s", a.status, a.body)
	}
	if a := call(t, http.MethodGet, u+"/99", HedwigLiveKey, ""); a.status != 404 {
		t.Fatalf("read unknown: %d", a.status)
	}
	if a := call(t, http.MethodPost, u, "hw_nobody", body); a.status != 401 {
		t.Fatalf("key: %d", a.status)
	}
	for name, bad := range map[string]string{
		"misspelt": `{"notice_typ":"flag","people":[{"ref":"p","email":"a@example.invalid"}]}`,
		"nobody":   `{"notice_type":"flag","people":[]}`,
		"no ref":   `{"notice_type":"flag","people":[{"email":"a@example.invalid"}]}`,
	} {
		if a := call(t, http.MethodPost, u, HedwigLiveKey, bad); a.status != 400 {
			t.Errorf("%s: %d %s", name, a.status, a.body)
		}
	}
	if a := call(t, http.MethodPost, u, HedwigLiveKey, `{"notice_type":"flag","people":[{"ref":"p"}]}`); a.status != 422 {
		t.Fatalf("no address: %d", a.status)
	}
	for mode, status := range map[string]int{"unauthorized": 401, "unknown_notice_type": 404, "missing_detail": 422, "rate_limited": 429, "internal": 500} {
		setMode(t, ts.URL, "hedwig", mode, nil)
		if a := call(t, http.MethodPost, u, HedwigLiveKey, body); a.status != status {
			t.Errorf("%s: %d", mode, a.status)
		}
	}
}

// ---- MJOLNIR, as docs/SESSION-INTERFACE.md in its repository -----------------

func tokenCall(t *testing.T, ts *httptest.Server, form url.Values, secret string) answer {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mjolnir/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if secret != "" {
		req.SetBasicAuth("rosetta", secret)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return answer{res.StatusCode, res.Header, b}
}

// signIn walks the form with no browser, and returns the refresh token.
func signIn(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	form := url.Values{"client_id": {"rosetta"}, "redirect_uri": {"http://localhost/cb"}, "state": {"s"}, "nonce": {"n"},
		"scope": {"openid offline_access"}, "code_challenge": {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"}, "account": {"dev-admin"}}
	res, err := client.PostForm(ts.URL+"/mjolnir/oauth2/auth", form)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	loc, _ := url.Parse(res.Header.Get("Location"))
	a := tokenCall(t, ts, url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")},
		"redirect_uri": {"http://localhost/cb"}, "code_verifier": {"dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"}}, "local-stand-in-only")
	if a.status != 200 {
		t.Fatalf("%d %s", a.status, a.body)
	}
	m := a.json(t)
	if m["id_token"] == "" || m["refresh_token"] == nil {
		t.Fatalf("%v", m)
	}
	return m["refresh_token"].(string)
}

func TestMjolnirDiscoveryAndNoPromptNone(t *testing.T) {
	ts, _ := start(t)
	m := call(t, http.MethodGet, ts.URL+"/mjolnir/.well-known/openid-configuration", "", "").json(t)
	if m["issuer"] != ts.URL+"/mjolnir" || m["revocation_endpoint"] != ts.URL+"/mjolnir/oauth2/revoke" {
		t.Fatalf("%v", m)
	}
	if a := call(t, http.MethodGet, ts.URL+"/mjolnir/.well-known/jwks.json", "", ""); a.status != 200 || !bytes.Contains(a.body, []byte(`"crv":"P-256"`)) {
		t.Fatalf("%d %s", a.status, a.body)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	q := url.Values{"client_id": {"rosetta"}, "redirect_uri": {"http://localhost/cb"}, "response_type": {"code"}, "state": {"s"},
		"code_challenge": {"x"}, "code_challenge_method": {"S256"}, "prompt": {"none"}}
	res, err := client.Get(ts.URL + "/mjolnir/oauth2/auth?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if !strings.Contains(res.Header.Get("Location"), "error=login_required") {
		t.Fatalf("prompt=none: %v", res.Header)
	}
}

func TestMjolnirRotatesAndRefusesAsMeasured(t *testing.T) {
	ts, _ := start(t)
	first := signIn(t, ts)
	renew := func(tok string) answer {
		return tokenCall(t, ts, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok}}, "local-stand-in-only")
	}
	if a := tokenCall(t, ts, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first}, "client_secret": {"local-stand-in-only"}}, ""); a.status != 401 {
		t.Fatalf("secret in the form: %d", a.status)
	}
	a := renew(first)
	if a.status != 200 {
		t.Fatalf("%d %s", a.status, a.body)
	}
	second := a.json(t)["refresh_token"].(string)
	if second == first {
		t.Fatal("no rotation")
	}
	if a := renew(first); a.status != 400 || a.json(t)["error"] != "invalid_grant" {
		t.Fatalf("reuse: %d %s", a.status, a.body)
	}
	if a := renew(second); a.status != 400 {
		t.Fatalf("reuse must kill the newest in the chain: %d", a.status)
	}

	third := signIn(t, ts)
	for mode, want := range map[string]struct {
		status int
		err    string
	}{
		"renew-invalid-grant": {400, "invalid_grant"},
		"renew-access-denied": {403, "access_denied"},
		"renew-server-error":  {500, "server_error"},
	} {
		setMode(t, ts.URL, "mjolnir", mode, nil)
		if a := renew(third); a.status != want.status || a.json(t)["error"] != want.err {
			t.Errorf("%s: %d %s", mode, a.status, a.body)
		}
	}
	setMode(t, ts.URL, "mjolnir", "normal", nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mjolnir/oauth2/revoke", strings.NewReader(url.Values{"token": {third}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("rosetta", "local-stand-in-only")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if a := renew(third); a.status != 400 || a.json(t)["error"] != "invalid_grant" {
		t.Fatalf("after revoke: %d %s", a.status, a.body)
	}
}

// ---- TVA, as docs/LOOKUP-FORMAT.md in its repository -------------------------

func TestTVAFolderIsInTheFileFormat(t *testing.T) {
	raw, err := os.ReadFile("../../dev/tva/systems.json")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(raw, []byte("\n")) != 1 || !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatal("the file is one line ending in a newline")
	}
	var f struct {
		Format  int              `json:"format"`
		Systems []map[string]any `json:"systems"`
	}
	if err := json.Unmarshal(raw, &f); err != nil || f.Format != 1 {
		t.Fatalf("%v %d", err, f.Format)
	}
	fields := []string{"reference", "name_si", "name_ta", "name_en", "status", "institution_reference", "institution_si", "institution_ta",
		"institution_en", "web_address", "owner_post_reference", "owner_post_si", "owner_post_ta", "owner_post_en", "owner_subject"}
	statuses := map[string]bool{}
	nullOwner, prev := false, ""
	for _, s := range f.Systems {
		if len(s) != len(fields) {
			t.Errorf("%v has %d fields", s["reference"], len(s))
		}
		for _, name := range fields {
			if _, has := s[name]; !has {
				t.Errorf("%v has no %s", s["reference"], name)
			}
		}
		statuses[s["status"].(string)] = true
		nullOwner = nullOwner || s["owner_subject"] == nil
		if ref := s["reference"].(string); ref <= prev {
			t.Errorf("not sorted at %s", ref)
		} else {
			prev = ref
		}
	}
	for _, want := range []string{"planned", "in_development", "may_join", "live", "retired"} {
		if !statuses[want] {
			t.Errorf("no system with status %s", want)
		}
	}
	if !nullOwner {
		t.Error("no system with a null owner subject")
	}
}

// ---- Never in the image ------------------------------------------------------

func TestStandInsAreNotInTheService(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "../../cmd/rosetta").Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	if bytes.Contains(out, []byte("internal/standin")) {
		t.Fatal("the service's binary depends on the stand-ins")
	}
}

// Signing out at MJOLNIR's stand-in ends the sign-in on that browser only: the next
// sign-in shows the form.
func TestMjolnirSignOutEndsTheSignIn(t *testing.T) {
	ts, _ := start(t)
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	q := url.Values{"client_id": {"rosetta"}, "redirect_uri": {"http://localhost/back"}, "response_type": {"code"}, "state": {"s"}, "code_challenge": {"c"}, "code_challenge_method": {"S256"}}
	auth := func() *http.Response {
		res, err := browser.Get(ts.URL + "/mjolnir/oauth2/auth?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	if res := auth(); res.StatusCode != 200 {
		t.Fatalf("no form at first: %d", res.StatusCode)
	}
	form := url.Values{"client_id": {"rosetta"}, "redirect_uri": {"http://localhost/back"}, "state": {"s"}, "code_challenge": {"c"}, "own": {"dev-admin"}}
	res, err := browser.PostForm(ts.URL+"/mjolnir/oauth2/auth", form)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res := auth(); res.StatusCode != 303 {
		t.Fatalf("signed in, it should pass with no form: %d", res.StatusCode)
	}
	res, err = browser.Get(ts.URL + "/mjolnir/sign-out")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(page), "signed out") || strings.Contains(string(page), "href") {
		t.Fatalf("%d %s", res.StatusCode, page)
	}
	if res := auth(); res.StatusCode != 200 {
		t.Fatalf("still signed in after signing out: %d", res.StatusCode)
	}
}
