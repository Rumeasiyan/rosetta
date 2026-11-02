package sheet

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// file makes an .xlsx as a spreadsheet program writes one: shared strings and a sheet of its own.
func file(t *testing.T, sheetXML, sharedXML string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	parts := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="A" sheetId="1" r:id="rId7"/><sheet name="B" sheetId="2" r:id="rId8"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId8" Target="worksheets/other.xml"/><Relationship Id="rId7" Target="/xl/worksheets/first.xml"/></Relationships>`,
		"xl/worksheets/first.xml":    sheetXML,
		"xl/worksheets/other.xml":    `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>wrong sheet</t></is></c></row></sheetData></worksheet>`,
	}
	if sharedXML != "" {
		parts["xl/sharedStrings.xml"] = sharedXML
	}
	for name, body := range parts {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(body))
	}
	z.Close()
	return b.Bytes()
}

func TestReadsWhatAProgramWrites(t *testing.T) {
	data := file(t, `<worksheet><sheetData>
		<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c><c r="D1"><v>12</v></c></row>
		<row r="2"/>
		<row r="3"><c r="B3" t="inlineStr"><is><r><t>rich </t></r><r><t>text</t></r></is></c></row>
		<row r="5"><c r="A5"><f>1+1</f><v>2</v></c></row>
		</sheetData></worksheet>`, `<sst><si><t>தமிழ்</t></si><si><r><t>a</t></r><r><t>b</t></r><rPh><t>phonetic</t></rPh></si></sst>`)
	got, err := Read(data, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if c := got[0].Cells; got[0].Number != 1 || c[0] != "தமிழ்" || c[1] != "" || c[2] != "ab" || c[3] != "12" {
		t.Fatalf("%+v", got[0])
	}
	if c := got[1].Cells; got[1].Number != 3 || c[1] != "rich text" {
		t.Fatalf("%+v", got[1])
	}
	if got[2].Number != 5 || !got[2].Formula {
		t.Fatalf("a formula: %+v", got[2])
	}
}

func TestRefusals(t *testing.T) {
	if _, err := Read([]byte("not a zip file"), 5); !errors.Is(err, ErrNotSheet) {
		t.Error(err)
	}
	if _, err := Read(make([]byte, MaxFile+1), 5); !errors.Is(err, ErrTooBig) {
		t.Error(err)
	}
	var plain bytes.Buffer
	z := zip.NewWriter(&plain)
	f, _ := z.Create("hello.txt")
	f.Write([]byte("no workbook"))
	z.Close()
	if _, err := Read(plain.Bytes(), 5); !errors.Is(err, ErrNotSheet) {
		t.Error(err)
	}
	if _, err := Read(file(t, `<worksheet><sheetData><row><c t="s"><v>9</v></c></row></sheetData></worksheet>`, `<sst/>`), 5); !errors.Is(err, ErrNotSheet) {
		t.Error("a shared string past the end: ", err)
	}
	if _, err := Read(file(t, `<worksheet><sheetData><row`, ""), 5); !errors.Is(err, ErrNotSheet) {
		t.Error("broken XML: ", err)
	}
}

// Over the limit is refused as soon as it is seen; blank rows do not count.
func TestTheRowLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<worksheet><sheetData>`)
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&b, `<row r="%d"><c r="A%d" t="inlineStr"><is><t>x</t></is></c></row><row r="%d"/>`, i*2, i*2, i*2+1)
	}
	b.WriteString(`</sheetData></worksheet>`)
	data := file(t, b.String(), "")
	if got, err := Read(data, 6); err != nil || len(got) != 6 {
		t.Fatalf("%d %v", len(got), err)
	}
	if _, err := Read(data, 5); !errors.Is(err, ErrTooManyRows) {
		t.Fatal(err)
	}
}

// A small file that unpacks to far more is stopped at the limit, not read in full.
func TestAFileThatUnpacksTooFarIsRefused(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, _ := z.Create("xl/workbook.xml")
	zeros := make([]byte, 1<<20)
	for range 40 {
		f.Write(zeros)
	}
	z.Close()
	if b.Len() > MaxFile {
		t.Fatalf("the test's file is %d bytes", b.Len())
	}
	if _, err := Read(b.Bytes(), 5); !errors.Is(err, ErrTooBig) {
		t.Fatal(err)
	}
}

// What goes out can never run as a formula, and what comes back is what went in.
func TestFormulasAreNeutralised(t *testing.T) {
	for in, want := range map[string]string{
		`=1+1`:                `'=1+1`,
		`+94`:                 `'+94`,
		`-1`:                  `'-1`,
		`@SUM(A1)`:            `'@SUM(A1)`,
		"\t=1":                "'\t=1",
		"\r=1":                "'\r=1",
		` =cmd|' /C calc'!A0`: `' =cmd|' /C calc'!A0`,
		`＝1`:                  `'＝1`,
		`Chief Secretary`:     `Chief Secretary`,
		`a=b`:                 `a=b`,
		``:                    ``,
		`දිස්ත්‍රික්කය`: `දිස්ත්‍රික්කය`,
	} {
		if got := Neutral(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
		if got := Restored(Neutral(in)); got != in {
			t.Errorf("back %q: %q", in, got)
		}
	}
	if Restored("'plain") != "'plain" {
		t.Error("an apostrophe of the writer's own was taken away")
	}
}

func TestAWrittenFileReadsBack(t *testing.T) {
	var b bytes.Buffer
	rows := [][]string{{"en", "si", "ta"}, {"A & <b>", `=HYPERLINK("http://x.invalid")`, "தமிழ்"}, {"bad\x01char", "", "  edges  "}}
	if err := Write(&b, rows); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "<f>") {
		t.Fatal("a formula cell was written")
	}
	got, err := Read(b.Bytes(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got[1].Cells[0] != "A & <b>" || got[1].Cells[1] != `'=HYPERLINK("http://x.invalid")` || got[1].Cells[2] != "தமிழ்" {
		t.Fatalf("%+v", got[1])
	}
	if got[2].Cells[0] != "badchar" || got[2].Cells[2] != "  edges  " {
		t.Fatalf("%+v", got[2])
	}
}

func TestColumnNames(t *testing.T) {
	for i, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 701: "ZZ"} {
		if got := columnName(i); got != want || column(want+"12") != i {
			t.Errorf("%d: %q %d", i, got, column(want+"12"))
		}
	}
}
