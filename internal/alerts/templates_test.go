package alerts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every notice ROSETTA sends has its template in deploy/hedwig, under its own name with hyphens: HEDWIG refuses
// a type with no template, so a kind added without one would never reach anybody.
func TestEveryNoticeHasItsTemplate(t *testing.T) {
	want := map[string]bool{"glossary_proposal": true, "glossary_import": true}
	for _, kind := range Kinds {
		if templates[kind] == "" {
			t.Fatalf("%s has no notice type", kind)
		}
		want[templates[kind]] = true
	}
	files, err := filepath.Glob("../../deploy/hedwig/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(want) {
		t.Errorf("%d files for %d notice types", len(files), len(want))
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var tpl struct {
			Type  string `json:"notice_type"`
			Texts map[string]struct {
				Subject string `json:"subject"`
				Body    string `json:"email_body"`
			} `json:"texts"`
		}
		if err := json.Unmarshal(raw, &tpl); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !want[tpl.Type] || filepath.Base(f) != strings.ReplaceAll(tpl.Type, "_", "-")+".json" || len(tpl.Texts) != 3 {
			t.Errorf("%s: %+v", f, tpl)
		}
	}
}
