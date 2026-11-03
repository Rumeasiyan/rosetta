// Command rosetta-standin plays BUMBLEBEE, MJOLNIR, CERBERUS, BIFROST, PENSIEVE
// and HEDWIG for local work, each under its own path prefix, so ROSETTA's base
// URLs are settings. Development only: it is not in the image and never runs on
// the shared server. Every person and text it holds is invented.
//
//	go run ./cmd/rosetta-standin
//	curl localhost:53190/control
//	curl -d '{"mode":"blocked"}' localhost:53190/control/bumblebee/mode
//	curl localhost:53190/control/pensieve/received
//
// Settings: STANDIN_LISTEN (default :53190), STANDIN_PUBLIC_URL (the address a
// browser reaches it on, default http://localhost:53190), STANDIN_CLIENT_ID and
// STANDIN_CLIENT_SECRET (the one client MJOLNIR's stand-in knows).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/standin"
)

func setting(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func main() {
	listen := setting("STANDIN_LISTEN", ":53190")
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		_, port, _ := strings.Cut(listen, ":")
		c := http.Client{Timeout: 3 * time.Second}
		res, err := c.Get("http://127.0.0.1:" + port + "/control")
		if err != nil || res.StatusCode != http.StatusOK {
			fmt.Fprintln(os.Stderr, "rosetta-standin: not answering")
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	s := standin.New(standin.Options{
		PublicURL:    setting("STANDIN_PUBLIC_URL", "http://localhost:53190"),
		ClientID:     setting("STANDIN_CLIENT_ID", "rosetta"),
		ClientSecret: setting("STANDIN_CLIENT_SECRET", "local-stand-in-only"),
	})
	defer s.Close()
	srv := &http.Server{Addr: listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		s.Close()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	logger.Info("rosetta-standin listening", "listen", listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("rosetta-standin stopped", "error", err)
		os.Exit(1)
	}
}
