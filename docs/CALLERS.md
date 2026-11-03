# Calling ROSETTA

How a system uses ROSETTA: every request, answer, reason and limit (R-CAL-08). This is the one shape of ROSETTA's API. The EP website, SCOS Pro and every other caller read this page and adapt to it; none gets a form of its own.

Text is yours to judge. You decide what you send and mark how sensitive it is, and you answer for that under the Personal Data Protection Act (R-NFR-11). Machine translation is a draft for a person to check, never official text.

## First answer, on your own machine

Everything here runs against stand-ins; nothing reaches a real website. Use invented text only, here and in any test.

```sh
docker compose -f dev/compose.yaml up -d postgres pgbouncer
dev/keys.sh
set -a; . ./.env.example; . dev/keys/paradox.env; set +a
go run ./cmd/rosetta migrate
go run ./cmd/rosetta-standin &
go run ./cmd/rosetta serve &
```

Sign in at <http://localhost:53080> as `dev-admin` at the stand-in's form. Register the system `TEST-0001` on `/systems`, and open English to Tamil for `standin-translate` on `/gate`. Save the example at the end of this page as `main.go` in an empty folder, then:

```sh
go run main.go keygen                      # prints a public key, and a private key to keep
```

Paste the public key on the system's screen and issue a key; it is shown once. Then:

```sh
export ROSETTA_URL=http://localhost:53080
export ROSETTA_KEY=rst_...
export ROSETTA_PRIVATE_KEY=...             # from keygen; leave it out for a key with no public key
go run main.go
```

## Where to call

| You are | Call | With |
|---|---|---|
| On another machine | `https://rosetta.epc.lk/api/...`, through Cloudflare (B 3) | Your key, and every request signed |
| On the same server | `/api/...` on port 8080, directly on the platform network (B 7) | Your key; a key with no public key signs nothing, and is refused if it comes through the front door |
| An officer's browser, for grammar only | `https://rosetta.epc.lk/api/v2/...`, from an address the administrator lists (R-CAL-06) | The officer's own sign-in |

Wait as long as the request limit below, plus a little: every request is answered or timed out within it.

## The key

Send `Authorization: Bearer rst_...` on every call. The administrator, or your system's owner, issues it on your system's screen, and it is shown once. Keep it with your other secrets, never in code or a log. ROSETTA holds only a hash, so a lost key cannot be read back: issue another. You may hold two live keys while changing them: issue a new one, switch, then turn the old one off (R-CAL-03). A system must be `live` or `may_join` in TVA's register to call at all (R-CAL-01).

## The signature

A key with a public key signs every request, with Ed25519, as RFC 9421 gives it. It is the profile in PARADOX's contract (`sign/v0.2.0`) with a nonce added. These three headers go with each request, and only exactly this form is accepted:

```text
Content-Digest: sha-256=:<base64 of the SHA-256 of the body>:
Signature-Input: rosetta=("@method" "@path" "content-digest");created=<unix seconds>;keyid="<your key's first 16 characters>";nonce="<nonce>";alg="ed25519";tag="caller-to-rosetta"
Signature: rosetta=:<base64 of the signature>:
```

What is signed is these four lines, joined by single newlines, with none at the end. The last line is everything after `rosetta=` in `Signature-Input`.

```text
"@method": POST
"@path": /api/v1/translate
"content-digest": sha-256=:<base64>:
"@signature-params": ("@method" "@path" "content-digest");created=<unix seconds>;keyid="<prefix>";nonce="<nonce>";alg="ed25519";tag="caller-to-rosetta"
```

- The method is in capitals, and an empty body still has its digest.
- A signed call carries no query. `GET /api/v1/gate` answers every direction for that reason.
- `created` may be up to 5 minutes from ROSETTA's clock, either way.
- A nonce is 16 to 128 of `A-Z a-z 0-9 . _ ~ -`, new for each request, sent again or not. A nonce seen already is refused.
- The public key is `ed25519-pub:` and the standard base64 of its 32 bytes; you paste it when the key is issued. Never paste a private key: it is refused.

Anything wrong with the signature answers `no key`, and is recorded as a denial in the provincial audit record. The answer does not say which part was wrong.

## Translating: `POST /api/v1/translate`

```json
{"from": "en", "to": "ta", "sensitivity": "ordinary", "passages": ["An invented letter.", "A second invented line."]}
```

| Field | Takes |
|---|---|
| `from`, `to` | `si`, `ta` or `en`, written so; never the same. Each of the six directions has its own gate |
| `sensitivity` | `ordinary`, `personal` or `confidential`. There is no default: leaving it out is `invalid request` (R-TRN-03) |
| `passages` | A list, at most as long as the limits below |

Fields not named here are ignored, so a newer caller does not break an older ROSETTA.

| Sensitivity | What happens (R-MSK-01) |
|---|---|
| `ordinary` | May go to any source; translated pieces are cached. Text holding an identity card number is treated as `personal`, the whole request with it, and the mismatch counts to you (R-MSK-04) |
| `personal` | May go to any source after masking; never cached, and never kept after the answer |
| `confidential` | Refused with `no provincial provider`: no provider runs on provincial machines yet |

Identity card and phone numbers are masked before text leaves the shared server, whatever you state (R-MSK-02). Masking is a help, not a guarantee: anything it does not recognise goes as written. Every source is an outside public website, and keeps or uses what it receives under its own terms (R-NFR-13).

A request is all or nothing. Either every passage comes back in the same order, or one failure names the position of the passage at fault (R-TRN-02). Sending the same request again gives the same answer while the glossary, memory and cache are unchanged, so retrying is safe (R-TRN-05). Glossary terms and the memory answer whatever the gate says (R-GAT-02), and while BUMBLEBEE or every source is down everything else fails at once with its reason (R-NFR-09). Your system must keep working without machine translation.

### The answer

```json
{
  "request_ref": "r-...",
  "from": "en", "to": "ta", "sensitivity": "ordinary", "sensitivity_mismatch": false,
  "glossary_version": 12, "fallback_used": false,
  "passages": [{
    "text": "...", "language": "ta",
    "origins": {"glossary": 0, "memory": 0, "cache": 0, "source": 1},
    "pieces": [{"origin": "source", "source": "some-site", "version": "3", "fallback_used": false}],
    "sources": [{"name": "some-site", "version": "3"}],
    "machine": true, "checked_by_person": false,
    "label": {"si": "...", "ta": "...", "en": "Machine translation, check before issue"},
    "fallback_used": false, "glossary_version": 12
  }]
}
```

| Item | Meaning |
|---|---|
| `request_ref` | The reference for this request. It is in every failure too, and goes with every call to a source, so records can be matched |
| `sensitivity`, `sensitivity_mismatch` | How the text was treated, and whether that differs from what you stated |
| `glossary_version` | The glossary version used. Keep it with the letter: past versions can be read on the screens (R-GLO-05) |
| `fallback_used` | A source earlier in the order was resting or failed |
| `passages[].language` | The language of `text`, for screen readers (R-NFR-05) |
| `passages[].origins` | Pieces from each of the four places, each always there |
| `passages[].pieces` | Each piece in order: its origin, and for `source` or `cache` the source and version |
| `passages[].machine` | A machine made any of it |
| `passages[].checked_by_person` | Wholly from the glossary or memory |
| `passages[].label` | Only where `machine` is true. Show it beside the text, in the reader's language (R-TRN-07) |
| `passages[].terms_not_placed` | Only when a source lost a glossary term: each as published, for the reader to place (R-GLO-03) |

## A long translation: background runs

A direct request must finish inside the request limit. Personal text is never cached, so a long personal request that times out starts again each time and may never finish. A background run takes the same request, answers straight away, and works for up to the run limit while you ask after it (R-TRN-10). Direct requests stay. Only systems call this, server to server; never an officer's browser. There is no call back: you ask.

| Call | Does |
|---|---|
| `POST /api/v1/translate/runs` | Starts a run. Takes exactly the body of `POST /api/v1/translate` |
| `GET /api/v1/translate/runs/{run}` | Says how it goes. Once it has ended, hands the answer over, once |
| `DELETE /api/v1/translate/runs/{run}` | Gives it up: it is forgotten at once |

All three use your key and are signed as every call is. A `GET` or `DELETE` has no body, so what is signed is the digest of the empty body, which must still come: `Content-Digest: sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:`.

### Starting

Answered `202`:

```json
{"run": "run-...", "state": "waiting", "ask_again_at": "<RFC 3339>"}
```

`state` is `waiting` or `working`, as in the table below. The reference is random. The request is checked at once and refused as a direct request would be, with the same reason, status and position: `invalid request`, `too long`, `no provincial provider`, `human translation required`, `no provider` while nothing could be called, and `no key`. Past the limits on runs below it is `over limit`, with `retry_at`. Glossary terms, the memory and the cache are read when you start, under the glossary version the answer will state.

### Asking

While it works, answered `200`, and never any text:

```json
{"state": "working", "pieces_done": 12, "pieces_total": 40, "ask_again_at": "<RFC 3339>"}
```

| `state` | Means |
|---|---|
| `waiting` | Taken on, waiting its turn: only a few runs are worked at a time |
| `working` | Pieces are going to a source |

Keep to `ask_again_at`, which is ROSETTA's reckoning from the pieces so far. Asking counts against neither your requests a minute nor your requests at once (R-CAL-07), and neither does giving up. Only the `POST` that starts a run counts, and only while it is answered. An ask leaves no usage record of its own.

An answer with `state` is still going; an answer without it is the end. Once a run has ended, the next ask answers exactly what `POST /api/v1/translate` would have answered, status and body: `200` and [the answer](#the-answer), or the failure's status and [its body](#failures), with `position` and `reason` (R-TRN-11). Its `request_ref` is the run's own, which every call to a source carried and its usage record holds. All of that is handed over once and then forgotten, so keep it before you do anything else with it; a connection lost during that answer loses the answer, and you send the request again (R-TRN-05).

### Giving up

`DELETE` answers `204` with no body, and the run is forgotten at once. Pieces already sent to a source cannot be called back. A second `DELETE` or an ask after it is `run not known`.

### When it is not known

`run not known` is `404`. It means any of these, and the answer does not say which: the run was handed over, given up, not collected in time, or lost when ROSETTA restarted (R-NFR-08), or the reference is not one of your system's. Another system's run reads just like one that was never there. Send the request again (R-TRN-05). Ordinary text picks up what the cache kept; personal text starts from the beginning.

### Holding

A run's text, its masks and its answer are held only in the service's memory, never in the database, on a disk, in a record or in a log (R-TRN-13). They go when the answer is handed over, when you give up, when the uncollected limit passes, and at any restart. Personal text stays uncached; confidential text is refused as ever (R-MSK-01). Ordinary text goes through the translation cache as a direct request's does (R-TRN-08), its numbers masked. A graceful stop loses every run just as a crash does. A run is one usage record, written when it ends, however it ends; a run lost to a restart leaves none (R-REC-01).

## Sending approved text: `POST /api/v1/memory`

Text a person approved in your system joins the shared memory, and answers every system from then on (R-MEM-01, R-MEM-02). Your own record of the approval stays the authoritative one.

```json
{"from": "en", "to": "ta", "sensitivity": "ordinary", "approver_post": "An invented post", "approved_on": "<YYYY-MM-DD>",
 "passages": [{"source": "An invented letter.", "approved": "..."}]}
```

| Field | Takes |
|---|---|
| `sensitivity` | `ordinary` only. Anything else is `invalid request`, and nothing is kept (R-MEM-03) |
| `approver_post` | 1 to 200 characters, as your system knows the post. Not a person's name, and no identity card number |
| `approved_on` | Optional. Today in Sri Lanka if left out, and never ahead of it |
| `passages` | Each a `source` and an `approved`, with the same limits as a translation |

A card number on either side refuses the whole request as `invalid request`, with the passage's position. The two sides must hold the same numbers, however written, and the same number of sentences, or that passage is left out and named in `not_kept`; the others still join.

```json
{"request_ref": "r-...", "from": "en", "to": "ta", "glossary_version": 12, "pieces": 3, "not_kept": [1]}
```

`pieces` is how many joined. A newer approval of the same piece replaces the older.

## Reading the gate: `GET /api/v1/gate`

So a closed direction can offer human translation instead of a broken button (R-GAT-04). It answers all six directions:

```json
{"directions": [
  {"from": "en", "to": "ta", "state": "open", "evaluated_on": "<YYYY-MM-DD>",
   "sources": [{"name": "some-site", "version": "3", "evaluated_on": "<YYYY-MM-DD>"}]},
  {"from": "si", "to": "ta", "state": "closed", "sources": []}
], "request_ref": "r-..."}
```

## Checking grammar: `POST /api/v2/check` and `GET /api/v2/languages`

The public form of LanguageTool's API version 2, so the editor's plug-in works unchanged (R-GRM-01). A check is `application/x-www-form-urlencoded`, with `language` and `text`:

```text
language=en-GB&text=We+recieve+letters.
```

`auto`, `en` and any `en-*` read as English. `si` and `ta` are listed as not offered, and refused with `unsupported` (R-GRM-02). Leaving `language` out is `invalid request`. An empty text answers an empty list. The text is kept nowhere (R-GRM-05).

```json
{"software": {"name": "ROSETTA", "version": "...", "apiVersion": 1},
 "language": {"name": "English", "code": "en", "detectedLanguage": {"name": "English", "code": "en"}},
 "matches": [{"message": "Possible spelling mistake", "shortMessage": "", "offset": 3, "length": 7,
   "replacements": [{"value": "receive"}],
   "context": {"text": "We recieve letters.", "offset": 3, "length": 7},
   "rule": {"id": "ROSETTA_SPELLING", "description": "spelling", "issueType": "misspelling", "category": {"id": "SPELLING", "name": "spelling"}},
   "source": "some-site"}],
 "request_ref": "r-...", "fallback_used": false, "sources": [{"name": "some-site", "version": "2"}]}
```

`offset` and `length` are in UTF-16 units of the text exactly as you sent it, also after masking and splitting. A suggestion gives at most 3 replacements, and one on a glossary term is dropped (R-GRM-04). `source`, `request_ref`, `fallback_used` and `sources` are additions the plug-in ignores.

`GET /api/v2/languages` answers a list:

```json
[{"name": "English", "code": "en", "longCode": "en-GB", "offered": true},
 {"name": "Sinhala (not offered)", "code": "si", "longCode": "si-LK", "offered": false},
 {"name": "Tamil (not offered)", "code": "ta", "longCode": "ta-LK", "offered": false}]
```

### From an officer's browser

The address must be listed for your system by the administrator, compared exactly, and the officer signed in. CORS names that one address with credentials allowed; a preflight goes through with no session and uses no limit. A refusal to a browser is plain text in the language of `Accept-Language`, with the reason in `X-Rosetta-Reason` and the reference in `X-Rosetta-Request-Ref`. The call counts to the system that lists the address.

## Failures

One reason from a fixed list, a message in three languages for the officer, and when to try again where it helps (R-ERR-01, R-ERR-02). An error never holds the text you sent.

```json
{"request_ref": "r-...", "error": {"reason": "no provider", "messages": {"si": "...", "ta": "...", "en": "..."},
  "position": 2, "retry_at": "<RFC 3339>", "glossary_terms": ["..."]}}
```

Decide on `reason`, not on the HTTP status or the words. `position` counts from 0 and comes when one passage is at fault. `retry_at`, with a `Retry-After` header, comes where it helps. `glossary_terms` comes only with `human translation required`, to help the translator.

| `reason` | HTTP | Means | What to do |
|---|---|---|---|
| `no key` | 401 | No key, an unknown or turned-off key, a bad signature, a key with no public key from outside, or a system no longer live or may join. Recorded as a denial | Fix the key or the signature. Do not retry as it stands |
| `over limit` | 429 | Past your requests a minute or at once, or the limits on runs. Not a denial. `retry_at` says when | Wait, then send again |
| `invalid request` | 400 | Not a request ROSETTA can read: a field missing or wrong, an address that is not there | Fix the request |
| `too long` | 413 | Past a limit below; `position` if one passage is at fault | Send less |
| `unsupported` | 422 | A language not offered for a check | Do not retry |
| `human translation required` | 409 | The pieces need a source, and this direction is closed for every allowed source. `glossary_terms` helps the translator | Offer human translation |
| `no provincial provider` | 403 | Confidential text | Do not send it here |
| `masking lost` | 502 | A source lost or changed a stand-in, so the value could not be put back (R-MSK-03) | Send again; if it persists, tell a person |
| `no provider` | 503 | Every allowed source failed, or BUMBLEBEE is down. `retry_at` is the earliest a source is expected | Keep working without it, and try later |
| `timed out` | 504 | Past the request limit below, or a run past its limit | Send again (R-TRN-05) |
| `run not known` | 404 | ROSETTA does not hold this run: handed over, given up, not collected, lost in a restart, or not your system's | Send it again (R-TRN-05) |

## Limits

These are ROSETTA's own, and the Office can change those marked a setting. Each system's requests a minute and at once are on its row, and only the administrator changes them. What BUMBLEBEE holds, such as the size of a piece and the rate at each website, is BUMBLEBEE's to say (`docs/CALLERS.md` in its repository, R-PRV-04); you need not know them, as ROSETTA splits and waits for you.

| Limit | Value | Past it |
|---|---|---|
| Passages in a request | 200, a setting (`passages_max`) | `too long` |
| Characters in a passage | 5,000, a setting (`passage_chars_max`) | `too long`, with `position` |
| Characters in a request | 20,000, a setting (`request_chars_max`) | `too long` |
| Characters in a check | 20,000, a setting (`grammar_chars_max`) | `too long` |
| Body of any request | 4 MiB as sent | `too long` |
| Requests a minute | 60 to start, your system's own (`caller_requests_per_minute`) | `over limit` |
| Requests at once | 5 to start, your system's own (`caller_requests_at_once`) | `over limit` |
| One request in all | 60 seconds, a setting (`request_limit_seconds`) | `timed out` |
| One background run, from when it is taken on, waiting included | 30 minutes, a setting (`run_limit_minutes`) | `timed out`, handed over when you ask |
| Runs at once | 5 for each system, a setting (`runs_per_system`) | `over limit`, with `retry_at` |
| Runs at once | 50 in all, a setting (`runs_in_all`) | `over limit`, with `retry_at` |
| An answer waiting to be collected | 10 minutes, a setting (`run_uncollected_minutes`) | Forgotten, then `run not known` |
| Live keys | 2 for each system | Refused on the screen |
| A signature's time | 5 minutes either way | `no key` |

Characters are Unicode code points; a check's positions are UTF-16 units. A minute is any 60 seconds. A refused request counts against no limit. A run counts against the limits on runs until it is forgotten, so one that has ended still counts until you collect it or give it up.

A system with no request for 90 days (a setting, `caller_idle_days`) is flagged to the administrator, and so is one no longer live or may join (R-CAL-05).

## What ROSETTA keeps

A usage record of every request: your system, the officer if signed in, the operation, sensitivity, languages, counts, sources, time taken, outcome and reference (R-REC-01). Never the text or the answer. A background run is one request with one record, written when it ends; its outcome is `given up` if you gave it up. Your system's owner reads it on `/usage`. Refused keys go to the provincial audit record as denials; usage stays in ROSETTA.

## When this page changes

Changes only add to what exists (R-ADM-07). A change that would break a caller runs beside the old way for at least 6 months, a setting, and every system owner is told. So read answers leniently: ignore fields you do not know. A later provider joins behind ROSETTA with no change in any caller (R-NFR-14).

## A copy-and-run example

Standard library only. It reads the gate, translates directly and as a background run, adds to the memory and checks grammar, signing when `ROSETTA_PRIVATE_KEY` is set, and shows each failure's reason. A failed call is printed, not fatal, since a closed gate is an answer.

<!-- markdownlint-disable MD010 -->
```go
// Command main calls ROSETTA as docs/CALLERS.md gives it. Invented text only.
//
//	go run main.go keygen
//	ROSETTA_URL=... ROSETTA_KEY=... ROSETTA_PRIVATE_KEY=... go run main.go
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var std = base64.StdEncoding

// sign sets the three headers, exactly as docs/CALLERS.md gives them. The key's prefix is its first
// 16 characters. body must be the very bytes that are sent.
func sign(r *http.Request, body []byte, apiKey string, key ed25519.PrivateKey) {
	sum := sha256.Sum256(body)
	digest := "sha-256=:" + std.EncodeToString(sum[:]) + ":"
	nonce := make([]byte, 16)
	rand.Read(nonce)
	params := fmt.Sprintf(`("@method" "@path" "content-digest");created=%d;keyid="%s";nonce="%s";alg="ed25519";tag="caller-to-rosetta"`,
		time.Now().Unix(), apiKey[:16], hex.EncodeToString(nonce))
	base := strings.Join([]string{
		`"@method": ` + r.Method,
		`"@path": ` + r.URL.EscapedPath(),
		`"content-digest": ` + digest,
		`"@signature-params": ` + params,
	}, "\n")
	r.Header.Set("Content-Digest", digest)
	r.Header.Set("Signature-Input", "rosetta="+params)
	r.Header.Set("Signature", "rosetta=:"+std.EncodeToString(ed25519.Sign(key, []byte(base)))+":")
}

// send makes one call and gives its status and body.
func send(method, path, contentType string, body []byte, apiKey string, key ed25519.PrivateKey) (int, []byte, error) {
	r, err := http.NewRequest(method, strings.TrimRight(os.Getenv("ROSETTA_URL"), "/")+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	r.Header.Set("Authorization", "Bearer "+apiKey)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if key != nil {
		sign(r, body, apiKey, key)
	}
	client := http.Client{Timeout: 70 * time.Second} // past the request limit, so its answer arrives
	res, err := client.Do(r)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	return res.StatusCode, raw, err
}

// call sends one request and prints what came back. It says a call could not be made at all as an
// error; a refusal is an answer, read from its reason.
func call(method, path, contentType string, body []byte, apiKey string, key ed25519.PrivateKey) ([]byte, error) {
	status, raw, err := send(method, path, contentType, body, apiKey, key)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK && status != http.StatusAccepted {
		var failure struct {
			RequestRef string `json:"request_ref"`
			Error      struct {
				Reason  string `json:"reason"`
				RetryAt string `json:"retry_at"`
			} `json:"error"`
		}
		json.Unmarshal(raw, &failure)
		fmt.Printf("%s %s: refused, reason %q, try again %q, reference %s\n", method, path, failure.Error.Reason, failure.Error.RetryAt, failure.RequestRef)
		return nil, nil
	}
	fmt.Printf("%s %s: %s\n", method, path, raw)
	return raw, nil
}

// backgroundRun starts a run, keeps to the time it is told, and collects the answer, which is
// handed over once. A second ask then says the run is not known. A second run is started and given up.
func backgroundRun(body []byte, apiKey string, key ed25519.PrivateKey) error {
	const runs = "/api/v1/translate/runs"
	raw, err := call(http.MethodPost, runs, "application/json", body, apiKey, key)
	if err != nil || raw == nil {
		return err
	}
	var started struct {
		Run        string `json:"run"`
		AskAgainAt string `json:"ask_again_at"`
	}
	if err := json.Unmarshal(raw, &started); err != nil {
		return err
	}
	at := started.AskAgainAt
	for {
		next, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return err
		}
		time.Sleep(time.Until(next))
		raw, err := call(http.MethodGet, runs+"/"+started.Run, "", nil, apiKey, key)
		if err != nil {
			return err
		}
		var state struct {
			State      string `json:"state"`
			AskAgainAt string `json:"ask_again_at"`
		}
		if raw != nil {
			json.Unmarshal(raw, &state)
		}
		if state.State == "" { // no state: this was the end, handed over
			break
		}
		at = state.AskAgainAt
	}
	if _, err := call(http.MethodGet, runs+"/"+started.Run, "", nil, apiKey, key); err != nil {
		return err
	}
	raw, err = call(http.MethodPost, runs, "application/json", body, apiKey, key)
	if err != nil || raw == nil {
		return err
	}
	if err := json.Unmarshal(raw, &started); err != nil {
		return err
	}
	status, _, err := send(http.MethodDelete, runs+"/"+started.Run, "", nil, apiKey, key)
	if err != nil {
		return err
	}
	if status == http.StatusNoContent {
		fmt.Printf("DELETE %s/%s: given up, forgotten\n", runs, started.Run)
	}
	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "keygen" {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("public key, for the system's screen: ed25519-pub:" + std.EncodeToString(public))
		fmt.Println("private key, for ROSETTA_PRIVATE_KEY: " + std.EncodeToString(private))
		return
	}
	apiKey := os.Getenv("ROSETTA_KEY")
	if len(apiKey) < 16 || os.Getenv("ROSETTA_URL") == "" {
		fmt.Fprintln(os.Stderr, "set ROSETTA_URL and ROSETTA_KEY")
		os.Exit(2)
	}
	var key ed25519.PrivateKey
	if v := os.Getenv("ROSETTA_PRIVATE_KEY"); v != "" {
		raw, err := std.DecodeString(v)
		if err != nil || len(raw) != ed25519.PrivateKeySize {
			fmt.Fprintln(os.Stderr, "ROSETTA_PRIVATE_KEY is not a private key from keygen")
			os.Exit(2)
		}
		key = raw
	}
	translation, _ := json.Marshal(map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "passages": []string{"An invented letter."}})
	memory, _ := json.Marshal(map[string]any{"from": "en", "to": "ta", "sensitivity": "ordinary", "approver_post": "An invented post",
		"passages": []map[string]string{{"source": "A second invented letter.", "approved": "இரண்டாவது புனைந்த கடிதம்."}}})
	check := url.Values{"language": {"en-GB"}, "text": {"We recieve letters."}}.Encode()
	for _, c := range []struct {
		method, path, contentType string
		body                      []byte
	}{
		{http.MethodGet, "/api/v1/gate", "", nil},
		{http.MethodPost, "/api/v1/translate", "application/json", translation},
		{http.MethodPost, "/api/v1/memory", "application/json", memory},
		{http.MethodPost, "/api/v2/check", "application/x-www-form-urlencoded", []byte(check)},
		{http.MethodGet, "/api/v2/languages", "", nil},
	} {
		if _, err := call(c.method, c.path, c.contentType, c.body, apiKey, key); err != nil {
			fmt.Fprintln(os.Stderr, "the call could not be made:", err)
			os.Exit(1)
		}
	}
	if err := backgroundRun(translation, apiKey, key); err != nil {
		fmt.Fprintln(os.Stderr, "the call could not be made:", err)
		os.Exit(1)
	}
}
```
<!-- markdownlint-enable MD010 -->
