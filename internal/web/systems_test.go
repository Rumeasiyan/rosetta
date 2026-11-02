package web

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/paradox/sign"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/Rumeasiyan/rosetta/internal/tva"
)

// ---- no database: who may act for a system, and what the screens send --------

func TestManagesFollowsTheSubjectAlone(t *testing.T) {
	sys := callers.Caller{OwnerSubject: "dev-owner"}
	admin := &current{grants: []people.Grant{{Role: people.Administrator}}}
	owner := &current{sess: session{subject: "dev-owner", email: "someone@example.invalid"}}
	other := &current{sess: session{subject: "dev-other", email: "dev-owner"}}
	keeper := &current{sess: session{subject: "dev-keeper"}, grants: []people.Grant{{Role: people.GlossaryKeeper}}}
	if !manages(admin, sys) || !manages(owner, sys) {
		t.Fatal("the administrator and the owner act for a system")
	}
	if manages(other, sys) || manages(keeper, sys) {
		t.Fatal("nobody else does, even with the subject as their email")
	}
	vacant := callers.Caller{}
	if manages(&current{}, vacant) || manages(other, vacant) {
		t.Fatal("no owner means nobody but the administrator, never someone with no subject")
	}
	if !manages(admin, vacant) {
		t.Fatal("the administrator acts for a system with no owner (R-CAL-02)")
	}
}

func TestSystemsScreenFollowsTheRole(t *testing.T) {
	admin := &current{grants: []people.Grant{{Role: people.Administrator}}}
	owner := &current{owned: []people.OwnedSystem{{Reference: "TEST-0001"}}}
	if !seesSystems(admin) || !seesSystems(owner) || seesSystems(&current{}) {
		t.Fatal("seesSystems")
	}
	has := func(c *current, id string) bool {
		for _, n := range nav(c) {
			if n.ID == id {
				return true
			}
		}
		return false
	}
	if !has(admin, "systems") || !has(owner, "systems") || has(&current{}, "systems") || has(owner, "people") {
		t.Fatal("the bar offers the wrong screens")
	}
}

func someSystems() []callers.Caller {
	at := time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)
	return []callers.Caller{
		{ID: 7, Reference: "TEST-0001", Name: "Test letter editor", OwnerPostRef: "31", OwnerSubject: "dev-owner", TVAStatus: tva.StatusLive, Flag: callers.FlagNone, LastRequestAt: &at, PerMinute: 60, AtOnce: 5},
		{ID: 8, Reference: "TEST-0004", Name: "Test system with no owner", TVAStatus: tva.StatusMayJoin, Flag: callers.FlagNoOwner, PerMinute: 60, AtOnce: 5},
	}
}

func TestSystemsScreensInEachLanguage(t *testing.T) {
	for _, l := range i18n.Langs {
		body := render(t, "systems", Page{Lang: l, Path: "/systems", Title: "systems", Here: "systems", CSRF: "form-token", Viewer: &Viewer{Name: "Test"},
			Data: systemsData{Systems: someSystems(), Register: true}})
		for _, want := range []string{i18n.T(l, "registerTitle"), i18n.T(l, "flagNoOwner"), `href="/systems/7"`, "TEST-0004", `name="csrf" value="form-token"`, `<label for="reference">`, "2030-01-01 10:00", `action="/systems/register"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		owner := render(t, "systems", Page{Lang: l, Path: "/systems", Title: "systems", Viewer: &Viewer{Name: "Test"}, Data: systemsData{Systems: someSystems()[:1]}})
		if strings.Contains(owner, "/systems/register") {
			t.Errorf("%s: an owner is offered registration", l)
		}
		one := render(t, "system", Page{Lang: l, Path: "/systems/8", Title: "systems", Viewer: &Viewer{Name: "Test"}, Data: systemData{System: someSystems()[1]}})
		if !strings.Contains(one, i18n.T(l, "flagNoOwner")) || !strings.Contains(one, i18n.T(l, "postNotKnown")) {
			t.Errorf("%s: %s", l, one)
		}
	}
}

// ---- with the database -------------------------------------------------------

func (w *world) freshSystems() {
	w.t.Helper()
	w.exec(`DELETE FROM usage_record`)
	w.exec(`DELETE FROM daily_total`)
	w.exec(`DELETE FROM memory_entry`)
	w.exec(`DELETE FROM alert`)
	w.exec(`DELETE FROM caller_origin`)
	w.exec(`DELETE FROM caller_key`)
	w.exec(`DELETE FROM caller`)
}

func TestRegisteringFollowsTheRegister(t *testing.T) {
	const marker = "CALLERTEXTMARKER"
	w := newWorld(t)
	w.app.deps.TVA = tva.Dir("../../dev/tva")
	admin, who := w.signedInAdmin()
	w.freshSystems()
	t.Cleanup(w.freshSystems)
	w.exec(`DELETE FROM audit_outbox`)
	token := admin.csrf()

	reg := func(ref string) page {
		return admin.post("/systems/register", url.Values{"csrf": {token}, "reference": {ref}})
	}
	if p := reg("TEST-0001"); !strings.Contains(p.body, "has been registered") || !strings.Contains(p.body, "Test letter editor") {
		t.Fatalf("live: %d %s", p.status, p.body)
	}
	if p := reg("TEST-0002"); !strings.Contains(p.body, "has been registered") {
		t.Fatalf("may join: %d", p.status)
	}
	// A null owner subject is registered, and flagged.
	if p := reg("TEST-0003"); !strings.Contains(p.body, "flagged") {
		t.Fatalf("vacant: %s", p.body)
	}
	for _, ref := range []string{"TEST-0005", "TEST-0006", "TEST-0007"} {
		if p := reg(ref); !strings.Contains(p.body, "neither live nor may join") {
			t.Errorf("%s: %s", ref, p.body)
		}
	}
	if p := reg(marker); !strings.Contains(p.body, "no such reference") || strings.Contains(p.body, marker) {
		t.Errorf("unknown: %s", p.body)
	}
	if p := reg("TEST-0001"); !strings.Contains(p.body, "registered already") {
		t.Errorf("again: %s", p.body)
	}
	var n int
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM caller`).Scan(&n)
	if n != 3 {
		t.Fatalf("%d systems", n)
	}
	var flag, owner, status string
	if err := w.pool.QueryRow(context.Background(), `SELECT flag, COALESCE(owner_subject, ''), tva_status FROM caller WHERE tva_reference = 'TEST-0003'`).Scan(&flag, &owner, &status); err != nil {
		t.Fatal(err)
	}
	if flag != callers.FlagNoOwner || owner != "" || status != "live" {
		t.Fatal(flag, owner, status)
	}
	got := w.outbox()
	if len(got) != 3 || got[0].Action != "creation" || got[0].Actor != who || got[0].ObjectType != "calling system" || got[0].ObjectRef != "TEST-0001" {
		t.Fatalf("%+v", got)
	}
	for _, e := range got {
		if strings.Contains(e.Raw, marker) {
			t.Fatal("the outbox holds what was typed")
		}
	}
	if strings.Contains(w.logs.String(), marker) {
		t.Fatal("a log holds what was typed")
	}
}

// A register that cannot be read registers nothing, and says so.
func TestRegisteringWithoutTheFile(t *testing.T) {
	w := newWorld(t)
	w.app.deps.TVA = tva.Dir(t.TempDir())
	admin, _ := w.signedInAdmin()
	w.freshSystems()
	t.Cleanup(w.freshSystems)
	p := admin.post("/systems/register", url.Values{"csrf": {admin.csrf()}, "reference": {"TEST-0001"}})
	if !strings.Contains(p.body, "could not be read") {
		t.Fatalf("%d %s", p.status, p.body)
	}
}

// A system owner reaches their own system and no other; anyone else is denied.
func TestOwnerReachesOnlyTheirOwnSystems(t *testing.T) {
	w := newWorld(t)
	w.app.deps.TVA = tva.Dir("../../dev/tva")
	admin, _ := w.signedInAdmin()
	w.freshSystems()
	t.Cleanup(w.freshSystems)
	ownerSubject := subject("owner")
	var mine, theirs int64
	for _, ins := range []struct {
		ref, subject string
		id           *int64
	}{{"OWN-1", ownerSubject, &mine}, {"OTHER-1", "someone-else", &theirs}} {
		if err := w.pool.QueryRow(context.Background(), `INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at)
			VALUES ($1, $1, $2, 'live', now()) RETURNING id`, ins.ref, ins.subject).Scan(ins.id); err != nil {
			t.Fatal(err)
		}
	}
	owner := w.browser()
	owner.signIn(ownerSubject)
	if p := owner.get("/systems"); p.status != 200 || !strings.Contains(p.body, "OWN-1") || strings.Contains(p.body, "OTHER-1") || strings.Contains(p.body, "/systems/register") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	if p := owner.get(sysPath(mine)); p.status != 200 {
		t.Fatalf("own: %d", p.status)
	}
	if p := owner.get(sysPath(theirs)); p.status != 403 {
		t.Fatalf("another's: %d", p.status)
	}
	if p := owner.get("/systems/999999999"); p.status != 403 {
		t.Fatalf("one that does not exist tells an owner nothing: %d", p.status)
	}
	if p := owner.post("/systems/register", url.Values{"csrf": {owner.csrf()}, "reference": {"TEST-0001"}}); p.status != 403 {
		t.Fatalf("registering: %d", p.status)
	}
	if p := admin.get(sysPath(theirs)); p.status != 200 {
		t.Fatalf("the administrator: %d", p.status)
	}
	if p := admin.get("/systems/999999999"); p.status != 404 {
		t.Fatalf("not there: %d", p.status)
	}
	nobody := w.browser()
	nobody.signIn(subject("officer"))
	if p := nobody.get("/systems"); p.status != 403 {
		t.Fatalf("no role: %d", p.status)
	}
}

func sysPath(id int64) string { return "/systems/" + itoa(id) }

// ---- keys ---------------------------------------------------------------------

func TestSystemScreenListsKeysNeverShowingOne(t *testing.T) {
	at := time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)
	keys := []callers.Key{
		{ID: 3, Prefix: "rst_0123456789ab", Signs: true, CreatedAt: at, CreatedBy: "Test Owner", LastUsedAt: &at},
		{ID: 2, Prefix: "rst_ba9876543210", CreatedAt: at, TurnedOffAt: &at},
	}
	for _, l := range i18n.Langs {
		body := render(t, "system", Page{Lang: l, Path: "/systems/7", Title: "systems", CSRF: "form-token", Viewer: &Viewer{Name: "Test"},
			Data: systemData{System: someSystems()[0], Keys: keys}})
		for _, want := range []string{"rst_0123456789ab", i18n.T(l, "keySigns"), i18n.T(l, "keyOnThisServer"), i18n.T(l, "keyOff"), `action="/systems/7/keys/3/off"`, `action="/systems/7/keys"`, `<label for="public_key">`, "Test Owner"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		if strings.Contains(body, `action="/systems/7/keys/2/off"`) {
			t.Errorf("%s: a key already off offers turning off", l)
		}
		shown := render(t, "key-issued", Page{Lang: l, Path: "/systems/7/keys", Title: "keyIssued", Viewer: &Viewer{Name: "Test"},
			Data: keyShown{System: someSystems()[0], Key: "rst_thewholekey", Prefix: "rst_", Signs: true}})
		if !strings.Contains(shown, "rst_thewholekey") || !strings.Contains(shown, esc(i18n.T(l, "keyOnce"))) || !strings.Contains(shown, i18n.T(l, "keySignsNote")) {
			t.Errorf("%s: %s", l, shown)
		}
	}
}

// keyOf reads the one key from the screen that shows it.
func keyOf(t *testing.T, p page) string {
	t.Helper()
	m := keyOnPage.FindStringSubmatch(p.body)
	if m == nil {
		t.Fatalf("no key on the page: %d %s", p.status, p.body)
	}
	return m[1]
}

var keyOnPage = regexp.MustCompile(`readonly spellcheck="false">(rst_[^<]+)</textarea>`)

func TestKeysAreShownOnceTwoLiveAndTurnedOffAtOnce(t *testing.T) {
	w := newWorld(t)
	admin, who := w.signedInAdmin()
	w.freshSystems()
	t.Cleanup(w.freshSystems)
	w.exec(`DELETE FROM audit_outbox`)
	var id int64
	if err := w.pool.QueryRow(context.Background(), `INSERT INTO caller (tva_reference, name, tva_status, tva_checked_at) VALUES ('KEY-1', 'Invented', 'live', now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	token := admin.csrf()
	path := sysPath(id)
	first := keyOf(t, admin.post(path+"/keys", url.Values{"csrf": {token}}))
	pub, priv, err := sign.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	second := keyOf(t, admin.post(path+"/keys", url.Values{"csrf": {token}, "public_key": {pub}}))
	if first == second {
		t.Fatal("the same key twice")
	}
	// A third is refused until one is turned off.
	if p := admin.post(path+"/keys", url.Values{"csrf": {token}}); !strings.Contains(p.body, "two live keys") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	// A private key pasted by mistake is refused, and not repeated.
	if p := admin.post(path+"/keys", url.Values{"csrf": {token}, "public_key": {priv}}); !strings.Contains(p.body, "not a public key") || strings.Contains(p.body, priv) {
		t.Fatalf("%d %s", p.status, p.body)
	}
	// The list shows prefixes, never a key; the table holds only its hash.
	list := admin.get(path)
	if strings.Contains(list.body, first) || strings.Contains(list.body, second) || !strings.Contains(list.body, first[:16]) {
		t.Fatal(list.body)
	}
	var kept int
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM caller_key WHERE key_hash = $1 AND public_key IS NULL`, callers.HashKey(first)).Scan(&kept)
	if kept != 1 {
		t.Fatalf("%d", kept)
	}
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM caller_key WHERE key_hash = $1 OR prefix = $1`, first).Scan(&kept)
	if kept != 0 {
		t.Fatal("the key itself is stored")
	}
	// Turned off, it stops at once, and the room comes back.
	h, err := callers.FindKey(context.Background(), w.pool, callers.HashKey(first))
	if err != nil || h.TurnedOff || h.Caller.Reference != "KEY-1" {
		t.Fatal(h, err)
	}
	if p := admin.post(path+"/keys/"+itoa(h.KeyID)+"/off", url.Values{"csrf": {token}}); !strings.Contains(p.body, "turned off") {
		t.Fatal(p.body)
	}
	if h, err = callers.FindKey(context.Background(), w.pool, callers.HashKey(first)); err != nil || !h.TurnedOff {
		t.Fatal(h, err)
	}
	if p := admin.post(path+"/keys/"+itoa(h.KeyID)+"/off", url.Values{"csrf": {token}}); !strings.Contains(p.body, "not live") {
		t.Fatal("twice", p.body)
	}
	keyOf(t, admin.post(path+"/keys", url.Values{"csrf": {token}}))
	// Each issue and turn-off is an entry, with neither key nor hash in it.
	got := w.outbox()
	if len(got) != 4 || got[0].Action != "key change" || got[0].Actor != who || got[0].ObjectType != "caller key" || !strings.HasPrefix(got[0].ObjectRef, "KEY-1 rst_") || got[2].Details["change"] != "turned off" {
		t.Fatalf("%+v", got)
	}
	for _, e := range got {
		for _, secret := range []string{first, second, callers.HashKey(first), priv} {
			if strings.Contains(e.Raw, secret) {
				t.Fatal(e.Raw)
			}
		}
	}
	for _, secret := range []string{first, second, priv} {
		if strings.Contains(w.logs.String(), secret) {
			t.Fatal("a log holds a key")
		}
	}
}

func TestKeysBelongToTheirOwnerAlone(t *testing.T) {
	w := newWorld(t)
	w.signedInAdmin()
	w.freshSystems()
	t.Cleanup(w.freshSystems)
	mine := subject("owner")
	var own, other int64
	ctx := context.Background()
	w.pool.QueryRow(ctx, `INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at) VALUES ('OWN-2', 'Invented', $1, 'live', now()) RETURNING id`, mine).Scan(&own)
	w.pool.QueryRow(ctx, `INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at) VALUES ('OTHER-2', 'Invented', 'someone-else', 'live', now()) RETURNING id`).Scan(&other)
	owner := w.browser()
	owner.signIn(mine)
	token := owner.csrf()
	key := keyOf(t, owner.post(sysPath(own)+"/keys", url.Values{"csrf": {token}}))
	if key == "" {
		t.Fatal("no key")
	}
	if p := owner.post(sysPath(other)+"/keys", url.Values{"csrf": {token}}); p.status != 403 {
		t.Fatalf("another's system: %d", p.status)
	}
	// A key of another system cannot be reached through one's own address.
	var theirs int64
	w.pool.QueryRow(ctx, `INSERT INTO caller_key (caller_id, key_hash, prefix) VALUES ($1, 'h', 'rst_other') RETURNING id`, other).Scan(&theirs)
	if p := owner.post(sysPath(own)+"/keys/"+itoa(theirs)+"/off", url.Values{"csrf": {token}}); !strings.Contains(p.body, "not live") {
		t.Fatal(p.body)
	}
	var off bool
	w.pool.QueryRow(ctx, `SELECT turned_off_at IS NOT NULL FROM caller_key WHERE id = $1`, theirs).Scan(&off)
	if off {
		t.Fatal("turned off another's key")
	}
	if p := owner.post(sysPath(other)+"/keys/"+itoa(theirs)+"/off", url.Values{"csrf": {token}}); p.status != 403 {
		t.Fatalf("%d", p.status)
	}
}

// ---- limits -------------------------------------------------------------------

func TestSystemScreenLimitsFormIsTheAdministrators(t *testing.T) {
	for _, l := range i18n.Langs {
		admin := render(t, "system", Page{Lang: l, Path: "/systems/7", Title: "systems", CSRF: "form-token", Viewer: &Viewer{Name: "Test"},
			Data: systemData{System: someSystems()[0], Admin: true, Limits: []callers.LimitChange{{Old: "60 a minute, 5 at once", New: "30 a minute, 5 at once", By: "Test Administrator"}}}})
		for _, want := range []string{`action="/systems/7/limits"`, `value="60"`, `value="5"`, `<label for="per_minute">`, "30 a minute", "Test Administrator"} {
			if !strings.Contains(admin, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		owner := render(t, "system", Page{Lang: l, Path: "/systems/7", Title: "systems", Viewer: &Viewer{Name: "Test"}, Data: systemData{System: someSystems()[0]}})
		if strings.Contains(owner, `/limits"`) || !strings.Contains(owner, "<strong>60</strong>") {
			t.Errorf("%s: %s", l, owner)
		}
	}
}

func TestLimitsStartFromTheSettingsAndChangeOnlyByTheAdministrator(t *testing.T) {
	w := newWorld(t)
	w.app.deps.TVA = tva.Dir("../../dev/tva")
	admin, who := w.signedInAdmin()
	w.freshSystems()
	t.Cleanup(w.freshSystems)
	w.clearSettings()
	t.Cleanup(w.clearSettings)
	w.exec(`DELETE FROM audit_outbox`)
	token := admin.csrf()
	ctx := context.Background()
	admin.post("/settings/caller_requests_per_minute", url.Values{"csrf": {token}, "value": {"40"}})
	w.app.settings.Every = 0 // as if RefreshEvery had passed
	admin.post("/systems/register", url.Values{"csrf": {token}, "reference": {"TEST-0001"}})
	var id int64
	var perMinute, atOnce int
	if err := w.pool.QueryRow(ctx, `SELECT id, requests_per_minute, requests_at_once FROM caller WHERE tva_reference = 'TEST-0001'`).Scan(&id, &perMinute, &atOnce); err != nil {
		t.Fatal(err)
	}
	if perMinute != 40 || atOnce != 5 {
		t.Fatal(perMinute, atOnce)
	}
	w.exec(`DELETE FROM audit_outbox`)
	path := sysPath(id) + "/limits"
	if p := admin.post(path, url.Values{"csrf": {token}, "per_minute": {"30"}, "at_once": {"2"}}); !strings.Contains(p.body, "in force now") || !strings.Contains(p.body, "30 a minute, 2 at once") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	if p := admin.post(path, url.Values{"csrf": {token}, "per_minute": {"0"}, "at_once": {"2"}}); !strings.Contains(p.body, "not values") {
		t.Fatal(p.body)
	}
	got := w.outbox()
	if len(got) != 1 || got[0].Action != "setting change" || got[0].ObjectType != "calling system" || got[0].ObjectRef != "TEST-0001" || got[0].Actor != who || got[0].Details["per_minute"] != "30" {
		t.Fatalf("%+v", got)
	}
	// A system's owner reads them and does not change them.
	var owned int64
	mine := subject("owner")
	w.pool.QueryRow(ctx, `INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at) VALUES ('OWN-3', 'Invented', $1, 'live', now()) RETURNING id`, mine).Scan(&owned)
	owner := w.browser()
	owner.signIn(mine)
	if p := owner.post(sysPath(owned)+"/limits", url.Values{"csrf": {owner.csrf()}, "per_minute": {"6000"}, "at_once": {"100"}}); p.status != 403 {
		t.Fatal(p.status)
	}
}
