package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/api"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/grammar"
	"github.com/Rumeasiyan/rosetta/internal/memory"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/standin"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/Rumeasiyan/rosetta/internal/usage"
	"github.com/jackc/pgx/v5"
)

// withTester gives the screens the same pipeline a calling system has, against the stand-in, with
// nothing kept: as cmd/rosetta wires it.
func (w *world) withTester() {
	w.t.Helper()
	ctx := context.Background()
	values := func(context.Context) settings.Values { return settings.Defaults() }
	client := bumblebee.Client{URL: w.standin.URL + "/bumblebee", Key: standin.BumblebeeKey}
	book := &sources.Book{Read: client.Sources, MaxAge: func(context.Context) time.Duration { return time.Minute },
		Kept: func(ctx context.Context, l bumblebee.List, at time.Time) error {
			return sources.Mirror(ctx, w.pool, l, at)
		}}
	if err := book.Refresh(ctx); err != nil {
		w.t.Fatal(err)
	}
	policy := func(ctx context.Context) sources.Policy {
		p, err := sources.LoadPolicy(ctx, w.pool)
		if err != nil {
			w.t.Fatal(err)
		}
		return p
	}
	fallback := &sources.Fallback{Book: book, Provider: client, Grammars: client, Policy: policy}
	cache := translate.Cache{Q: w.pool, Settings: values}
	calls := api.New(api.Deps{
		TestTranslator: &translate.Service{Memory: memory.Served{Q: w.pool}, Cache: cache, Sources: fallback, Settings: values},
		Grammar:        &grammar.Service{Sources: fallback, Settings: values},
		Usage:          func(ctx context.Context, r usage.Record) error { return usage.Keep(ctx, w.pool, r) },
	})
	w.app.deps.Tester = calls
	w.app.deps.Sources = book
}

// R-ADM-04: the owner or the administrator sends invented text and reads the full answer, and it
// counts as a test, with the text nowhere (E-ADM-05's note; Annex A).
func TestATestFromTheScreens(t *testing.T) {
	w := newWorld(t)
	admin, owner, mine, theirs := w.usageWorld()
	w.withTester()
	w.exec(`DELETE FROM cache_piece`)
	w.exec(`DELETE FROM gate_decision`)
	t.Cleanup(func() {
		w.exec(`DELETE FROM gate_decision`)
		w.exec(`DELETE FROM cache_piece`)
		w.exec(`DELETE FROM source_offer`)
		w.exec(`DELETE FROM source WHERE name LIKE 'standin-%'`)
	})
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		_, err := sources.OpenGate(ctx, tx, "standin-translate", sources.Translate("en", "ta"), sources.Evaluation{TestSet: "shared-1", Score: 80, Reason: "An invented reason.", EvaluatedOn: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}, w.personIDOf(owner))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	const marker = "INVENTEDTESTTEXT"
	send := func(b *browser, id int64, form url.Values) page {
		t.Helper()
		form.Set("csrf", b.csrf())
		return b.post(sysPath(id)+"/test", form)
	}
	p := owner.get(sysPath(mine) + "/test")
	if p.status != 200 || !strings.Contains(p.body, "invented text only") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	form := url.Values{"operation": {"translate"}, "from": {"en"}, "to": {"ta"}, "sensitivity": {"ordinary"}, "text": {"A letter about " + marker + "."}}
	p = send(owner, mine, form)
	if p.status != 200 || !strings.Contains(p.body, "It was answered") || !strings.Contains(p.body, "standin-translate") || !strings.Contains(p.body, "machine") || !strings.Contains(p.body, "fallback_used") || !strings.Contains(p.body, "request_ref") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	// It is kept nowhere: not in the cache, a record, the outbox or a log.
	rows, err := w.pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		var n int
		if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()+` t WHERE t::text LIKE '%`+marker+`%'`).Scan(&n); err != nil {
			t.Fatal(table, err)
		}
		if n != 0 {
			t.Errorf("the text is in %s", table)
		}
	}
	if strings.Contains(w.logs.String(), marker) {
		t.Fatal(w.logs)
	}
	var op, outcome string
	var person *int64
	var pieces int
	if err := w.pool.QueryRow(ctx, `SELECT operation, outcome, person_id, pieces_source FROM usage_record WHERE operation = 'test' AND caller_id = $1 ORDER BY id DESC LIMIT 1`, mine).Scan(&op, &outcome, &person, &pieces); err != nil {
		t.Fatal(err)
	}
	if op != "test" || outcome != "answered" || person == nil || *person != w.personIDOf(owner) || pieces != 1 {
		t.Fatal(op, outcome, person, pieces)
	}
	// It shows sources and fallback when the first source fails.
	w.exec(`INSERT INTO gate_decision (source_id, from_language, to_language, source_version, state, test_set_version, score, evaluated_on, reason, decided_by)
		SELECT s.id, 'en', 'ta', s.version, 'open', 'shared-1', 80, '2020-01-01', 'An invented reason.', $1 FROM source s WHERE s.name = 'standin-translate-b'`, w.personIDOf(owner))
	w.control("bumblebee", "blocked", map[string]string{"source": "standin-translate"})
	p = send(admin, theirs, form)
	if p.status != 200 || !strings.Contains(p.body, "standin-translate-b") || !strings.Contains(p.body, "Fallback used: <strong>Yes") ||
		!strings.Contains(p.body, `role="note">Machine translation through standin-translate-b, check before issue</p>`) {
		t.Fatalf("%d %s", p.status, p.body)
	}
	w.control("bumblebee", "normal", nil)
	// A check, with its suggestions and the source that made them.
	p = send(owner, mine, url.Values{"operation": {"grammar"}, "text": {"We recieve " + marker + "."}})
	if p.status != 200 || !strings.Contains(p.body, "standin-grammar") || !strings.Contains(p.body, "Possible spelling mistake") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	// A refusal shows the reason and the whole answer.
	form.Set("sensitivity", "confidential")
	if p = send(owner, mine, form); !strings.Contains(p.body, "No provincial provider") || !strings.Contains(p.body, "messages") || strings.Contains(w.logs.String(), marker) {
		t.Fatalf("%d %s", p.status, p.body)
	}
	// An owner reaches only their own; no role, none.
	if p := owner.get(sysPath(theirs) + "/test"); p.status != 403 {
		t.Fatalf("another's: %d", p.status)
	}
	if p := send(owner, theirs, form); p.status != 403 {
		t.Fatalf("another's: %d", p.status)
	}
	nobody := w.browser()
	nobody.signIn(subject("officer"))
	if p := nobody.get(sysPath(mine) + "/test"); p.status != 403 {
		t.Fatalf("no role: %d", p.status)
	}
	// In each language.
	for _, lang := range []string{"si", "ta"} {
		owner.get("/lang/" + lang)
		if p := owner.get(sysPath(mine) + "/test"); p.status != 200 || strings.Contains(p.body, "invented text only") {
			t.Errorf("%s: %d", lang, p.status)
		}
	}
	owner.get("/lang/en")
}

// personIDOf reads who a browser is signed in as, from its home page's session.
func (w *world) personIDOf(b *browser) int64 {
	w.t.Helper()
	u := b.get("/")
	if u.status != 200 {
		w.t.Fatal(u.status)
	}
	w.app.sessions.mu.Lock()
	defer w.app.sessions.mu.Unlock()
	for _, c := range b.jar.Cookies(mustURL(w.t, w.screens.URL)) {
		if c.Name == SessionCookie {
			if s, ok := w.app.sessions.byToken[c.Value]; ok {
				return s.personID
			}
		}
	}
	w.t.Fatal("not signed in")
	return 0
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
