// Package posts looks up the post a role holder holds, one person at a time, through BIFROST
// (R-ADM-06; B 21): when a role is granted, at sign-in, and in a daily pass over live roles. It
// keeps no list.
//
// The rule, from E-CAL-05's note. Only found: false means not found; every other answer changes
// nothing. A person found earlier and now not found, or no longer at that post at that office, loses
// the role, at once. A person never found keeps it, flagged, with the post "not known", and the first
// time they are found the post is filled and the flag clears.
package posts

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/bifrost"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Looker asks after one person.
type Looker interface {
	Lookup(ctx context.Context, email string) (bifrost.Answer, error)
}

// Person is who is being asked after.
type Person struct {
	ID      int64
	Subject string
	Email   string
}

// Service holds what a look-up needs.
type Service struct {
	Pool   *pgxpool.Pool
	Looker Looker // nil is a failed look-up, every time
	// Kick starts sending audit entries and alerts once a look-up's changes are kept.
	Kick   func()
	Logger *slog.Logger
	// Sleep waits, for the daily pass's pace; a test passes its own.
	Sleep func(ctx context.Context, d time.Duration) error
	// Changed, when set, is told in the look-up's transaction that a person's flag may have moved.
	Changed func(ctx context.Context, tx pgx.Tx, personID int64, flagged bool) error
}

func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Written is how a post stands on a role: the post, then the office. It is compared whole, so it is
// always written this one way.
func Written(o bifrost.Office) string {
	return strings.TrimSpace(o.Post) + ", " + strings.TrimSpace(o.Office)
}

// held is each post the answer gives, in its order.
func held(a bifrost.Answer) []string {
	out := make([]string, 0, len(a.Offices))
	for _, o := range a.Offices {
		if strings.TrimSpace(o.Post) != "" {
			out = append(out, Written(o))
		}
	}
	return out
}

// Apply changes one person's live roles by BIFROST's answer, in tx. It reports how many roles ended.
func Apply(ctx context.Context, tx pgx.Tx, p Person, a bifrost.Answer, now time.Time) (ended int, flagged bool, err error) {
	grants, err := people.LiveForUpdate(ctx, tx, p.ID)
	if err != nil {
		return 0, false, err
	}
	posts := held(a)
	for _, g := range grants {
		known := g.PostRef != people.PostNotKnown
		switch {
		case known && slices.Contains(posts, g.PostRef):
			err = people.SetPost(ctx, tx, g.ID, g.PostRef, false)
		case known:
			// Found earlier, and now not found, at no post or at another (R-ADM-06).
			if err = people.EndForPost(ctx, tx, g.ID); err == nil {
				details := map[string]any{"change": "ended", "role": g.Role}
				if g.Language != "" {
					details["language"] = string(g.Language)
				}
				err = audit.Record(ctx, tx, audit.Event{Actor: audit.System, Action: audit.ActionRoleChange, ObjectType: "person", ObjectRef: p.Subject,
					Result: audit.ResultDone, Reason: "The person no longer holds the post the role was granted on", Details: details}, now)
				ended++
			}
		case len(posts) > 0:
			// The first time they are found: the first post given, as a role names one.
			err = people.SetPost(ctx, tx, g.ID, posts[0], false)
		default:
			// Never found, or found at no post with nothing earlier to compare: they keep the role.
			err = people.SetPost(ctx, tx, g.ID, people.PostNotKnown, true)
			flagged = true
		}
		if err != nil {
			return ended, flagged, err
		}
	}
	return ended, flagged, nil
}

// lookUp asks once, and turns no answer at all into a failed look-up.
func (s *Service) lookUp(ctx context.Context, email string) (bifrost.Answer, error) {
	if s.Looker == nil {
		return bifrost.Answer{}, errors.New("BIFROST's client is not set")
	}
	if strings.TrimSpace(email) == "" {
		return bifrost.Answer{}, errors.New("the person has no address to ask after")
	}
	return s.Looker.Lookup(ctx, email)
}

// answered keeps one answer's changes in one transaction.
func (s *Service) answered(ctx context.Context, p Person, a bifrost.Answer) (int, error) {
	var ended int
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var flagged bool
		var err error
		if ended, flagged, err = Apply(ctx, tx, p, a, time.Now()); err != nil {
			return err
		}
		if s.Changed != nil {
			return s.Changed(ctx, tx, p.ID, flagged)
		}
		return nil
	})
	if err == nil && s.Kick != nil {
		s.Kick()
	}
	return ended, err
}

// checkTimeout is the longest a person waits on a look-up, when signing in or being granted a role.
const checkTimeout = 5 * time.Second

// Check looks one person up now, and gives the post a session keeps for its audit entries. A failed
// look-up changes nothing, and the session goes on with what the roles held.
func (s *Service) Check(ctx context.Context, p Person) string {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	a, err := s.lookUp(cctx, p.Email)
	if err != nil {
		s.log().Warn("a post look-up failed, and changes nothing", "why", err.Error())
		return s.lastKnown(cctx, p)
	}
	if _, err := s.answered(cctx, p, a); err != nil {
		s.log().Error("what a post look-up found was not kept", "error", err)
		return s.lastKnown(cctx, p)
	}
	if posts := held(a); len(posts) > 0 {
		return posts[0]
	}
	return people.PostNotKnown
}

func (s *Service) lastKnown(ctx context.Context, p Person) string {
	grants, err := people.Live(ctx, s.Pool, p.ID)
	if err != nil {
		return people.PostNotKnown
	}
	for _, g := range grants {
		if g.PostRef != people.PostNotKnown {
			return g.PostRef
		}
	}
	return people.PostNotKnown
}

// PassLimit is the longest one daily pass runs. PARADOX waits for the job's own timeout (contract
// sign/v0.2.0, set in apps/rosetta/schedules.yaml), so the pass stops well before any sensible one.
const PassLimit = 8 * time.Minute

// PassResult is what a pass did.
type PassResult struct {
	Asked, Failed, Ended int
	Left                 int // not reached before the limit
}

func (s *Service) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
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

// Pass looks up everyone holding a live role, one at a time under perMinute, outside any
// transaction, so waiting holds none of the five connections. It honours Retry-After, asking the same
// person once more after the wait, and stops at PassLimit; whoever is left waits for tomorrow or for
// their next sign-in. A failed look-up changes nothing and the pass goes on (R-ADM-06).
func (s *Service) Pass(ctx context.Context, perMinute int) (PassResult, error) {
	ctx, cancel := context.WithTimeout(ctx, PassLimit)
	defer cancel()
	everyone, err := people.HoldingRoles(ctx, s.Pool)
	if err != nil {
		return PassResult{}, err
	}
	var out PassResult
	gap := time.Minute / time.Duration(max(perMinute, 1))
	for i, x := range everyone {
		if i > 0 {
			if err := s.sleep(ctx, gap); err != nil {
				out.Left = len(everyone) - i
				break
			}
		}
		out.Asked++
		a, err := s.lookUp(ctx, x.Email)
		var refused *bifrost.Refused
		if errors.As(err, &refused) && refused.TooMany() {
			wait := refused.Wait
			if wait <= 0 {
				wait = time.Minute
			}
			if err := s.sleep(ctx, wait); err != nil {
				out.Failed++
				out.Left = len(everyone) - i - 1
				break
			}
			a, err = s.lookUp(ctx, x.Email)
		}
		if err != nil {
			out.Failed++
			s.log().Warn("a post look-up failed, and changes nothing", "why", err.Error())
			continue
		}
		ended, err := s.answered(ctx, Person(x), a)
		if err != nil {
			out.Failed++
			s.log().Error("what a post look-up found was not kept", "error", err)
			continue
		}
		out.Ended += ended
	}
	return out, nil
}
