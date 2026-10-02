# Build status

Single source of truth for where the Writers' Guild build stands. Update this
file as part of every milestone commit (see `CLAUDE.md`). When this file and
the git history disagree, the git history wins; fix this file.

Last updated: 2026-10-03 (M9 in progress) · `develop` at `6cd68b0` on origin (user reports the five M2 branches merged; not yet visible on origin at the time of writing)

## Milestones

Defined in `docs/BRIEF.md` ("Milestones") and mapped in `docs/DEVELOPMENT.md`
(section 4). One commit per milestone, message `M<n>: <title>`.

| # | Scope | Status | Commit |
|---|-------|--------|--------|
| 1 | Projects, chapters, editor, story bible, writers, test writer, fake gateway | Done | `d9eb22a` |
| 2 | Critique workflow: run engine, SSE, parallel critics, JSON validation, Langfuse metadata | Built on 5 feature branches, awaiting merge | see branches |
| 3 | Editor-in-chief synthesis, issue list, accept/reject | Built on 2 feature branches, awaiting merge | see branches |
| 4 | Revision with diff and hunks, bible-keeper proposals | Built on 3 feature branches, awaiting merge | see branches |
| 5 | Co-writing workflow and compare mode | Built on 2 feature branches, awaiting merge | see branches |
| 6 | Run history, cost and writer stats, settings page | Built on 2 feature branches, awaiting merge | see branches |
| 7 | Accounts: login, sessions, workspace per user, admin adds users, isolation test | Built on 4 feature branches, awaiting merge | see branches |
| 8 | Account management: usage per user, roles, passwords, disable, delete | Built on 2 feature branches, awaiting merge | see branches |
| 9 | In-app guide for new users | In progress (guide-content pushed; guide-screenshots next) | |

## Feature branches

All work happens on a `feature/*` branch; the user merges to `develop` on
GitHub. All 23 branches were created from `develop` at `d9eb22a` on
2026-10-01. A downstream branch must rebase onto `develop` after its upstream
branch is merged.

| Milestone | Branch | Depends on | Status |
|-----------|--------|------------|--------|
| any | `feature/ci` | — | Not started |
| any | `feature/guide-content` | — | Pushed (awaiting merge) |
| 2 | `feature/run-engine` | — | Pushed (awaiting merge) |
| 2 | `feature/text-tooling` | — | Pushed (awaiting merge) |
| 2 | `feature/structured-output` | text-tooling | Pushed (awaiting merge) |
| 2 | `feature/critique-workflow` | run-engine, structured-output | Pushed (awaiting merge) |
| 2 | `feature/critique-ui` | critique-workflow | Pushed (awaiting merge) |
| 3 | `feature/editor-in-chief` | critique-workflow | Pushed (awaiting merge) |
| 3 | `feature/issue-decisions` | editor-in-chief | Pushed (awaiting merge) |
| 4 | `feature/word-diff` | — | Pushed (awaiting merge) |
| 4 | `feature/revision-workflow` | issue-decisions, word-diff | Pushed (awaiting merge) |
| 4 | `feature/bible-keeper` | revision-workflow | Pushed (awaiting merge) |
| 5 | `feature/cowrite` | run-engine | Pushed (awaiting merge) |
| 5 | `feature/compare-mode` | cowrite | Pushed (awaiting merge) |
| 6 | `feature/run-history` | — | Pushed (awaiting merge) |
| 6 | `feature/writer-stats` | — | Pushed (awaiting merge) |
| 7 | `feature/auth-lib` | — | Pushed (awaiting merge) |
| 7 | `feature/login` | auth-lib | Pushed (awaiting merge) |
| 7 | `feature/users-and-account-pages` | login | Pushed (awaiting merge) |
| 7 | `feature/isolation-test` | login | Pushed (awaiting merge) |
| 8 | `feature/account-usage` | users-and-account-pages | Pushed (awaiting merge) |
| 8 | `feature/account-management` | users-and-account-pages | Pushed (awaiting merge) |
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
- `feature/bible-keeper` is done and pushed: migration 00006
  `bible_proposals`, bible keeper run after an applied revision (bible with
  entry ids, revised chapter, applied changes; validation with one retry;
  updates merged over the entry), POST /api/chapters/{id}/bible-updates, GET
  /api/projects/{id}/bible/proposals, GET /api/runs/{id}/bible-proposals, PUT
  /api/bible-proposals/{id} (approve, edit and approve, reject; applies to the
  bible in one transaction), Bible keeper section in the Guild panel and the
  pending list at the top of the bible page, fake-gateway keeper reply, unit
  and integration coverage.

Milestone 4 is complete pending merge (order: word-diff, revision-workflow,
bible-keeper) and the live gateway check.

Milestone 5 (stacked on `feature/bible-keeper`; the user reports M2–M4
merged, origin did not show it yet when work started):

- `feature/cowrite` is done and pushed: migration 00007 `drafts`,
  guild.Cowrite (one writer co-writes, two or three compare, in parallel;
  selection or continue-from-cursor mode; reply cleaned with one retry),
  POST /api/chapters/{id}/drafts, GET /api/runs/{id}/drafts, GET
  /api/chapters/{id}/drafts, PUT /api/drafts/{id}/decision (inserted,
  replaced, discarded, pending to take back), Co-write dialog with presets
  and scene notes, Co-writer section streaming the draft with Insert at
  cursor / Replace selection / Discard / Take back (Markdown inserted through
  the editor), reattach to pending drafts on load; unit and integration tests.
- `feature/compare-mode` is done and pushed: the Co-write dialog takes one
  writer to co-write or two or three to compare, drafts sit side by side in
  the Guild panel with independent decisions, the fake gateway varies its
  drafts by writer, integration coverage for a compare run with one failing
  writer.

Milestone 5 is complete pending merge (order: cowrite, compare-mode) and the
live gateway check.

Milestone 6 (stacked on `feature/compare-mode`; origin still showed no merges
when work started):

- `feature/run-history` is done and pushed: GET /api/chapters/{id}/history
  (runs newest first with per-kind summaries and counts, totals, kind
  counts, `before` paging), GET /api/runs/{id}/calls (gateway calls with cost
  per writer), History page at /chapters/{id}/history with totals, kind
  filter chips, expandable call tables and "Load older runs"; the chapter's
  snapshot panel is now called Versions; integration coverage in the critique
  and co-write tests.
- `feature/writer-stats` is done and pushed: GET /api/stats/writers (issues
  cited per writer with decisions and acceptance rate, drafts and their use,
  cost/tokens/calls per kind; period 7d/30d/90d/all; project filter), Stats
  page with period, project and sort controls and a nav link; unit and
  integration coverage. The Settings page (scene token limit, autosave
  interval) exists since M1.

Milestone 6 is complete pending merge (order: run-history, writer-stats) and
the live gateway check.

Milestone 7 (stacked on `feature/writer-stats`; origin still showed no merges
when work started):

- `feature/auth-lib` is done and pushed: `auth.Signer` (HMAC-SHA256 signed
  session tokens carrying user id, auth version and issue time; max age;
  tamper and future-dated tokens rejected; secret of at least 32 characters)
  and `auth.Limiter` (five free wrong passwords per username and address,
  then 1, 2, 4, 8 and 15 minutes; reset on success; idle buckets swept);
  unit tests for both. Passwords (bcrypt, rules, dummy hash) were already in
  the package since M1.
- `feature/login` is done and pushed: cookie sessions replace the
  single-account middleware (`wg_session`, HttpOnly, SameSite=Lax, Secure
  over HTTPS; auth version checked on every request), POST /api/auth/login
  (same refusal for wrong username and password, limiter with Retry-After,
  disabled accounts refused), POST /api/auth/logout, SESSION_SECRET /
  SESSION_MAX_AGE_DAYS / COOKIE_SECURE (random one-process secret with a
  warning when unset), bootstrap promotes a lone pre-existing user to admin,
  login page with lockout message and Guide link, Sign out button, any 401
  returns the app to the login page; integration tests for sessions and
  lockouts.
- `feature/users-and-account-pages` is done and pushed: GET/POST /api/users
  (admins only; username rules, first password, role, 409 on a taken name;
  new accounts seeded), PUT /api/account (display name), PUT
  /api/account/password (current password required, 8+ characters, auth
  version bumped so other sessions end, fresh cookie for this browser);
  Users page with the account list and an add form (admins; nav link only
  for admins), Account page (display name, password change), the header
  name links to it; integration coverage.
- `feature/isolation-test` is done and pushed: TestIntegrationIsolation walks
  every router route as a second account against the first account's
  fixtures (one of everything), with a probe per route (404 / 403 / public /
  own-data-only) and fails for any route without one or any probe naming a
  vanished route.

Milestone 7 is complete pending merge (order: auth-lib, login,
users-and-account-pages, isolation-test) and the live gateway check. Note:
the dev database now has a throwaway admin account `tester` (created
2026-10-03 for browser checks); delete it on the Users page once M8 adds
deletion, or leave it. Milestone 8 (stacked on `feature/isolation-test`; origin still showed none of
the stack merged when work started):

- `feature/account-usage` is done and pushed: GET /api/users/usage (admins;
  projects and chapters held now, runs, calls, tokens and cost over
  7d/30d/90d/all with totals), `disabled_at` on the User schema, the Users
  page shows the usage table with a period selector and totals; isolation
  probe and integration coverage.
- `feature/account-management` is done and pushed: PUT /api/users/{id}
  (display name, role), PUT /api/users/{id}/password, POST .../disable
  (signs out, cancels runs, keeps work), POST .../enable, DELETE
  /api/users/{id} (disabled accounts only, username typed); nobody changes
  their own role or access, one active admin always remains, all inside a
  transaction holding an advisory lock (the concurrent-demotion test proves
  exactly one of two simultaneous demotions goes through); Users page row
  actions with dialogs; isolation probes and integration coverage.

Milestone 8 is complete pending merge (order: account-usage,
account-management) and the live gateway check. Milestone 9 (stacked on `feature/account-management`; origin still showed
none of the stack merged when work started):

- `feature/guide-content` is done and pushed: 18 Markdown chapters in
  `guide/src` (welcome, signing in, first project, story bible, writing,
  the writers and their cast, convening, the editor's list, revising, bible
  keeper, co-writing and compare, history and stats, account, admins, a
  sample chapter, where to click, troubleshooting, glossary), `marks.json`
  with every figure's caption and lettered legend, and `guide/build` (Go):
  Markdown to HTML with goldmark, figures with legends and amber lettered
  marks drawn on the screenshots, table of contents, inline styles only, a
  check that nothing is fetched from other servers, and a single-file copy
  with images inlined. `make guide` / Docker build it; `guide/dist` is
  ignored by git. Unit tests incl. an end-to-end build and anchor check.
- Next: `feature/guide-screenshots` — capture every screen, add the pixel
  positions of the marks, rebuild.

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
