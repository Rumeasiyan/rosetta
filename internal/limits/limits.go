// Package limits holds each calling system to its requests a minute and requests at
// once (R-CAL-07). It is about what a system sends ROSETTA, in memory, in the one
// process there is. It has nothing to do with BUMBLEBEE's rate, which BUMBLEBEE's own
// queue enforces (R-PRV-04).
package limits

import (
	"sync"
	"time"
)

// Window is the "minute" in requests a minute: any 60 seconds, not a clock minute, so
// a burst across the turn of a minute gets no double share.
const Window = time.Minute

// tryAgainRunning is what a caller over its requests at once is told: there is no
// telling when one will end, so a short wait.
const tryAgainRunning = time.Second

type system struct {
	recent  []time.Time // the requests inside the window, oldest first
	running int
}

// Limiter counts each system's requests.
type Limiter struct {
	Now func() time.Time // time.Now when nil

	mu sync.Mutex
	by map[int64]*system
}

// Acquire lets a request start, or says how long to wait. A request that started must
// call release when it ends; a refused one does not count against either limit.
func (l *Limiter) Acquire(id int64, perMinute, atOnce int) (release func(), wait time.Duration, ok bool) {
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.by == nil {
		l.by = map[int64]*system{}
	}
	s := l.by[id]
	if s == nil {
		s = &system{}
		l.by[id] = s
	}
	cut := 0
	for cut < len(s.recent) && now.Sub(s.recent[cut]) >= Window {
		cut++
	}
	s.recent = s.recent[cut:]
	if len(s.recent) >= perMinute {
		// When enough have left the window for this one to fit.
		return nil, s.recent[len(s.recent)-perMinute].Add(Window).Sub(now), false
	}
	if s.running >= atOnce {
		return nil, tryAgainRunning, false
	}
	s.recent = append(s.recent, now)
	s.running++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			s.running--
		})
	}, 0, true
}
