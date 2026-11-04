# ROSETTA - agent working guide

ROSETTA is the translation and grammar service of the Eastern Province, Sri Lanka: the one way every provincial system translates and checks text in Sinhala, Tamil and English, with one shared glossary. Machine output is a draft for a person to check, never official text.

## Where to look

| Path | When you need it |
|---|---|
| `docs/ROSETTA-PRD.md` | **What to build.** Requirements, the stack (section 5), Annex A (data model), Annex B (architecture). |
| `docs/ROSETTA-BACKLOG.md` | **Build order.** Stories citing the PRD. Tick a box when its story is done. |
| `docs/DECISION-LOG.md` | Build decisions the PRD and backlog do not hold. |
| `docs/diagrams/` | `build-erd.py` draws Annex A and writes its dictionary into the PRD; a test checks the schema against that dictionary. |
| `cmd/rosetta/` | The service: `serve`, `migrate`, `evaluate`, `-version`, `-healthcheck`. |
| `cmd/rosetta-paradoxcall/` | Local stand-in for PARADOX. Not in the image. |
| `cmd/rosetta-standin/`, `internal/standin` | Local stand-ins for BUMBLEBEE, MJOLNIR, CERBERUS, BIFROST, PENSIEVE and HEDWIG, each under its own path prefix; `/control` sets a failure mode and reads back what came. Not in the image. |
| `cmd/rosetta-load/` | Seeds capacity and measures against stand-ins (E-NFR-01). Not in the image. `uptime` first: the machine runs many stacks. |
| `internal/db` | PgBouncer-safe pool and migrations (`migrations/NNN-*.sql`, never edited once applied). |
| `internal/jobs` | The job address PARADOX calls: signature, run record, lock. |
| `internal/oidc` | Sign-in through MJOLNIR: code flow with PKCE, renew, revoke. |
| `internal/web`, `internal/i18n` | The screens (Go templates and htmx, the Stone bands look, embedded fonts and scripts) and the one message file. |
| `internal/people` | The person's record and roles. |
| `internal/audit` | The outbox of entries for PENSIEVE: written in the same transaction as the work, sent straight after and by the job `audit-catch-up`. |
| `internal/settings` | The Office's settings: each PRD "(default: ...)" as stories need it, their history, and the read-on-use cache. |
| `internal/config` | Settings from the environment: the listen address, the database, the addresses and keys of other services. |
| `internal/tva`, `internal/callers` | TVA's `systems.json` (format 1), read from its bucket on the server and a folder locally, and the calling systems with their owners. |
| `internal/reasons` | The fixed reasons and how a failure is answered, with messages in three languages. |
| `internal/bumblebee`, `internal/sources` | BUMBLEBEE's client, and what ROSETTA knows of its sources: the list and size limit on a timer. |
| `internal/mask` | Masking and restoring card and phone numbers: pure, with a map to the original offsets. |
| `internal/apisign` | The signature on calls from another machine: PARADOX's profile plus a nonce. |
| `internal/api` | The `/api/` path: key and signature on every request, refusals as denials. |
| `internal/limits` | Requests a minute and at once for each calling system, in memory. |
| `internal/translate` | The piece pipeline: mask, split, glossary, memory, cache, source, join; all or nothing. |
| `internal/runs` | Background runs held in process memory only: limits, a few worked at a time, one timer per run, hand-over, give up, forgotten at any stop. |
| `internal/glossary` | The one shared glossary: entries, keepers' checks, publishing, versions, and telling keepers. |
| `internal/memory` | The shared memory: approved text added, served, found, rechecked, removed. |
| `internal/terms` | Finding glossary terms in a text, and the one normalise function. Pure. |
| `internal/sheet` | One worksheet of an `.xlsx` file, read and written with the standard library. Pure. |
| `internal/usage` | The usage record of every request, without text: kept, added up and removed. Never sent to PENSIEVE. |
| `internal/bifrost`, `internal/posts` | BIFROST's client, and post look-ups: only `found: false` is not found, a failed look-up changes nothing. |
| `internal/flags` | The job `daily`'s transaction: TVA's copies, each system's flag, roles past their date. |
| `internal/alerts` | Alerts by email when a state changes (R-ADM-03): looked at on the source list's timer, kept in `alert`, sent through HEDWIG. |
| `internal/evaluate` | `rosetta evaluate`: a test set against one source, giving the score the gate records. |
| `internal/hedwig` | HEDWIG's client, as its `docs/API.md` gives it. |
| `internal/grammar` | The grammar check: mask, split, place each suggestion in the text sent, join. Nothing of the text is kept. |
| `deploy/` | What the shared server needs from ROSETTA, handed to ALLSPARK: compose, env examples, Caddy site, PgBouncer lines, PARADOX's schedule, HEDWIG's templates (`how-to-deploy.md`). |
| `dev/` | The local stack. Start only what you need; the machine runs many stacks. `dev/tva/` is TVA's stand-in, a local folder. `dev/plugin-test/` walks the ONLYOFFICE plug-in in a real Document Server (`how-to-run.md`). |

## Commands

```sh
docker compose -f dev/compose.yaml up -d postgres pgbouncer
export ROSETTA_TEST_DATABASE_URL=postgres://rosetta:rosetta@localhost:53432/rosetta
go test -p 1 ./...
docker compose -f dev/compose.yaml down
```

To run it and walk the screens from source, with the database up: `set -a; . ./.env.example; . dev/keys/paradox.env; set +a`, then `go run ./cmd/rosetta migrate`, `go run ./cmd/rosetta-standin` and `go run ./cmd/rosetta serve`, then open <http://localhost:53080> and sign in as `dev-admin` at the stand-in's form. Call a job with `go run ./cmd/rosetta-paradoxcall -key dev/keys/paradox.key -url http://localhost:53080 -job audit-catch-up`, and read what arrived with `curl localhost:53190/control/pensieve/received`. To translate: register a system on `/systems`, issue its key there, open a direction on `/gate`, then `POST /api/v1/translate` with `Authorization: Bearer <key>` and `{"from":"en","to":"ta","sensitivity":"ordinary","passages":["An invented letter."]}`, and read what BUMBLEBEE's stand-in was sent with `curl localhost:53190/control/bumblebee/received`. A long translation goes to `POST /api/v1/translate/runs`, asked after on `GET /api/v1/translate/runs/{run}` (`docs/CALLERS.md`). To check grammar as a browser would: list an address on a system's screen, then `curl localhost:53080/api/v2/check -H 'Origin: <address>' -H 'X-Cerberus-Account: dev-officer' -d 'language=en-GB' --data-urlencode 'text=We recieve letters.'`; on the server only Caddy sets that header. To teach the memory: `POST /api/v1/memory` with the key and `{"from":"en","to":"ta","sensitivity":"ordinary","approver_post":"An invented post","passages":[{"source":"An invented line.","approved":"..."}]}`. Keepers need a role for each language on `/people`, so clear `role_grant` first or the `dev-admin` bootstrap will not work; read notices with `curl localhost:53190/control/hedwig/received`.

## Constraints

| # | Rule | Source |
|---|---|---|
| C1 | Change the PRD, never restate it. A figure another system owns is cited, never copied. | Owners register |
| C2 | Invented people and text only, in development and tests. | R-NFR-11 |
| C3 | Secrets never go in git or chat. The repository is public: no infrastructure detail beyond the PRD's stack. | R-NFR-10 |
| C4 | Caller text never reaches a log, record, error or the outbox. Grammar text is never written anywhere. | R-NFR-11, R-GRM-05 |
| C5 | No text leaves the server unmasked; masking is always on. | R-MSK-02 |
| C6 | No cron or job loop: PARADOX calls, signed. Only in-process reading (the source list) may use a timer. | STACK 2 |
| C7 | Database only through PgBouncer in transaction mode, pool 5; session entry for migrations. | STACK 4 |
| C8 | No dates, prices or em dashes in this repository. British English. | Owner |

Merging: stories land on a release branch, and the owner's standing rule is that commit, push, merge and version tags need no question: report after (`docs/STANDARDS.md` section 4). This repository is public, so read every pull request's file list before merging.
