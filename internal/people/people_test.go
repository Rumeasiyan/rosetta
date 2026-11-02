package people

import (
	"errors"
	"testing"
	"time"
)

func TestParseRequest(t *testing.T) {
	today := time.Date(2030, 1, 15, 0, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		role, language, ends string
		want                 error
	}{
		"administrator":          {Administrator, "", "", nil},
		"keeper":                 {GlossaryKeeper, "ta", "", nil},
		"reviewer with end":      {MemoryReviewer, "si", "2030-01-15", nil},
		"system owner":           {"system owner", "", "", ErrBadRole},
		"blank role":             {"", "", "", ErrBadRole},
		"administrator language": {Administrator, "en", "", ErrBadLanguage},
		"keeper no language":     {GlossaryKeeper, "", "", ErrBadLanguage},
		"keeper unknown":         {GlossaryKeeper, "fr", "", ErrBadLanguage},
		"past date":              {GlossaryKeeper, "ta", "2030-01-14", ErrBadDate},
		"not a date":             {GlossaryKeeper, "ta", "soon", ErrBadDate},
	} {
		got, err := ParseRequest(c.role, c.language, c.ends, today)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
		if err == nil && got.Role != c.role {
			t.Errorf("%s: %+v", name, got)
		}
	}
	got, err := ParseRequest(GlossaryKeeper, "ta", "2030-02-01", today)
	if err != nil || *got.Language != "ta" || got.EndsOn.Format(time.DateOnly) != "2030-02-01" {
		t.Fatalf("%v %+v", err, got)
	}
}
