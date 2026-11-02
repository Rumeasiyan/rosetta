# ROSETTA decision log

Build decisions the PRD and backlog do not hold. Newest first.

## Source order and switch

With no order set, translation goes by source name, so the order never depends on how the list was written, and grammar by English benchmark score (R-GRM-06). Sources the administrator placed come first and the rest follow. A position left blank is not placed, and only the numbers' order counts. A system owner sees the source screen, since R-NFR-13 means them to choose, but changes nothing; the history is the administrator's. A source turned off keeps its place in the form. Changes are `change` entries, as P-ING-02 has no nearer word.

## Keys

A key is `rst_`, twelve hex characters and 256 random bits; the first sixteen characters are the prefix lists show and `keyid` carries. It is stored as SHA-256: with that many random bits there is nothing to guess, so a slow hash would only cost every request. The two-live rule holds the system's row while counting. The refused key's denial names the fixed route `/api/`, the system if the key told which and otherwise the actor `unidentified caller`, with fixed words why. The system and prefix go in `object_ref`, which PENSIEVE does not scan, since a TVA reference could look like anything. Counts by reason are in memory until E-ADM-04 shows them: a request with no known key has no calling system, so a usage record cannot hold it. A request that a database fault stops from being checked answers "no provider", not "no key".

## The signature's form on /api/

PARADOX's profile with one parameter added: `rosetta=("@method" "@path" "content-digest");created=<unix>;keyid="<prefix>";nonce="<nonce>";alg="ed25519";tag="caller-to-rosetta"`, nothing else accepted. The label and tag are this build's choice, so a signature made for PARADOX can never pass here, nor one for here pass there. A nonce is 16 to 128 of `A-Za-z0-9._~-`. Only a verified signature's nonce is remembered, so nobody can fill the memory or spend a caller's nonce without its key. After a restart a request still inside its window could pass once more, which is safe (R-TRN-05). E-CAL-04 publishes this. A key with no public key is not asked to sign, wherever the call came from: nothing here tells whether it arrived through Cloudflare, so a key issued without a public key would also work from outside if it leaked. Issue with a public key for any caller off this server.

## The masking stand-in's form

`⟦M1⟧`, `⟦M2⟧`: brackets no language here writes, the form BUMBLEBEE's stand-in already looked for, and room for the glossary's `⟦G1⟧` in E-GLO-04. Numbers are counted from one within a piece (`Slice`), so the same sentence with another number in it is the same piece for the cache, and a number the caller wrote as a stand-in is skipped. Restoring is strict: each stand-in exactly once, and any other of that shape the text did not hold is a change, so a source that repeats one fails with "masking lost" rather than guessing. What counts as a number is written in `internal/mask`: digits of any script, single spaces, hyphens or dots between them, a Sinhala or Tamil letter joined to the end does not stop it, a Latin letter does. Twelve digits are a card, ten beginning 0 and the `94` forms a phone. A reference in those shapes gets masked too, which does no harm. Whether the real sites keep `⟦M1⟧` is unverified until E-ENB-18.

## How the source list is read

`internal/sources` Book reads the list on the in-process timer AGENTS C6 allows, twice within the `source_list_seconds` setting (R-PRV-01), so one failed read does not make it stale. A list older than the setting is never used to send anything, with no read on the way: while BUMBLEBEE is down a request fails at once (R-NFR-09) rather than waiting for it. The size is the list's `limits.max_text_chars`, then the last one known, and 5,000 code points only if it was never read, which sends nothing anyway since nothing is fresh. A source gone from the list stays in the table as `off`, since gate decisions and orders refer to it. The stand-in gains a second translation source, `standin-translate-b`, and a control param `source` that keeps a failure mode to one source, so fallback can be tested and walked.

## Where the reasons' messages live, and their statuses

R-ERR-02 says these messages are approved texts in the memory. The memory comes with R4, so for now they sit in the one message file, best effort like every other word there, and E-MEM-01 can seed them once a person has checked them. The HTTP status for each reason is this build's choice, written in `internal/reasons`: no key 401, over limit 429, invalid request 400, too long 413, unsupported 422, human translation required 409, no provincial provider 403, masking lost 502, no provider 503, timed out 504. Callers decide on the reason. A position counts from 0, as the answer's list does. ROSETTA failing inside itself, such as losing its database, has no reason of its own on the fixed list, so it answers "no provider" with the fault in the log only.

## Registering a system

Only the administrator registers (s2). TVA's file is read when the form is sent and before the transaction opens, so a slow read holds no connection of the five. The name kept is the English one, else whichever TVA has, at most 200 characters. The owner's post is kept as TVA's reference: the title has three languages and Annex A one column. The copies refresh in E-CAL-05's daily pass, so until then an owner who changes in TVA changes here only by that pass. A system that does not exist looks refused to anyone but the administrator, so an owner learns nothing about other systems. Registering is PENSIEVE's `creation` action, since P-ING-02 has no word nearer.

## How the audit outbox sends

B 13 says straight after each change and from the outbox. After a change commits, a goroutine (never a timer, AGENTS C6) sends what is due, one at a time, with a 15 second limit, and the person's request does not wait for it. What cannot go waits for `audit-catch-up`, which PARADOX calls. A sent entry's row is deleted. A failed attempt backs off from one minute to one hour, or honours `Retry-After`; `not_kept` counts as failed. Any other refusal is a fault in the code that wrote the entry, since the same again gets the same answer (INTAKE.md): the row stays, marked in `refused_at` and `refusal` (migration 002, additions Annex A does not list), is not sent again, and is logged as an error with its ID and reason only. A batch is 100 entries, split further if PENSIEVE answers 413. A row older than 30 days is deleted (Annex A: at most 30 days, and PENSIEVE would refuse it) and the count logged as an error. Telling the administrator about refused or dropped rows waits for E-ADM-06, so until then only the log shows them.

## Which settings exist in R1

E-ADM-03 registers three: `request_limit_seconds` (R-ERR-03), `caller_idle_days` (R-CAL-05) and `failure_alert_percent` (R-ADM-03), so the screen, history and refresh have something real to work on. Nothing reads them yet: their stories do, and add the rest as they need them. Defaults are the PRD's; the allowed ranges are this build's own choice. A setting at its default has no row. A change is in force on the first use after 30 seconds, read on use with no timer, and the last good values stay if a read fails. The screen shows both the saved value and the one in force. A refused value is never repeated, kept or logged.

## Roles

The administrator names a person by the email they last signed in with, rather than picking from a list: every officer who ever signed in could otherwise outgrow a page (R-NFR-03). The role is still held by their account. The last administrator cannot be ended, since nobody could then grant anything. An end date counts through its whole day in Sri Lanka. A role reaching its end date stops being live with no audit entry until E-CAL-05 adds a daily pass. If every administrator's end date passes, the bootstrap setting works again, as it only asks whether one exists.

## Audit entries are written from E-ADM-02 on

`internal/audit` writes the outbox row in the same transaction as the work, so roles and settings have their entries from the start; E-REC-01 adds the sending. The row's `details` column holds the whole entry, since Annex A gives it as who, what and when and has no column for most of what P-ING-01 asks. A denial names the route (`GET /people`), never the address as sent. A bad form token is also a denial. Reasons are fixed words. Only a signed-in person's refusal is recorded: without a session nobody has been identified, and CERBERUS answers for those.

## How sign-in holds a session

Sessions live in process memory, as PENSIEVE's do: Annex A has no session table, and after a restart a person passes through MJOLNIR with no form. ROSETTA holds no copy of MJOLNIR's limits: a session lasts until MJOLNIR refuses a renewal (`invalid_grant` or `access_denied`). `ForgetAfter`, 12 hours unseen, only keeps memory small. On any other answer from MJOLNIR nobody is let through and the session is kept. A request with `X-Timer-Request` neither renews nor counts as the person being seen. A renewal outlives the person's request, because a rotated token cannot be sent twice. This goes beyond PENSIEVE, whose client has no renewal.

## The Secure flag comes from ROSETTA_PUBLIC_URL

PENSIEVE decides from `X-Forwarded-Proto`. ROSETTA decides from its own setting, so nothing a proxy sends or fails to send can turn it off. Plain http is accepted only for an address on this machine, and the service refuses to start otherwise.

## ROSETTA_SIGNIN_INTERNAL_URL

The issuer must read the same to the browser and to ROSETTA, and locally and on the platform network the two reach MJOLNIR differently (B 8). This optional setting moves the addresses ROSETTA calls itself; the issuer is compared exactly as published, trailing slash included. PENSIEVE instead runs from source to sign in locally. How it behaves against the real MJOLNIR is unverified until E-ENB-08.

## Sign-ins and sign-outs are not sent to PENSIEVE

P-ING-02: MJOLNIR records sign-ins and sends each to PENSIEVE, and no other system keeps a second sign-in history. So ROSETTA sends neither, and keeps only `last_seen_at`, moved at sign-in and at each renewal.

## Screens live at the root

`/`, `/signin`, `/people` and so on, since not every screen is for an administrator: `/api/`, `/jobs/` and `/healthz` are the other parts. Cookies have `Path=/` and no `Domain`.

## The Sinhala and Tamil words are best effort

`internal/i18n/messages.go` holds them, written without a native reader. A person must check them before Go live. A test holds only that each is present and in its own script.

## Language at sign-in

A pick already in the language cookie wins and is stored on the person; otherwise the stored choice applies, and a first-time person takes the browser's language. `person.screen_language` cannot otherwise tell "never chose" from English.

## Stand-ins answer as their owners document; ROSETTA's clients come with the stories that call them

One `cmd/rosetta-standin` serves BUMBLEBEE, MJOLNIR, CERBERUS, BIFROST, PENSIEVE and HEDWIG under their own path prefixes on one port, with `/control` for failure modes and read-backs. E-ENB-03 gives their addresses and keys as settings (`internal/config`); the Go interface and client for each service arrives with the first story that calls it, so nothing here guesses at a shape. TVA's stand-in is the folder `dev/tva`. Read-backs hold the text sent, since invented text is all that may reach a stand-in, but never a key or secret. The stand-in's masking pattern is a control setting until E-MSK-01 fixes the stand-in's form. Its translation is the text with a mark such as `[ta]` in front, so a test can tell it from the original (R-TRN-09).

## One database role

One role, `rosetta`, as the platform template gives (allspark `apps/_template/database.sql`), not an owner and service pair. Nothing in ROSETTA needs an append-only table the service cannot change.

## Job names

`nightly`, `partitions`, `daily` and `audit-catch-up`. The TVA refresh runs inside `daily`, since flags depend on it.

## The screens' design is approved

The owner picked direction `stone` in `design-lab/`, "Stone bands": three flat bands of stone, olive and brown, one for each language, on warm paper; a heavy serif for terms; a plain system font for everything else; outlined pill controls. It takes its layout from a reference the owner chose, in ROSETTA's own colours. Tables stack at phone width. Screens copy `design-lab/src/directions/stone/`. Fonts are served from ROSETTA itself: nothing outside (R-NFR-11).

## Grammar reads who is signed in from CERBERUS, not ROSETTA's own session

An officer working in the editor may never have opened rosetta.epc.lk, so holds no ROSETTA cookie: it is host-only, set after ROSETTA's own sign-in. Only MJOLNIR's cookie, on the parent domain, can reach the check, CERBERUS checks it, and Caddy removes it. So the grammar paths take the person from `X-Cerberus-Account`, which only Caddy sets, and screens keep ROSETTA's own session. A system on the platform network could forge that header on the grammar path; it gains only a check counted to that person, never the screens. This changes Annex B link 2 and depends on the owner's decision on `docs/plugin-findings.md`.
