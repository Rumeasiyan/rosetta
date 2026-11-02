# ROSETTA decision log

Build decisions the PRD and backlog do not hold. Newest first.

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
