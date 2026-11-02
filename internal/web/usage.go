package web

import (
	"bytes"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/sheet"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/usage"
	"github.com/jackc/pgx/v5"
)

// usageRowsShown is the most lines a screen holds, so a page stays small (R-NFR-03). The export
// holds them all.
const usageRowsShown = 200

// usageDays is how far back a view starts when nobody says.
const usageDays = 30

func (a *App) mountUsage(mux *http.ServeMux) {
	mux.Handle("GET /usage", a.signedIn(a.only(seesSystems, a.usagePage)))
	mux.Handle("GET /usage/export", a.signedIn(a.only(seesSystems, a.usageExport)))
}

func opWord(op string) string {
	switch op {
	case usage.OpTranslate:
		return "opTranslate"
	case usage.OpGrammar:
		return "opGrammar"
	case usage.OpMemoryAdd:
		return "opMemoryAdd"
	case usage.OpTest:
		return "opTest"
	}
	return "opRead"
}

// reasonWords is the short word for each outcome.
var reasonWords = map[string]string{
	usage.Answered: "whyAnswered", string(reasons.NoKey): "whyNoKey", string(reasons.OverLimit): "whyOverLimit", string(reasons.Invalid): "whyInvalid",
	string(reasons.TooLong): "whyTooLong", string(reasons.Unsupported): "whyUnsupported", string(reasons.HumanRequired): "whyHumanRequired",
	string(reasons.NoProvincial): "whyNoProvincial", string(reasons.MaskingLost): "whyMaskingLost", string(reasons.NoProvider): "whyNoProvider", string(reasons.TimedOut): "whyTimedOut",
}

func whyWord(outcome string) string {
	if w, ok := reasonWords[outcome]; ok {
		return w
	}
	return "whyInvalid"
}

func timeWord(bucket string) string {
	switch bucket {
	case "under_1s":
		return "timeUnder1s"
	case "under_2s":
		return "timeUnder2s"
	case "under_6s":
		return "timeUnder6s"
	case "under_15s":
		return "timeUnder15s"
	case "under_60s":
		return "timeUnder60s"
	}
	return "timeOver60s"
}

func groupWord(group string) string {
	switch group {
	case usage.BySystem:
		return "usageBySystem"
	case usage.ByOperation:
		return "usageByOperation"
	case usage.ByDirection:
		return "usageByDirection"
	case usage.BySource:
		return "usageBySource"
	}
	return "usageByDay"
}

// usageLine is one line as a screen or a file shows it.
type usageLine struct {
	Label string
	usage.Row
}

// usageData is the screen's data.
type usageData struct {
	Filter    usage.Filter
	Since     string
	Until     string
	Systems   []callers.Caller
	Sources   []string
	Groups    []string
	Ops       []string
	Lines     []usageLine
	More      bool
	Whole     usage.Row
	Reasons   []reasonCount
	ExportURL string
	Today     string
}

// reasonCount is one reason and how many.
type reasonCount struct {
	Reason string
	Count  int64
}

// reasonsOf is a row's reasons, in the fixed list's order.
func reasonsOf(r usage.Row) []reasonCount {
	var out []reasonCount
	for _, reason := range reasons.All {
		if n := r.Reasons[string(reason)]; n > 0 {
			out = append(out, reasonCount{string(reason), n})
		}
	}
	return out
}

// usageSystems is who this person sees usage for: everything for the administrator, and a system
// owner's own only (R-REC-03; R-ADM-01).
func (a *App) usageSystems(r *http.Request) ([]callers.Caller, error) {
	c := fromCtx(r)
	if isAdmin(c) {
		return callers.All(r.Context(), a.deps.Pool)
	}
	return callers.OwnedBy(r.Context(), a.deps.Pool, c.sess.subject)
}

// usageFilter reads a view from the address. What is not on the lists is no view, so nothing typed
// reaches the query or an audit entry. A system not this person's is a denial.
func (a *App) usageFilter(r *http.Request, systems []callers.Caller, known []string, today time.Time) (f usage.Filter, bad, denied bool) {
	q := r.URL.Query()
	f.Until = today
	f.Since = today.AddDate(0, 0, -usageDays)
	f.Group = usage.ByDay
	if v := q.Get("since"); v != "" {
		d, err := time.Parse(time.DateOnly, v)
		bad = bad || err != nil
		f.Since = d
	}
	if v := q.Get("until"); v != "" {
		d, err := time.Parse(time.DateOnly, v)
		bad = bad || err != nil
		f.Until = d
	}
	bad = bad || f.Until.Before(f.Since)
	if v := q.Get("group"); slices.Contains(usage.Groupings, v) {
		f.Group = v
	} else if v != "" {
		bad = true
	}
	if v := q.Get("operation"); slices.Contains(usage.Operations, v) {
		f.Operation = v
	} else if v != "" {
		bad = true
	}
	for _, x := range []struct {
		name string
		to   *string
	}{{"from", &f.From}, {"to", &f.To}} {
		v := q.Get(x.name)
		if _, ok := i18n.Parse(v); ok && string(i18n.Lang(v)) == v {
			*x.to = v
		} else if v != "" {
			bad = true
		}
	}
	if v := q.Get("source"); slices.Contains(known, v) {
		f.Source = v
	} else if v != "" {
		bad = true
	}
	c := fromCtx(r)
	f.AllCallers = isAdmin(c)
	for _, s := range systems {
		f.Callers = append(f.Callers, s.ID)
	}
	if v := q.Get("system"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || !slices.Contains(f.Callers, id) {
			if isAdmin(c) {
				return f, true, false
			}
			return f, false, true
		}
		f.Caller = id
	}
	return f, bad, false
}

// usageQuery is the same view again, for the export link.
func usageQuery(f usage.Filter) string {
	v := url.Values{"group": {f.Group}, "since": {f.Since.Format(time.DateOnly)}, "until": {f.Until.Format(time.DateOnly)}}
	if f.Caller != 0 {
		v.Set("system", strconv.FormatInt(f.Caller, 10))
	}
	for name, x := range map[string]string{"operation": f.Operation, "from": f.From, "to": f.To, "source": f.Source} {
		if x != "" {
			v.Set(name, x)
		}
	}
	return v.Encode()
}

// usageLabel is a line's name in the reader's language: nothing but the system's name comes from elsewhere.
func usageLabel(l i18n.Lang, group string, r usage.Row) string {
	switch group {
	case usage.BySystem:
		return r.Name + " (" + r.Key + ")"
	case usage.ByOperation:
		return i18n.T(l, opWord(r.Key))
	case usage.ByDirection:
		from, to, _ := cutSpace(r.Key)
		return directionLabel(l, from, to)
	}
	return r.Key
}

func cutSpace(s string) (string, string, bool) {
	for i := range s {
		if s[i] == ' ' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

func directionLabel(l i18n.Lang, from, to string) string {
	f, fromOK := i18n.Parse(from)
	t, toOK := i18n.Parse(to)
	switch {
	case fromOK && toOK:
		return i18n.Name(f) + " → " + i18n.Name(t)
	case fromOK:
		return i18n.Name(f)
	}
	return i18n.T(l, "usageAll")
}

// usageView reads the view the address names, and answers for itself if it cannot.
func (a *App) usageView(w http.ResponseWriter, r *http.Request) (usageData, bool) {
	ctx := r.Context()
	systems, err := a.usageSystems(r)
	if err != nil {
		a.fail(w, r, err)
		return usageData{}, false
	}
	names, err := sources.Names(ctx, a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return usageData{}, false
	}
	today, err := people.Today(ctx, a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return usageData{}, false
	}
	f, bad, denied := a.usageFilter(r, systems, names, today)
	if denied {
		a.deny(w, r, "Not this person's system", "notAllowed")
		return usageData{}, false
	}
	if bad {
		back(w, r, "/usage", "usageBadView", "bad")
		return usageData{}, false
	}
	view, err := usage.Make(ctx, a.deps.Pool, f)
	if err != nil {
		a.fail(w, r, err)
		return usageData{}, false
	}
	d := usageData{Filter: f, Since: f.Since.Format(time.DateOnly), Until: f.Until.Format(time.DateOnly), Systems: systems, Sources: names, Groups: usage.Groupings, Ops: usage.Operations,
		Whole: view.Whole, Reasons: reasonsOf(view.Whole), ExportURL: "/usage/export?" + usageQuery(f), Today: today.Format(time.DateOnly)}
	lang := Language(r)
	for _, row := range view.Rows {
		d.Lines = append(d.Lines, usageLine{Label: usageLabel(lang, f.Group, row), Row: row})
	}
	return d, true
}

func (a *App) usagePage(w http.ResponseWriter, r *http.Request) {
	d, ok := a.usageView(w, r)
	if !ok {
		return
	}
	if len(d.Lines) > usageRowsShown {
		d.Lines, d.More = d.Lines[:usageRowsShown], true
	}
	p := a.page(r, "usage", "usage")
	p.Data = d
	a.Render(w, r, http.StatusOK, "usage", p)
}

// usageExport sends the view as a spreadsheet file (R-REC-03), and an export is an audit entry
// (P-ING-02). The entry names the view and a number of lines; no count of usage goes to PENSIEVE.
func (a *App) usageExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	d, ok := a.usageView(w, r)
	if !ok {
		return
	}
	lang := Language(r)
	head := []string{i18n.T(lang, groupWord(d.Filter.Group)), i18n.T(lang, "usageRequests"), i18n.T(lang, "whyAnswered"), i18n.T(lang, "usageCharacters"), i18n.T(lang, "usagePiecesGlossary"),
		i18n.T(lang, "usagePiecesMemory"), i18n.T(lang, "usagePiecesCache"), i18n.T(lang, "usagePiecesSource"), i18n.T(lang, "usageMismatches")}
	for _, b := range usage.Buckets {
		head = append(head, i18n.T(lang, timeWord(b.Name)))
	}
	for _, reason := range reasons.All {
		head = append(head, i18n.T(lang, whyWord(string(reason))))
	}
	rows := [][]string{head}
	line := func(label string, x usage.Row) []string {
		num := func(n int64) string { return strconv.FormatInt(n, 10) }
		out := []string{label, num(x.Requests), num(x.Answered), num(x.Characters), num(x.Pieces.Glossary), num(x.Pieces.Memory), num(x.Pieces.Cache), num(x.Pieces.Source), num(x.Mismatches)}
		for _, n := range x.Buckets {
			out = append(out, num(n))
		}
		for _, reason := range reasons.All {
			out = append(out, num(x.Reasons[string(reason)]))
		}
		return out
	}
	for _, l := range d.Lines {
		rows = append(rows, line(l.Label, l.Row))
	}
	rows = append(rows, line(i18n.T(lang, "usageWhole"), d.Whole))
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		details := map[string]any{"group": d.Filter.Group, "since": d.Since, "until": d.Until, "lines": len(d.Lines)}
		if d.Filter.Caller != 0 {
			details["system"] = strconv.FormatInt(d.Filter.Caller, 10)
		}
		for name, x := range map[string]string{"operation": d.Filter.Operation, "from": d.Filter.From, "to": d.Filter.To, "source": d.Filter.Source} {
			if x != "" {
				details[name] = x
			}
		}
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionExport, ObjectType: "usage view", ObjectRef: d.Filter.Group + " " + d.Since + " " + d.Until,
			Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: details}, now)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var out bytes.Buffer
	if err := sheet.Write(&out, rows); err != nil {
		a.fail(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	h.Set("Content-Disposition", `attachment; filename="rosetta-usage-`+d.Since+`-`+d.Until+`.xlsx"`)
	h.Set("Cache-Control", "no-store")
	SecurityHeaders(h)
	w.WriteHeader(http.StatusOK)
	out.WriteTo(w)
}
