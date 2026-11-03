// Package standin plays every service ROSETTA talks to, for development only
// (E-ENB-03). One address serves each under its own path prefix, so a base URL
// is a setting and Go live swaps settings, not code. A control address sets
// each one's failure mode and reads back what it received.
//
// Each stand-in follows the published contract in its owner's repository:
// BUMBLEBEE docs/CALLERS.md, MJOLNIR docs/SESSION-INTERFACE.md, CERBERUS
// docs/JOINING.md, BIFROST docs/EXCHANGE.md, PENSIEVE docs/INTAKE.md and HEDWIG
// docs/API.md. TVA is not served: its stand-in is the folder dev/tva. PARADOX's
// is cmd/rosetta-paradoxcall. This package is never part of the image.
package standin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// keepReceived is how many requests each stand-in remembers.
const keepReceived = 500

// maxBody is the most read from any one request.
const maxBody = 8 << 20

// silentLimit is the longest a stand-in told to stop answering holds a request.
const silentLimit = 5 * time.Minute

// Received is one request a stand-in saw. Keys and secrets are never kept: the
// scheme of the Authorization header shows that one came, and the secret parts
// of a form are replaced.
type Received struct {
	At     time.Time         `json:"at"`
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Query  string            `json:"query,omitempty"`
	Auth   string            `json:"auth,omitempty"`
	Header map[string]string `json:"header,omitempty"`
	Body   string            `json:"body,omitempty"`
}

// service is one stand-in's state.
type service struct {
	modes    []string
	mode     string
	params   map[string]string
	received []Received
}

// Server is the whole stand-in. Make one with New.
type Server struct {
	public string // the address a browser reaches this on, without a trailing slash

	mu       sync.Mutex
	services map[string]*service
	stop     chan struct{}
	stopOnce sync.Once

	mjolnir  *mjolnirState
	pensieve *pensieveState
	hedwig   *hedwigState
}

// Options are the stand-in's settings.
type Options struct {
	PublicURL    string // the address a browser reaches it on: the issuer's base
	ClientID     string // the one client MJOLNIR's stand-in knows
	ClientSecret string
}

// New makes a stand-in with every service in its normal mode.
func New(o Options) *Server {
	s := &Server{public: strings.TrimRight(o.PublicURL, "/"), stop: make(chan struct{})}
	s.services = map[string]*service{
		"bumblebee": {modes: bumblebeeModes},
		"mjolnir":   {modes: mjolnirModes},
		"cerberus":  {modes: cerberusModes},
		"bifrost":   {modes: bifrostModes()},
		"pensieve":  {modes: pensieveModes},
		"hedwig":    {modes: hedwigModes},
	}
	s.mjolnir = newMjolnir(o.ClientID, o.ClientSecret)
	s.resetAll()
	return s
}

// SetPublicURL names the address a browser reaches the stand-in on, for a test
// that learns it only after starting.
func (s *Server) SetPublicURL(u string) { s.public = strings.TrimRight(u, "/") }

// Close releases requests held by a silent stand-in.
func (s *Server) Close() { s.stopOnce.Do(func() { close(s.stop) }) }

func (s *Server) resetAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.services {
		v.mode, v.params, v.received = v.modes[0], map[string]string{}, nil
	}
	s.pensieve = &pensieveState{kept: map[string]pensieveKept{}}
	s.hedwig = &hedwigState{byKey: map[string]json.RawMessage{}, notices: map[int64]json.RawMessage{}}
	s.mjolnir.reset()
}

// mode reads one service's mode and a copy of its params.
func (s *Server) mode(name string) (string, map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.services[name]
	p := make(map[string]string, len(v.params))
	for k, x := range v.params {
		p[k] = x
	}
	return v.mode, p
}

func intParam(p map[string]string, name string, fallback int) int {
	if n, err := strconv.Atoi(p[name]); err == nil {
		return n
	}
	return fallback
}

// Handler serves every stand-in and the control address.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /control", s.controlList)
	mux.HandleFunc("POST /control/reset", s.controlReset)
	mux.HandleFunc("POST /control/{service}/mode", s.controlMode)
	mux.HandleFunc("GET /control/{service}/received", s.controlReceived)
	s.mountBumblebee(mux)
	s.mountMjolnir(mux)
	s.mountCerberus(mux)
	s.mountBifrost(mux)
	s.mountPensieve(mux)
	s.mountHedwig(mux)
	return mux
}

// serve wraps a stand-in's handler: it records the request, then applies the
// modes every stand-in shares (silent and slow).
func (s *Server) serve(name string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, maxBody))
		r.Body = io.NopCloser(bytes.NewReader(body))
		s.record(name, r, body)
		mode, p := s.mode(name)
		switch mode {
		case "silent":
			t := time.NewTimer(silentLimit)
			defer t.Stop()
			select {
			case <-r.Context().Done():
			case <-s.stop:
			case <-t.C:
			}
			return
		case "slow":
			t := time.NewTimer(time.Duration(intParam(p, "delay_ms", 2000)) * time.Millisecond)
			defer t.Stop()
			select {
			case <-t.C:
			case <-r.Context().Done():
				return
			case <-s.stop:
				return
			}
		}
		next(w, r)
	}
}

// secretFields are the form fields a read-back never shows.
var secretFields = []string{"client_secret", "code", "code_verifier", "refresh_token", "token"}

// recordedHeaders are the headers worth reading back.
var recordedHeaders = []string{"Content-Type", "Hx-Request", "Origin", "X-Forwarded-Method", "X-Forwarded-Uri", "X-Timer-Request"}

func (s *Server) record(name string, r *http.Request, body []byte) {
	rec := Received{At: time.Now().UTC(), Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body)}
	if scheme, _, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok {
		rec.Auth = scheme
	}
	for _, h := range recordedHeaders {
		if v := r.Header.Get(h); v != "" {
			if rec.Header == nil {
				rec.Header = map[string]string{}
			}
			rec.Header[h] = v
		}
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		if form, err := url.ParseQuery(string(body)); err == nil {
			for _, f := range secretFields {
				if form.Has(f) {
					form.Set(f, "(sent)")
				}
			}
			rec.Body = form.Encode()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.services[name]
	v.received = append(v.received, rec)
	if len(v.received) > keepReceived {
		v.received = v.received[len(v.received)-keepReceived:]
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) controlList(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type one struct {
		Name     string            `json:"name"`
		Mode     string            `json:"mode"`
		Modes    []string          `json:"modes"`
		Params   map[string]string `json:"params"`
		Received int               `json:"received"`
	}
	out := []one{}
	for name, v := range s.services {
		out = append(out, one{name, v.mode, v.modes, v.params, len(v.received)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"services": out})
}

func (s *Server) controlReset(w http.ResponseWriter, _ *http.Request) {
	s.resetAll()
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (s *Server) controlMode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode   string            `json:"mode"`
		Params map[string]string `json:"params"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body is not JSON"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.services[r.PathValue("service")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such service"})
		return
	}
	if !slices.Contains(v.modes, in.Mode) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no such mode", "modes": v.modes})
		return
	}
	v.mode, v.params = in.Mode, in.Params
	if v.params == nil {
		v.params = map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": v.mode, "params": v.params})
}

func (s *Server) controlReceived(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.services[r.PathValue("service")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such service"})
		return
	}
	out := v.received
	if out == nil {
		out = []Received{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"received": out})
}
