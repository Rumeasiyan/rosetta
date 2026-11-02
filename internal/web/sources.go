package web

import (
	"context"
	"net/http"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/sources"
)

// SourceList is BUMBLEBEE's source list as the screens read it.
type SourceList interface {
	Snapshot(ctx context.Context) sources.Snapshot
	MaxText() int
}

func (a *App) mountSources(mux *http.ServeMux) {
	mux.Handle("GET /sources", a.signedIn(a.only(seesSystems, a.sourcesPage)))
}

// stateWord is the message key for a source's state.
func stateWord(state string) string {
	switch state {
	case bumblebee.StateOn:
		return "stateOn"
	case bumblebee.StateRestingBlock:
		return "stateRestBlock"
	case bumblebee.StateRestingChange:
		return "stateRestChange"
	}
	return "stateOff"
}

// sourcesData is the source screen's data. Every source is an outside website, and
// the screen says so beside each, so the Office and system owners can choose
// (R-NFR-13).
type sourcesData struct {
	Snapshot sources.Snapshot
	MaxText  int
}

func (a *App) readSources(ctx context.Context) sourcesData {
	if a.deps.Sources == nil {
		return sourcesData{MaxText: sources.DefaultMaxText}
	}
	return sourcesData{Snapshot: a.deps.Sources.Snapshot(ctx), MaxText: a.deps.Sources.MaxText()}
}

func (a *App) sourcesPage(w http.ResponseWriter, r *http.Request) {
	p := a.page(r, "sources", "sources")
	p.Data = a.readSources(r.Context())
	a.Render(w, r, http.StatusOK, "sources", p)
}
