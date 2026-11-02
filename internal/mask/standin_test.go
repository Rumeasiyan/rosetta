package mask_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/mask"
	"github.com/Rumeasiyan/rosetta/internal/standin"
)

// R-MSK-03, against BUMBLEBEE's stand-in: stand-ins survive when it keeps them, and
// losing or changing one is ErrLost. What reaches it never holds the value.
func TestStandInsAgainstBumblebeesStandIn(t *testing.T) {
	s := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { s.Close(); ts.Close() })
	post := func(path, body, key string) []byte {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return b
	}
	const value = "0771234567"
	m := mask.Mask("Call " + value + " today.")
	send := func() string {
		b, _ := json.Marshal(map[string]string{"request_id": "r-1", "source": "standin-translate", "operation": "translate", "from": "en", "to": "ta", "text": m.Text})
		var out struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(post("/bumblebee/v1/requests", string(b), standin.BumblebeeKey), &out); err != nil {
			t.Fatal(err)
		}
		return out.Text
	}
	mode := func(name string, params map[string]string) {
		b, _ := json.Marshal(map[string]any{"mode": name, "params": params})
		res, err := http.Post(ts.URL+"/control/bumblebee/mode", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	got, err := m.Restore(send())
	if err != nil || got != "[ta] Call "+value+" today." {
		t.Fatal(got, err)
	}
	mode("lose-masking", nil)
	if _, err := m.Restore(send()); !errors.Is(err, mask.ErrLost) {
		t.Fatalf("lost: %v", err)
	}
	mode("change-term", map[string]string{"from": "⟦M1⟧", "to": "⟦M 1⟧"})
	if _, err := m.Restore(send()); !errors.Is(err, mask.ErrLost) {
		t.Fatalf("changed: %v", err)
	}
	res, err := http.Get(ts.URL + "/control/bumblebee/received")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	seen, _ := io.ReadAll(res.Body)
	if bytes.Contains(seen, []byte(value)) || !bytes.Contains(seen, []byte("⟦M1⟧")) {
		t.Fatalf("what reached the stand-in: %s", seen)
	}
}

func TestTermsStandInBesideValues(t *testing.T) {
	m := mask.Mask("Ring 0771234567 about the Board and 0719999999.")
	at := strings.Index(m.Text, "Board")
	p := m.Protect([]mask.Protection{{Start: at, End: at + len("Board"), Answer: "சபை"}})
	if p.Text != "Ring ⟦M1⟧ about the ⟦G1⟧ and ⟦M2⟧." || p.Plain() != "Ring 0771234567 about the Board and 0719999999." {
		t.Fatalf("%q %q", p.Text, p.Plain())
	}
	if len(p.Terms()) != 1 || p.Terms()[0].Answer != "சபை" || p.Terms()[0].Value != "Board" {
		t.Fatalf("%+v", p.Terms())
	}
	u := p.Unprotected()
	if u.Text != m.Text || len(u.Items) != 2 {
		t.Fatalf("%+v", u)
	}
	if got, err := u.Restore("⟦M2⟧ ⟦M1⟧ Board"); err != nil || got != "0719999999 0771234567 Board" {
		t.Fatalf("%q %v", got, err)
	}
	// A part numbers each family from one.
	s := p.Slice(strings.Index(p.Text, "⟦G1⟧"), len(p.Text))
	if s.Text != "⟦G1⟧ and ⟦M1⟧." || s.Plain() != "Board and 0719999999." {
		t.Fatalf("%q %q", s.Text, s.Plain())
	}
	if none := m.Protect(nil); none.Text != m.Text {
		t.Fatal(none.Text)
	}
}
