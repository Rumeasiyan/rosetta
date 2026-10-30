# Contributing to ROSETTA

Thank you for helping. This guide explains how work happens here. Read it once before you start, and come back to it when unsure.

**Read [docs/STANDARDS.md](docs/STANDARDS.md)** for how we name, write, format and check everything: issues, branches, commits, pull requests, files, lint.

New to GitHub? Start with [docs/GETTING-STARTED.md](docs/GETTING-STARTED.md): it walks through forking, branches and pull requests step by step.

## 1. Nobody assigns work: you take the lead

**Work is not assigned.** Look at the open issues, pick something, or open a new issue for an idea of your own, and take it to the end. Please do not wait to be given a task.

- **Pick an issue.** Start with one labelled `good first issue` if you are new.
- **Claim it.** Comment "I am taking this" and write in two or three lines how you plan to do it. One person (or one pair) per issue.
- **Finish it, or hand it back.** If you cannot continue, say so in the issue so someone else can take it. An issue with no activity for 14 days may be taken by someone else.
- **Have an idea?** Open an issue first, so others can see and comment before you build.

## 2. Questions go in the issue

If you have a doubt about a piece of work, **ask in that issue**, not in private chat. The answer then stays with the work, and the next person with the same question finds it. General questions that fit no issue: open one with the `question` label.

## 3. How a piece of work moves

1. **Fork** this repository to your own account (once).
2. **Create a branch** for each issue, named after it: `12-tamil-glossary-format`.
3. **Commit little and often.** Each commit does one thing and says what: `data: add Tamil glossary sample`, not "update" or "fix".
4. **Open a pull request** to `main` of this repository. Fill in the template: what, why, which issue (`Closes #12`), how you checked it. Open it early as a **draft** if you want feedback.
5. **Review.** At least one other contributor reviews. Answer every comment: change what is asked, or explain why not.
6. **Merge** after approval (squash merge). The issue closes with it.

Keep pull requests small: if a change takes more than fifteen minutes to review, split it.

## 4. Reviewing others' work

Reviewing counts as contribution. Read the change, try it where you can, and say what is good as well as what to change. "Looks good" alone is not a review.

## 5. How contributions are recognised

All work is tracked **only through this repository**:

- issues opened, discussed and resolved;
- pull requests opened and merged;
- commits;
- reviews given.

**Certificates and letters of appreciation are issued from this record.** Work that is not in the repository (only in chat, email or a shared folder) cannot be counted, so put your findings into an issue or a pull request. Use your own GitHub account and make sure your commits carry it.

## 6. Research findings

Much of the early work is research: models, tools, datasets and papers for Sinhala, Tamil and English translation. Record each finding with the **Research finding** issue template, and put longer write-ups in `docs/research/` through a pull request. See [docs/RESEARCH.md](docs/RESEARCH.md).

## 7. Rules that have no exceptions

- **No real government documents, names, NIC numbers or phone numbers.** Use public or made-up text only.
- **Check the licence of every dataset, model and library** before you add or recommend it, and write the licence in the issue or pull request. If it does not allow our use, it does not go in.
- **Never commit passwords, keys or tokens.** If it happens by mistake, report it at once (see [SECURITY.md](SECURITY.md)); deleting the line is not enough.
- **This repository is public.** Anything committed can be seen by anyone, forever.
- Follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## 8. Licence

By contributing, you agree that your contribution is licensed under this repository's [MIT License](LICENSE).
