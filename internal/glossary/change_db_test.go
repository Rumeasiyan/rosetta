package glossary_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type changeWorld struct {
	t      *testing.T
	pool   *pgxpool.Pool
	by     int64
	caller int64
	source int64
}

func newChangeWorld(t *testing.T) *changeWorld {
	t.Helper()
	pool := dbtest.Open(t)
	empty(t, pool)
	w := &changeWorld{t: t, pool: pool, by: keeper(t, pool)}
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `INSERT INTO caller (tva_reference, name, tva_status, tva_checked_at) VALUES ($1, 'Invented', 'live', now()) RETURNING id`, fmt.Sprintf("CHANGE-%d", time.Now().UnixNano())).Scan(&w.caller); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source (name, version, state, seen_at) VALUES ($1, '1', 'on', now()) RETURNING id`, fmt.Sprintf("change-source-%d", time.Now().UnixNano())).Scan(&w.source); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { empty(t, pool) })
	return w
}

func (w *changeWorld) memory(from, to, source, approved string) int64 {
	w.t.Helper()
	var id int64
	if err := w.pool.QueryRow(context.Background(), `INSERT INTO memory_entry (caller_id, glossary_version, from_language, to_language, source_text, source_hash, approved_text, approver_post, approved_on)
		VALUES ($1, 0, $2, $3, $4, md5($4 || $3), $5, 'An invented post', '2020-01-01') RETURNING id`, w.caller, from, to, source, approved).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *changeWorld) cache(from, to, source, translation string) {
	w.t.Helper()
	if _, err := w.pool.Exec(context.Background(), `INSERT INTO cache_piece (source_hash, from_language, to_language, source_id, glossary_version, source_text, translation, source_version, expires_at)
		VALUES (md5($3 || $2), $1, $2, $5, 0, $3, $4, '1', now() + interval '1 day')`, from, to, source, translation, w.source); err != nil {
		w.t.Fatal(err)
	}
}

func (w *changeWorld) state(id int64) (state string, entry *int64, version *int) {
	w.t.Helper()
	if err := w.pool.QueryRow(context.Background(), `SELECT state, recheck_glossary_entry_id, recheck_glossary_version FROM memory_entry WHERE id = $1`, id).Scan(&state, &entry, &version); err != nil {
		w.t.Fatal(err)
	}
	return
}

func (w *changeWorld) cached() []string {
	w.t.Helper()
	rows, err := w.pool.Query(context.Background(), `SELECT source_text FROM cache_piece ORDER BY source_text`)
	if err != nil {
		w.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		w.t.Fatal(err)
	}
	return out
}

// R-GLO-06: a term newly published removes the cached pieces holding it and marks the memory entries
// holding it, in any case, on either side. What does not hold it is left alone, as is what has gone
// to a source with the term stood in for.
func TestAPublishedTermClearsTheCacheAndMarksTheMemory(t *testing.T) {
	w := newChangeWorld(t)
	sourceSide := w.memory("en", "ta", "The chief secretary wrote.", "செயலாளர் எழுதினார்.")
	translated := w.memory("en", "ta", "A note from the office.", "பிரதம செயலாளர் குறிப்பு.")
	otherLanguage := w.memory("ta", "en", "பிரதம செயலாளர் வந்தார்.", "An officer came.")
	unrelated := w.memory("en", "ta", "Nothing of the kind here.", "இங்கே இல்லை.")
	sinhala := w.memory("en", "si", "The Chief Secretary came.", "ප්‍රධාන ලේකම් පැමිණියේය.")
	w.cache("en", "ta", "The CHIEF secretary called.", "அழைத்தார்.")
	w.cache("en", "si", "A note.", "ප්‍රධාන ලේකම් සටහන.")
	w.cache("en", "ta", "The ⟦G1⟧ called.", "⟦G1⟧ அழைத்தார்.")
	w.cache("en", "ta", "A plain piece.", "ஒரு துண்டு.")

	id := publish(t, w.pool, w.by, glossary.TypePost, "Chief Secretary", "ප්‍රධාන ලේකම්", "பிரதம செயலாளர்", nil)
	for name, m := range map[string]int64{"source": sourceSide, "translated": translated, "other language": otherLanguage, "sinhala": sinhala} {
		state, entry, version := w.state(m)
		if state != "recheck" || entry == nil || *entry != id || version == nil || *version != 1 {
			t.Errorf("%s: %s %v %v", name, state, entry, version)
		}
	}
	if state, _, _ := w.state(unrelated); state != "served" {
		t.Errorf("unrelated: %s", state)
	}
	if got := strings.Join(w.cached(), "|"); got != "A plain piece.|The ⟦G1⟧ called." {
		t.Fatalf("%s", got)
	}
}

// A correction of the Tamil concerns what is in Tamil or going into it: an English to Sinhala entry is
// still right. The old words count as well as the new, and one already marked stays with what marked it.
func TestACorrectionOfOneLanguage(t *testing.T) {
	w := newChangeWorld(t)
	first := publish(t, w.pool, w.by, glossary.TypePost, "Chief Clerk", "ප්‍රධාන ලිපිකරු", "பிரதம எழுத்தர்", nil)
	intoTamil := w.memory("en", "ta", "The Chief Clerk came.", "வந்தார்.")
	holdsOld := w.memory("si", "ta", "A note.", "பிரதம எழுத்தர் குறிப்பு.")
	fromTamil := w.memory("ta", "en", "பிரதம எழுத்தர் வந்தார்.", "An officer came.")
	intoSinhala := w.memory("en", "si", "The Chief Clerk came.", "ප්‍රධාන ලිපිකරු පැමිණියේය.")
	w.cache("en", "ta", "The Chief Clerk called.", "அழைத்தார்.")
	w.cache("en", "si", "The Chief Clerk called.", "කතා කළේය.")
	publish(t, w.pool, w.by, glossary.TypePost, "Chief Clerk", "ප්‍රධාන ලිපිකරු", "தலைமை எழுத்தர்", &first)
	for name, m := range map[string]int64{"into Tamil": intoTamil, "holds the old words": holdsOld, "from Tamil": fromTamil} {
		if state, _, _ := w.state(m); state != "recheck" {
			t.Errorf("%s: %s", name, state)
		}
	}
	if state, _, _ := w.state(intoSinhala); state != "served" {
		t.Error("an entry the change does not touch was marked")
	}
	var left []string
	rows, err := w.pool.Query(context.Background(), `SELECT to_language FROM cache_piece`)
	if err != nil {
		t.Fatal(err)
	}
	if left, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || len(left) != 1 || left[0] != "si" {
		t.Fatalf("%v %v", left, err)
	}
}

// A change to the note or type alone changes no term, so nothing is marked or removed.
func TestAChangeToANoteAlone(t *testing.T) {
	w := newChangeWorld(t)
	first := publish(t, w.pool, w.by, glossary.TypePost, "Registrar", "රෙජිස්ට්‍රාර්", "பதிவாளர்", nil)
	m := w.memory("en", "ta", "The Registrar came.", "வந்தார்.")
	w.cache("en", "ta", "The Registrar called.", "அழைத்தார்.")
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		id, langs, err := glossary.Propose(ctx, tx, glossary.Proposal{Type: glossary.TypeTerm, Note: "Changed.", By: w.by, Replaces: &first, Texts: map[i18n.Lang]string{}})
		if err != nil {
			return err
		}
		for _, l := range langs {
			if _, err := glossary.Check(ctx, tx, id, l, "", w.by); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := w.state(m); state != "served" || len(w.cached()) != 1 {
		t.Fatalf("%s %v", state, w.cached())
	}
}

// A term more entries hold than one publication may mark is refused, and the entry stays held for
// keepers to word again: nothing is marked or removed, and no version taken.
func TestATermTooCommonIsRefusedWhole(t *testing.T) {
	w := newChangeWorld(t)
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `INSERT INTO memory_entry (caller_id, glossary_version, from_language, to_language, source_text, source_hash, approved_text, approver_post, approved_on)
		SELECT $1, 0, 'en', 'ta', 'A common word ' || n || '.', md5(n::text), 'சொல்.', 'An invented post', '2020-01-01' FROM generate_series(1, $2::int) n`, w.caller, glossary.MaxMarks+1); err != nil {
		t.Fatal(err)
	}
	w.cache("en", "ta", "A common word.", "சொல்.")
	t.Cleanup(func() { w.pool.Exec(context.Background(), `TRUNCATE memory_entry`) })
	err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		id, langs, err := glossary.Propose(ctx, tx, glossary.Proposal{Type: glossary.TypeTerm, By: w.by, Texts: map[i18n.Lang]string{i18n.EN: "common word", i18n.SI: "පොදු", i18n.TA: "பொது"}})
		if err != nil {
			return err
		}
		for _, l := range langs {
			if _, err := glossary.Check(ctx, tx, id, l, "", w.by); err != nil {
				return err
			}
		}
		return nil
	})
	if !errors.Is(err, glossary.ErrTooCommon) {
		t.Fatal(err)
	}
	var marked int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM memory_entry WHERE state = 'recheck'`).Scan(&marked); err != nil || marked != 0 || len(w.cached()) != 1 {
		t.Fatalf("%d %v %v", marked, err, w.cached())
	}
	if v, err := glossary.Current(ctx, w.pool); err != nil || v != 0 {
		t.Fatalf("%d %v", v, err)
	}
}

// It runs in the publishing transaction: rolled back, nothing is marked or removed, and no version.
func TestItIsAllOneTransaction(t *testing.T) {
	w := newChangeWorld(t)
	m := w.memory("en", "ta", "The Auditor came.", "வந்தார்.")
	w.cache("en", "ta", "The Auditor called.", "அழைத்தார்.")
	ctx := context.Background()
	stop := fmt.Errorf("stop")
	err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		id, langs, err := glossary.Propose(ctx, tx, glossary.Proposal{Type: glossary.TypePost, By: w.by, Texts: map[i18n.Lang]string{i18n.EN: "Auditor", i18n.SI: "විගණක", i18n.TA: "கணக்காய்வாளர்"}})
		if err != nil {
			return err
		}
		var done glossary.Checked
		for _, l := range langs {
			if done, err = glossary.Check(ctx, tx, id, l, "", w.by); err != nil {
				return err
			}
		}
		if !done.Published || done.Change.MemoryMarked != 1 || done.Change.CacheRemoved != 1 {
			t.Errorf("%+v", done)
		}
		return stop
	})
	if err != stop {
		t.Fatal(err)
	}
	if state, _, _ := w.state(m); state != "served" || len(w.cached()) != 1 {
		t.Fatalf("%s %v", state, w.cached())
	}
	if v, err := glossary.Current(ctx, w.pool); err != nil || v != 0 {
		t.Fatalf("%d %v", v, err)
	}
}

// R-NFR-01: a memory of 500,000 pieces. This fills the tables, so it runs only when asked:
//
//	ROSETTA_MEASURE_MEMORY=500000 go test -p 1 -count=1 -v -timeout 30m -run TestMeasure ./internal/glossary
//
// It reports how long a publication takes in its transaction, what the plans are, and how long the
// other questions asked of a full memory take. The figures are in docs/DECISION-LOG.md.
func TestMeasurePublishingOverAFullMemory(t *testing.T) {
	raw := os.Getenv("ROSETTA_MEASURE_MEMORY")
	if raw == "" {
		t.Skip("set ROSETTA_MEASURE_MEMORY to the number of pieces to measure with")
	}
	pieces, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatal(err)
	}
	w := newChangeWorld(t)
	ctx := context.Background()
	// So many rows go at once, or the last step would be stopped at the platform's 30 seconds.
	t.Cleanup(func() { w.pool.Exec(context.Background(), `TRUNCATE memory_entry`) })
	started := time.Now()
	// Invented sentences of about 70 characters, one in 500 holding the term. Each differs, as
	// pieces of the memory do.
	const fill = `INSERT INTO memory_entry (caller_id, glossary_version, from_language, to_language, source_text, source_hash, approved_text, approver_post, approved_on)
		SELECT $1, 0, 'en', 'ta',
			'The ' || w.a[1 + n % 12] || ' office wrote ' || md5(n::text) || CASE WHEN n % 500 = 0 THEN ' to the Measured Registrar' ELSE ' to the ' || w.a[1 + (n / 7) % 12] END || '.',
			md5(n::text || 'x'),
			'அலுவலகம் ' || md5(n::text) || ' எழுதியது ' || w.b[1 + n % 8] || '.',
			'An invented post', '2020-01-01'
		FROM generate_series($2::bigint, $3::bigint) n,
			(SELECT ARRAY['district','provincial','zonal','regional','education','health','roads','lands','water','finance','planning','works'] AS a,
				ARRAY['இன்று','நேற்று','நாளை','மாதம்','ஆண்டு','வாரம்','நேரம்','காலை'] AS b) w`
	const batch = 50000
	for from := 1; from <= pieces; from += batch {
		if _, err := w.pool.Exec(ctx, fill, w.caller, from, min(from+batch-1, pieces)); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%d pieces added in %v", pieces, time.Since(started))
	if _, err := w.pool.Exec(ctx, `ANALYZE memory_entry`); err != nil {
		t.Fatal(err)
	}
	var size string
	if err := w.pool.QueryRow(ctx, `SELECT pg_size_pretty(pg_total_relation_size('memory_entry'))`).Scan(&size); err != nil {
		t.Fatal(err)
	}
	t.Logf("memory_entry with its indexes: %s", size)

	explain := func(sql string, args ...any) string {
		rows, err := w.pool.Query(ctx, `EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY ON) `+sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(lines, "\n")
	}
	t.Logf("the plan of a mark, read only:\n%s", explain(`SELECT count(*) FROM memory_entry WHERE state = 'served' AND ((from_language = 'en' AND source_text ILIKE ANY($1::text[]))
		OR (to_language = 'en' AND approved_text ILIKE ANY($1::text[])))`, []string{"%Measured Registrar%"}))
	if plan := explain(`SELECT 1 FROM memory_entry WHERE source_hash = md5('250000x') AND from_language = 'en' AND to_language = 'ta' AND state = 'served'`); !strings.Contains(plan, "Index") {
		t.Errorf("a served lookup scans: %s", plan)
	} else {
		t.Logf("the plan of serving a piece:\n%s", plan)
	}

	// A new term, in the transaction a keeper's third check runs.
	began := time.Now()
	id := publish(t, w.pool, w.by, glossary.TypePost, "Measured Registrar", "මැනූ රෙජිස්ට්‍රාර්", "அளவிட்ட பதிவாளர்", nil)
	first := time.Since(began)
	var marked int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM memory_entry WHERE state = 'recheck'`).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	t.Logf("publishing a new term over %d pieces: %v, %d marked", pieces, first, marked)
	if marked != pieces/500 {
		t.Errorf("%d marked, want %d", marked, pieces/500)
	}
	// A correction, with most of the memory unaffected, and a term nothing holds.
	began = time.Now()
	publish(t, w.pool, w.by, glossary.TypePost, "Measured Registrar", "", "திருத்திய பதிவாளர்", &id)
	t.Logf("publishing a correction: %v", time.Since(began))
	began = time.Now()
	publish(t, w.pool, w.by, glossary.TypeTerm, "Nothing Holds This Term", "කිසිවක් නැත", "எதுவும் இல்லை", nil)
	t.Logf("publishing a term nothing holds: %v", time.Since(began))
	// A word every piece holds, the worst a keeper could publish: refused whole, in moments, with
	// nothing marked, and not as a statement timing out.
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM memory_entry WHERE state = 'recheck'`).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	began = time.Now()
	err = pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		id, langs, err := glossary.Propose(ctx, tx, glossary.Proposal{Type: glossary.TypeTerm, By: w.by, Texts: map[i18n.Lang]string{i18n.EN: "office", i18n.SI: "කාර්යාලය", i18n.TA: "அலுவலகம்"}})
		if err != nil {
			return err
		}
		for _, l := range langs {
			if _, err := glossary.Check(ctx, tx, id, l, "", w.by); err != nil {
				return err
			}
		}
		return nil
	})
	t.Logf("publishing a word every piece holds: %v, %v", time.Since(began), err)
	if !errors.Is(err, glossary.ErrTooCommon) {
		t.Errorf("%v", err)
	}
	var after int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM memory_entry WHERE state = 'recheck'`).Scan(&after); err != nil || after != marked {
		t.Errorf("%d marked, was %d: %v", after, marked, err)
	}
	// The most one publication may mark, in its statement's 30 seconds.
	if _, err := w.pool.Exec(ctx, `UPDATE memory_entry SET source_text = source_text || ' Limit Term' WHERE id IN (SELECT id FROM memory_entry WHERE state = 'served' ORDER BY id LIMIT 500)`); err != nil {
		t.Fatal(err)
	}
	began = time.Now()
	publish(t, w.pool, w.by, glossary.TypeTerm, "Limit Term", "සීමා පදය", "வரம்புச் சொல்", nil)
	t.Logf("publishing a term 500 more pieces hold: %v", time.Since(began))
	began = time.Now()
	var found int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT 1 FROM memory_entry m WHERE m.state IN ('served', 'recheck') AND (m.source_text ILIKE '%measured registrar%' OR m.approved_text ILIKE '%measured registrar%') LIMIT 100) x`).Scan(&found); err != nil {
		t.Fatal(err)
	}
	t.Logf("finding by text on the reviewer's screen: %v, %d found", time.Since(began), found)
	if first > 60*time.Second {
		t.Errorf("a publication took %v, inside one of five connections", first)
	}
}
