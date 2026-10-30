# Getting started on GitHub

A step-by-step guide for anyone who has not worked on a shared GitHub project before. It takes about 30 minutes the first time.

## 1. Set up once

1. **Create a GitHub account** at https://github.com/signup if you do not have one. Use a username you are happy to show on a certificate.
2. **Turn on two-factor authentication:** your avatar, **Settings**, **Password and authentication**.
3. **Install Git** from https://git-scm.com/downloads and tell it who you are:
   ```sh
   git config --global user.name "Your Name"
   git config --global user.email "the-email-on-your-github-account@example.com"
   ```
   The email must match your GitHub account, or your commits will not be counted for you.
4. Optional but easier: install **GitHub Desktop** (https://desktop.github.com) or the **GitHub CLI** (https://cli.github.com).

## 2. Fork and clone

1. On https://github.com/Rumeasiyan/rosetta press **Fork**. You now have your own copy, `your-name/rosetta`.
2. Copy your fork to your computer:
   ```sh
   git clone https://github.com/YOUR-USERNAME/rosetta.git
   cd rosetta
   git remote add upstream https://github.com/Rumeasiyan/rosetta.git
   ```

## 3. Work on an issue

1. Pick an issue and comment that you are taking it (see [CONTRIBUTING.md](../CONTRIBUTING.md)).
2. Get the latest work and make a branch for the issue:
   ```sh
   git checkout main
   git pull upstream main
   git checkout -b 12-short-description
   ```
3. Make your change. Then save it in a commit:
   ```sh
   git add .
   git commit -m "Add Tamil glossary sample"
   git push origin 12-short-description
   ```
4. GitHub shows a **Compare and pull request** button on your fork. Press it, check the target is `Rumeasiyan/rosetta` `main`, fill in the template and write `Closes #12`.

## 4. After review

- A reviewer may ask for changes. Make them on the same branch, commit and push again: the pull request updates by itself.
- When it is merged, update your copy and delete the branch:
  ```sh
  git checkout main
  git pull upstream main
  git push origin main
  git branch -d 12-short-description
  ```

## 5. Only adding a document?

You can do it all in the browser: open your fork, go to the folder (for example `docs/research/`), **Add file**, **Create new file**, write it, then choose **Create a new branch** and **Propose changes**. It becomes a pull request like any other.

## 6. Stuck?

Ask in the issue you are working on. Someone who knows will answer there, and the next person finds the answer too.
