package limits

import (
	"sync"
	"testing"
	"time"
)

func TestRequestsAMinute(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	l := &Limiter{Now: func() time.Time { return now }}
	for i := range 3 {
		release, _, ok := l.Acquire(1, 3, 10)
		if !ok {
			t.Fatal(i)
		}
		release()
		now = now.Add(10 * time.Second)
	}
	// Three in the last 30 seconds: the next fits when the oldest leaves the window.
	_, wait, ok := l.Acquire(1, 3, 10)
	if ok || wait != 30*time.Second {
		t.Fatal(ok, wait)
	}
	// Another system is not held back by this one.
	if _, _, ok := l.Acquire(2, 3, 10); !ok {
		t.Fatal("system 2")
	}
	// A refusal does not count, so waiting as told is enough.
	now = now.Add(wait)
	if _, _, ok := l.Acquire(1, 3, 10); !ok {
		t.Fatal("after the wait")
	}
}

// A lower limit set while requests are recent still says a wait that is enough.
func TestALimitLoweredInForceAtOnce(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	l := &Limiter{Now: func() time.Time { return now }}
	for range 3 {
		release, _, _ := l.Acquire(1, 60, 10)
		release()
		now = now.Add(time.Second)
	}
	_, wait, ok := l.Acquire(1, 1, 10)
	if ok || wait != 59*time.Second {
		t.Fatal(ok, wait)
	}
	now = now.Add(wait)
	if _, _, ok := l.Acquire(1, 1, 10); !ok {
		t.Fatal("after the wait")
	}
}

func TestRequestsAtOnce(t *testing.T) {
	l := &Limiter{}
	r1, _, ok1 := l.Acquire(1, 100, 2)
	r2, _, ok2 := l.Acquire(1, 100, 2)
	if !ok1 || !ok2 {
		t.Fatal("two fit")
	}
	if _, wait, ok := l.Acquire(1, 100, 2); ok || wait <= 0 {
		t.Fatal(ok, wait)
	}
	r1()
	r1() // twice changes nothing
	r3, _, ok := l.Acquire(1, 100, 2)
	if !ok {
		t.Fatal("room after one ended")
	}
	if _, _, ok := l.Acquire(1, 100, 2); ok {
		t.Fatal("a release counted twice")
	}
	r2()
	r3()
}

func TestAtOnceHoldsUnderLoad(t *testing.T) {
	l := &Limiter{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	running, most := 0, 0
	for range 100 {
		wg.Go(func() {
			release, _, ok := l.Acquire(1, 1000, 5)
			if !ok {
				return
			}
			mu.Lock()
			running++
			most = max(most, running)
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			running--
			mu.Unlock()
			release()
		})
	}
	wg.Wait()
	if most > 5 {
		t.Fatal(most)
	}
}
