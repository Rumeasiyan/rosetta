package alerts_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/alerts"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/flags"
	"github.com/Rumeasiyan/rosetta/internal/hedwig"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/standin"
	"github.com/Rumeasiyan/rosetta/internal/usage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type world struct {
	t      *testing.T
	pool   *pgxpool.Pool
	stand  *httptest.Server
	list   bumblebee.List
	fresh  bool
	readAt time.Time
	policy sources.Policy
	values settings.Values
	w      *alerts.Watcher
}

func newWorld(t *testing.T) *world {
	t.Helper()
	pool := dbtest.Open(t)
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	x := &world{t: t, pool: pool, stand: ts, fresh: true, readAt: time.Now(), values: settings.Defaults()}
	x.wipe()
	t.Cleanup(x.wipe)
	x.restart()
	return x
}

func (x *world) wipe() {
	x.t.Helper()
	ctx := context.Background()
	for _, sql := range []string{`DELETE FROM alert`, `DELETE FROM alert_state`, `DELETE FROM usage_record`, `DELETE FROM daily_total`, `DELETE FROM memory_entry`, `DELETE FROM caller_origin`, `DELETE FROM caller_key`, `DELETE FROM caller`,
		`DELETE FROM cache_piece`, `DELETE FROM gate_decision`, `DELETE FROM source_order`, `DELETE FROM source_offer`, `DELETE FROM source`, `UPDATE role_grant SET ended_at = now() WHERE ended_at IS NULL`} {
		if _, err := x.pool.Exec(ctx, sql); err != nil {
			x.t.Fatal(sql, err)
		}
	}
}

// restart is the service starting again: a new Watcher, the states still kept.
func (x *world) restart() {
	x.w = &alerts.Watcher{Pool: x.pool, Snapshot: func(context.Context) sources.Snapshot {
		return sources.Snapshot{List: x.list, Fresh: x.fresh, ReadAt: x.readAt}
	}, Policy: func(context.Context) sources.Policy { return x.policy }, Settings: func(context.Context) settings.Values { return x.values }}
}

func (x *world) setList(states map[string]string) {
	x.t.Helper()
	x.list = bumblebee.List{}
	for name, state := range states {
		s := bumblebee.Source{Name: name, Version: "1", State: state}
		s.Offers = []bumblebee.Offer{{Operation: bumblebee.OpTranslate, From: "en", To: "ta"}, {Operation: bumblebee.OpGrammar, From: "en"}}
		x.list.Sources = append(x.list.Sources, s)
	}
	if err := sources.Mirror(context.Background(), x.pool, x.list, time.Now()); err != nil {
		x.t.Fatal(err)
	}
}

func (x *world) kinds() []string {
	x.t.Helper()
	rows, err := x.pool.Query(context.Background(), `SELECT kind FROM alert ORDER BY id`)
	if err != nil {
		x.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		x.t.Fatal(err)
	}
	return out
}

func (x *world) person(name string, admin bool) int64 {
	x.t.Helper()
	ctx := context.Background()
	var id int64
	if err := x.pool.QueryRow(ctx, `INSERT INTO person (mjolnir_subject, name, email, screen_language) VALUES ($1, $1, $1 || '@example.invalid', 'ta')
		ON CONFLICT (mjolnir_subject) DO UPDATE SET name = EXCLUDED.name RETURNING id`, name).Scan(&id); err != nil {
		x.t.Fatal(err)
	}
	if admin {
		if _, err := x.pool.Exec(ctx, `INSERT INTO role_grant (person_id, role) VALUES ($1, 'administrator')`, id); err != nil {
			x.t.Fatal(err)
		}
	}
	return id
}

func (x *world) system(ref, owner string) int64 {
	x.t.Helper()
	var id int64
	if err := x.pool.QueryRow(context.Background(), `INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at) VALUES ($1, $1, NULLIF($2, ''), 'live', now()) RETURNING id`, ref, owner).Scan(&id); err != nil {
		x.t.Fatal(err)
	}
	return id
}

func want(t *testing.T, got []string, kinds ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(kinds, "|") {
		t.Fatalf("alerts %v, want %v", got, kinds)
	}
}

// One alert when a state changes (E-ADM-06's note), and a restart tells nobody twice.
func TestOneAlertForEachChange(t *testing.T) {
	x := newWorld(t)
	ctx := context.Background()
	x.setList(map[string]string{"site-a": bumblebee.StateOn})
	x.w.Tick(ctx)
	x.w.Tick(ctx)
	want(t, x.kinds())

	// A source paused, seen many times: one alert. Grammar then has nowhere to go: one more.
	x.setList(map[string]string{"site-a": bumblebee.StateRestingBlock})
	x.w.Tick(ctx)
	x.w.Tick(ctx)
	x.restart()
	x.w.Tick(ctx)
	want(t, x.kinds(), alerts.SourcePaused, alerts.NoSource)

	// It resumes: one alert, and the direction is fine again, needing none.
	x.setList(map[string]string{"site-a": bumblebee.StateOn})
	x.w.Tick(ctx)
	x.w.Tick(ctx)
	want(t, x.kinds(), alerts.SourcePaused, alerts.NoSource, alerts.SourceResumed)

	// BUMBLEBEE stops: one alert, however long, with no alert per direction.
	x.fresh, x.readAt = false, time.Now().Add(-5*time.Minute)
	x.w.Tick(ctx)
	x.w.Tick(ctx)
	x.restart()
	x.w.Tick(ctx)
	want(t, x.kinds(), alerts.SourcePaused, alerts.NoSource, alerts.SourceResumed, alerts.BumblebeeDown)

	x.fresh, x.readAt = true, time.Now()
	x.w.Tick(ctx)
	x.w.Tick(ctx)
	want(t, x.kinds(), alerts.SourcePaused, alerts.NoSource, alerts.SourceResumed, alerts.BumblebeeDown, alerts.BumblebeeBack)
}

// Starting up with nothing read yet is not BUMBLEBEE stopping.
func TestStartingUpIsNotAnOutage(t *testing.T) {
	x := newWorld(t)
	x.fresh, x.readAt = false, time.Time{}
	x.w.Tick(context.Background())
	want(t, x.kinds())
	// After its whole age with nothing read, it is.
	clock := time.Now().Add(30 * time.Second)
	x.w.Now = func() time.Time { return clock }
	x.w.Tick(context.Background())
	want(t, x.kinds())
	clock = clock.Add(2 * time.Minute)
	x.w.Tick(context.Background())
	want(t, x.kinds(), alerts.BumblebeeDown)
}

// A direction only counts when it has been opened: closed at the gate is the gate's own state.
func TestADirectionClosedAtTheGateIsNoAlert(t *testing.T) {
	x := newWorld(t)
	ctx := context.Background()
	x.setList(map[string]string{"site-a": bumblebee.StateOn})
	x.w.Tick(ctx)
	// Only for translation: open it, then the one source rests.
	s, _ := x.list.Find("site-a")
	x.policy = sources.Policy{Gates: map[sources.GateKey]sources.Gate{{Source: "site-a", From: "en", To: "ta"}: {State: sources.GateOpen, SourceVersion: s.Version, TestSet: "shared-1", Score: 80, EvaluatedOn: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}}}
	x.w.Tick(ctx)
	want(t, x.kinds())
	x.setList(map[string]string{"site-a": bumblebee.StateRestingChange})
	x.w.Tick(ctx)
	rows, err := x.pool.Query(ctx, `SELECT subject FROM alert WHERE kind = $1 ORDER BY id`, alerts.NoSource)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "translate en-ta,grammar en" {
		t.Fatalf("%v", got)
	}
}

func (x *world) records(caller int64, op, outcome string, n int) {
	x.t.Helper()
	for i := 0; i < n; i++ {
		ref := "r-alert-" + time.Now().Format("150405.000000000") + "-" + outcome
		if err := usage.Keep(context.Background(), x.pool, usage.Record{Ref: ref, CallerID: caller, Operation: op, Outcome: outcome}); err != nil {
			x.t.Fatal(err)
		}
	}
}

// Failures past the setting in an hour, for one system, once. Exactly at it is not past it; refusals
// and tests are not failures.
func TestFailuresPassingTheLimit(t *testing.T) {
	x := newWorld(t)
	ctx := context.Background()
	x.setList(map[string]string{"site-a": bumblebee.StateOn})
	id := x.system("ALERT-1", "")
	x.records(id, usage.OpTranslate, usage.Answered, 4)
	x.records(id, usage.OpTranslate, "no provider", 1) // 20 percent exactly
	x.records(id, usage.OpTranslate, "over limit", 5)
	x.records(id, usage.OpTest, "no provider", 10)
	x.w.Tick(ctx)
	want(t, x.kinds())
	x.records(id, usage.OpTranslate, "timed out", 2) // now 3 in 12
	x.w.Tick(ctx)
	x.w.Tick(ctx)
	want(t, x.kinds(), alerts.FailuresOver)
	var caller int64
	if err := x.pool.QueryRow(ctx, `SELECT caller_id FROM alert`).Scan(&caller); err != nil || caller != id {
		t.Fatal(caller, err)
	}
	// Under again, and over again later, is a new change.
	x.records(id, usage.OpTranslate, usage.Answered, 40)
	x.w.Tick(ctx)
	want(t, x.kinds(), alerts.FailuresOver)
	x.records(id, usage.OpTranslate, "masking lost", 40)
	x.w.Tick(ctx)
	want(t, x.kinds(), alerts.FailuresOver, alerts.FailuresOver)
	// The setting moves the line.
	x.wipe()
	x.setList(map[string]string{"site-a": bumblebee.StateOn})
	id = x.system("ALERT-2", "")
	x.records(id, usage.OpTranslate, usage.Answered, 3)
	x.records(id, usage.OpTranslate, "no provider", 1)
	x.values[settings.FailureAlertPercent.Name] = 50
	x.restart()
	x.w.Tick(ctx)
	want(t, x.kinds())
}

// A notice goes to the live administrators and the system's own owner, with no words of anyone's in it.
func TestWhoIsTold(t *testing.T) {
	x := newWorld(t)
	ctx := context.Background()
	x.person("alert-admin", true)
	x.person("alert-owner", false)
	x.person("alert-other", false)
	id := x.system("ALERT-3", "alert-owner")
	x.system("ALERT-4", "alert-other")
	n := &alerts.Notifier{Pool: x.pool, PublicURL: "http://rosetta.example.invalid", Sender: hedwig.Client{URL: x.stand.URL + "/hedwig", Key: standin.HedwigTestKey}}
	if err := alerts.Raise(ctx, x.pool, alerts.Alert{Kind: alerts.FailuresOver, CallerID: id}); err != nil {
		t.Fatal(err)
	}
	if err := alerts.Raise(ctx, x.pool, alerts.Alert{Kind: alerts.NoSource, Subject: "translate en-ta"}); err != nil {
		t.Fatal(err)
	}
	sent, err := n.Send(ctx, x.pool)
	if err != nil || sent != 2 {
		t.Fatal(sent, err)
	}
	notices := x.hedwig()
	if len(notices) != 2 {
		t.Fatalf("%+v", notices)
	}
	first, second := notices[0], notices[1]
	if first.Type != "rosetta_failures_over" || !strings.Contains(first.RequestKey, "rosetta-alert-") || first.Details["system"] != "ALERT-3" || !strings.Contains(first.Details["link"], "/systems/") {
		t.Fatalf("%+v", first)
	}
	if got := emails(first); got != "alert-admin@example.invalid,alert-owner@example.invalid" {
		t.Fatal(got)
	}
	if first.People[0].Language != "ta" {
		t.Fatalf("%+v", first.People[0])
	}
	// About no system, it goes to the administrator alone.
	if got := emails(second); got != "alert-admin@example.invalid" || second.Details["about"] != "translate en-ta" {
		t.Fatalf("%s %+v", got, second)
	}
	// Sent once, and not again.
	if sent, err = n.Send(ctx, x.pool); err != nil || sent != 0 {
		t.Fatal(sent, err)
	}
	var unsent int
	if err := x.pool.QueryRow(ctx, `SELECT count(*) FROM alert WHERE sent_at IS NULL OR hedwig_notice_id IS NULL`).Scan(&unsent); err != nil || unsent != 0 {
		t.Fatal(unsent, err)
	}
}

// What cannot go waits, and goes with the catch-up; with nobody to tell it is not tried for ever.
func TestWhatCannotGoWaits(t *testing.T) {
	x := newWorld(t)
	ctx := context.Background()
	n := &alerts.Notifier{Pool: x.pool, Sender: hedwig.Client{URL: x.stand.URL + "/hedwig", Key: standin.HedwigTestKey}}
	if err := alerts.Raise(ctx, x.pool, alerts.Alert{Kind: alerts.BumblebeeDown}); err != nil {
		t.Fatal(err)
	}
	x.person("alert-admin-2", true)
	x.hedwigMode("internal")
	sent, err := n.Send(ctx, x.pool)
	if err != nil || sent != 0 {
		t.Fatal(sent, err)
	}
	x.hedwigMode("normal")
	if err := pgx.BeginFunc(ctx, x.pool, func(tx pgx.Tx) error {
		var err error
		sent, err = n.Job(ctx, tx, "run")
		return err
	}); err != nil || sent != 1 {
		t.Fatal(sent, err)
	}
	if _, err := x.pool.Exec(ctx, `UPDATE role_grant SET ended_at = now() WHERE ended_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if err := alerts.Raise(ctx, x.pool, alerts.Alert{Kind: alerts.BumblebeeBack}); err != nil {
		t.Fatal(err)
	}
	if sent, err = n.Send(ctx, x.pool); err != nil || sent != 0 {
		t.Fatal(sent, err)
	}
	var unsent int
	if err := x.pool.QueryRow(ctx, `SELECT count(*) FROM alert WHERE sent_at IS NULL`).Scan(&unsent); err != nil || unsent != 0 {
		t.Fatal(unsent, err)
	}
}

// Flags tell once: a system, a language, a person, each when it becomes so.
func TestFlagsTellOnce(t *testing.T) {
	x := newWorld(t)
	ctx := context.Background()
	id := x.system("ALERT-5", "")
	run := func(c flags.Changes) {
		t.Helper()
		if err := pgx.BeginFunc(ctx, x.pool, func(tx pgx.Tx) error { return alerts.Flags(ctx, tx, c) }); err != nil {
			t.Fatal(err)
		}
	}
	run(flags.Changes{Systems: []flags.SystemChange{{ID: id, Old: "none", New: "idle"}}, Languages: []string{"ta"}})
	want(t, x.kinds(), alerts.CallerFlagged, alerts.LanguageFlagged)
	run(flags.Changes{Languages: []string{"ta"}})
	want(t, x.kinds(), alerts.CallerFlagged, alerts.LanguageFlagged)
	run(flags.Changes{Systems: []flags.SystemChange{{ID: id, Old: "idle", New: "none"}}})
	run(flags.Changes{Languages: []string{"ta"}})
	want(t, x.kinds(), alerts.CallerFlagged, alerts.LanguageFlagged, alerts.LanguageFlagged)

	person := x.person("alert-flagged", false)
	state := func(flagged bool) {
		t.Helper()
		if err := pgx.BeginFunc(ctx, x.pool, func(tx pgx.Tx) error { return alerts.PersonState(ctx, tx, person, flagged) }); err != nil {
			t.Fatal(err)
		}
	}
	state(true)
	state(true)
	state(false)
	var n int
	var subject string
	if err := x.pool.QueryRow(ctx, `SELECT count(*), max(subject) FROM alert WHERE kind = $1`, alerts.PersonFlagged).Scan(&n, &subject); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if strings.Contains(subject, "alert-flagged") || !strings.HasPrefix(subject, "rosetta-person-") {
		t.Fatal(subject)
	}
}

type notice struct {
	Type       string            `json:"notice_type"`
	RequestKey string            `json:"request_key"`
	Details    map[string]string `json:"details"`
	People     []hedwig.Person   `json:"people"`
}

func emails(n notice) string {
	out := make([]string, len(n.People))
	for i, p := range n.People {
		out[i] = p.Email
	}
	return strings.Join(out, ",")
}

func (x *world) hedwig() []notice {
	x.t.Helper()
	res, err := http.Get(x.stand.URL + "/control/hedwig/received")
	if err != nil {
		x.t.Fatal(err)
	}
	defer res.Body.Close()
	var got struct {
		Received []standin.Received `json:"received"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		x.t.Fatal(err)
	}
	var out []notice
	for _, r := range got.Received {
		var n notice
		if err := json.Unmarshal([]byte(r.Body), &n); err != nil {
			x.t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func (x *world) hedwigMode(mode string) {
	x.t.Helper()
	res, err := http.Post(x.stand.URL+"/control/hedwig/mode", "application/json", strings.NewReader(`{"mode":"`+mode+`"}`))
	if err != nil {
		x.t.Fatal(err)
	}
	res.Body.Close()
}
