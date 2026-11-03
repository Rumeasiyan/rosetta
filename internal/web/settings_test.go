package web

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/settings"
)

func TestSettingsScreensInEachLanguage(t *testing.T) {
	s := settings.CallerIdleDays
	for _, l := range i18n.Langs {
		body := render(t, "settings", Page{Lang: l, Path: "/settings", Title: "settings", Here: "settings", CSRF: "form-token", Viewer: &Viewer{Name: "Test"},
			Data: []settingRow{{Int: s, Saved: 45, InForce: 90}}})
		for _, want := range []string{i18n.T(l, s.Word), `value="45"`, `action="/settings/caller_idle_days"`, `name="csrf" value="form-token"`, `for="v-caller_idle_days"`, "R-CAL-05"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		hist := render(t, "settings-history", Page{Lang: l, Path: "/settings/history", Title: "settingsHistory", Viewer: &Viewer{Name: "Test"},
			Data: []historyRow{{HistoryRow: settings.HistoryRow{Name: s.Name, Old: "90", New: "45", By: "Test Administrator"}, Word: s.Word, Unit: s.Unit}}})
		if !strings.Contains(hist, i18n.T(l, s.Word)) || !strings.Contains(hist, "Test Administrator") {
			t.Errorf("%s: %s", l, hist)
		}
	}
}

func (w *world) settingRows() map[string]string {
	w.t.Helper()
	rows, err := w.pool.Query(context.Background(), `SELECT name, value FROM setting`)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var n, v string
		if err := rows.Scan(&n, &v); err != nil {
			w.t.Fatal(err)
		}
		out[n] = v
	}
	return out
}

func (w *world) clearSettings() {
	w.t.Helper()
	w.exec(`DELETE FROM change_history`)
	w.exec(`DELETE FROM setting`)
}

func TestChangingASettingKeepsHistoryAndAnEntry(t *testing.T) {
	w := newWorld(t)
	admin, who := w.signedInAdmin()
	w.clearSettings()
	t.Cleanup(w.clearSettings)
	w.exec(`DELETE FROM audit_outbox`)
	token := admin.csrf()
	p := admin.post("/settings/caller_idle_days", url.Values{"csrf": {token}, "value": {"45"}})
	if p.status != 200 || !strings.Contains(p.body, "Saved") || !strings.Contains(p.body, `value="45"`) {
		t.Fatalf("%d %s", p.status, p.body)
	}
	if got := w.settingRows()["caller_idle_days"]; got != "45" {
		t.Fatalf("%q", got)
	}
	var subject, ref, old, now, by string
	err := w.pool.QueryRow(context.Background(), `SELECT h.subject, h.subject_ref, h.old_value, h.new_value, p.mjolnir_subject
		FROM change_history h JOIN person p ON p.id = h.changed_by ORDER BY h.id DESC LIMIT 1`).Scan(&subject, &ref, &old, &now, &by)
	if err != nil {
		t.Fatal(err)
	}
	if subject != "setting" || ref != "caller_idle_days" || old != "90" || now != "45" || by != who {
		t.Fatalf("%s %s %s %s %s", subject, ref, old, now, by)
	}
	got := w.outbox()
	if len(got) != 1 || got[0].Action != "setting change" || got[0].Post != "not known" || got[0].ObjectRef != "caller_idle_days" || got[0].Details["new"] != "45" {
		t.Fatalf("%+v", got)
	}
	if h := admin.get("/settings/history"); h.status != 200 || !strings.Contains(h.body, "Test "+who) || !strings.Contains(h.body, "45") {
		t.Fatalf("%d %s", h.status, h.body)
	}
	// The same value again changes nothing.
	admin.post("/settings/caller_idle_days", url.Values{"csrf": {token}, "value": {"45"}})
	var n int
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM change_history`).Scan(&n)
	if n != 1 || len(w.outbox()) != 1 {
		t.Fatalf("%d", n)
	}
}

// The change reaches what ROSETTA uses on the first read after RefreshEvery.
func TestAChangeComesIntoForce(t *testing.T) {
	w := newWorld(t)
	admin, _ := w.signedInAdmin()
	w.clearSettings()
	t.Cleanup(w.clearSettings)
	ctx := context.Background()
	if got := w.app.settings.Get(ctx).Of(settings.CallerIdleDays); got != 90 {
		t.Fatalf("%d", got)
	}
	admin.post("/settings/caller_idle_days", url.Values{"csrf": {admin.csrf()}, "value": {"45"}})
	w.app.settings.Every = 0 // as if RefreshEvery had passed
	if got := w.app.settings.Get(ctx).Of(settings.CallerIdleDays); got != 45 {
		t.Fatalf("%d", got)
	}
	// A wrong row is seen and the last good value stays.
	w.exec(`UPDATE setting SET value = 'nonsense' WHERE name = 'caller_idle_days'`)
	if got := w.app.settings.Get(ctx).Of(settings.CallerIdleDays); got != 45 {
		t.Fatalf("%d", got)
	}
	if !strings.Contains(w.logs.String(), "settings not read") {
		t.Fatal("not logged")
	}
}

func TestSettingsRefusals(t *testing.T) {
	const marker = "CALLERTEXTMARKER"
	w := newWorld(t)
	admin, _ := w.signedInAdmin()
	w.clearSettings()
	t.Cleanup(w.clearSettings)
	w.exec(`DELETE FROM audit_outbox`)
	token := admin.csrf()
	for _, bad := range []string{marker, "", "0", "-1", "3651", "123456789V", "200012345678"} {
		p := admin.post("/settings/caller_idle_days", url.Values{"csrf": {token}, "value": {bad}})
		if p.status != 200 || !strings.Contains(p.body, "not a value") || strings.Contains(p.body, marker) {
			t.Errorf("%q: %d", bad, p.status)
		}
	}
	if len(w.settingRows()) != 0 || len(w.outbox()) != 0 {
		t.Fatalf("a refused value changed something: %v %v", w.settingRows(), w.outbox())
	}
	if p := admin.post("/settings/no_such_setting", url.Values{"csrf": {token}, "value": {"5"}}); p.status != 404 {
		t.Fatalf("%d", p.status)
	}
	if p := admin.post("/settings/caller_idle_days", url.Values{"value": {"5"}}); p.status != 403 {
		t.Fatalf("no form token: %d", p.status)
	}
	if strings.Contains(w.logs.String(), marker) {
		t.Fatal("a log holds it")
	}
	var n int
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM change_history WHERE new_value = $1`, marker).Scan(&n)
	if n != 0 {
		t.Fatal("history holds it")
	}
	// Not an administrator: every setting screen is refused, as a denial.
	other := w.browser()
	other.signIn(subject("officer"))
	otherToken := other.csrf()
	w.exec(`DELETE FROM audit_outbox`)
	for _, path := range []string{"/settings", "/settings/history"} {
		if p := other.get(path); p.status != 403 {
			t.Errorf("%s: %d", path, p.status)
		}
	}
	if p := other.post("/settings/caller_idle_days", url.Values{"csrf": {otherToken}, "value": {"5"}}); p.status != 403 {
		t.Fatalf("%d", p.status)
	}
	got := w.outbox()
	if len(got) != 3 || got[0].ObjectRef != "GET /settings" || got[2].ObjectRef != "POST /settings/{name}" {
		t.Fatalf("%+v", got)
	}
	if len(w.settingRows()) != 0 {
		t.Fatal("a person with no role changed a setting")
	}
}
