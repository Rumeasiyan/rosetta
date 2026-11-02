package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/api"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dbMarker = "CALLERTEXTMARKER"

func freshSystem(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := context.Background()
	for _, sql := range []string{`DELETE FROM audit_outbox`, `DELETE FROM usage_record`, `DELETE FROM daily_total`, `DELETE FROM memory_entry`, `DELETE FROM alert`, `DELETE FROM caller_origin`, `DELETE FROM caller_key`, `DELETE FROM caller`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO caller (tva_reference, name, tva_status, tva_checked_at) VALUES ('DB-1', 'Invented', 'live', now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// With the real tables: a key works until it is turned off, then "no key" at once,
// the refused one kept as a denial, and nothing sent reaches a row.
func TestKeysAgainstTheDatabase(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	id := freshSystem(t, pool)
	var key string
	var made callers.Key
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		key, made, err = issueByTestPerson(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s := api.New(api.Deps{Keys: api.PoolKeys{Pool: pool}, Denied: api.PoolDenials(pool, nil)})
	mux := http.NewServeMux()
	s.Mount(mux)
	send := func(k string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/nothing", strings.NewReader(dbMarker))
		r.Header.Set("Authorization", "Bearer "+k)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec.Code
	}
	if got := send(key); got != http.StatusBadRequest {
		t.Fatalf("a live key passes the check to an unknown path: %d", got)
	}
	var used bool
	if err := pool.QueryRow(ctx, `SELECT k.last_used_at IS NOT NULL AND c.last_request_at IS NOT NULL FROM caller_key k JOIN caller c ON c.id = k.caller_id WHERE k.id = $1`, made.ID).Scan(&used); err != nil || !used {
		t.Fatal(used, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE caller_key SET turned_off_at = now() WHERE id = $1`, made.ID); err != nil {
		t.Fatal(err)
	}
	if got := send(key); got != http.StatusUnauthorized {
		t.Fatalf("off: %d", got)
	}
	if got := send(dbMarker); got != http.StatusUnauthorized {
		t.Fatalf("unknown: %d", got)
	}
	rows, err := pool.Query(ctx, `SELECT details::text FROM audit_outbox ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || !strings.Contains(all[0], "DB-1") || !strings.Contains(all[1], "unidentified caller") {
		t.Fatalf("%v", all)
	}
	for _, e := range all {
		if strings.Contains(e, dbMarker) || strings.Contains(e, key) {
			t.Fatalf("%s", e)
		}
	}
}

func issueByTestPerson(ctx context.Context, tx pgx.Tx, id int64) (string, callers.Key, error) {
	var person int64
	if err := tx.QueryRow(ctx, `INSERT INTO person (mjolnir_subject, name, email) VALUES ('keys-db-test', 'Invented', 'k@example.invalid')
		ON CONFLICT (mjolnir_subject) DO UPDATE SET name = EXCLUDED.name RETURNING id`).Scan(&person); err != nil {
		return "", callers.Key{}, err
	}
	return callers.Issue(ctx, tx, id, person, "")
}

// The whole way from the database: a listed address, read and counted there.
func TestABrowserCallCountedInTheDatabase(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	id := freshSystem(t, pool)
	t.Cleanup(func() { freshSystem(t, pool) })
	if _, err := pool.Exec(ctx, `INSERT INTO caller_origin (caller_id, origin) VALUES ($1, 'https://writedocument.epc.test')`, id); err != nil {
		t.Fatal(err)
	}
	srv := api.New(api.Deps{Keys: api.PoolKeys{Pool: pool}, Origins: api.PoolOrigins{Pool: pool}})
	mux := http.NewServeMux()
	srv.Mount(mux)
	send := func(origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v2/languages", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Cerberus-Account", "officer-1")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}
	if rec := send("https://elsewhere.epc.test"); rec.Code != 401 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	rec := send("https://writedocument.epc.test")
	if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != "https://writedocument.epc.test" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	var used bool
	if err := pool.QueryRow(ctx, `SELECT last_request_at IS NOT NULL FROM caller WHERE id = $1`, id).Scan(&used); err != nil || !used {
		t.Fatal(used, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE caller SET tva_status = 'retired' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if rec := send("https://writedocument.epc.test"); rec.Code != 401 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("a retired system: %d %v", rec.Code, rec.Header())
	}
}
