package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/oidc"
	"github.com/Rumeasiyan/rosetta/internal/standin"
	"github.com/jackc/pgx/v5/pgxpool"
)

const clientSecret = "local-stand-in-only"

var testSeq atomic.Int64

// world is ROSETTA's screens against the database and the stand-ins.
type world struct {
	t       *testing.T
	pool    *pgxpool.Pool
	app     *App
	screens *httptest.Server
	standin *httptest.Server
	logs    *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newWorld(t *testing.T) *world {
	t.Helper()
	pool := dbtest.Open(t)
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: clientSecret})
	sts := httptest.NewServer(sd.Handler())
	sd.SetPublicURL(sts.URL)
	t.Cleanup(func() { sd.Close(); sts.Close() })

	w := &world{t: t, pool: pool, standin: sts, logs: &syncBuffer{}}
	logger := slog.New(slog.NewJSONHandler(w.logs, nil))
	mux := http.NewServeMux()
	ts := httptest.NewUnstartedServer(mux)
	ts.Start()
	t.Cleanup(ts.Close)
	w.screens = ts
	app, err := New(Deps{
		Pool: pool, Logger: logger, PublicURL: ts.URL,
		SignIn: oidc.New(oidc.Config{Issuer: sts.URL + "/mjolnir", ClientID: "rosetta", ClientSecret: clientSecret, RedirectURL: ts.URL + oidc.CallbackPath}, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	app.Mount(mux)
	w.app = app
	return w
}

// expireTokens puts every session's access token past its end.
func (w *world) expireTokens() {
	s := w.app.sessions
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.byToken {
		v.expires = time.Now().Add(-time.Second)
	}
}

func (w *world) control(service, mode string, params map[string]string) {
	w.t.Helper()
	b, _ := json.Marshal(map[string]any{"mode": mode, "params": params})
	res, err := http.Post(w.standin.URL+"/control/"+service+"/mode", "application/json", bytes.NewReader(b))
	if err != nil {
		w.t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		w.t.Fatalf("control %s %s: %d", service, mode, res.StatusCode)
	}
}

// received reads back what a stand-in saw.
func (w *world) received(service string) []standin.Received {
	w.t.Helper()
	res, err := http.Get(w.standin.URL + "/control/" + service + "/received")
	if err != nil {
		w.t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Received []standin.Received `json:"received"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		w.t.Fatal(err)
	}
	return out.Received
}

// grants counts the calls of one grant type on MJOLNIR's token address.
func (w *world) grants(kind string) int {
	n := 0
	for _, r := range w.received("mjolnir") {
		if strings.HasSuffix(r.Path, "/oauth2/token") && strings.Contains(r.Body, "grant_type="+kind) {
			n++
		}
	}
	return n
}

// browser is one person's browser: its own cookies, following redirects unless told.
type browser struct {
	w      *world
	client *http.Client
	jar    *cookiejar.Jar
}

func (w *world) browser() *browser {
	jar, err := cookiejar.New(nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return &browser{w: w, jar: jar, client: &http.Client{Jar: jar}}
}

type page struct {
	status int
	header http.Header
	body   string
	url    string
}

func (b *browser) do(req *http.Request) page {
	b.w.t.Helper()
	res, err := b.client.Do(req)
	if err != nil {
		b.w.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return page{res.StatusCode, res.Header, string(body), res.Request.URL.Path}
}

func (b *browser) get(path string) page {
	b.w.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, b.w.screens.URL+path, nil)
	return b.do(req)
}

func (b *browser) post(path string, form url.Values) page {
	b.w.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, b.w.screens.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return b.do(req)
}

// noFollow answers the first response rather than following a redirect.
func (b *browser) noFollow(req *http.Request) page {
	b.w.t.Helper()
	old := b.client.CheckRedirect
	b.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer func() { b.client.CheckRedirect = old }()
	return b.do(req)
}

var formField = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)

// signIn goes through the screens and MJOLNIR's form as subject, ending signed in.
func (b *browser) signIn(subject string) page {
	b.w.t.Helper()
	form := b.get("/signin/start")
	if !strings.Contains(form.body, "MJOLNIR stand-in") {
		return form // MJOLNIR's session was live: it passed with no form
	}
	vals := url.Values{"own": {subject}}
	for _, m := range formField.FindAllStringSubmatch(form.body, -1) {
		v := strings.NewReplacer("&amp;", "&", "&#43;", "+", "&#34;", `"`, "&#39;", "'").Replace(m[2])
		vals.Set(m[1], v)
	}
	res, err := b.client.PostForm(b.w.standin.URL+"/mjolnir/oauth2/auth", vals)
	if err != nil {
		b.w.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return page{res.StatusCode, res.Header, string(body), res.Request.URL.Path}
}

var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// csrf reads the form token from a page the person can see.
func (b *browser) csrf() string {
	b.w.t.Helper()
	p := b.get("/")
	m := csrfField.FindStringSubmatch(p.body)
	if m == nil {
		b.w.t.Fatalf("no form token on the home page: %d %s", p.status, p.body)
	}
	return m[1]
}

func subject(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), testSeq.Add(1))
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.pool.Exec(context.Background(), sql, args...); err != nil {
		w.t.Fatal(err)
	}
}
