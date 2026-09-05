---
name: review-pr
description: Review a GitHub or Origin pull request by gathering origin pr view/diff or gh pr view/diff (detect forge from the git remote or URL), then running skeptic-code-review. Use when asked to review a PR, pull request, or GitHub or Origin review.
---

# Review PR

1. Gather the PR. Detect forge from **either** a user-supplied URL **or** the git remote — never mix a number from one forge into the other.
   - If the user gave a URL, classify **only** that URL (parse the host; do not substring-match `github.com` / `cursor.com` inside a path or credential):
     - host `github.com` → GitHub; use that URL with `gh`.
     - host `cursor.com` and path `/codebase/<owner>/<repo>/pull/N` → Origin; pass that URL to `origin pr view` / `origin pr diff`.
     - host `origin.cursor.com` and path ending `/pull/N` → Origin, but do **not** pass that URL through (the CLI rejects it). Use `origin pr view N` or rewrite to `https://cursor.com/codebase/<owner>/<repo>/pull/N`.
     - any other host → ask.
   - If there is no URL, classify `git remote get-url origin` only (HTTPS or `git@host:path`; parse the host): host `origin.cursor.com` → Origin; host `github.com` → GitHub; any other host → ask.
   - **Origin** commands: `origin pr view` and `origin pr diff`. Locators: current branch, a number, `'#N'` (quoted — unquoted `#N` is a shell comment), or an Origin pull URL as above. `-R owner/repo` is a repo override used together with a locator when the git remote is the wrong repo, not a locator by itself.
   - **GitHub** commands: `gh pr view` and `gh pr diff` with a positional number, URL, or the current branch.
   If the chosen forge’s CLI errors, ask for a URL, number, or a pasted diff — do not invent one, and do not reuse the same number or foreign URL on the other CLI. Current-branch with no user locator may try the other forge only if you still do not pass a number or a foreign URL.
2. Run the `skeptic-code-review` skill with that diff, the PR title/body as intent, and the workspace path.
3. Stop-the-line, effectiveness, and `capture-lesson` rules from that skill apply. A SHAPE / DIRECTION kick-back stops the line — do not “just fix it” on this PR. After the loop, run `record-hint-outcome` if there is signal; otherwise say `no effectiveness signal`.
