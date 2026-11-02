package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

// E-GRM-07, as far as it goes without a Document Server: the stock plug-in's own parse, field
// by field, from sdkjs-plugins/content/languagetool/scripts/langTool.js in ONLYOFFICE's public
// plug-ins repository (docs/plugin-findings.md). Watching it run is still to do.

// jsSlice is String.prototype.slice on a text, in UTF-16 units, as the plug-in cuts.
func jsSlice(text string, from, to int) string {
	return string(utf16.Decode(utf16.Encode([]rune(text))[from:to]))
}

// capturedCheck is a check as the plug-in's $.ajax sends it: a form POST, with jQuery's way of
// writing a space, each line break doubled by the plug-in, and the language `auto` it starts
// with. The text is invented.
func capturedCheck(origin string) *http.Request {
	const body = "text=%F0%9F%98%80+We+recieve+letters+at+077+123+4567.%0A%0AThey+seperate+them.&language=auto&enabledOnly=false"
	r := httptest.NewRequest(http.MethodPost, "https://rosetta.epc.test"+checkPath, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "*/*")
	r.Header.Set("Origin", origin)
	r.Header.Set("Referer", origin+"/")
	r.Header.Set(accountHeader, "officer-1")
	return r
}

func TestWhatThePluginParsesIsAllThere(t *testing.T) {
	b := newBench(t)
	withChecks(t, b)
	withOrigins(t, b)
	const sent = "😀 We recieve letters at 077 123 4567.\n\nThey seperate them."

	// getLanguages(), then init(): name, code and longCode of each, and no two of one name.
	r := httptest.NewRequest(http.MethodGet, languagesPath, nil)
	r.Header.Set("Origin", listedOrigin)
	r.Header.Set(accountHeader, "officer-1")
	rec := b.send(r)
	wantCORS(t, "languages", rec)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	var languages []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &languages); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range languages {
		for _, field := range []string{"name", "code", "longCode"} {
			if v, ok := l[field].(string); !ok || v == "" {
				t.Fatalf("%s of %v", field, l)
			}
		}
		names = append(names, l["name"].(string))
	}
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	if len(slices.Compact(sorted)) != len(names) {
		t.Fatalf("the plug-in drops a repeated name: %v", names)
	}

	// checkText(), then its .done: $.ajax parses JSON by the Content-Type.
	rec = b.send(capturedCheck(listedOrigin))
	wantCORS(t, "check", rec)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	var out struct {
		Language *struct {
			Name *string `json:"name"`
		} `json:"language"`
		Matches *[]struct {
			Message      *string `json:"message"`
			ShortMessage *string `json:"shortMessage"`
			Offset       *int    `json:"offset"`
			Length       *int    `json:"length"`
			Replacements *[]struct {
				Value *string `json:"value"`
			} `json:"replacements"`
			Context *struct {
				Text   *string `json:"text"`
				Offset *int    `json:"offset"`
				Length *int    `json:"length"`
			} `json:"context"`
			Rule *struct {
				ID        *string `json:"id"`
				IssueType *string `json:"issueType"`
				Category  *struct {
					ID   *string `json:"id"`
					Name *string `json:"name"`
				} `json:"category"`
			} `json:"rule"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// changeLangInSelect(oResponse.language.name) looks the name up in the list.
	if out.Language == nil || out.Language.Name == nil || !slices.Contains(names, *out.Language.Name) {
		t.Fatalf("language: %s", rec.Body)
	}
	if out.Matches == nil || len(*out.Matches) != 2 {
		t.Fatalf("matches: %s", rec.Body)
	}
	for i, want := range []string{"recieve", "seperate"} {
		m := (*out.Matches)[i]
		if m.Message == nil || *m.Message == "" || m.ShortMessage == nil || m.Offset == nil || m.Length == nil || m.Replacements == nil || m.Context == nil || m.Context.Text == nil || m.Context.Offset == nil || m.Context.Length == nil {
			t.Fatalf("%d: a field is missing: %s", i, rec.Body)
		}
		// setTextWithErrors: txt.slice(offset, offset + length).
		if got := jsSlice(sent, *m.Offset, *m.Offset+*m.Length); got != want {
			t.Errorf("%d marks %q", i, got)
		}
		// parseResult: context.text.slice(offset, offset + length) is the same word.
		if got := jsSlice(*m.Context.Text, *m.Context.Offset, *m.Context.Offset+*m.Context.Length); got != want {
			t.Errorf("%d: context marks %q", i, got)
		}
		// el.replacements.slice(0, 5), then elem.value on each.
		if len(*m.Replacements) == 0 || len(*m.Replacements) > 3 {
			t.Errorf("%d: %d replacements", i, len(*m.Replacements))
		}
		for _, x := range *m.Replacements {
			if x.Value == nil || *x.Value == "" {
				t.Errorf("%d: no value: %s", i, rec.Body)
			}
		}
		if m.Rule == nil || m.Rule.ID == nil || m.Rule.IssueType == nil || m.Rule.Category == nil || m.Rule.Category.ID == nil || m.Rule.Category.Name == nil {
			t.Fatalf("%d: rule: %s", i, rec.Body)
		}
		if *m.Rule.IssueType != "misspelling" || *m.Rule.Category.ID != "SPELLING" {
			t.Errorf("%d: %s %s", i, *m.Rule.IssueType, *m.Rule.Category.ID)
		}
	}

	// correctMistakesPosition: taking the doubled breaks off leaves the second word true to the
	// text in the editor, which has single ones.
	shown := strings.ReplaceAll(sent, "\n\n", "\n")
	second := (*out.Matches)[1]
	if got := jsSlice(shown, *second.Offset-1, *second.Offset-1+*second.Length); got != "seperate" {
		t.Errorf("after its correction: %q", got)
	}

	// With nothing to say, oResponse.matches.map still works, and no language is needed.
	r = capturedCheck(listedOrigin)
	r.Body = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("text=Nothing+wrong+here.&language=en-GB&enabledOnly=false")).Body
	rec = b.send(r)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"matches":[]`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	// .fail(function(e) { text: e.responseText }): a refusal is words, and readable.
	r = capturedCheck(listedOrigin)
	r.Header.Del(accountHeader)
	rec = b.send(r)
	wantCORS(t, "refusal", rec)
	if rec.Code != 401 || rec.Body.Len() == 0 || strings.Contains(rec.Body.String(), "{") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
