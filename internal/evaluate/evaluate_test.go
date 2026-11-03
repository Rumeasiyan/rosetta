package evaluate_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/evaluate"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/standin"
)

func TestReadBothShapes(t *testing.T) {
	got, err := evaluate.Read("a.tsv", strings.NewReader("id\ten\tsi\tta\tsource\tnotes\n1\tOne.\tඑක.\tஒன்று.\tmade up\t\n2\tTwo.\t\tஇரண்டு.\tmade up\t\n"))
	if err != nil || len(got) != 2 || got[0].TA != "ஒன்று." || got[1].SI != "" {
		t.Fatal(got, err)
	}
	got, err = evaluate.Read("a.jsonl", strings.NewReader(`{"id":"1","en":"One.","ta":"ஒன்று."}`+"\n\n"+`{"id":"2","en":"Two.","si":"දෙක."}`+"\n"))
	if err != nil || len(got) != 2 || got[1].SI != "දෙක." {
		t.Fatal(got, err)
	}
	for name, body := range map[string]string{"a.tsv": "id\ten\tnothing\n1\tOne.\tx\n", "b.jsonl": "not json\n", "c.tsv": ""} {
		if _, err := evaluate.Read(name, strings.NewReader(body)); !errors.Is(err, evaluate.ErrBadSet) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The name changes with the bytes, and takes the gate's form (sources.ParseEvaluation).
func TestNameFitsTheGate(t *testing.T) {
	a, b := evaluate.Name("dev/test-set/invented.tsv", []byte("one")), evaluate.Name("dev/test-set/invented.tsv", []byte("two"))
	if a == b || !strings.HasPrefix(a, "invented:") {
		t.Fatal(a, b)
	}
	today := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{a, evaluate.Name("weird name (1).tsv", nil), evaluate.Name("(((.tsv", nil), evaluate.Name(strings.Repeat("x", 300)+".tsv", nil)} {
		if _, err := sources.ParseEvaluation(name, "1", "2030-01-01", today); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestBleu(t *testing.T) {
	same := []string{"the office is open on weekdays", "please send the form to the secretary"}
	if got := evaluate.Bleu(same, same); got != 100 {
		t.Fatal(got)
	}
	if got := evaluate.Bleu([]string{"nothing alike here at all"}, []string{"the office is open on weekdays"}); got != 0 {
		t.Fatal(got)
	}
	part := evaluate.Bleu([]string{"the office is open on some days"}, []string{"the office is open on weekdays"})
	if part <= 0 || part >= 100 {
		t.Fatal(part)
	}
	short := evaluate.Bleu([]string{"the office is open"}, []string{"the office is open on weekdays"})
	if short >= 100 {
		t.Fatalf("a short answer pays for it: %v", short)
	}
	// The same words however written: Unicode forms, case, punctuation.
	if got := evaluate.Bleu([]string{"THE OFFICE, is open: on weekdays."}, []string{"the office is open on weekdays"}); got != 100 {
		t.Fatal(got)
	}
	tamil := "அலுவலகம் வார நாட்களில் திறந்திருக்கும் என்று சொன்னார்"
	if got := evaluate.Bleu([]string{tamil}, []string{tamil + "."}); got != 100 {
		t.Fatalf("Tamil vowel signs stay in the word: %v", got)
	}
	if got := evaluate.Bleu([]string{""}, []string{"a"}); got != 0 {
		t.Fatal(got)
	}
}

func stand(t *testing.T) (*standin.Server, bumblebee.Client) {
	t.Helper()
	sd := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(sd.Handler())
	t.Cleanup(func() { sd.Close(); ts.Close() })
	return sd, bumblebee.Client{URL: ts.URL + "/bumblebee", Key: standin.BumblebeeKey}
}

// The command's work against the stand-in: it masks, it gives what the gate records.
func TestRunAgainstTheStandIn(t *testing.T) {
	_, c := stand(t)
	raw, err := os.ReadFile("../../dev/test-set/invented.tsv")
	if err != nil {
		t.Fatal(err)
	}
	set, err := evaluate.Read("invented.tsv", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	name := evaluate.Name("invented.tsv", raw)
	at := time.Date(2030, 1, 1, 20, 0, 0, 0, time.UTC) // already the 2nd in Sri Lanka
	r, err := evaluate.Run(context.Background(), c, set, name, "standin-translate", "en", "ta", at)
	if err != nil {
		t.Fatal(err)
	}
	if r.Sentences != 10 || r.Version != "1" || r.EvaluatedOn != "2030-01-02" || r.Score != 0 || r.Metric == "" || r.TestSet != name {
		t.Fatalf("%+v", r)
	}
	if out, _ := json.Marshal(r); strings.Contains(string(out), "office") {
		t.Fatalf("a result holds test text: %s", out)
	}
	if _, err := sources.ParseEvaluation(r.TestSet, "1", "2030-01-01", time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := evaluate.Run(context.Background(), c, set, name, "no-such-site", "en", "ta", at); !errors.Is(err, evaluate.ErrNoOffer) {
		t.Fatal(err)
	}
	if _, err := evaluate.Run(context.Background(), c, nil, name, "standin-translate", "en", "ta", at); !errors.Is(err, evaluate.ErrNoneFound) {
		t.Fatal(err)
	}
}

// fake answers from a table of what it was told, and fails when told to.
type fake struct {
	answer func(q bumblebee.Request) (bumblebee.Answer, error)
	sent   []string
}

func (f *fake) Sources(context.Context) (bumblebee.List, error) {
	return bumblebee.List{Sources: []bumblebee.Source{{Name: "site", Version: "7", State: bumblebee.StateOn, Offers: []bumblebee.Offer{{Operation: bumblebee.OpTranslate, From: "en", To: "ta"}}}}}, nil
}

func (f *fake) Translate(_ context.Context, q bumblebee.Request) (bumblebee.Answer, error) {
	f.sent = append(f.sent, q.Text)
	return f.answer(q)
}

func TestAPerfectSourceScoresAHundredAndNumbersAreMasked(t *testing.T) {
	const reference = "ஒரு நீண்ட சோதனை வாக்கியம் 0771234567 இங்கே முடிகிறது"
	set := []evaluate.Sentence{{ID: "1", EN: "A long test sentence 0771234567 ends here", TA: reference}}
	f := &fake{answer: func(bumblebee.Request) (bumblebee.Answer, error) {
		return bumblebee.Answer{Text: strings.ReplaceAll(reference, "0771234567", "⟦M1⟧"), Source: "site", Version: "9"}, nil
	}}
	r, err := evaluate.Run(context.Background(), f, set, "n", "site", "en", "ta", time.Now())
	if err != nil || r.Score != 100 || r.Version != "9" {
		t.Fatalf("%+v %v", r, err)
	}
	if strings.Contains(f.sent[0], "0771234567") {
		t.Fatal("a number left unmasked")
	}
}

// Any failure gives no score, and the error holds no text.
func TestAFailureGivesNoScore(t *testing.T) {
	const marker = "SETTEXTMARKER"
	set := []evaluate.Sentence{{ID: "1", EN: marker, TA: marker}, {ID: "2", EN: marker, TA: marker}}
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"blocked", &bumblebee.Failure{Reason: "blocked"}, "answered blocked"},
		{"silence", errors.New("no answer " + marker), "could not be reached"},
	} {
		f := &fake{answer: func(bumblebee.Request) (bumblebee.Answer, error) { return bumblebee.Answer{}, c.err }}
		_, err := evaluate.Run(context.Background(), f, set, "n", "site", "en", "ta", time.Now())
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), marker) {
			t.Errorf("%s: %v", c.name, err)
		}
		if len(f.sent) != 1 {
			t.Errorf("%s: went on after a failure: %d", c.name, len(f.sent))
		}
	}
	// A masked number lost is a failure, with no answer scored.
	set = []evaluate.Sentence{{ID: "3", EN: "Call 0771234567 now", TA: "x"}}
	f := &fake{answer: func(bumblebee.Request) (bumblebee.Answer, error) { return bumblebee.Answer{Text: "nothing"}, nil }}
	if _, err := evaluate.Run(context.Background(), f, set, "n", "site", "en", "ta", time.Now()); err == nil || !strings.Contains(err.Error(), "masked number") {
		t.Fatal(err)
	}
	// Busy is waited for once as it says.
	tries := 0
	soon := time.Now().Add(50 * time.Millisecond)
	f = &fake{answer: func(bumblebee.Request) (bumblebee.Answer, error) {
		tries++
		if tries == 1 {
			return bumblebee.Answer{}, &bumblebee.Failure{Reason: "busy", RetryAt: &soon}
		}
		return bumblebee.Answer{Text: "fine"}, nil
	}}
	set = []evaluate.Sentence{{ID: "4", EN: "Fine", TA: "fine"}}
	if _, err := evaluate.Run(context.Background(), f, set, "n", "site", "en", "ta", time.Now()); err != nil || tries != 2 {
		t.Fatal(err, tries)
	}
}
