package standin

import (
	"net/http"
	"net/url"
)

// The first mode allows. CERBERUS's stand-in allows, refuses shortly, or does
// not answer (E-ENB-03); signed-out is the other refusal in JOINING.md.
var cerberusModes = []string{"allow", "refuse", "signed-out", "slow", "silent"}

func (s *Server) mountCerberus(mux *http.ServeMux) {
	mux.HandleFunc("/cerberus/check", s.serve("cerberus", s.cerberusCheck))
}

// cerberusCheck answers as forward_auth expects: Caddy passes the first request's
// method and address in X-Forwarded-Method and X-Forwarded-Uri.
func (s *Server) cerberusCheck(w http.ResponseWriter, r *http.Request) {
	mode, p := s.mode("cerberus")
	// JOINING.md: a refusal to a registered page's address carries CORS headers.
	refuseCORS := func() {
		if o := r.Header.Get("Origin"); o != "" && o == p["registered_origin"] {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
		}
	}
	switch mode {
	case "refuse":
		refuseCORS()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Refused by an office rule. Reference: standin-1\n"))
	case "signed-out":
		method := r.Header.Get("X-Forwarded-Method")
		if method == "" || method == http.MethodGet && r.Header.Get("HX-Request") == "" {
			http.Redirect(w, r, s.issuer()+"/oauth2/auth", http.StatusSeeOther)
			return
		}
		refuseCORS()
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusUnauthorized)
	default:
		a := Accounts[0]
		if subject := p["account"]; subject != "" {
			a = Account{Subject: subject, Name: "Test " + subject, Email: subject + "@example.invalid"}
		}
		h := w.Header()
		h.Set("X-Cerberus-Account", a.Subject)
		h.Set("X-Cerberus-Email", a.Email)
		h.Set("X-Cerberus-Name", url.PathEscape(a.Name))
		h.Set("X-Cerberus-Level", "ordinary")
		h.Set("X-Cerberus-Passkey", "no")
		w.WriteHeader(http.StatusOK)
	}
}
