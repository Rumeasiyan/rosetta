// Package reasons holds the fixed list of why ROSETTA refused or failed a request
// (R-ERR-01), and how that is answered: one reason, a message for the officer in
// Sinhala, Tamil and English (R-ERR-02), and where it helps, when to try again.
//
// A failure never holds the text that was sent: nothing here takes text, only a
// reason, a position in the list, a time and glossary terms, which are the Office's.
package reasons

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
)

// Reason is one word from the fixed list.
type Reason string

// The fixed list (R-ERR-01). The words go to callers as they are written here.
const (
	NoKey         Reason = "no key"
	OverLimit     Reason = "over limit"
	Invalid       Reason = "invalid request"
	TooLong       Reason = "too long"
	Unsupported   Reason = "unsupported"
	HumanRequired Reason = "human translation required"
	NoProvincial  Reason = "no provincial provider"
	MaskingLost   Reason = "masking lost"
	NoProvider    Reason = "no provider"
	TimedOut      Reason = "timed out"
)

// All is the whole list, in the order of R-ERR-01.
var All = []Reason{NoKey, OverLimit, Invalid, TooLong, Unsupported, HumanRequired, NoProvincial, MaskingLost, NoProvider, TimedOut}

// statuses and words are each reason's HTTP status and message key. Callers decide
// on the reason; the status only says the same thing to the HTTP layer.
var (
	statuses = map[Reason]int{
		NoKey: http.StatusUnauthorized, OverLimit: http.StatusTooManyRequests, Invalid: http.StatusBadRequest,
		TooLong: http.StatusRequestEntityTooLarge, Unsupported: http.StatusUnprocessableEntity, HumanRequired: http.StatusConflict,
		NoProvincial: http.StatusForbidden, MaskingLost: http.StatusBadGateway, NoProvider: http.StatusServiceUnavailable,
		TimedOut: http.StatusGatewayTimeout,
	}
	words = map[Reason]string{
		NoKey: "reasonNoKey", OverLimit: "reasonOverLimit", Invalid: "reasonInvalid", TooLong: "reasonTooLong", Unsupported: "reasonUnsupported",
		HumanRequired: "reasonHumanRequired", NoProvincial: "reasonNoProvincial", MaskingLost: "reasonMaskingLost", NoProvider: "reasonNoProvider", TimedOut: "reasonTimedOut",
	}
)

// Status is the reason's HTTP status.
func (r Reason) Status() int { return statuses[r] }

// Word is the message key of the reason.
func (r Reason) Word() string { return words[r] }

// Messages is the reason's message in the three languages, for the caller to show
// in the reader's language (R-ERR-02).
func (r Reason) Messages() map[string]string {
	out := make(map[string]string, len(i18n.Langs))
	for _, l := range i18n.Langs {
		out[string(l)] = i18n.T(l, words[r])
	}
	return out
}

// Failure is one refusal or failure. Its Error is the reason alone, so logging one
// can never carry text.
type Failure struct {
	Reason   Reason
	Position *int       // the passage at fault, counted from 0, when one is (R-TRN-02)
	RetryAt  *time.Time // when to try again, where it helps
	Terms    []string   // glossary terms found, to help the translator (R-GAT-03)
}

func (f *Failure) Error() string { return string(f.Reason) }

// New is a failure with nothing more to say.
func New(r Reason) *Failure { return &Failure{Reason: r} }

// At says which passage failed.
func (f *Failure) At(position int) *Failure {
	f.Position = &position
	return f
}

// Until says when to try again; the zero time says nothing.
func (f *Failure) Until(at time.Time) *Failure {
	if !at.IsZero() {
		u := at.UTC()
		f.RetryAt = &u
	}
	return f
}

// body is the answer's shape. It holds nothing a caller sent except the position.
type body struct {
	RequestRef string `json:"request_ref,omitempty"`
	Error      detail `json:"error"`
}

type detail struct {
	Reason   Reason            `json:"reason"`
	Messages map[string]string `json:"messages"`
	Position *int              `json:"position,omitempty"`
	RetryAt  string            `json:"retry_at,omitempty"`
	Terms    []string          `json:"glossary_terms,omitempty"`
}

// retryAfter sets Retry-After on any failure that says when.
func retryAfter(h http.Header, f *Failure, now time.Time) {
	if f.RetryAt == nil {
		return
	}
	wait := int(f.RetryAt.Sub(now).Seconds() + 0.999)
	if wait < 1 {
		wait = 1
	}
	h.Set("Retry-After", strconv.Itoa(wait))
}

// WriteText answers with the failure as plain text, for a reader who is shown the body as
// it is: the grammar plug-in shows responseText (docs/plugin-findings.md). The reason and
// reference go in headers, as no words of a form's own could carry them there.
func WriteText(w http.ResponseWriter, requestRef string, f *Failure, message string, now time.Time) {
	h := w.Header()
	retryAfter(h, f, now)
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Rosetta-Reason", string(f.Reason))
	h.Set("X-Rosetta-Request-Ref", requestRef)
	w.WriteHeader(f.Reason.Status())
	w.Write([]byte(message))
}

// Write answers with the failure. Retry-After goes with any failure that says when.
func Write(w http.ResponseWriter, requestRef string, f *Failure, now time.Time) {
	d := detail{Reason: f.Reason, Messages: f.Reason.Messages(), Position: f.Position, Terms: f.Terms}
	h := w.Header()
	if f.RetryAt != nil {
		d.RetryAt = f.RetryAt.Format(time.RFC3339)
	}
	retryAfter(h, f, now)
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(f.Reason.Status())
	json.NewEncoder(w).Encode(body{RequestRef: requestRef, Error: d})
}
