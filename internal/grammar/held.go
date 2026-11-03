package grammar

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/settings"
)

// HeldBytes is the most the held suggestions may take, by a fair estimate. A figure of this
// build's, not the PRD's: E-NFR-01 measures it inside the memory cap (R-NFR-01).
const HeldBytes = 8 << 20

// heldOverhead is what one entry costs beyond its words, by the same estimate.
const heldOverhead = 256

// Held spares sources by keeping the suggestions for an identical text for a while
// (R-GRM-05). It lives only in this process, so it is gone at a restart, and it is found by a
// hash: neither the text nor its plain hash is held, and each suggestion's context is made
// afresh from the text sent. Safe for concurrent use.
type Held struct {
	Max int              // HeldBytes when 0
	Now func() time.Time // time.Now when nil

	once  sync.Once
	salt  [32]byte
	mu    sync.Mutex
	byKey map[[sha256.Size]byte]*list.Element
	order *list.List // oldest first
	size  int
}

type heldEntry struct {
	key    [sha256.Size]byte
	answer Answer
	at     time.Time
	size   int
}

func (h *Held) start() {
	h.once.Do(func() {
		rand.Read(h.salt[:])
		h.byKey = map[[sha256.Size]byte]*list.Element{}
		h.order = list.New()
	})
}

func (h *Held) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Held) most() int {
	if h.Max > 0 {
		return h.Max
	}
	return HeldBytes
}

func (h *Held) key(text string) [sha256.Size]byte {
	d := sha256.New()
	d.Write(h.salt[:])
	d.Write([]byte(text))
	var out [sha256.Size]byte
	d.Sum(out[:0])
	return out
}

func (h *Held) remove(e *list.Element) {
	x := e.Value.(*heldEntry)
	h.order.Remove(e)
	delete(h.byKey, x.key)
	h.size -= x.size
}

// Get is what is held for this text, if it is younger than keep. What is older is forgotten.
func (h *Held) Get(text string, keep time.Duration) (Answer, bool) {
	h.start()
	key := h.key(text)
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.byKey[key]
	if !ok {
		return Answer{}, false
	}
	x := e.Value.(*heldEntry)
	if h.now().Sub(x.at) >= keep {
		h.remove(e)
		return Answer{}, false
	}
	return x.answer, true
}

func sizeOf(a Answer) int {
	n := heldOverhead
	for _, s := range a.Suggestions {
		n += heldOverhead + len(s.Message) + len(s.Category) + len(s.Source)
		for _, r := range s.Replacements {
			n += heldOverhead + len(r)
		}
	}
	return n
}

// Put holds an answer, letting go of the oldest to stay under the size. An answer too big to
// hold at all is not held.
func (h *Held) Put(text string, a Answer) {
	h.start()
	x := &heldEntry{key: h.key(text), answer: a, at: h.now(), size: sizeOf(a)}
	if x.size > h.most() {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if e, ok := h.byKey[x.key]; ok {
		h.remove(e)
	}
	for h.size+x.size > h.most() {
		h.remove(h.order.Front())
	}
	h.byKey[x.key] = h.order.PushBack(x)
	h.size += x.size
}

// Sweep forgets everything older than keep, so a text no one sends again does not stay past
// its time (R-NFR-12). It runs off the source list's timer: no timer of its own (AGENTS C6).
func (h *Held) Sweep(keep time.Duration) {
	h.start()
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for e := h.order.Front(); e != nil; e = h.order.Front() {
		if now.Sub(e.Value.(*heldEntry).at) < keep {
			return
		}
		h.remove(e)
	}
}

// Len is how many answers are held.
func (h *Held) Len() int {
	h.start()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.order.Len()
}

// keepFor is the setting, as a time.
func keepFor(v settings.Values) time.Duration {
	return time.Duration(v.Of(settings.GrammarHoldMinutes)) * time.Minute
}

// SweepWith is Sweep at the time the settings give, for the source list's timer to call.
func (h *Held) SweepWith(ctx context.Context, values func(ctx context.Context) settings.Values) {
	h.Sweep(keepFor(values(ctx)))
}
