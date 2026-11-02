# ROSETTA decision log

Build decisions the PRD and backlog do not hold. Newest first.

## The glossary's spreadsheet file

An `.xlsx` file, read and written by `internal/sheet` with Go's standard library alone (BSD-3-Clause, as the rest of the build), so there is no reader's licence to carry and no new dependency: `go.mod` is unchanged. CSV was left out: it opens with the wrong encoding in the office's spreadsheet program, and it is where formula injection does its harm. Files from openpyxl and files this package writes have been read; the export has been opened by openpyxl and by LibreOffice.

What is read has limits of its own: 8 MiB as sent, 32 MiB for any part unpacked, read to that limit whatever the file's header claims, so a small file that unpacks far is refused rather than filling the 250 MB. The upload stays in memory, as the root filesystem is read-only. `glossary_import_rows` (R-GLO-07, default 5,000) counts rows with anything in them, not the first, and one row over refuses the whole file, keeping nothing. The first row names the columns: `type`, and at least one of `en`, `si`, `ta`, with `english`, `sinhala` and `tamil` allowed; others are not read.

A row that cannot join (a bad type, a text a term may not be, a term already published or held earlier in the file, or a cell holding a formula, whose stored value is not taken instead) is left out and counted, and the screen gives its row number and why, never its words. Rows join as `proposed` in two whole statements rather than row by row, so 5,000 rows took under 3 seconds here and hold one of the five connections only that long. One notice goes for the whole file. A file that fails to read keeps no `glossary_import` row; one that reads keeps its name, shown only on these screens.

On export every cell is text, and any value a spreadsheet could read as a formula, beginning `=`, `+`, `-`, `@`, a tab or a line break, or their wide forms, after any spaces, gets a leading apostrophe. An inline string is not a formula in an `.xlsx` as it stands; the apostrophe is for when a person saves it on as CSV. On import a leading apostrophe before such a mark is taken off, so the file can come in again. The export is the published glossary at the present version; a past version is read on the screens. It is PENSIEVE's `export` action (P-ING-02), with the row count and no words.

## Telling keepers of a proposal

The notice is a row in `alert`, written in the proposal's transaction, as the one record of what HEDWIG carries (Annex A), with `glossary_entry_id` and `glossary_import_id` added (migration 006) and two kinds, `glossary proposal` and `glossary import`, that Annex A's list of kinds does not name. After the change commits a goroutine sends what is due and the person does not wait (AGENTS C6). What does not go waits, and PARADOX's `audit-catch-up` sends it, rather than a new job on the schedules, since both are what is owed to another service. `hedwig_notice_id` holds HEDWIG's request number, as one notice goes to several people and that number finds them all. `request_key` is `rosetta-alert-<id>`, so a retry tells nobody twice. A rate limit stops the pass; any other refusal stays for the next, and logs its code. Past 90 days, Annex A's keeping, nobody is told.

Who is told: the live keepers for the languages still unchecked when the notice goes, by the reference `rosetta-person-<id>`, an opaque number and never the account or an address (HEDWIG's `docs/API.md`), in the language they chose on the screens. A notice holds a number, the languages and a link, never a word of the glossary, so a term is read on the screens and not in an inbox. With nobody to tell, or the entry already settled, the notice counts as done rather than being retried for ever: the work stays on the waiting list, and a language with no keeper is E-CAL-05's flag. The template, `glossary_proposal` and `glossary_import`, is made in HEDWIG at E-ENB-13, which already asks for one template per kind.

## What an officer with no role may see

R-GLO-04 opens proposing to anyone signed in, so the bar offers it to everyone, `/glossary/mine` shows what they proposed with its outcome, and they can open their own entries. The list and other people's entries stay closed (R-ADM-01). So they can correct a term they cannot look up, a proposal of a term already published is refused and leads to that entry's correction with its words filled in, and `/glossary/propose?corrects=<id>` opens for any published entry. That shows a published term to anyone signed in who guesses a number, which seemed fair: published terms are meant to appear in letters to citizens. A waiting entry is not shown this way.

## How the glossary's entries move

A keeper checks only their own language, and the third check publishes the entry there and then, with the next version number, taken one at a time under a lock so the number goes up by exactly one (R-GLO-05). What is published never changes: a correction is a new entry that replaces the old one when it is published, and until then the old one stays in force. One correction of an entry waits at a time (migration 005). A correction brings the old words for any language left blank, and a language reading as it did keeps its check, so only the keepers concerned are asked (R-GLO-04); a change to the type or note alone asks all three, as otherwise nobody would be left to check it. Keepers may write a language left blank, or correct their own, before or after checking it, until the entry is published.

A keeper refuses an entry by declining to check their own language, so one whose language is already checked, or carried over, has no say: that is how "only on their own language" reads for a whole entry. A refusal needs a reason, for the proposer. An administrator is not a keeper and neither checks nor refuses, but opens the waiting list and may propose.

A text comes out one way, so a term another published entry holds in the same language, whatever its case, is refused when proposed, when checked and again at publishing, as a rival may have got there first. Keepers who truly have one word for two things must word them apart. A term begins with a letter or a digit so it can be found in a text, is on one line, and holds no card or phone number or the stand-ins' brackets, since masking would stop it ever being found. An entry kept as written reads the same in all three, so one text written fills the three, and each keeper still checks it (R-GLO-02).

Reading the glossary, a term's page and past versions take any role, a system owner's included (R-ADM-01). A page shows 100 entries and finding by text narrows it (R-NFR-03). `glossary_version.summary` holds counts only, never a term. The audit entries are `creation`, `approval`, `rejection` and, for a publication, `change` on the version: languages and numbers, no words. Past versions live at `/glossary-versions`, as a path under `/glossary/{id}/` would be ambiguous. htmx no longer copies `style` attributes when it settles a page, as the policy refuses them anyway and the browser complained.

## Browser calls on the grammar paths

Who is signed in comes from `X-Cerberus-Account`, which only Caddy sets, as the entry further down says; absent is not signed in, never a default. A system on the platform network could forge that header on these paths, and gains only a check counted to that person, never the screens. CORS names only an origin in `caller_origin`, compared exactly, with `Access-Control-Allow-Credentials: true`, on answers and on ROSETTA's own refusals, so the plug-in's panel can read them; never `*`. An origin not listed gets no CORS headers and "no key" with 401, also when it carries a good key, since R-NFR-10 accepts browser calls only from listed addresses; a server sends no `Origin`. A request with neither a key nor an `Origin` has no system to count to, and is refused the same way. A key with a listed origin goes by the key's rules and counts to the key's system. A key's refusal is the `/api/` JSON; a browser's is plain text in the language of `Accept-Language` if it names one of the three, else English, with `X-Rosetta-Reason` and `X-Rosetta-Request-Ref` in headers. Being signed out and being unlisted have their own words, as "no key" tells an officer nothing, but the reason is still "no key" from the fixed list. "No provider" and "timed out" have words saying check rather than translation.

A preflight needs no session or key and uses no limit, answers 204 for a listed origin and is refused like any unlisted call, without a denial, so a page cannot fill the outbox by being opened. A real refused call is kept as a denial like a refused key, naming the system where one is known, and fixed words why. The limits per minute and at once are the system's own, and the languages list counts as a call too, since it costs the same to answer and comes from the same system.

Addresses are listed on a system's screen, by the administrator only (R-CAL-06); an owner sees them. An address is `https://` and a host, with `http://` only on this machine, written one way, with no path or wildcard. One address counts to one system, so migration 004 makes `origin` unique and the second system to list it is refused. A change is history, in `change_history` as `caller origin`, an addition Annex A does not list, and an audit entry. R-CAL-06's default of the letter editor's address means no row until E-ENB-19 lists it: there is no system to hold it before then.

B 9 has `POST` on the grammar paths checked by CERBERUS as a person's request, so a key caller from outside would be stopped there before reaching ROSETTA, though R-GRM-01 lets it call. A key caller on the platform network comes directly and is not affected. E-ENB-10 has to settle this if a system outside ever checks grammar with a key.

`Deps.Usage` is the hook E-REC-02 fills, called once for each check with no text in it. The stock plug-in sends no credentials as shipped (`docs/plugin-findings.md`), so until its `$.ajax` is changed or the owner decides otherwise, CERBERUS sees nobody and no check reaches ROSETTA from it.

## Grammar sources and fallback

Grammar and translation share one way of trying sources, so a busy wait, a hold until `retry_at`, a resting source, a stale list and "too long" all behave the same, and a hold set by one operation holds a source for the other. Grammar has no gate. Its text is treated as personal, so it goes to any source once masked and is kept nowhere, whatever it holds. A source with no score counts as 0, so it comes after any with a positive score and before one the benchmark marks below zero. An empty list is an answer, and no later source is asked for a second opinion. The stand-in gains `standin-grammar-b`, scoring 55 to the first's 72, so fallback can be tested and walked. The answer's `fallback_used` is true if any piece used fallback. The order and switch screens have offered grammar since E-PRV-02.

## Grammar text is kept nowhere

The suggestions for an identical text are held in process memory only, for `grammar_hold_minutes` (default 10), so a restart forgets them. They are found by a SHA-256 of the text with a random salt made at the start, so even the hash means nothing elsewhere. What is held is the placed suggestions, with no text of the caller's beyond what a source put in a message or replacement; each `context` is made afresh from the text sent. An answer is held only when whole. The cap is `HeldBytes`, 8 MiB by estimate, oldest let go first; that figure is this build's and E-NFR-01 measures it, so it is a constant, not a setting. To be forgotten on time with nobody asking, what is too old is swept on each pass of the source list's timer (about every 30 seconds), through a `Tick` on the Book rather than a timer of its own (AGENTS C6). The same text from another officer or system gets the same held answer, as R-GRM-05 speaks of an identical text, not a person's.

## What counts as placed exactly

A suggestion is dropped, never moved, when it lies outside its piece, has no length, starts or ends between the two units of a character, or touches a stand-in in any way. That last one covers a span holding a whole stand-in as well as one inside it: such a finding is about a number the source never saw, and its replacement could only hold the mask. For the same reason a replacement holding a stand-in goes, and a suggestion whose message holds one goes. Suggestions come back in the order of the text, and one overlapping an earlier one is dropped, as the plug-in marks them one after another and would show it in the wrong place. Each piece is mapped by its own stand-ins, numbered from one, and then by where the piece begins in the whole text, since a stand-in's number and so its length differs between the two.

## The check's form

`POST /api/v2/check` and `GET /api/v2/languages` follow LanguageTool's public v2 form, read against the plug-in's own source. Beyond what the backlog lists, `language.name` is needed: the plug-in looks it up in the language list whenever there is a match, so it is always `English`, the name the list gives. Additions the plug-in ignores: `source` on each match (R-GRM-03), `offered` on each language, and `request_ref`, `fallback_used` and `sources` on the answer. A rule's `id` is `ROSETTA_` and the category, as sources give no rule of their own; `issueType` is LanguageTool's word for the category. Sinhala and Tamil are in the list as `Sinhala (not offered)` and `Tamil (not offered)`, since R-GRM-02 lists them, and picking one answers "unsupported". English's long code is `en-GB`. `auto`, `en` and any `en-*` are English, in any case; no `language` at all is "invalid request"; any other is "unsupported". `enabledOnly` is read by nothing. A key caller's refusal is the JSON shape of the rest of `/api/`.

A text under the limit goes to a source whole, not sentence by sentence as translation does: R-GRM-02 asks for splitting only past what sources allow, a source reads a sentence better among its neighbours, and a letter costs one call and not one each sentence. Over the limit it breaks where `translate.Split` breaks it, then joins neighbours up to the limit. A piece with no letters outside its stand-ins is not sent. An empty text answers an empty list. Text that is not valid UTF-8 is "invalid request", as no position could be true of it. `grammar_chars_max` is R-GRM-02's 20,000, in code points.

Each match's `context` is made from the text as sent while answering, 40 units either side, and never cuts a two-unit character. The plug-in puts that context into its panel as HTML; it replaced `<` before sending, so what comes back is what it sent, and ROSETTA does not escape it, which would move the offsets.

## What the R2 walk by eye changed

A source switch in `/sources/history` read `true` and `false` in every language; it now reads on and off in the reader's. The gate's history at phone width showed labels with nothing after them on a closed decision; a label now comes only with its value. A true 360 pixels comes from the browser's own emulated width, measured as `window.innerWidth`, since `frame-ancestors 'none'` refuses the page inside a frame. E-ERR-01 and E-TRN-02 stay unticked for a native reader, as R-ERR-02 calls their words approved.

## The cache

It holds pieces as they went to the source, stand-ins and all, so no card or phone number is ever in the table, and the same sentence with another number answers from one row. A machine's answer stays under the gate: only a source the piece could go to now answers from the cache, on the version it runs now, so a source turned off, closed or on a new version gives no cached answer either (R-GAT-02 exempts only the glossary and memory). A resting source does, as nothing goes to it. So, with BUMBLEBEE down and the list stale, cached pieces still answer as the last known list and the gate allow; R-NFR-09 names only the glossary and memory, and this goes a little further, which seemed right for callers. A piece is kept as soon as the source answers and its stand-ins come back whole, so a request that later fails need not pay again when sent again (R-TRN-05). A piece split again is kept whole when one source made all of it. Failing to read or write the cache costs a source call and logs fixed words, never the answer. `cache_days` is the setting; E-REC-03 removes expired rows.

## The answer's shape

Each passage gives `text`, `language`, `origins` (a count for each of the four), `pieces` (each piece's origin, source and version, and whether it used fallback), `sources` (each once), `machine`, `checked_by_person`, `label` in three languages only where a machine made any of it, `fallback_used` and `glossary_version`; the answer repeats those last two and adds `request_ref`, `from`, `to`, `sensitivity` and `sensitivity_mismatch`. A piece from the cache counts as a machine's, naming the source that made it. A passage with nothing in it is neither a machine's nor a person's. Because every answer must state the glossary version, being unable to read it fails the request as "no provider". The label's Sinhala and Tamil are best effort like the other messages. E-CAL-04 publishes all this.

## Sensitivity

Confidential is refused first and goes nowhere, not even the glossary's look-up, since R-MSK-01 says refused. A card number in ordinary text makes the whole request personal, as another passage may well name the same person, and the answer says so in `sensitivity` and `sensitivity_mismatch`, which R-MSK-04 does not ask for but costs a caller nothing. A phone number alone is no mismatch: R-MSK-04 names only the card. Personal text is not looked for in the cache either, so even its hash stays out of the database; the glossary and memory are still asked, as they hold people's work and keep nothing of the text. Mismatches are counted in memory by system until E-REC-02 writes `usage_record.mismatch`, so today's counts go at a restart.

## What counts as returning the original (R-TRN-09)

A source that answers with nothing, or with the very text sent (normalised, spacing at the edges aside), has failed, so the next source is tried; with every source doing so the request is "no provider". The exception is a piece with no letters outside its stand-ins, such as a number on a line of its own, which is the same in any language. This means a piece that truly stays the same, such as a system's name alone, fails until the glossary's "keep as written" answers it in R4. That was chosen over ever passing an echo off as a translation.

## The piece pipeline

A passage is masked before it is split, so a number written with spaces can never be broken up and sent in parts, and the splitter sees no dots inside numbers. Pieces go to a source one at a time, in order: simple, fair to BUMBLEBEE's queue, and the first failure stops the rest. A piece repeated in one request goes once. A long request can run into R-ERR-03 under BUMBLEBEE's rate, in which case the caller may send it again and ordinary text picks up from the cache; E-NFR-01 must measure this, and sending some at once is the first thing to try if it matters. `too long` for characters, passages or body says so with a position when one passage is at fault. The 5,000 characters of a passage is a setting too, though R-TRN-01 marks only its neighbours. Only the first `too long` from a source re-splits; a part too long again fails, as the list keeps changing under it.

## How fallback decides

Only `failure.reason` counts, and only it is read: the explanation is never kept, so an error cannot carry text. A reason it does not know, an answer in no documented shape and a refused key on BUMBLEBEE's side are all `unavailable`, as CALLERS.md has it. `busy`, `blocked` and `changed` hold the source in memory until their `retry_at`, for every request, as R-PRV-05 says every "try again after" is respected; a busy source this request is waiting on is held from the others too. A busy wait must leave 2 seconds for the call, and no call starts with under 2 seconds of the request left, so a late fallback cannot run past R-ERR-03: that is this build's figure. The one-call wait of 60 seconds is the one CALLERS.md gives and is a constant, as the source list does not publish it. Fallback used means a source earlier in the order was resting or failed, not merely a busy source tried again.

## Limits per system

The settings `caller_requests_per_minute` and `caller_requests_at_once` (R-CAL-07) are what a new system starts with; each system's own figures then live on its row, changed by the administrator on its screen, so a system's owner cannot raise them. They are read with the key on every request, so a change is in force at once. A minute is any 60 seconds, not a clock minute. The count is in memory, in the one process there is, so a restart starts it again. A refused request counts against neither limit, and a refused key touches none. Migration 003 adds `caller limit` to `change_history`'s subjects, an addition Annex A does not list, since R-ADM-05 keeps every such change as history.

## The gate

A decision is for one source and direction, and counts only for the version it was made on, compared with what the list says now, so a new version is closed at once with nothing to do. No decision is closed. Opening again on a new evaluation ends the old decision, which stays as history. A test set's name is 1 to 100 letters, digits, spaces and `._:-`, and one PENSIEVE would refuse is refused as a bad name. `GET /api/v1/gate` answers all six directions, as a signed call carries no query; a source turned off for ROSETTA is not among those open. It reads the list as last known, stale or not, since the gate is about evaluations and not about who is up. The screen is the administrator's alone.

## Source order and switch

With no order set, translation goes by source name, so the order never depends on how the list was written, and grammar by English benchmark score (R-GRM-06). Sources the administrator placed come first and the rest follow. A position left blank is not placed, and only the numbers' order counts. A system owner sees the source screen, since R-NFR-13 means them to choose, but changes nothing; the history is the administrator's. A source turned off keeps its place in the form. Changes are `change` entries, as P-ING-02 has no nearer word.

## Keys

A key is `rst_`, twelve hex characters and 256 random bits; the first sixteen characters are the prefix lists show and `keyid` carries. It is stored as SHA-256: with that many random bits there is nothing to guess, so a slow hash would only cost every request. The two-live rule holds the system's row while counting. The refused key's denial names the fixed route `/api/`, the system if the key told which and otherwise the actor `unidentified caller`, with fixed words why. The system and prefix go in `object_ref`, which PENSIEVE does not scan, since a TVA reference could look like anything. Counts by reason are in memory until E-ADM-04 shows them: a request with no known key has no calling system, so a usage record cannot hold it. A request that a database fault stops from being checked answers "no provider", not "no key".

## The signature's form on /api/

PARADOX's profile with one parameter added: `rosetta=("@method" "@path" "content-digest");created=<unix>;keyid="<prefix>";nonce="<nonce>";alg="ed25519";tag="caller-to-rosetta"`, nothing else accepted. The label and tag are this build's choice, so a signature made for PARADOX can never pass here, nor one for here pass there. A nonce is 16 to 128 of `A-Za-z0-9._~-`. Only a verified signature's nonce is remembered, so nobody can fill the memory or spend a caller's nonce without its key. After a restart a request still inside its window could pass once more, which is safe (R-TRN-05). E-CAL-04 publishes this. A key with no public key is for a caller on this server, which comes directly on the platform network (B 7), so it is not asked to sign; the same key on a request showing it came through the front door (B 3) is refused with "no key" and 401, so a leaked one is no use from outside. The front door shows as `Cf-Connecting-Ip` or `X-Forwarded-For`, either one, even empty. Caddy's `common` snippet sets the first on every site and its reverse proxy adds the second, whatever the caller sent, so a caller outside cannot remove them, and forging them refuses only itself. A system on this server that calls through Caddy rather than directly is refused the same, and needs a public key.

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

## Signing out goes on to MJOLNIR

ROSETTA's button revokes its refresh token, ends its own session and sends the browser with a 303 to `ROSETTA_SIGNOUT_URL`, MJOLNIR's own page, as MJOLNIR's `docs/SESSION-INTERFACE.md` asks. Before this it stopped at a page of its own, so the person stayed signed in at MJOLNIR and "sign in" passed with no form. Hydra's `/oauth2/sessions/logout` is not used: it signs nobody out, as measured there. The form token is still checked, and a refused one ends nothing. The address is required, https except on this machine, and is the only place a sign-out goes; nothing in the request can change it. A request with no session goes there too. MJOLNIR's page does not come back, so ROSETTA's own signed-out message has gone. MJOLNIR being out of reach does not stop the sign-out here, though the browser then lands on an error of MJOLNIR's. The stand-in's `/mjolnir/sign-out` ends its sign-in cookie and shows a plain page.

Chrome holds a redirect after a form's post to the page's `form-action`, so a page with the sign-out form on it (any page showing a signed-in person) adds the origins of `ROSETTA_SIGNOUT_URL` and `ROSETTA_SIGNIN_ISSUER` there, as sign-out passes through both of MJOLNIR's hosts. They come from the settings, and nothing else in the policy moves; a page without that form keeps `'self'` alone. Sign-in is a link, not a form, so `form-action` does not apply to it.

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

## The stand-in listens on 53190

Port 53090 was held on the Docker side of the development machine with no container showing it, which left the stand-in running with no network. Docker's first error scrolled past while the container still reported healthy, so a Compose run that answers 502 at sign-in points here first.
