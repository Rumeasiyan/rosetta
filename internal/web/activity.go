package web

import (
	"net/http"
	"strings"
)

// activityParts are the parts of the screens with their own idle limit, as
// docs/cerberus-declaration.yaml declares them; a test holds the two together. The
// signal goes to an address under the page's own part, so that past that part's limit
// it is refused while what the person typed is still on the page.
var activityParts = []string{
	"/people", "/settings", "/gate", "/sources", "/status", "/systems", "/usage", "/memory",
	"/glossary/import", "/glossary/export",
}

const activitySuffix = "/activity"

// activityPath is the address the signal goes to from a page at this path.
func activityPath(path string) string {
	for _, part := range activityParts {
		if path == part || strings.HasPrefix(path, part+"/") {
			return part + activitySuffix
		}
	}
	return activitySuffix
}

// mountActivity answers the signal with 204 and no work. It reads no session and
// changes nothing: the request's use is that CERBERUS saw it.
func (a *App) mountActivity(mux *http.ServeMux) {
	answer := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	mux.HandleFunc("POST "+activitySuffix, answer)
	for _, part := range activityParts {
		mux.HandleFunc("POST "+part+activitySuffix, answer)
	}
}
