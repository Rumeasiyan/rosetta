package web

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sheet"
	"github.com/jackc/pgx/v5"
)

// A file is held in memory while it is read, whatever its size up to the limit: the root filesystem
// is read-only (§5), so nothing may spill to disk.
const uploadMemory = sheet.MaxFile + 1<<20

// glossaryImportData is the import screen, and what the last file did.
type glossaryImportData struct {
	Limit int
	Done  *glossary.Imported
}

func (a *App) importRows(r *http.Request) int {
	return a.settings.Get(r.Context()).Of(settings.GlossaryImportRows)
}

func (a *App) glossaryImportForm(w http.ResponseWriter, r *http.Request) {
	p := a.page(r, "glossaryImport", "glossary")
	p.Data = glossaryImportData{Limit: a.importRows(r)}
	a.Render(w, r, http.StatusOK, "glossary-import", p)
}

// glossaryImport reads the file and adds its rows as proposals (R-GLO-07). Over the row limit, or a
// file it cannot read, keeps nothing. What the file held is never logged or sent to the audit.
func (a *App) glossaryImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	r.Body = http.MaxBytesReader(w, r.Body, uploadMemory)
	if err := r.ParseMultipartForm(uploadMemory); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			back(w, r, "/glossary/import", "glossaryImportTooBig", "bad")
			return
		}
		a.deny(w, r, "Form token did not match", "badForm")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if subtle.ConstantTimeCompare([]byte(r.FormValue("csrf")), []byte(c.sess.csrf)) != 1 {
		a.deny(w, r, "Form token did not match", "badForm")
		return
	}
	file, head, err := r.FormFile("file")
	if err != nil {
		back(w, r, "/glossary/import", "glossaryImportNotSheet", "bad")
		return
	}
	defer file.Close()
	var data bytes.Buffer
	if _, err := io.Copy(&data, file); err != nil {
		a.fail(w, r, errors.New("the file could not be read"))
		return
	}
	rows, err := sheet.Read(data.Bytes(), a.importRows(r)+1) // the first row names the columns
	switch {
	case errors.Is(err, sheet.ErrTooManyRows):
		back(w, r, "/glossary/import", "glossaryImportTooMany", "bad")
		return
	case errors.Is(err, sheet.ErrTooBig):
		back(w, r, "/glossary/import", "glossaryImportTooBig", "bad")
		return
	case err != nil:
		back(w, r, "/glossary/import", "glossaryImportNotSheet", "bad")
		return
	}
	var done glossary.Imported
	err = a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		var err error
		if done, err = glossary.Import(ctx, tx, head.Filename, rows, c.sess.personID); err != nil {
			return err
		}
		if done.Accepted > 0 {
			if err := glossary.RaiseImport(ctx, tx, done.ID); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionCreation, ObjectType: "glossary import", ObjectRef: strconv.FormatInt(done.ID, 10),
			Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: map[string]any{"rows": done.Rows, "accepted": done.Accepted, "refused": len(done.Refused)}}, now)
	})
	if errors.Is(err, glossary.ErrNoColumns) {
		back(w, r, "/glossary/import", "glossaryImportColumns", "bad")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.tellKeepers()
	p := a.page(r, "glossaryImport", "glossary")
	p.Flash, p.FlashTone = "glossaryImportDone", "good"
	p.Data = glossaryImportData{Limit: a.importRows(r), Done: &done}
	a.Render(w, r, http.StatusOK, "glossary-import", p)
}

// glossaryExport sends the published glossary as a spreadsheet file, with no formula in it. An
// export is an audit entry (P-ING-02).
func (a *App) glossaryExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	rows, err := glossary.Export(ctx, a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	version, err := glossary.Current(ctx, a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	err = a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionExport, ObjectType: "glossary", ObjectRef: "version " + strconv.Itoa(version),
			Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: map[string]any{"rows": len(rows) - 1}}, now)
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
	h.Set("Content-Disposition", `attachment; filename="rosetta-glossary-version-`+strconv.Itoa(version)+`.xlsx"`)
	h.Set("Cache-Control", "no-store")
	SecurityHeaders(h)
	w.WriteHeader(http.StatusOK)
	out.WriteTo(w)
}
