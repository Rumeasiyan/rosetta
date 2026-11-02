# The ONLYOFFICE LanguageTool plug-in, against R-CAL-06

Stories E-GRM-01 and E-GRM-07. First read from the plug-in's source (`sdkjs-plugins/content/languagetool/scripts/langTool.js`, version 1.0.6, in ONLYOFFICE's public plug-ins repository), then watched running: Document Server 8.2.3-6 Community in a local container, JWT off, the editor at `https://writedocument.epc.test`, ROSETTA from source at `https://rosetta.epc.test`, a page at `https://page.epc.test` opening an invented `.docx`, real Caddy with its internal CA and `forward_auth` to CERBERUS's stand-in, and Chrome headless, driven by Playwright with every call recorded. `dev/plugin-test/` holds the harness.

Two forms of the plug-in were run. **A:** addresses edited only. **B:** A, plus credentials on.

## The edit a Document Server's installer makes

Ready for the owner's decision. In `scripts/langTool.js` of the plug-in as fetched, four lines, of which two are new (the file indents with tabs, shown here as spaces). On epc.lk the address is `https://rosetta.epc.lk`.

```diff
-    var serviceUrl       = "https://api.languagetoolplus.com/v2/check";
+    var serviceUrl       = "https://rosetta.epc.test/api/v2/check";
```

```diff
     function getLanguages() {
-        var url = "https://api.languagetoolplus.com/v2/languages";
+        var url = "https://rosetta.epc.test/api/v2/languages";
         return $.ajax({
             method: 'GET',
+            xhrFields: {withCredentials: true},
             url: url
         });
     };
```

```diff
         $.ajax({
             method: 'POST',
+            xhrFields: {withCredentials: true},
             beforeSend: function(request) {
```

The folder goes into `/var/www/onlyoffice/documentserver/sdkjs-plugins/`, named by the plug-in's GUID, `{7CDB02C9-A0BF-4B56-9A1A-71C860B8498F}`. Document Server then lists it by itself in `/plugins.json`, with no restart and no image rebuild. Without credentials on, nobody is signed in and every check is refused. The edit must be made again whenever the plug-in is updated.

What the owner should weigh with it:

- **The plug-in's frame has the editor's origin**, so any plug-in on that Document Server could call ROSETTA as the officer if it turned credentials on. CORS cannot tell them apart.
- **It loads three scripts and styles from the public internet** on each opening (below). They run in the frame that holds the officer's text and may send the cookie. The Document Server has its own copy under `sdkjs-plugins/v1/`; changing `index.html` to use it would be a further edit, not walked.

## What was read, and what was watched

| Question | Read from the source | Watched | Consequence |
|---|---|---|---|
| Does it send credentials? | No: no `xhrFields`. | **Confirmed.** Run A: no `Cookie` on either call, though the sign-in cookie was set for `.epc.test`. Run B: `Cookie` on both. | As shipped no sign-in reaches ROSETTA. The owner's decision. |
| Can it be pointed at ROSETTA unchanged? | No: the address is in `serviceUrl` and `getLanguages()`. | **Confirmed.** | R-GRM-01's "unchanged" means the file's addresses change. |
| Which origin is the plug-in's frame? | Assumed the editor's. | **Confirmed.** `self.origin` in the frame is `https://writedocument.epc.test`; its parent is the editor on that origin, and the page above is another. `Origin` on the calls is the same, and `Referer` is that address and `/`. | The address to list on the system is the editor's, not the page's. |
| Does a check send a preflight? | No: a form POST and a plain GET. | **Confirmed.** No `OPTIONS` in any run, credentials on or off. | The `OPTIONS` mark stays harmless. ROSETTA's named origin with credentials allowed lets the browser read the answer: seen. |
| Does the cookie reach ROSETTA? | Not asked. | **No, as designed.** Caddy strips it after the check. Asked with curl, a page behind the same guard saw other cookies pass, the account header arrive, and no sign-in cookie. | As JOINING describes. |
| Does it send text only on Check? | Yes: `checkText` runs only from the Check button. | **Partly refuted.** Nothing is sent on opening or on a change of selection, and no timer: opening sends only `GET /api/v2/languages`, no text. But accepting a suggestion clicks Check itself and sends the whole text again. | K-SES-03's timer mark is not needed. One officer working through a letter sends one check for each correction, against the system's limit. PRD Annex B's "only when they click Check" is left for the owner with the edit. |
| Language codes? | `auto` at first, and the list's `longCode`. `enabledOnly=false` too. | **Confirmed, and more.** Fields sent are `text`, `language`, `enabledOnly` and nothing else. `auto` on the first check; after an answer with a match the panel moves to English and sends `en-GB` from then on. `si-LK` and `ta-LK` when chosen. | ROSETTA answers `auto` and `en-GB`, and refuses Sinhala and Tamil with a 422 whose words the panel shows. Its Auto is not detection: English is assumed. |
| How does it read a refusal? | Shows `e.responseText` in the panel. | **Confirmed.** With CORS headers, the words appear in red. Without, the browser hides the answer and the panel shows **nothing**: the reason is only in the console. | CERBERUS must list the editor's address (E-ENB-10), or a refusal is a blank panel. |
| What of a refused language list? | Not read. | **Nothing visible.** `ouch[object Object]` in the console and an empty list. Check still works, and is refused in turn. | Run A and a signed-out officer open a panel with an empty list. |
| What does it read of an answer? | The fields in `internal/api/plugin_test.go`. | **Confirmed.** A real request is now in that test, byte for byte. | |
| What text does it send? | Line breaks doubled; `<` and `>` as `&lt` and `&gt`. | **Confirmed** on the wire. An invented `<img>` in the text came back and stayed inert, in the field and the panel. | Offsets are of the text as sent. |

## Run A: addresses edited only

Signed in, cookie set, plug-in otherwise stock. `GET /api/v2/languages` and, on Check, `POST /api/v2/check` both left with no cookie and got CERBERUS's 401. **Refused, as the source said.** The panel showed CERBERUS's words, "Please reload this page and sign in again." Reloading cannot help, since the plug-in will never send the cookie, so an officer is told something untrue. The language list stayed empty. Selection and the panel kept working; typing after a refusal was tried in run B.

## Run B: credentials on

| Tried | Seen |
|---|---|
| The cookie | Sent on both calls; `Sec-Fetch-Site: same-site`; the stand-in's account reached ROSETTA. |
| The language list | Auto, English, "Sinhala (not offered)", "Tamil (not offered)". |
| Check, on three paragraphs with an emoji and a phone number | Four suggestions, each marked on the right word, in the field and in each context. BUMBLEBEE's stand-in received the number masked. |
| Each correction, then **Insert to document** (the name this version gives Replace) | The selection, all three paragraphs, became the corrected text: spelling right, emoji and number intact. |
| Sinhala, Tamil | `si-LK`, `ta-LK`, 422, and in the panel: "ROSETTA does not offer this language for this work." |
| English, Auto | `en-GB`, `auto`, answered. |
| A text of about 8,000 characters | Split and joined: 80 of 80 marks, each on the right word. |
| ROSETTA's own refusals | 503 and 429: their plain words in the panel. |
| Cookie removed on the open panel | 401, "Please reload this page and sign in again." |
| An office rule (403) | The stand-in's rule words and reference in the panel. |
| Either, with the address not registered at CERBERUS | No CORS headers, empty panel. |
| CERBERUS stopped | Caddy's 502, no CORS headers, empty panel. |
| Typing in the document | Worked after the cookie's removal and with CERBERUS stopped. |
| Signing in again | Check worked on the same panel, with nothing reopened. |
| Logs | The text did not appear in ROSETTA's log. |

## A hazard in the plug-in itself

**Insert to document acts on the selection as it is when clicked, with the whole field's text.** While mistakes stand, the plug-in stops following the selection. An officer who checks one passage, clicks elsewhere and then inserts overwrites the new selection with the checked passage. In the walk, selecting the second line after a check and inserting put the three checked lines in its place, so the letter held them twice. From the source, with nothing selected it inserts at the cursor. This is stock behaviour and the edit above does not change it.

## What remains unproven

- The real CERBERUS and MJOLNIR: the stand-in reads an invented cookie, and real cookies' attributes are theirs (E-ENB-10). The stand-in's `SameSite=Lax` cookie went because all three names are same-site; a page outside that site was not tried.
- Installing by bind mount. The mount as first set up came up empty, apparently because Docker could not share the temporary folder it named, and Document Server was to run once, so the plug-in went in with `docker cp`. The mount in `dev/plugin-test/compose.yaml` has not run. The folder, its name and `/plugins.json` listing it are what was watched.
- Any browser but headless Chrome on macOS.
- Part of a document selected before the first check: the walk's selection tries came after a check of the whole, while the panel held its text.
- Rich text: the document was plain paragraphs. What Insert to document does to bold, tables or lists was not seen.
- Sinhala or Tamil **text** under Auto. The languages were chosen by firing the list's own event from the walk, not with the mouse, and text in the field was set by script in the long-text and bracket tries.
- The usage record: `usage_record` stays empty until E-REC-02 fills `Deps.Usage`. The system's `last_request_at` moved and its limit applied.
- The version SCOS Pro installs, should it differ from 1.0.6.
