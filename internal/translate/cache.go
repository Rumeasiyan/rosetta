package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The cache of ordinary pieces (R-TRN-08; Annex A: cache_piece, UNLOGGED), so the same
// piece is not sent to a source twice and the same request gets the same answer
// (R-TRN-05). It holds pieces as they went to the source: stand-ins, not numbers, so no
// identity card or phone number is ever in it. Expired rows are removed by E-REC-03.

// CacheQuerier is what the cache runs through.
type CacheQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Hash is what a piece is found by: its normalised text, with no way back to it.
func Hash(normal string) string {
	sum := sha256.Sum256([]byte(normal))
	return hex.EncodeToString(sum[:])
}

// Cache is the cache in the database.
type Cache struct {
	Q        CacheQuerier
	Settings func(ctx context.Context) settings.Values
}

// Answer finds this piece from the first source in order that has it. Only a source
// the piece could go to now counts: a machine's answer is under the gate, as only people's
// checked work is not (R-GAT-02). So a source turned off, closed, or on a new version
// since it answered does not answer from here. A resting one does: nothing goes to it.
func (c Cache) Answer(ctx context.Context, l Lookup) (Hit, bool, error) {
	if len(l.Allowed) == 0 {
		return Hit{}, false, nil
	}
	names := make([]string, 0, len(l.Allowed))
	for _, s := range l.Allowed {
		names = append(names, s.Name)
	}
	rows, err := c.Q.Query(ctx, `SELECT s.name, c.source_version, c.translation FROM cache_piece c JOIN source s ON s.id = c.source_id
		WHERE c.source_hash = $1 AND c.from_language = $2 AND c.to_language = $3 AND s.name = ANY($4) AND c.expires_at > now()`,
		Hash(l.Normal), l.From, l.To, names)
	if err != nil {
		return Hit{}, false, err
	}
	type row struct{ name, version, text string }
	found, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.name, &x.version, &x.text)
		return x, err
	})
	if err != nil {
		return Hit{}, false, err
	}
	for _, s := range l.Allowed {
		for _, x := range found {
			if x.name == s.Name && x.version == s.Version {
				return Hit{Text: x.text, Origin: OriginCache, Source: x.name, Version: x.version}, true, nil
			}
		}
	}
	return Hit{}, false, nil
}

// Keep is one ordinary piece a source translated.
type Keep struct {
	From, To        string
	Normal          string // the piece as sent, normalised
	Translation     string // as the source answered it, stand-ins and all
	Source, Version string
	GlossaryVersion int
}

// Keep holds the piece for the setting's days. A newer answer from the same source
// replaces the older one. A source the table has not heard of keeps nothing.
func (c Cache) Keep(ctx context.Context, k Keep) error {
	days := c.Settings(ctx).Of(settings.CacheDays)
	_, err := c.Q.Exec(ctx, `INSERT INTO cache_piece (source_hash, from_language, to_language, source_id, glossary_version, source_text, translation, source_version, created_at, expires_at)
		SELECT $1, $2, $3, s.id, $4, $5, $6, $7, now(), now() + $8::interval FROM source s WHERE s.name = $9
		ON CONFLICT (source_hash, from_language, to_language, source_id) DO UPDATE SET glossary_version = EXCLUDED.glossary_version,
			source_text = EXCLUDED.source_text, translation = EXCLUDED.translation, source_version = EXCLUDED.source_version,
			created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at`,
		Hash(k.Normal), k.From, k.To, k.GlossaryVersion, k.Normal, k.Translation, k.Version,
		(time.Duration(days) * 24 * time.Hour).String(), k.Source)
	return err
}
