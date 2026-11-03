package sources

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/terms"
)

// A failed source call goes to the next source in order (R-PRV-03). What each reason
// means is docs/CALLERS.md in BUMBLEBEE's repository; ROSETTA never tries to get round
// a block, a bot check or a sign-in wall, and respects every "try again after" a
// source gives (R-PRV-05).

const (
	// callWait is the wait on one call, as docs/CALLERS.md gives it: BUMBLEBEE answers
	// every request within less. The whole request's limit (R-ERR-03) also holds it.
	callWait = 60 * time.Second
	// leastTime is the least left for a call to start. Less than this, and a late
	// fallback could only run the request past its limit.
	leastTime = 2 * time.Second
)

// ErrTooLate says too little of the request's time is left to start a call.
var ErrTooLate = errors.New("sources: too little time left to call a source")

// Exhausted says every allowed source failed or is resting (R-PRV-06). RetryAt is the
// earliest time a source is expected back, or zero when nothing said.
type Exhausted struct {
	RetryAt time.Time
}

func (e *Exhausted) Error() string { return "sources: every allowed source failed" }

// TooLong says the piece is over the size the list gives now, after reading it again
// (R-PRV-04): split it to Limit, and send the parts.
type TooLong struct {
	Limit int
}

func (e *TooLong) Error() string { return "sources: the piece is over the size in the source list" }

// Provider is who a source call goes to: BUMBLEBEE, and later any other behind it.
type Provider interface {
	Translate(ctx context.Context, q bumblebee.Request) (bumblebee.Answer, error)
}

// Piece is one masked piece of text to translate. Text never goes in an error or a log.
type Piece struct {
	Ref         string // the request reference, passed as request_id (R-REC-01)
	From, To    string
	Sensitivity string
	Text        string
}

// Result is a piece translated.
type Result struct {
	Text     string
	Source   string
	Version  string
	Fallback bool // a source earlier in the order was resting or failed
}

// Fallback sends a piece to the first source that answers.
type Fallback struct {
	Book     *Book
	Provider Provider
	// Grammars is who a grammar call goes to; nil answers none.
	Grammars GrammarProvider
	Policy   func(ctx context.Context) Policy
	Logger   *slog.Logger
	Now      func() time.Time                                 // time.Now when nil
	Sleep    func(ctx context.Context, d time.Duration) error // waits for d or ctx, a real wait when nil

	mu    sync.Mutex
	holds map[string]time.Time // source: not before this
}

func (f *Fallback) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Fallback) log() *slog.Logger {
	if f.Logger != nil {
		return f.Logger
	}
	return slog.New(slog.DiscardHandler)
}

func (f *Fallback) sleep(ctx context.Context, d time.Duration) error {
	if f.Sleep != nil {
		return f.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// hold keeps a source from being called before at, by any request.
func (f *Fallback) hold(name string, at *time.Time) {
	if at == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.holds == nil {
		f.holds = map[string]time.Time{}
	}
	if at.After(f.holds[name]) {
		f.holds[name] = *at
	}
}

// heldUntil is when a source may be called again, if that is still ahead.
func (f *Fallback) heldUntil(name string, now time.Time) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	at, ok := f.holds[name]
	if ok && !now.Before(at) {
		delete(f.holds, name)
		return time.Time{}, false
	}
	return at, ok
}

// Allowed is the sources a piece of this kind may go to, whatever they are doing now,
// and whether the list has ever been read. The list as last known is enough for this:
// the gate and the order do not depend on who is up.
func (f *Fallback) Allowed(ctx context.Context, from, to, sensitivity string) ([]bumblebee.Source, bool) {
	snap := f.Book.Snapshot(ctx)
	return f.Policy(ctx).Candidates(snap.List, Translate(from, to), sensitivity), !snap.ReadAt.IsZero()
}

// earlier keeps the earliest of two times, where zero means none.
func earlier(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}

// timeLeft is what remains of ctx's time, and whether it has a limit.
func timeLeft(ctx context.Context, now time.Time) (time.Duration, bool) {
	d, ok := ctx.Deadline()
	if !ok {
		return 0, false
	}
	return d.Sub(now), true
}

// attempted is the source that answered.
type attempted struct {
	source, version string
	fallback        bool // a source earlier in the order was resting or failed
}

// sourceCall is one call to one source, giving the version it answered with. A translation
// and a grammar check differ only here.
type sourceCall func(ctx context.Context, s bumblebee.Source) (string, error)

// attempt tries the sources for this work in order, until one answers. Its error is an
// *Exhausted, a *TooLong, ErrTooLate, or ctx's own. While the list is older than its limit
// nothing is sent at all, so every such request fails at once (R-NFR-09). text is only
// measured, on "too long".
func (f *Fallback) attempt(ctx context.Context, d Direction, sensitivity, ref, text string, call sourceCall) (attempted, error) {
	snap := f.Book.Snapshot(ctx)
	if !snap.Fresh {
		return attempted{}, &Exhausted{}
	}
	var back time.Time
	passed := false // a source earlier in the order did not answer
	for _, s := range f.Policy(ctx).Candidates(snap.List, d, sensitivity) {
		now := f.now()
		if !s.Usable(now) {
			passed = true
			if s.RestUntil != nil {
				back = earlier(back, *s.RestUntil)
			}
			continue
		}
		if at, held := f.heldUntil(s.Name, now); held {
			passed = true
			back = earlier(back, at)
			continue
		}
		if left, limited := timeLeft(ctx, now); limited && left < leastTime {
			return attempted{}, ErrTooLate
		}
		version, err := f.call(ctx, s, call)
		var failed *bumblebee.Failure
		if errors.As(err, &failed) && failed.Reason == bumblebee.ReasonBusy {
			version, err = f.onceMore(ctx, s, call, failed)
		}
		if err == nil {
			if version == "" {
				version = s.Version
			}
			return attempted{source: s.Name, version: version, fallback: passed}, nil
		}
		if !errors.As(err, &failed) {
			return attempted{}, err // the request's own time ran out, or it was given up
		}
		passed = true
		f.log().Warn("a source call failed", "request_ref", ref, "source", s.Name, "reason", failed.Reason)
		switch failed.Reason {
		case bumblebee.ReasonBusy, bumblebee.ReasonBlocked, bumblebee.ReasonChanged:
			// Not before retry_at, by this request or any other (R-PRV-05).
			f.hold(s.Name, failed.RetryAt)
			if failed.RetryAt != nil {
				back = earlier(back, *failed.RetryAt)
			}
		case bumblebee.ReasonTooLong:
			// It counts as a failure only if the list, read again, does not call for
			// splitting it further (R-PRV-04).
			f.Book.Refresh(ctx)
			if limit := f.Book.MaxText(); utf8.RuneCountInString(text) > limit {
				return attempted{}, &TooLong{Limit: limit}
			}
		}
	}
	return attempted{}, &Exhausted{RetryAt: back}
}

// Translate sends the piece to the first source in order that can take it. Its error is as
// attempt's.
func (f *Fallback) Translate(ctx context.Context, p Piece) (Result, error) {
	var answer string
	a, err := f.attempt(ctx, Translate(p.From, p.To), p.Sensitivity, p.Ref, p.Text, func(ctx context.Context, s bumblebee.Source) (string, error) {
		ans, err := f.Provider.Translate(ctx, bumblebee.Request{RequestID: p.Ref, Source: s.Name, From: p.From, To: p.To, Text: p.Text})
		if err != nil {
			return "", err
		}
		if !acceptable(ans.Text, p.Text) {
			return "", &bumblebee.Failure{Reason: bumblebee.ReasonUnavailable}
		}
		answer = ans.Text
		return ans.Version, nil
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Text: answer, Source: a.source, Version: a.version, Fallback: a.fallback}, nil
}

// MaxText is the size to split to.
func (f *Fallback) MaxText() int { return f.Book.MaxText() }

// standIn finds what is a stand-in's shape, which holds no words of the caller's.
var standIn = regexp.MustCompile(`⟦[^⟦⟧]*⟧`)

// acceptable refuses an answer that is empty, or that is only the text sent back (R-TRN-09):
// a site that fails often echoes what it was given, and ROSETTA never passes that off as a
// translation. A piece with no letters in it, such as a number on its own line, comes back
// the same in any language.
func acceptable(answer, sent string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	if terms.Normalise(answer) != terms.Normalise(strings.TrimSpace(sent)) {
		return true
	}
	return !strings.ContainsFunc(standIn.ReplaceAllString(sent, ""), unicode.IsLetter)
}

// call is one source call, inside the wait CALLERS.md gives. The wait running out with the
// request's own time left is that source not answering.
func (f *Fallback) call(ctx context.Context, s bumblebee.Source, call sourceCall) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, callWait)
	defer cancel()
	version, err := call(cctx, s)
	if err != nil && ctx.Err() == nil && cctx.Err() != nil {
		return version, &bumblebee.Failure{Reason: bumblebee.ReasonTimeout}
	}
	return version, err
}

// onceMore waits as a busy source asked and tries it once more, when the wait is shorter than
// the time left (R-PRV-03). Otherwise, or if it fails again, the failure stands and the caller
// moves on.
func (f *Fallback) onceMore(ctx context.Context, s bumblebee.Source, call sourceCall, busy *bumblebee.Failure) (string, error) {
	if busy.RetryAt == nil {
		return "", busy
	}
	now := f.now()
	wait := max(busy.RetryAt.Sub(now), 0)
	if left, limited := timeLeft(ctx, now); limited && wait+leastTime > left {
		return "", busy
	}
	f.hold(s.Name, busy.RetryAt) // others do not call it while this one waits
	if err := f.sleep(ctx, wait); err != nil {
		return "", err
	}
	return f.call(ctx, s, call)
}
