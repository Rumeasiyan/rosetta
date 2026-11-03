// Package sheet reads and writes one worksheet of an .xlsx file with Go's standard library alone
// (BSD-3-Clause), so no reader's licence or code comes into ROSETTA. It reads what spreadsheet
// programs write, shared strings and inline strings, and refuses what is too large before it is
// unpacked in full. It writes every value as text and neutralises what a spreadsheet could read
// as a formula.
package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The limits of what is read. A row's limit is the caller's; these hold back a file that unpacks to
// far more than it looks (R-NFR-01: a small program in 250 MB).
const (
	MaxFile     = 8 << 20  // the file as sent
	maxUnpacked = 32 << 20 // any one part of it, unpacked
	maxColumns  = 64       // columns read in a row
)

// The refusals. None holds anything of the file.
var (
	ErrNotSheet    = errors.New("sheet: not a spreadsheet file this can read")
	ErrTooBig      = errors.New("sheet: the file is too large")
	ErrTooManyRows = errors.New("sheet: the file has more rows than allowed")
)

// Row is one row with anything in it. Number counts from 1 as the program shows it; Formula says a
// cell held one, which is never taken as a value.
type Row struct {
	Number  int
	Cells   []string
	Formula bool
}

// Read reads the first worksheet. maxRows counts rows with anything in them, the first row included.
func Read(data []byte, maxRows int) ([]Row, error) {
	if len(data) > MaxFile {
		return nil, ErrTooBig
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrNotSheet
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[strings.TrimPrefix(path.Clean(f.Name), "/")] = f
	}
	target, err := firstSheet(files)
	if err != nil {
		return nil, err
	}
	shared, err := sharedStrings(files["xl/sharedStrings.xml"])
	if err != nil {
		return nil, err
	}
	raw, err := unpack(files[target])
	if err != nil {
		return nil, err
	}
	return rows(raw, shared, maxRows)
}

// unpack reads one part, stopping at the limit whatever its header claimed.
func unpack(f *zip.File) ([]byte, error) {
	if f == nil {
		return nil, ErrNotSheet
	}
	r, err := f.Open()
	if err != nil {
		return nil, ErrNotSheet
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, maxUnpacked+1))
	if err != nil {
		return nil, ErrNotSheet
	}
	if len(b) > maxUnpacked {
		return nil, ErrTooBig
	}
	return b, nil
}

// firstSheet follows the workbook to its first sheet's part.
func firstSheet(files map[string]*zip.File) (string, error) {
	raw, err := unpack(files["xl/workbook.xml"])
	if err != nil {
		return "", err
	}
	var book struct {
		Sheets []struct {
			ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if xml.Unmarshal(raw, &book) != nil || len(book.Sheets) == 0 {
		return "", ErrNotSheet
	}
	raw, err = unpack(files["xl/_rels/workbook.xml.rels"])
	if err != nil {
		return "", err
	}
	var rels struct {
		List []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(raw, &rels) != nil {
		return "", ErrNotSheet
	}
	for _, r := range rels.List {
		if r.ID == book.Sheets[0].ID {
			if strings.HasPrefix(r.Target, "/") {
				return strings.TrimPrefix(path.Clean(r.Target), "/"), nil
			}
			return path.Clean("xl/" + r.Target), nil
		}
	}
	return "", ErrNotSheet
}

// sharedStrings reads the strings a sheet's cells point to; a file need not have any.
func sharedStrings(f *zip.File) ([]string, error) {
	if f == nil {
		return nil, nil
	}
	raw, err := unpack(f)
	if err != nil {
		return nil, err
	}
	var out []string
	d := xml.NewDecoder(bytes.NewReader(raw))
	var b strings.Builder
	depthPhonetic, inText := 0, false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, ErrNotSheet
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				b.Reset()
			case "rPh":
				depthPhonetic++
			case "t":
				inText = depthPhonetic == 0
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				out = append(out, b.String())
			case "rPh":
				depthPhonetic--
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
}

// column is a cell reference's column, from 0, or -1.
func column(ref string) int {
	n := 0
	seen := false
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A') + 1
		seen = true
	}
	if !seen {
		return -1
	}
	return n - 1
}

// rows reads the sheet's rows, stopping as soon as there are too many.
func rows(raw []byte, shared []string, maxRows int) ([]Row, error) {
	d := xml.NewDecoder(bytes.NewReader(raw))
	var out []Row
	var row Row
	var kind, ref string
	var value strings.Builder
	var hasFormula, inValue bool
	next := 0
	cellEnd := func() error {
		text := value.String()
		switch kind {
		case "s":
			i, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil || i < 0 || i >= len(shared) {
				return ErrNotSheet
			}
			text = shared[i]
		case "b":
			text = map[string]string{"1": "TRUE", "0": "FALSE"}[strings.TrimSpace(text)]
		}
		at := column(ref)
		if at < 0 {
			at = next
		}
		next = at + 1
		if at >= maxColumns {
			return nil
		}
		for len(row.Cells) <= at {
			row.Cells = append(row.Cells, "")
		}
		row.Cells[at] = text
		row.Formula = row.Formula || hasFormula
		return nil
	}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, ErrNotSheet
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row, next = Row{Number: len(out) + 1}, 0
				for _, a := range t.Attr {
					if a.Name.Local == "r" {
						if n, err := strconv.Atoi(a.Value); err == nil && n > 0 {
							row.Number = n
						}
					}
				}
			case "c":
				kind, ref, hasFormula = "", "", false
				value.Reset()
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "t":
						kind = a.Value
					case "r":
						ref = a.Value
					}
				}
			case "f":
				hasFormula = true
			case "v", "t":
				inValue = true
			case "rPh":
				if err := d.Skip(); err != nil {
					return nil, ErrNotSheet
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inValue = false
			case "c":
				if err := cellEnd(); err != nil {
					return nil, err
				}
			case "row":
				if blank(row) {
					continue
				}
				if len(out) >= maxRows {
					return nil, ErrTooManyRows
				}
				out = append(out, row)
			}
		case xml.CharData:
			if inValue {
				value.Write(t)
			}
		}
	}
}

func blank(r Row) bool {
	for _, c := range r.Cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return !r.Formula
}

// formulaMarks are what a spreadsheet may take as the start of a formula, the wide forms too.
const formulaMarks = "=+-@\t\r\n＝＋－＠"

// Neutral makes a value safe to open: one a spreadsheet could read as a formula gets a leading
// apostrophe, so it stays words, whichever program opens the file and however it is saved on.
func Neutral(s string) string {
	if trimmed := strings.TrimLeft(s, " "); trimmed != "" {
		if r, _ := utf8.DecodeRuneInString(trimmed); strings.ContainsRune(formulaMarks, r) {
			return "'" + s
		}
	}
	return s
}

// Restored undoes Neutral on a value read back, so a file written here can come in again.
func Restored(s string) string {
	if rest, ok := strings.CutPrefix(s, "'"); ok && Neutral(rest) != rest {
		return rest
	}
	return s
}

// clean drops what XML cannot hold, as a sheet is no place for a broken file.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r < 0x20, r == 0xFFFE, r == 0xFFFF, r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
}

// columnName is a column's letters, from 0.
func columnName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

const (
	contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`
	packageRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`
	workbook = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`
	workbookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`
)

// Write writes rows as one worksheet of text cells, each neutralised.
func Write(w io.Writer, rows [][]string) error {
	z := zip.NewWriter(w)
	for _, part := range []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes}, {"_rels/.rels", packageRels}, {"xl/workbook.xml", workbook}, {"xl/_rels/workbook.xml.rels", workbookRels},
	} {
		f, err := z.Create(part.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, part.body); err != nil {
			return err
		}
	}
	f, err := z.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(f, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`); err != nil {
		return err
	}
	for i, row := range rows {
		n := strconv.Itoa(i + 1)
		if _, err := io.WriteString(f, `<row r="`+n+`">`); err != nil {
			return err
		}
		for j, cell := range row {
			if _, err := io.WriteString(f, `<c r="`+columnName(j)+n+`" t="inlineStr"><is><t xml:space="preserve">`); err != nil {
				return err
			}
			if err := xml.EscapeText(f, []byte(clean(Neutral(cell)))); err != nil {
				return err
			}
			if _, err := io.WriteString(f, `</t></is></c>`); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(f, `</row>`); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(f, `</sheetData></worksheet>`); err != nil {
		return err
	}
	return z.Close()
}
