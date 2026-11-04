package tva_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/tva"
)

const secret = "an-invented-secret"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func bucket(url string) tva.Bucket {
	return tva.Bucket{Endpoint: url, Name: tva.DefaultBucket, Key: "invented-key", Secret: secret, Region: "us-east-1"}
}

func TestBucketReadsOneFileAndSigns(t *testing.T) {
	var path, query, auth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query, auth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.Write([]byte(`{"format":1,"systems":[{"reference":"EP-0001","status":"live"}]}` + "\n"))
	}))
	defer ts.Close()
	f, err := bucket(ts.URL + "/").Read(context.Background())
	if err != nil || len(f.Systems) != 1 {
		t.Fatal(f, err)
	}
	if path != "/tva-lookup/systems.json" || query != "" {
		t.Fatalf("asked for %q %q: only the file, and never a list", path, query)
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=invented-key/") || strings.Contains(auth, secret) {
		t.Fatal(auth)
	}
}

// Before TVA's first daily run there is no file, and after it a list with nobody on it. The first
// is a failed read, the second an empty file: flags keeps its copies on either.
func TestNoFileYetAndAnEmptyList(t *testing.T) {
	body := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if body == "" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(secret))
			return
		}
		w.Write([]byte(body))
	}))
	defer ts.Close()
	_, err := bucket(ts.URL).Read(context.Background())
	if err == nil || !strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), secret) {
		t.Fatal(err)
	}
	body = `{"format":1,"systems":[]}`
	if f, err := bucket(ts.URL).Read(context.Background()); err != nil || len(f.Systems) != 0 {
		t.Fatal(f, err)
	}
	body = `{"format":2,"systems":[]}`
	if _, err := bucket(ts.URL).Read(context.Background()); !errors.Is(err, tva.ErrFormat) {
		t.Fatal(err)
	}
}

func TestAnUnreachableBucketNamesNoKey(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	ts.Close()
	_, err := bucket(ts.URL).Read(context.Background())
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "invented-key") {
		t.Fatal(err)
	}
}

func TestFromEnv(t *testing.T) {
	r, err := tva.FromEnv(env(map[string]string{"ROSETTA_TVA_DIR": "dev/tva"}))
	if d, ok := r.(tva.Dir); err != nil || !ok || d != "dev/tva" {
		t.Fatal(r, err)
	}
	r, err = tva.FromEnv(env(map[string]string{"ROSETTA_TVA_DIR": "dev/tva", "ROSETTA_TVA_S3_ENDPOINT": "http://caddy:8333", "ROSETTA_TVA_S3_KEY": "k", "ROSETTA_TVA_S3_SECRET": "s"}))
	if b, ok := r.(tva.Bucket); err != nil || !ok || b.Name != "tva-lookup" || b.Region != "us-east-1" {
		t.Fatal(r, err)
	}
	for _, m := range []map[string]string{
		{},
		{"ROSETTA_TVA_S3_ENDPOINT": "http://caddy:8333"},
		{"ROSETTA_TVA_S3_ENDPOINT": "http://caddy:8333", "ROSETTA_TVA_S3_KEY": "k"},
		{"ROSETTA_TVA_S3_ENDPOINT": "caddy", "ROSETTA_TVA_S3_KEY": "k", "ROSETTA_TVA_S3_SECRET": "s"},
	} {
		if _, err := tva.FromEnv(env(m)); err == nil {
			t.Errorf("%v started", m)
		}
	}
}
