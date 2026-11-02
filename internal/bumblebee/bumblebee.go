// Package bumblebee is ROSETTA's client for BUMBLEBEE, its only provider
// (R-PRV-01; B 15). What it may send and how it is answered is docs/CALLERS.md in
// BUMBLEBEE's repository. BUMBLEBEE owns the limits on its sources: this package
// reads them from the source list and keeps no copy (R-PRV-04).
package bumblebee

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The source states in CALLERS.md.
const (
	StateOn            = "on"
	StateRestingBlock  = "resting-block"
	StateRestingChange = "resting-change"
	StateOff           = "off"
)

// The operations in CALLERS.md.
const (
	OpTranslate = "translate"
	OpGrammar   = "grammar"
)

// maxList is the most read of a source list; a list is a few kilobytes.
const maxList = 1 << 20

// Offer is what one source does.
type Offer struct {
	Operation string `json:"operation"`
	From      string `json:"from_language"`
	To        string `json:"to_language"` // translation only
}

// Source is one entry of the source list.
type Source struct {
	Name      string
	Version   string
	State     string
	RestUntil *time.Time // only while resting
	Offers    []Offer
	Scores    map[string]float64 // grammar sources: benchmark score by language
}

// Offer says whether the source offers this work. to is "" for grammar.
func (s Source) Offer(operation, from, to string) bool {
	for _, o := range s.Offers {
		if o.Operation == operation && o.From == from && o.To == to {
			return true
		}
	}
	return false
}

// Usable says whether the source may be called now. A resting source is skipped
// until its resume time (R-PRV-01), and if it gives none, until the list says
// otherwise. Any state CALLERS.md does not list is not called.
func (s Source) Usable(now time.Time) bool {
	switch s.State {
	case StateOn:
		return true
	case StateRestingBlock, StateRestingChange:
		return s.RestUntil != nil && !now.Before(*s.RestUntil)
	}
	return false
}

// List is the source list.
type List struct {
	MaxTextChars int // 0 when the list does not give one
	Sources      []Source
}

// Find is the source with this name.
func (l List) Find(name string) (Source, bool) {
	for _, s := range l.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return Source{}, false
}

// ParseList reads the body of GET /v1/sources. Fields it does not know are ignored.
func ParseList(raw []byte) (List, error) {
	var in struct {
		Limits struct {
			MaxTextChars int `json:"max_text_chars"`
		} `json:"limits"`
		Sources *[]struct {
			Name      string             `json:"name"`
			Version   string             `json:"version"`
			State     string             `json:"state"`
			RestUntil string             `json:"rest_until"`
			Offers    []Offer            `json:"offers"`
			Scores    map[string]float64 `json:"scores"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Sources == nil {
		return List{}, errors.New("bumblebee: the source list is not in the documented shape")
	}
	out := List{MaxTextChars: max(in.Limits.MaxTextChars, 0)}
	for _, s := range *in.Sources {
		if s.Name == "" {
			return List{}, errors.New("bumblebee: a source has no name")
		}
		src := Source{Name: s.Name, Version: s.Version, State: s.State, Offers: s.Offers, Scores: s.Scores}
		if at, err := time.Parse(time.RFC3339, s.RestUntil); err == nil {
			src.RestUntil = &at
		}
		out.Sources = append(out.Sources, src)
	}
	return out, nil
}

// Client calls BUMBLEBEE. Its key goes in a bearer header and nowhere else.
type Client struct {
	URL  string // ROSETTA_BUMBLEBEE_URL, with no trailing slash
	Key  string // ROSETTA_BUMBLEBEE_KEY
	HTTP *http.Client
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Sources reads the source list. CALLERS.md: it never starts a browser, so read it
// as often as you like.
func (c Client) Sources(ctx context.Context) (List, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.URL, "/")+"/v1/sources", nil)
	if err != nil {
		return List{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	res, err := c.http().Do(req)
	if err != nil {
		return List{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxList))
	if err != nil {
		return List{}, err
	}
	if res.StatusCode != http.StatusOK {
		return List{}, fmt.Errorf("bumblebee: the source list answered %d", res.StatusCode)
	}
	return ParseList(raw)
}

// The failure reasons in CALLERS.md. ROSETTA decides on these, never on the HTTP
// status or the explanation, which may change.
const (
	ReasonBusy        = "busy"
	ReasonBlocked     = "blocked"
	ReasonChanged     = "changed"
	ReasonTimeout     = "timeout"
	ReasonTooLong     = "too-long"
	ReasonUnsupported = "unsupported"
	ReasonSourceOff   = "source-off"
	ReasonUnavailable = "unavailable"
)

// maxAnswer is the most read of one answer. A text is limited in code points, and a
// translation may grow, so this is well past any real one.
const maxAnswer = 8 << 20

// Request is one source call: translation of one masked piece (POST /v1/requests).
type Request struct {
	RequestID string // ROSETTA's request reference (R-REC-01)
	Source    string
	From, To  string
	Text      string
}

// Answer is a source's translation.
type Answer struct {
	Text    string
	Source  string
	Version string
}

// Failure is a source call that failed, with one of the reasons above. It holds
// neither the explanation nor the answer, so an error can never carry text.
type Failure struct {
	Reason  string
	RetryAt *time.Time
}

func (f *Failure) Error() string { return "bumblebee: " + f.Reason }

// Translate sends one piece to one source. BUMBLEBEE uses only this source and never
// falls back: that is ROSETTA's choice (R-PRV-03). An error is a *Failure, or ctx's
// own error when the time ran out or the request was given up; an answer in no
// documented shape, a key refused or no answer at all count as "unavailable", as
// CALLERS.md has it for anything else.
func (c Client) Translate(ctx context.Context, q Request) (Answer, error) {
	raw, err := json.Marshal(map[string]string{"request_id": q.RequestID, "source": q.Source, "operation": OpTranslate, "from": q.From, "to": q.To, "text": q.Text})
	if err != nil {
		return Answer{}, &Failure{Reason: ReasonUnavailable}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/v1/requests", bytes.NewReader(raw))
	if err != nil {
		return Answer{}, &Failure{Reason: ReasonUnavailable}
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Answer{}, ctx.Err()
		}
		return Answer{}, &Failure{Reason: ReasonUnavailable}
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	if err != nil {
		if ctx.Err() != nil {
			return Answer{}, ctx.Err()
		}
		return Answer{}, &Failure{Reason: ReasonUnavailable}
	}
	return parseAnswer(body)
}

// parseAnswer reads an answer on failure.reason, whatever the status was.
func parseAnswer(body []byte) (Answer, error) {
	var out struct {
		Text    *string `json:"text"`
		Source  string  `json:"source"`
		Version string  `json:"version"`
		Failure *struct {
			Reason  string `json:"reason"`
			RetryAt string `json:"retry_at"`
		} `json:"failure"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Answer{}, &Failure{Reason: ReasonUnavailable}
	}
	if out.Failure != nil {
		f := &Failure{Reason: ReasonUnavailable}
		switch r := out.Failure.Reason; r {
		case ReasonBusy, ReasonBlocked, ReasonChanged, ReasonTimeout, ReasonTooLong, ReasonUnsupported, ReasonSourceOff:
			f.Reason = r
		}
		if at, err := time.Parse(time.RFC3339, out.Failure.RetryAt); err == nil {
			f.RetryAt = &at
		}
		return Answer{}, f
	}
	if out.Text == nil {
		return Answer{}, &Failure{Reason: ReasonUnavailable}
	}
	return Answer{Text: *out.Text, Source: out.Source, Version: out.Version}, nil
}
