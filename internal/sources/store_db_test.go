package sources_test

import (
	"context"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/sources"
)

func TestMirrorKeepsTheListAndTheSwitch(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, sql := range []string{`DELETE FROM cache_piece`, `DELETE FROM source_order`, `DELETE FROM gate_decision`, `DELETE FROM source_offer`, `DELETE FROM source`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	rest := now.Add(time.Hour)
	l := bumblebee.List{MaxTextChars: 9, Sources: []bumblebee.Source{
		{Name: "mirror-a", Version: "1", State: "resting-block", RestUntil: &rest, Offers: []bumblebee.Offer{{Operation: "translate", From: "en", To: "ta"}}},
		{Name: "mirror-b", Version: "2", State: "on", Offers: []bumblebee.Offer{{Operation: "grammar", From: "en"}}, Scores: map[string]float64{"en": 72}},
	}}
	if err := sources.Mirror(ctx, pool, l, now); err != nil {
		t.Fatal(err)
	}
	var state, kind string
	var until *time.Time
	if err := pool.QueryRow(ctx, `SELECT state, rest_until, processes_text FROM source WHERE name = 'mirror-a'`).Scan(&state, &until, &kind); err != nil {
		t.Fatal(err)
	}
	if state != "resting-block" || until == nil || kind != "outside public website" {
		t.Fatal(state, until, kind)
	}
	var score float64
	if err := pool.QueryRow(ctx, `SELECT benchmark_score FROM source_offer o JOIN source s ON s.id = o.source_id WHERE s.name = 'mirror-b'`).Scan(&score); err != nil || score != 72 {
		t.Fatal(score, err)
	}
	// The administrator's switch stays; a source that left the list goes off.
	if _, err := pool.Exec(ctx, `UPDATE source SET on_for_rosetta = false WHERE name = 'mirror-b'`); err != nil {
		t.Fatal(err)
	}
	l.Sources = l.Sources[1:]
	if err := sources.Mirror(ctx, pool, l, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var on bool
	if err := pool.QueryRow(ctx, `SELECT on_for_rosetta FROM source WHERE name = 'mirror-b'`).Scan(&on); err != nil || on {
		t.Fatal(on, err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM source WHERE name = 'mirror-a'`).Scan(&state); err != nil || state != "off" {
		t.Fatal(state, err)
	}
}
