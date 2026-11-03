// Package bifrost is ROSETTA's client for BIFROST, as docs/EXCHANGE.md in its repository gives it
// (B 21). ROSETTA looks up one person's post by email, under its own agreement in TVA; it never
// holds a list. Only an answer of found: false means not found: everything else is a failed look-up
// and says nothing about the person (R-ADM-06). Nothing here logs a key or an address.
package bifrost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Fact is the fact looked up, by the name its agreement holds.
const Fact = "scroll.staff-lookup"

// callTimeout is the longest one look-up waits.
const callTimeout = 10 * time.Second

// Office is one place a person holds a post.
type Office struct {
	Office string `json:"office"`
	Post   string `json:"post"`
}

// Answer is BIFROST's answer about one person. Found with no offices is still found, at no post.
type Answer struct {
	Found   bool
	Offices []Office
}

// Refused is BIFROST refusing, by the code it documents. Wait is its Retry-After, on too_many.
type Refused struct {
	Status int
	Code   string
	Wait   time.Duration
}

func (r *Refused) Error() string {
	return fmt.Sprintf("BIFROST refused with status %d, code %s", r.Status, r.Code)
}

const tooMany = "too_many"

// TooMany says the key is over its limit a minute, and how long to wait.
func (r *Refused) TooMany() bool { return r.Code == tooMany }

// Client calls BIFROST.
type Client struct {
	URL  string
	Key  string // never logged
	HTTP *http.Client
}

// Lookup asks after one person. An error is a failed look-up, whatever caused it, and holds nothing
// of the address asked about.
func (c Client) Lookup(ctx context.Context, email string) (Answer, error) {
	body, err := json.Marshal(map[string]string{"email": email})
	if err != nil {
		return Answer{}, errors.New("the look-up could not be written")
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/api/facts/"+Fact+"/lookup", bytes.NewReader(body))
	if err != nil {
		return Answer{}, errors.New("BIFROST's address is not usable")
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return Answer{}, errors.New("BIFROST could not be reached")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"refusal"`
		}
		json.Unmarshal(raw, &refusal)
		out := &Refused{Status: res.StatusCode, Code: refusal.Code}
		if s, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && s > 0 {
			out.Wait = time.Duration(s) * time.Second
		}
		return Answer{}, out
	}
	var answer struct {
		Found   *bool    `json:"found"`
		Offices []Office `json:"offices"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || answer.Found == nil {
		return Answer{}, errors.New("BIFROST's answer has no found in it")
	}
	return Answer{Found: *answer.Found, Offices: answer.Offices}, nil
}
