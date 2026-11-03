package settings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseTakesOnlyTheAllowedRange(t *testing.T) {
	s := CallerIdleDays
	for _, ok := range []string{"1", "90", " 3650 "} {
		if _, err := s.Parse(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "0", "-5", "+5", "3651", "ninety", "9.5", "1e3", "90 days", "<b>", "123456789012"} {
		if _, err := s.Parse(bad); !errors.Is(err, ErrBadValue) {
			t.Errorf("%q was taken: %v", bad, err)
		}
	}
}

// A refusal never repeats the value.
func TestErrorHoldsNoValue(t *testing.T) {
	_, err := CallerIdleDays.Parse("CALLERTEXTMARKER")
	if err == nil || strings.Contains(err.Error(), "CALLERTEXTMARKER") {
		t.Fatalf("%v", err)
	}
}

func TestEverySettingIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range All {
		if seen[s.Name] || s.Word == "" || s.Unit == "" || !strings.HasPrefix(s.Source, "R-") {
			t.Errorf("%+v", s)
		}
		seen[s.Name] = true
		if s.Default < s.Min || s.Default > s.Max {
			t.Errorf("%s: the default is out of its own range", s.Name)
		}
		if _, ok := Find(s.Name); !ok {
			t.Errorf("%s not found", s.Name)
		}
	}
	if _, ok := Find("nothing"); ok {
		t.Error("found nothing")
	}
}

func TestDefaultsAreThePRDs(t *testing.T) {
	v := Defaults()
	if v.Of(RequestLimitSeconds) != 60 || v.Of(CallerIdleDays) != 90 || v.Of(FailureAlertPercent) != 20 {
		t.Fatalf("%v", v)
	}
	// R-TRN-12 and R-TRN-13's figures.
	if v.Of(RunLimitMinutes) != 30 || v.Of(RunsPerSystem) != 5 || v.Of(RunsInAll) != 50 || v.Of(RunUncollectedMinutes) != 10 {
		t.Fatalf("%v", v)
	}
	if (Values{}).Of(CallerIdleDays) != 90 {
		t.Fatal("no value is the default")
	}
}

// A change is in force on the first use after RefreshEvery, with no timer, and the
// wait is inside R-ADM-05's 60 seconds.
func TestCachedReadsAgainOnUseWithinTheLimit(t *testing.T) {
	if RefreshEvery >= 60*time.Second {
		t.Fatal("a change could take longer than 60 seconds")
	}
	clock := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	reads, value := 0, 1
	var fail error
	c := NewCached(0, func(context.Context) (int, error) {
		reads++
		return value, fail
	}, RefreshEvery, nil)
	c.Now = func() time.Time { return clock }
	ctx := context.Background()
	if c.Get(ctx) != 1 || reads != 1 {
		t.Fatalf("first use reads: %d", reads)
	}
	value = 2
	clock = clock.Add(RefreshEvery - time.Second)
	if c.Get(ctx) != 1 || reads != 1 {
		t.Fatal("read too soon")
	}
	clock = clock.Add(time.Second)
	if c.Get(ctx) != 2 || reads != 2 {
		t.Fatalf("not read again: %d", reads)
	}
	// A failed read keeps the last good value, and waits.
	fail = errors.New("no database")
	value = 3
	clock = clock.Add(RefreshEvery)
	if c.Get(ctx) != 2 {
		t.Fatal("a failed read lost the value")
	}
	if c.Get(ctx) != 2 || reads != 3 {
		t.Fatalf("%d reads", reads)
	}
}
