package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const runMarker = "INVENTEDRUNMARKER"

// everywhere counts rows of every table holding the text, whatever its column.
func everywhere(t *testing.T, w *pathWorld, text string) map[string]int {
	t.Helper()
	ctx := context.Background()
	rows, err := w.pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) < 10 {
		t.Fatalf("only %d tables", len(tables))
	}
	found := map[string]int{}
	for _, table := range tables {
		name := pgx.Identifier{table}.Sanitize()
		var n int
		if err := w.pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s AS t WHERE t::text LIKE '%%' || $1 || '%%'`, name), text).Scan(&n); err != nil {
			t.Fatal(table, err)
		}
		if n > 0 {
			found[table] = n
		}
	}
	return found
}

func (w *pathWorld) runCall(method, path, body string) (int, map[string]any, string) {
	w.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+w.key)
	rec := httptest.NewRecorder()
	w.mux.ServeHTTP(rec, r)
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			w.t.Fatal(err)
		}
	}
	return rec.Code, out, rec.Body.String()
}

func runBody(sensitivity string, passages ...string) string {
	raw, _ := json.Marshal(map[string]any{"from": "en", "to": "ta", "sensitivity": sensitivity, "passages": passages})
	return string(raw)
}

// R-TRN-13, R-REC-01: a personal run, against the database with every table looked through: nothing
// of its text anywhere but what BUMBLEBEE's stand-in was sent, masked, and one record with counts.
// Ordinary text goes to the cache as a direct request's does (R-TRN-08), and that is named here.
func TestAPersonalRunLeavesNothingBehind(t *testing.T) {
	w := newPathWorld(t)
	w.open("en", "ta")
	personal := "A personal " + runMarker + " note for 0771234567."
	code, out, raw := w.runCall(http.MethodPost, "/api/v1/translate/runs", runBody("personal", personal))
	if code != 202 {
		t.Fatalf("%d %s", code, raw)
	}
	run := out["run"].(string)
	var answer map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for {
		code, got, raw := w.runCall(http.MethodGet, "/api/v1/translate/runs/"+run, "")
		if code != 200 {
			t.Fatalf("%d %s", code, raw)
		}
		if _, working := got["state"]; !working {
			answer = got
			break
		}
		if strings.Contains(raw, runMarker) {
			t.Fatal(raw)
		}
		if time.Now().After(deadline) {
			t.Fatal("never ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
	passage := answer["passages"].([]any)[0].(map[string]any)
	if !strings.Contains(passage["text"].(string), runMarker) || !strings.Contains(passage["text"].(string), "0771234567") || answer["sensitivity"] != "personal" {
		t.Fatalf("%v", answer)
	}
	if code, _, raw := w.runCall(http.MethodGet, "/api/v1/translate/runs/"+run, ""); code != 404 || !strings.Contains(raw, "run not known") {
		t.Fatalf("%d %s", code, raw)
	}
	// The record is written as the run ends, and may follow the hand-over by a moment.
	ctx := context.Background()
	ref := answer["request_ref"].(string)
	var sensitivity, outcome, operation string
	var passages, source int
	for i := 0; ; i++ {
		err := w.pool.QueryRow(ctx, `SELECT sensitivity, outcome, operation, passages, pieces_source FROM usage_record WHERE request_ref = $1`, ref).Scan(&sensitivity, &outcome, &operation, &passages, &source)
		if err == nil {
			break
		}
		if i > 200 {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sensitivity != "personal" || outcome != "answered" || operation != "translate" || passages != 1 || source != 1 {
		t.Fatal(sensitivity, outcome, operation, passages, source)
	}
	for _, text := range []string{runMarker, "0771234567"} {
		if found := everywhere(t, w, text); len(found) != 0 {
			t.Errorf("%s: %v", text, found)
		}
		if strings.Contains(w.logs.String(), text) {
			t.Errorf("%s in the log", text)
		}
	}
	// What did leave: the text with its number masked, as R-MSK-02 has it, and a source call's
	// reference. BUMBLEBEE's stand-in shows it.
	sent := w.received()
	if !strings.Contains(sent, runMarker) || strings.Contains(sent, "0771234567") {
		t.Fatal(sent)
	}

	// An ordinary run keeps its masked pieces in the cache as a direct request does: only there.
	ordinary := "An ordinary " + runMarker + " note."
	if code, _, raw := w.runCall(http.MethodPost, "/api/v1/translate/runs", runBody("ordinary", ordinary)); code != 202 {
		t.Fatalf("%d %s", code, raw)
	}
	for i := 0; ; i++ {
		found := everywhere(t, w, runMarker)
		if found["cache_piece"] == 1 && len(found) == 1 {
			break
		}
		if i > 200 {
			t.Fatalf("%v", found)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
