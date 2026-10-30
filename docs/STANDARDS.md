# Working standards

How we name, write, format and check everything in this repository. Following these keeps the work easy to review, easy to find, and easy for the next person to pick up. Reviewers will point back here.

## Contents

1. [Issues](#1-issues)
2. [Branches](#2-branches)
3. [Commits](#3-commits)
4. [Pull requests](#4-pull-requests)
5. [Reviews](#5-reviews)
6. [Files and folders](#6-files-and-folders)
7. [Text in three languages](#7-text-in-three-languages)
8. [Formatting and lint](#8-formatting-and-lint)
9. [Code](#9-code)
10. [Documents](#10-documents)
11. [Data, models and large files](#11-data-models-and-large-files)
12. [Before you open a pull request](#12-before-you-open-a-pull-request)

---

## 1. Issues

- **One issue, one thing.** "Compare three Sinhala-Tamil models" is one issue; "research everything" is not.
- **Title says the outcome**, short and specific:
  - Good: `Test-set: 50 government-style sentences in each language`
  - Bad: `test set`, `Help needed`, `Issue`
- **Use a template** (research finding, idea, question). Fill every section; write "not known yet" rather than leaving it blank.
- **Labels:** at least one of `research`, `idea`, `question`, `docs`, `bug`; add `good first issue` for small, well-explained tasks.
- **Claim before you start:** comment "I am taking this" and your plan in two or three lines.
- **Keep it moving:** post a short update at least once a week while you work on it; say if you stop.
- **Link related work:** `Related to #7`, `Depends on #12`.
- **Close it with the pull request** (`Closes #12`), or with a comment saying why it is closed without one.

## 2. Branches

- **Never work on `main`.** `main` only changes through pull requests.
- **One branch per issue**, created from the latest `main`.
- **Name:** `<issue number>-<short-description>`, lower case, words joined by hyphens:
  - Good: `12-tamil-glossary-format`, `31-nllb-sinhala-benchmark`
  - Bad: `my-branch`, `Fix`, `test_new_final2`
- **Short-lived:** a branch should live days, not weeks. Merge or close it.
- **Keep it up to date:** bring in new work from `main` before asking for review (`git pull upstream main`).

## 3. Commits

- **Small and focused:** each commit does one thing. If your message needs "and", it is probably two commits.
- **Message format:**

  ```text
  <type>: <what, in the imperative, under 72 characters>

  <optional body: why, and anything a reviewer must know; wrap at 72>

  Refs #12
  ```

- **Types:**

  | Type | For |
  |---|---|
  | `feat` | new feature or capability |
  | `fix` | a bug fix |
  | `docs` | documents only |
  | `research` | research write-ups, results, test runs |
  | `data` | test sets, glossaries, sample data |
  | `test` | tests only |
  | `refactor` | code change with no change in behaviour |
  | `chore` | tooling, configuration, housekeeping |

- **Imperative mood**, as if giving an order: "Add", "Fix", "Remove", not "Added" or "Fixes".
  - Good: `research: add NLLB-200 Sinhala to Tamil results on test set`
  - Good: `docs: explain how to run the benchmark script`
  - Bad: `update`, `fixed stuff`, `final`, `asdf`, `changes as per review`
- **Your own identity:** commit with the email on your GitHub account, or the commit is not counted for you.
- **Never commit:** secrets, real personal data, large files (see section 11), generated files, editor or system files (`.DS_Store`, `.vscode/`, `__pycache__/`).

## 4. Pull requests

- **One pull request per issue**, and **small**: a reviewer should finish it in about 15 minutes (as a guide, under about 300 changed lines, not counting data files).
- **Title** follows the commit format: `research: compare three Sinhala-Tamil models`.
- **Description:** fill the template: what, why, `Closes #12`, how you checked it.
- **Draft early** if you want feedback before it is finished; mark it **Ready for review** when it is.
- **Checks must pass** (section 8) before review.
- **Answer every review comment:** change it, or explain why not, then resolve the conversation.
- **Do not merge your own pull request.** A reviewer approves; then it is squash-merged.
- **No unrelated changes:** do not reformat files you are not working on.

## 5. Reviews

- Review within **two days** when you are asked.
- **Check:** does it do what the issue asks; is it correct; is it clear; are sources and licences given; no secrets or personal data.
- **Be specific and kind:** say what to change and why, and what is good. Suggest with GitHub's "suggestion" feature where you can.
- **Label your comments:** `must:` (blocks merging), `should:` (please change), `nit:` (small, optional).
- **Approve** only when you would be happy to maintain the change yourself.

## 6. Files and folders

- **Names:** lower case, words joined by hyphens, no spaces, English letters only:
  - Good: `docs/research/nllb-200-benchmark.md`, `scripts/run-benchmark.py`
  - Bad: `My Research (final).md`, `NLLB_Results V2.md`, `සිංහල.txt`
- **Exceptions** by convention: `README.md`, `CONTRIBUTING.md`, `LICENSE`, `Makefile`, `Dockerfile`, and Go files (`snake_case.go`, as Go requires).
- **Dates in names:** `YYYY-MM-DD`, e.g. `2026-11-02-meeting-notes.md`.
- **No version numbers in names** (`v2`, `final`, `new`): Git keeps the versions.
- **Where things go:**

  | Folder | Contents |
  |---|---|
  | `docs/` | guides for contributors |
  | `docs/research/` | research write-ups, one topic per file |
  | `docs/research/test-set/` | the shared test sentences |
  | `glossary/` | the shared term list |
  | `scripts/` | small scripts for research and benchmarks |
  | `brand/` | logos (do not change without asking) |

  New top-level folders: propose them in an issue first.

## 7. Text in three languages

- **UTF-8 everywhere**, no BOM.
- **Unicode NFC normalisation** for all Sinhala and Tamil text before saving or comparing, so the same word is always the same bytes. In Python: `unicodedata.normalize("NFC", text)`.
- **Never fake a script with images or legacy fonts** (FM Abhaya, Bamini and the like). Unicode only.
- **Test sets:** one sentence per row, in **TSV** (tab-separated) or **JSONL**, with columns `id`, `en`, `si`, `ta`, `source`, `notes`. `source` says where the sentence came from (made up, public site with link).
- **Label the language** of every sample with its code: `en`, `si`, `ta`.

## 8. Formatting and lint

The repository has settings files, and a check runs on every pull request. Set up your editor once so it formats on save.

| What | Tool | Settings file | Run it yourself |
|---|---|---|---|
| All files: UTF-8, LF line ends, final newline, spaces | EditorConfig | `.editorconfig` | your editor applies it (install the EditorConfig plugin if needed) |
| Markdown | markdownlint | `.markdownlint.json` | `npx markdownlint-cli2 "**/*.md"` |
| Python | Ruff (format and lint) | `pyproject.toml` | `ruff format . && ruff check .` |
| Go | gofmt and go vet | (built in) | `gofmt -l . && go vet ./...` |
| JSON and YAML | Prettier | (defaults) | `npx prettier --check "**/*.{json,yml,yaml}"` |

- **Format before you commit.** A pull request with formatting failures is not reviewed.
- **Fix lint warnings, do not silence them.** If a rule truly does not fit, explain the exception in the pull request.

## 9. Code

- **Research scripts in Python** (3.11 or newer), in `scripts/`; the service itself, when we build it, in **Go**. Other languages: propose in an issue first.
- **Readable first:** clear names (`source_sentence`, not `s1`), short functions, comments that explain *why*, not *what*.
- **Every script starts with a short docstring:** what it does, how to run it, what it needs.
- **Dependencies:** list them (`requirements.txt` with versions pinned, or `go.mod`). Check each one's licence.
- **No hard-coded paths, keys or passwords.** Use arguments or environment variables; never commit `.env`.
- **Reproducible results:** fix random seeds; write down the model name and version, the hardware, and the time taken.
- **Notebooks:** allowed for exploring, but clear the outputs before committing, and move anything that matters into a script or a write-up.
- **Tests** for any code others will reuse; run them before each pull request.

## 10. Documents

- **Markdown**, one topic per file, with a title and a one-line summary at the top.
- **Plain, short sentences.** Many readers use English as a second language.
- **Cite every source** with a link, and say which version or date you looked at.
- **Show results as tables**, with the test set and settings stated.
- **Headings** in sentence case (`## How we compare options`), one `#` title per file.
- **Images** go in an `images/` folder next to the document, named like files (section 6), with alt text.

## 11. Data, models and large files

- **No model weights, archives or large datasets in the repository.** Link to where they can be downloaded, with their licence.
- **Size limit:** no single file over **5 MB** without agreement in an issue first.
- **No real personal or government data**, ever. Made-up or public text only, with the source stated.
- **State the licence** of every dataset you add or use. If you cannot find it, the data does not go in.

## 12. Before you open a pull request

- [ ] It answers one issue, and says `Closes #...`
- [ ] Branch is up to date with `main`
- [ ] Formatted and lint passes (section 8)
- [ ] Commit messages follow section 3
- [ ] File names follow section 6
- [ ] Sources and licences stated; no secrets; no real personal data
- [ ] You have read your own diff once, top to bottom
