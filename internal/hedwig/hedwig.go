// Package hedwig is ROSETTA's client for HEDWIG, the provincial notice service (B 14), as
// docs/API.md in its repository gives it. ROSETTA sends details and never wording: the wording
// is HEDWIG's template, in three languages. A person is named by an opaque reference, never a
// national ID. Nothing here logs a key, an address or a detail.
package hedwig

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

// callTimeout is the longest one call waits.
const callTimeout = 10 * time.Second

// Person is one person to tell. Ref is an opaque ID (API.md); Language is si, ta or en.
type Person struct {
	Ref      string `json:"ref"`
	Email    string `json:"email"`
	Language string `json:"language,omitempty"`
}

// Notice is one real-world event. RequestKey makes a second send of it send nothing again.
type Notice struct {
	Type       string            `json:"notice_type"`
	RequestKey string            `json:"request_key"`
	Details    map[string]string `json:"details,omitempty"`
	People     []Person          `json:"people"`
}

// Accepted is HEDWIG's answer: the request's number, by which to ask what happened.
type Accepted struct {
	RequestID int64
	Status    string // queued, or test from a test key
}

// Refused is HEDWIG refusing, with its code, which does not change (API.md). Its message is for
// reading, and could name a detail, so it is not kept. Wait is set on rate_limited.
type Refused struct {
	Status int
	Code   string
	Wait   time.Duration
}

func (r *Refused) Error() string {
	return fmt.Sprintf("HEDWIG refused with status %d, code %s", r.Status, r.Code)
}

// Client calls HEDWIG.
type Client struct {
	URL  string
	Key  string // never logged
	HTTP *http.Client
}

// Send sends one notice.
func (c Client) Send(ctx context.Context, n Notice) (Accepted, error) {
	body, err := json.Marshal(n)
	if err != nil {
		return Accepted{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/v1/notices", bytes.NewReader(body))
	if err != nil {
		return Accepted{}, errors.New("HEDWIG's address is not usable")
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return Accepted{}, errors.New("HEDWIG could not be reached")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusAccepted {
		var answer struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(raw, &answer)
		refused := &Refused{Status: res.StatusCode, Code: answer.Error.Code}
		if s, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && s > 0 {
			refused.Wait = time.Duration(s) * time.Second
		}
		return Accepted{}, refused
	}
	var answer struct {
		RequestID int64 `json:"request_id"`
		Notices   []struct {
			Status string `json:"status"`
		} `json:"notices"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return Accepted{}, errors.New("HEDWIG's answer is not JSON")
	}
	out := Accepted{RequestID: answer.RequestID}
	if len(answer.Notices) > 0 {
		out.Status = answer.Notices[0].Status
	}
	return out, nil
}
