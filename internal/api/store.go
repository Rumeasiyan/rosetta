package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolKeys finds keys in the database.
type PoolKeys struct{ Pool *pgxpool.Pool }

// Find is the key with this hash.
func (k PoolKeys) Find(ctx context.Context, hash string) (callers.Holder, error) {
	return callers.FindKey(ctx, k.Pool, hash)
}

// Used notes the key and its system were used.
func (k PoolKeys) Used(ctx context.Context, h callers.Holder) error {
	return callers.Used(ctx, k.Pool, h.KeyID, h.Caller.ID)
}

// unidentified stands as the actor when a key told nobody's name.
const unidentified = "unidentified caller"

// DenialEvent is the audit entry for a refused key. The route is fixed, never the
// address as sent.
func DenialEvent(d Denial) audit.Event {
	actor := audit.Actor{Subject: unidentified, Post: audit.PostNotKnown}
	if d.System != "" {
		actor = audit.Actor{Subject: d.System, Post: audit.PostSystem}
	}
	return audit.Event{Actor: actor, Action: audit.ActionDenial, ObjectType: "api", ObjectRef: Prefix,
		Result: audit.ResultDenied, Reason: d.Why, Visitor: d.Visitor}
}

// PoolDenials keeps denials in the outbox and starts sending them.
func PoolDenials(pool *pgxpool.Pool, kick func()) func(context.Context, Denial) error {
	return func(ctx context.Context, d Denial) error {
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return audit.Record(ctx, tx, DenialEvent(d), time.Now())
		})
		if err == nil && kick != nil {
			kick()
		}
		return err
	}
}

// visitor is who sent the request. audit.Visitor checks what it reads.
func visitor(r *http.Request) string {
	if v := audit.Visitor(r); net.ParseIP(strings.TrimSpace(v)) != nil {
		return v
	}
	return ""
}
