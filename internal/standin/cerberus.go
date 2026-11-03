package standin

import (
	"net/http"
	"net/url"
)

// The first mode allows. CERBERUS's stand-in allows, refuses shortly, or does
// not answer (E-ENB-03); signed-out is the other refusal in JOINING.md.
// "cookie" decides by the sign-in cookie, as the real check does: the cookie's value is the
// account, and with no cookie the answer is signed-out's. A browser walk needs this, since it
// shows whether a browser sent the cookie at all (dev/plugin-test).
var cerberusModes = []string{"allow", "refuse", "signed-out", "slow", "silent", "cookie"}

// signInCookie stands for MJOLNIR's cookie: the stand-in reads it, and Caddy removes it after
// the check. The real name is not needed here.
const signInCookie = "standin_session"

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
	signedIn := ""
	if mode == "cookie" {
		if c, err := r.Cookie(signInCookie); err == nil {
			signedIn = c.Value
		}
		if signedIn == "" {
			mode = "signed-out"
		}
	}
	switch mode {
	case "refuse":
		refuseCORS()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Refused by an office rule. Reference: standin-1\n"))
	case "signed-out":
		if cerberusPage(r) {
			http.Redirect(w, r, s.issuer()+"/oauth2/auth", http.StatusSeeOther)
			return
		}
		refuseCORS()
		w.Header().Set("HX-Refresh", "true")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Please reload this page and sign in again.\n"))
	default:
		a := Accounts[0]
		subject := p["account"]
		if signedIn != "" {
			subject = signedIn
		}
		if subject != "" {
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

// cerberusPage says whether this is a page being opened, which goes to sign-in, rather than a
// form or background request, which gets a 401. A browser's XHR says so in Sec-Fetch-Mode.
func cerberusPage(r *http.Request) bool {
	method := r.Header.Get("X-Forwarded-Method")
	if method != "" && method != http.MethodGet && method != http.MethodHead || r.Header.Get("HX-Request") != "" {
		return false
	}
	mode := r.Header.Get("Sec-Fetch-Mode")
	return mode == "" || mode == "navigate"
}
