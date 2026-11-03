package translate

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Normalise is the one way text is normalised before it is compared or stored
// (R-NFR-05): the same word is always matched, whichever way it was typed. What goes to
// a source and comes back in an answer is left as it was written.
func Normalise(s string) string { return norm.NFC.String(s) }

// Span is a piece of a text, in bytes.
type Span struct {
	Start, End int
}

// Split finds the pieces of a text: its sentences, and longer sentences broken at word
// boundaries so none is over limit code points (R-TRN-04). A line break always ends a
// piece. What lies between the spans, line breaks and spacing at the edges included, is
// left to the caller to keep byte for byte. The rules are plain and work for the three
// scripts; a sentence not split costs nothing but a longer piece.
//
// Text with masking stand-ins goes through here after masking: a stand-in has no space
// in it, and a long word is never cut inside one.
func Split(text string, limit int) []Span {
	var out []Span
	for _, s := range sentences(text) {
		out = append(out, words(text, s, limit)...)
	}
	return out
}

func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// isEnd is a mark that can end a sentence in the three scripts. Sinhala writes the full
// stop, and in older text the kunddaliya; Tamil the full stop, and the danda is seen.
func isEnd(r rune) bool {
	switch r {
	case '.', '!', '?', '…', '।', '॥', '෴':
		return true
	}
	return false
}

// isCloser may follow the mark, and belongs to the sentence it ends.
func isCloser(r rune) bool {
	switch r {
	case '"', '\'', ')', ']', '}', '”', '’', '»', '›':
		return true
	}
	return false
}

// sentences cuts the text at line breaks and at sentence ends.
func sentences(text string) []Span {
	var out []Span
	start := -1
	finish := func(end int) {
		if start < 0 {
			return
		}
		for end > start {
			r, n := utf8.DecodeLastRuneInString(text[start:end])
			if !unicode.IsSpace(r) {
				break
			}
			end -= n
		}
		if end > start {
			out = append(out, Span{start, end})
		}
		start = -1
	}
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		switch {
		case isLineBreak(r):
			finish(i)
			i += n
		case unicode.IsSpace(r):
			i += n
		case isEnd(r):
			if start < 0 {
				start = i
			}
			after := i + n
			for after < len(text) {
				x, m := utf8.DecodeRuneInString(text[after:])
				if !isEnd(x) && !isCloser(x) {
					break
				}
				after += m
			}
			next, _ := utf8.DecodeRuneInString(text[after:])
			if after == len(text) || unicode.IsSpace(next) {
				if endsHere(text, start, i, after) {
					finish(after)
				}
			}
			i = after
		default:
			if start < 0 {
				start = i
			}
			i += n
		}
	}
	finish(len(text))
	return out
}

// abbreviations are the English short forms that do not end a sentence.
var abbreviations = []string{"mr", "mrs", "ms", "dr", "prof", "hon", "rev", "sgt", "capt", "col", "lt", "gen", "st", "mt", "ltd", "pvt", "inc", "vs", "fig", "dept", "govt", "approx", "eg", "ie"}

// endsHere decides whether the mark at [at, after) ends a sentence. Only a plain full
// stop can be something else: an initial, an abbreviation, a list number.
func endsHere(text string, start, at, after int) bool {
	mark := text[at:after]
	if strings.TrimRight(mark, `"')]}”’»›`) != "." {
		return true
	}
	// A small Latin letter next means the sentence goes on.
	rest := strings.TrimLeftFunc(text[after:], func(r rune) bool { return unicode.IsSpace(r) && !isLineBreak(r) })
	next, _ := utf8.DecodeRuneInString(rest)
	if unicode.IsLower(next) && unicode.Is(unicode.Latin, next) {
		return false
	}
	word := text[start:at]
	if i := strings.LastIndexFunc(word, unicode.IsSpace); i >= 0 {
		_, n := utf8.DecodeRuneInString(word[i:])
		word = word[i+n:]
	}
	word = strings.TrimLeft(word, `"'([{“‘«‹`)
	switch {
	case word == "":
		return true
	case letters(word) == 1:
		return false // an initial, as in A. B. Silva
	case strings.EqualFold(word, "no") && unicode.IsDigit(next):
		return false // No. 5
	case slices.Contains(abbreviations, strings.ToLower(word)):
		return false
	case isShortNumber(word):
		return false // a list number
	case isInitialism(word):
		return false // e.g, U.S, and the same in Sinhala and Tamil
	}
	return true
}

func isShortNumber(word string) bool {
	if utf8.RuneCountInString(word) > 2 {
		return false
	}
	for _, r := range word {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// isInitialism is short parts joined by full stops.
func isInitialism(word string) bool {
	parts := strings.Split(word, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if n := letters(p); n == 0 || n > 2 {
			return false
		}
	}
	return true
}

// isJoined is a character that belongs to the letter before it: a vowel sign or other
// mark, or the joiners Sinhala and Tamil use.
func isJoined(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || r == '‌' || r == '‍'
}

// virama silences a consonant in Sinhala (al-lakuna) and Tamil (pulli), so it does not
// make a syllable of its own: எஸ் is written as one initial.
func virama(r rune) bool { return r == '්' || r == '்' }

// letters counts what a reader takes as letters of a word.
func letters(word string) int {
	n := 0
	runes := []rune(word)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		if i+1 < len(runes) && virama(runes[i+1]) && n > 0 {
			continue
		}
		n++
	}
	return n
}

// words breaks a sentence that is over limit: at the last space that fits, and a word
// longer than limit by itself at the last safe place.
func words(text string, s Span, limit int) []Span {
	if limit < 1 {
		limit = 1
	}
	var out []Span
	for s.Start < s.End {
		part := text[s.Start:s.End]
		if utf8.RuneCountInString(part) <= limit {
			return append(out, s)
		}
		fit, lastSpace, count := 0, -1, 0
		for i, r := range part {
			if unicode.IsSpace(r) && i > 0 {
				lastSpace = i // a space just past the limit still ends a piece that fits
			}
			if count == limit {
				fit = i
				break
			}
			count++
		}
		cut := lastSpace
		if lastSpace <= 0 {
			cut = safeCut(part, fit)
		}
		// Any spacing here stays between the pieces.
		next := cut
		for cut > 0 {
			r, n := utf8.DecodeLastRuneInString(part[:cut])
			if !unicode.IsSpace(r) {
				break
			}
			cut -= n
		}
		for next < len(part) {
			r, n := utf8.DecodeRuneInString(part[next:])
			if !unicode.IsSpace(r) {
				break
			}
			next += n
		}
		out = append(out, Span{s.Start, s.Start + cut})
		s.Start += next
	}
	return out
}

// safeCut moves a cut back so it falls neither inside a masking stand-in nor before a
// mark that belongs to the letter in front of it. It always moves on: with no safe place
// before it, it goes forward to the next.
func safeCut(part string, cut int) int {
	safe := func(i int) bool {
		if i <= 0 || i >= len(part) {
			return false
		}
		r, _ := utf8.DecodeRuneInString(part[i:])
		if isJoined(r) {
			return false
		}
		open := strings.LastIndex(part[:i], "⟦")
		return open < 0 || strings.Contains(part[open:i], "⟧")
	}
	for i := cut; i > 0; {
		if safe(i) {
			return i
		}
		_, n := utf8.DecodeLastRuneInString(part[:i])
		i -= n
	}
	for i := cut; i < len(part); {
		if safe(i) {
			return i
		}
		_, n := utf8.DecodeRuneInString(part[i:])
		i += n
	}
	return len(part)
}
