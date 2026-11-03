// Package sources holds what ROSETTA knows of BUMBLEBEE's browser sources: the list
// and its size limit (R-PRV-01, R-PRV-04).
//
// Book holds the list, read again on an in-process timer. That is the one timer
// AGENTS C6 allows: ROSETTA never sleeps, and this is reading, not scheduled work.
package sources

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
)

// DefaultMaxText is the size to split to while the source list has never been read.
// It is a fallback, not a copy of BUMBLEBEE's limit: whenever the list can be read,
// its limits.max_text_chars is the only figure used, and a too-long answer makes
// ROSETTA read the list again and split again (R-PRV-04).
const DefaultMaxText = 5000

// readTimeout is the longest one read of the list waits.
const readTimeout = 10 * time.Second

// Book is the source list as last read. Safe for concurrent use.
type Book struct {
	Read func(ctx context.Context) (bumblebee.List, error)
	// MaxAge is how old the list may be (R-PRV-01): a setting, read on each use.
	MaxAge func(ctx context.Context) time.Duration
	// Kept, when set, is told of each list read, for the database's copy. A failure
	// there is logged and changes nothing here.
	Kept func(ctx context.Context, l bumblebee.List, at time.Time) error
	// Tick, when set, is called on each pass of the timer, read or not: in-process tidying
	// that needs no timer of its own (AGENTS C6).
	Tick   func(ctx context.Context)
	Logger *slog.Logger
	Now    func() time.Time // time.Now when nil

	mu     sync.Mutex
	list   bumblebee.List
	readAt time.Time
}

// Snapshot is the list at one moment.
type Snapshot struct {
	List   bumblebee.List
	ReadAt time.Time // zero if it was never read
	// Fresh says the list is younger than its limit. An older list is never used to
	// send anything, so while BUMBLEBEE is down requests fail at once (R-NFR-09).
	Fresh bool
}

func (b *Book) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Book) log() *slog.Logger {
	if b.Logger != nil {
		return b.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Refresh reads the list now. A failed read keeps the last list, which goes stale.
func (b *Book) Refresh(ctx context.Context) error {
	rctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	l, err := b.Read(rctx)
	if err != nil {
		b.log().Warn("the source list was not read", "error", err)
		return err
	}
	at := b.now()
	b.mu.Lock()
	b.list, b.readAt = l, at
	b.mu.Unlock()
	if b.Kept != nil {
		if err := b.Kept(rctx, l, at); err != nil {
			b.log().Error("the source list was not kept in the database", "error", err)
		}
	}
	return nil
}

// Snapshot is the list as it stands.
func (b *Book) Snapshot(ctx context.Context) Snapshot {
	b.mu.Lock()
	s := Snapshot{List: b.list, ReadAt: b.readAt}
	b.mu.Unlock()
	s.Fresh = !s.ReadAt.IsZero() && b.now().Sub(s.ReadAt) < b.MaxAge(ctx)
	return s
}

// MaxText is the size to split to, in code points: the list's, the last one known
// if it can no longer be read, and DefaultMaxText if it never could be.
func (b *Book) MaxText() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.list.MaxTextChars > 0 {
		return b.list.MaxTextChars
	}
	return DefaultMaxText
}

// Run reads the list until ctx ends, twice within each MaxAge, so one failed read
// does not make it stale.
func (b *Book) Run(ctx context.Context) {
	for {
		b.Refresh(ctx)
		if b.Tick != nil {
			b.Tick(ctx)
		}
		wait := max(b.MaxAge(ctx)/2, time.Second)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
