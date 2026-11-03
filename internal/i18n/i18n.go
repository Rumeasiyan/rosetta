// Package i18n holds every word ROSETTA's screens show, in Sinhala, Tamil and
// English (R-NFR-05), in the one message file messages.go. A screen never carries
// its own text: it asks T for a key, and a test fails if any key lacks a language
// or any screen names a key that does not exist.
//
// The Sinhala and Tamil texts are best effort and a person must check them before
// Go live (docs/DECISION-LOG.md).
package i18n

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Lang is one of the three languages.
type Lang string

// The three languages.
const (
	SI Lang = "si"
	TA Lang = "ta"
	EN Lang = "en"
)

// Langs is every language, in the order the switch shows them.
var Langs = []Lang{SI, TA, EN}

// Parse returns the language s names, or false.
func Parse(s string) (Lang, bool) {
	if l := Lang(strings.ToLower(strings.TrimSpace(s))); slices.Contains(Langs, l) {
		return l, true
	}
	return "", false
}

// Name is each language's name in its own script.
func Name(l Lang) string {
	switch l {
	case SI:
		return "සිංහල"
	case TA:
		return "தமிழ்"
	}
	return "English"
}

// text is one message in the three languages.
type text struct{ EN, SI, TA string }

func (t text) in(l Lang) string {
	switch l {
	case SI:
		return t.SI
	case TA:
		return t.TA
	}
	return t.EN
}

// T is the text for key in lang. A missing key shows as itself, so it is seen on
// the screen and caught by the tests, never shown as blank.
func T(lang Lang, key string) string {
	if m, ok := messages[key]; ok {
		if s := m.in(lang); s != "" {
			return s
		}
	}
	return key
}

// Has reports whether key is a message.
func Has(key string) bool { _, ok := messages[key]; return ok }

// Keys lists every message key (for tests).
func Keys() []string {
	keys := make([]string, 0, len(messages))
	for k := range messages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Missing lists the languages key lacks (for tests).
func Missing(key string) []Lang {
	var out []Lang
	for _, l := range Langs {
		if strings.TrimSpace(messages[key].in(l)) == "" {
			out = append(out, l)
		}
	}
	return out
}

// Accepted reads an Accept-Language header and returns the languages ROSETTA has,
// most wanted first. "si-LK" counts as "si"; q=0 means "not this one".
func Accepted(header string) []Lang {
	type choice struct {
		lang Lang
		q    float64
	}
	var cs []choice
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		base, _, _ := strings.Cut(tag, "-")
		l, ok := Parse(base)
		if !ok {
			continue
		}
		q := 1.0
		if v, found := strings.CutPrefix(strings.TrimSpace(params), "q="); found {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		if q > 0 {
			cs = append(cs, choice{l, q})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].q > cs[j].q })
	var out []Lang
	for _, c := range cs {
		if !slices.Contains(out, c.lang) {
			out = append(out, c.lang)
		}
	}
	return out
}

// Choose is the language for a request: the person's pick if they made one, else
// the browser's first language ROSETTA has, else English.
func Choose(picked string, accepted []Lang) Lang {
	if l, ok := Parse(picked); ok {
		return l
	}
	if len(accepted) > 0 {
		return accepted[0]
	}
	return EN
}
