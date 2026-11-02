package standin

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// BumblebeeKey is the only live key BUMBLEBEE's stand-in takes; BumblebeeOffKey
// is a key it knows as turned off.
const (
	BumblebeeKey    = "bb_standin"
	BumblebeeOffKey = "bb_turned_off"
)

// The first mode is normal. After it, the eight reasons in CALLERS.md, then what
// E-ENB-03 asks for beyond them.
var bumblebeeModes = []string{
	"normal",
	"busy", "blocked", "changed", "timeout", "too-long", "unsupported", "source-off", "unavailable",
	"lose-masking", "change-term", "slow", "silent",
}

// reasonStatus is each failure reason's status in CALLERS.md.
var reasonStatus = map[string]int{
	"busy": 503, "blocked": 502, "changed": 502, "timeout": 504,
	"too-long": 413, "unsupported": 422, "source-off": 503, "unavailable": 503,
}

// reasonRetry is the reasons that carry retry_at.
var reasonRetry = map[string]bool{"busy": true, "blocked": true, "changed": true}

// defaultMaxText stands for BUMBLEBEE's setting; the control sets max_text_chars.
const defaultMaxText = 5000

type bbSource struct {
	name, version string
	grammar       bool
}

var bbSources = []bbSource{
	{name: "standin-translate", version: "1"},
	{name: "standin-grammar", version: "2", grammar: true},
	{name: "standin-translate-b", version: "1"},
}

var bbRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// defaultMasking finds a masking stand-in in the form internal/mask gives them; the
// control's "match" replaces it.
const defaultMasking = `⟦[^⟧]*⟧`

func (s *Server) mountBumblebee(mux *http.ServeMux) {
	mux.HandleFunc("POST /bumblebee/v1/requests", s.serve("bumblebee", s.bbRequest))
	mux.HandleFunc("GET /bumblebee/v1/sources", s.serve("bumblebee", s.bbSources))
	mux.HandleFunc("GET /bumblebee/health", s.serve("bumblebee", s.bbHealth))
}

// bbModeFor is the mode for one source: the control's param "source", when set,
// keeps a failure mode to that source and leaves the others normal, so fallback can
// be walked.
func bbModeFor(mode string, p map[string]string, source string) string {
	if only := p["source"]; only != "" && only != source {
		return "normal"
	}
	return mode
}

// bbKey answers a refused key and reports whether the call may go on.
func bbKey(w http.ResponseWriter, r *http.Request) bool {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case !ok || key == "":
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no_key"})
	case key == BumblebeeOffKey:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "key_turned_off"})
	case key != BumblebeeKey:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unknown_key"})
	default:
		return true
	}
	return false
}

func (s *Server) bbSources(w http.ResponseWriter, r *http.Request) {
	if !bbKey(w, r) {
		return
	}
	mode, p := s.mode("bumblebee")
	var list []map[string]any
	for _, src := range bbSources {
		state, rest := "on", ""
		switch bbModeFor(mode, p, src.name) {
		case "blocked":
			state, rest = "resting-block", retryAt(p)
		case "changed":
			state, rest = "resting-change", retryAt(p)
		case "source-off":
			state = "off"
		}
		e := map[string]any{"name": src.name, "version": src.version, "state": state}
		if rest != "" {
			e["rest_until"] = rest
		}
		if src.grammar {
			e["offers"] = []map[string]string{{"operation": "grammar", "from_language": "en"}}
			e["scores"] = map[string]int{"en": 72}
		} else {
			var offers []map[string]string
			for _, from := range []string{"si", "ta", "en"} {
				for _, to := range []string{"si", "ta", "en"} {
					if from != to {
						offers = append(offers, map[string]string{"operation": "translate", "from_language": from, "to_language": to})
					}
				}
			}
			e["offers"] = offers
		}
		list = append(list, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"limits":  map[string]int{"max_text_chars": intParam(p, "max_text_chars", defaultMaxText)},
		"sources": list,
	})
}

func (s *Server) bbHealth(w http.ResponseWriter, _ *http.Request) {
	mode, _ := s.mode("bumblebee")
	status, ready := http.StatusOK, true
	if mode == "unavailable" {
		status, ready = http.StatusServiceUnavailable, false
	}
	var sources []map[string]string
	for _, src := range bbSources {
		sources = append(sources, map[string]string{"name": src.name, "state": "on"})
	}
	writeJSON(w, status, map[string]any{"ready": ready, "sources": sources})
}

func retryAt(p map[string]string) string {
	return time.Now().UTC().Add(time.Duration(intParam(p, "retry_seconds", 60)) * time.Second).Format(time.RFC3339)
}

func (s *Server) bbRequest(w http.ResponseWriter, r *http.Request) {
	if !bbKey(w, r) {
		return
	}
	started := time.Now()
	mode, p := s.mode("bumblebee")
	var in struct {
		RequestID string `json:"request_id"`
		Source    string `json:"source"`
		Operation string `json:"operation"`
		From      string `json:"from"`
		To        string `json:"to"`
		Language  string `json:"language"`
		Text      string `json:"text"`
	}
	answer := map[string]any{}
	fail := func(reason, why string) {
		f := map[string]string{"reason": reason, "explanation": why}
		if reasonRetry[reason] {
			f["retry_at"] = retryAt(p)
		}
		answer["failure"] = f
		answer["took_ms"] = time.Since(started).Milliseconds()
		writeJSON(w, reasonStatus[reason], answer)
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail("unsupported", "the request is malformed")
		return
	}
	answer["request_id"] = in.RequestID
	answer["source"] = in.Source
	var src *bbSource
	for i := range bbSources {
		if bbSources[i].name == in.Source {
			src = &bbSources[i]
		}
	}
	if src != nil {
		answer["version"] = src.version
	}
	mode = bbModeFor(mode, p, in.Source)
	if _, isFailure := reasonStatus[mode]; isFailure {
		fail(mode, "the stand-in was told to fail this way")
		return
	}
	langs := map[string]bool{"si": true, "ta": true, "en": true}
	switch {
	case !bbRequestID.MatchString(in.RequestID):
		fail("unsupported", "request_id is not of the allowed form")
		return
	case src == nil:
		fail("unsupported", "no such source")
		return
	case in.Operation == "translate" && (src.grammar || !langs[in.From] || !langs[in.To] || in.From == in.To):
		fail("unsupported", "not a direction this source offers")
		return
	case in.Operation == "grammar" && (!src.grammar || in.Language != "en"):
		fail("unsupported", "not a language this source offers")
		return
	case in.Operation != "translate" && in.Operation != "grammar":
		fail("unsupported", "no such operation")
		return
	case utf8.RuneCountInString(in.Text) > intParam(p, "max_text_chars", defaultMaxText):
		fail("too-long", "the text is over the limit in the source list")
		return
	}
	answer["took_ms"] = time.Since(started).Milliseconds()
	if in.Operation == "grammar" {
		answer["suggestions"] = bbGrammar(in.Text)
		writeJSON(w, http.StatusOK, answer)
		return
	}
	// A plain mark on the text, with line breaks and masking stand-ins kept, so
	// a test can tell it from the original (R-TRN-09).
	text := "[" + in.To + "] " + in.Text
	switch mode {
	case "lose-masking":
		match := p["match"]
		if match == "" {
			match = defaultMasking
		}
		re, err := regexp.Compile(match)
		if err != nil {
			fail("unavailable", "the control's match is not a regular expression")
			return
		}
		text = re.ReplaceAllString(text, "")
	case "change-term":
		text = strings.ReplaceAll(text, p["from"], p["to"])
	}
	answer["text"] = text
	writeJSON(w, http.StatusOK, answer)
}

// bbGrammar finds a few invented mistakes, with positions in UTF-16 units of the
// text as sent.
func bbGrammar(text string) []map[string]any {
	found := []map[string]any{}
	for _, m := range []struct{ wrong, right string }{{"recieve", "receive"}, {"seperate", "separate"}} {
		from := 0
		for {
			i := strings.Index(text[from:], m.wrong)
			if i < 0 {
				break
			}
			at := from + i
			found = append(found, map[string]any{
				"start":        len(utf16.Encode([]rune(text[:at]))),
				"length":       len(utf16.Encode([]rune(m.wrong))),
				"message":      "Possible spelling mistake",
				"replacements": []string{m.right},
				"category":     "spelling",
			})
			from = at + len(m.wrong)
		}
	}
	return found
}
