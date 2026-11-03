// Package db opens ROSETTA's database.
//
// ROSETTA reaches PostgreSQL only through PgBouncer in transaction mode, with
// a pool of 5 (STACK 4). Transaction mode hands each transaction a different
// server connection, so nothing may rely on session state between
// transactions, and named prepared statements cannot be relied on to survive.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxConns is the most connections the service opens: ROSETTA's pool of 5.
const MaxConns = 5

// OpenTimeout is how long Open waits for the database to answer.
const OpenTimeout = 30 * time.Second

// Open connects to url with at most maxConns connections and checks that
// the database answers.
func Open(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		// The URL carries a password, so the error must not repeat it.
		return nil, errors.New("database URL is not valid")
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = 0
	// Exec mode sends each statement in one round trip (parse, bind and
	// execute before a single sync), with parameter types taken from the Go
	// values. Describe-exec splits the describe from the execute with a sync
	// between them, and outside a transaction PgBouncer may hand the two
	// halves to different server connections: under concurrency that fails
	// with "unnamed prepared statement does not exist". Named prepared
	// statements do not survive transaction pooling either.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, OpenTimeout)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database not reachable: %w", err)
	}
	return pool, nil
}
