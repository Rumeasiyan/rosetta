// Package config reads ROSETTA's settings from the environment. On the server
// they come from ROSETTA's one env file; nothing is read from disk.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// DefaultListen is where ROSETTA listens when ROSETTA_LISTEN is unset: port
// 8080 on every address, as the platform's Caddy site expects.
const DefaultListen = ":8080"

// Config holds the settings the service needs to start. Other packages read
// their own settings (stand-ins, PARADOX's key).
type Config struct {
	Listen string // ROSETTA_LISTEN, host:port
	// DatabaseURL reaches ROSETTA's database through PgBouncer in transaction
	// mode, as the service role. It
	// carries a password: never log or print it. Schema changes use
	// MIGRATE_DATABASE_URL, the session-mode entry, read only by `rosetta migrate`.
	DatabaseURL string
}

// FromEnv reads ROSETTA_LISTEN and DATABASE_URL. Pass os.Getenv.
func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		Listen:      strings.TrimSpace(getenv("ROSETTA_LISTEN")),
		DatabaseURL: strings.TrimSpace(getenv("DATABASE_URL")),
	}
	if cfg.Listen == "" {
		cfg.Listen = DefaultListen
	}
	if _, err := ListenPort(cfg.Listen); err != nil {
		return cfg, err
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("DATABASE_URL is not set")
	}
	return cfg, nil
}

// Links are the addresses and keys of the services ROSETTA calls. Every one is
// a setting, so Go live swaps settings, not code (E-ENB-03): locally they name
// the stand-ins in cmd/rosetta-standin, on the server the real services. An
// address has no trailing slash, and a caller adds the path its owner documents.
type Links struct {
	BumblebeeURL, BumblebeeKey string // ROSETTA_BUMBLEBEE_URL, _KEY
	BifrostURL, BifrostKey     string // ROSETTA_BIFROST_URL, _KEY
	PensieveURL, PensieveKey   string // ROSETTA_PENSIEVE_URL, _KEY
	HedwigURL, HedwigKey       string // ROSETTA_HEDWIG_URL, _KEY
}

// LinksFromEnv reads the links. An address that is set must be http or https;
// the story that first calls a service makes its address required.
func LinksFromEnv(getenv func(string) string) (Links, error) {
	var l Links
	for _, a := range []struct {
		name     string
		url, key *string
	}{
		{"BUMBLEBEE", &l.BumblebeeURL, &l.BumblebeeKey},
		{"BIFROST", &l.BifrostURL, &l.BifrostKey},
		{"PENSIEVE", &l.PensieveURL, &l.PensieveKey},
		{"HEDWIG", &l.HedwigURL, &l.HedwigKey},
	} {
		v := strings.TrimRight(strings.TrimSpace(getenv("ROSETTA_"+a.name+"_URL")), "/")
		if err := checkAddress("ROSETTA_"+a.name+"_URL", v); err != nil {
			return l, err
		}
		*a.url, *a.key = v, strings.TrimSpace(getenv("ROSETTA_"+a.name+"_KEY"))
	}
	return l, nil
}

func checkAddress(name, v string) error {
	if v == "" {
		return nil
	}
	if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%s is not an http or https address", name)
	}
	return nil
}

// ListenPort returns the port of a listen address such as ":8080",
// "0.0.0.0:8080" or "[::]:8080".
func ListenPort(listen string) (int, error) {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 0, fmt.Errorf("ROSETTA_LISTEN %q is not host:port", listen)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("ROSETTA_LISTEN %q has no port from 1 to 65535", listen)
	}
	return n, nil
}
