package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// usageRow is one record as the table holds it, whole, so a test can look through all of it.
func usageRows(t *testing.T, w *pathWorld, op string) []map[string]any {
	t.Helper()
	rows, err := w.pool.Query(context.Background(), `SELECT row_to_json(u)::text FROM usage_record u WHERE operation = $1 AND caller_id = (SELECT caller_id FROM caller_key WHERE key_hash = encode(sha256($2::bytea), 'hex')) ORDER BY id`, op, w.key)
	if err != nil {
		t.Fatal(err)
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	out := make([]map[string]any, len(all))
	for i, raw := range all {
		if err := json.Unmarshal([]byte(raw), &out[i]); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, usageMarker) || strings.Contains(raw, "123456789V") {
			t.Fatalf("a record holds text: %s", raw)
		}
	}
	return out
}

const usageMarker = "USAGEMARKERTEXT"

// A record has what R-REC-01 lists, kept in monthly parts, and never any text, whatever was asked.
func TestARecordForEachOperationAndNoText(t *testing.T) {
	w := newPathWorld(t)
	w.open("en", "ta")
	ctx := context.Background()
	w.publish("post", "Invented Registrar", "ඉන්වෙන්ටඩ් ලේඛකාධිකාරී", "புனைந்த பதிவாளர்")

	// The first goes to a source, and a whole term comes from the glossary.
	if code, out := w.translate("ordinary", "A letter about "+usageMarker+". Invented Registrar."); code != 200 {
		t.Fatal(out)
	}
	rows := usageRows(t, w, "translate")
	if len(rows) != 1 {
		t.Fatalf("%v", rows)
	}
	r := rows[0]
	if r["outcome"] != "answered" || r["from_language"] != "en" || r["to_language"] != "ta" || r["sensitivity"] != "ordinary" || r["passages"] != 1.0 ||
		r["pieces_source"] != 1.0 || r["pieces_glossary"] != 1.0 || r["mismatch"] != false || !strings.HasPrefix(r["request_ref"].(string), "r-") {
		t.Fatalf("%v", r)
	}
	if got := r["sources_used"].([]any); len(got) != 1 || got[0] != "standin-translate" {
		t.Fatalf("%v", r)
	}
	// The same again comes from the cache; and a card makes it personal, counted as a mismatch.
	w.translate("ordinary", "A letter about "+usageMarker+". Invented Registrar.")
	w.translate("ordinary", usageMarker+" holds 123456789V here.")
	rows = usageRows(t, w, "translate")
	if rows[1]["pieces_cache"] != 1.0 || rows[1]["pieces_source"] != 0.0 {
		t.Fatalf("%v", rows[1])
	}
	if rows[2]["mismatch"] != true || rows[2]["sensitivity"] != "personal" {
		t.Fatalf("%v", rows[2])
	}
	// A failure keeps its reason; fallback to no source is no provider.
	w.mode("unavailable")
	w.translate("personal", "Another "+usageMarker+" letter.")
	w.mode("normal")
	rows = usageRows(t, w, "translate")
	if rows[3]["outcome"] != "no provider" || rows[3]["pieces_source"] != 0.0 {
		t.Fatalf("%v", rows[3])
	}

	// A check keeps a length, a source and no text; the same text again is spared the source.
	check := func() int {
		form := url.Values{"language": {"en-GB"}, "text": {"We recieve " + usageMarker + "."}}
		q := httptest.NewRequest(http.MethodPost, "/api/v2/check", strings.NewReader(form.Encode()))
		q.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		q.Header.Set("Authorization", "Bearer "+w.key)
		rec := httptest.NewRecorder()
		w.mux.ServeHTTP(rec, q)
		return rec.Code
	}
	if check() != 200 || check() != 200 {
		t.Fatal("a check failed")
	}
	rows = usageRows(t, w, "grammar")
	if len(rows) != 2 || rows[0]["pieces_source"] != 1.0 || rows[0]["characters"].(float64) == 0 || rows[1]["pieces_cache"] != 1.0 || rows[1]["pieces_source"] != 0.0 {
		t.Fatalf("%v", rows)
	}

	// A read of the gate is a request too.
	q := httptest.NewRequest(http.MethodGet, "/api/v1/gate", nil)
	q.Header.Set("Authorization", "Bearer "+w.key)
	w.mux.ServeHTTP(httptest.NewRecorder(), q)
	if rows = usageRows(t, w, "read"); len(rows) != 1 || rows[0]["outcome"] != "answered" {
		t.Fatalf("%v", rows)
	}

	// Usage never goes to PENSIEVE, one by one or in total (owners register). What the outbox holds
	// is anything but: nothing in it names a request's reference, or usage.
	var count int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM audit_outbox a WHERE a.details::text LIKE '%usage%' OR EXISTS
		(SELECT 1 FROM usage_record u WHERE a.details::text LIKE '%' || u.request_ref || '%')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d outbox entries speak of usage", count)
	}
	if strings.Contains(w.logs.String(), usageMarker) {
		t.Fatal(w.logs)
	}
}

// An account that never signed in on the screens still counts, and is matched to a person who has.
func TestARecordNamesTheOfficer(t *testing.T) {
	w := newPathWorld(t)
	ctx := context.Background()
	var subject string
	if err := w.pool.QueryRow(ctx, `SELECT mjolnir_subject FROM person WHERE id = $1`, w.person).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	var system int64
	if err := w.pool.QueryRow(ctx, `SELECT caller_id FROM caller_key WHERE key_hash = encode(sha256($1::bytea), 'hex')`, w.key).Scan(&system); err != nil {
		t.Fatal(err)
	}
	const origin = "https://usage-editor.example.invalid"
	if _, err := w.pool.Exec(ctx, `INSERT INTO caller_origin (caller_id, origin) VALUES ($1, $2) ON CONFLICT DO NOTHING`, system, origin); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.pool.Exec(context.Background(), `DELETE FROM caller_origin WHERE origin = $1`, origin) })
	for _, account := range []string{subject, "officer-never-seen"} {
		q := httptest.NewRequest(http.MethodPost, "/api/v2/check", strings.NewReader("language=en-GB&text=Nothing+here."))
		q.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		q.Header.Set("Origin", origin)
		q.Header.Set("X-Cerberus-Account", account)
		rec := httptest.NewRecorder()
		w.mux.ServeHTTP(rec, q)
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
	rows := usageRows(t, w, "grammar")
	if len(rows) != 2 || rows[0]["officer_account"] != subject || rows[0]["person_id"] != float64(w.person) || rows[1]["officer_account"] != "officer-never-seen" || rows[1]["person_id"] != nil {
		t.Fatalf("%v", rows)
	}
}
