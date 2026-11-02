// Package health answers ROSETTA's health check.
//
// The container platform runs `/app -healthcheck` inside the container. That
// command probes the running service's /healthz over the loopback address,
// and the service answers healthy when its database answers.
package health

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Path is where the service answers the health check.
const Path = "/healthz"

// DBTimeout is the longest the database ping may take.
const DBTimeout = 2 * time.Second

// ProbeTimeout is the longest the health check command waits. It is longer
// than DBTimeout so a slow database shows as unhealthy, not as a timeout.
const ProbeTimeout = 3 * time.Second

// Handler answers GET with 200 "ok" when check passes and 503 "unhealthy"
// when it fails. The reason is logged, never sent: a database error can
// name hosts and users.
func Handler(check func(context.Context) error, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := check(r.Context()); err != nil {
			logger.Warn("health check failed", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, "unhealthy\n")
			return
		}
		io.WriteString(w, "ok\n")
	})
}

// DBCheck returns a check that pings the database within DBTimeout.
func DBCheck(pool *pgxpool.Pool) func(context.Context) error {
	return func(ctx context.Context) error {
		if pool == nil {
			return errors.New("database: no connection pool")
		}
		ctx, cancel := context.WithTimeout(ctx, DBTimeout)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("database not reachable: %w", err)
		}
		return nil
	}
}

// LoopbackURL is the health check address for a service on port. The probe
// runs inside the container, so the loopback address always reaches it.
func LoopbackURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port) + Path
}

// Probe asks url for its health within ProbeTimeout. Only 200 is healthy.
func Probe(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("health check address %q is not valid: %w", url, err)
	}
	// A client of its own: no proxy from the environment and no redirects,
	// so the probe only ever talks to the service it was pointed at.
	client := &http.Client{
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("service not reachable at %s: %w", url, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("service at %s answered %s", url, resp.Status)
	}
	return nil
}
