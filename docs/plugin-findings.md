# The ONLYOFFICE LanguageTool plug-in, read against R-CAL-06

Story E-GRM-01. Found by reading the plug-in's source (`sdkjs-plugins/content/languagetool/scripts/langTool.js`, version 1.0.6, in ONLYOFFICE's public plug-ins repository). Not yet watched running on a Document Server: E-GRM-07 does that. The version SCOS Pro installs may differ.

| Question | Finding | Consequence |
|---|---|---|
| Does it send credentials? | **No.** `$.ajax` is called with no `xhrFields`, so `withCredentials` is off, and a browser sends no cookie on a cross-origin call. | **As shipped, no sign-in reaches ROSETTA.** CERBERUS refuses the check, and R-CAL-06's "only while the officer is signed in" cannot hold. The owner's decision. |
| Can it be pointed at ROSETTA unchanged? | **No, one file is edited.** The address is fixed in `serviceUrl` and again in `getLanguages()`. The plug-in's own README gives editing that file as the way to use your own service. | R-GRM-01's "unchanged" means the file's addresses change. |
| Does a check send a preflight? | **No.** The check is a form POST (`application/x-www-form-urlencoded`) and the languages call a plain GET, both CORS simple requests. | The `OPTIONS` mark stays harmless. With credentials on, ROSETTA still names the exact origin and allows credentials, or the browser hides the answer. |
| Does it send text only on Check? | **Yes.** `checkText` runs only from the Check button. No timer. | K-SES-03's timer mark is not needed. |
| Language codes? | Sends `language` as the list's `longcode`, and `auto` by default, as well as a code such as `en-GB`. `enabledOnly=false` is also sent. | ROSETTA's list offers English, and treats `auto` and any `en-*` as English. |
| How does it read a refusal? | Shows `e.responseText` in the result panel. | A refusal needs CORS headers to be read, or the panel shows nothing. Refusals carry plain text in the reader's language. |
