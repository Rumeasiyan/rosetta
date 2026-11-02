package api_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/jackc/pgx/v5"
)

func callersDoc(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../docs/CALLERS.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The page says what the code does: every reason with its status, every limit with its figure.
func TestCallersPageHoldsEveryReasonAndLimit(t *testing.T) {
	doc := callersDoc(t)
	for _, r := range reasons.All {
		if !strings.Contains(doc, fmt.Sprintf("| `%s` | %d |", r, r.Status())) {
			t.Errorf("no line for reason %q with status %d", r, r.Status())
		}
	}
	for s, figure := range map[settings.Int]string{
		settings.PassagesMax: "200, a setting", settings.PassageCharsMax: "5,000, a setting", settings.RequestCharsMax: "20,000, a setting", settings.GrammarCharsMax: "20,000, a setting",
		settings.CallerPerMinute: "60 to start", settings.CallerAtOnce: "5 to start", settings.RequestLimitSeconds: "60 seconds, a setting",
	} {
		line := figure + " "
		if !strings.Contains(doc, line) && !strings.Contains(doc, figure+", ") {
			t.Errorf("no figure %q", figure)
		}
		if !strings.Contains(doc, "`"+s.Name+"`") {
			t.Errorf("no setting %s", s.Name)
		}
		digits := strings.ReplaceAll(strings.Fields(figure)[0], ",", "")
		digits = strings.TrimSuffix(digits, ",")
		if digits != fmt.Sprint(s.Default) {
			t.Errorf("%s: the page says %s, the default is %d", s.Name, digits, s.Default)
		}
	}
	if !strings.Contains(doc, "caller-to-rosetta") || !strings.Contains(doc, "(R-ADM-07)") {
		t.Error("the signature's tag or the rule on changes is missing")
	}
}

var goBlock = regexp.MustCompile("(?s)```go\n(.*?)```")

// The example on the page runs as it stands, signed and with a key on this server, through the real
// path against the stand-ins, and a signature made by its code passes internal/apisign.
func TestTheExampleOnThePageRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the Go toolchain")
	}
	w := newPathWorld(t)
	w.open("en", "ta")
	ts := httptest.NewServer(w.mux)
	t.Cleanup(ts.Close)
	m := goBlock.FindStringSubmatch(callersDoc(t))
	if m == nil {
		t.Fatal("no Go example on the page")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte(m[1]), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(env ...string) string {
		t.Helper()
		cmd := exec.Command("go", "run", file)
		cmd.Env = append(os.Environ(), env...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return string(out)
	}
	// With a key that signs nothing, on this server.
	out := run("ROSETTA_URL="+ts.URL, "ROSETTA_KEY="+w.key)
	wantRun(t, out)

	// With a key that signs.
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var system int64
	if err := w.pool.QueryRow(ctx, `SELECT caller_id FROM caller_key WHERE key_hash = $1`, callers.HashKey(w.key)).Scan(&system); err != nil {
		t.Fatal(err)
	}
	var signed string
	if err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		var err error
		signed, _, err = callers.Issue(ctx, tx, system, w.person, "ed25519-pub:"+base64.StdEncoding.EncodeToString(pub))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	out = run("ROSETTA_URL="+ts.URL, "ROSETTA_KEY="+signed, "ROSETTA_PRIVATE_KEY="+base64.StdEncoding.EncodeToString(priv))
	wantRun(t, out)

	// A signed key with no signature is refused, and the example says so by reason.
	out = run("ROSETTA_URL="+ts.URL, "ROSETTA_KEY="+signed)
	if !strings.Contains(out, `refused, reason "no key"`) {
		t.Fatal(out)
	}
}

func wantRun(t *testing.T, out string) {
	t.Helper()
	for _, want := range []string{`GET /api/v1/gate: {"directions"`, `POST /api/v1/translate: {"request_ref"`, `"glossary_version"`, `POST /api/v1/memory: {"request_ref"`, `"pieces":1`, `POST /api/v2/check: {"software"`, `"offset":3`, `GET /api/v2/languages: [`} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}
