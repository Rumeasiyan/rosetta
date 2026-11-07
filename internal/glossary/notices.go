package glossary

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/hedwig"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The notices keepers are sent (R-GLO-04), each a template in HEDWIG (E-ENB-13). The alert's
// kind is what is kept here; the template is the notice's type there.
const (
	KindProposal = "glossary proposal"
	KindImport   = "glossary import"
	// KindPersonAdded tells someone they were added. Its alert's subject is their row in person.
	KindPersonAdded = "person added"

	noticeProposal    = "glossary_proposal"
	noticeImport      = "glossary_import"
	noticePersonAdded = "rosetta_person_added"
)

// sendWait is the longest one pass of sending may take after a person's change.
const sendWait = 30 * time.Second

// keptDays is how long an alert is kept (Annex A), and past it nobody is told.
const keptDays = 90

// RaiseProposal writes the notice for a proposal in tx, due to go once tx is kept.
func RaiseProposal(ctx context.Context, tx pgx.Tx, entry int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO alert (kind, glossary_entry_id) VALUES ($1, $2)`, KindProposal, entry)
	return err
}

// RaiseImport writes one notice for a whole import, rather than one for each row.
func RaiseImport(ctx context.Context, tx pgx.Tx, imp int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO alert (kind, glossary_import_id) VALUES ($1, $2)`, KindImport, imp)
	return err
}

// RaisePersonAdded writes the notice telling someone they were added.
func RaisePersonAdded(ctx context.Context, tx pgx.Tx, person int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO alert (kind, subject) VALUES ($1, $2)`, KindPersonAdded, strconv.FormatInt(person, 10))
	return err
}

// Sender sends one notice.
type Sender interface {
	Send(ctx context.Context, n hedwig.Notice) (hedwig.Accepted, error)
}

// Notifier tells keepers what waits for them. After a change a goroutine sends what is due, never
// a timer (AGENTS C6), and the person's request does not wait; what could not go waits for the
// catch-up job. Each notice carries its alert's own request key, so sending it twice sends it once.
// A notice holds a number and a link, never a word of the glossary.
type Notifier struct {
	Pool      *pgxpool.Pool
	Sender    Sender // nil sends nothing: notices wait
	PublicURL string
	Logger    *slog.Logger

	mu      sync.Mutex
	running bool
	again   bool
	wg      sync.WaitGroup
}

func (n *Notifier) log() *slog.Logger {
	if n.Logger != nil {
		return n.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Kick starts sending what is due, without waiting for it.
func (n *Notifier) Kick() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.running {
		n.again = true
		return
	}
	n.running = true
	n.wg.Go(func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), sendWait)
			if _, err := n.Send(ctx, n.Pool); err != nil {
				n.log().Error("notices to keepers were not read")
			}
			cancel()
			n.mu.Lock()
			if !n.again {
				n.running = false
				n.mu.Unlock()
				return
			}
			n.again = false
			n.mu.Unlock()
		}
	})
}

// Wait is for shutting down: it lets a pass in hand finish.
func (n *Notifier) Wait() { n.wg.Wait() }

// Job sends what waits, as part of the job PARADOX calls for what is owed to other services.
func (n *Notifier) Job(ctx context.Context, tx pgx.Tx, _ string) (int, error) { return n.Send(ctx, tx) }

// Send goes through what is due and reports how many went. A notice that cannot go stays for the
// next try and stops none of the others; when HEDWIG asks for a wait, the rest wait too.
func (n *Notifier) Send(ctx context.Context, q Querier) (int, error) {
	if n.Sender == nil {
		return 0, nil
	}
	rows, err := q.Query(ctx, `SELECT id, kind, glossary_entry_id, glossary_import_id, subject FROM alert
		WHERE sent_at IS NULL AND kind IN ($1, $2, $3) AND raised_at > now() - make_interval(days => $4) ORDER BY id`, KindProposal, KindImport, KindPersonAdded, keptDays)
	if err != nil {
		return 0, err
	}
	type due struct {
		id    int64
		kind  string
		entry *int64
		imp   *int64
		about *string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (due, error) {
		var d due
		err := r.Scan(&d.id, &d.kind, &d.entry, &d.imp, &d.about)
		return d, err
	})
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, d := range list {
		notice, err := n.build(ctx, q, d.kind, d.id, d.entry, d.imp, d.about)
		if err != nil {
			return sent, err
		}
		if len(notice.People) == 0 {
			// Nobody to tell: a language with no keeper is flagged by E-CAL-05, and the work
			// stays on the waiting list.
			n.log().Warn("a notice had no keeper to go to", "alert", d.id)
			if _, err := q.Exec(ctx, `UPDATE alert SET sent_at = now() WHERE id = $1`, d.id); err != nil {
				return sent, err
			}
			continue
		}
		accepted, err := n.Sender.Send(ctx, notice)
		if err != nil {
			n.log().Warn("a notice to keepers did not go", "alert", d.id, "why", err.Error())
			var refused *hedwig.Refused
			if errors.As(err, &refused) && refused.Wait > 0 {
				break
			}
			continue
		}
		if _, err := q.Exec(ctx, `UPDATE alert SET sent_at = now(), hedwig_notice_id = $2 WHERE id = $1`, d.id, strconv.FormatInt(accepted.RequestID, 10)); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

// build is the notice for one alert: who to tell, in their own language, and where to look.
func (n *Notifier) build(ctx context.Context, q Querier, kind string, alert int64, entry, imp *int64, about *string) (hedwig.Notice, error) {
	out := hedwig.Notice{RequestKey: "rosetta-alert-" + strconv.FormatInt(alert, 10), Details: map[string]string{}}
	langs := i18n.Langs
	if kind == KindPersonAdded {
		out.Type = noticePersonAdded
		out.Details["link"] = strings.TrimRight(n.PublicURL, "/")
		var err error
		out.People, err = addedPerson(ctx, q, about)
		return out, err
	}
	switch {
	case kind == KindProposal && entry != nil:
		e, err := Get(ctx, q, *entry)
		if err != nil {
			return out, err
		}
		out.Type = noticeProposal
		out.Details["entry"] = strconv.FormatInt(e.ID, 10)
		out.Details["link"] = strings.TrimRight(n.PublicURL, "/") + "/glossary/" + strconv.FormatInt(e.ID, 10)
		if !e.Waiting() {
			return out, nil // settled before anyone could be told
		}
		langs = e.Unchecked()
		names := make([]string, len(langs))
		for i, l := range langs {
			names[i] = string(l)
		}
		out.Details["languages"] = strings.Join(names, ", ")
	case kind == KindImport && imp != nil:
		out.Type = noticeImport
		out.Details["import"] = strconv.FormatInt(*imp, 10)
		out.Details["link"] = strings.TrimRight(n.PublicURL, "/") + "/glossary/waiting"
	}
	var err error
	out.People, err = keepersFor(ctx, q, langs)
	return out, err
}

// addedPerson is who a person added notice goes to: nobody if they have since been removed.
func addedPerson(ctx context.Context, q Querier, about *string) ([]hedwig.Person, error) {
	if about == nil {
		return nil, nil
	}
	id, err := strconv.ParseInt(*about, 10, 64)
	if err != nil {
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT email, screen_language FROM person WHERE id = $1 AND added_at IS NOT NULL AND email <> ''`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (hedwig.Person, error) {
		p := hedwig.Person{Ref: "rosetta-person-" + *about}
		err := r.Scan(&p.Email, &p.Language)
		return p, err
	})
}

// keepersFor is the live keepers for these languages, each once. An opaque reference names each to
// HEDWIG (API.md), with the address MJOLNIR gave and the language they chose on the screens. One
// with no address cannot be told and is left out, as HEDWIG would refuse the whole request.
func keepersFor(ctx context.Context, q Querier, langs []i18n.Lang) ([]hedwig.Person, error) {
	names := make([]string, len(langs))
	for i, l := range langs {
		names[i] = string(l)
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT p.id, p.email, p.screen_language FROM role_grant g JOIN person p ON p.id = g.person_id
		WHERE g.role = 'glossary keeper' AND g.language = ANY($1) AND g.ended_at IS NULL AND g.starts_at <= now()
			AND (g.ends_on IS NULL OR g.ends_on >= (now() AT TIME ZONE 'Asia/Colombo')::date) AND p.email <> '' ORDER BY p.id`, names)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (hedwig.Person, error) {
		var id int64
		var p hedwig.Person
		err := r.Scan(&id, &p.Email, &p.Language)
		p.Ref = "rosetta-person-" + strconv.FormatInt(id, 10)
		return p, err
	})
}

// Mine is what one person proposed, newest first, each with its outcome (R-GLO-04).
func Mine(ctx context.Context, q Querier, person int64) ([]Entry, error) {
	return collect(ctx, q, entrySQL+` WHERE e.proposed_by = $1 ORDER BY e.id DESC LIMIT $2`, person, PageLimit)
}

// DuplicateError says which entry holds the term, so a proposer can correct it instead.
type DuplicateError struct{ Entry int64 }

func (e *DuplicateError) Error() string { return ErrDuplicate.Error() }

// Is makes it read as ErrDuplicate.
func (e *DuplicateError) Is(target error) bool { return target == ErrDuplicate }
