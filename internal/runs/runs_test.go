package runs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/reasons"
)

// task is work that waits to be told to end, or for its context.
type task struct {
	release chan Result
	started chan struct{}
	done    atomic.Int64
	total   int
	panics  bool

	mu    sync.Mutex
	ended []Result
	told  chan struct{}
}

func newTask() *task {
	return &task{release: make(chan Result, 1), started: make(chan struct{}), total: 4, told: make(chan struct{}, 4)}
}

func (t *task) Work(ctx context.Context) Result {
	close(t.started)
	if t.panics {
		panic("text a fault must not repeat")
	}
	select {
	case r := <-t.release:
		return r
	case <-ctx.Done():
		return Result{Failure: reasons.New(reasons.TimedOut)}
	}
}

func (t *task) Pieces() (int, int) { return int(t.done.Load()), t.total }

func (t *task) Ended(r Result) {
	t.mu.Lock()
	t.ended = append(t.ended, r)
	t.mu.Unlock()
	t.told <- struct{}{}
}

func (t *task) endings() []Result {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Result(nil), t.ended...)
}

func (t *task) waitTold(tb testing.TB) {
	tb.Helper()
	select {
	case <-t.told:
	case <-time.After(5 * time.Second):
		tb.Fatal("never told how it ended")
	}
}

func (t *task) waitStarted(tb testing.TB) {
	tb.Helper()
	select {
	case <-t.started:
	case <-time.After(5 * time.Second):
		tb.Fatal("never started")
	}
}

func book(l Limits) *Book {
	return &Book{Limits: func(context.Context) Limits { return l }}
}

func wide() Limits {
	return Limits{PerSystem: 5, InAll: 50, AtATime: 2, Longest: time.Minute, Uncollected: time.Minute}
}

func begin(t *testing.T, b *Book, system int64, x Task) string {
	t.Helper()
	slot, _, err := b.Reserve(system)
	if err != nil {
		t.Fatal(err)
	}
	started, err := b.Begin(slot, "r-test", x)
	if err != nil {
		t.Fatal(err)
	}
	if started.AskAgainAt.Before(time.Now()) || (started.State != Waiting && started.State != Working) {
		t.Fatalf("%+v", started)
	}
	return started.Ref
}

// Only so many are worked at a time, and the rest wait in turn.
func TestAFewAreWorkedAtATime(t *testing.T) {
	b := book(wide())
	defer b.Stop()
	one, two, three := newTask(), newTask(), newTask()
	r1, r2, r3 := begin(t, b, 1, one), begin(t, b, 1, two), begin(t, b, 1, three)
	one.waitStarted(t)
	two.waitStarted(t)
	if w, g := b.Counts(nil, true); w != 1 || g != 2 {
		t.Fatalf("waiting %d, working %d", w, g)
	}
	if v, ok := b.Ask(1, r3); !ok || v.State != Waiting || v.Total != 4 || v.Result != nil {
		t.Fatalf("%+v", v)
	}
	if v, ok := b.Ask(1, r1); !ok || v.State != Working {
		t.Fatalf("%+v", v)
	}
	one.release <- Result{Body: []byte("one")}
	three.waitStarted(t)
	one.waitTold(t)
	if v, ok := b.Ask(1, r1); !ok || string(v.Result.Body) != "one" || v.Result.Ref != "r-test" {
		t.Fatalf("%+v %v", v, ok)
	}
	if _, ok := b.Ask(1, r1); ok {
		t.Fatal("handed over twice")
	}
	if len(one.endings()) != 1 || one.endings()[0].GivenUp {
		t.Fatalf("%+v", one.endings())
	}
	two.release <- Result{Failure: reasons.New(reasons.NoProvider).At(3)}
	two.waitTold(t)
	if v, _ := b.Ask(1, r2); v.Result == nil || v.Result.Failure.Reason != reasons.NoProvider || *v.Result.Failure.Position != 3 {
		t.Fatalf("%+v", v)
	}
	three.release <- Result{}
	three.waitTold(t)
}

// One system's five do not hold up another's.
func TestAnotherSystemIsNotHeldUp(t *testing.T) {
	l := wide()
	l.AtATime = 2
	b := book(l)
	defer b.Stop()
	a1, a2, a3, c := newTask(), newTask(), newTask(), newTask()
	begin(t, b, 1, a1)
	a1.waitStarted(t)
	begin(t, b, 1, a2)
	a2.waitStarted(t)
	begin(t, b, 1, a3)
	begin(t, b, 2, c)
	a1.release <- Result{}
	c.waitStarted(t)
	select {
	case <-a3.started:
		t.Fatal("the second system's should have gone first")
	default:
	}
}

// Past either limit a new run is refused with when to try again, and room comes back when a run is
// forgotten: an answer not yet collected still counts.
func TestLimits(t *testing.T) {
	l := wide()
	l.PerSystem, l.InAll = 2, 3
	b := book(l)
	defer b.Stop()
	first := newTask()
	r1 := begin(t, b, 1, first)
	begin(t, b, 1, newTask())
	_, wait, err := b.Reserve(1)
	if !errors.Is(err, ErrFull) || wait <= 0 {
		t.Fatalf("%v %v", err, wait)
	}
	begin(t, b, 2, newTask())
	if _, _, err := b.Reserve(3); !errors.Is(err, ErrFull) {
		t.Fatalf("in all: %v", err)
	}
	first.waitStarted(t)
	first.release <- Result{Body: []byte("x")}
	first.waitTold(t)
	if _, _, err := b.Reserve(3); !errors.Is(err, ErrFull) {
		t.Fatalf("an answer waiting still counts: %v", err)
	}
	if v, ok := b.Ask(1, r1); !ok || v.Result == nil {
		t.Fatal("no answer")
	}
	slot, _, err := b.Reserve(3)
	if err != nil {
		t.Fatal(err)
	}
	slot.Release()
	slot.Release()
	if _, _, err := b.Reserve(3); err != nil {
		t.Fatal("room given back was lost:", err)
	}
}

func TestAnotherSystemKnowsNothing(t *testing.T) {
	b := book(wide())
	defer b.Stop()
	x := newTask()
	ref := begin(t, b, 1, x)
	if _, ok := b.Ask(2, ref); ok {
		t.Fatal("asked by another system")
	}
	if b.GiveUp(2, ref) {
		t.Fatal("given up by another system")
	}
	if _, ok := b.Ask(1, ref); !ok {
		t.Fatal("its own system lost it")
	}
}

func TestReferencesAreRandomAndLong(t *testing.T) {
	b := book(wide())
	defer b.Stop()
	a, c := begin(t, b, 1, newTask()), begin(t, b, 1, newTask())
	if a == c || !strings.HasPrefix(a, "run-") || len(a) < 40 {
		t.Fatal(a, c)
	}
}

// Giving up forgets at once, in work or waiting, and each still says how it ended, once.
func TestGivingUp(t *testing.T) {
	l := wide()
	l.AtATime = 1
	b := book(l)
	defer b.Stop()
	working, waiting, after := newTask(), newTask(), newTask()
	r1, r2 := begin(t, b, 1, working), begin(t, b, 1, waiting)
	working.waitStarted(t)
	if !b.GiveUp(1, r2) {
		t.Fatal("waiting")
	}
	waiting.waitTold(t)
	if e := waiting.endings(); len(e) != 1 || !e[0].GivenUp {
		t.Fatalf("%+v", e)
	}
	if !b.GiveUp(1, r1) {
		t.Fatal("working")
	}
	if _, ok := b.Ask(1, r1); ok {
		t.Fatal("not forgotten at once")
	}
	if b.GiveUp(1, r1) {
		t.Fatal("gave up twice")
	}
	working.waitTold(t)
	if e := working.endings(); len(e) != 1 || !e[0].GivenUp {
		t.Fatalf("%+v", e)
	}
	// The room is free, and a run ended and given up says nothing more.
	begin(t, b, 1, after)
	after.waitStarted(t)
	r3 := begin(t, b, 2, newTask())
	b.GiveUp(2, r3)
	after.release <- Result{Body: []byte("x")}
	after.waitTold(t)
}

func TestARunEndedAndGivenUpKeepsItsOutcome(t *testing.T) {
	b := book(wide())
	defer b.Stop()
	x := newTask()
	ref := begin(t, b, 1, x)
	x.waitStarted(t)
	x.release <- Result{Body: []byte("x")}
	x.waitTold(t)
	if !b.GiveUp(1, ref) {
		t.Fatal("not held")
	}
	time.Sleep(20 * time.Millisecond)
	if e := x.endings(); len(e) != 1 || e[0].GivenUp {
		t.Fatalf("%+v", e)
	}
}

// A run's limit counts from when it was taken on, waiting included: nothing is held longer because
// others were ahead of it.
func TestTheLimitCountsWaitingToo(t *testing.T) {
	l := wide()
	l.AtATime, l.Longest = 1, 150*time.Millisecond
	b := book(l)
	defer b.Stop()
	working, waiting := newTask(), newTask()
	r1 := begin(t, b, 1, working)
	r2 := begin(t, b, 1, waiting)
	waiting.waitTold(t)
	if v, ok := b.Ask(1, r2); !ok || v.Result == nil || v.Result.Failure.Reason != reasons.TimedOut || v.Result.Ref != "r-test" {
		t.Fatalf("%+v %v", v, ok)
	}
	working.waitTold(t)
	if v, ok := b.Ask(1, r1); !ok || v.Result == nil || v.Result.Failure.Reason != reasons.TimedOut {
		t.Fatalf("%+v %v", v, ok)
	}
	select {
	case <-waiting.started:
		t.Fatal("work started on a run past its limit")
	default:
	}
}

// Past the limit a failure of any kind reads as the limit.
func TestAFailureAtTheLimitIsTimedOut(t *testing.T) {
	l := wide()
	l.Longest = 50 * time.Millisecond
	b := book(l)
	defer b.Stop()
	x := &stubborn{}
	slot, _, _ := b.Reserve(1)
	started, _ := b.Begin(slot, "r", x)
	ref := started.Ref
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if v, ok := b.Ask(1, ref); ok && v.Result != nil {
			if v.Result.Failure.Reason != reasons.TimedOut {
				t.Fatal(v.Result.Failure.Reason)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("never ended")
}

type stubborn struct{}

func (stubborn) Work(ctx context.Context) Result {
	<-ctx.Done()
	return Result{Failure: reasons.New(reasons.NoProvider)}
}
func (stubborn) Pieces() (int, int) { return 0, 1 }
func (stubborn) Ended(Result)       {}

// An answer not collected is forgotten at its time, and the record of how it ended stays.
func TestUncollectedIsForgotten(t *testing.T) {
	l := wide()
	l.Uncollected = 30 * time.Millisecond
	b := book(l)
	defer b.Stop()
	x := newTask()
	ref := begin(t, b, 1, x)
	x.waitStarted(t)
	x.release <- Result{Body: []byte("x")}
	x.waitTold(t)
	time.Sleep(150 * time.Millisecond)
	if _, ok := b.Ask(1, ref); ok {
		t.Fatal("still held")
	}
	if e := x.endings(); len(e) != 1 || string(e[0].Body) != "x" {
		t.Fatalf("%+v", e)
	}
	if _, _, err := b.Reserve(1); err != nil {
		t.Fatal(err)
	}
}

// A stop forgets everything at once, takes nothing new, and what it lost leaves no record.
func TestAStopLosesEveryRunAndLeavesNoRecord(t *testing.T) {
	l := wide()
	l.AtATime = 1
	b := book(l)
	working, waiting := newTask(), newTask()
	r1 := begin(t, b, 1, working)
	r2 := begin(t, b, 1, waiting)
	working.waitStarted(t)
	spare, _, err := b.Reserve(1)
	if err != nil {
		t.Fatal(err)
	}
	b.Stop()
	b.Stop()
	b.Wait()
	for _, ref := range []string{r1, r2} {
		if _, ok := b.Ask(1, ref); ok {
			t.Fatal("held after a stop")
		}
	}
	if len(working.endings()) != 0 || len(waiting.endings()) != 0 {
		t.Fatal("a run lost to a stop left a record")
	}
	if _, _, err := b.Reserve(1); !errors.Is(err, ErrStopped) {
		t.Fatal(err)
	}
	if _, err := b.Begin(spare, "r", newTask()); !errors.Is(err, ErrStopped) {
		t.Fatal(err)
	}
	if w, g := b.Counts(nil, true); w != 0 || g != 0 {
		t.Fatal(w, g)
	}
}

// A fault in one run answers with a reason, says nothing it held, and the rest go on.
func TestAFaultStaysInOneRun(t *testing.T) {
	var logs bytes.Buffer
	b := book(wide())
	b.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	defer b.Stop()
	x := newTask()
	x.panics = true
	ref := begin(t, b, 1, x)
	x.waitTold(t)
	if strings.Contains(logs.String(), "must not repeat") || !strings.Contains(logs.String(), "a run stopped on a fault") {
		t.Fatal(logs.String())
	}
	if v, ok := b.Ask(1, ref); !ok || v.Result == nil || v.Result.Failure.Reason != reasons.NoProvider {
		t.Fatalf("%+v", v)
	}
	y := newTask()
	begin(t, b, 1, y)
	y.waitStarted(t)
}

// Counts for a system owner are that owner's systems'.
func TestCounts(t *testing.T) {
	b := book(wide())
	defer b.Stop()
	begin(t, b, 1, newTask())
	begin(t, b, 2, newTask())
	begin(t, b, 2, newTask())
	if w, g := b.Counts([]int64{2}, false); w+g != 2 {
		t.Fatal(w, g)
	}
	if w, g := b.Counts(nil, false); w+g != 0 {
		t.Fatal(w, g)
	}
	if w, g := b.Counts(nil, true); w+g != 3 {
		t.Fatal(w, g)
	}
}

// When to ask follows the rate so far, within its bounds, and in whole seconds.
func TestAskAgain(t *testing.T) {
	b := book(wide())
	defer b.Stop()
	now := time.Now()
	x := newTask()
	r := &run{task: x, state: Waiting, deadline: now.Add(time.Hour)}
	if got := b.askAgain(r, now); got.Sub(now) < askWaiting || got.Sub(now) > askWaiting+time.Second || got.Nanosecond() != 0 {
		t.Fatal(got.Sub(now))
	}
	r.state, r.began = Working, now.Add(-10*time.Second)
	if got := b.askAgain(r, now); got.Sub(now) < askLeast || got.Sub(now) > askLeast+time.Second {
		t.Fatal(got.Sub(now))
	}
	x.total = 100
	x.done.Store(1)
	if got := b.askAgain(r, now); got.Sub(now) < askMost || got.Sub(now) > askMost+time.Second {
		t.Fatal("slow pieces:", got.Sub(now))
	}
	x.total = 3
	if got := b.askAgain(r, now); got.Sub(now) < 20*time.Second || got.Sub(now) > 21*time.Second {
		t.Fatal("two left at ten seconds each:", got.Sub(now))
	}
	r.deadline = now.Add(2 * time.Second)
	if got := b.askAgain(r, now); got.Sub(now) > 4*time.Second {
		t.Fatal("past its own end:", got.Sub(now))
	}
}
