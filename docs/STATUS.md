# Build status

Single source of truth for where the Writers' Guild build stands. Update this
file as part of every milestone commit (see `CLAUDE.md`). When this file and
the git history disagree, the git history wins; fix this file.

Last updated: 2026-10-02 · `develop` at `6cd68b0`

## Milestones

Defined in `docs/BRIEF.md` ("Milestones") and mapped in `docs/DEVELOPMENT.md`
(section 4). One commit per milestone, message `M<n>: <title>`.

| # | Scope | Status | Commit |
|---|-------|--------|--------|
| 1 | Projects, chapters, editor, story bible, writers, test writer, fake gateway | Done | `d9eb22a` |
| 2 | Critique workflow: run engine, SSE, parallel critics, JSON validation, Langfuse metadata | Next | |
| 3 | Editor-in-chief synthesis, issue list, accept/reject | Not started | |
| 4 | Revision with diff and hunks, bible-keeper proposals | Not started | |
| 5 | Co-writing workflow and compare mode | Not started | |
| 6 | Run history, cost and writer stats, settings page | Not started | |
| 7 | Accounts: login, sessions, workspace per user, admin adds users, isolation test | Not started | |
| 8 | Account management: usage per user, roles, passwords, disable, delete | Not started | |
| 9 | In-app guide for new users | Not started | |

## Feature branches

All work happens on a `feature/*` branch; the user merges to `develop` on
GitHub. All 23 branches were created from `develop` at `d9eb22a` on
2026-10-01. A downstream branch must rebase onto `develop` after its upstream
branch is merged.

| Milestone | Branch | Depends on | Status |
|-----------|--------|------------|--------|
| any | `feature/ci` | — | Not started |
| any | `feature/guide-content` | — | Not started |
| 2 | `feature/run-engine` | — | Pushed (awaiting merge) |
| 2 | `feature/text-tooling` | — | Pushed (awaiting merge) |
| 2 | `feature/structured-output` | text-tooling | Not started |
| 2 | `feature/critique-workflow` | run-engine, structured-output | Not started |
| 2 | `feature/critique-ui` | critique-workflow | Not started |
| 3 | `feature/editor-in-chief` | critique-workflow | Not started |
| 3 | `feature/issue-decisions` | editor-in-chief | Not started |
| 4 | `feature/word-diff` | — | Not started |
| 4 | `feature/revision-workflow` | issue-decisions, word-diff | Not started |
| 4 | `feature/bible-keeper` | revision-workflow | Not started |
| 5 | `feature/cowrite` | run-engine | Not started |
| 5 | `feature/compare-mode` | cowrite | Not started |
| 6 | `feature/run-history` | — | Not started |
| 6 | `feature/writer-stats` | — | Not started |
| 7 | `feature/auth-lib` | — | Not started |
| 7 | `feature/login` | auth-lib | Not started |
| 7 | `feature/users-and-account-pages` | login | Not started |
| 7 | `feature/isolation-test` | login | Not started |
| 8 | `feature/account-usage` | users-and-account-pages | Not started |
| 8 | `feature/account-management` | users-and-account-pages | Not started |
| 9 | `feature/guide-screenshots` | everything else | Not started |

Status values: Not started · In progress · Pushed (awaiting merge) · Merged.

## In progress

Milestone 2, built as a stack of branches (each branched off the previous one
and pushed; merge them to `develop` in the order run-engine, text-tooling,
structured-output, critique-workflow, critique-ui). One commit per branch,
messages `M2: <title> (n/5)`.

- `feature/run-engine` is done and pushed: background engine, numbered
  `run_events`, SSE replay with `Last-Event-ID`, cancel, chapter run list,
  `RUN_TIMEOUT_SECONDS`.
- `feature/text-tooling` is done and pushed: token estimation, scene
  splitting at breaks/headings with paragraph fallback, quote anchoring that
  tolerates curly quotes, dashes, whitespace and Markdown markers.
- Next: `feature/structured-output`.

## Open items

- **M1 live gateway check pending.** `.env` still points `LITELLM_BASE_URL` at
  the fake gateway because no real `LITELLM_BASE_URL` / `LITELLM_API_KEY` has
  been supplied. Repeat the M1 live check against the `lumos-chat` alias once
  they land, before or alongside the M2 live check.
- Until Milestone 7 the app runs as a single bootstrap account with no login
  page.

## Environment notes

- Postgres runs in the compose stack on 127.0.0.1:5433 (5432 is taken locally).
- `make test-db` recreates `writersguild_test` after the db container is
  recreated.
- The desktop preview runner cannot read `~/Desktop` (macOS privacy); start dev
  servers from the shell and stop them after each check.
- Secrets live only in the local `.env`. Never write them into the repo, a
  commit, or chat.
