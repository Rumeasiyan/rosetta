# ROSETTA on the shared server

What the platform needs from ROSETTA. These files are copied into ALLSPARK by its "Adding one system" steps; ROSETTA never changes the server itself. Secrets are never here.

| File | Where it goes |
|---|---|
| `compose.yaml` | `apps/rosetta/`. The image tag is the released version. |
| `rosetta.env.example`, `rosetta-migrate.env.example` | Same folder, as examples for the two env files, which exist on the server only. |
| `pgbouncer.lines` | `[databases]` in PgBouncer's file, with the host the other systems' lines use. |
| `site.caddy` | The platform's Caddy sites, as `rosetta.caddy`. |
| `schedules.yaml` | `apps/rosetta/`, for PARADOX. |
| `hedwig/*.json` | One template per notice type, named in the file, saved on HEDWIG's Templates screen. |
| `../docs/cerberus-declaration.yaml` | Entered on CERBERUS's Systems screen, with `system:` filled from ROSETTA's TVA reference. |

## Database

The template's `database.sql` as it is, with `-v system=rosetta`: one role, which owns the database and every table, `CONNECTION LIMIT` 6. ROSETTA grants nothing in its migrations. The first migration creates `pg_trgm`, a trusted extension the database's owner may create.

## Sign-in

A client at MJOLNIR, registered by hand on Hydra's admin port, since joining on MJOLNIR's screen needs a TVA entry: id `rosetta`, `client_secret_basic`, grants `authorization_code` and `refresh_token`, response type `code`, scopes `openid offline_access email profile`, redirect `https://rosetta.epc.lk/signin/callback`. The secret goes straight into `rosetta.env`, shown nowhere.

## Keys

- **PARADOX:** ROSETTA queues no jobs, so it holds no private key. Make a pair, keep the public line as `paradox.pub` and destroy the private key.
- **TVA's bucket:** ROSETTA's own read-only key, with the action `Read:tva-lookup/systems.json*` alone: no `List`. It goes in `rosetta.env` as `ROSETTA_TVA_S3_KEY` and `ROSETTA_TVA_S3_SECRET`; SeaweedFS is then restarted. A server step, done by the owner. The Office records it on TVA's Read-only keys screen once TVA's sign-in works.
- **PENSIEVE, BUMBLEBEE, HEDWIG, BIFROST:** each needs ROSETTA's TVA entry. Until then ROSETTA runs without them: the settings file says what each does while empty. Set each key when it is issued, then `docker compose up -d --force-recreate`.

## Order

1. Database, PgBouncer lines, env files, client, TVA key.
2. Migrate: `docker compose --profile migrate run --rm migrate`. It exits 0. Then start ROSETTA; it answers `/healthz` with nothing in TVA, no file in the bucket and none of the later keys.
3. The first administrator signs in once, and becomes one. Check `/people`. Until BIFROST answers, their post stays "not known", flagged.
4. Register `docs/cerberus-declaration.yaml` with CERBERUS (a placeholder `system:` until step 5).
5. ROSETTA's TVA entry. Then its keys: PENSIEVE (registered as a source), BUMBLEBEE (added as a caller on its own machine), HEDWIG (the templates first), BIFROST (its agreement names `scroll.staff-lookup`).
6. Directions open at `/gate` only on evaluations of the real sources.
7. Once TVA lists a calling system, the administrator registers it on `/systems` and issues its key.

## Addresses

- PARADOX: `http://rosetta:8080/jobs/<name>`.
- Systems on the server: `http://rosetta:8080/api/...`, with their key.
- Systems on other machines: `https://rosetta.epc.lk/api/...`, with their key, every request signed (`docs/CALLERS.md`).

## Checks

- `docker exec rosetta-rosetta-1 /app -healthcheck` exits 0.
- From outside: `/__up` answers 200; `/jobs/daily` and `/healthz` answer 404.
- Kuma: an HTTP monitor for `https://rosetta.epc.lk/__up` and a Docker Container monitor for `rosetta-rosetta-1`.
- `docker logs rosetta-rosetta-1` holds a warning for each later key not yet set, and nothing of any text.
