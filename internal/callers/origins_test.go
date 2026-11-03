package callers

import (
	"errors"
	"testing"
)

func TestParseOrigin(t *testing.T) {
	for text, want := range map[string]string{
		"https://writedocument.epc.test":      "https://writedocument.epc.test",
		" https://writedocument.epc.test/ ":   "https://writedocument.epc.test",
		"HTTPS://WriteDocument.EPC.test":      "https://writedocument.epc.test",
		"https://writedocument.epc.test:8443": "https://writedocument.epc.test:8443",
		"http://localhost:53180":              "http://localhost:53180",
		"http://127.0.0.1:53180":              "http://127.0.0.1:53180",
	} {
		if got, err := ParseOrigin(text); err != nil || got != want {
			t.Errorf("%q: %q %v", text, got, err)
		}
	}
	for _, text := range []string{
		"", "*", "null", "writedocument.epc.test", "http://writedocument.epc.test", "ftp://writedocument.epc.test",
		"https://*.epc.test", "https://", "https://writedocument.epc.test/path", "https://writedocument.epc.test?x=1",
		"https://writedocument.epc.test#x", "https://person@writedocument.epc.test", "https://a.test, https://b.test",
		"javascript:alert(1)",
	} {
		if got, err := ParseOrigin(text); !errors.Is(err, ErrBadOrigin) {
			t.Errorf("%q: %q %v", text, got, err)
		}
	}
}
