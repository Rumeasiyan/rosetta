# ROSETTA decision log

Build decisions the PRD and backlog do not hold. Newest first.

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

## Language at sign-in

A pick already in the language cookie wins and is stored on the person; otherwise the stored choice applies, and a first-time person takes the browser's language. `person.screen_language` cannot otherwise tell "never chose" from English.

## Stand-ins answer as their owners document; ROSETTA's clients come with the stories that call them

One `cmd/rosetta-standin` serves BUMBLEBEE, MJOLNIR, CERBERUS, BIFROST, PENSIEVE and HEDWIG under their own path prefixes on one port, with `/control` for failure modes and read-backs. E-ENB-03 gives their addresses and keys as settings (`internal/config`); the Go interface and client for each service arrives with the first story that calls it, so nothing here guesses at a shape. TVA's stand-in is the folder `dev/tva`. Read-backs hold the text sent, since invented text is all that may reach a stand-in, but never a key or secret. The stand-in's masking pattern is a control setting until E-MSK-01 fixes the stand-in's form. Its translation is the text with a `[ta] ` mark, so a test can tell it from the original (R-TRN-09).

## One database role

One role, `rosetta`, as the platform template gives (allspark `apps/_template/database.sql`), not an owner and service pair. Nothing in ROSETTA needs an append-only table the service cannot change.

## Job names

`nightly`, `partitions`, `daily` and `audit-catch-up`. The TVA refresh runs inside `daily`, since flags depend on it.

## The screens' design is approved

The owner picked direction `stone` in `design-lab/`, "Stone bands": three flat bands of stone, olive and brown, one for each language, on warm paper; a heavy serif for terms; a plain system font for everything else; outlined pill controls. It takes its layout from a reference the owner chose, in ROSETTA's own colours. Tables stack at phone width. Screens copy `design-lab/src/directions/stone/`. Fonts are served from ROSETTA itself: nothing outside (R-NFR-11).

## Grammar reads who is signed in from CERBERUS, not ROSETTA's own session

An officer working in the editor may never have opened rosetta.epc.lk, so holds no ROSETTA cookie: it is host-only, set after ROSETTA's own sign-in. Only MJOLNIR's cookie, on the parent domain, can reach the check, CERBERUS checks it, and Caddy removes it. So the grammar paths take the person from `X-Cerberus-Account`, which only Caddy sets, and screens keep ROSETTA's own session. A system on the platform network could forge that header on the grammar path; it gains only a check counted to that person, never the screens. This changes Annex B link 2 and depends on the owner's decision on `docs/plugin-findings.md`.
