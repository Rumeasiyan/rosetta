package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/Rumeasiyan/rosetta/internal/api"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/grammar"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/memory"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/standin"
	"github.com/Rumeasiyan/rosetta/internal/terms"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/Rumeasiyan/rosetta/internal/usage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pathWorld is the whole path against the database and the stand-in, glossary and all.
type pathWorld struct {
	t      *testing.T
	pool   *pgxpool.Pool
	mux    *http.ServeMux
	stand  *httptest.Server
	logs   *bytes.Buffer
	key    string
	person int64
}

func newPathWorld(t *testing.T) *pathWorld {
	t.Helper()
	pool := dbtest.Open(t)
	ctx := context.Background()
	id := freshSystem(t, pool)
	for _, sql := range []string{`DELETE FROM alert`, `DELETE FROM cache_piece`, `DELETE FROM source_order`, `DELETE FROM gate_decision`, `DELETE FROM source_offer`, `DELETE FROM source`,
		`DELETE FROM memory_entry`, `DELETE FROM glossary_term`, `DELETE FROM glossary_entry`, `DELETE FROM glossary_import`, `DELETE FROM glossary_version WHERE number > 0`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM alert`, `DELETE FROM cache_piece`, `DELETE FROM gate_decision`, `DELETE FROM source_offer`, `DELETE FROM source`,
			`DELETE FROM memory_entry`, `DELETE FROM glossary_term`, `DELETE FROM glossary_entry`, `DELETE FROM glossary_version WHERE number > 0`} {
			pool.Exec(context.Background(), sql)
		}
	})
	w := &pathWorld{t: t, pool: pool, logs: &bytes.Buffer{}}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		w.key, _, err = issueByTestPerson(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO person (mjolnir_subject, name, email) VALUES ($1, 'Invented', 'g@example.invalid') RETURNING id`, fmt.Sprintf("glossary-path-%d", time.Now().UnixNano())).Scan(&w.person); err != nil {
		t.Fatal(err)
	}
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	w.stand = httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); w.stand.Close() })
	logger := slog.New(slog.NewJSONHandler(w.logs, nil))
	values := func(context.Context) settings.Values { return settings.Defaults() }
	client := bumblebee.Client{URL: w.stand.URL + "/bumblebee", Key: standin.BumblebeeKey}
	book := &sources.Book{Read: client.Sources, Logger: logger, MaxAge: func(context.Context) time.Duration { return time.Minute },
		Kept: func(ctx context.Context, l bumblebee.List, at time.Time) error {
			return sources.Mirror(ctx, pool, l, at)
		}}
	if err := book.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	policy := func(ctx context.Context) sources.Policy {
		p, err := sources.LoadPolicy(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	cache := translate.Cache{Q: pool, Settings: values}
	published := &glossary.Book{Q: pool}
	fallback := &sources.Fallback{Book: book, Provider: client, Grammars: client, Policy: policy, Logger: logger}
	calls := api.New(api.Deps{
		Keys: api.PoolKeys{Pool: pool}, Denied: api.PoolDenials(pool, nil), Logger: logger, Snapshot: book.Snapshot, Policy: policy, Origins: api.PoolOrigins{Pool: pool},
		Translator: &translate.Service{Glossary: published, Memory: memory.Served{Q: pool}, Cache: cache, Keeper: cache, Sources: fallback, Settings: values, Logger: logger},
		Memory:     &memory.Service{Pool: pool, Settings: values, Logger: logger},
		Usage:      func(ctx context.Context, r usage.Record) error { return usage.Keep(ctx, pool, r) },
		Grammar: &grammar.Service{Sources: fallback, Settings: values, Held: &grammar.Held{}, Logger: logger, Glossary: func(ctx context.Context) (*terms.Set, error) {
			_, set, err := published.Terms(ctx, "en", "en")
			return set, err
		}},
	})
	w.mux = http.NewServeMux()
	calls.Mount(w.mux)
	return w
}

func (w *pathWorld) mode(name string) {
	w.t.Helper()
	res, err := http.Post(w.stand.URL+"/control/bumblebee/mode", "application/json", strings.NewReader(`{"mode":"`+name+`"}`))
	if err != nil {
		w.t.Fatal(err)
	}
	res.Body.Close()
}

func (w *pathWorld) received() string {
	w.t.Helper()
	res, err := http.Get(w.stand.URL + "/control/bumblebee/received")
	if err != nil {
		w.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func (w *pathWorld) open(from, to string) {
	w.t.Helper()
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		_, err := sources.OpenGate(ctx, tx, "standin-translate", sources.Translate(from, to), sources.Evaluation{TestSet: "shared-1", Score: 80, EvaluatedOn: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}, w.person)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *pathWorld) publish(kind, en, si, ta string) int64 {
	w.t.Helper()
	ctx := context.Background()
	var id int64
	if err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		var err error
		var langs []i18n.Lang
		id, langs, err = glossary.Propose(ctx, tx, glossary.Proposal{Type: kind, By: w.person, Texts: map[i18n.Lang]string{i18n.EN: en, i18n.SI: si, i18n.TA: ta}})
		if err != nil {
			return err
		}
		for _, l := range langs {
			if _, err := glossary.Check(ctx, tx, id, l, "", w.person); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *pathWorld) translate(sensitivity string, passages ...string) (int, map[string]any) {
	w.t.Helper()
	raw, _ := json.Marshal(map[string]any{"from": "en", "to": "ta", "sensitivity": sensitivity, "passages": passages})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/translate", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+w.key)
	rec := httptest.NewRecorder()
	w.mux.ServeHTTP(rec, r)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		w.t.Fatal(rec.Body.String())
	}
	return rec.Code, out
}

func passageOf(out map[string]any, i int) map[string]any {
	return out["passages"].([]any)[i].(map[string]any)
}

// The exact path E-GLO-04 exists for: every source echoes, so a lone system name failed under
// R-TRN-09. Published as "keep as written", the same request is answered, from the glossary, with
// nothing sent, under a closed gate, marked as a person's work.
func TestKeepAsWrittenAnswersWhatAnEchoFailed(t *testing.T) {
	w := newPathWorld(t)
	w.open("en", "ta")
	w.mode("echo")
	if code, out := w.translate("ordinary", "SCOS Pro"); code != 503 || !strings.Contains(jsonText(out), "no provider") {
		t.Fatalf("before the entry: %d %v", code, out)
	}
	sent := strings.Count(w.received(), "/v1/requests")
	if sent == 0 {
		t.Fatal("the failure did not come from a source")
	}
	w.publish(glossary.TypeKeep, "SCOS Pro", "", "")
	code, out := w.translate("ordinary", "SCOS Pro")
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	p := passageOf(out, 0)
	if p["text"] != "SCOS Pro" || p["machine"] != false || p["checked_by_person"] != true || p["label"] != nil || p["origins"].(map[string]any)["glossary"] != float64(1) {
		t.Fatalf("%v", p)
	}
	if out["glossary_version"] != float64(1) || p["glossary_version"] != float64(1) || strings.Count(w.received(), "/v1/requests") != sent {
		t.Fatalf("%v, %d calls", out, strings.Count(w.received(), "/v1/requests"))
	}
	// The gate closed for every source, and BUMBLEBEE gone: it still answers (R-GAT-02, R-NFR-09).
	if _, err := w.pool.Exec(context.Background(), `DELETE FROM gate_decision`); err != nil {
		t.Fatal(err)
	}
	w.mode("unavailable")
	if code, out := w.translate("personal", "SCOS Pro"); code != 200 || passageOf(out, 0)["text"] != "SCOS Pro" {
		t.Fatalf("%d %v", code, out)
	}
	// Echoing a sentence that holds it is still a failure: only the term is the same in any language.
	w.open("en", "ta")
	w.mode("echo")
	if code, out := w.translate("ordinary", "SCOS Pro is closed today."); code != 503 {
		t.Fatalf("%d %v", code, out)
	}
	// All or nothing holds: the sentence's failure takes the name with it (R-TRN-02).
	if code, out := w.translate("ordinary", "SCOS Pro", "A sentence nobody will translate."); code != 503 || out["passages"] != nil {
		t.Fatalf("%d %v", code, out)
	}
}

// R-GLO-03 through the whole path: nothing of a term reaches a source; each comes back as published;
// the cache holds the stand-in; a passage from the glossary alone carries no label.
func TestTermsThroughTheWholePath(t *testing.T) {
	w := newPathWorld(t)
	w.publish(glossary.TypePost, "Chief Secretary", "ප්‍රධාන ලේකම්", "பிரதம செயலாளர்")
	w.publish(glossary.TypeKeep, "SCOS Pro", "", "")
	// Closed: the terms go back to help the translator, and nothing has gone anywhere.
	code, refused := w.translate("ordinary", "The Chief Secretary uses SCOS Pro.")
	if code != 409 || !strings.Contains(jsonText(refused), "பிரதம செயலாளர்") || strings.Contains(w.received(), "/v1/requests") {
		t.Fatalf("%d %v", code, refused)
	}
	w.open("en", "ta")
	code, out := w.translate("ordinary", "The Chief Secretary uses SCOS Pro.", "SCOS Pro", "\n Chief Secretary. \n")
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if p := passageOf(out, 0); p["text"] != "[ta] The பிரதம செயலாளர் uses SCOS Pro." || p["machine"] != true || p["label"] == nil {
		t.Fatalf("%v", p)
	}
	if p := passageOf(out, 1); p["checked_by_person"] != true || p["label"] != nil {
		t.Fatalf("%v", p)
	}
	if p := passageOf(out, 2); p["text"] != "\n பிரதம செயலாளர். \n" || p["checked_by_person"] != true {
		t.Fatalf("%q", p["text"])
	}
	got := w.received()
	if strings.Contains(got, "Chief Secretary") || strings.Contains(got, "SCOS Pro") || !strings.Contains(got, "⟦G1⟧") {
		t.Fatal(got)
	}
	var withTerm int
	if err := w.pool.QueryRow(context.Background(), `SELECT count(*) FROM cache_piece WHERE source_text LIKE '%Chief%' OR translation LIKE '%செயலாளர்%' OR source_text LIKE '%SCOS%'`).Scan(&withTerm); err != nil || withTerm != 0 {
		t.Fatalf("%d %v", withTerm, err)
	}
	// Caller text reaches no log and no outbox entry.
	const marker = "Glossarypathmarker"
	if code, out := w.translate("ordinary", marker+" of the Chief Secretary."); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if strings.Contains(w.logs.String(), marker) || strings.Contains(w.logs.String(), "Chief Secretary") {
		t.Fatal(w.logs.String())
	}
	var n int
	if err := w.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_outbox WHERE details::text LIKE '%' || $1 || '%' OR details::text LIKE '%Chief Secretary%'`, marker).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
}

func (w *pathWorld) check(text string) (int, map[string]any) {
	w.t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v2/check", strings.NewReader(url.Values{"language": {"en-GB"}, "text": {text}}.Encode()))
	r.Header.Set("Authorization", "Bearer "+w.key)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	w.mux.ServeHTTP(rec, r)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		w.t.Fatal(rec.Body.String())
	}
	return rec.Code, out
}

// matchesAt is each match's place in the text, as the plug-in reads it.
func matchesAt(out map[string]any) [][2]int {
	var at [][2]int
	for _, m := range out["matches"].([]any) {
		x := m.(map[string]any)
		at = append(at, [2]int{int(x["offset"].(float64)), int(x["length"].(float64))})
	}
	return at
}

// R-GRM-04 through the whole path: a term keepers published is never marked, what surrounds it still
// is, positions stay true in the plug-in's units, and an answer held from before the term was published
// is not served with the suggestion on it. No text is kept.
func TestNoSuggestionOnAGlossaryTerm(t *testing.T) {
	w := newPathWorld(t)
	text := "𝒜 We recieve from the Recieve Office, 0771234567, and recieve more."
	code, before := w.check(text)
	if code != 200 {
		t.Fatalf("%d %v", code, before)
	}
	if len(matchesAt(before)) != 2 {
		t.Fatalf("%v", before)
	}
	w.publish(glossary.TypeInstitution, "We recieve", "අපි", "நாம்")
	sent := strings.Count(w.received(), "/v1/requests")
	code, after := w.check(text)
	if code != 200 {
		t.Fatalf("%d %v", code, after)
	}
	got := matchesAt(after)
	if len(got) != 1 || got[0] != matchesAt(before)[1] {
		t.Fatalf("held from before the term: %v, was %v", got, matchesAt(before))
	}
	if strings.Count(w.received(), "/v1/requests") != sent {
		t.Fatal("the held answer was not used")
	}
	// Another text, new to the sources, after a character of two units and a masked number.
	second := "𝒜 0771234567 We recieve, and recieve."
	code, out := w.check(second)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	got = matchesAt(out)
	want := len(utf16.Encode([]rune("𝒜 0771234567 We recieve, and ")))
	if len(got) != 1 || got[0][0] != want || got[0][1] != len("recieve") {
		t.Fatalf("%v, want at %d", got, want)
	}
	if ctx := out["matches"].([]any)[0].(map[string]any)["context"].(map[string]any); ctx["offset"] == nil {
		t.Fatalf("%v", ctx)
	}
	if strings.Contains(w.logs.String(), "recieve") {
		t.Fatal(w.logs.String())
	}
	var n int
	for _, sql := range []string{`SELECT count(*) FROM audit_outbox WHERE details::text LIKE '%recieve%'`, `SELECT count(*) FROM cache_piece WHERE source_text LIKE '%recieve%'`, `SELECT count(*) FROM memory_entry WHERE source_text LIKE '%recieve%'`} {
		if err := w.pool.QueryRow(context.Background(), sql).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d %v", sql, n, err)
		}
	}
}
