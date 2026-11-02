package mask

import (
	"errors"
	"strings"
	"testing"
)

// Values are invented. The first of each pair is the text, the second what must be
// masked in it, in order.
var found = []struct {
	name, text string
	want       []string
	kinds      []Kind
}{
	{"old card", "Card 123456789V was seen.", []string{"123456789V"}, []Kind{Card}},
	{"old card lower x", "Card 987654321x, seen.", []string{"987654321x"}, []Kind{Card}},
	{"old card with a space", "Card 123456789 V was seen.", []string{"123456789 V"}, []Kind{Card}},
	{"new card", "Card 199012345678.", []string{"199012345678"}, []Kind{Card}},
	{"new card grouped", "Card 1990-1234-5678 here", []string{"1990-1234-5678"}, []Kind{Card}},
	{"mobile", "Call 0771234567 today", []string{"0771234567"}, []Kind{Phone}},
	{"mobile spaced", "Call 077 123 4567 today", []string{"077 123 4567"}, []Kind{Phone}},
	{"mobile hyphens", "Call 077-123-4567.", []string{"077-123-4567"}, []Kind{Phone}},
	{"mobile dots", "Call 077.123.4567.", []string{"077.123.4567"}, []Kind{Phone}},
	{"landline", "Office: 026-2221234", []string{"026-2221234"}, []Kind{Phone}},
	{"landline brackets", "Office (026) 222 1234.", []string{"(026) 222 1234"}, []Kind{Phone}},
	{"plus", "Call +94771234567.", []string{"+94771234567"}, []Kind{Phone}},
	{"plus spaced", "Call +94 77 123 4567.", []string{"+94 77 123 4567"}, []Kind{Phone}},
	{"plus with trunk", "Call +94 (0) 77 123 4567.", []string{"+94 (0) 77 123 4567"}, []Kind{Phone}},
	{"double zero", "Call 0094771234567.", []string{"0094771234567"}, []Kind{Phone}},
	{"no plus", "Call 94771234567.", []string{"94771234567"}, []Kind{Phone}},
	{"Sinhala, a case ending joined", "කරුණාකර 0771234567ට අමතන්න. හැඳුනුම්පත 123456789Vයි.", []string{"0771234567", "123456789V"}, []Kind{Phone, Card}},
	{"Tamil, a case ending joined", "தயவுசெய்து 077 123 4567க்கு அழைக்கவும். அடையாள அட்டை 199012345678ஆகும்.", []string{"077 123 4567", "199012345678"}, []Kind{Phone, Card}},
	{"Tamil digits", "அழைக்க ௦௭௭௧௨௩௪௫௬௭ எண்", []string{"௦௭௭௧௨௩௪௫௬௭"}, []Kind{Phone}},
	{"Sinhala digits", "අමතන්න ෦෭෭෧෨෩෪෫෬෭ දැන්", []string{"෦෭෭෧෨෩෪෫෬෭"}, []Kind{Phone}},
	{"no-break space", "Call 077 123 4567 now", []string{"077 123 4567"}, []Kind{Phone}},
	{"non-breaking hyphen", "Call 077‑123‑4567 now", []string{"077‑123‑4567"}, []Kind{Phone}},
	{"edges", "0771234567", []string{"0771234567"}, []Kind{Phone}},
	{"two the same", "0771234567 and 0771234567.", []string{"0771234567", "0771234567"}, []Kind{Phone, Phone}},
	{"beside each other", "123456789V,0771234567", []string{"123456789V", "0771234567"}, []Kind{Card, Phone}},
	{"on separate lines", "0771234567\n123456789V", []string{"0771234567", "123456789V"}, []Kind{Phone, Card}},
}

func TestMaskFindsTheForms(t *testing.T) {
	for _, c := range found {
		m := Mask(c.text)
		if len(m.Items) != len(c.want) {
			t.Errorf("%s: %d found in %q", c.name, len(m.Items), m.Text)
			continue
		}
		for i, it := range m.Items {
			if it.Value != c.want[i] || it.Kind != c.kinds[i] {
				t.Errorf("%s: %+v", c.name, it)
			}
			if strings.Contains(m.Text, it.Value) {
				t.Errorf("%s: %q still in %q", c.name, it.Value, m.Text)
			}
			if m.Text[it.Start:it.End] != it.Token || c.text[it.OrigStart:it.OrigEnd] != it.Value {
				t.Errorf("%s: offsets %+v", c.name, it)
			}
		}
		if m.Plain() != c.text {
			t.Errorf("%s: Plain %q", c.name, m.Plain())
		}
	}
}

func TestMaskLeavesOtherNumbersAlone(t *testing.T) {
	for _, text := range []string{
		"", "A letter with no numbers.", "Rs. 1,250.00 on 2030-01-01", "Reference 12345", "Room 203, floor 4",
		"12345678V", "1234567890V", "12345678901", "1234567890123", // too few or too many
		"Ref ABC0771234567", "0771234567ABC", "ID123456789V", "123456789VX", // part of a longer reference
		"077123456", "07712345678", "123456789 Vote", "95771234567",
	} {
		if m := Mask(text); len(m.Items) != 0 || m.Text != text {
			t.Errorf("%q became %q", text, m.Text)
		}
	}
}

func TestStandInsAreNumberedAndNeverRepeatWhatTheTextHolds(t *testing.T) {
	m := Mask("0771234567 then 123456789V")
	if m.Text != "⟦M1⟧ then ⟦M2⟧" {
		t.Fatal(m.Text)
	}
	m = Mask("Written ⟦M1⟧ then 0771234567")
	if m.Text != "Written ⟦M1⟧ then ⟦M2⟧" || m.Items[0].Token != "⟦M2⟧" {
		t.Fatal(m.Text)
	}
	got, err := m.Restore("[ta] Written ⟦M1⟧ then ⟦M2⟧")
	if err != nil || got != "[ta] Written ⟦M1⟧ then 0771234567" {
		t.Fatal(got, err)
	}
}

func TestRestore(t *testing.T) {
	m := Mask("Call 0771234567 with 123456789V.")
	for answer, want := range map[string]string{
		"[ta] Call ⟦M1⟧ with ⟦M2⟧.": "[ta] Call 0771234567 with 123456789V.",
		"⟦M2⟧ வுடன் ⟦M1⟧ அழைக்க":    "123456789V வுடன் 0771234567 அழைக்க",
	} {
		if got, err := m.Restore(answer); err != nil || got != want {
			t.Errorf("%q: %q %v", answer, got, err)
		}
	}
	for _, answer := range []string{
		"Call with ⟦M2⟧.",           // lost
		"Call ⟦M1⟧ with.",           // lost
		"Call with.",                // both lost
		"Call ⟦M3⟧ with ⟦M2⟧.",      // changed
		"Call [M1] with ⟦M2⟧.",      // changed
		"Call ⟦ M1 ⟧ with ⟦M2⟧.",    // changed
		"Call ⟦M1⟧ ⟦M1⟧ with ⟦M2⟧.", // repeated
		"Call ⟦M1⟧ with ⟦M2⟧ ⟦M9⟧.", // one it never held
		"Call ⟦M1 with ⟦M2⟧.",       // half
	} {
		if got, err := m.Restore(answer); !errors.Is(err, ErrLost) || got != "" {
			t.Errorf("%q: %q %v", answer, got, err)
		}
	}
	if got, err := Mask("Nothing here.").Restore("Rien ici."); err != nil || got != "Rien ici." {
		t.Fatal(got, err)
	}
	if _, err := Mask("Nothing here.").Restore("Rien ⟦M1⟧ ici."); !errors.Is(err, ErrLost) {
		t.Fatal("a stand-in the text never held is a change")
	}
}

// The error never carries the answer or a value.
func TestLostHoldsNoText(t *testing.T) {
	_, err := Mask("MARKER 0771234567").Restore("MARKER gone")
	if err == nil || strings.Contains(err.Error(), "MARKER") || strings.Contains(err.Error(), "0771234567") {
		t.Fatal(err)
	}
}

// A slice is numbered from one, so the same sentence with another number is the same
// piece, and restores by itself.
func TestSlice(t *testing.T) {
	m := Mask("First 0771234567 end. Second 123456789V and 0719999999 end.")
	cut := strings.Index(m.Text, "Second")
	a, b := m.Slice(0, cut-1), m.Slice(cut, len(m.Text))
	if a.Text != "First ⟦M1⟧ end." || b.Text != "Second ⟦M1⟧ and ⟦M2⟧ end." {
		t.Fatal(a.Text, b.Text)
	}
	if a.Plain() != "First 0771234567 end." || b.Plain() != "Second 123456789V and 0719999999 end." {
		t.Fatal(a.Plain(), b.Plain())
	}
	if !b.HasCard() || a.HasCard() {
		t.Fatal("kinds")
	}
	for _, it := range b.Items {
		if b.Plain()[it.OrigStart:it.OrigEnd] != it.Value || b.Text[it.Start:it.End] != it.Token {
			t.Fatalf("%+v", it)
		}
	}
	got, err := b.Restore("[x] ⟦M2⟧ ⟦M1⟧")
	if err != nil || got != "[x] 0719999999 123456789V" {
		t.Fatal(got, err)
	}
	if same := Mask("Second 0770000000 and 0710000000 end."); same.Text != b.Text {
		t.Fatal(same.Text)
	}
	// A stand-in written as such in the text keeps its own meaning in a slice.
	w := Mask("A ⟦M1⟧ 0771234567. B.")
	if s := w.Slice(0, len(w.Text)); s.Text != w.Text {
		t.Fatal(s.Text)
	}
}

// Positions are in UTF-16 units of the text sent, also with text outside the Basic
// Multilingual Plane before and after a masked value.
func TestToOriginal(t *testing.T) {
	const orig = "😀 0771234567 😀 end"
	m := Mask(orig)
	if m.Text != "😀 ⟦M1⟧ 😀 end" {
		t.Fatal(m.Text)
	}
	// In UTF-16: 😀 is 2 and ⟦M1⟧ is 4.
	for pos, want := range map[int]int{0: 0, 2: 2, 3: 3, 7: 13, 8: 14, 11: 17, 14: 20} {
		got, ok := m.ToOriginal(pos)
		if !ok || got != want {
			t.Errorf("%d: %d %v, want %d", pos, got, ok, want)
		}
	}
	for _, pos := range []int{4, 5, 6} {
		if _, ok := m.ToOriginal(pos); ok {
			t.Errorf("%d is inside a stand-in", pos)
		}
	}
	if got, ok := Mask("plain").ToOriginal(3); !ok || got != 3 {
		t.Fatal(got, ok)
	}
}
