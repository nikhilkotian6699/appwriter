# Development structure and flow

This document is the working agreement for building Writers' Guild. The product
brief lives in [BRIEF.md](BRIEF.md); this file says how the code is organised and
in which order it is built.

## 1. Repository layout

```
api/openapi.yaml            single source of truth for the HTTP contract
cmd/writersguild/           the app binary (API, SSE, static SPA, /guide/, migrations)
cmd/fakegateway/            OpenAI-compatible stand-in for the LiteLLM gateway (dev only)
internal/
  config/                   environment parsing, defaults, validation
  db/                       pgx pool, goose runner, migrations/*.sql, queries/*.sql,
                            sqlcgen/ (generated)
  auth/                     bcrypt, password and username rules, cookie sessions, login limiter
  llm/                      Client interface, LiteLLM implementation, mock, cost estimation, backoff
  runs/                     run engine: background execution, event store, SSE broker, cancel
  guild/                    workflows: critique, editor-in-chief, revision, bible-keeper,
                            co-write, compare, test-writer; prompt assembly and the fixed suffix
  text/                     slugs, token estimation, scene splitting, quote anchoring,
                            word-level diff and hunk application
  seed/                     example writers and system agents for a new account
  api/                      chi router, middleware, generated server interface (gen.go),
                            handlers_*.go, static handlers, error mapping
  testutil/                 helpers for integration tests (TEST_DATABASE_URL)
web/                        Vite + React + TypeScript + Tailwind + TipTap
  src/api/                  TypeScript client generated from api/openapi.yaml
  src/pages/ components/    UI
  dist/                     build output, embedded into the Go binary
guide/                      guide sources, screenshots, build script; dist/ is embedded
docs/                       BRIEF.md, DEVELOPMENT.md
Dockerfile, docker-compose.yml, Makefile, .env.example, README.md
```

Layering inside the binary, top to bottom: router and middleware, generated
handlers, domain services (`guild`, `runs`), infrastructure (`llm`, `db`).
A layer only imports the layers below it. `text` and `auth` are pure libraries
with no database access, which is what keeps their unit tests fast.

## 2. Conventions

- **Contract first.** Change `api/openapi.yaml`, then run `make generate`.
  Go handlers and the TypeScript client both come from the spec; a route that
  exists on only one side does not compile.
- **Database.** Schema changes are new goose files under
  `internal/db/migrations` (`NNNNN_name.sql`, Up and Down). Queries live in
  `internal/db/queries/*.sql` and are compiled by sqlc. No hand-written SQL in
  handlers. Every owned table carries `user_id`, and every query filters on it.
- **Current user.** Handlers take the user from the request context, never from
  the URL or body. Until milestone 7 a middleware resolves the single bootstrap
  account; milestone 7 replaces that middleware with cookie authentication.
- **LLM access.** Only through `llm.Client`. Every request carries Langfuse
  metadata and records one `model_calls` row with cost, tokens and whether the
  cost was estimated.
- **Runs.** Every workflow, including a writer test, is a `runs` row. Long
  workflows append `run_events` as they go so the browser can replay them.
- **Secrets.** Only in `.env` (git-ignored). Compose reads them with
  `env_file`. Nothing secret is printed in logs, tests, commits or chat.
- **Errors.** JSON body `{"error":{"code":"...","message":"..."}}`; codes are
  stable strings the frontend can switch on.

## 3. Flow per milestone

Each milestone follows the same loop:

1. **Spec** — add endpoints and schemas to `api/openapi.yaml`; add migrations
   and sqlc queries; `make generate`.
2. **Backend** — services first with unit tests, then handlers, then
   integration tests against `TEST_DATABASE_URL` using the mock gateway.
3. **Frontend** — regenerate the client, build the screens, `npm run build`
   and `tsc` clean.
4. **Local run** — `make dev` (Go binary plus Vite dev server against the fake
   gateway), exercise the feature in the browser, then stop the dev servers.
5. **Container** — `docker compose up -d --build`, verify through the container
   against the real gateway alias (`lumos-chat`); when no key is available yet,
   verify with the `fake` profile and repeat the live check once the key lands.
6. **Commit** — exactly one commit per milestone, message `M<n>: <title>`.
7. **Stop** and hand over for testing.

Definition of done for a milestone: everything in its bullet of the brief works
end to end, `go test ./...` and the integration suite pass, the frontend builds
without type errors, the container runs, README is updated for anything new.

## 4. Milestone map

| # | Scope | New tables | Key packages |
|---|-------|-----------|--------------|
| 1 | Projects, chapters, editor, versions, story bible, writers, aliases, test writer, fake gateway | users, user_settings, writers, projects, chapters, chapter_versions, bible_entries, runs, run_events, model_calls | config, db, llm, auth (bcrypt), text (slug), seed, guild (test), api, web |
| 2 | Critique: run engine, SSE, parallel critics, JSON validation, scenes, backoff | critiques | runs, guild (critique), text (tokens, scenes, quotes) |
| 3 | Editor-in-chief synthesis, issue list, decisions | issues | guild (synthesis) |
| 4 | Revision with diff and hunks, stale check, bible-keeper proposals | revisions, bible_proposals | text (diff), guild (revise, bible) |
| 5 | Co-write and compare | drafts | guild (cowrite) |
| 6 | History, cost, writer stats, settings page | — | api (stats), web |
| 7 | Login, sessions, limiter, admin bootstrap, reset flag, Users page, Account page, isolation test | — | auth, api |
| 8 | Usage per account, roles, passwords, disable, delete, admin invariants | — | api |
| 9 | Guide: content, screenshots, build script, /guide/ | — | guide |

## 5. Local environment

- `docker compose up -d db` starts Postgres 17 on 127.0.0.1:5433 (5432 is taken
  by a local Homebrew Postgres on the development machine).
- `make test-db` creates the `writersguild_test` database used by
  `TEST_DATABASE_URL`.
- `docker compose --profile fake up -d` also starts the fake gateway; point
  `LITELLM_BASE_URL` at `http://fakegateway:4000` in `.env` to use it from the
  app container, or at `http://127.0.0.1:4000` when running the binary locally.
