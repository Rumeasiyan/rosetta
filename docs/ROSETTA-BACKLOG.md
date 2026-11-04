# ROSETTA backlog

Source: `docs/ROSETTA-PRD.md` v1.8 (requirements, Annex A data model, Annex B architecture). No process guide. Stories cite these; read them for the detail. `s2` is PRD section 2, `§5` the technology stack, `B n` link n in Annex B. IDs with `P-`, `K-` and `R-LKP` belong to PENSIEVE, CERBERUS and TVA.

## Definition of done (every story)

- Tests cover every rule the story touches, including its refusals.
- A test proves the story puts no caller text in logs, records, errors or the outbox, and no grammar text anywhere (R-NFR-11).
- Screens check the role on every request; a system owner reaches only their own systems (R-ADM-01).
- Screens work in Sinhala, Tamil and English, at desktop and 360 pixels wide, with no outside fonts or scripts.
- Every "(default: ...)" is a setting (R-ADM-05). A figure another system owns is cited, never copied (owners register).
- Each action R-REC-02 or PENSIEVE's list names becomes an outbox entry.
- Before Go live nothing reaches an outside service; stand-ins and invented text only (E-ENB-03). Production has no interim path for a system not ready.
- The story was walked as each user involved (below); `.env.example` and the published call documentation are updated if behaviour changed; its box is ticked.

## How to build

- **Solo:** the main session builds it alone. All of R1 is Solo, since it sets the patterns everything else copies, as is any story many others hook into.
- **Wave n:** stories with the same wave need nothing from each other. Build them at the same time, one subagent per story in its own git worktree, at most five at once; the main session reviews, merges, runs all tests and ticks Done before moving on.
- **With you:** needs the user, or the platform through ALLSPARK, to sign in, hand over keys or set up a service. The agent prepares everything, then stops and asks.
- **Verify before Done:** after building, run ROSETTA locally with one command. Use a browser tool (such as Claude in Chrome) to walk the story as each person involved, on desktop and phone widths; send the documented calls as the calling system and as PARADOX's stand-in; read back the answers, records, outbox and stand-in logs. Tick Done only when the tests pass and the walk matches the PRD; otherwise fix and walk again.

## Releases

| Release | Goal |
|---|---|
| R1 Foundation | Locally, with one command: the service, its data and every stand-in run; a person signs in through the MJOLNIR stand-in and holds a role; settings, PARADOX's signed calls and the audit outbox work; the plug-in's three unknowns are answered. |
| R2 Translation | Locally, a registered system with its key translates a list of passages in any open direction, through masking, the cache and BUMBLEBEE's stand-in with fallback, and gets every passage back or one error. |
| R3 Grammar | Locally, the plug-in with the owner's edit (`docs/plugin-findings.md`), on the local Document Server, checks an invented English letter through ROSETTA, signed in, with suggestions in the right places and nothing kept. |
| R4 Glossary and memory | Locally, keepers publish terms in three languages, reviewers manage memory, and translations and checks honour both: terms are kept, approved text answers without any source. |
| R5 Records, alerts and readiness | Locally, the Office can see, count and be alerted about everything; a caller can build from the published documents; capacity, speed, continuity and accessibility are shown. |
| R6 Go live | ROSETTA runs on ep-shared-01 at rosetta.epc.lk, connected to each provincial service, and SCOS Pro's letter editor translates letters and checks their English through it. |

## R1 Foundation

| # | Done | Run | ID | Story | Cites | Pri | Needs |
|---|---|---|---|---|---|---|---|
| 1 | [x] | Solo | E-ENB-01 | Go service, Compose run, settings from one env file, `-healthcheck` | §5; B 6; R-NFR-14 | Must |  |
| 2 | [x] | Solo | E-ENB-02 | PostgreSQL behind PgBouncer; migrations for every table in Annex A | Annex A, A.1; §5; B 10 | Must | 1 |
| 3 | [x] | Solo | E-ENB-03 | Stand-ins behind interfaces: BUMBLEBEE, MJOLNIR, CERBERUS, TVA's file, BIFROST, PENSIEVE, HEDWIG, PARADOX's caller | §5; B 8, B 9, B 11 to B 15, B 21; R-NFR-14 | Must | 1 |
| 4 | [x] | Solo | E-GRM-01 | Spike: the stock plug-in on a local Document Server, against a stand-in check | R-CAL-06, R-GRM-01; B 2, B 9 | Must | 1 |
| 5 | [x] | Solo | E-ADM-01 | Sign-in through MJOLNIR's stand-in, the person's record, ROSETTA's own session | R-ADM-01; §5; B 8 | Must | 2, 3 |
| 6 | [x] | Solo | E-ADM-02 | Screen frame in three languages, then grant and end roles | R-ADM-01, R-ADM-06, R-NFR-03, R-NFR-04, R-NFR-05, R-NFR-06, R-NFR-11; s2 | Must | 5 |
| 7 | [x] | Solo | E-ADM-03 | Settings and their history, in force within 60 seconds | R-ADM-05 | Must | 6 |
| 8 | [x] | Solo | E-ENB-04 | PARADOX's signed calls: check, run record, a run already done is skipped | §5; B 12 | Must | 2, 3 |
| 9 | [x] | Solo | E-REC-01 | Audit outbox to PENSIEVE's stand-in, with the catch-up job | R-REC-02; B 13 | Must | 7, 8 |

Notes:

- **E-ENB-01:** Listens on 8080; JSON logs to stderr; `-healthcheck` calls the service itself and exits 0 when ready. Memory, CPU and read-only root are set in the local Compose file too, so a breach shows early.
- **E-ENB-02:** Roles and limits as STACK 4 gives them; migrations through the session-mode entry. The first month's usage partition comes with the migration; later ones with E-REC-03.
- **E-ENB-03:** Each chosen by a setting, so Go live swaps settings, not code. The BUMBLEBEE stand-in answers as `docs/CALLERS.md` in its repository documents, `limits` included, and can be told to fail with each of the eight reasons, lose a masking stand-in, change a glossary term, answer slowly or stop answering. The TVA stand-in is a local file in the form of `docs/LOOKUP-FORMAT.md` in TVA's repository, with every status, `may_join` included, and a null owner subject. BIFROST's stand-in answers one person's post by email: found, found with no offices, not found, each refusal, or no answer. CERBERUS's stand-in allows, refuses shortly, or does not answer. Only development uses stand-ins: no interim path for a provincial system not ready reaches production.
- **E-GRM-01:** Goes first because R-CAL-06 rests on it. Document Server 8.2 Community in a local container, stock plug-in unchanged, local TLS with the editor at `writedocument.epc.test` and ROSETTA at `rosetta.epc.test`, so the two are same-site over HTTPS as on epc.lk. Watch the network panel and record in `docs/`: does it send credentials; is the session cookie sent with them; does it send text only on a Check click or also on a timer; does it send regional language codes; how it behaves on a short refusal, and whether the refusal needs CORS headers for it to be read. Send the refusal and timer findings to CERBERUS's session (K-CHK-03, K-SES-03). If credentials are not sent, stop and ask: R-CAL-06 changes. Found in `docs/plugin-findings.md`, from the source; the watching moves to E-GRM-07.
- **E-ADM-01:** Cookie host-only, Secure, HttpOnly, SameSite=Lax. Caddy removes MJOLNIR's own cookies before a request reaches ROSETTA (ALLSPARK decision on sign-in cookies), so ROSETTA relies only on its own. Renew and revoke as MJOLNIR's `docs/SESSION-INTERFACE.md` says, never on a timer. No second sign-in history: `last_seen_at` only.
- **E-ADM-02:** The first administrator comes from a bootstrap setting that works only while no administrator exists. Each system has its own theme (STACK 2). No outside fonts or scripts.
- **E-ENB-04:** Uses the module at `sign/v0.2.0`; the stand-in signs with a test key pair. Missed runs are PARADOX's to report (owners register): no monitor push per job.
- **E-REC-01:** Entry shape as PENSIEVE's P-ING-01 requires; `post` comes from the look-up at sign-in (E-CAL-05). PENSIEVE refuses a blank one: send exactly `not known`, in lower case, when it is not known at the time of the action, which until E-CAL-05 is always, and `system` for automatic work. Older entries are not rewritten once a post is learned. The stand-in refuses a blank too. PENSIEVE refuses details holding a key or identity card number, so test that nothing of the kind gets in.

## R2 Translation

| # | Done | Run | ID | Story | Cites | Pri | Needs |
|---|---|---|---|---|---|---|---|
| 10 | [x] | Wave 1 | E-CAL-01 | Register a system from TVA's status; its owner | R-CAL-01, R-CAL-02; B 11 | Must | 9 |
| 11 | [ ] | Wave 1 | E-ERR-01 | The fixed reasons and their messages in three languages | R-ERR-01, R-ERR-02 | Must | 6 |
| 12 | [x] | Wave 1 | E-PRV-01 | Read BUMBLEBEE's source list; size limit and states | R-PRV-01, R-NFR-13; B 15 | Must | 3, 6 |
| 13 | [x] | Wave 1 | E-MSK-01 | Mask and restore identity card and phone numbers | R-MSK-02, R-MSK-03 | Must | 1 |
| 14 | [x] | Wave 2 | E-CAL-02 | Issue, list and turn off keys; check on each request | R-CAL-03, R-CAL-04, R-NFR-10 | Must | 10, 11 |
| 15 | [x] | Wave 2 | E-PRV-02 | Source order for each direction and grammar; turn a source off | R-PRV-02 | Must | 12 |
| 16 | [x] | Wave 2 | E-GAT-01 | Gate: open and close a direction per source, read its state | R-GAT-01, R-GAT-02, R-GAT-04 | Must | 12 |
| 17 | [x] | Wave 3 | E-CAL-03 | Limits per system | R-CAL-07 | Must | 14 |
| 18 | [x] | Wave 3 | E-PRV-03 | Source call and fallback | R-PRV-03, R-PRV-04, R-PRV-05, R-PRV-06, R-ERR-03; B 15 | Must | 11, 15, 16 |
| 19 | [x] | Solo | E-TRN-01 | Translate a list: split, join, all or nothing | R-TRN-01, R-TRN-02, R-TRN-04, R-TRN-09, R-GAT-03; B 3 | Must | 13, 17, 18 |
| 20 | [x] | Wave 4 | E-MSK-02 | Sensitivity: what may go, what is kept, mismatches | R-MSK-01, R-MSK-04, R-TRN-03, R-NFR-11 | Must | 19 |
| 21 | [ ] | Wave 4 | E-TRN-02 | Answer fields and the machine label | R-TRN-06, R-TRN-07, R-NFR-05 | Must | 19 |
| 22 | [x] | Wave 5 | E-TRN-03 | Cache for ordinary pieces; same request, same answer | R-TRN-05, R-TRN-08, R-NFR-12 | Must | 20 |

Notes:

- **E-CAL-01:** The reader refuses a file format it does not know and ignores fields it does not know (TVA's R-LKP-05). `may_join` counts, as live does. Read the owner as `docs/LOOKUP-FORMAT.md` in TVA's repository gives it; a null subject means the flag in R-CAL-02. Match on the subject, never on email.
- **E-CAL-02:** R-CAL-03 points to STACK section 2 for the rule; a third live key is refused until one is turned off. Requests from another machine also carry the signature in §5; ROSETTA checks it, on PARADOX's fixed profile plus a nonce (`docs/contract.md` in its repository): `@method`, `@path` and `content-digest` covered, `created` inside its window, the nonce held in memory for that window, `keyid` the key's prefix. `@path` leaves out the query, so a signed call carries none and one with a query string is refused. It answers "no key" with 401 on failure. The caller keeps its private key and pastes its public key when the key is issued on the screens: no enrolment endpoint. The two live keys give the overlap for rotation; turning a key off revokes its public key. No scopes: a key allows what R-CAL and R-TRN allow any system.
- **E-PRV-01:** The refresh is an in-process timer, allowed because ROSETTA never sleeps and this is reading, not scheduled work. Keep a default size for when the list cannot be read. BUMBLEBEE counts size in code points, positions are UTF-16 (R-GRM-03): keep the two apart. `state` values as `docs/CALLERS.md` gives them.
- **E-MSK-01:** A pure package with no store, table-tested on old and new card forms and Sri Lankan phone forms in all three scripts' text. Stand-ins must survive translation; test by the BUMBLEBEE stand-in changing them (R-MSK-03). Each stand-in keeps a map to the original offsets, which E-GRM-04 uses.
- **E-GAT-01:** Screens for the administrator. The evaluation it rests on comes from E-GAT-02; until then the local run opens a direction with a recorded test score.
- **E-PRV-03:** Decide on `failure.reason`, never the HTTP status. `too-long` refreshes the list and splits again before it counts as a failure. A `blocked` source is not called before `retry_at`. Pass the request reference as `request_id`. The wait on one call is the one in `docs/CALLERS.md`; the whole request stays inside R-ERR-03, so a late fallback may not start.
- **E-TRN-01:** Solo because later stories all hook into its piece pipeline. Line breaks and edge spacing kept byte for byte around the pieces. The sentence splitter is rule-based for three scripts, with a table test; a too-long sentence breaks at word boundaries.
- **E-TRN-03:** `UNLOGGED` table; lookup by hash of normalised text, language pair and source. Expired rows are removed by E-REC-03.

**Still to run for R2.** Every R2 story is built, and its tests have passed with the database, none skipped. A scripted walk passed against the service and the stand-ins from source: every registration and refusal, keys and signatures, the order and switch, the gate in force inside 60 seconds, a translation with its spacing and masking, fallback on each reason, the cache, limits, every new screen answering in three languages, and nothing of the text in the log, the outbox or at PENSIEVE's stand-in. A second walk in Chrome took `/systems`, a system's screen, the key shown once, `/sources/history`, `/gate` with an open form and its history, and the settings through three languages at 1280 and a true 360 pixels wide: none runs wider than its window, and the key shows once. Looked at by eye: each screen on at least one width and language. The two boxes left open are the ones that rest on the words below:

- **E-ERR-01 and E-TRN-02, best effort words:** every Sinhala and Tamil message R2 adds, the reasons and the label above all, wants a native reader before Go live. R-ERR-02 calls the messages approved texts, so these two stay open until a person has read them.

<!-- r2-still -->

## R3 Grammar

| # | Done | Run | ID | Story | Cites | Pri | Needs |
|---|---|---|---|---|---|---|---|
| 23 | [x] | Solo | E-GRM-02 | Check and languages endpoints in the plug-in's form; split and join | R-GRM-01, R-GRM-02, R-GRM-03; B 2 | Must | 19 |
| 24 | [x] | Wave 6 | E-GRM-03 | Browser calls: signed-in officer, listed addresses, counted to the system | R-CAL-06, R-NFR-10; B 2, B 9 | Must | 23 |
| 25 | [x] | Wave 6 | E-GRM-04 | Positions true to the caller's text after masking and splitting | R-GRM-03, R-MSK-02 | Must | 23 |
| 26 | [x] | Wave 6 | E-GRM-05 | No grammar text kept; suggestions held in memory | R-GRM-05, R-NFR-11, R-NFR-12 | Must | 23 |
| 27 | [x] | Wave 6 | E-GRM-06 | Grammar sources by benchmark score, with fallback | R-GRM-06, R-PRV-03 | Must | 23 |
| 28 | [x] | Solo | E-GRM-07 | Walk the plug-in, with the owner's edit, against ROSETTA on the local Document Server | R-CAL-06, R-GRM-01, R-GRM-03; B 2 | Must | 24, 25, 26, 27 |

Notes:

- **E-GRM-02:** ROSETTA sends only `en`; regional codes follow E-GRM-01's finding, normalised at the edge or refused. Translation stays apart from these paths, so a key-only mark in CERBERUS never covers them (B 9).
- **E-GRM-03:** Answer the preflight without a session; on the POST, allow credentials and name the exact origin, never `*`. When CERBERUS's stand-in refuses, confirm what E-GRM-01 found, and that the editor stays usable.
- **E-GRM-04:** Test with text outside the Basic Multilingual Plane before and after a masked value and across a split. A suggestion that cannot be placed exactly is left out, never moved.
- **E-GRM-05:** In-process map with a size cap, keyed by hash, gone at restart. A test greps logs, records, outbox and the database after a check for a marker in the text.

**Still to run for R3.** Every R3 story is built, and its tests have passed with the database, none skipped. A walk in Chrome passed against the service and the stand-ins from source, with a small stand-in for Caddy setting `X-Cerberus-Account`, and an XMLHttpRequest from a page on a second origin calling as the plug-in calls: languages and a check with positions true in the browser's own units, masking at the source, the held answer, refusals readable in the reader's language, an address not listed hidden from the page, a preflight with no session, fallback by score, every source failing, the new screens at 1280 and 360 pixels in three languages, and no marker in the log, any table, or at PENSIEVE's stand-in. A key from outside with no public key was refused against the running service. E-GRM-02, E-GRM-04, E-GRM-05 and E-GRM-06 are ticked on that. What follows has not run:

- **E-GRM-07, the plug-in itself:** walked on a local Document Server through real Caddy and CERBERUS's stand-in (`docs/plugin-findings.md`, `dev/plugin-test/`). With its addresses edited alone, as the note's stock plug-in, it is refused, as expected. With credentials on as well, it signs in, loads the languages, places every suggestion rightly, and corrects the document. The owner took the edit with credentials on, keeping the public scripts, so the story is done with that form; the risks taken are in `docs/plugin-findings.md`, and the real CERBERUS is left to E-ENB-10. The hazard of Insert after moving the selection is open with the owner.
- **E-GRM-03, the refusal from CERBERUS:** seen with real Caddy and CERBERUS's stand-in, the plug-in's credentials on. The panel shows the words of a refusal that carries CORS headers, shows nothing when one does not, and the editor stays usable throughout (`docs/plugin-findings.md`). Done; the real CERBERUS is E-ENB-10.
- **The plug-in's credentials:** watched: as shipped no cookie reaches CERBERUS, nobody is signed in and every browser check is refused. The edit that changes this is written out in `docs/plugin-findings.md`, and the owner took it.
- **Best effort words:** every Sinhala and Tamil message R3 adds wants a native reader before Go live, the refusals above all.

<!-- r3-still -->

## R4 Glossary and memory

| # | Done | Run | ID | Story | Cites | Pri | Needs |
|---|---|---|---|---|---|---|---|
| 29 | [x] | Solo | E-GLO-01 | Entries, keepers' checks, publishing and versions | R-GLO-01, R-GLO-02, R-GLO-05 | Must | 7, 9 |
| 30 | [x] | Wave 7 | E-GLO-02 | Any officer proposes a term; keepers told; proposer sees the outcome | R-GLO-04 | Must | 29 |
| 31 | [x] | Wave 7 | E-GLO-03 | Import and export a spreadsheet file | R-GLO-07 | Should | 29 |
| 32 | [x] | Wave 7 | E-GLO-04 | Terms kept in translation; whole terms answered from the glossary | R-GLO-03, R-TRN-04, R-GAT-02, R-NFR-05 | Must | 29 |
| 33 | [x] | Wave 7 | E-GLO-05 | No suggestions on a glossary term | R-GRM-04 | Must | 29 |
| 34 | [x] | Wave 7 | E-MEM-01 | Add approved text to the memory; refuse what may not be kept | R-MEM-01, R-MEM-02, R-MEM-03; B 3 | Must | 29 |
| 35 | [x] | Wave 8 | E-MEM-02 | Serve from memory, marked as checked by a person | R-TRN-04, R-TRN-07, R-MEM-02, R-GAT-02 | Must | 34 |
| 36 | [x] | Wave 8 | E-MEM-03 | Find, recheck, correct and remove memory entries | R-MEM-04, R-MEM-05 | Must | 34 |
| 37 | [x] | Wave 9 | E-GLO-06 | A published change clears cached pieces and marks memory for recheck | R-GLO-06, R-TRN-08, R-MEM-04 | Must | 32, 36 |

Notes:

- **E-GLO-01:** Keepers without a language are flagged in E-CAL-05. All text goes through one normalise function (R-NFR-05) before compare or store.
- **E-GLO-02:** The notice to keepers goes through HEDWIG's stand-in.
- **E-GLO-04:** Protect terms with placeholders of the same family as masking stand-ins, so one check covers both. A term that cannot be placed is flagged on the passage, not failed.
- **E-GLO-06:** Runs in the publishing transaction for memory marks; `pg_trgm` finds the pieces, so test it at 500,000 memory pieces (R-NFR-01).

**Still to run for R4.** Every R4 story is built, and its tests have passed with the database. Scripted walks in headless Chrome passed against the service and the stand-ins from source, as keepers, an officer with no role, memory reviewers, the administrator and a calling system, at 1280 and a true emulated 360 pixels in three languages, with no screen running wider than its window; each screen was looked at by eye on at least one width and language. E-MEM-01 and E-MEM-02 add no screen, so their walks were the calling system's calls alone. Nothing of a term or of caller text was found in the log, the outbox or at PENSIEVE's stand-in. What follows has not run:

- **The plug-in with terms:** E-GLO-05 was walked by sending the plug-in's calls, with no Document Server. E-GRM-07 stays open as R3 left it.
- **Real services:** HEDWIG's notices reached only its stand-in; the templates `glossary_proposal` and `glossary_import` wait for E-ENB-13. A source keeping `⟦G1⟧` is as unverified as `⟦M1⟧` until E-ENB-18.
- **Spreadsheet programs:** files from openpyxl were read, and the export was opened by openpyxl and LibreOffice. Microsoft Excel has not been tried.
- **Best effort words:** every Sinhala and Tamil message R4 adds wants a native reader before Go live. R-ERR-02's approved texts are not seeded into the memory, as E-ERR-01 still waits for that reader.
- **The real machine's figures:** the 500,000 pieces were measured on the local stack (`docs/DECISION-LOG.md`); E-NFR-01 measures under load.

<!-- r4-still -->

## R5 Records, alerts and readiness

| # | Done | Run | ID | Story | Cites | Pri | Needs |
|---|---|---|---|---|---|---|---|
| 38 | [x] | Solo | E-REC-02 | A usage record for every request, without text | R-REC-01; B 15 | Must | 22, 28, 35 |
| 39 | [x] | Wave 10 | E-REC-03 | Nightly totals, removals, next months' partitions | R-REC-04, R-NFR-12; B 12 | Must | 38 |
| 40 | [x] | Wave 10 | E-REC-04 | Usage screens and export | R-REC-03, R-REC-02 | Must | 38 |
| 41 | [x] | Wave 10 | E-ADM-04 | Status screen | R-ADM-02, R-NFR-13 | Must | 38 |
| 42 | [x] | Wave 10 | E-ADM-05 | Send a test request from the screens | R-ADM-04 | Should | 38 |
| 43 | [x] | Wave 10 | E-CAL-04 | Publish how to call ROSETTA, with a working example | R-CAL-08, R-ADM-07, R-NFR-14 | Must | 38 |
| 44 | [x] | Wave 11 | E-CAL-05 | Daily flags, TVA refresh, post look-ups and role ends | R-CAL-02, R-CAL-05, R-ADM-06; s2; B 11, B 21 | Must | 39 |
| 45 | [x] | Wave 11 | E-ADM-06 | Alerts by email to the administrator and owners, from BUMBLEBEE's state | R-ADM-03; B 14 | Must | 41 |
| 46 | [x] | Wave 11 | E-GAT-02 | Run the shared test set against a source for the gate | R-GAT-01, R-NFR-14 | Must | 40 |
| 67 | [x] | Solo | E-TRN-04 | A translation as a background run: start, ask, collect, give up | R-TRN-10, R-TRN-11, R-TRN-12, R-TRN-13, R-CAL-07, R-ERR-01, R-ERR-03, R-REC-01, R-ADM-02, R-NFR-08; B 3 | Must | 19, 38, 41, 43 |
| 47 | [ ] | Solo | E-NFR-01 | Capacity, speed and memory under load | R-NFR-01, R-NFR-02; §5 | Must | 44, 45, 46, 67 |
| 48 | [x] | Wave 12 | E-NFR-02 | Continuity: BUMBLEBEE or every source down; restart mid-request | R-NFR-08, R-NFR-09 | Must | 47 |
| 49 | [ ] | Wave 12 | E-NFR-03 | Screens audit: weak connection, devices, WCAG, no trackers | R-NFR-03, R-NFR-04, R-NFR-06, R-NFR-11 | Must | 47 |

Notes:

- **E-REC-02:** Usage stays in ROSETTA and never goes to PENSIEVE, one by one or in total (owners register). Refused keys and screens do go there, as denials (E-REC-01); "over limit" is not a denial. Caddy's limit per address holds back a flood of bad keys.
- **E-REC-03:** One PARADOX job each; `paradox_run` skips repeats. Also clears expired cache and finished outbox rows.
- **E-REC-04:** An export is an audit entry (P-ING-02).
- **E-ADM-05:** Invented text only; say so on the screen. Cites the same pipeline, so it is recorded as operation `test`.
- **E-CAL-04:** `docs/CALLERS.md`, in the form BUMBLEBEE's has: every request, answer, reason and limit, with a copy-and-run Go example against the stand-ins. The EP website and SCOS Pro read this; neither gets its own shape.
- **E-CAL-05:** A language with no keeper is flagged here too (s2). Owners come from `systems.json` only. For granted roles it looks up one person's post by email through BIFROST, fact `scroll.staff-lookup`, as `docs/EXCHANGE.md` in BIFROST's repository gives it: when a role is granted, at sign-in, and in a daily pass over live roles. A person found earlier and now not found, or at another post at that office, loses the role, within a day even if they stay signed out. A person never found keeps it, flagged, with the post "not known"; the first time they are found, `post_ref` is filled and the flag clears. Only `found: false` means not found; every other status, refusal or silence, `no_list_held` included, is a failed look-up and changes nothing. `found: true` with no offices counts as found at no post. The daily pass paces itself under BIFROST's limit per key and honours `Retry-After`. The session keeps the post for audit entries; no list is stored. A person switched off is MJOLNIR's, not this story's.
- **E-ADM-06:** One alert when a state changes, not one per failed request. Only ROSETTA's own owners. Silence from audit is PENSIEVE's to flag, and not alerted here.
- **E-GAT-02:** A command that takes a test set and a source and writes the score the gate records. Test text is invented or supplied by volunteers, never real letters.
- **E-TRN-04:** The shape agreed with SCOS Pro's session, for every caller (R-CAL-08): `POST /api/v1/translate/runs` with the body of `POST /api/v1/translate`, 202 with `run`, `state` and `ask_again_at`; `GET /api/v1/translate/runs/{run}`; `DELETE` the same. `run not known` is 404. GET and DELETE are signed as any call, with the digest of the empty body. The reference is random, and a run asked after by another system is `run not known`. The run uses the piece pipeline as it stands, a few worked at a time. Forgetting at its time is one timer for that run, not a job loop (AGENTS C6): keep it so. `docs/CALLERS.md` gains all of this; `run not known` joins E-ERR-01's messages. Walk a restart mid-run, then ask after the run. Search the log, tables and outbox for a marker in a personal run: none, ever.
- **E-NFR-01:** Against stand-ins with their slow modes: inside 250 MB with `GOMEMLIMIT` 200 MiB at capacity; grammar's held suggestions and the most background runs (R-TRN-12) counted. Check `docker stats`, since ROSETTA is one of the 6 systems awake.

## R6 Go live

| # | Done | Run | ID | Story | Cites | Pri | Needs |
|---|---|---|---|---|---|---|---|
| 50 | [ ] | With you | E-ENB-05 | Tagged image to GHCR; `apps/rosetta/` with STACK 2 controls, `import awake`, no sleep labels, on the platform network | §5; B 6, B 20; R-NFR-07 | Must | 43, 48, 49 |
| 51 | [ ] | With you | E-ENB-06 | Database `rosetta` and its pools on the shared server; migrations run | §5; B 10 | Must | 50 |
| 52 | [ ] | With you | E-ENB-07 | rosetta.epc.lk through the tunnel and Caddy | §5; B 1, B 4, B 5, B 6 | Must | 51 |
| 53 | [ ] | With you | E-ENB-08 | MJOLNIR connected: ROSETTA registered as a client, real sign-in | B 8; R-ADM-01 | Must | 52 |
| 54 | [ ] | With you | E-ENB-09 | First administrator; roles for glossary keepers and memory reviewers named by the Office | s2; R-ADM-06 | Must | 53 |
| 55 | [ ] | With you | E-ENB-10 | CERBERUS connected: ROSETTA's parts marked, checks walked | B 9; R-ADM-01, R-NFR-10 | Must | 53 |
| 56 | [ ] | With you | E-ENB-11 | TVA's lookup file read with ROSETTA's read-only key | B 11; R-CAL-01 | Must | 52 |
| 57 | [ ] | With you | E-ENB-21 | BIFROST connected: ROSETTA's agreement in TVA, its key, a post looked up | B 21; R-ADM-06 | Must | 52 |
| 58 | [ ] | With you | E-ENB-12 | PENSIEVE connected: entries reach the audit log | B 13; R-REC-02 | Must | 52, 57 |
| 59 | [ ] | With you | E-ENB-13 | HEDWIG connected: a test alert reaches the administrator | B 14; R-ADM-03 | Must | 52 |
| 60 | [ ] | With you | E-ENB-14 | PARADOX schedules running | B 12; §5; R-REC-04 | Must | 52 |
| 61 | [ ] | With you | E-ENB-15 | Uptime Kuma: address and container monitors; alert timed | B 18; R-NFR-07 | Must | 52 |
| 62 | [ ] | With you | E-ENB-16 | PHOENIX backup of database `rosetta`; a restore tried | B 19; R-NFR-08 | Must | 51 |
| 63 | [ ] | With you | E-ENB-17 | BUMBLEBEE connected through the link: key handed over, source list read | B 15, B 16, B 17; R-PRV-01, R-NFR-10 | Must | 52 |
| 64 | [ ] | With you | E-ENB-18 | Directions opened at the gate on evaluations of the real sources | R-GAT-01, R-GRM-06; B 17 | Must | 63 |
| 65 | [ ] | With you | E-ENB-19 | SCOS Pro registered as a caller, key handed over, first real translation | B 3; R-CAL-01, R-CAL-03, R-TRN-01 | Must | 54, 55, 56, 58, 59, 60, 61, 62, 64 |
| 66 | [ ] | With you | E-ENB-20 | Plug-in installed on SCOS Pro's Document Server, pointed at ROSETTA; an officer checks a letter | B 2; R-CAL-06, R-GRM-01 | Must | 65 |

Notes:

- **E-ENB-05:** Follows `apps/_template`. The always-on decision entry and the networks rule are ALLSPARK's (allspark#28). Stories here follow dependency, not a date; a provincial service not yet there holds its own story, with no interim path.
- **E-ENB-07:** No Cloudflare Access on any screen (owners register).
- **E-ENB-10:** The owning office's rule manager marks the parts, with a reason (K-SYS-05): the key-only translation and memory paths open; `OPTIONS` on the grammar paths open, `POST` checked; screens checked. ROSETTA keeps no idle timer: by the owner's ruling each system's owner and developers decide idle per part and CERBERUS enforces what is declared, real activity keeps a session alive, and no level requires a passkey. Give the administrator `docs/cerberus-declaration.yaml`, the form in CERBERUS's `docs/JOINING.md`. No idle limit on the grammar paths, by the owner's ruling, as the editor sends no activity signal; the other figures are this build's choice: 1 hour at the base, 30 minutes on administration and personal data. Then repeat E-GRM-03's refusal test for real, and stop CERBERUS once to see grammar stop and the editor stay usable. `writedocument.epc.lk` must be a registered system in CERBERUS for its refusals to carry CORS headers.
- **E-ENB-11:** Through RUNBOOK's step for a reading key (B4), on ALLSPARK's side. The key goes in the server's env file, by the user, never in chat. `tva.Bucket` is the reader; `deploy/how-to-deploy.md` gives its scope.
- **E-ENB-21:** Each reader signs its own agreement in TVA; ROSETTA's names `scroll.staff-lookup` and these two purposes; it cannot read the whole list. Needs BIFROST and SCROLL there.
- **E-ENB-12:** Needs PENSIEVE there (allspark#10): ROSETTA registered as a source, its key, and the path marked open by PENSIEVE's owner.
- **E-ENB-13:** Needs HEDWIG there (allspark#25): ROSETTA a calling system with its own key, and one notice template per alert kind.
- **E-ENB-14:** `apps/rosetta/schedules.yaml` with `wake: none`, and `apps/rosetta/paradox.pub`; `deploy.sh` loads a system only with both.
- **E-ENB-15:** A Docker Container monitor, since it never sleeps, plus `/__up`.
- **E-ENB-16:** Policy and frequency are PHOENIX's (RUNBOOK step 7); after a restore the cache is empty and refills (A.1).
- **E-ENB-17:** `http://bumblebee:8080` on a network ROSETTA joins, whichever link layout the user settles (allspark#28, allspark#29). ROSETTA's key is shown once on BUMBLEBEE's machine and entered in the env file by the user. BUMBLEBEE's story E-ENB-20 is the other side.
- **E-ENB-18:** With invented or volunteers' test text only. Evaluation needs E-GAT-02 run against each real source. By the owner's ruling, the six directions on `google-translate` open on the ten-sentence invented set, marked unchecked, as the administrator's decision, not a pass. A native reader's evaluation is still to come.
- **E-ENB-19:** The `/api/` signature checked for real. Officers' translations go by background run if SCOS Pro's own owner adopts it; that is theirs to decide, and the direct path stays for them meanwhile. The EP website joins later the same way, as a caller under R-CAL-08.
- **E-ENB-20:** SCOS Pro's session does the install on its server; the Check click is done by an officer on an invented letter first, then real ones. Same-site, credentials and timing as found in E-GRM-01 are confirmed on the real addresses.

## Not in the PRD

- None.
