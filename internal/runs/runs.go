// Package runs holds background runs (R-TRN-10 to R-TRN-13): who started each, its state, and in
// the end its answer, in this process's memory and nowhere else. A run's text, masks and answer are
// inside its Task and Result, which this package never reads, logs or writes down. They are
// forgotten at hand-over, when the system gives up, when the uncollected limit passes, and at any
// stop; a Go program cannot wipe a string, so forgetting is dropping every reference.
//
// There is no job loop (AGENTS C6): each run has one timer of its own at a time, first for its limit
// and then for how long its answer waits. A few runs are worked at a time, as pieces go through
// BUMBLEBEE's queue.
package runs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/oidc"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
)

// The states a system can be told. A run that has ended is told by its answer.
const (
	Waiting = "waiting"
	Working = "working"
)

const ended = "ended"

// How soon a system is told to ask again: this build's figures, not the PRD's. A run in work is
// asked after when its pieces at their rate so far should be done, within these bounds.
const (
	askWaiting = 15 * time.Second
	askLeast   = 5 * time.Second
	askMost    = 60 * time.Second
)

// tryAgainFull is what a system past a limit is told: there is no telling when a run will end, so a
// short wait, as with requests at once.
const tryAgainFull = time.Minute

// readTimeout is the longest the limits are waited for.
const readTimeout = 5 * time.Second

// Limits are read when they are needed, so a change to a setting holds for what comes next.
type Limits struct {
	PerSystem, InAll, AtATime int
	Longest, Uncollected      time.Duration
}

// LimitsOf is the limits as the settings hold them (R-TRN-12, R-TRN-13).
func LimitsOf(v settings.Values) Limits {
	return Limits{
		PerSystem:   v.Of(settings.RunsPerSystem),
		InAll:       v.Of(settings.RunsInAll),
		AtATime:     v.Of(settings.RunsWorkedAtOnce),
		Longest:     time.Duration(v.Of(settings.RunLimitMinutes)) * time.Minute,
		Uncollected: time.Duration(v.Of(settings.RunUncollectedMinutes)) * time.Minute,
	}
}

// Result is how a run ended: its answer as it will be sent, or its failure. GivenUp says its system
// gave it up, and then there is neither.
type Result struct {
	Ref     string // the request reference, for a failure's answer
	Body    []byte
	Failure *reasons.Failure
	GivenUp bool
}

// Task is the work of one run.
type Task interface {
	// Work does it, until ctx ends. A failure holds no text.
	Work(ctx context.Context) Result
	// Pieces is how many are done and how many there are, safe to read during Work.
	Pieces() (done, total int)
	// Ended is told once, how the run ended, for its record (R-REC-01). A run lost to a stop is
	// never told.
	Ended(r Result)
}

// run is one background run.
type run struct {
	ref        string
	requestRef string
	system     int64
	task       Task // dropped when it ends, with the text and masks it holds
	state      string
	deadline   time.Time // counted from when it was accepted, waiting included
	// uncollected is how long its answer waits, as the setting stood when it started.
	uncollected time.Duration
	began       time.Time // when work started
	first       int       // pieces already done then
	cancel      context.CancelFunc
	gaveUp      bool
	lost        bool
	result      Result
	timer       *time.Timer
	timerGen    int
}

// Book is every run held. The zero value with Limits set is ready; Stop ends it.
type Book struct {
	Limits func(ctx context.Context) Limits
	Logger *slog.Logger

	mu      sync.Mutex
	runs    map[string]*run
	queue   []*run        // waiting, oldest first
	working int           // workers going, a given-up run's too until its work stops
	held    map[int64]int // each system's runs, reserved or held
	inWork  map[int64]int // each system's runs in work
	total   int           // all runs, reserved or held
	stopped bool
	going   sync.WaitGroup
}

func (b *Book) log() *slog.Logger {
	if b.Logger != nil {
		return b.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// limits reads the limits, never while holding the lock: it may read the database.
func (b *Book) limits() Limits {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	return b.Limits(ctx)
}

// Slot is room kept for one run while it is checked, by one goroutine. It must be used by Begin or
// given back by Release.
type Slot struct {
	b      *Book
	system int64
	used   bool
}

// Release gives the room back, if Begin did not take it.
func (s *Slot) Release() {
	if s.used {
		return
	}
	s.used = true
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.free(s.system)
}

// free gives one system's room back. Called under mu.
func (b *Book) free(system int64) {
	b.total--
	b.held[system]--
	if b.held[system] <= 0 {
		delete(b.held, system)
	}
}

// ErrStopped says the service is stopping and takes no new run.
var ErrStopped = errors.New("runs: the service is stopping")

// ErrFull says a limit stands in the way; the wait says when to try again.
var ErrFull = errors.New("runs: over a limit")

// Reserve keeps room for a run of this system, or says how long to wait (R-TRN-12). A run counts
// until it is forgotten, so one ended and not yet collected still counts: its answer is in memory.
func (b *Book) Reserve(system int64) (*Slot, time.Duration, error) {
	l := b.limits()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return nil, 0, ErrStopped
	}
	if b.held[system] >= l.PerSystem || b.total >= l.InAll {
		return nil, tryAgainFull, ErrFull
	}
	if b.held == nil {
		b.held = map[int64]int{}
	}
	b.total++
	b.held[system]++
	return &Slot{b: b, system: system}, 0, nil
}

// Started is a run just taken on: its random reference, its state and when to ask.
type Started struct {
	Ref, State string
	AskAgainAt time.Time
}

// Begin takes the reserved room and starts a run. requestRef is the reference its record, source
// calls and failure carry.
func (b *Book) Begin(slot *Slot, requestRef string, task Task) (Started, error) {
	l := b.limits()
	b.mu.Lock()
	defer b.mu.Unlock()
	slot.used = true
	if b.stopped {
		b.free(slot.system)
		return Started{}, ErrStopped
	}
	now := time.Now()
	r := &run{ref: "run-" + oidc.Random(), requestRef: requestRef, system: slot.system, task: task, state: Waiting, deadline: now.Add(l.Longest), uncollected: l.Uncollected}
	if b.runs == nil {
		b.runs = map[string]*run{}
		b.inWork = map[int64]int{}
	}
	b.runs[r.ref] = r
	b.queue = append(b.queue, r)
	b.setTimer(r, l.Longest, b.limitReached)
	b.dispatch(l.AtATime)
	return Started{Ref: r.ref, State: r.state, AskAgainAt: b.askAgain(r, now)}, nil
}

// setTimer gives the run its one timer. A timer that has fired already but not yet taken the lock
// finds its number out of date, and does nothing. Called under mu.
func (b *Book) setTimer(r *run, d time.Duration, then func(*run)) {
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timerGen++
	gen := r.timerGen
	r.timer = time.AfterFunc(d, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if r.timerGen == gen && b.runs[r.ref] == r {
			then(r)
		}
	})
}

// limitReached ends a run still waiting at its limit. One in work ends by its context's deadline,
// the same moment, on its own goroutine. Called under mu.
func (b *Book) limitReached(r *run) {
	if r.state != Waiting {
		return
	}
	b.unqueue(r)
	res := Result{Failure: reasons.New(reasons.TimedOut)}
	task := r.task
	b.end(r, res)
	b.tell(task, res)
}

// tell tells a task how it ended, away from the lock, and a stop waits for it. Called under mu.
func (b *Book) tell(task Task, res Result) {
	b.going.Add(1)
	go func() {
		defer b.going.Done()
		task.Ended(res)
	}()
}

// end keeps a run's result for its system to collect, drops its text and masks, and starts the
// uncollected limit (R-TRN-13). Called under mu.
func (b *Book) end(r *run, res Result) {
	res.Ref = r.requestRef
	r.state, r.result, r.task = ended, res, nil
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	b.setTimer(r, r.uncollected, b.forgetUncollected)
}

// forgetUncollected is the uncollected limit passing. Called under mu.
func (b *Book) forgetUncollected(r *run) {
	b.log().Info("a run's answer was not collected and is forgotten", "request_ref", r.requestRef)
	b.forget(r)
}

// forget drops a run altogether. Called under mu.
func (b *Book) forget(r *run) {
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timerGen++
	r.result, r.task = Result{}, nil
	delete(b.runs, r.ref)
	b.free(r.system)
}

func (b *Book) unqueue(r *run) {
	for i, x := range b.queue {
		if x == r {
			b.queue = append(b.queue[:i], b.queue[i+1:]...)
			return
		}
	}
}

// next is the next run to work: the system with the fewest in work first, so one system's five do
// not hold up everyone else, then the oldest. Called under mu.
func (b *Book) next() *run {
	var pick *run
	for _, r := range b.queue {
		if pick == nil || b.inWork[r.system] < b.inWork[pick.system] {
			pick = r
		}
	}
	return pick
}

// dispatch starts what there is room to work. Called under mu.
func (b *Book) dispatch(atATime int) {
	for b.working < atATime && !b.stopped {
		r := b.next()
		if r == nil {
			return
		}
		b.unqueue(r)
		now := time.Now()
		ctx, cancel := context.WithDeadline(context.Background(), r.deadline)
		r.state, r.cancel, r.began = Working, cancel, now
		r.first, _ = r.task.Pieces()
		b.working++
		b.inWork[r.system]++
		b.going.Add(1)
		go b.work(ctx, r, r.task)
	}
}

// work runs one run on its own goroutine, and keeps how it ended.
func (b *Book) work(ctx context.Context, r *run, task Task) {
	defer b.going.Done()
	res := b.safely(ctx, r.requestRef, task)
	// A deadline passing can show itself in other ways, as a look-up failing: it is still the limit.
	if res.Failure != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.Failure.Reason = reasons.TimedOut
	}
	l := b.limits()
	b.mu.Lock()
	b.working--
	b.inWork[r.system]--
	if b.inWork[r.system] <= 0 {
		delete(b.inWork, r.system)
	}
	lost, gaveUp := r.lost, r.gaveUp
	if lost || gaveUp {
		res = Result{GivenUp: true}
	} else {
		b.end(r, res)
	}
	b.dispatch(l.AtATime)
	b.mu.Unlock()
	if !lost {
		task.Ended(res)
	}
}

// safely keeps a fault in one run from stopping every other. What a panic says is never logged, as
// it could quote what it was working on.
func (b *Book) safely(ctx context.Context, requestRef string, task Task) (res Result) {
	defer func() {
		if recover() != nil {
			b.log().Error("a run stopped on a fault", "request_ref", requestRef)
			res = Result{Failure: reasons.New(reasons.NoProvider)}
		}
	}()
	return task.Work(ctx)
}

// View is what a system is told when it asks.
type View struct {
	State       string
	Done, Total int
	AskAgainAt  time.Time
	// Result is set once the run has ended. It is handed over this once, and the run is forgotten.
	Result *Result
}

// Ask answers the system that started a run. A reference it does not know, one forgotten and
// another system's all say no, the same way (R-TRN-11).
func (b *Book) Ask(system int64, ref string) (View, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.runs[ref]
	if !ok || r.system != system {
		return View{}, false
	}
	if r.state == ended {
		res := r.result
		b.forget(r)
		return View{State: ended, Result: &res}, true
	}
	now := time.Now()
	done, total := r.task.Pieces()
	return View{State: r.state, Done: done, Total: total, AskAgainAt: b.askAgain(r, now)}, true
}

// askAgain is when to ask next, in whole seconds. Called under mu.
func (b *Book) askAgain(r *run, now time.Time) time.Time {
	wait := askWaiting
	if r.state == Working {
		wait = askLeast
		done, total := r.task.Pieces()
		if since := done - r.first; since > 0 {
			wait = min(max(now.Sub(r.began)/time.Duration(since)*time.Duration(total-done), askLeast), askMost)
		}
	}
	at := now.Add(wait)
	// It ends by then in any case.
	if end := r.deadline.Add(time.Second); at.After(end) {
		at = end
	}
	return at.Add(time.Second - 1).Truncate(time.Second).UTC()
}

// GiveUp forgets a run at once, whatever it was doing, and says whether the system had one by that
// reference (R-TRN-11). One in work stops soon after; its record is written then.
func (b *Book) GiveUp(system int64, ref string) bool {
	l := b.limits()
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.runs[ref]
	if !ok || r.system != system {
		return false
	}
	switch r.state {
	case Waiting:
		b.unqueue(r)
		b.tell(r.task, Result{GivenUp: true})
	case Working:
		r.gaveUp = true
		r.cancel()
		r.cancel = nil
	}
	b.forget(r)
	b.dispatch(l.AtATime)
	return true
}

// Counts is the runs waiting and working, for these systems or for all (R-ADM-02).
func (b *Book) Counts(systems []int64, all bool) (waiting, working int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.runs {
		if !all && !contains(systems, r.system) {
			continue
		}
		switch r.state {
		case Waiting:
			waiting++
		case Working:
			working++
		}
	}
	return waiting, working
}

func contains(list []int64, x int64) bool {
	for _, v := range list {
		if v == x {
			return true
		}
	}
	return false
}

// Stop forgets every run at once and takes no more: a run does not outlive the process, and one
// lost like this leaves no record (R-NFR-08, R-REC-01). It does not wait; Wait does.
func (b *Book) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return
	}
	b.stopped = true
	for _, r := range b.runs {
		r.lost = true
		if r.cancel != nil {
			r.cancel()
			r.cancel = nil
		}
		if r.timer != nil {
			r.timer.Stop()
		}
		r.timerGen++
		r.result, r.task = Result{}, nil
	}
	b.runs, b.queue = nil, nil
}

// Wait waits for the work and records still going after Stop.
func (b *Book) Wait() { b.going.Wait() }
