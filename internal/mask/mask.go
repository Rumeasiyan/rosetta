// Package mask replaces identity card numbers (old and new forms) and phone numbers
// with stand-ins before text leaves the shared server, whatever the declared
// sensitivity (R-MSK-02), and puts them back in the answer (R-MSK-03). It is a pure
// package: the link between a stand-in and its value lives only in the Masked value
// the request holds, and is never stored. Masking is a help, not a guarantee:
// anything it does not recognise is sent as written.
//
// A stand-in is ⟦M1⟧, ⟦M2⟧ and so on. The brackets are not a language's, and the
// family leaves room for the glossary's placeholders (E-GLO-04), so one check covers
// both. Each stand-in keeps a map to its original offsets, for E-GRM-04.
package mask

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Kind is what a masked value is.
type Kind string

// The kinds of value masked.
const (
	Card  Kind = "card"
	Phone Kind = "phone"
)

// ErrLost says the source lost or changed a stand-in (R-MSK-03). It holds no text.
var ErrLost = errors.New("mask: a stand-in was lost or changed")

// Item is one masked value and where it sits. Offsets are bytes.
type Item struct {
	Token     string
	Value     string
	Kind      Kind
	Start     int // in Masked.Text
	End       int
	OrigStart int // in the original text
	OrigEnd   int
}

// Masked is text with its values replaced. Its Items are in the order of the text.
type Masked struct {
	Text  string
	Items []Item
}

// How numbers look, on a copy of the text with one byte for each character. Spaces,
// hyphens and dots may separate digits, one at a time, as people write them.
var pattern = func() *regexp.Regexp {
	const sep = `[ .-]?`
	card := `[0-9]{9}[ -]?[VvXx]` + // old form
		`|[0-9]{12}|[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}` // new form
	phone := `0(?:` + sep + `[0-9]){9}` + // 077 123 4567, 011-2345678
		`|\(0[0-9]{2}\)` + sep + `[0-9](?:` + sep + `[0-9]){6}` + // (011) 234 5678
		`|(?:\+|00)?94` + sep + `(?:\(0\)` + sep + `)?[1-9](?:` + sep + `[0-9]){8}` // +94 77 123 4567, 0094771234567
	re := regexp.MustCompile(`(` + card + `)|(` + phone + `)`)
	re.Longest()
	return re
}()

// tokenPattern finds anything of a stand-in's shape, whether ROSETTA's or not.
var tokenPattern = regexp.MustCompile(`⟦[^⟦⟧]*⟧`)

// digitValue reads a digit of any script (Tamil and Sinhala have their own) as its
// value. The decimal digit ranges in Unicode each start at zero.
func digitValue(r rune) (byte, bool) {
	if !unicode.IsDigit(r) {
		return 0, false
	}
	for _, x := range unicode.Digit.R16 {
		if r >= rune(x.Lo) && r <= rune(x.Hi) {
			return byte((r-rune(x.Lo))%10) + '0', true
		}
	}
	for _, x := range unicode.Digit.R32 {
		if r >= rune(x.Lo) && r <= rune(x.Hi) {
			return byte((r-rune(x.Lo))%10) + '0', true
		}
	}
	return 0, false
}

// shadow is the text with one byte for each character: digits as ASCII digits, the
// ways of writing a space or a hyphen as those, and anything else not ASCII as 1. A
// Sinhala or Tamil letter or a case ending joined to a number does not stop it being
// a number; a Latin letter does, as in a reference. offs gives each character's byte
// offset in the text.
func shadow(text string) (out []byte, offs []int) {
	out = make([]byte, 0, len(text))
	offs = make([]int, 0, len(text)+1)
	for i, r := range text {
		offs = append(offs, i)
		switch {
		case r < utf8.RuneSelf:
			out = append(out, byte(r))
		case r == 0xA0 || r == 0x202F || r == 0x2009:
			out = append(out, ' ')
		case r >= 0x2010 && r <= 0x2013:
			out = append(out, '-')
		default:
			if d, ok := digitValue(r); ok {
				out = append(out, d)
			} else {
				out = append(out, 1)
			}
		}
	}
	return out, append(offs, len(text))
}

func wordByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// tokenFor is the stand-in with number n.
func tokenFor(n int) string { return fmt.Sprintf("⟦M%d⟧", n) }

// numberer hands out stand-ins, skipping any the text already holds as written.
type numberer struct {
	text string
	next int
}

func (n *numberer) token() string {
	for {
		n.next++
		if t := tokenFor(n.next); !strings.Contains(n.text, t) {
			return t
		}
	}
}

// Mask replaces every identity card and phone number it recognises.
func Mask(text string) Masked {
	sh, offs := shadow(text)
	num := numberer{text: text}
	var b strings.Builder
	var items []Item
	last := 0 // in text
	for pos := 0; pos < len(sh); {
		m := pattern.FindSubmatchIndex(sh[pos:])
		if m == nil {
			break
		}
		start, end := pos+m[0], pos+m[1]
		if (start > 0 && wordByte(sh[start-1])) || (end < len(sh) && wordByte(sh[end])) {
			pos = start + 1
			continue
		}
		kind := Phone
		if m[2] >= 0 {
			kind = Card
		}
		from, to := offs[start], offs[end]
		b.WriteString(text[last:from])
		token := num.token()
		at := b.Len()
		b.WriteString(token)
		items = append(items, Item{Token: token, Value: text[from:to], Kind: kind, Start: at, End: b.Len(), OrigStart: from, OrigEnd: to})
		last = to
		pos = end
	}
	b.WriteString(text[last:])
	return Masked{Text: b.String(), Items: items}
}

// HasCard says the text held an identity card number (R-MSK-04).
func (m Masked) HasCard() bool {
	return slices.ContainsFunc(m.Items, func(i Item) bool { return i.Kind == Card })
}

// Plain is the text as it was before masking.
func (m Masked) Plain() string {
	var b strings.Builder
	last := 0
	for _, it := range m.Items {
		b.WriteString(m.Text[last:it.Start])
		b.WriteString(it.Value)
		last = it.End
	}
	b.WriteString(m.Text[last:])
	return b.String()
}

// Slice is the part of the masked text from start to end, in bytes, as a Masked of
// its own: its stand-ins numbered from one, so the same sentence with another number
// in it is the same piece, and its offsets its own. A stand-in is never cut: Split
// keeps them whole.
func (m Masked) Slice(start, end int) Masked {
	var inside []Item
	for _, it := range m.Items {
		if it.Start >= start && it.End <= end {
			inside = append(inside, it)
		}
	}
	// What the part holds beyond its own stand-ins is all a new number must avoid.
	var rest strings.Builder
	last := start
	for _, it := range inside {
		rest.WriteString(m.Text[last:it.Start])
		last = it.End
	}
	rest.WriteString(m.Text[last:end])
	num := numberer{text: rest.String()}

	var b strings.Builder
	var items []Item
	last = start
	shift := 0 // how far the original text has moved inside this part
	for _, it := range inside {
		b.WriteString(m.Text[last:it.Start])
		token := num.token()
		at := b.Len()
		b.WriteString(token)
		from := it.Start + shift - start
		items = append(items, Item{Token: token, Value: it.Value, Kind: it.Kind, Start: at, End: b.Len(), OrigStart: from, OrigEnd: from + len(it.Value)})
		shift += len(it.Value) - (it.End - it.Start)
		last = it.End
	}
	b.WriteString(m.Text[last:end])
	return Masked{Text: b.String(), Items: items}
}

// Restore puts the values back into a source's answer. Each stand-in must be there
// exactly once; one lost, changed or repeated, or one the text never held, is
// ErrLost, never an answer without the value (R-MSK-03). They may move, as a
// translation reorders a sentence.
func (m Masked) Restore(answer string) (string, error) {
	known := make(map[string]bool, len(m.Items))
	pairs := make([]string, 0, 2*len(m.Items))
	for _, it := range m.Items {
		known[it.Token] = true
		pairs = append(pairs, it.Token, it.Value)
		if strings.Count(answer, it.Token) != 1 {
			return "", ErrLost
		}
	}
	for _, t := range tokenPattern.FindAllString(answer, -1) {
		if !known[t] && !strings.Contains(m.Text, t) {
			return "", ErrLost
		}
	}
	return strings.NewReplacer(pairs...).Replace(answer), nil
}

// ToOriginal maps a position in Masked.Text to the same place in the original text,
// both in UTF-16 units, the units the editor counts (R-GRM-03). It says false for a
// position inside a stand-in, which has no exact place there: E-GRM-04 leaves such a
// suggestion out rather than moving it.
func (m Masked) ToOriginal(pos int) (int, bool) {
	maskedAt, origAt, last := 0, 0, 0
	for _, it := range m.Items {
		before := units(m.Text[last:it.Start])
		if pos < maskedAt+before {
			break
		}
		maskedAt += before
		origAt += before
		token, value := units(it.Token), units(it.Value)
		if pos == maskedAt {
			return origAt, true
		}
		if pos < maskedAt+token {
			return 0, false
		}
		maskedAt += token
		origAt += value
		last = it.End
	}
	return origAt + pos - maskedAt, true
}

// units is the length of s in UTF-16 units.
func units(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// Units is the length of s in UTF-16 units, the units the editor counts (R-GRM-03).
func Units(s string) int { return units(s) }

// WithoutStandIns is s with anything of a stand-in's shape taken out, whether ROSETTA's or
// not: what of it is the writer's own words.
func WithoutStandIns(s string) string { return tokenPattern.ReplaceAllString(s, "") }
