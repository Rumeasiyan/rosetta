package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/standin"
	"github.com/jackc/pgx/v5"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestVersion(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"-version"}, &out, &errs, env(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if strings.TrimSpace(out.String()) != rosetta.Version {
		t.Fatalf("printed %q", out.String())
	}
}

func TestNoCommandIsUsage(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run(nil, &out, &errs, env(nil)); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errs.String(), "usage:") {
		t.Fatalf("stderr %q", errs.String())
	}
}

func TestMigrateNeedsItsURL(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"migrate"}, &out, &errs, env(nil)); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errs.String(), "MIGRATE_DATABASE_URL") {
		t.Fatalf("stderr %q", errs.String())
	}
}

// A test set against the stand-in gives one line of what the gate records; nothing holds its text.
func TestEvaluateCommand(t *testing.T) {
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	e := env(map[string]string{"ROSETTA_BUMBLEBEE_URL": ts.URL + "/bumblebee", "ROSETTA_BUMBLEBEE_KEY": standin.BumblebeeKey})
	var out, errs bytes.Buffer
	code := run([]string{"evaluate", "-set", "../../dev/test-set/invented.tsv", "-source", "standin-translate", "-from", "en", "-to", "ta"}, &out, &errs, e)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	for _, want := range []string{`"test_set":"invented:`, `"source":"standin-translate"`, `"source_version":"1"`, `"score":`, `"sentences":10`, `"evaluated_on":`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %s in %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "office") {
		t.Fatal(out.String())
	}
	for name, args := range map[string][]string{
		"no set":         {"evaluate", "-source", "standin-translate", "-from", "en", "-to", "ta"},
		"same languages": {"evaluate", "-set", "../../dev/test-set/invented.tsv", "-source", "standin-translate", "-from", "en", "-to", "en"},
		"no source":      {"evaluate", "-set", "../../dev/test-set/invented.tsv", "-source", "nothing", "-from", "en", "-to", "ta"},
	} {
		out.Reset()
		errs.Reset()
		if code := run(args, &out, &errs, e); code == 0 {
			t.Errorf("%s: passed: %s", name, out.String())
		}
	}
	errs.Reset()
	if code := run([]string{"evaluate", "-set", "x", "-source", "y", "-from", "en", "-to", "ta"}, &out, &errs, env(nil)); code == 0 || !strings.Contains(errs.String(), "ROSETTA_BUMBLEBEE_URL") {
		t.Fatalf("%d %s", code, errs.String())
	}
}

// The service does not start without its sign-in settings.
func TestServeNeedsSignInSettings(t *testing.T) {
	e := env(map[string]string{"DATABASE_URL": "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"})
	var errs bytes.Buffer
	if err := serve(context.Background(), e, &errs, nil); err == nil {
		t.Fatal("started without sign-in settings")
	}
}

// Nor without somewhere to send its audit entries. This reaches the database, since
// the settings are read in order, so it needs the local stack.
func TestServeNeedsTheRegister(t *testing.T) {
	url := os.Getenv("ROSETTA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ROSETTA_TEST_DATABASE_URL not set; start dev/compose.yaml to run database tests")
	}
	e := env(map[string]string{
		"DATABASE_URL":                 url,
		"PARADOX_PUBLIC_KEY":           "ed25519-pub:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"ROSETTA_PUBLIC_URL":           "http://localhost:53080",
		"ROSETTA_SIGNIN_ISSUER":        "http://localhost:53190/mjolnir",
		"ROSETTA_SIGNIN_CLIENT_ID":     "rosetta",
		"ROSETTA_SIGNIN_CLIENT_SECRET": "local-stand-in-only",
		"ROSETTA_SIGNOUT_URL":          "http://localhost:53190/mjolnir/sign-out",
	})
	var logs bytes.Buffer
	err := serve(context.Background(), e, &logs, nil)
	if err == nil || !strings.Contains(err.Error(), "ROSETTA_TVA_DIR") {
		t.Fatalf("%v", err)
	}
}

// PENSIEVE, BIFROST, BUMBLEBEE and HEDWIG each give a key only once ROSETTA has its TVA entry, which
// comes after ROSETTA runs, so it starts with none of them and says what waits.
func TestServeStartsWithNoKeysYet(t *testing.T) {
	url := os.Getenv("ROSETTA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ROSETTA_TEST_DATABASE_URL not set; start dev/compose.yaml to run database tests")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := ln.Addr().String()
	ln.Close()
	e := env(map[string]string{
		"ROSETTA_LISTEN":               free,
		"DATABASE_URL":                 url,
		"PARADOX_PUBLIC_KEY":           "ed25519-pub:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"ROSETTA_PUBLIC_URL":           "http://localhost:53080",
		"ROSETTA_SIGNIN_ISSUER":        "http://localhost:53190/mjolnir",
		"ROSETTA_SIGNIN_CLIENT_ID":     "rosetta",
		"ROSETTA_SIGNIN_CLIENT_SECRET": "local-stand-in-only",
		"ROSETTA_SIGNOUT_URL":          "http://localhost:53190/mjolnir/sign-out",
		"ROSETTA_TVA_S3_ENDPOINT":      "http://127.0.0.1:1",
		"ROSETTA_TVA_S3_KEY":           "invented",
		"ROSETTA_TVA_S3_SECRET":        "invented",
	})
	ready := make(chan string, 1)
	var logs safeBuffer
	done := make(chan error, 1)
	go func() { done <- serve(ctx, e, &logs, ready) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve stopped: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("stop: %v", err)
	}
	for _, want := range []string{"no PENSIEVE key", "no BIFROST key", "no BUMBLEBEE key"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("no word of %q in %s", want, logs.String())
		}
	}
}

// safeBuffer is a log the service's timer may write to while a test reads it.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// R-NFR-08: a request in progress when the service stops fails with a reason, not a cut connection,
// and may be sent again.
func TestARequestCutByAStopFailsWithAReason(t *testing.T) {
	url := os.Getenv("ROSETTA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ROSETTA_TEST_DATABASE_URL not set; start dev/compose.yaml to run database tests")
	}
	pool := dbtest.Open(t)
	bg := context.Background()
	for _, sql := range []string{`DELETE FROM cache_piece`, `DELETE FROM source_order`, `DELETE FROM gate_decision`, `DELETE FROM source_offer`, `DELETE FROM source`} {
		if _, err := pool.Exec(bg, sql); err != nil {
			t.Fatal(err)
		}
	}
	var person, system, source int64
	stamp := time.Now().Format("150405.000000")
	if err := pool.QueryRow(bg, `INSERT INTO person (mjolnir_subject, name, email) VALUES ($1, 'Invented', 'stop@example.invalid') RETURNING id`, "stop-"+stamp).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(bg, `INSERT INTO caller (tva_reference, name, tva_status, tva_checked_at) VALUES ($1, 'Invented', 'live', now()) RETURNING id`, "STOP-"+stamp).Scan(&system); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(bg, `INSERT INTO source (name, version, state, seen_at) VALUES ('standin-translate', '1', 'on', now()) RETURNING id`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(bg, `INSERT INTO gate_decision (source_id, from_language, to_language, source_version, state, test_set_version, score, evaluated_on, decided_by)
		VALUES ($1, 'en', 'ta', '1', 'open', 'stop-set', 80, '2020-01-01', $2)`, source, person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM cache_piece`, `DELETE FROM gate_decision`, `DELETE FROM source_offer`, `DELETE FROM source`} {
			pool.Exec(bg, sql)
		}
	})
	var key string
	if err := pgx.BeginFunc(bg, pool, func(tx pgx.Tx) error {
		var err error
		key, _, err = callers.Issue(bg, tx, system, person, "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := ln.Addr().String()
	ln.Close()
	e := env(map[string]string{
		"ROSETTA_LISTEN": free, "DATABASE_URL": url, "PARADOX_PUBLIC_KEY": "ed25519-pub:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"ROSETTA_PUBLIC_URL": "http://localhost:53080", "ROSETTA_SIGNIN_ISSUER": ts.URL + "/mjolnir", "ROSETTA_SIGNIN_CLIENT_ID": "rosetta", "ROSETTA_SIGNIN_CLIENT_SECRET": "x",
		"ROSETTA_SIGNOUT_URL": "http://localhost:53190/mjolnir/sign-out", "ROSETTA_PENSIEVE_URL": ts.URL + "/pensieve", "ROSETTA_PENSIEVE_KEY": "pv_standin", "ROSETTA_TVA_DIR": "../../dev/tva",
		"ROSETTA_BUMBLEBEE_URL": ts.URL + "/bumblebee", "ROSETTA_BUMBLEBEE_KEY": standin.BumblebeeKey, "ROSETTA_BIFROST_URL": ts.URL + "/bifrost", "ROSETTA_BIFROST_KEY": standin.BifrostKey,
	})
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	ready := make(chan string, 1)
	var logs bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- serve(ctx, e, &logs, ready) }()
	addr := <-ready
	// The list is read first; only then does the source go slow.
	translate := func() (*http.Response, error) {
		r, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api/v1/translate",
			strings.NewReader(`{"from":"en","to":"ta","sensitivity":"ordinary","passages":["An invented sentence caught by a stop `+stamp+`."]}`))
		r.Header.Set("Authorization", "Bearer "+key)
		return http.DefaultClient.Do(r)
	}
	for i := 0; ; i++ {
		res, err := translate()
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if i > 100 {
			t.Fatal("never answered", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	res, err := http.Post(ts.URL+"/control/bumblebee/mode", "application/json", strings.NewReader(`{"mode":"slow","params":{"delay_ms":"40000"}}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	type answer struct {
		status int
		body   string
		err    error
	}
	got := make(chan answer, 1)
	go func() {
		r, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api/v1/translate",
			strings.NewReader(`{"from":"en","to":"ta","sensitivity":"personal","passages":["Another invented sentence caught by a stop."]}`))
		r.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			got <- answer{err: err}
			return
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		got <- answer{status: res.StatusCode, body: string(raw)}
	}()
	time.Sleep(500 * time.Millisecond)
	stopped := time.Now()
	cancel()
	a := <-got
	if a.err != nil {
		t.Fatalf("a cut connection, not a reason: %v", a.err)
	}
	if a.status != 504 || !strings.Contains(a.body, `"timed out"`) {
		t.Fatalf("%d %s", a.status, a.body)
	}
	if err := <-done; err != nil {
		t.Fatalf("stop: %v", err)
	}
	if took := time.Since(stopped); took > ShutdownGrace {
		t.Fatalf("the stop took %v", took)
	}
}

// With nothing listening, the health check fails rather than passes.
func TestHealthcheckFailsWhenNothingListens(t *testing.T) {
	var out, errs bytes.Buffer
	e := env(map[string]string{"ROSETTA_LISTEN": "127.0.0.1:1"})
	if code := run([]string{"-healthcheck"}, &out, &errs, e); code != 1 {
		t.Fatalf("exit %d", code)
	}
}

// The service logs JSON, and the health check passes while it runs.
func TestServeAnswersHealthAndLogsJSON(t *testing.T) {
	url := os.Getenv("ROSETTA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ROSETTA_TEST_DATABASE_URL not set; start dev/compose.yaml to run database tests")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := ln.Addr().String()
	ln.Close()
	e := env(map[string]string{
		"ROSETTA_LISTEN":               free,
		"DATABASE_URL":                 url,
		"PARADOX_PUBLIC_KEY":           "ed25519-pub:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"ROSETTA_PUBLIC_URL":           "http://localhost:53080",
		"ROSETTA_SIGNIN_ISSUER":        "http://localhost:53190/mjolnir",
		"ROSETTA_SIGNIN_CLIENT_ID":     "rosetta",
		"ROSETTA_SIGNIN_CLIENT_SECRET": "local-stand-in-only",
		"ROSETTA_SIGNOUT_URL":          "http://localhost:53190/mjolnir/sign-out",
		"ROSETTA_PENSIEVE_URL":         "http://localhost:53190/pensieve",
		"ROSETTA_PENSIEVE_KEY":         "pv_standin",
		"ROSETTA_TVA_DIR":              "../../dev/tva",
		"ROSETTA_BUMBLEBEE_URL":        "http://localhost:53190/bumblebee",
		"ROSETTA_BUMBLEBEE_KEY":        "bb_standin",
		"ROSETTA_BIFROST_URL":          "http://localhost:53190/bifrost",
		"ROSETTA_BIFROST_KEY":          "bf_standin",
	})
	ready := make(chan string, 1)
	var logs bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- serve(ctx, e, &logs, ready) }()
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("serve stopped: %v", err)
	}
	h := env(map[string]string{"ROSETTA_LISTEN": addr})
	var out, errs bytes.Buffer
	if code := run([]string{"-healthcheck"}, &out, &errs, h); code != 0 {
		t.Fatalf("healthcheck exit %d: %s", code, errs.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !strings.HasPrefix(logs.String(), "{") {
		t.Fatalf("logs are not JSON: %q", logs.String())
	}
}
