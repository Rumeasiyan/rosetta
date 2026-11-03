// Package evaluate runs a test set against one source and gives the score the gate records (R-GAT-01,
// R-NFR-14): a new source version starts closed, and an administrator opens a direction only on such an
// evaluation. It goes straight to BUMBLEBEE, as the gate would stop anything else, and masks numbers as
// everything leaving the server does (AGENTS C5). Test text is invented or supplied by volunteers,
// never real letters; nothing here logs it or writes it anywhere.
package evaluate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/mask"
	"github.com/Rumeasiyan/rosetta/internal/terms"
)

// Sentence is one row of a test set: the shape docs/STANDARDS.md section 7 gives it, in TSV or JSONL.
type Sentence struct {
	ID string `json:"id"`
	EN string `json:"en"`
	SI string `json:"si"`
	TA string `json:"ta"`
}

func (s Sentence) in(language string) string {
	switch language {
	case "en":
		return s.EN
	case "si":
		return s.SI
	case "ta":
		return s.TA
	}
	return ""
}

// The failures. None holds a word of the set.
var (
	ErrBadSet    = errors.New("the test set is not TSV with columns id, en, si, ta, or JSONL of the same")
	ErrNoneFound = errors.New("the test set holds no sentence in both languages")
	ErrNoOffer   = errors.New("the source is not on the list, or does not offer this direction")
)

// maxSet is the most read of a test set.
const maxSet = 8 << 20

// Read reads a test set. A name ending .jsonl is JSON lines; anything else is TSV with a header.
func Read(name string, r io.Reader) ([]Sentence, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxSet+1))
	if err != nil || len(raw) > maxSet {
		return nil, ErrBadSet
	}
	if strings.EqualFold(filepath.Ext(name), ".jsonl") {
		var out []Sentence
		scan := bufio.NewScanner(bytes.NewReader(raw))
		scan.Buffer(make([]byte, 1<<20), maxSet)
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if line == "" {
				continue
			}
			var s Sentence
			if json.Unmarshal([]byte(line), &s) != nil {
				return nil, ErrBadSet
			}
			out = append(out, s)
		}
		return out, nil
	}
	c := csv.NewReader(bytes.NewReader(raw))
	c.Comma, c.LazyQuotes, c.FieldsPerRecord = '\t', true, -1
	rows, err := c.ReadAll()
	if err != nil || len(rows) < 1 {
		return nil, ErrBadSet
	}
	at := map[string]int{}
	for i, h := range rows[0] {
		at[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, need := range []string{"en", "si", "ta"} {
		if _, ok := at[need]; !ok {
			return nil, ErrBadSet
		}
	}
	get := func(row []string, name string) string {
		i, ok := at[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	out := make([]Sentence, 0, len(rows)-1)
	for _, row := range rows[1:] {
		out = append(out, Sentence{ID: get(row, "id"), EN: get(row, "en"), SI: get(row, "si"), TA: get(row, "ta")})
	}
	return out, nil
}

var nameParts = regexp.MustCompile(`[^\pL\pN._-]+`)

// Name is how a test set is recorded on the gate: its file's name and a short hash of its bytes, so
// another version of the set never reads as this one. It takes the form the gate takes.
func Name(file string, raw []byte) string {
	sum := sha256.Sum256(raw)
	base := nameParts.ReplaceAllString(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)), "-")
	if base == "" || !unicode.IsLetter([]rune(base)[0]) && !unicode.IsDigit([]rune(base)[0]) {
		base = "set"
	}
	if r := []rune(base); len(r) > 80 {
		base = string(r[:80])
	}
	return base + ":" + hex.EncodeToString(sum[:])[:12]
}

// Result is what the gate records, and how it came.
type Result struct {
	TestSet     string  `json:"test_set"`
	Source      string  `json:"source"`
	Version     string  `json:"source_version"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	Score       float64 `json:"score"`
	Metric      string  `json:"metric"`
	Sentences   int     `json:"sentences"`
	EvaluatedOn string  `json:"evaluated_on"`
}

// Translator is BUMBLEBEE's client as an evaluation uses it.
type Translator interface {
	Sources(ctx context.Context) (bumblebee.List, error)
	Translate(ctx context.Context, q bumblebee.Request) (bumblebee.Answer, error)
}

// Run sends each sentence with text in both languages to the source, one at a time, and scores what
// comes back. All must answer: a score on part of a set would not be the set's, so any failure gives
// no score and names the reason and the row's ID only. A busy source is waited for once as its
// retry_at says, within the limit, and a block is never tried again (R-PRV-05).
func Run(ctx context.Context, b Translator, set []Sentence, testSet, source, from, to string, now time.Time) (Result, error) {
	list, err := b.Sources(ctx)
	if err != nil {
		return Result{}, errors.New("BUMBLEBEE's source list was not read")
	}
	s, ok := list.Find(source)
	if !ok || !s.Offer(bumblebee.OpTranslate, from, to) {
		return Result{}, ErrNoOffer
	}
	random := make([]byte, 6)
	rand.Read(random)
	prefix := "eval-" + hex.EncodeToString(random)
	var candidates, references []string
	version := s.Version
	for i, row := range set {
		text, reference := row.in(from), row.in(to)
		if text == "" || reference == "" {
			continue
		}
		m := mask.Mask(text)
		q := bumblebee.Request{RequestID: fmt.Sprintf("%s-%d", prefix, i), Source: source, From: from, To: to, Text: m.Text}
		a, err := b.Translate(ctx, q)
		var f *bumblebee.Failure
		if errors.As(err, &f) && f.Reason == "busy" && f.RetryAt != nil {
			if wait := time.Until(*f.RetryAt); wait > 0 && wait < MaxBusyWait {
				t := time.NewTimer(wait)
				select {
				case <-t.C:
				case <-ctx.Done():
					t.Stop()
					return Result{}, ctx.Err()
				}
			}
			a, err = b.Translate(ctx, q)
		}
		if err != nil {
			return Result{}, failed(row, i, err)
		}
		restored, err := m.Restore(a.Text)
		if err != nil {
			return Result{}, fmt.Errorf("row %s: the source lost a masked number", rowName(row, i))
		}
		if a.Version != "" {
			version = a.Version
		}
		candidates = append(candidates, restored)
		references = append(references, reference)
	}
	if len(references) == 0 {
		return Result{}, ErrNoneFound
	}
	return Result{TestSet: testSet, Source: source, Version: version, From: from, To: to, Score: Bleu(candidates, references), Metric: Metric,
		Sentences: len(references), EvaluatedOn: now.In(colombo).Format(time.DateOnly)}, nil
}

// MaxBusyWait is the longest one busy source is waited for.
const MaxBusyWait = 2 * time.Minute

var colombo = func() *time.Location {
	l, err := time.LoadLocation("Asia/Colombo")
	if err != nil {
		return time.FixedZone("Asia/Colombo", 5*3600+1800)
	}
	return l
}()

func rowName(s Sentence, i int) string {
	if s.ID != "" && len(s.ID) <= 40 {
		return s.ID
	}
	return fmt.Sprint(i + 1)
}

func failed(s Sentence, i int, err error) error {
	var f *bumblebee.Failure
	if errors.As(err, &f) {
		return fmt.Errorf("row %s: the source answered %s, so no score", rowName(s, i), f.Reason)
	}
	return fmt.Errorf("row %s: the source could not be reached, so no score", rowName(s, i))
}

// Metric names the score: corpus BLEU over whole words, 0 to 100 (docs/DECISION-LOG.md).
const Metric = "bleu-4 words"

// words is a text's words as compared: normalised as all text is, in lower case, without punctuation.
func words(text string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range strings.ToLower(terms.Normalise(text)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.M, r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func grams(w []string, n int) map[string]int {
	out := map[string]int{}
	for i := 0; i+n <= len(w); i++ {
		out[strings.Join(w[i:i+n], "\x00")]++
	}
	return out
}

// Bleu is corpus BLEU over one reference each, with n-grams up to 4 and the brevity penalty, from 0
// to 100. A set sharing no four words in a row with its references scores 0.
func Bleu(candidates, references []string) float64 {
	const maxN = 4
	var matched, total [maxN + 1]float64
	var candidateLength, referenceLength float64
	for i := range candidates {
		c, r := words(candidates[i]), words(references[i])
		candidateLength += float64(len(c))
		referenceLength += float64(len(r))
		for n := 1; n <= maxN; n++ {
			have, want := grams(c, n), grams(r, n)
			for g, count := range have {
				matched[n] += math.Min(float64(count), float64(want[g]))
				total[n] += float64(count)
			}
		}
	}
	if candidateLength == 0 {
		return 0
	}
	sum := 0.0
	for n := 1; n <= maxN; n++ {
		if matched[n] == 0 || total[n] == 0 {
			return 0
		}
		sum += math.Log(matched[n]/total[n]) / maxN
	}
	brevity := 1.0
	if candidateLength < referenceLength {
		brevity = math.Exp(1 - referenceLength/candidateLength)
	}
	return math.Round(brevity*math.Exp(sum)*10000) / 100
}
