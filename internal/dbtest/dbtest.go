// Package dbtest gives tests the local stack's database (dev/compose.yaml),
// migrated. Tests that use it skip when ROSETTA_TEST_DATABASE_URL is not set.
package dbtest

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open migrates the database and returns a pool through the transaction-mode
// entry, as the service uses it.
func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	service := os.Getenv("ROSETTA_TEST_DATABASE_URL")
	if service == "" {
		t.Skip("ROSETTA_TEST_DATABASE_URL not set; start dev/compose.yaml to run database tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	open := func(u string, n int32) *pgxpool.Pool {
		p, err := db.Open(ctx, u, n)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	session := open(sessionURL(t, service), 1)
	defer session.Close()
	if err := db.Migrate(ctx, session); err != nil {
		t.Fatal(err)
	}
	pool := open(service, 2)
	t.Cleanup(pool.Close)
	return pool
}

// sessionURL is the session-mode entry on the same host as service.
func sessionURL(t *testing.T, service string) string {
	t.Helper()
	u, err := url.Parse(service)
	if err != nil {
		t.Fatal(err)
	}
	u.Path += "_session"
	return u.String()
}
