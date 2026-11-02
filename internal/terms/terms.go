// Package terms finds glossary terms in a text and holds the one way text is normalised
// (R-NFR-05). It is pure, and knows nothing of where the terms come from. Offsets are bytes of the
// text as it was written, so what surrounds a term is never touched, however it was typed.
package terms

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Normalise is the one way text is normalised before it is compared or stored (R-NFR-05): the same
// word is always matched, whichever way it was typed. What goes to a source and comes back in an
// answer is left as it was written. Nothing else in ROSETTA calls the normaliser.
func Normalise(s string) string { return norm.NFC.String(s) }

// Term is one published term in one direction.
type Term struct {
	Source string // in the language of the text
	Target string // what stands in the other language
}

// Found is one term in a text. Start and End are bytes of the text as written.
type Found struct {
	Start, End int
	Term       *Term
}

type indexed struct {
	folded string
	term   *Term
}

// Set is the terms of one direction, found by their first word.
type Set struct {
	byFirst map[string][]indexed
	size    int
}

// New makes a set. A term with nothing to find by is left out.
func New(list []Term) *Set {
	s := &Set{byFirst: make(map[string][]indexed, len(list))}
	for i := range list {
		t := &list[i]
		folded, _ := fold(t.Source)
		first := firstWord(folded)
		if first == "" || t.Target == "" {
			continue
		}
		s.byFirst[first] = append(s.byFirst[first], indexed{folded, t})
		s.size++
	}
	for _, l := range s.byFirst {
		// The longest term wins where one holds another.
		slices.SortStableFunc(l, func(a, b indexed) int { return len(b.folded) - len(a.folded) })
	}
	return s
}

// Len is how many terms the set holds.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return s.size
}

// isWord is what a word is made of in the three scripts. Sinhala and Tamil write their vowels as
// marks, so those are part of a word; a case ending joined to a term therefore makes another word,
// which is not this term.
func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) }

func firstWord(folded string) string {
	end := strings.IndexFunc(folded, func(r rune) bool { return !isWord(r) })
	if end < 0 {
		return folded
	}
	return folded[:end]
}

// isGap is a space on one line. A line break is not one: a term never runs across it.
func isGap(r rune) bool {
	return unicode.IsSpace(r) && r != '\n' && r != '\r' && r != '\v' && r != '\f' && r != 0x85 && r != 0x2028 && r != 0x2029
}

// fold is the text normalised, in lower case, with each run of spaces as one. at gives the original
// byte offset of each byte of it, or -1 inside a character, with one more entry for the end. Folding
// never changes what is written anywhere: it is only for comparing.
func fold(text string) (string, []int) {
	var it norm.Iter
	it.InitString(norm.NFC, text)
	b := make([]byte, 0, len(text))
	at := make([]int, 0, len(text)+1)
	pos, gap := 0, false
	for !it.Done() {
		seg := it.Next()
		end := it.Pos()
		first := true
		for len(seg) > 0 {
			r, n := utf8.DecodeRune(seg)
			seg = seg[n:]
			if isGap(r) {
				if gap {
					first = false
					continue
				}
				r, gap = ' ', true
			} else {
				gap = false
			}
			from := len(b)
			b = utf8.AppendRune(b, unicode.ToLower(r))
			for i := from; i < len(b); i++ {
				if i == from && first {
					at = append(at, pos)
				} else {
					at = append(at, -1)
				}
			}
			first = false
		}
		pos = end
	}
	return string(b), append(at, len(text))
}

// Find is every term in the text, in order and apart: the longest where they overlap. A term must
// stand as whole words, on one line. Anything of a stand-in's shape is skipped, so a term never
// matches inside one.
func (s *Set) Find(text string) []Found {
	if s.Len() == 0 {
		return nil
	}
	folded, at := fold(text)
	var out []Found
	for p := 0; p < len(folded); {
		r, n := utf8.DecodeRuneInString(folded[p:])
		if r == '⟦' {
			if end := strings.IndexRune(folded[p:], '⟧'); end >= 0 {
				p += end + len("⟧")
				continue
			}
		}
		if !isWord(r) {
			p += n
			continue
		}
		word := firstWord(folded[p:])
		if f, end, ok := s.match(folded, at, p, word); ok {
			out = append(out, f)
			p = end
			continue
		}
		p += len(word)
	}
	return out
}

func (s *Set) match(folded string, at []int, p int, word string) (Found, int, bool) {
	for _, c := range s.byFirst[word] {
		if !strings.HasPrefix(folded[p:], c.folded) {
			continue
		}
		end := p + len(c.folded)
		if end < len(folded) {
			next, _ := utf8.DecodeRuneInString(folded[end:])
			last, _ := utf8.DecodeLastRuneInString(c.folded)
			if isWord(next) && isWord(last) {
				continue
			}
		}
		if at[p] < 0 || at[end] < 0 {
			continue // inside a character as it was written
		}
		return Found{Start: at[p], End: at[end], Term: c.term}, end, true
	}
	return Found{}, 0, false
}
