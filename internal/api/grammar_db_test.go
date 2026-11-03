package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/api"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/grammar"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/standin"
	"github.com/Rumeasiyan/rosetta/internal/usage"
	"github.com/jackc/pgx/v5"
)

// R-GRM-05 and the release's goal, with everything real but BUMBLEBEE: checks that are answered
// and checks that are refused, then a marker in the text is looked for in the logs, in every
// row of every table, in what a working directory gained, and in the usage hook. The text
// reaches only the caller, and BUMBLEBEE's stand-in, masked.
func TestNoGrammarTextIsKeptAnywhere(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	id := freshSystem(t, pool)
	t.Cleanup(func() { freshSystem(t, pool) })
	const site = "https://writedocument.epc.test"
	if _, err := pool.Exec(ctx, `INSERT INTO caller_origin (caller_id, origin) VALUES ($1, $2)`, id, site); err != nil {
		t.Fatal(err)
	}
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	client := bumblebee.Client{URL: ts.URL + "/bumblebee", Key: standin.BumblebeeKey}
	book := &sources.Book{Read: client.Sources, MaxAge: func(context.Context) time.Duration { return time.Minute }}
	if err := book.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var uses []usage.Record
	srv := api.New(api.Deps{
		Keys: api.PoolKeys{Pool: pool}, Origins: api.PoolOrigins{Pool: pool}, Denied: api.PoolDenials(pool, nil), Logger: logger,
		Grammar: &grammar.Service{
			Sources:  &sources.Fallback{Book: book, Provider: client, Grammars: client, Policy: func(context.Context) sources.Policy { return sources.Policy{} }, Logger: logger},
			Settings: func(context.Context) settings.Values { return settings.Defaults() }, Held: &grammar.Held{}, Logger: logger,
		},
		Usage: func(ctx context.Context, u usage.Record) error {
			uses = append(uses, u)
			return usage.Keep(ctx, pool, u)
		},
	})
	mux := http.NewServeMux()
	srv.Mount(mux)

	const marker = "GRAMMARTEXTMARKER"
	const card = "123456789V"
	text := "We recieve this " + marker + " from " + card + " today."
	sum := sha256.Sum256([]byte(text))
	send := func(origin, account, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v2/check", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		if account != "" {
			r.Header.Set("X-Cerberus-Account", account)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}
	form := func(text, language string) string {
		return url.Values{"text": {text}, "language": {language}, "enabledOnly": {"false"}}.Encode()
	}

	// Answered, twice, and the second from memory.
	for range 2 {
		rec := send(site, "officer-1", form(text, "auto"))
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		var out struct {
			Matches []struct {
				Offset, Length int
				Context        struct{ Text string }
			}
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Matches) != 1 || out.Matches[0].Offset != 3 || out.Matches[0].Length != 7 || !strings.Contains(out.Matches[0].Context.Text, marker) {
			t.Fatalf("%s", rec.Body)
		}
	}
	// Refused every way there is, each holding the marker.
	for name, c := range map[string]struct {
		origin, account, body string
		status                int
	}{
		"not signed in": {site, "", form(text, "en"), 401},
		"not listed":    {"https://elsewhere.epc.test", "officer-1", form(text, "en"), 401},
		"Sinhala":       {site, "officer-1", form(text, "si"), 422},
		"bad form":      {site, "officer-1", "text=%zz" + marker, 400},
		"too long":      {site, "officer-1", form(strings.Repeat(marker, 2000), "en"), 413},
	} {
		rec := send(c.origin, c.account, c.body)
		if rec.Code != c.status || strings.Contains(rec.Body.String(), marker) {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	// What went to the source was masked, and what stayed in the stand-in's read-back is only
	// what a source is sent.
	res, err := http.Get(ts.URL + "/control/bumblebee/received")
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
	sentToSource := false
	for _, r := range got.Received {
		if strings.Contains(r.Body, marker) {
			sentToSource = true
		}
		if strings.Contains(r.Body, card) {
			t.Fatalf("a card number left the server: %s", r.Body)
		}
	}
	if !sentToSource {
		t.Fatal("the check never reached the source")
	}

	places := map[string]string{"the log": logs.String()}
	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) < 20 {
		t.Fatalf("only %d tables", len(tables))
	}
	for _, table := range tables {
		rows, err := pool.Query(ctx, `SELECT t::text FROM `+pgx.Identifier{table}.Sanitize()+` t`)
		if err != nil {
			t.Fatal(table, err)
		}
		all, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(table, err)
		}
		places["table "+table] = strings.Join(all, "\n")
	}
	if len(uses) == 0 {
		t.Fatal("the hook heard nothing")
	}
	places["the usage hook"] = strings.TrimSpace(strings.Join(func() []string {
		var out []string
		for _, u := range uses {
			raw, _ := json.Marshal(u)
			out = append(out, string(raw))
		}
		return out
	}(), "\n"))
	for place, held := range places {
		for _, part := range []string{marker, card, hex.EncodeToString(sum[:])} {
			if strings.Contains(held, part) {
				t.Errorf("%q is in %s", part, place)
			}
		}
	}
	// A refusal was kept as a denial, so the outbox above held something to look through.
	if !strings.Contains(places["table audit_outbox"], "Nobody is signed in") {
		t.Fatalf("no denial kept: %s", places["table audit_outbox"])
	}
}
