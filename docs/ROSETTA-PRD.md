# ROSETTA

The translation and grammar service of the Eastern Province, Sri Lanka: the one way every provincial system translates and checks text in Sinhala, Tamil and English, with one shared glossary.
Version 0.1, draft for cut rounds. Owner: the Office of the Chief Secretary, Eastern Province. Address: rosetta.epc.lk.

## 1. Overview

Every citizen-facing provincial service must work in Sinhala, Tamil and English, including its forms, messages and notices. If each system finds its own way to translate and check grammar, the same official term comes out differently in each, failures are hidden in different ways, and no one can move all systems to a better translator at once.

ROSETTA is one always-on service that every provincial system calls for language work. It contains:

- registration of calling systems, each with its own key;
- translation of short text, batches of interface text, long text and documents;
- grammar and spelling checks, which the letter editor's checking plug-in uses unchanged;
- one shared glossary of official terms, kept by a named keeper for each language;
- review and approval of translations by a person, and a shared memory of approved translations;
- providers behind one way in: browser sources through BUMBLEBEE now, and models through JARVIS later, chosen by order, with fallback;
- masking of identity card and phone numbers before text leaves the shared server;
- conversion of text typed in legacy fonts to Unicode;
- usage records without text, and screens for the administrator, the glossary keepers and system owners.

**How it works.** A system sends text with its key, the languages, its purpose and how sensitive the text is, and may name a provider. ROSETTA converts legacy fonts, splits the text into sentences and answers each from the glossary, the approved memory or the cache where it can. It masks identity card and phone numbers in the rest, sends it to the named provider or the first working one in order, puts glossary terms and masked values back, and returns the draft with its source and the label "Machine translation, check before issue". A person checks and approves it; approved text joins the memory for every system. Grammar checks follow the same path and return suggestions; their text is never stored. New providers, such as the models volunteers are researching, join behind ROSETTA and callers change nothing.

The name comes from the Rosetta Stone, which carries the same text in three scripts.

**Not included:** speech, reading scanned images, Sinhala or Tamil typed in Roman letters, issuing official texts (each system issues its own), choosing or running models (JARVIS), browser work (BUMBLEBEE), terms of use of outside sites (managed by the Office outside the systems), prices.

Values marked "(default: ...)" are settings the Office can change.

## 2. Stakeholders

| Stakeholder | Role in ROSETTA |
|---|---|
| The Office of the Chief Secretary | Owns ROSETTA and its data. **Administrator**: registers calling systems and issues their keys, sets providers, their order and the language gate, changes settings, names glossary keepers, grants roles |
| Officers fluent in Sinhala, Tamil or English, named by the Office | **Glossary keeper** for one language: approves, corrects and replaces terms in that language (R-GLO). **Memory reviewer**: confirms approved translations marked for recheck (R-REV-08) |
| Owner of a calling system (an officer) | **System owner** for their own systems: sees their usage and errors, issues and turns off their system's keys, sends test requests, receives alerts about their systems |
| Officers using a calling system | Translate and check text through that system. The officer the system names as **approver** approves a translation before it is issued (R-REV-03); any officer may propose a glossary term (R-GLO-05) |
| Calling systems (the letter editor first, then the provincial website, the notification service and others) | Call ROSETTA with their key; never call a provider, BUMBLEBEE or a model directly. Each decides what it sends, marks its sensitivity, and answers for what it issues |
| Volunteers and maintainers | Research and test models that may become providers; run the shared server; never see real text |

Every role is held by a personal account signed in through MJOLNIR, the provincial sign-in. A system owner's powers cover only their own systems. The glossary, memory, settings and records belong to the Office, never to a person or a calling system. When a person moves, their roles end on the effective date and records keep the post they held at the time. A calling system left without an owner, or a language left without a glossary keeper, is flagged to the administrator.

## 3. Functional requirements

### 3.1 Calling systems and keys (R-CAL)

| ID | Requirement |
|---|---|
| R-CAL-01 | A system can be registered as a caller only if it has a live entry in the provincial system register (TVA). ROSETTA checks the entry when the system is registered and records its reference. |
| R-CAL-02 | Each calling system has one named owner. A system whose owner's role ends is flagged to the administrator (R-ADM-03). |
| R-CAL-03 | The administrator, or the system owner for their own system, issues a key for a calling system and can turn it off at once. The key is shown once and can never be shown again. A system may hold two live keys at a time, so a key can be replaced without stopping. |
| R-CAL-04 | A request with no key, an unknown key or a turned-off key is refused with the reason "no key" (R-ERR-01) and counted on the status screen (R-ADM-02). |
| R-CAL-05 | A calling system that has sent no request for 90 days (default: 90 days), or whose register entry is no longer live, is flagged to the administrator. |
| R-CAL-06 | An officer's browser may call the grammar check (R-GRM-01) directly, only while the officer is signed in, and only from addresses the administrator lists for that calling system (default: the letter editor's address). The call is counted to that system. |
| R-CAL-07 | Each calling system has limits: requests a minute (default: 60) and requests at once (default: 5). A request over a limit is refused with the reason "over limit" and when to try again. |
| R-CAL-08 | The administrator can pause a calling system. While paused, its requests are refused with the reason "paused". |

### 3.2 Translation (R-TRN)

| ID | Requirement |
|---|---|
| R-TRN-01 | A system sends text of up to 5,000 characters (default: 5,000), the source language (Sinhala, Tamil, English or "detect") and the target language, and receives the translation in the same request. All six directions between the three languages are offered, each under its own gate (R-GAT). |
| R-TRN-02 | Every request states its purpose: form field, interface text, notification, letter draft or notice draft. The purpose sets the label (R-TRN-07), the gate behaviour (R-GAT-03) and whether the translation is tracked for review (R-REV-01). |
| R-TRN-03 | Every request states the sensitivity of its text: ordinary, personal or confidential (R-MSK-01). There is no default; a request without it is refused with the reason "invalid request". |
| R-TRN-04 | "Detect" names the source language from the script of the text. Text in Roman letters is taken as English. Text that mixes scripts, or that ROSETTA cannot place, is refused with the reason "unsupported language" and a message asking the caller to name the language. |
| R-TRN-05 | ROSETTA splits text into sentences, and longer sentences at word boundaries, and answers each piece in this order: a whole glossary term (R-GLO-03), the approved memory (R-REV-06), the cache (R-TRN-09), then a provider (R-PRV-03). The answer is the pieces joined in their original order, with line breaks kept. |
| R-TRN-06 | Every answer states, for the whole text and for each piece: where it came from (glossary, memory, cache or provider), the provider and its version, whether a machine produced it, the glossary version used and the request reference. |
| R-TRN-07 | Every answer that a machine produced in whole or part carries the label "Machine translation, check before issue" in all three languages, for the caller to show beside the text on every screen and printout. Answers wholly from the glossary or memory carry no label. |
| R-TRN-08 | Every answer says whether it may be issued: only text wholly from the glossary or the approved memory, or a tracked translation that is approved (R-REV-03). |
| R-TRN-09 | Each translated piece of ordinary text is kept in a cache for 30 days (default: 30 days), by text, language pair and provider, so the same piece is not sent to a provider twice. Personal and confidential text is never cached. A glossary change removes cached pieces that hold the changed term (R-GLO-07). |
| R-TRN-10 | **Batch:** a system may send up to 200 short strings (default: 200), at most 20,000 characters in all, of one direction and purpose, such as a screen's interface text, and receives one answer for each. One failed string does not fail the rest; each failed string carries its own reason. |
| R-TRN-11 | **Names:** a system may ask for a person's or place's name to be written in another script (for example a Tamil name in Sinhala letters) rather than translated. ROSETTA uses the glossary first, then a provider that offers it; if none does, it refuses with the reason "unsupported". |
| R-TRN-12 | ROSETTA never returns the original text, or part of it, as if it were the translation. A piece that cannot be translated fails with its reason. |

### 3.3 Grammar and spelling checks (R-GRM)

| ID | Requirement |
|---|---|
| R-GRM-01 | ROSETTA answers grammar checks in the form the letter editor's existing checking plug-in expects, including its list of languages, so the plug-in works unchanged once pointed at ROSETTA. Other systems may call the same check with their key. |
| R-GRM-02 | A check sends text of up to 20,000 characters (default: 20,000) and its language (Sinhala, Tamil or English), and receives a list of suggestions, or an empty list when there is nothing to suggest. ROSETTA splits longer text into pieces of at most 5,000 characters for its providers and joins their suggestions. |
| R-GRM-03 | Each suggestion gives its start and length in the units the editor counts (UTF-16), a message, up to 3 replacements, a category (for example spelling, grammar or style) and the provider that made it. Positions always refer to the text the caller sent, also after masking (R-MSK-03) and splitting. |
| R-GRM-04 | Suggestions on a glossary term in that language are dropped, so approved names, posts and offices are never marked as errors. |
| R-GRM-05 | Grammar text is never written to disk, a record or the cache. To spare providers, the suggestions for an identical text are held in memory for 10 minutes (default: 10 minutes) and then forgotten. |
| R-GRM-06 | Grammar providers for each language are tried in order of their benchmark score in that language, as BUMBLEBEE publishes it (R-PRV-05), unless the administrator sets another order. A language with no working provider answers with the reason "unsupported" for that language. |

### 3.4 Providers and fallback (R-PRV)

| ID | Requirement |
|---|---|
| R-PRV-01 | There are two kinds of provider: browser sources, reached only through BUMBLEBEE; and models, on provincial machines or self-hosted, reached only through JARVIS. A system sends the same request whichever provider serves it, and a new provider needs no change in any caller. |
| R-PRV-02 | The administrator keeps a provider catalogue. Each provider has its kind, the operations it offers (translation, grammar, names), its languages and directions, where it processes text (outside sites or provincial machines), and its state (on, paused with its resume time, or off). |
| R-PRV-03 | For each operation and direction, the administrator sets the order of providers. ROSETTA sends each piece to the first provider in order that is on, open under the gate (R-GAT) and allowed for the text's sensitivity (R-MSK-01). |
| R-PRV-04 | A system may name a provider in its request (for example "browser Google Translate"). ROSETTA uses that provider first and, unless the request says "this provider only", falls back to the rest of the order. A provider the system may not use, or one that does not offer the operation, is refused with the reason "unsupported". |
| R-PRV-05 | ROSETTA reads BUMBLEBEE's list of sources, with each source's operations, languages, state, resume time and grammar benchmark scores, every 60 seconds (default: 60 seconds) and skips paused sources until their resume time. |
| R-PRV-06 | A provider call that fails (unavailable, busy, blocked, page changed, timed out, too long, unsupported, turned off or signed out) goes to the next provider in order. On "busy" with a suggested wait shorter than the time left, ROSETTA waits and tries the same provider once more first. The answer says that fallback was used. |
| R-PRV-07 | ROSETTA sends each provider call at most 5,000 characters, waits at most 60 seconds for it (default: 60 seconds) and keeps each provider under its limit of calls a minute (default: 30 a minute for each browser source). |
| R-PRV-08 | ROSETTA never tries to get round a block, a bot check or a sign-in wall. It respects every "try again after" a provider gives. |
| R-PRV-09 | If every allowed provider fails, the request fails with the reason "no provider" and the earliest time a provider is expected back, never with the original text (R-TRN-12). |
| R-PRV-10 | Each provider is checked every 5 minutes (default: 5 minutes) with a small invented test text. A provider that fails 3 checks in a row is marked down and skipped; it is marked up again after 2 good checks. Both changes are alerted (R-ADM-03). |

### 3.5 Sensitivity and masking (R-MSK)

| ID | Requirement |
|---|---|
| R-MSK-01 | **Ordinary** text may go to any provider. **Personal** text, which holds names, addresses or similar, may go to any provider after masking (R-MSK-02), but is never cached, never kept in memory and never kept after the answer. **Confidential** text goes only to providers on provincial machines; while there is none, it is refused with the reason "no provincial provider". |
| R-MSK-02 | Before any text leaves the shared server for a provider, ROSETTA replaces identity card numbers (old and new forms) and phone numbers with stand-ins, whatever the declared sensitivity. Masking is a help, not a guarantee: anything it does not recognise is sent as written. |
| R-MSK-03 | Stand-ins in the provider's answer are replaced with the original values before the answer goes back. A stand-in the provider lost or changed makes that piece fail with the reason "masking lost", never an answer without the value. |
| R-MSK-04 | Text marked ordinary that holds an identity card number is treated as personal, and the mismatch is counted for the system owner (R-REC-04). |
| R-MSK-05 | The link between stand-ins and original values exists only while the request runs; it is never stored or written to any record. |

### 3.6 Glossary (R-GLO)

| ID | Requirement |
|---|---|
| R-GLO-01 | There is one shared glossary for the province. Each entry holds the same term in Sinhala, Tamil and English, its type (post, institution, place, term, or "keep as written" such as system names and reference formats), a note, and the keeper who checked each language. |
| R-GLO-02 | An entry is published only when all three languages are checked by their keepers. A term with only two checked is held. Entries are never deleted, only replaced; a replaced entry keeps its history. |
| R-GLO-03 | A text that is wholly one glossary term is answered from the glossary, with no provider and under any gate state. |
| R-GLO-04 | Every glossary term found in a text appears in the translation as its published translation. ROSETTA protects the terms before sending the text and checks them in the answer; a term the provider changed or dropped is put back, and if it cannot be placed the answer flags it for the reader. "Keep as written" entries are never translated. |
| R-GLO-05 | Any signed-in officer may propose a new or corrected term, through a calling system or the screens. The keepers for the languages concerned are told, and the proposer sees the outcome. |
| R-GLO-06 | The glossary has one version number, which goes up by one with every published change. Every translation records the version it used, and any past version can be read, so a letter can be checked against the glossary it was written with. |
| R-GLO-07 | A published change removes cached pieces that hold the changed term (R-TRN-09) and marks memory entries that hold it for recheck (R-REV-08). |
| R-GLO-08 | Calling systems may read the published glossary, whole or the changes since a version, so a system that keeps a copy stays current. It can also be exported as a spreadsheet file. |
| R-GLO-09 | The administrator can import entries from a spreadsheet file of up to 5,000 rows (default: 5,000). Imported entries wait as proposals for the keepers (R-GLO-02). |

### 3.7 Review and translation memory (R-REV)

| ID | Requirement |
|---|---|
| R-REV-01 | Letter and notice drafts are always tracked; other purposes may ask to be tracked. A tracked translation has a reference and a status: machine draft, reviewed or approved. A translation a person typed starts as reviewed. |
| R-REV-02 | Through the calling system, an officer fluent in the target language may edit a tracked translation and mark it reviewed, or return it to draft with a note. |
| R-REV-03 | The calling system names the approver of each tracked translation (for a letter, the officer who owns it). Only that person, signed in, can approve it; a system key never can. A wrong step is refused with the reason "not allowed". |
| R-REV-04 | Every version of a tracked translation is kept with who changed it and when. A change after approval returns it to reviewed, and it needs approval again. |
| R-REV-05 | A system can read a tracked translation's current text, status and history by its reference. Tracked translations are kept while the calling system needs them and removed at its request, or after 2 years without a change (default: 2 years). |
| R-REV-06 | When a translation of ordinary text is approved, each of its pieces joins the shared memory with the approver's post, the date and the glossary version. Memory serves every calling system and is kept until replaced. |
| R-REV-07 | Personal and confidential text never joins the memory, even when approved. |
| R-REV-08 | Memory entries marked for recheck (R-GLO-07) are not served until a memory reviewer for that language confirms, corrects or removes them on the screens. |
| R-REV-09 | The administrator, or a memory reviewer for their language, can find memory entries by text and remove a wrong one. Removal is recorded (R-REC-03). |

### 3.8 Long text and documents (R-DOC)

| ID | Requirement |
|---|---|
| R-DOC-01 | A system may send text of up to 100,000 characters (default: 100,000), or a Word (.docx) or plain text file of up to 10 MB, as a job, and receives a job reference at once. Other file types are refused. |
| R-DOC-02 | Jobs are worked through in the background, in order of arrival, and only while no short request is waiting. A system can read a job's progress (pieces done of total) at any time. |
| R-DOC-03 | When a job ends, ROSETTA tells the officer who asked, through the notification service, that a translation is ready. The message holds no text. A system may instead read the job's state. |
| R-DOC-04 | A finished job gives each piece beside its source, so the reviewer checks one against the other. For a file it also gives a translated file with the same layout. A job is always tracked (R-REV-01). |
| R-DOC-05 | A job with some failed pieces ends as "partial"; each failed piece is left empty with its reason, never filled with the original. |
| R-DOC-06 | A system may cancel its job; pieces not yet sent to a provider are dropped. Job inputs and results are removed 7 days after the job ends (default: 7 days); the tracked translation stays (R-REV-05). |

### 3.9 Legacy fonts (R-FNT)

| ID | Requirement |
|---|---|
| R-FNT-01 | A system may send Tamil typed with the Bamini layout or Sinhala typed with the FM Abhaya layout and receive it in Unicode. The conversion follows a fixed table: the same input always gives the same output. |
| R-FNT-02 | A translation request or job may name its legacy layout, and ROSETTA converts the text before translating. |
| R-FNT-03 | Characters the table cannot convert are listed with their positions, never dropped or guessed. |
| R-FNT-04 | Further layouts are added when a system needs one, with a keeper for that language checking the table before it is used. |

### 3.10 Language gate (R-GAT)

| ID | Requirement |
|---|---|
| R-GAT-01 | Each direction is open or closed for each provider. The administrator opens it only after recording the evaluation it rests on: the test set, the score and the provider version. A new provider version starts closed. |
| R-GAT-02 | The glossary, the memory and legacy font conversion are not machine translation and are served whatever the gate state. |
| R-GAT-03 | For a direction closed for every allowed provider, a request is refused with the reason "human translation required", with any glossary terms found in the text to help the translator. Letter and notice drafts may instead receive a tracked draft with the stronger label "Unchecked machine draft: a person must check every sentence before use", if the administrator allows it (default: not allowed). |
| R-GAT-04 | Systems can read each direction's gate state, the providers open for it and the date of its evaluation, so a closed direction can offer human translation instead of a broken button. |
| R-GAT-05 | Grammar checks follow the same gate, by language and provider. |

### 3.11 Errors (R-ERR)

| ID | Requirement |
|---|---|
| R-ERR-01 | Every refusal or failure gives one reason from a fixed list (no key, paused, over limit, invalid request, too long, unsupported language, unsupported, not allowed, not found, human translation required, no provincial provider, masking lost, no provider, timed out) and, where it helps, when to try again. |
| R-ERR-02 | Every reason comes with a message for the officer or citizen in Sinhala, Tamil and English, so the caller can show it in the reader's language. These messages are approved texts in the memory. |
| R-ERR-03 | An error never holds the text that was sent. |
| R-ERR-04 | A short request that runs longer than 60 seconds in all (default: 60 seconds) stops with the reason "timed out"; the caller may send it again or send it as a job (R-DOC-01). |

### 3.12 Records (R-REC)

| ID | Requirement |
|---|---|
| R-REC-01 | Every request leaves a usage record: calling system, officer (if signed in), operation, purpose, sensitivity, languages, length, pieces by source (glossary, memory, cache, provider), providers used, fallback used, time taken, outcome or reason and request reference. It never holds the text or the answer. |
| R-REC-02 | ROSETTA passes its request reference to every provider call, so a provider's own records can be matched to ROSETTA's. |
| R-REC-03 | Glossary changes, reviews and approvals, memory removals, gate changes, provider state changes, key changes and setting changes are sent to the provincial audit record (PENSIEVE) with who, what and when, never text. |
| R-REC-04 | Usage screens show requests, pieces by source, failures by reason, sensitivity mismatches (R-MSK-04) and time taken, by calling system, operation, direction, provider and day. A system owner sees only their own systems. Any view can be exported as a spreadsheet file. |
| R-REC-05 | Each night, usage records are added to daily totals. Usage records are removed after 2 years (default: 2 years); daily totals after 5 years (default: 5 years). |

### 3.13 Administration (R-ADM)

| ID | Requirement |
|---|---|
| R-ADM-01 | The screens are open only to named people with a role (R-ADM-06). Calling systems never use the screens. |
| R-ADM-02 | A status screen shows each provider's state and resume time, BUMBLEBEE and JARVIS up or down, jobs waiting, and today's requests, refusals and failures by reason. |
| R-ADM-03 | The administrator is alerted by email when a provider goes down or up, when every provider for a direction is down, when BUMBLEBEE or JARVIS stops answering, when failures for one system pass 20 percent in an hour (default: 20 percent), and when a system or language is flagged (R-CAL-02, R-CAL-05). A system owner gets the alerts about their own systems. |
| R-ADM-04 | A system owner or the administrator can send a test request with invented text from the screens and see the full answer, including sources and fallback. |
| R-ADM-05 | Every change to a setting, provider order, gate or key is kept as history with who made it and when, and takes effect within 60 seconds with no change in any calling system. |
| R-ADM-06 | The administrator grants and ends the roles of administrator, glossary keeper and memory reviewer (each for one language) and system owner. Each role has an end date or none; ending a role takes effect at once. |
| R-ADM-07 | Changes to how systems call ROSETTA only add to what exists. A change that would break a caller runs beside the old way for at least 6 months (default: 6 months), and every system owner is told. |

## 4. Non-functional requirements

| ID | Requirement |
|---|---|
| R-NFR-01 | **Capacity:** up to 30 calling systems; 3,000 requests an hour, with up to 300 provider calls an hour and 30 a minute at peak; 20 jobs waiting; a glossary of 20,000 entries and a memory of 500,000 pieces. |
| R-NFR-02 | **Speed:** answers wholly from the glossary, memory or cache in under 1 second for 95 percent of requests. A 500-character translation or grammar check through a browser source typically in 2 to 6 seconds; ROSETTA adds no more than 0.5 seconds to a provider's time. Screens load in under 2 seconds. |
| R-NFR-03 | **Weak connections:** screens work on a 1 Mbps connection; each page is under 200 KB after its first load and needs no large script. |
| R-NFR-04 | **Devices:** screens work on current Chrome, Edge and Firefox on office computers and on phones from 360 pixels wide. |
| R-NFR-05 | **Languages:** screens and messages in Sinhala, Tamil and English, chosen by the user and remembered. All text is Unicode, normalised the same way before it is compared or stored, so the same word is always matched. Every answer names the language of its text, for screen readers. |
| R-NFR-06 | **Accessibility:** screens meet WCAG 2.2 level AA. |
| R-NFR-07 | **Availability:** ROSETTA is always on and never sleeps. If it stops answering, the administrator is alerted within 5 minutes by the platform monitor, which does not depend on ROSETTA or the email service. |
| R-NFR-08 | **Recovery:** the glossary, memory, tracked translations, settings, keys and records are backed up nightly and can be restored to any moment. Requests in progress during a restart fail with a reason and may be sent again; jobs carry on from where they stopped. |
| R-NFR-09 | **Continuity:** while every provider for a direction is down, the glossary and memory still answer, and every other request fails at once with its reason; callers must keep working without machine translation. While no provincial model is joined, confidential text is refused (R-MSK-01). |
| R-NFR-10 | **Security:** keys are stored so they cannot be read back; all traffic is encrypted; ROSETTA's own credentials for BUMBLEBEE and JARVIS live only on the server, never in the repository or on the screens; browser calls are accepted only from listed addresses (R-CAL-06); every request to the screens passes the provincial access policy (CERBERUS) once it runs. |
| R-NFR-11 | **Privacy (Personal Data Protection Act, No. 9 of 2022):** each calling system decides what it sends and marks its sensitivity, and answers for that under the Act. ROSETTA keeps no grammar text (R-GRM-05), never keeps personal or confidential text after the answer (R-MSK-01), masks identity card and phone numbers before text leaves the shared server (R-MSK-02), and keeps no text in records (R-REC-01). Screens load no analytics, trackers or outside fonts. Development and evaluation use invented text only. |
| R-NFR-12 | **Retention:** cache 30 days (R-TRN-09); grammar results 10 minutes in memory (R-GRM-05); job inputs and results 7 days (R-DOC-06); tracked translations 2 years without change (R-REV-05); memory and glossary until replaced; usage records 2 years and totals 5 years (R-REC-05). |
| R-NFR-13 | **Third-party privacy:** an outside site used as a browser source receives the text, after masking, and keeps or uses it under that site's own terms, not ROSETTA's. The provider catalogue (R-PRV-02) records where each provider processes text, so the Office and system owners can choose. |
| R-NFR-14 | **Maintainability:** one small program. Adding a provider of a known kind needs a setting only. Development runs with one command, with stand-in providers and a stand-in email service, and the volunteers' shared test set can be run against any provider to produce the evaluation the gate needs (R-GAT-01). |

## 5. Technology stack

| Part | Choice |
|---|---|
| Service | Go, one small program on ep-shared-01 under the ALLSPARK STACK; always on (an exception the user approved, joining the platform network as a shared service like JARVIS); memory cap 250 MB, Go memory target 200 MB, 1 CPU; logs 3 files of 10 MB; read-only root filesystem; a `-healthcheck` flag that exits 0 when healthy |
| Data and files | Database `rosetta` in the shared PostgreSQL through PgBouncer (pool 5, migrations pool 1); cache in the database, grammar results in process memory only (no cache server); job files in ROSETTA's own SeaweedFS bucket, uploads and downloads by signed links (10 MB, .docx and .txt only), removed on schedule |
| Request API | HTTPS JSON, versioned; callers on ep-shared-01 call ROSETTA directly on the platform network; callers on other machines through Cloudflare to `/api/` only, with signed requests and their key; keys stored hashed |
| Grammar API | LanguageTool-compatible check and languages endpoints, so the editor's ONLYOFFICE LanguageTool plug-in works unchanged; called from the officer's browser with MJOLNIR sign-in, CORS only for the listed editor addresses |
| Browser sources | BUMBLEBEE on its own machine, reached only through its Cloudflare tunnel from a link container on ep-shared-01 with an Access service token, plus ROSETTA's own key; synchronous calls of at most 5,000 characters; BUMBLEBEE keeps no text |
| Model providers | Through JARVIS only (STACK rule: no system calls a model directly), at `http://jarvis:8080` on the platform network, as one of JARVIS's named uses; the models are researched in this repository and run behind JARVIS |
| Schedules | PARADOX, contract `sign/v0.2.0` (`apps/rosetta/schedules.yaml`, `paradox.pub`): jobs through PARADOX's queue, provider checks, nightly totals and removals, daily flags; each call signed and checked |
| Screens and sign-in | Go templates and htmx; MJOLNIR (OpenID Connect); Cloudflare Access in front of rosetta.epc.lk screens; CERBERUS checked by Caddy once it runs |
| Links to other services | TVA's published register file read from SeaweedFS with a read-only key; PENSIEVE for audit entries; HEDWIG for alert email and job notices |
| Operations | Uptime Kuma on `/__up` and a container monitor; one env file for secrets on the server, `.example` only in the repository; PHOENIX nightly backup of the database and bucket; Docker Compose with stand-in providers for development; GitHub Actions builds tagged images to GHCR; `deploy.sh` |

## Annex A: Entity relationship diagram

To be produced with the companion prompt.

## Annex B: Technical architecture

To be produced with the companion prompt.
