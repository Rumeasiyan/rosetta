// Command rosetta-load measures ROSETTA at the capacity R-NFR-01 names, against stand-ins (E-NFR-01).
// Development only: it is not in the image. Every text and person it makes is invented.
//
//	rosetta-load seed -keys FILE     30 systems with keys, 20,000 published entries, 500,000 memory pieces
//	rosetta-load run  -keys FILE -pid PID -rate 0.84 -for 5m -source-delay 2s
//
// seed reads DATABASE_URL. run sends what R-NFR-01 names, 3,000 requests an hour with up to 300 source
// calls, in the mix below, and gives each kind's times and the service's largest resident memory.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: rosetta-load seed|run ...")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "seed":
		err = seed(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	default:
		err = fmt.Errorf("no command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rosetta-load:", err)
		os.Exit(1)
	}
}

func seed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	keysFile := fs.String("keys", "", "file for the systems' keys")
	systems := fs.Int("systems", 30, "calling systems")
	entries := fs.Int("glossary", 20000, "published glossary entries")
	pieces := fs.Int("memory", 500000, "memory pieces")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, os.Getenv("DATABASE_URL"), 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	started := time.Now()
	var person int64
	if err := pool.QueryRow(ctx, `INSERT INTO person (mjolnir_subject, name, email) VALUES ('load-person', 'Invented Loader', 'load@example.invalid')
		ON CONFLICT (mjolnir_subject) DO UPDATE SET name = EXCLUDED.name RETURNING id`).Scan(&person); err != nil {
		return err
	}
	keys := make([]string, 0, *systems)
	var first int64
	for i := range *systems {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO caller (tva_reference, name, tva_status, tva_checked_at, requests_per_minute, requests_at_once)
			VALUES ($1, $1, 'live', now(), 600, 20) RETURNING id`, fmt.Sprintf("LOAD-%d-%d", started.Unix(), i)).Scan(&id); err != nil {
			return err
		}
		if first == 0 {
			first = id
		}
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			key, _, err := callers.Issue(ctx, tx, id, person, "")
			keys = append(keys, key)
			return err
		}); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(keys)
	if err := os.WriteFile(*keysFile, raw, 0o600); err != nil {
		return err
	}
	fmt.Printf("%d systems in %v\n", *systems, time.Since(started).Round(time.Millisecond))

	started = time.Now()
	if err := glossary(ctx, pool, person, *entries); err != nil {
		return err
	}
	fmt.Printf("%d published entries in %v\n", *entries, time.Since(started).Round(time.Millisecond))

	started = time.Now()
	const fill = `INSERT INTO memory_entry (caller_id, glossary_version, from_language, to_language, source_text, source_hash, approved_text, approver_post, approved_on)
		SELECT $1, 0, 'en', 'ta', 'Load memory piece number ' || n || ' is invented.', encode(sha256(('Load memory piece number ' || n || ' is invented.')::bytea), 'hex'),
			'புனைந்த நினைவகப் பகுதி ' || n || ' இங்கே.', 'An invented post', '2020-01-01'
		FROM generate_series($2::bigint, $3::bigint) n`
	const batch = 50000
	for from := 1; from <= *pieces; from += batch {
		if _, err := pool.Exec(ctx, fill, first, from, min(from+batch-1, *pieces)); err != nil {
			return err
		}
	}
	if _, err := pool.Exec(ctx, `ANALYZE memory_entry`); err != nil {
		return err
	}
	var size string
	if err := pool.QueryRow(ctx, `SELECT pg_size_pretty(pg_total_relation_size('memory_entry'))`).Scan(&size); err != nil {
		return err
	}
	fmt.Printf("%d memory pieces in %v, %s with indexes\n", *pieces, time.Since(started).Round(time.Millisecond), size)
	return nil
}

// glossary publishes invented entries directly, as keepers' checks would have, under one version.
func glossary(ctx context.Context, pool *pgxpool.Pool, person int64, n int) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var version int
		if err := tx.QueryRow(ctx, `INSERT INTO glossary_version (number, published_by, summary) SELECT COALESCE(max(number), 0) + 1, $1, 'load' FROM glossary_version RETURNING number`, person).Scan(&version); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `WITH made AS (INSERT INTO glossary_entry (type, status, published_in, proposed_by)
				SELECT 'term', 'published', $1, $2 FROM generate_series(1, $3) RETURNING id),
			numbered AS (SELECT id, row_number() OVER (ORDER BY id) AS n FROM made)
			INSERT INTO glossary_term (glossary_entry_id, language, text, checked_at, checked_by)
			SELECT id, l, CASE l WHEN 'en' THEN 'Loadterm ' || n WHEN 'si' THEN 'පදය ' || n ELSE 'சொல் ' || n END, now(), $2
			FROM numbered, unnest(ARRAY['en', 'si', 'ta']) AS l`, version, person, n); err != nil {
			return err
		}
		return nil
	})
}

// sample is one request's outcome.
type sample struct {
	kind   string
	took   time.Duration
	status int
	reason string
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	keysFile := fs.String("keys", "", "file holding the systems' keys")
	target := fs.String("url", "http://localhost:53080", "ROSETTA's address")
	pid := fs.Int("pid", 0, "the service's process, to read its memory")
	rate := fs.Float64("rate", 3000.0/3600, "requests a second")
	length := fs.Duration("for", 5*time.Minute, "how long")
	delay := fs.Duration("source-delay", 0, "how long the stand-in takes, so ROSETTA's own time can be told")
	fill := fs.Int("held", 0, "large distinct checks to send first, to fill what grammar holds")
	if err := fs.Parse(args); err != nil {
		return err
	}
	raw, err := os.ReadFile(*keysFile)
	if err != nil {
		return err
	}
	var keys []string
	if err := json.Unmarshal(raw, &keys); err != nil {
		return err
	}
	client := &http.Client{Timeout: 90 * time.Second}
	send := func(kind, key, path, contentType, body string) sample {
		r, _ := http.NewRequest(http.MethodPost, *target+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", contentType)
		at := time.Now()
		res, err := client.Do(r)
		took := time.Since(at)
		if err != nil {
			return sample{kind: kind, took: took, reason: "no answer"}
		}
		defer res.Body.Close()
		answer, _ := io.ReadAll(res.Body)
		s := sample{kind: kind, took: took, status: res.StatusCode}
		if res.StatusCode != 200 {
			var f struct {
				Error struct {
					Reason string `json:"reason"`
				} `json:"error"`
			}
			json.Unmarshal(answer, &f)
			s.reason = f.Error.Reason
		}
		return s
	}
	translate := func(kind, key, text string) sample {
		b, _ := json.Marshal(map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "passages": []string{text}})
		return send(kind, key, "/api/v1/translate", "application/json", string(b))
	}
	check := func(kind, key, text string) sample {
		return send(kind, key, "/api/v2/check", "application/x-www-form-urlencoded", url.Values{"language": {"en-GB"}, "text": {text}}.Encode())
	}

	// The largest resident memory, read every two seconds.
	stop := make(chan struct{})
	peak := 0
	var peakMu sync.Mutex
	readRSS := func() int {
		out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(*pid)).Output()
		if err != nil {
			return 0
		}
		n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		return n
	}
	if *pid != 0 {
		go func() {
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					n := readRSS()
					peakMu.Lock()
					peak = max(peak, n)
					peakMu.Unlock()
				}
			}
		}()
		fmt.Printf("resident memory at the start: %d KiB\n", readRSS())
	}
	// Grammar's held suggestions, at their cap: large checks full of findings, each different. A
	// suggestion's words count for what is held, so this is the worst case.
	if *fill > 0 {
		at := time.Now()
		for i := range *fill {
			text := strings.Repeat("We recieve this seperate letter. ", 500) + fmt.Sprintf("Invented number %d.", i)
			if s := check("held", keys[i%len(keys)], text); s.status != 200 {
				fmt.Printf("a large check answered %d %s\n", s.status, s.reason)
			}
		}
		fmt.Printf("sent %d large checks in %v\n", *fill, time.Since(at).Round(time.Millisecond))
		if *pid != 0 {
			fmt.Printf("resident memory after filling what grammar holds: %d KiB\n", readRSS())
		}
	}
	// Warm: one of each answer the mix asks for again, so cache and memory are what they answer from.
	nCache := 40
	for i := range nCache {
		translate("warm", keys[0], fmt.Sprintf("Load cache sentence %d is invented.", i))
	}
	var mu sync.Mutex
	var samples []sample
	var wg sync.WaitGroup
	gap := time.Duration(float64(time.Second) / *rate)
	t := time.NewTicker(gap)
	end := time.After(*length)
	sent := 0
loop:
	for {
		select {
		case <-end:
			break loop
		case <-t.C:
		}
		sent++
		key := keys[rand.IntN(len(keys))]
		wg.Go(func() {
			var s sample
			switch p := rand.IntN(100); {
			case p < 10: // 300 source calls in 3,000 requests
				s = translate("source", key, fmt.Sprintf("Load source sentence %d %d is invented.", time.Now().UnixNano(), rand.Int()))
			case p < 45:
				s = translate("memory", key, fmt.Sprintf("Load memory piece number %d is invented.", 1+rand.IntN(500000)))
			case p < 70:
				s = translate("cache", key, fmt.Sprintf("Load cache sentence %d is invented.", rand.IntN(nCache)))
			case p < 85:
				s = translate("glossary", key, fmt.Sprintf("Loadterm %d", 1+rand.IntN(20000)))
			default:
				s = check("check-held", key, "We recieve one invented letter.")
			}
			mu.Lock()
			samples = append(samples, s)
			mu.Unlock()
		})
	}
	t.Stop()
	wg.Wait()
	close(stop)
	kinds := map[string][]time.Duration{}
	bad := map[string]int{}
	for _, s := range samples {
		kinds[s.kind] = append(kinds[s.kind], s.took)
		if s.status != 200 {
			bad[s.kind+" "+s.reason]++
		}
	}
	fmt.Printf("sent %d in %v, at %.2f a second\n", sent, *length, *rate)
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		d := kinds[k]
		slices.Sort(d)
		q := func(p int) time.Duration { return d[min(len(d)*p/100, len(d)-1)].Round(100 * time.Microsecond) }
		line := fmt.Sprintf("%-11s n=%-5d p50=%-10v p95=%-10v max=%v", k, len(d), q(50), q(95), d[len(d)-1].Round(100*time.Microsecond))
		if k == "source" && *delay > 0 {
			line += fmt.Sprintf("  rosetta's own at p95=%v", q(95)-*delay)
		}
		fmt.Println(line)
	}
	fmt.Println("not answered:", bad)
	if *pid != 0 {
		fmt.Printf("largest resident memory seen: %d KiB (%.1f MiB)\n", peak, float64(peak)/1024)
	}
	return nil
}
