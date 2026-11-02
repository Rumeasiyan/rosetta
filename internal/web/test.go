package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/Rumeasiyan/rosetta/internal/api"
	"github.com/Rumeasiyan/rosetta/internal/callers"
)

// Tester sends a test request through the same pipeline a calling system's goes through.
type Tester interface {
	Test(ctx context.Context, q api.TestRequest) api.TestAnswer
}

var errNoTester = errors.New("web: test requests are not wired up")

// The sensitivities a test may state: the same list a caller has (R-TRN-03).
var testSensitivities = []string{"ordinary", "personal", "confidential"}

func sensitivityWord(s string) string {
	switch s {
	case "personal":
		return "sensitivityPersonal"
	case "confidential":
		return "sensitivityConfidential"
	}
	return "sensitivityOrdinary"
}

// testData is the test screen: what was sent, only to show it again, and the full answer.
type testData struct {
	System      callers.Caller
	Operation   string
	From, To    string
	Sensitivity string
	Text        string
	Answer      *api.TestAnswer
}

func (a *App) mountTest(mux *http.ServeMux) {
	mux.Handle("GET /systems/{id}/test", a.signedIn(a.forSystem(a.testPage)))
	mux.Handle("POST /systems/{id}/test", a.signedIn(a.post(a.forSystem(a.sendTest))))
}

func (a *App) testPage(w http.ResponseWriter, r *http.Request, s callers.Caller) {
	p := a.page(r, "testTitle", "systems")
	p.Data = testData{System: s, Operation: "translate", From: "en", To: "ta", Sensitivity: "ordinary"}
	a.Render(w, r, http.StatusOK, "system-test", p)
}

// sendTest sends the test (R-ADM-04). Its text goes nowhere but the source and this one answer,
// which no browser keeps (Render sends no-store), and is never logged or recorded.
func (a *App) sendTest(w http.ResponseWriter, r *http.Request, s callers.Caller) {
	f := r.PostForm
	d := testData{System: s, Operation: f.Get("operation"), From: f.Get("from"), To: f.Get("to"), Sensitivity: f.Get("sensitivity"), Text: f.Get("text")}
	if d.Operation != "grammar" {
		d.Operation = "translate"
	}
	if a.deps.Tester == nil {
		a.fail(w, r, errNoTester)
		return
	}
	answer := a.deps.Tester.Test(r.Context(), api.TestRequest{Caller: s, Officer: fromCtx(r).sess.subject, Grammar: d.Operation == "grammar",
		From: d.From, To: d.To, Sensitivity: d.Sensitivity, Text: d.Text})
	d.Answer = &answer
	p := a.page(r, "testTitle", "systems")
	p.Data = d
	a.Render(w, r, http.StatusOK, "system-test", p)
}
