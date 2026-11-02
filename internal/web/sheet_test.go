package web

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sheet"
)

// upload sends a file on the import screen. token is the form's, or "" to leave it out.
func (b *browser) upload(name string, data []byte, token string) page {
	b.w.t.Helper()
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	if token != "" {
		m.WriteField("csrf", token)
	}
	f, _ := m.CreateFormFile("file", name)
	f.Write(data)
	m.Close()
	req, _ := http.NewRequest(http.MethodPost, b.w.screens.URL+"/glossary/import", &body)
	req.Header.Set("Content-Type", m.FormDataContentType())
	return b.do(req)
}

func xlsx(t *testing.T, rows ...[]string) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := sheet.Write(&b, rows); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// R-GLO-07: imported entries wait as proposals for the keepers; a row that cannot join is left out
// with its number and why; nothing is published; the keepers are told once for the whole file.
func TestAnImportWaitsAsProposals(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypePost, "Existing Post", "පවතින", "இருக்கும்", "")
	w.publish(id, k)
	admin, _ := w.holding("admin", "admin")
	before := w.version()
	p := admin.upload("C:\\Users\\invented\\terms.xlsx", xlsx(t,
		[]string{"Type", "English", "si", "ta", "note", "ignored"},
		[]string{"post", "Imported Post", "ආයාත", "இறக்குமதி", "An invented note", "not read"},
		[]string{"keep as written", "SCOS Imported", "", "", "", ""},
		[]string{"nonsense", "Bad Type", "", "", "", ""},
		[]string{"term", "existing post", "", "", "", ""},
		[]string{"term", "imported post", "", "", "", ""},
		[]string{"term", "Call 0771234567", "", "", "", ""},
		[]string{"term", "", "", "", "", ""},
	), admin.csrf())
	if p.status != 200 || !strings.Contains(p.body, i18n.T(i18n.EN, "glossaryImportDone")) {
		t.Fatalf("%d %s", p.status, p.body)
	}
	for _, want := range []string{"Row 4:", "Row 5:", "Row 6:", "Row 7:", i18n.T(i18n.EN, "glossaryBadType"), i18n.T(i18n.EN, "glossaryDuplicate"), i18n.T(i18n.EN, "glossaryBadText")} {
		if !strings.Contains(p.body, want) {
			t.Errorf("no %q", want)
		}
	}
	if strings.Contains(p.body, "Bad Type") || strings.Contains(p.body, "0771234567") {
		t.Error("a refused row's words were repeated")
	}
	ctx := context.Background()
	var rows, accepted, refused int
	var name string
	if err := w.pool.QueryRow(ctx, `SELECT file_name, rows, accepted, refused FROM glossary_import`).Scan(&name, &rows, &accepted, &refused); err != nil {
		t.Fatal(err)
	}
	if name != "terms.xlsx" || rows != 7 || accepted != 2 || refused != 5 {
		t.Fatalf("%q %d %d %d", name, rows, accepted, refused)
	}
	if w.version() != before {
		t.Fatal("importing published something")
	}
	waiting, err := glossary.Waiting(ctx, w.pool, nil)
	if err != nil || len(waiting) != 2 {
		t.Fatalf("%+v %v", waiting, err)
	}
	for _, e := range waiting {
		if e.Status != glossary.StatusProposed || len(e.Unchecked()) != 3 {
			t.Errorf("%+v", e)
		}
		if e.Term(i18n.EN).Text == "SCOS Imported" && e.Term(i18n.TA).Text != "SCOS Imported" {
			t.Errorf("kept as written: %+v", e)
		}
	}
	// The keepers publish it as they would anything else.
	w.publish(waiting[0].ID, k)
	if e := w.entry(waiting[0].ID); e.Status != glossary.StatusPublished {
		t.Fatal(e.Status)
	}
	n := w.notifier()
	if sent, err := n.Send(ctx, w.pool); err != nil || sent < 1 {
		t.Fatalf("%d %v", sent, err)
	}
	notices, _ := w.noticesSent()
	imports := 0
	for _, x := range notices {
		if x.NoticeType == "glossary_import" && len(x.People) == 3 {
			imports++
		}
	}
	if imports != 1 {
		t.Fatalf("%d notices for the file", imports)
	}
}

// R-GLO-07 and the setting: over the row limit is refused whole; at the limit it is read, in moments.
func TestTheRowLimitRefusesTheWholeFile(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	admin, _ := w.holding("admin", "admin")
	rows := [][]string{{"type", "en", "si", "ta", "note"}}
	limit := settings.GlossaryImportRows.Default
	if limit != 5000 {
		t.Fatalf("R-GLO-07's default is 5,000, here %d", limit)
	}
	for i := range limit {
		rows = append(rows, []string{"term", fmt.Sprintf("Invented term %d", i), fmt.Sprintf("සිංහල %d", i), fmt.Sprintf("தமிழ் %d", i), ""})
	}
	started := time.Now()
	p := admin.upload("full.xlsx", xlsx(t, rows...), admin.csrf())
	took := time.Since(started)
	if p.status != 200 || !strings.Contains(p.body, ">5000<") && !strings.Contains(p.body, "5000") {
		t.Fatalf("%d", p.status)
	}
	t.Logf("5,000 rows imported in %v", took)
	if took > 30*time.Second {
		t.Fatalf("5,000 rows took %v, holding one of five connections", took)
	}
	var kept int
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM glossary_entry WHERE status = 'proposed'`).Scan(&kept)
	if kept != limit {
		t.Fatalf("%d", kept)
	}
	w.glossaryClean()
	rows = append(rows, []string{"term", "One too many", "", "", ""})
	p = admin.upload("over.xlsx", xlsx(t, rows...), admin.csrf())
	says(t, p, i18n.EN, "glossaryImportTooMany")
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM glossary_entry`).Scan(&kept)
	if kept != 0 {
		t.Fatalf("%d kept from a refused file", kept)
	}
}

func TestWhatAnImportRefuses(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	admin, _ := w.holding("admin", "admin")
	officer, _ := w.holding("officer")
	keeper, _ := w.holding("keeper", "keeper en")
	good := xlsx(t, []string{"type", "en"}, []string{"term", "A Term"})
	says(t, admin.upload("x.xlsx", []byte("not a spreadsheet"), admin.csrf()), i18n.EN, "glossaryImportNotSheet")
	says(t, admin.upload("x.xlsx", xlsx(t, []string{"nothing", "useful"}, []string{"a", "b"}), admin.csrf()), i18n.EN, "glossaryImportColumns")
	says(t, admin.upload("x.xlsx", xlsx(t, []string{"en"}, []string{"a"}), admin.csrf()), i18n.EN, "glossaryImportColumns")
	says(t, admin.upload("x.xlsx", make([]byte, sheet.MaxFile+1<<21), admin.csrf()), i18n.EN, "glossaryImportTooBig")
	if p := admin.upload("x.xlsx", good, ""); p.status != 403 {
		t.Errorf("no token: %d", p.status)
	}
	if p := admin.upload("x.xlsx", good, "wrong"); p.status != 403 {
		t.Errorf("a wrong token: %d", p.status)
	}
	for name, b := range map[string]*browser{"officer": officer, "keeper": keeper} {
		if p := b.upload("x.xlsx", good, b.csrf()); p.status != 403 {
			t.Errorf("%s: %d", name, p.status)
		}
		for _, path := range []string{"/glossary/import", "/glossary/export"} {
			if p := b.get(path); p.status != 403 {
				t.Errorf("%s, %s: %d", name, path, p.status)
			}
		}
	}
	var kept int
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM glossary_entry`).Scan(&kept)
	if kept != 0 {
		t.Fatalf("%d kept", kept)
	}
	// A cell holding a formula is left out, and its cached value is not taken instead.
	f := fileWithFormula(t)
	p := admin.upload("f.xlsx", f, admin.csrf())
	if !strings.Contains(p.body, "Row 2:") {
		t.Fatal(p.body)
	}
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM glossary_entry`).Scan(&kept)
	if kept != 0 {
		t.Fatalf("%d kept", kept)
	}
}

// fileWithFormula is a sheet whose second row's English term is worked out by a formula.
func fileWithFormula(t *testing.T) []byte {
	t.Helper()
	raw := xlsx(t, []string{"type", "en"}, []string{"term", "PLACEHOLDER"})
	got, err := rewriteSheet(raw, `<c r="B2" t="inlineStr"><is><t xml:space="preserve">PLACEHOLDER</t></is></c>`, `<c r="B2" t="str"><f>CONCAT("A","B")</f><v>AB</v></c>`)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// R-GLO-07: the export is the published glossary, whole; nothing in it can run as a formula; it is an
// audit entry; what it wrote can come in again, and is then refused as already there.
func TestTheExportCarriesNoFormula(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	a := k[i18n.EN].propose(glossary.TypePost, "Exported Post", "අපනයන", "ஏற்றுமதி", `=HYPERLINK("http://example.invalid","click")`)
	w.publish(a, k)
	b := k[i18n.EN].propose(glossary.TypeKeep, "SCOS Export", "", "", "@SUM(A1)")
	w.publish(b, k)
	k[i18n.EN].propose(glossary.TypeTerm, "Still Waiting", "", "", "")
	admin, _ := w.holding("admin", "admin")
	w.exec(`DELETE FROM audit_outbox`)
	p := admin.get("/glossary/export")
	if p.status != 200 || !strings.Contains(p.header.Get("Content-Disposition"), "attachment") || !strings.Contains(p.header.Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("%d %v", p.status, p.header)
	}
	rows, err := sheet.Read([]byte(p.body), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || strings.Join(rows[0].Cells, ",") != "en,si,ta,type,note" {
		t.Fatalf("%+v", rows)
	}
	all := ""
	for _, r := range rows {
		if r.Formula {
			t.Fatalf("a formula: %+v", r)
		}
		all += strings.Join(r.Cells, "|") + "\n"
		for _, c := range r.Cells {
			if strings.HasPrefix(c, "=") || strings.HasPrefix(c, "@") || strings.HasPrefix(c, "+") || strings.HasPrefix(c, "-") {
				t.Fatalf("not neutralised: %q", c)
			}
		}
	}
	if !strings.Contains(all, `'=HYPERLINK`) || !strings.Contains(all, `'@SUM`) || strings.Contains(all, "Still Waiting") {
		t.Fatal(all)
	}
	got := w.outbox()
	if len(got) != 1 || got[0].Action != "export" || got[0].Post != "not known" || strings.Contains(got[0].Raw, "Exported Post") {
		t.Fatalf("%+v", got)
	}
	// In again: every row is already there, and the note comes back as it was written.
	again := admin.upload("again.xlsx", []byte(p.body), admin.csrf())
	if !strings.Contains(again.body, "Row 2:") || !strings.Contains(again.body, "Row 3:") {
		t.Fatal(again.body)
	}
	w.glossaryClean()
	admin.upload("again.xlsx", []byte(p.body), admin.csrf())
	var note string
	if err := w.pool.QueryRow(context.Background(), `SELECT note FROM glossary_entry WHERE type = 'post'`).Scan(&note); err != nil || !strings.HasPrefix(note, `=HYPERLINK`) {
		t.Fatalf("%q %v", note, err)
	}
}

// No word of a file reaches a log or the outbox, kept or refused, whatever its name.
func TestNoWordOfAFileLeaves(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	w.keepers()
	const secret = "Markersheetword"
	admin, _ := w.holding("admin", "admin")
	admin.upload(secret+".xlsx", xlsx(t, []string{"type", "en", "note"}, []string{"term", secret + " one", secret}, []string{"bad", secret + " two", ""}), admin.csrf())
	admin.get("/glossary/export")
	w.notifier().Send(context.Background(), w.pool)
	for _, x := range w.outbox() {
		if strings.Contains(x.Raw, secret) {
			t.Errorf("in the outbox: %s", x.Raw)
		}
	}
	if _, raw := w.noticesSent(); strings.Contains(raw, secret) {
		t.Error("in a notice")
	}
	if strings.Contains(w.logs.String(), secret) {
		t.Error("in the logs")
	}
}

func TestImportScreenInEachLanguage(t *testing.T) {
	done := &glossary.Imported{Rows: 3, Accepted: 2, Refused: []glossary.RowRefusal{{Row: 4, Word: "glossaryBadType"}}}
	for _, l := range i18n.Langs {
		body := render(t, "glossary-import", Page{Lang: l, Path: "/glossary/import", Title: "glossaryImport", CSRF: "form-token", Viewer: &Viewer{Name: "Test"}, Data: glossaryImportData{Limit: 5000, Done: done}})
		for _, want := range []string{`enctype="multipart/form-data"`, `name="csrf" value="form-token"`, i18n.T(l, "glossaryBadType"), i18n.T(l, "glossaryImportNote"), "5000"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
	}
}

// rewriteSheet changes one part of a written file, to make what this package would never write.
func rewriteSheet(raw []byte, from, to string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		w, err := z.Create(f.Name)
		if err != nil {
			return nil, err
		}
		w.Write(bytes.ReplaceAll(body, []byte(from), []byte(to)))
	}
	err = z.Close()
	return out.Bytes(), err
}
