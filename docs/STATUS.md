# Build status

Single source of truth for where the Writers' Guild build stands. Update this
file as part of every milestone commit (see `CLAUDE.md`). When this file and
the git history disagree, the git history wins; fix this file.

Last updated: 2026-10-02 (M3 built) · `develop` at `6cd68b0` on origin (user reports the five M2 branches merged; not yet visible on origin at the time of writing)

## Milestones

Defined in `docs/BRIEF.md` ("Milestones") and mapped in `docs/DEVELOPMENT.md`
(section 4). One commit per milestone, message `M<n>: <title>`.

| # | Scope | Status | Commit |
|---|-------|--------|--------|
| 1 | Projects, chapters, editor, story bible, writers, test writer, fake gateway | Done | `d9eb22a` |
| 2 | Critique workflow: run engine, SSE, parallel critics, JSON validation, Langfuse metadata | Built on 5 feature branches, awaiting merge | see branches |
| 3 | Editor-in-chief synthesis, issue list, accept/reject | Built on 2 feature branches, awaiting merge | see branches |
| 4 | Revision with diff and hunks, bible-keeper proposals | In progress (word-diff, revision-workflow pushed; bible-keeper next) | |
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
| 2 | `feature/structured-output` | text-tooling | Pushed (awaiting merge) |
| 2 | `feature/critique-workflow` | run-engine, structured-output | Pushed (awaiting merge) |
| 2 | `feature/critique-ui` | critique-workflow | Pushed (awaiting merge) |
| 3 | `feature/editor-in-chief` | critique-workflow | Pushed (awaiting merge) |
| 3 | `feature/issue-decisions` | editor-in-chief | Pushed (awaiting merge) |
| 4 | `feature/word-diff` | — | Pushed (awaiting merge) |
| 4 | `feature/revision-workflow` | issue-decisions, word-diff | Pushed (awaiting merge) |
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
- `feature/structured-output` is done and pushed: critique JSON extraction,
  validation against the chapter (severity, quotes anchored, ≤3 issues, ≤2
  sentences, unique ids), ValidationError + RetryPrompt for the one retry.
- `feature/critique-workflow` is done and pushed: critiques table, parallel
  streaming critics with coalesced deltas, validation retry, per-scene runs,
  partial results, POST /api/chapters/{id}/critiques and
  GET /api/runs/{id}/critiques, fake-gateway canned critique, integration
  tests (workflow incl. failing/flaky writer, scenes; cancel). Verified through
  the container against the fake gateway.
- `feature/critique-ui` is done and pushed: Convene dialog (critic picker,
  default all enabled), Guild panel beside the editor (live streaming cards,
  validated issues, bible conflicts, warnings, raw reply, cancel, totals with
  "est." mark, stale-chapter note), EventSource with Last-Event-ID reconnect,
  auto-reattach to a run in session after reload, issue click highlights the
  quote in the TipTap editor. Verified in the browser against the container
  and fake gateway.

Milestone 2 is complete pending the live check against the real `lumos-chat`
gateway (see Open items). The user reports all five branches merged.

Milestone 3 (stacked on `feature/critique-ui`, since the merges were not yet
visible on origin when work started; rebase onto `develop` with
`git rebase --onto origin/develop origin/feature/critique-ui` once they are):

- `feature/editor-in-chief` is done and pushed: migration 00004 `issues`,
  editor-in-chief synthesis step inside the critique run (labelled sources,
  validation with one retry, fallback to the critics' notes when the editor
  fails), `GET /api/runs/{id}/issues`, editor.* events, Editor-in-chief
  section at the top of the Guild panel with source chips, fake-gateway editor
  reply, unit tests (input assembly, output validation, fallback) and
  integration coverage (merged sources, fallback).
- `feature/issue-decisions` is done and pushed: `PUT /api/issues/{id}/decision`
  (accepted / rejected / pending=undo, optional edited_fix kept across undo,
  cleared with null or empty), Accept / Edit fix / Reject / Undo controls per
  issue in the Guild panel with decision counts and a disabled "Revise"
  button for milestone 4, integration coverage incl. isolation.

Milestone 3 is complete pending merge (order: editor-in-chief, then
issue-decisions) and the live gateway check.

Milestone 4 (stacked on `feature/issue-decisions`; the user reports M2 and M3
merged, origin did not show it yet when work started):

- `feature/word-diff` is done and pushed: `text.Diff` (Myers on word,
  whitespace and punctuation tokens; nearby changes grouped into hunks with
  byte offsets, word-level ops and context), `text.ApplyHunks` (apply any
  subset, rejects a changed base text), `text.Stats`; unit tests incl. round
  trips and a bounded large rewrite.
- `feature/revision-workflow` is done and pushed: migration 00005 `revisions`,
  lead-writer run (per-scene, accepted issues re-anchored by quote, skipped
  ones reported, reply validation with one retry, whitespace preserved),
  word-level hunks stored; POST /api/chapters/{id}/revisions, GET
  /api/revisions/{id}, GET /api/chapters/{id}/revisions, POST .../apply
  (snapshot `pre_revision`, chosen hunks, 409 when stale or decided), POST
  .../discard; Revise button, Lead writer section with streaming and the
  side-by-side review dialog (accept all / selected / discard); fake-gateway
  lead-writer reply; unit tests (validator, locating issues, prompts) and
  integration coverage (two revisions, subset apply, snapshot, stale,
  discard, isolation).
- Next: `feature/bible-keeper`.

## Open items

- **M1 and M2 live gateway checks pending.** `.env` still points
  `LITELLM_BASE_URL` at the fake gateway because no real `LITELLM_BASE_URL` /
  `LITELLM_API_KEY` has been supplied. Once they land: run a writer test (M1)
  and convene the Guild on a chapter (M2) through the container, and check the
  Langfuse trace carries trace_id, session_id, generation_name `critic:<slug>`,
  trace_user_id and the tags.
- **Merge order for M2:** run-engine → text-tooling → structured-output →
  critique-workflow → critique-ui. Each branch contains the previous ones, so
  merging in order is clean; after all five, every other `feature/*` branch
  must be rebased onto `develop` before work starts on it.
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
