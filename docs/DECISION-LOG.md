# ROSETTA decision log

Build decisions the PRD and backlog do not hold. Newest first.

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
