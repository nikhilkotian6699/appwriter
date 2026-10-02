# Writers' Guild — instructions for Claude Code

## Start of every session

1. Read `docs/STATUS.md`. It is the single source of truth for which
   milestone and feature branch we are on and what is still open.
2. Cross-check with `git log --oneline -5` and `git branch -r`. Commit
   messages follow `M<n>: <title>`, so git alone tells you the last finished
   milestone. If `docs/STATUS.md` disagrees with git, trust git and fix the
   file.
3. Read `docs/BRIEF.md` (the product brief and milestone list) and
   `docs/DEVELOPMENT.md` (architecture, per-milestone flow, environment).

## Working rules

- Build milestone by milestone. **Stop after each milestone** and hand over
  for testing; do not start the next one unprompted.
- Work on the matching `feature/*` branch (see the table in
  `docs/STATUS.md`); the user merges to `develop` on GitHub. Rebase a
  downstream branch onto `develop` after its upstream branch is merged.
- Exactly **one commit per milestone**, message `M<n>: <title>`.
- Verify each milestone against the real gateway alias (`lumos-chat`) through
  the docker container, not only the fake gateway. If no key is available,
  record the pending live check under "Open items" in `docs/STATUS.md`.
- Stop every dev server you start once a check is done; leave only the
  container running.
- **Never** write the gateway key, any password, or other secret into the
  repo, a commit message, or chat. They live only in the local `.env`.

## Keeping status current

- Update `docs/STATUS.md` **before** each milestone commit: milestone status
  and commit hash, branch statuses, "In progress", "Open items", and the
  "Last updated" line.
- When you start work on a branch, mark it "In progress"; when you push it,
  mark it "Pushed (awaiting merge)".
- Housekeeping commits that only touch `docs/STATUS.md` or `CLAUDE.md` are
  allowed outside the one-commit-per-milestone rule.
