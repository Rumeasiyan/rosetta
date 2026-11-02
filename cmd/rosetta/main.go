// Command rosetta is the translation and grammar service of the Eastern Province.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Rumeasiyan/rosetta"
	"github.com/Rumeasiyan/rosetta/internal/api"
	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/config"
	"github.com/Rumeasiyan/rosetta/internal/db"
	"github.com/Rumeasiyan/rosetta/internal/health"
	"github.com/Rumeasiyan/rosetta/internal/jobs"
	"github.com/Rumeasiyan/rosetta/internal/oidc"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/tva"
	"github.com/Rumeasiyan/rosetta/internal/web"
)

const usage = `usage:
  rosetta migrate        bring the schema up to date (MIGRATE_DATABASE_URL)
  rosetta serve          run ROSETTA (settings from the environment; see .env.example)
  rosetta -version       print the version
  rosetta -healthcheck   exit 0 if the running service's database answers
`

// ShutdownGrace is how long requests in flight may finish after a stop signal.
const ShutdownGrace = 10 * time.Second

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// run is main without the process exit, so tests can drive it.
func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) > 0 {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		switch args[0] {
		case "migrate":
			return report(stderr, migrate(ctx, getenv))
		case "serve":
			return report(stderr, serve(ctx, getenv, stderr, nil))
		}
	}
	fs := flag.NewFlagSet("rosetta", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	showVersion := fs.Bool("version", false, "print the version and exit")
	healthcheck := fs.Bool("healthcheck", false, "exit 0 if the running service's database answers")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	switch {
	case *showVersion:
		fmt.Fprintln(stdout, rosetta.Version)
		return 0
	case *healthcheck:
		return report(stderr, runHealthcheck(getenv))
	}
	fmt.Fprint(stderr, usage)
	return 2
}

func report(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintln(stderr, "rosetta:", err)
	return 1
}

func runHealthcheck(getenv func(string) string) error {
	listen := strings.TrimSpace(getenv("ROSETTA_LISTEN"))
	if listen == "" {
		listen = config.DefaultListen
	}
	port, err := config.ListenPort(listen)
	if err != nil {
		return err
	}
	return health.Probe(context.Background(), health.LoopbackURL(port))
}

func migrate(ctx context.Context, getenv func(string) string) error {
	url := strings.TrimSpace(getenv("MIGRATE_DATABASE_URL"))
	if url == "" {
		return errors.New("MIGRATE_DATABASE_URL is not set (through the session-mode entry)")
	}
	pool, err := db.Open(ctx, url, 1)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	defer pool.Close()
	return db.Migrate(ctx, pool)
}

// serve runs until ctx ends. ready, when not nil, receives the listening address.
func serve(ctx context.Context, getenv func(string) string, logs io.Writer, ready chan<- string) error {
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	cfg, err := config.FromEnv(getenv)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL, db.MaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	keys, err := jobs.ParseKeys(getenv("PARADOX_PUBLIC_KEY"))
	if err != nil {
		return err
	}

	signin, err := oidc.FromEnv(getenv)
	if err != nil {
		return err
	}
	public, err := oidc.PublicURL(getenv)
	if err != nil {
		return err
	}
	links, err := config.LinksFromEnv(getenv)
	if err != nil {
		return err
	}
	if links.PensieveURL == "" || links.PensieveKey == "" {
		return errors.New("ROSETTA_PENSIEVE_URL and ROSETTA_PENSIEVE_KEY are not set (audit entries; see .env.example)")
	}
	if links.TVADir == "" {
		return errors.New("ROSETTA_TVA_DIR is not set (the system register's file; see .env.example)")
	}
	if links.BumblebeeURL == "" || links.BumblebeeKey == "" {
		return errors.New("ROSETTA_BUMBLEBEE_URL and ROSETTA_BUMBLEBEE_KEY are not set (browser sources; see .env.example)")
	}
	values := settings.NewCached(settings.Defaults(), func(ctx context.Context) (settings.Values, error) {
		return settings.Load(ctx, pool)
	}, settings.RefreshEvery, logger)
	provider := bumblebee.Client{URL: links.BumblebeeURL, Key: links.BumblebeeKey, HTTP: &http.Client{}}
	book := &sources.Book{
		Read: provider.Sources,
		MaxAge: func(ctx context.Context) time.Duration {
			return time.Duration(values.Get(ctx).Of(settings.SourceListSeconds)) * time.Second
		},
		Kept: func(ctx context.Context, l bumblebee.List, at time.Time) error {
			return sources.Mirror(ctx, pool, l, at)
		},
		Logger: logger,
	}
	reading, stopReading := context.WithCancel(ctx)
	var readers sync.WaitGroup
	readers.Go(func() { book.Run(reading) })
	defer readers.Wait()
	defer stopReading()
	outbox := &audit.Outbox{Pool: pool, Sender: audit.Pensieve{URL: links.PensieveURL, Key: links.PensieveKey}, Logger: logger}
	defer outbox.Wait()
	screens, err := web.New(web.Deps{Pool: pool, SignIn: oidc.New(signin, nil), PublicURL: public, FirstAdmin: strings.TrimSpace(getenv("ROSETTA_FIRST_ADMIN")), Outbox: outbox, TVA: tva.Dir(links.TVADir), Sources: book, Logger: logger})
	if err != nil {
		return err
	}

	calls := api.New(api.Deps{Keys: api.PoolKeys{Pool: pool}, Denied: api.PoolDenials(pool, outbox.Kick), Logger: logger})

	mux := http.NewServeMux()
	screens.Mount(mux)
	calls.Mount(mux)
	mux.Handle(health.Path, health.Handler(health.DBCheck(pool), logger))
	mux.Handle(jobs.Prefix, jobs.Handler(pool, keys, jobs.Set(
		jobs.Job{Name: "nightly", Run: jobs.Nothing},    // E-REC-03
		jobs.Job{Name: "partitions", Run: jobs.Nothing}, // E-REC-03
		jobs.Job{Name: "daily", Run: jobs.Nothing},      // E-CAL-05
		jobs.Job{Name: "audit-catch-up", Run: outbox.Job},
	), logger))

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	if ready != nil {
		ready <- ln.Addr().String()
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	logger.Info("rosetta listening", "listen", cfg.Listen, "version", rosetta.Version)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), ShutdownGrace)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}
