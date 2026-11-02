package translate_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/jackc/pgx/v5/pgxpool"
)

func cacheWorld(t *testing.T) (*pgxpool.Pool, translate.Cache, bumblebee.List) {
	t.Helper()
	pool := dbtest.Open(t)
	ctx := context.Background()
	for _, sql := range []string{`DELETE FROM cache_piece`, `DELETE FROM source_order`, `DELETE FROM gate_decision`, `DELETE FROM source_offer`, `DELETE FROM source`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	offer := []bumblebee.Offer{{Operation: "translate", From: "en", To: "ta"}}
	l := bumblebee.List{Sources: []bumblebee.Source{{Name: "cache-a", Version: "1", State: "on", Offers: offer}, {Name: "cache-b", Version: "1", State: "on", Offers: offer}}}
	if err := sources.Mirror(ctx, pool, l, time.Now()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM cache_piece`) })
	return pool, translate.Cache{Q: pool, Settings: func(context.Context) settings.Values { return settings.Defaults() }}, l
}

func lookup(l bumblebee.List, normal string) translate.Lookup {
	return translate.Lookup{From: "en", To: "ta", Sensitivity: sources.Ordinary, Text: normal, Normal: normal, Allowed: l.Sources}
}

func TestTheCacheFindsByTextPairAndSource(t *testing.T) {
	pool, c, l := cacheWorld(t)
	ctx := context.Background()
	k := translate.Keep{From: "en", To: "ta", Normal: "Call ⟦M1⟧ now.", Translation: "[ta] Call ⟦M1⟧ now.", Source: "cache-b", Version: "1", GlossaryVersion: 0}
	if err := c.Keep(ctx, k); err != nil {
		t.Fatal(err)
	}
	hit, ok, err := c.Answer(ctx, lookup(l, k.Normal))
	if err != nil || !ok || hit.Text != k.Translation || hit.Origin != translate.OriginCache || hit.Source != "cache-b" || hit.Version != "1" {
		t.Fatalf("%+v %v %v", hit, ok, err)
	}
	// Another text, another pair, no source allowed, another version: not found.
	if _, ok, _ := c.Answer(ctx, lookup(l, "Another.")); ok {
		t.Fatal("another text")
	}
	other := lookup(l, k.Normal)
	other.To = "si"
	if _, ok, _ := c.Answer(ctx, other); ok {
		t.Fatal("another pair")
	}
	none := lookup(l, k.Normal)
	none.Allowed = l.Sources[:1]
	if _, ok, _ := c.Answer(ctx, none); ok {
		t.Fatal("a source not allowed now")
	}
	none.Allowed = nil
	if _, ok, _ := c.Answer(ctx, none); ok {
		t.Fatal("no source allowed")
	}
	newer := bumblebee.List{Sources: []bumblebee.Source{{Name: "cache-b", Version: "2"}}}
	if _, ok, _ := c.Answer(ctx, lookup(newer, k.Normal)); ok {
		t.Fatal("a new version starts closed, and its old answers with it")
	}
	// The first in order that has it answers.
	k.Source, k.Translation = "cache-a", "[a] Call ⟦M1⟧ now."
	if err := c.Keep(ctx, k); err != nil {
		t.Fatal(err)
	}
	if hit, ok, _ := c.Answer(ctx, lookup(l, k.Normal)); !ok || hit.Source != "cache-a" {
		t.Fatalf("%+v", hit)
	}
	// A newer answer replaces the older; the row keeps it for the setting's days.
	k.Translation = "[a2] Call ⟦M1⟧ now."
	if err := c.Keep(ctx, k); err != nil {
		t.Fatal(err)
	}
	var n int
	var days float64
	if err := pool.QueryRow(ctx, `SELECT count(*), max(extract(epoch FROM expires_at - created_at) / 86400)::float8 FROM cache_piece`).Scan(&n, &days); err != nil {
		t.Fatal(err)
	}
	if n != 2 || days < 29.9 || days > 30.1 {
		t.Fatal(n, days)
	}
	// Past its days it does not answer.
	if _, err := pool.Exec(ctx, `UPDATE cache_piece SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Answer(ctx, lookup(l, k.Normal)); ok {
		t.Fatal("expired")
	}
	// A source the table has not heard of keeps nothing, and is no failure.
	if err := c.Keep(ctx, translate.Keep{From: "en", To: "ta", Normal: "x", Translation: "y", Source: "nobody", Version: "1"}); err != nil {
		t.Fatal(err)
	}
}

func TestTheVersionReadFromTheTable(t *testing.T) {
	pool := dbtest.Open(t)
	n, err := translate.GlossaryVersion(pool)(context.Background())
	if err != nil || n < 0 {
		t.Fatal(n, err)
	}
}

// The table is what A.1 says it is, and normalised text finds the same row.
func TestTheSameWordFindsTheSameRow(t *testing.T) {
	_, c, l := cacheWorld(t)
	ctx := context.Background()
	normal := translate.Normalise("කො")
	if err := c.Keep(ctx, translate.Keep{From: "en", To: "ta", Normal: normal, Translation: "[ta] x", Source: "cache-a", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Answer(ctx, lookup(l, translate.Normalise("කො"))); !ok {
		t.Fatal("not found")
	}
	if strings.Contains(normal, "ෙ") {
		t.Fatal("not normalised")
	}
}
