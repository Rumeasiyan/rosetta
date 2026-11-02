package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pair = map[string]string

func (w *pathWorld) remember(body map[string]any) (int, map[string]any) {
	w.t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/memory", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+w.key)
	rec := httptest.NewRecorder()
	w.mux.ServeHTTP(rec, r)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		w.t.Fatal(rec.Body.String())
	}
	return rec.Code, out
}

func approved(passages ...pair) map[string]any {
	return map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "approver_post": "An invented post", "passages": passages}
}

type memoryRow struct {
	source, hash, approved, post, state string
	version                             int
	replacedBy                          *int64
	on                                  time.Time
}

func (w *pathWorld) memoryRows() []memoryRow {
	w.t.Helper()
	rows, err := w.pool.Query(context.Background(), `SELECT source_text, source_hash, approved_text, approver_post, state, glossary_version, replaced_by_id, approved_on FROM memory_entry ORDER BY id`)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []memoryRow
	for rows.Next() {
		var r memoryRow
		if err := rows.Scan(&r.source, &r.hash, &r.approved, &r.post, &r.state, &r.version, &r.replacedBy, &r.on); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// R-MEM-01, R-MEM-02: approved text is split into pieces and joins the memory, with the approver's post,
// the calling system, the date and the glossary version. A newer approval replaces the older, whose
// words go. An audit entry says what was done and holds no text.
func TestApprovedTextJoinsTheMemory(t *testing.T) {
	w := newPathWorld(t)
	w.publish("term", "Invented Term", "පදය", "சொல்")
	const secret = "Memorymarkertext"
	code, out := w.remember(approved(pair{"source": secret + " one. Two here.\n\nThree.", "approved": "ஒன்று " + secret + ". இரண்டு.\n\nமூன்று."}, pair{"source": "Café list.", "approved": "பட்டியல்."}))
	if code != 200 || out["pieces"] != float64(4) || len(out["not_kept"].([]any)) != 0 || out["glossary_version"] != float64(1) || out["request_ref"] == nil {
		t.Fatalf("%d %v", code, out)
	}
	rows := w.memoryRows()
	if len(rows) != 4 {
		t.Fatalf("%+v", rows)
	}
	if r := rows[1]; r.source != "Two here." || r.approved != "இரண்டு." || r.post != "An invented post" || r.state != "served" || r.version != 1 || r.on.IsZero() {
		t.Fatalf("%+v", r)
	}
	if rows[3].source != "Café list." {
		t.Fatalf("not normalised (R-NFR-05): %q", rows[3].source)
	}
	var caller int64
	if err := w.pool.QueryRow(context.Background(), `SELECT DISTINCT caller_id FROM memory_entry`).Scan(&caller); err != nil || caller == 0 {
		t.Fatal(err)
	}
	// A newer approval of the same piece replaces the older, and takes its words away.
	code, out = w.remember(approved(pair{"source": "Two here.", "approved": "புதிய இரண்டு."}))
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	rows = w.memoryRows()
	if len(rows) != 5 || rows[1].state != "replaced" || rows[1].source != "" || rows[1].approved != "" || rows[1].replacedBy == nil || rows[4].approved != "புதிய இரண்டு." || rows[4].state != "served" {
		t.Fatalf("%+v", rows)
	}
	// R-REC-02: each addition is an entry, by the system, with no text.
	ctx := context.Background()
	var n, withText int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE details->>'object_type' = 'memory' AND action = 'creation' AND details->>'post' = 'system'),
		count(*) FILTER (WHERE details::text LIKE '%' || $1 || '%' OR details::text LIKE '%An invented post%' OR details::text LIKE '%Two here%') FROM audit_outbox`, secret).Scan(&n, &withText); err != nil {
		t.Fatal(err)
	}
	if n < 2 || withText != 0 {
		t.Fatalf("%d entries, %d with text", n, withText)
	}
	if strings.Contains(w.logs.String(), secret) || strings.Contains(w.logs.String(), "invented post") {
		t.Fatal(w.logs.String())
	}
}

// R-MEM-03: personal and confidential text is refused as "invalid request" and never kept, and text marked
// ordinary holding a card is personal (R-MSK-04), wherever it is. So is anything else that cannot be taken.
func TestWhatTheMemoryRefuses(t *testing.T) {
	w := newPathWorld(t)
	good := pair{"source": "A piece.", "approved": "ஒரு துண்டு."}
	const secret = "Memoryrefusedmarker"
	for name, c := range map[string]struct {
		body map[string]any
		code int
	}{
		"personal":        {map[string]any{"from": "en", "to": "ta", "sensitivity": "personal", "approver_post": "A post", "passages": []pair{{"source": secret, "approved": secret}}}, 400},
		"confidential":    {map[string]any{"from": "en", "to": "ta", "sensitivity": "confidential", "approver_post": "A post", "passages": []pair{good}}, 400},
		"no sensitivity":  {map[string]any{"from": "en", "to": "ta", "approver_post": "A post", "passages": []pair{good}}, 400},
		"card in source":  {approved(good, pair{"source": secret + " 123456789V.", "approved": "x."}), 400},
		"new card":        {approved(pair{"source": "A number.", "approved": secret + " 199012345678."}), 400},
		"no post":         {map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "passages": []pair{good}}, 400},
		"a card for post": {map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "approver_post": "123456789V", "passages": []pair{good}}, 400},
		"same language":   {map[string]any{"from": "en", "to": "en", "sensitivity": "ordinary", "approver_post": "A post", "passages": []pair{good}}, 400},
		"other language":  {map[string]any{"from": "en", "to": "fr", "sensitivity": "ordinary", "approver_post": "A post", "passages": []pair{good}}, 400},
		"no passages":     {map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "approver_post": "A post", "passages": []pair{}}, 400},
		"blank side":      {approved(pair{"source": "A piece.", "approved": "  "}), 400},
		"missing side":    {approved(pair{"source": "A piece."}), 400},
		"stand-in shape":  {approved(pair{"source": "A ⟦M1⟧ piece.", "approved": "x ⟦M1⟧."}), 400},
		"a future date":   {map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "approver_post": "A post", "approved_on": "2999-01-01", "passages": []pair{good}}, 400},
		"not a date":      {map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "approver_post": "A post", "approved_on": "soon", "passages": []pair{good}}, 400},
		"too long":        {approved(pair{"source": strings.Repeat("a", 5001), "approved": "x"}), 413},
	} {
		code, out := w.remember(c.body)
		if code != c.code {
			t.Errorf("%s: %d %v", name, code, out)
		}
		if strings.Contains(jsonText(out), secret) {
			t.Errorf("%s: the answer holds the text: %v", name, out)
		}
	}
	if rows := w.memoryRows(); len(rows) != 0 {
		t.Fatalf("kept: %+v", rows)
	}
	if strings.Contains(w.logs.String(), secret) || strings.Contains(w.logs.String(), "123456789V") {
		t.Fatal(w.logs.String())
	}
	// A body that is not JSON, and no key at all.
	r := httptest.NewRequest(http.MethodPost, "/api/v1/memory", strings.NewReader(`{"from": `+secret))
	r.Header.Set("Authorization", "Bearer "+w.key)
	rec := httptest.NewRecorder()
	w.mux.ServeHTTP(rec, r)
	if rec.Code != 400 || strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	r = httptest.NewRequest(http.MethodPost, "/api/v1/memory", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	w.mux.ServeHTTP(rec, r)
	if rec.Code != 401 {
		t.Fatal(rec.Code)
	}
	var n int
	if err := w.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_outbox WHERE details::text LIKE '%' || $1 || '%'`, secret).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
}

// Pieces that do not line up are left out and named; the rest join. Numbers masking recognises are kept
// as stand-ins, and must be on both sides.
func TestPiecesThatDoNotLineUp(t *testing.T) {
	w := newPathWorld(t)
	code, out := w.remember(approved(
		pair{"source": "One. Two.", "approved": "ஒன்றும் இரண்டும்."},
		pair{"source": "Ring 0771234567 or 011 2345678 now.", "approved": "இப்போது 0112345678 அல்லது 0771234567 அழைக்க."},
		pair{"source": "Ring 0771234567.", "approved": "வேறு 0719999999."},
		pair{"source": "A number 12.", "approved": "இல்லை."},
		pair{"source": "12 3.", "approved": "12 3."},
	))
	if code != 200 || out["pieces"] != float64(2) {
		t.Fatalf("%d %v", code, out)
	}
	notKept := out["not_kept"].([]any)
	if len(notKept) != 2 || notKept[0] != float64(0) || notKept[1] != float64(2) {
		t.Fatalf("%v", notKept)
	}
	rows := w.memoryRows()
	if len(rows) != 2 || rows[0].source != "Ring ⟦M1⟧ or ⟦M2⟧ now." || rows[0].approved != "இப்போது ⟦M2⟧ அல்லது ⟦M1⟧ அழைக்க." {
		t.Fatalf("%+v", rows)
	}
	for _, r := range rows {
		if strings.Contains(r.source+r.approved, "0771234567") {
			t.Fatalf("a number was kept: %+v", r)
		}
	}
}
