package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta"
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
func TestServeNeedsPensieve(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "ROSETTA_PENSIEVE_URL") {
		t.Fatalf("%v", err)
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
