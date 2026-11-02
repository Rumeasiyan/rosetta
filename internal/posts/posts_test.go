package posts_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bifrost"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/Rumeasiyan/rosetta/internal/posts"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeLooker answers as it is told, by email.
type fakeLooker struct {
	answers map[string]func() (bifrost.Answer, error)
	asked   []string
}

func (f *fakeLooker) Lookup(_ context.Context, email string) (bifrost.Answer, error) {
	f.asked = append(f.asked, email)
	return f.answers[email]()
}

func found(office, post string) func() (bifrost.Answer, error) {
	return func() (bifrost.Answer, error) {
		return bifrost.Answer{Found: true, Offices: []bifrost.Office{{Office: office, Post: post}}}, nil
	}
}

func notFound() (bifrost.Answer, error) { return bifrost.Answer{}, nil }

func noOffices() (bifrost.Answer, error) { return bifrost.Answer{Found: true}, nil }

func failed() (bifrost.Answer, error) { return bifrost.Answer{}, errors.New("no answer") }

type holder struct {
	person posts.Person
	grant  int64
}

var seq int

// holding is a person holding a live role, with the post given.
func holding(t *testing.T, pool *pgxpool.Pool, post string) holder {
	t.Helper()
	ctx := context.Background()
	seq++
	stamp := time.Now().UnixNano()
	var h holder
	h.person.Subject = "post-subject-" + itoaN(stamp) + itoaN(int64(seq))
	h.person.Email = h.person.Subject + "@example.invalid"
	if err := pool.QueryRow(ctx, `INSERT INTO person (mjolnir_subject, name, email) VALUES ($1, 'Invented', $2) RETURNING id`, h.person.Subject, h.person.Email).Scan(&h.person.ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO role_grant (person_id, role, language, post_ref) VALUES ($1, 'glossary keeper', 'ta', $2) RETURNING id`, h.person.ID, post).Scan(&h.grant); err != nil {
		t.Fatal(err)
	}
	return h
}

func itoaN(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{digits[n%10]}, out...)
		n /= 10
	}
	return string(out)
}

type grantNow struct {
	post    string
	ended   bool
	flagged bool
	checked bool
}

func read(t *testing.T, pool *pgxpool.Pool, id int64) grantNow {
	t.Helper()
	var g grantNow
	if err := pool.QueryRow(context.Background(), `SELECT post_ref, ended_at IS NOT NULL, post_flagged_at IS NOT NULL, post_checked_at IS NOT NULL FROM role_grant WHERE id = $1`, id).Scan(&g.post, &g.ended, &g.flagged, &g.checked); err != nil {
		t.Fatal(err)
	}
	return g
}

func entries(t *testing.T, pool *pgxpool.Pool, subject string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT details::text FROM audit_outbox WHERE details->>'object_ref' = $1 ORDER BY id`, subject)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

const clerk = "Invented Clerk, EP-I-1"

// E-CAL-05's note, line by line.
func TestTheRule(t *testing.T) {
	pool := dbtest.Open(t)
	s := &posts.Service{Pool: pool}
	ctx := context.Background()
	check := func(h holder, answer func() (bifrost.Answer, error)) {
		t.Helper()
		s.Looker = &fakeLooker{answers: map[string]func() (bifrost.Answer, error){h.person.Email: answer}}
		s.Check(ctx, h.person)
	}

	t.Run("a person never found keeps the role, flagged, with the post not known", func(t *testing.T) {
		h := holding(t, pool, people.PostNotKnown)
		check(h, notFound)
		if g := read(t, pool, h.grant); g.ended || !g.flagged || g.post != people.PostNotKnown || !g.checked {
			t.Fatalf("%+v", g)
		}
		// The first time they are found the post is filled and the flag clears.
		check(h, found("EP-I-1", "Invented Clerk"))
		if g := read(t, pool, h.grant); g.ended || g.flagged || g.post != clerk {
			t.Fatalf("%+v", g)
		}
	})

	t.Run("found true with no offices counts as found at no post", func(t *testing.T) {
		h := holding(t, pool, people.PostNotKnown)
		check(h, noOffices)
		if g := read(t, pool, h.grant); g.ended || !g.flagged || g.post != people.PostNotKnown {
			t.Fatalf("never found before, so kept: %+v", g)
		}
		earlier := holding(t, pool, clerk)
		check(earlier, noOffices)
		if g := read(t, pool, earlier.grant); !g.ended {
			t.Fatalf("found earlier, now at no post: %+v", g)
		}
	})

	t.Run("found earlier and now not found loses the role", func(t *testing.T) {
		h := holding(t, pool, clerk)
		check(h, notFound)
		if g := read(t, pool, h.grant); !g.ended {
			t.Fatalf("%+v", g)
		}
		got := entries(t, pool, h.person.Subject)
		if len(got) != 1 || !strings.Contains(got[0], `"action": "role change"`) || !strings.Contains(got[0], "no longer holds the post") || !strings.Contains(got[0], `"post": "system"`) {
			t.Fatalf("%v", got)
		}
		if strings.Contains(got[0], h.person.Email) {
			t.Fatal("the address is in the entry")
		}
	})

	t.Run("at another post at that office loses the role", func(t *testing.T) {
		h := holding(t, pool, clerk)
		check(h, found("EP-I-1", "Invented Director"))
		if g := read(t, pool, h.grant); !g.ended {
			t.Fatalf("%+v", g)
		}
	})

	t.Run("the same post stands, and any office holding it does", func(t *testing.T) {
		h := holding(t, pool, clerk)
		check(h, func() (bifrost.Answer, error) {
			return bifrost.Answer{Found: true, Offices: []bifrost.Office{{Office: "EP-I-2", Post: "Another"}, {Office: "EP-I-1", Post: "Invented Clerk"}}}, nil
		})
		if g := read(t, pool, h.grant); g.ended || g.post != clerk {
			t.Fatalf("%+v", g)
		}
	})

	t.Run("a failed look-up changes nothing", func(t *testing.T) {
		for _, post := range []string{clerk, people.PostNotKnown} {
			h := holding(t, pool, post)
			before := read(t, pool, h.grant)
			check(h, failed)
			if g := read(t, pool, h.grant); g != before {
				t.Fatalf("%+v became %+v", before, g)
			}
			if got := entries(t, pool, h.person.Subject); len(got) != 0 {
				t.Fatalf("%v", got)
			}
		}
	})

	t.Run("no address asked after is a failed look-up", func(t *testing.T) {
		h := holding(t, pool, clerk)
		h.person.Email = ""
		s.Looker = &fakeLooker{}
		if got := s.Check(ctx, h.person); got != clerk {
			t.Fatal(got)
		}
		if g := read(t, pool, h.grant); g.ended {
			t.Fatalf("%+v", g)
		}
	})

	t.Run("the session keeps the post", func(t *testing.T) {
		h := holding(t, pool, clerk)
		s.Looker = &fakeLooker{answers: map[string]func() (bifrost.Answer, error){h.person.Email: found("EP-I-1", "Invented Clerk")}}
		if got := s.Check(ctx, h.person); got != clerk {
			t.Fatal(got)
		}
		s.Looker = &fakeLooker{answers: map[string]func() (bifrost.Answer, error){h.person.Email: failed}}
		if got := s.Check(ctx, h.person); got != clerk {
			t.Fatal(got)
		}
		s.Looker = nil
		if got := s.Check(ctx, h.person); got != clerk {
			t.Fatal(got)
		}
	})

	t.Run("the last administrator goes too, as R-ADM-06 makes no exception", func(t *testing.T) {
		h := holding(t, pool, clerk)
		if _, err := pool.Exec(ctx, `UPDATE role_grant SET role = 'administrator', language = NULL WHERE id = $1`, h.grant); err != nil {
			t.Fatal(err)
		}
		check(h, notFound)
		if g := read(t, pool, h.grant); !g.ended {
			t.Fatalf("%+v", g)
		}
	})
}

func clearRoles(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE role_grant SET ended_at = now() WHERE ended_at IS NULL`); err != nil {
		t.Fatal(err)
	}
}

// The daily pass asks after each person once, at its pace, honours Retry-After, and a failed look-up
// stops nothing. Nobody is asked twice however many roles they hold.
func TestThePass(t *testing.T) {
	pool := dbtest.Open(t)
	clearRoles(t, pool)
	t.Cleanup(func() { clearRoles(t, pool) })
	ctx := context.Background()
	a := holding(t, pool, clerk)
	b := holding(t, pool, people.PostNotKnown)
	c := holding(t, pool, clerk)
	d := holding(t, pool, clerk)
	if _, err := pool.Exec(ctx, `INSERT INTO role_grant (person_id, role, language, post_ref) VALUES ($1, 'memory reviewer', 'ta', $2)`, a.person.ID, clerk); err != nil {
		t.Fatal(err)
	}
	tries := 0
	f := &fakeLooker{answers: map[string]func() (bifrost.Answer, error){
		a.person.Email: notFound,
		b.person.Email: found("EP-I-1", "Invented Clerk"),
		c.person.Email: failed,
		d.person.Email: func() (bifrost.Answer, error) {
			tries++
			if tries == 1 {
				return bifrost.Answer{}, &bifrost.Refused{Status: 429, Code: "too_many", Wait: 42 * time.Second}
			}
			return bifrost.Answer{Found: true, Offices: []bifrost.Office{{Office: "EP-I-1", Post: "Invented Clerk"}}}, nil
		},
	}}
	var waits []time.Duration
	s := &posts.Service{Pool: pool, Looker: f, Sleep: func(_ context.Context, w time.Duration) error { waits = append(waits, w); return nil }}
	got, err := s.Pass(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got.Asked != 4 || got.Failed != 1 || got.Ended != 2 || got.Left != 0 {
		t.Fatalf("%+v", got)
	}
	if len(f.asked) != 5 {
		t.Fatalf("%v", f.asked)
	}
	// Three gaps of two seconds between four people, and one wait of what BIFROST asked.
	want := map[time.Duration]int{2 * time.Second: 3, 42 * time.Second: 1}
	for _, w := range waits {
		want[w]--
	}
	for w, n := range want {
		if n != 0 {
			t.Fatalf("waits %v: %v off by %d", waits, w, n)
		}
	}
	if g := read(t, pool, a.grant); !g.ended {
		t.Fatalf("a: %+v", g)
	}
	if g := read(t, pool, b.grant); g.ended || g.post != clerk {
		t.Fatalf("b: %+v", g)
	}
	if g := read(t, pool, c.grant); g.ended {
		t.Fatalf("c: a failed look-up ended it: %+v", g)
	}
	if g := read(t, pool, d.grant); g.ended {
		t.Fatalf("d: %+v", g)
	}
}

// A wait past the limit stops the pass and leaves the rest alone.
func TestThePassStopsAtItsLimit(t *testing.T) {
	pool := dbtest.Open(t)
	clearRoles(t, pool)
	t.Cleanup(func() { clearRoles(t, pool) })
	holding(t, pool, clerk)
	holding(t, pool, clerk)
	s := &posts.Service{Pool: pool, Looker: &fakeLooker{answers: map[string]func() (bifrost.Answer, error){}}}
	s.Looker = lookerFunc(func(string) (bifrost.Answer, error) {
		return bifrost.Answer{Found: true, Offices: []bifrost.Office{{Office: "EP-I-1", Post: "Invented Clerk"}}}, nil
	})
	s.Sleep = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	got, err := s.Pass(context.Background(), 1)
	if err != nil || got.Asked != 1 || got.Left != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

type lookerFunc func(string) (bifrost.Answer, error)

func (f lookerFunc) Lookup(_ context.Context, email string) (bifrost.Answer, error) { return f(email) }
