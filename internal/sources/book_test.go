package sources

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func age(d time.Duration) func(context.Context) time.Duration {
	return func(context.Context) time.Duration { return d }
}

func TestBookIsFreshOnlyInsideItsLimit(t *testing.T) {
	c := &clock{time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := &Book{Now: c.now, MaxAge: age(60 * time.Second), Read: func(context.Context) (bumblebee.List, error) {
		return bumblebee.List{MaxTextChars: 700, Sources: []bumblebee.Source{{Name: "a", State: "on"}}}, nil
	}}
	ctx := context.Background()
	if s := b.Snapshot(ctx); s.Fresh || !s.ReadAt.IsZero() {
		t.Fatal("a list never read is not fresh")
	}
	if err := b.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if s := b.Snapshot(ctx); !s.Fresh || len(s.List.Sources) != 1 {
		t.Fatalf("%+v", s)
	}
	c.t = c.t.Add(59 * time.Second)
	if !b.Snapshot(ctx).Fresh {
		t.Fatal("59 seconds old")
	}
	c.t = c.t.Add(time.Second)
	if b.Snapshot(ctx).Fresh {
		t.Fatal("never older than the limit (R-PRV-01)")
	}
}

// The size is the list's; the last known if the list goes away; a default only if it
// was never read. A read that fails keeps the last list, which goes stale.
func TestMaxTextFollowsTheList(t *testing.T) {
	var read func() (bumblebee.List, error)
	b := &Book{MaxAge: age(time.Minute), Read: func(context.Context) (bumblebee.List, error) { return read() }}
	if b.MaxText() != DefaultMaxText {
		t.Fatal("no list: the default")
	}
	ctx := context.Background()
	read = func() (bumblebee.List, error) { return bumblebee.List{MaxTextChars: 321}, nil }
	b.Refresh(ctx)
	if b.MaxText() != 321 {
		t.Fatal(b.MaxText())
	}
	read = func() (bumblebee.List, error) { return bumblebee.List{}, errors.New("down") }
	if b.Refresh(ctx) == nil {
		t.Fatal("no failure")
	}
	if b.MaxText() != 321 {
		t.Fatal(b.MaxText())
	}
	read = func() (bumblebee.List, error) { return bumblebee.List{}, nil }
	b.Refresh(ctx)
	if b.MaxText() != DefaultMaxText {
		t.Fatal("a list giving no size: the default")
	}
}

func TestBookTellsTheCopyAndSurvivesItsFailure(t *testing.T) {
	var logs bytes.Buffer
	calls := 0
	b := &Book{MaxAge: age(time.Minute), Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Read: func(context.Context) (bumblebee.List, error) { return bumblebee.List{MaxTextChars: 9}, nil },
		Kept: func(context.Context, bumblebee.List, time.Time) error { calls++; return errors.New("no database") }}
	if err := b.Refresh(context.Background()); err != nil || calls != 1 || b.MaxText() != 9 {
		t.Fatal(err, calls)
	}
	if !strings.Contains(logs.String(), "not kept") {
		t.Fatal(logs.String())
	}
}

func TestBookLogsAFailureOnceAndItsEnd(t *testing.T) {
	var logs bytes.Buffer
	var err error
	b := &Book{MaxAge: age(time.Minute), Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Read: func(context.Context) (bumblebee.List, error) { return bumblebee.List{}, err }}
	count := func(line string) int { return strings.Count(logs.String(), line) }

	err = errors.New("answered 401")
	for range 3 {
		b.Refresh(context.Background())
	}
	if n := count("the source list was not read"); n != 1 {
		t.Fatalf("one failure, read three times, logged %d times", n)
	}

	err = errors.New("answered 503")
	b.Refresh(context.Background())
	if n := count("the source list was not read"); n != 2 {
		t.Fatalf("a changed failure was not logged: %d lines", n)
	}

	err = nil
	b.Refresh(context.Background())
	b.Refresh(context.Background())
	if n := count("the source list is read again"); n != 1 {
		t.Fatalf("the end of a failure logged %d times", n)
	}

	err = errors.New("answered 401")
	b.Refresh(context.Background())
	if n := count("the source list was not read"); n != 3 {
		t.Fatalf("a new failure after a good read was not logged: %d lines", n)
	}
}

func TestRunReadsUntilTold(t *testing.T) {
	reads := make(chan struct{}, 100)
	b := &Book{MaxAge: age(time.Millisecond), Read: func(context.Context) (bumblebee.List, error) {
		reads <- struct{}{}
		return bumblebee.List{}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx); close(done) }()
	<-reads
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestStoredState(t *testing.T) {
	for in, want := range map[string]string{"on": "on", "resting-block": "resting-block", "resting-change": "resting-change", "off": "off", "later": "off", "": "off"} {
		if storedState(in) != want {
			t.Errorf("%q", in)
		}
	}
}

// The timer's pass ticks whether the list could be read or not, so what hangs off it does not
// stop while BUMBLEBEE is down.
func TestRunTicksEachPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan struct{}, 10)
	book := &Book{
		Read:   func(context.Context) (bumblebee.List, error) { return bumblebee.List{}, errors.New("down") },
		MaxAge: func(context.Context) time.Duration { return time.Millisecond },
		Tick:   func(context.Context) { ticks <- struct{}{} },
	}
	done := make(chan struct{})
	go func() { book.Run(ctx); close(done) }()
	<-ticks
	cancel()
	<-done
}
