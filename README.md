# Writers' Guild

A fiction-writing workspace where a panel of AI co-writers critiques and helps
draft your chapters, an editor-in-chief agent synthesizes their notes, and you
stay in control of every change. Every account is a private workspace.

The product brief is in [docs/BRIEF.md](docs/BRIEF.md); the code layout and the
way it is built are in [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

## Stack

Go (chi, pgx, sqlc, goose) and Postgres on the server; React, TypeScript,
Vite, TipTap and Tailwind in the browser. All model calls go through a LiteLLM
gateway over its OpenAI-compatible API. Langfuse is attached to the gateway,
so the app only sends metadata. One Go binary serves the API, the streams, the
built frontend and the user guide.

## Requirements

- Docker with Compose v2 (Docker Desktop is fine).
- A LiteLLM gateway URL and key, or the built-in fake gateway for trying it out.
- For development only: Go 1.27, Node 22, `sqlc` (`brew install sqlc`).

## Quick start

1. Copy `.env.example` to `.env` and fill it in. `.env` is git-ignored and is
   the only place secrets live.
2. Start everything:

   ```bash
   docker compose up -d --build
   ```

   Postgres listens on `127.0.0.1:5433` only; the app is on
   [http://localhost:8080](http://localhost:8080).
3. Without a real gateway, start the stand-in as well and point
   `LITELLM_BASE_URL=http://fakegateway:4000` in `.env`:

   ```bash
   docker compose --profile fake up -d --build
   ```

The first start creates the first account (an admin) from `APP_USERNAME` and
`APP_PASSWORD` and seeds its writers. After that the password lives in the
database; changing the variable does nothing until you run the reset flag
(see Accounts).

## Environment variables

| Variable | Required | Meaning |
|---|---|---|
| `LITELLM_BASE_URL` | yes | Gateway base URL; the app calls `POST {url}/v1/chat/completions`, `GET {url}/v1/models`, `GET {url}/model/info`. |
| `LITELLM_API_KEY` | usually | Sent as `Authorization: Bearer …`. May be empty for the fake gateway. |
| `APP_NAME` | yes (default `writersguild`) | Prefix of the alias convention and the first Langfuse tag. |
| `DEFAULT_MODEL_ALIAS` | no | When set, every seeded writer of a new account uses this alias, so the app works on a gateway with no per-writer aliases. |
| `APP_USERNAME`, `APP_PASSWORD` | first start | Username (lower-case, 3 to 32 characters) and password (8 or more characters) of the first admin. |
| `POSTGRES_PASSWORD` | yes (compose) | Password of the `writersguild` database role. Compose derives `DATABASE_URL` from it. |
| `DATABASE_URL` | outside compose | Full connection string when running the binary yourself. |
| `PORT` | no (8080) | HTTP port. |
| `LLM_TIMEOUT_SECONDS` | no (120) | Per-request timeout for writer tests and single calls. |
| `RUN_TIMEOUT_SECONDS` | no (900) | Upper bound for one whole background run (a critique with all its writers, a revision, …). |
| `TEST_DATABASE_URL` | tests only | Integration tests run only when set. |

## Model aliases

Every writer stores a gateway alias, never a provider model id. The
convention is `{APP_NAME}-{writer-slug}`: a writer named "García Márquez" gets
the slug `garcia-marquez` and the alias `writersguild-garcia-marquez`. The
alias is pre-filled when you add a writer and can be changed; the form also
offers a dropdown of the gateway's models that start with `{APP_NAME}-`.

The three system agents follow the same rule with fixed slugs:
`editor-in-chief`, `lead-writer` and `bible-keeper`.

With `DEFAULT_MODEL_ALIAS` set (for example `lumos-chat`), seeded writers use
that alias instead, and you can still point individual writers elsewhere.

## Adding a writer

On the gateway, add a model alias for the writer, for example in the LiteLLM
`config.yaml`:

```yaml
model_list:
  - model_name: writersguild-woolf
    litellm_params:
      model: anthropic/claude-sonnet-5-5
      api_key: os.environ/ANTHROPIC_API_KEY
```

In the app, open Writers, click "Add writer", enter the name (the slug and
alias fill in), write the system prompt describing the voice and craft
sensibility to emulate, tick the roles (critic, co-writer), and press "Test
writer" to send a sample request through the gateway before saving. The app
appends a fixed suffix to every prompt: original text only, never reproduce
passages from published work, always follow the required output format.

## Accounts

- The first account comes from `APP_USERNAME` / `APP_PASSWORD` at first start.
- Lost the admin password? Set `APP_PASSWORD` and run the binary once with the
  reset flag, then start normally:

  ```bash
  docker compose run --rm app -reset-admin-password
  ```

- Login, further accounts and the Users page arrive in milestone 7 of the
  brief. Until then the app runs as a single workspace, and every table is
  already scoped by account so that milestone is additive.

## Development

```bash
make help              # list targets
make generate          # OpenAPI -> Go server interface + TS client, sqlc queries
make test              # unit tests and frontend type check
make test-integration  # integration tests against TEST_DATABASE_URL
make test-db           # create writersguild_test on the compose Postgres
make build             # frontend build, then both binaries into bin/
make fake              # run the fake gateway locally on :4000
make dev-api           # run the API locally (reads .env)
make dev-web           # Vite dev server on :5173 with /api proxied to :8080
```

The HTTP contract is `api/openapi.yaml`; both the Go server interface and the
TypeScript client are generated from it. Schema changes are goose migrations
under `internal/db/migrations`, queries are sqlc files under
`internal/db/queries`.

### Runs and event streams

Every workflow is a run (`runs` table) executed in the background by the run
engine (`internal/runs`), so it survives the browser tab that started it. As a
run proceeds it appends numbered events (`run_events`); the browser follows
them at `GET /api/runs/{id}/events` as Server-Sent Events. Each event carries
`id` (the sequence number), `event` (the type) and a JSON `data` payload. A
reconnecting browser sends `Last-Event-ID` (or `?after=`) and receives only
what it missed, first from the database and then live. The stream closes
after `run.finished`; a run that was already over replays and ends with an
`end` event, which tells the browser not to reconnect.

### Convening the Guild (critique)

`POST /api/chapters/{id}/critiques` starts a critique run with the chosen
critics (default: every enabled writer with the critic role) and returns the
run at once. Each critic reads the story bible and the chapter in parallel and
streams its reply; the run emits `critique.plan`, then per writer
`writer.started`, `writer.delta` (coalesced text chunks), `writer.retry` when
a reply failed validation, and `writer.done` or `writer.failed`. Every reply
must be the JSON object described in `guild.CritiqueFormat`; the app
validates it (severity, at most three issues, every quote found in the chapter
text, ids unique) and asks once more on invalid output. Validated critiques,
with byte offsets of every quote into the chapter, are at
`GET /api/runs/{id}/critiques`; one failing writer does not fail the run.

When the critics are done, the editor-in-chief (a system writer of the
account, generation name `editor-in-chief`) receives every validated note
labelled with a source id (`<writer slug>/<issue id>`), the story bible and
the chapter, and returns one prioritized list: duplicates merged,
contradictions resolved in favour of the author's intent, every issue citing
the critics that raised it. The list is validated like a critique (quotes
anchored, every issue cites a known source; one retry) and stored in the
`issues` table, ready for the author's decisions. If the editor-in-chief
fails, the critics' issues are listed unmerged, most severe first, and the
run's result says `synthesis: fallback`. Events: `editor.started`,
`editor.delta`, `editor.retry`, `editor.done`. The list is at
`GET /api/runs/{id}/issues`.

In the app, "Convene the Guild" on a chapter saves pending edits, lets you
pick the critics, and opens the Guild panel beside the editor: each critic
streams into its own card, then shows its overall note, issues with severity,
quote, problem and fix, and story bible conflicts. Clicking a quote highlights
the passage in the editor. The editor-in-chief's list sits at the top of the
panel with each issue's sources; the critics' notes fold away beneath it. The
panel follows a run across reloads and shows the run's tokens and cost
(marked "est." when estimated).

Chapters estimated above the account's scene token limit (Settings) are split
at scene breaks and headings and critiqued scene by scene; issue ids are then
prefixed `s1-`, `s2-`, … and offsets still address the whole chapter.

`POST /api/runs/{id}/cancel` stops a run; `GET /api/chapters/{id}/runs?kind=`
lists a chapter's runs, newest first. Runs left running by a crash are marked
failed at the next start.

The fake gateway (`cmd/fakegateway`) speaks the same API as LiteLLM with
canned replies, usage, a cost header and streaming. Any model id containing
`429` or `500` fails that way, which is handy for checking backoff.

## Running a copy on another machine

Build here, ship the image as built, and restore the database from a dump.

```bash
# on this machine
docker compose build
docker save writersguild:latest | gzip > writersguild-image.tgz
docker compose exec -T db pg_dump -U writersguild -Fc writersguild > writersguild.dump

# a Docker context for the other machine (SSH access to a Docker host)
docker context create other --docker "host=ssh://user@other-host"

# copy docker-compose.yml, a filled-in .env, the image and the dump over, then:
docker --context other load < writersguild-image.tgz
docker --context other compose up -d db
docker --context other compose exec -T db pg_restore -U writersguild -d writersguild --clean --if-exists < writersguild.dump
docker --context other compose up -d --no-build app
```

`--no-build` keeps the copied image instead of rebuilding from source. The
`.env` on the other machine needs its own `POSTGRES_PASSWORD` only if the
database is created fresh there; a restored dump keeps its roles' passwords as
set in the restored cluster, so use the same value.
