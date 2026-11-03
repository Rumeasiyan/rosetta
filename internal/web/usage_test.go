package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/sheet"
	"github.com/Rumeasiyan/rosetta/internal/usage"
	"github.com/jackc/pgx/v5"
)

// usageWorld has two systems with an owner each, a record today for both and a total for one.
func (w *world) usageWorld() (admin, owner *browser, mine, theirs int64) {
	w.t.Helper()
	admin, _ = w.signedInAdmin()
	w.freshSystems()
	w.t.Cleanup(w.freshSystems)
	w.exec(`INSERT INTO source (name, version, state, seen_at) VALUES ('usage-site-a', '1', 'on', now()), ('usage-site-b', '1', 'on', now()) ON CONFLICT (name) DO NOTHING`)
	w.t.Cleanup(func() { w.exec(`DELETE FROM source WHERE name LIKE 'usage-site-%'`) })
	ownerSubject := subject("usageowner")
	ctx := context.Background()
	for _, ins := range []struct {
		ref, who string
		id       *int64
	}{{"USE-MINE", ownerSubject, &mine}, {"USE-THEIRS", "someone-else", &theirs}} {
		if err := w.pool.QueryRow(ctx, `INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at) VALUES ($1, $1, $2, 'live', now()) RETURNING id`, ins.ref, ins.who).Scan(ins.id); err != nil {
			w.t.Fatal(err)
		}
	}
	owner = w.browser()
	owner.signIn(ownerSubject)
	keep := func(ref string, caller int64, r usage.Record) {
		r.Ref, r.CallerID = ref, caller
		if err := usage.Keep(ctx, w.pool, r); err != nil {
			w.t.Fatal(err)
		}
	}
	base := usage.Record{Operation: usage.OpTranslate, From: "en", To: "ta", Sensitivity: "ordinary", Passages: 1, Characters: 50, DurationMS: 800, Outcome: usage.Answered}
	a := base
	a.Pieces, a.Sources, a.Mismatch = usage.Pieces{Source: 3, Glossary: 1}, []string{"usage-site-a", "usage-site-b"}, true
	keep(subject("r-a"), mine, a)
	f := base
	f.Outcome, f.DurationMS = "no provider", 3000
	keep(subject("r-b"), mine, f)
	g := base
	g.Operation, g.From, g.To, g.Pieces.Memory = usage.OpMemoryAdd, "ta", "si", 2
	keep(subject("r-c"), theirs, g)
	return admin, owner, mine, theirs
}

func TestUsageShowsWhatRRec03Asks(t *testing.T) {
	w := newWorld(t)
	admin, _, mine, _ := w.usageWorld()
	p := admin.get("/usage?group=system")
	if p.status != 200 || !strings.Contains(p.body, "USE-MINE") || !strings.Contains(p.body, "USE-THEIRS") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	for _, want := range []string{"Pieces from the glossary", "Sensitivity mismatches", "95 of each 100 took", "under 6 seconds", "No provider"} {
		if !strings.Contains(p.body, want) {
			t.Errorf("no %q", want)
		}
	}
	// By each way R-REC-03 names, and each filter.
	for path, want := range map[string]string{
		"/usage?group=day":                         time.Now().In(colombo).Format(time.DateOnly),
		"/usage?group=operation":                   "Added to memory",
		"/usage?group=direction":                   "தமிழ் → සිංහල",
		"/usage?group=source":                      "usage-site-b",
		"/usage?group=day&source=usage-site-b":     "usage-site-b",
		"/usage?group=operation&from=ta&to=si":     "Added to memory",
		"/usage?group=system&system=" + itoa(mine): "USE-MINE",
	} {
		if p := admin.get(path); p.status != 200 || !strings.Contains(p.body, want) {
			t.Errorf("%s: %d, no %q", path, p.status, want)
		}
	}
	// A filter keeps out what it should.
	if p := admin.get("/usage?group=system&system=" + itoa(mine)); strings.Contains(p.body, "<strong>USE-THEIRS") {
		t.Error("another system is in the view")
	}
	if p := admin.get("/usage?group=direction&operation=translate"); strings.Contains(p.body, "தமிழ் →") {
		t.Error("another operation is in the view")
	}
	// In the other languages too.
	for _, lang := range []string{"si", "ta"} {
		admin.get("/lang/" + lang)
		if p := admin.get("/usage?group=operation"); p.status != 200 || strings.Contains(p.body, "Pieces from") {
			t.Errorf("%s: %d", lang, p.status)
		}
	}
	admin.get("/lang/en")
}

// A view of what the totals hold, joined with what they do not: nothing counts twice.
func TestUsageJoinsTotalsAndToday(t *testing.T) {
	w := newWorld(t)
	admin, _, mine, _ := w.usageWorld()
	ctx := context.Background()
	yesterday := time.Now().In(colombo).AddDate(0, 0, -1).Format(time.DateOnly)
	w.exec(`INSERT INTO daily_total (day, caller_id, operation, from_language, to_language, outcome, source, requests, characters, pieces_source, time_buckets)
		VALUES ($1::date, $2, 'translate', 'en', 'ta', 'answered', '', 7, 700, 7, '{"under_1s": 7}')`, yesterday, mine)
	f := usage.Filter{Group: usage.ByDay, Since: time.Now().AddDate(0, 0, -3), Until: time.Now().AddDate(0, 0, 1), AllCallers: true}
	v, err := usage.Make(ctx, w.pool, f)
	if err != nil {
		t.Fatal(err)
	}
	if v.Whole.Requests != 10 || len(v.Rows) != 2 || v.Rows[1].Key != yesterday || v.Rows[1].Requests != 7 {
		t.Fatalf("%+v", v)
	}
	// After the night's work, the same view gives the same numbers.
	if err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error { _, err := usage.AddTotals(ctx, tx); return err }); err != nil {
		t.Fatal(err)
	}
	again, err := usage.Make(ctx, w.pool, f)
	if err != nil || again.Whole.Requests != 10 {
		t.Fatalf("%+v %v", again, err)
	}
	if p := admin.get("/usage?group=day&since=" + yesterday); !strings.Contains(p.body, yesterday) {
		t.Fatal(p.body)
	}
}

// A system owner sees only their own systems (R-REC-03), and asking for another's is a denial.
func TestUsageForAnOwnerIsTheirOwn(t *testing.T) {
	w := newWorld(t)
	_, owner, mine, theirs := w.usageWorld()
	p := owner.get("/usage?group=system")
	if p.status != 200 || !strings.Contains(p.body, "USE-MINE") || strings.Contains(p.body, "USE-THEIRS") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	if p := owner.get("/usage?group=system&system=" + itoa(theirs)); p.status != 403 {
		t.Fatalf("another's: %d", p.status)
	}
	if p := owner.get("/usage/export?group=system&system=" + itoa(theirs)); p.status != 403 {
		t.Fatalf("another's export: %d", p.status)
	}
	if p := owner.get("/usage?group=operation"); strings.Contains(p.body, "<strong>Added to memory") {
		t.Fatal("another system's operation shows")
	}
	if p := owner.get("/usage/export?group=system&system=" + itoa(mine)); p.status != 200 {
		t.Fatalf("own export: %d", p.status)
	}
	nobody := w.browser()
	nobody.signIn(subject("officer"))
	if p := nobody.get("/usage"); p.status != 403 {
		t.Fatalf("no role: %d", p.status)
	}
	if p := nobody.get("/usage/export"); p.status != 403 {
		t.Fatalf("no role: %d", p.status)
	}
}

func TestABadViewIsRefusedWithoutRepeatingIt(t *testing.T) {
	w := newWorld(t)
	admin, _, _, _ := w.usageWorld()
	const marker = "VIEWMARKER"
	for _, q := range []string{"since=" + marker, "until=2020-01-01&since=2021-01-01", "group=" + marker, "operation=" + marker, "from=" + marker, "source=" + marker, "system=" + marker} {
		p := admin.get("/usage?" + q)
		if p.status != 200 || strings.Contains(p.body, marker) || !strings.Contains(p.body, "could not be made") {
			t.Errorf("%s: %d", q, p.status)
		}
	}
	if strings.Contains(w.logs.String(), marker) {
		t.Fatal(w.logs)
	}
}

// Any view exports as a spreadsheet file, with an audit entry and no usage figures sent to PENSIEVE.
func TestUsageExport(t *testing.T) {
	w := newWorld(t)
	admin, _, _, _ := w.usageWorld()
	w.exec(`DELETE FROM audit_outbox`)
	p := admin.get("/usage/export?group=system")
	if p.status != 200 || !strings.Contains(p.header.Get("Content-Disposition"), "rosetta-usage-") {
		t.Fatalf("%d %v", p.status, p.header)
	}
	rows, err := sheet.Read([]byte(p.body), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0].Cells[0] != "Calling system" || !strings.HasPrefix(rows[1].Cells[0], "USE-") || rows[3].Cells[0] != "The whole view" || rows[3].Cells[1] != "3" {
		t.Fatalf("%+v", rows)
	}
	got := w.outbox()
	if len(got) != 1 || got[0].Action != "export" || got[0].ObjectType != "usage view" {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got[0].Raw, "USE-MINE") || strings.Contains(got[0].Raw, "request_ref") {
		t.Fatal(got[0].Raw)
	}
}
