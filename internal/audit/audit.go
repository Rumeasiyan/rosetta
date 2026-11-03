// Package audit keeps what ROSETTA owes PENSIEVE, the provincial audit record
// (R-REC-02; B 13). Record writes an entry to the outbox inside the transaction
// doing the work, so the change and its entry are kept together or not at all.
//
// An entry has the shape docs/INTAKE.md in PENSIEVE's repository gives (P-ING-01).
// It says who did what to which reference, never text a caller sent.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/oidc"
	"github.com/jackc/pgx/v5"
)

// The posts an entry may carry until E-CAL-05 looks posts up. PENSIEVE refuses a
// blank one, so these go exactly as written.
const (
	PostNotKnown = "not known" // a person, whose post is not known at the time
	PostSystem   = "system"    // ROSETTA's own automatic work
)

// The actions ROSETTA sends, from PENSIEVE's one list (P-ING-02). Sign-in and
// sign-out are MJOLNIR's to send.
const (
	ActionDenial        = "denial"
	ActionCreation      = "creation"
	ActionChange        = "change"
	ActionRoleChange    = "role change"
	ActionKeyChange     = "key change"
	ActionSettingChange = "setting change"
)

// The results PENSIEVE takes.
const (
	ResultDenied = "denied"
	ResultDone   = "done"
)

// Longest is what PENSIEVE takes for each item.
const (
	maxDetails = 8 << 10
	maxReason  = 1000
	maxShort   = 200
	maxWord    = 100
	maxVisitor = 64
)

// Actor is who acted.
type Actor struct {
	PersonID int64 // zero for ROSETTA itself
	Subject  string
	Name     string
	Post     string
}

// System is ROSETTA acting by itself.
var System = Actor{Subject: "rosetta", Name: "ROSETTA", Post: PostSystem}

// Event is one action to record.
type Event struct {
	Actor      Actor
	Action     string
	ObjectType string
	ObjectRef  string
	Result     string
	Reason     string         // fixed words, never text a person typed
	Visitor    string         // the visitor's address, if any
	Details    map[string]any // small; see CheckDetails
}

var actions = map[string]bool{ActionDenial: true, ActionCreation: true, ActionChange: true, ActionRoleChange: true, ActionKeyChange: true, ActionSettingChange: true}

// Entry is what PENSIEVE receives, without the items the outbox has its own
// columns for: entry_id as id, occurred_at, action.
type Entry struct {
	ID         string         `json:"id"`
	OccurredAt string         `json:"occurred_at"`
	Actor      string         `json:"actor"`
	ActorName  string         `json:"actor_name,omitempty"`
	Post       string         `json:"post"`
	Action     string         `json:"action"`
	ObjectType string         `json:"object_type"`
	ObjectRef  string         `json:"object_ref"`
	Result     string         `json:"result"`
	Reason     string         `json:"reason,omitempty"`
	Visitor    string         `json:"visitor_address,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// build checks e and makes its entry. A fault here is a fault in the code that
// wrote the event: PENSIEVE would refuse it for good, so it stops the work now.
func build(e Event, id string, at time.Time) (Entry, error) {
	switch {
	case strings.TrimSpace(e.Actor.Subject) == "":
		return Entry{}, errors.New("audit: an entry needs an actor")
	case strings.TrimSpace(e.Actor.Post) == "":
		return Entry{}, errors.New("audit: an entry needs a post: send not known")
	case !actions[e.Action]:
		return Entry{}, fmt.Errorf("audit: %q is not on the list ROSETTA sends", e.Action)
	case e.ObjectType == "" || e.ObjectRef == "":
		return Entry{}, errors.New("audit: an entry needs what it acted on")
	case e.Result != ResultDenied && e.Result != ResultDone:
		return Entry{}, fmt.Errorf("audit: %q is not a result ROSETTA sends", e.Result)
	}
	if err := CheckDetails(e.Details); err != nil {
		return Entry{}, err
	}
	return Entry{
		ID: id, OccurredAt: at.UTC().Format(time.RFC3339Nano),
		Actor: fit(e.Actor.Subject, maxShort), ActorName: fit(e.Actor.Name, maxShort), Post: fit(e.Actor.Post, maxShort),
		Action: e.Action, ObjectType: fit(e.ObjectType, maxWord), ObjectRef: fit(e.ObjectRef, maxShort), Result: e.Result,
		Reason: fit(e.Reason, maxReason), Visitor: fit(e.Visitor, maxVisitor), Details: e.Details,
	}, nil
}

// Record keeps e in tx's outbox, due at once. The row's details column holds the
// whole entry: who, what and when (Annex A).
func Record(ctx context.Context, tx pgx.Tx, e Event, now time.Time) error {
	id := "rosetta-" + oidc.Random()
	entry, err := build(e, id, now)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	var actor *int64
	if e.Actor.PersonID != 0 {
		actor = &e.Actor.PersonID
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_outbox (entry_id, occurred_at, actor_id, action, details, next_try_at)
		VALUES ($1, $2, $3, $4, $5, $2)`, id, now, actor, e.Action, string(raw))
	return err
}

var (
	secretName = regexp.MustCompile(`password|passwd|pwd|secret|apikey|key|token|authorization|otp|credential|bearer`)
	idName     = regexp.MustCompile(`nic|nationalid|idnumber|identitycard`)
	secretText = regexp.MustCompile(`(?i)\b[a-z]{2}_[a-z0-9_-]{6,}|\bbearer\s+\S+|\beyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.|PRIVATE KEY|password=`)
	idText     = regexp.MustCompile(`(?i)(^|[^0-9a-z])([0-9]{9}[vx]|[0-9]{12})($|[^0-9a-z])`)
)

// CheckDetails refuses what PENSIEVE refuses (docs/INTAKE.md), every name and value
// at every depth: a secret or an identity card number, by name or by look; a body
// over its limit. It is a little wider than PENSIEVE on names, so nothing of the
// kind gets as far as being refused there.
func CheckDetails(details map[string]any) error {
	if details == nil {
		return nil
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if len(raw) > maxDetails {
		return errors.New("audit: details are over 8 KB")
	}
	var plain any
	if err := json.Unmarshal(raw, &plain); err != nil {
		return err
	}
	return scan(plain)
}

func scan(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for name, inner := range x {
			flat := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(name))
			if secretName.MatchString(flat) || idName.MatchString(flat) {
				// The name is the code's own: safe to say.
				return fmt.Errorf("audit: details name %q may not be sent", name)
			}
			if err := scan(inner); err != nil {
				return err
			}
		}
	case []any:
		for _, inner := range x {
			if err := scan(inner); err != nil {
				return err
			}
		}
	case string:
		return scanText(x)
	case float64:
		return scanText(fmt.Sprintf("%.0f", x))
	}
	return nil
}

func scanText(s string) error {
	if secretText.MatchString(s) || idText.MatchString(s) {
		return errors.New("audit: details hold what looks like a key or an identity card number")
	}
	return nil
}

// Visitor is the visitor's address: Cloudflare's header on the server, the
// connection's own locally.
func Visitor(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" && net.ParseIP(v) != nil {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}

// fit shortens s to n characters, so a long name never stops a record.
func fit(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
