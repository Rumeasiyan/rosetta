package web

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/bifrost"
	"github.com/Rumeasiyan/rosetta/internal/posts"
)

// withPosts looks posts up through the stand-in, as cmd/rosetta does.
func (w *world) withPosts() {
	w.app.deps.Posts = &posts.Service{Pool: w.pool, Looker: bifrost.Client{URL: w.standin.URL + "/bifrost", Key: "bf_standin"}}
}

// Signing in looks the person up, a role gone is gone from the first page, and the session keeps the
// post for its audit entries (E-CAL-05's note).
func TestSignInLooksThePostUp(t *testing.T) {
	w := newWorld(t)
	admin, _ := w.signedInAdmin()
	w.withPosts()

	// A keeper is granted a role: looked up at once, with the post filled.
	who := subject("posted")
	keeper := w.browser()
	keeper.signIn(who)
	var email string
	if err := w.pool.QueryRow(context.Background(), `SELECT email FROM person WHERE mjolnir_subject = $1`, who).Scan(&email); err != nil {
		t.Fatal(err)
	}
	p := admin.post("/people/grant", url.Values{"csrf": {admin.csrf()}, "email": {email}, "role": {"glossary keeper"}, "language": {"ta"}})
	if p.status != 200 || !strings.Contains(p.body, "Invented Clerk, EP-I-1") {
		t.Fatalf("%d %s", p.status, p.body)
	}

	// At the next sign-in, still there: the post is on what they do.
	keeper.get("/")
	again := w.browser()
	again.signIn(who)
	w.exec(`DELETE FROM audit_outbox`)
	p = again.get("/glossary/propose")
	if p.status != 200 {
		t.Fatal(p.status)
	}
	if err := w.denied(again); err != nil {
		t.Fatal(err)
	}
	got := w.outbox()
	if len(got) == 0 || got[len(got)-1].Post != "Invented Clerk, EP-I-1" {
		t.Fatalf("%+v", got)
	}

	// Found earlier and now not found: the role is gone from the first page.
	w.control("bifrost", "not-found", map[string]string{"email": email})
	gone := w.browser()
	gone.signIn(who)
	if p := gone.get("/glossary"); p.status != 403 {
		t.Fatalf("%d", p.status)
	}

	// A failed look-up at sign-in changes nothing, and signing in still works.
	w.control("bifrost", "no_list_held", nil)
	other := subject("posted")
	b := w.browser()
	b.signIn(other)
	if p := b.get("/"); p.status != 200 {
		t.Fatal(p.status)
	}
	if strings.Contains(w.logs.String(), email) {
		t.Fatal("an address is in the log")
	}
}

// denied makes one audit entry as this person: a page their role does not open.
func (w *world) denied(b *browser) error {
	b.get("/people")
	return nil
}

// A person never found keeps the role, flagged on the people and status screens.
func TestANeverFoundPersonIsFlagged(t *testing.T) {
	w := newWorld(t)
	admin, _ := w.signedInAdmin()
	w.withPosts()
	w.control("bifrost", "not-found", nil)
	who := subject("never")
	keeper := w.browser()
	keeper.signIn(who)
	var email string
	if err := w.pool.QueryRow(context.Background(), `SELECT email FROM person WHERE mjolnir_subject = $1`, who).Scan(&email); err != nil {
		t.Fatal(err)
	}
	p := admin.post("/people/grant", url.Values{"csrf": {admin.csrf()}, "email": {email}, "role": {"memory reviewer"}, "language": {"si"}})
	if p.status != 200 || !strings.Contains(p.body, "Not found in the staff list yet") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	if p := admin.get("/status"); !strings.Contains(p.body, email) || !strings.Contains(p.body, "Not found in the staff list yet") {
		t.Fatal(p.body)
	}
	if p := keeper.get("/memory"); p.status != 200 {
		t.Fatalf("they keep the role: %d", p.status)
	}
	// The first time they are found the flag clears.
	w.control("bifrost", "found", nil)
	again := w.browser()
	again.signIn(who)
	if p := admin.get("/status"); strings.Contains(p.body, email) {
		t.Fatal("still flagged")
	}
}
