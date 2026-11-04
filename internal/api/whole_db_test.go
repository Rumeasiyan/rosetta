package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/api"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/standin"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/jackc/pgx/v5"
)

// The release's goal, with everything real but BUMBLEBEE: a registered system with its
// key translates a list through masking, the cache and the stand-in with fallback, and
// gets every passage back or one error. Afterwards a marker in the text is looked for in
// the logs, the outbox and every table that could hold it.
func TestTheWholePathAgainstTheDatabase(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	id := freshSystem(t, pool)
	for _, sql := range []string{`DELETE FROM cache_piece`, `DELETE FROM source_order`, `DELETE FROM gate_decision`, `DELETE FROM source_offer`, `DELETE FROM source`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM cache_piece`, `DELETE FROM gate_decision`, `DELETE FROM source_offer`, `DELETE FROM source`} {
			pool.Exec(context.Background(), sql)
		}
	})
	var key string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		key, _, err = issueByTestPerson(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	stand := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); stand.Close() })
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	values := func(context.Context) settings.Values { return settings.Defaults() }
	client := bumblebee.Client{URL: stand.URL + "/bumblebee", Key: standin.BumblebeeKey}
	book := &sources.Book{Read: client.Sources, Logger: logger,
		MaxAge: func(context.Context) time.Duration { return time.Minute },
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
	calls := api.New(api.Deps{
		Keys: api.PoolKeys{Pool: pool}, Denied: api.PoolDenials(pool, nil), Logger: logger, Snapshot: book.Snapshot, Policy: policy,
		Translator: &translate.Service{Memory: translate.None{}, Cache: cache, Keeper: cache,
			Sources:  &sources.Fallback{Book: book, Provider: client, Policy: policy, Logger: logger},
			Settings: values, Version: translate.GlossaryVersion(pool), Logger: logger},
	})
	mux := http.NewServeMux()
	calls.Mount(mux)
	send := func(sensitivity, text string) (int, map[string]any) {
		raw, _ := json.Marshal(map[string]any{"from": "en", "to": "ta", "sensitivity": sensitivity, "passages": []string{text}})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/translate", bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return rec.Code, out
	}
	received := func() string {
		res, err := http.Get(stand.URL + "/control/bumblebee/received")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	const ordinary = "An invented " + dbMarker + " letter. Call 0771234567 today."
	const personal = "A personal " + dbMarker + " note."

	// Closed until opened: a person is needed, and nothing has gone anywhere.
	if code, out := send("ordinary", ordinary); code != 409 || !strings.Contains(strings.ToLower(jsonText(out)), "human translation required") {
		t.Fatalf("%d %v", code, out)
	}
	if strings.Contains(received(), "/v1/requests") {
		t.Fatal("a closed direction reached a source")
	}
	// Open it, as the gate's screen does.
	if _, err := pool.Exec(ctx, `INSERT INTO person (mjolnir_subject, name, email) VALUES ('whole-db-test', 'Invented', 'w@example.invalid')
		ON CONFLICT (mjolnir_subject) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var person int64
	if err := pool.QueryRow(ctx, `SELECT id FROM person WHERE mjolnir_subject = 'whole-db-test'`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := sources.OpenGate(ctx, tx, "standin-translate", sources.Translate("en", "ta"), sources.Evaluation{TestSet: "shared-1", Score: 80, Reason: "An invented reason.", EvaluatedOn: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}, person)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	code, first := send("ordinary", ordinary)
	if code != 200 {
		t.Fatalf("%d %v", code, first)
	}
	passage := first["passages"].([]any)[0].(map[string]any)
	if passage["text"] != "[ta] An invented "+dbMarker+" letter. [ta] Call 0771234567 today." || passage["machine"] != true {
		t.Fatalf("%v", passage)
	}
	if strings.Contains(received(), "0771234567") {
		t.Fatal("a number left the server")
	}
	// R-TRN-05: the same again, the same answer, from the cache, with nothing sent.
	before := strings.Count(received(), "/v1/requests")
	code, second := send("ordinary", ordinary)
	if code != 200 || strings.Count(received(), "/v1/requests") != before {
		t.Fatalf("%d, a second call", code)
	}
	again := second["passages"].([]any)[0].(map[string]any)
	if again["text"] != passage["text"] || again["origins"].(map[string]any)["cache"] != float64(2) || again["machine"] != true || second["request_ref"] == first["request_ref"] {
		t.Fatalf("%v", second)
	}
	// Ordinary pieces are in the cache, with stand-ins for numbers. Personal text, and
	// ordinary text holding a card, leave nothing.
	if code, _ := send("personal", personal); code != 200 {
		t.Fatal(code)
	}
	if code, out := send("ordinary", "Card 123456789V and "+dbMarker+"2 here."); code != 200 || out["sensitivity"] != "personal" {
		t.Fatalf("%d %v", code, out)
	}
	var total, withNumber, withPersonal int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE source_text LIKE '%0771234567%' OR translation LIKE '%0771234567%' OR source_text LIKE '%123456789V%' OR translation LIKE '%123456789V%'),
		count(*) FILTER (WHERE source_text LIKE '%personal%' OR source_text LIKE '%' || $1 || '2%') FROM cache_piece`, dbMarker).Scan(&total, &withNumber, &withPersonal); err != nil {
		t.Fatal(err)
	}
	if total != 2 || withNumber != 0 || withPersonal != 0 {
		t.Fatal(total, withNumber, withPersonal)
	}
	// One failure, whole: the source gone, a piece not cached, no provider and no text.
	if _, err := http.Post(stand.URL+"/control/bumblebee/mode", "application/json", strings.NewReader(`{"mode":"unavailable"}`)); err != nil {
		t.Fatal(err)
	}
	code, failed := send("ordinary", "Not seen "+dbMarker+" before.")
	if code != 503 || strings.Contains(jsonText(failed), dbMarker) || !strings.Contains(jsonText(failed), "no provider") {
		t.Fatalf("%d %v", code, failed)
	}
	// R-NFR-09: what the cache holds still answers.
	if code, _ := send("ordinary", ordinary); code != 200 {
		t.Fatalf("with the source down: %d", code)
	}
	// Caller text reaches no log, no outbox entry and no table but the cache's ordinary rows.
	if strings.Contains(logs.String(), dbMarker) || strings.Contains(logs.String(), "0771234567") || strings.Contains(logs.String(), "123456789V") {
		t.Fatal(logs.String())
	}
	for _, sql := range []string{
		`SELECT count(*) FROM audit_outbox WHERE details::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM change_history WHERE old_value LIKE '%' || $1 || '%' OR new_value LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM memory_entry WHERE source_text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM setting WHERE value LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM gate_decision WHERE test_set_version LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM usage_record WHERE outcome LIKE '%' || $1 || '%' OR request_ref LIKE '%' || $1 || '%'`,
	} {
		var n int
		if err := pool.QueryRow(ctx, sql, dbMarker).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: %d", sql, n)
		}
	}
}

func jsonText(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
