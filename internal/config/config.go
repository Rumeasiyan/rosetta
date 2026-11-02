// Package config reads ROSETTA's settings from the environment. On the server
// they come from ROSETTA's one env file; nothing is read from disk.
package config

import (
	"errors"
	"fmt"
	"net"
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
	// MIGRATE_DATABASE_URL, read only by `rosetta migrate`, so the running
	// service never holds the owner's password.
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
