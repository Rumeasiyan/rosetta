# ROSETTA - agent working guide

ROSETTA is the translation and grammar service of the Eastern Province, Sri Lanka: the one way every provincial system translates and checks text in Sinhala, Tamil and English, with one shared glossary. Machine output is a draft for a person to check, never official text.

## Where to look

| Path | When you need it |
|---|---|
| `docs/ROSETTA-PRD.md` | **What to build.** Requirements, the stack (section 5), Annex A (data model), Annex B (architecture). |
| `docs/ROSETTA-BACKLOG.md` | **Build order.** Stories citing the PRD. Tick a box when its story is done. |
| `docs/DECISION-LOG.md` | Build decisions the PRD and backlog do not hold. |
| `docs/diagrams/` | `build-erd.py` draws Annex A and writes its dictionary into the PRD; a test checks the schema against that dictionary. |
| `cmd/rosetta/` | The service: `serve`, `migrate`, `-version`, `-healthcheck`. |
| `cmd/rosetta-paradoxcall/` | Local stand-in for PARADOX. Not in the image. |
| `cmd/rosetta-standin/`, `internal/standin` | Local stand-ins for BUMBLEBEE, MJOLNIR, CERBERUS, BIFROST, PENSIEVE and HEDWIG, each under its own path prefix; `/control` sets a failure mode and reads back what came. Not in the image. |
| `internal/db` | PgBouncer-safe pool and migrations (`migrations/NNN-*.sql`, never edited once applied). |
| `internal/jobs` | The job address PARADOX calls: signature, run record, lock. |
| `internal/oidc` | Sign-in through MJOLNIR: code flow with PKCE, renew, revoke. |
| `internal/web`, `internal/i18n` | The screens (Go templates and htmx, the Stone bands look, embedded fonts and scripts) and the one message file. |
| `internal/people` | The person's record and roles. |
| `internal/config` | Settings from the environment: the listen address, the database, the addresses and keys of other services. |
| `dev/` | The local stack. Start only what you need; the machine runs many stacks. `dev/tva/` is TVA's stand-in, a local folder. |

## Commands

```sh
docker compose -f dev/compose.yaml up -d postgres pgbouncer
export ROSETTA_TEST_DATABASE_URL=postgres://rosetta:rosetta@localhost:53432/rosetta
go test -p 1 ./...
docker compose -f dev/compose.yaml down
```

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

Merging: stories land on a release branch; the owner says yes per release (`docs/STANDARDS.md` section 4).
