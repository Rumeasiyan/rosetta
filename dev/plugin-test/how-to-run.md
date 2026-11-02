# The plug-in walk

Runs ONLYOFFICE's LanguageTool plug-in in a real Document Server against ROSETTA, behind real Caddy and CERBERUS's stand-in, and records what Chrome sends (E-GRM-07). What it found is in `docs/plugin-findings.md`. The plug-in's files are never committed; everything in the document is invented; local only.

Document Server is heavy: start it only while the machine can bear it, and bring it straight down.

## Set up

From the repository's root, with `gh`, Go, Node and Chrome:

```sh
docker compose -f dev/compose.yaml up -d postgres pgbouncer
set -a; . ./.env.example; . dev/keys/paradox.env; set +a
go run ./cmd/rosetta migrate
docker compose -f dev/compose.yaml exec -T postgres psql -U rosetta -d rosetta -f - < dev/plugin-test/seed.sql
go run ./cmd/rosetta-standin &
ROSETTA_PUBLIC_URL=https://rosetta.epc.test go run ./cmd/rosetta serve &
curl -X POST localhost:53190/control/cerberus/mode \
  -d '{"mode":"cookie","params":{"registered_origin":"https://writedocument.epc.test"}}'

cd dev/plugin-test
python3 make-docx.py page/invented.docx
mkdir -p walk-out/plugin
./plugin.sh fetch walk-out/plugin
./plugin.sh A walk-out/plugin    # or B, for credentials on
npm install --prefix walk-out playwright-core
docker compose --profile editor up -d
```

`plugin.sh` shows its edit as a diff. Between runs it is enough to run it again and open a new page; Document Server serves the file as it stands.

`cookie` mode decides by `standin_session`, whose value is the account. Caddy publishes on `127.0.0.1:53443`; Chrome is told by `--host-resolver-rules` that every `.epc.test` name is there, and `/etc/hosts` is not touched.

## Walk

```sh
NODE_PATH=walk-out/node_modules node walk.js A
```

Runs: `A`, `B` (Check, corrections, Insert to document, each language), `signed-out`, `refusals` (each of CERBERUS's, on one open panel; with `STANDIN_PID` set, it stops the stand-in too), `selection`, `edge` (brackets, long text, ROSETTA's own refusals; set the system's `requests_per_minute` first to see the limit). Use form A for run `A`, and form B for all the others. Each writes `network.json`, `panel.json`, `frame.json` and screenshots to `walk-out/<run>/`. The console shows each call with its cookie and origin.

## Bring it down

```sh
docker compose --profile editor down -v
docker compose -f ../compose.yaml down -v
```

`-v` matters: Document Server's image declares volumes of its own.
