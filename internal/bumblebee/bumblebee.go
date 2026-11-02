// Package bumblebee is ROSETTA's client for BUMBLEBEE, its only provider
// (R-PRV-01; B 15). What it may send and how it is answered is docs/CALLERS.md in
// BUMBLEBEE's repository. BUMBLEBEE owns the limits on its sources: this package
// reads them from the source list and keeps no copy (R-PRV-04).
package bumblebee

import (
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
