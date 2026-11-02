package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnv(t *testing.T) {
	const db = "postgres://rosetta:x@localhost:6432/rosetta"
	tests := []struct {
		name     string
		env      map[string]string
		listen   string
		errMatch string
	}{
		{"defaults", map[string]string{"DATABASE_URL": db}, ":8080", ""},
		{"own listen", map[string]string{"DATABASE_URL": db, "ROSETTA_LISTEN": "127.0.0.1:9090"}, "127.0.0.1:9090", ""},
		{"no database", map[string]string{}, "", "DATABASE_URL"},
		{"bad listen", map[string]string{"DATABASE_URL": db, "ROSETTA_LISTEN": "8080"}, "", "host:port"},
		{"port out of range", map[string]string{"DATABASE_URL": db, "ROSETTA_LISTEN": ":70000"}, "", "1 to 65535"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := FromEnv(env(tt.env))
			if tt.errMatch != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMatch) {
					t.Fatalf("err = %v, want one containing %q", err, tt.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Listen != tt.listen || cfg.DatabaseURL != db {
				t.Fatalf("got listen %q, want %q", cfg.Listen, tt.listen)
			}
		})
	}
}

func TestErrorsNeverRepeatTheDatabaseURL(t *testing.T) {
	_, err := FromEnv(env(map[string]string{"DATABASE_URL": "postgres://u:secret@h/d", "ROSETTA_LISTEN": "bad"}))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v; must exist and must not contain the password", err)
	}
}

func TestLinksAreSettings(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	l, err := LinksFromEnv(get(map[string]string{
		"ROSETTA_BUMBLEBEE_URL": "http://standin:53090/bumblebee/",
		"ROSETTA_BUMBLEBEE_KEY": " bb_test ",
		"ROSETTA_TVA_DIR":       "dev/tva",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if l.BumblebeeURL != "http://standin:53090/bumblebee" || l.BumblebeeKey != "bb_test" || l.TVADir != "dev/tva" {
		t.Fatalf("%+v", l)
	}
	if _, err := LinksFromEnv(get(map[string]string{"ROSETTA_PENSIEVE_URL": "ftp://a"})); err == nil || !strings.Contains(err.Error(), "ROSETTA_PENSIEVE_URL") {
		t.Fatalf("an address that is not http was taken: %v", err)
	}
}
