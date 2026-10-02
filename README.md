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
| `SESSION_SECRET` | recommended | Signs the login cookie; 32 or more random characters. Without it a one-process secret is made at start and every restart signs everyone out. |
| `SESSION_MAX_AGE_DAYS` | no (30) | How long a login lasts. |
| `COOKIE_SECURE` | no (auto) | Force the Secure flag on the cookie; it is set automatically over HTTPS (also behind a proxy sending `X-Forwarded-Proto`). |
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

Every account is a workspace of its own: projects, chapters, story bibles,
writers, runs, settings and stats belong to it and nobody else sees them, the
admin included. There is no sharing.

Sign in with username and password (`POST /api/auth/login`). The session is a
signed, HttpOnly cookie (`wg_session`) that stops working when the account is
disabled or its password changes, because it carries the account's auth
version and the server compares it on every request. A wrong username is
refused exactly like a wrong password. After five wrong passwords for a
username from one address, further tries wait a minute, then twice as long
each time, up to a quarter of an hour (429 with `Retry-After`).

The first account is created at the first start from `APP_USERNAME` and
`APP_PASSWORD`, as an admin; after that the password lives in the database.
`writersguild -reset-admin-password` sets it again from the environment for a
lost password (and signs that account out everywhere). An installation that
had a single user before accounts arrived keeps everything: that user is the
admin. Roles are admin and author; only an admin adds accounts, on the Users
page (`GET`/`POST /api/users`), with a username, an optional display name, a
first password and a role; there is no sign-up form. A new account starts
with its own settings, the example writers and the system agents. Every
account has an Account page (`PUT /api/account`, `PUT /api/account/password`)
to change its display name and its password: at least 8 characters, current
password required, and a change signs the account out everywhere else while
the browser that made it keeps a fresh session.

Managing accounts (Users page, admins only): `GET /api/users/usage` shows
what every account holds (projects, chapters) and used over the last 7, 30
or 90 days or all time (runs, gateway calls, tokens, cost) with totals. Per
account the admin can change the display name and the role
(`PUT /api/users/{id}`), set a new password (`PUT /api/users/{id}/password`,
which signs the account out everywhere), disable and enable it
(`POST .../disable`, `POST .../enable`: disabling signs it out, cancels its
runs and keeps its work), and delete a disabled account after typing its
username (`DELETE /api/users/{id}`, which removes everything it owns). Two
rules hold whatever is clicked or called: nobody changes their own role or
access, and one active admin always remains; the checks run inside a
transaction holding an advisory lock, so two admins acting at the same moment
cannot both get through. The admin sees accounts and usage, never
manuscripts.

### Isolation test

`TestIntegrationIsolation` (in `internal/api`) walks every route of the
router and calls each one as a second account against the first account's
project, chapter, snapshot, bible entry, writer, run, critique, issue,
revision, draft and proposal. Each route needs a probe saying what must come
back (404 for another account's things, 403 for admin-only routes, or a reply
that mentions none of the first account's identifiers); a new route without a
probe fails the test, so no endpoint can ship without saying how it keeps
accounts apart.

## The guide

The guide for first-time users lives in `guide/src`: numbered Markdown
chapters, `marks.json` (the caption and lettered legend of every screenshot,
with the marks' pixel positions) and `img/` (the screenshots). `make guide`
(or `go run ./guide/build`) turns them into `guide/dist/index.html` plus the
annotated images, served under `/guide/` without login and linked as "Guide"
from the header and the login page, and into `guide/dist/writers-guild-guide.html`,
one self-contained file with the images inlined, to pass on. The build refuses
a page that would request another server, and a figure whose screenshot is
missing renders as a labelled placeholder so the words still read. The Docker
image builds the guide itself; locally run `make guide` once before
`make dev-api` if you want `/guide/` to serve.

The screenshots in `guide/src/img` were taken in a browser at phone width
(592 CSS pixels, 800 pixels wide in the image) on a workspace holding the
guide's own sample chapter, so they stay readable in a narrow column. To
recapture one, sign in to a test account, take the screenshot at the same
width, save it as `guide/src/img/<name>.png`, and update the marks' `x` and
`y` in `marks.json` (pixel positions in the image; a mark without a position
appears in the legend only). Then `make guide`.

## Development

```bash
make help              # list targets
make generate          # OpenAPI -> Go server interface + TS client, sqlc queries
make test              # unit tests and frontend type check
make test-integration  # integration tests against TEST_DATABASE_URL
make test-db           # create writersguild_test on the compose Postgres
make guide             # build the user guide into guide/dist
make build             # frontend build, guide, then both binaries into bin/
make fake              # run the fake gateway locally on :4000
make dev-api           # run the API locally (reads .env)
make dev-web           # Vite dev server on :5173 with /api proxied to :8080
```

The desktop app's Preview (`.claude/launch.json`) starts one thing: the Vite
dev server (`web`, normally :5173, or a free port the app hands it through
`PORT`) with `/api` and `/guide` proxied to the container on :8080. The API
and the fake gateway are not in that file because the compose containers
already hold :8080 and :4000; open http://127.0.0.1:8080 directly for the
built app, and http://127.0.0.1:4000/v1/models for the fake gateway. To run
either outside Docker, stop the matching container first (`docker compose
stop app` or `docker compose stop fakegateway`) and use `make dev-api` or
`make fake` from a terminal.

The HTTP contract is `api/openapi.yaml`; both the Go server interface and the
TypeScript client are generated from it. Schema changes are goose migrations
under `internal/db/migrations`, queries are sqlc files under
`internal/db/queries`.

### Revising with the lead writer

"Revise" on a critique's issue list starts a revision run
(`POST /api/chapters/{id}/revisions` with the critique run id). The lead
writer (system agent `lead-writer`) receives the chapter, the story bible and
the accepted issues with their final wording (the author's edit when there is
one), scene by scene above the scene token limit, and returns the revised
text; scenes without accepted issues are never sent. Accepted issues whose
passage is no longer in the chapter are skipped and reported. The reply is
checked (not empty, not a summary or an expansion) with one retry, then
diffed against the chapter into word-level hunks (`internal/text/diff.go`)
and stored as a proposed revision (`revisions` table).

`GET /api/revisions/{id}` returns the hunks with word-level ops and whether
the revision is stale (the chapter changed since). `POST .../apply` takes a
"before revision" snapshot and applies all hunks or the chosen ones;
`POST .../discard` drops it. A stale or already decided revision cannot be
applied. In the app the review dialog shows every change before and after,
side by side, with a checkbox per change: accept all, accept the selected
ones, or discard.

### Keeping the story bible (bible keeper)

Applying a revision starts a bible update run (`bible_update`; also
`POST /api/chapters/{id}/bible-updates` by hand). The bible keeper (system
agent `bible-keeper`) receives the story bible with each entry's id, the
chapter as it now stands and the changes the revision made, and returns
proposals: add an entry, update one (the fields as they should read), or
delete one, each with a one-sentence rationale. Proposals are validated
(action, section, rationale; unknown entry ids are dropped with a note) and
stored in `bible_proposals`; nothing touches the bible until the author
decides. `PUT /api/bible-proposals/{id}` with `approved` applies the
proposal (optionally with an edited title and fields), `rejected` drops it.
Pending proposals are listed at `GET /api/projects/{id}/bible/proposals`
and shown at the top of the story bible page; the Guild panel shows the
keeper's proposals right after the revision. Events: `bible.started`,
`bible.delta`, `bible.retry`, `bible.done`.

### Co-writing (Workflow 2)

"Co-write" on a chapter asks one writer with the co-writer role for a draft
(`POST /api/chapters/{id}/drafts`). With a selection in the editor the writer
proposes text to take the passage's place; without one it continues from the
cursor, reading the text just before and after it. The instruction is free
text (presets: draft this scene, continue from here, rewrite this dialogue,
tighten this passage, give me 3 alternative openings), scene notes are
optional, and the story bible and the chapter go along. The draft streams
into the Guild panel in the writer's voice (`draft.started`, `draft.delta`,
`draft.retry`, `draft.done` / `draft.failed`); the author inserts it at the
cursor, replaces the selection, or discards it, and can take a discarded
draft back. Drafts and decisions are stored (`drafts` table,
`PUT /api/drafts/{id}/decision`), which the writer stats use later.

**Compare** is the same request sent to two or three co-writers at once
(`writer_ids` with two or three ids; run kind `compare`). The drafts stream
in parallel and sit side by side in the Guild panel, each with its own
Insert, Replace, Discard and Take back; one writer failing does not spoil
the others.

### History and cost

`GET /api/chapters/{id}/history` lists every run of a chapter, newest first,
each with a one-line summary and the counts behind it (critics and issues
with their decisions, revision changes and how many were applied, drafts and
what became of them, story bible proposals and their fate), plus totals of
runs, cost and tokens over every run matching the `kind` filter and per-kind
counts for the filter chips; page with `before`. `GET /api/runs/{id}/calls`
lists the gateway calls a run made with tokens, cost and latency per writer,
so cost per run breaks down per writer. Cost comes from the gateway's
`x-litellm-response-cost` header; streamed replies carry none, so their cost
is estimated from token counts and `GET /model/info` prices and shown with
"est." wherever it appears. In the app, "History" on a chapter opens the page
(the snapshot panel is "Versions").

### Writer stats

`GET /api/stats/writers?period=7d|30d|90d|all&project_id=` gives, for every
writer of the account, the issues on the editor-in-chief's list that cite the
writer (an issue counts for every critic who raised it) with the author's
decisions and acceptance rate, the drafts the writer finished and how many
went into the chapter, and cost, tokens and calls per kind of run. The Stats
page (nav "Stats") shows it with period, project and sort controls; writers
with nothing in the period fold away.

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

Each issue carries the author's decision: `PUT /api/issues/{id}/decision`
with `accepted`, `rejected`, or `pending` to undo; `accepted` with
`edited_fix` is "edit the fix", and the author's wording is what the revision
(milestone 4) will apply. In the Guild panel every issue has Accept, Edit fix
and Reject buttons, decided issues show Undo, and the header counts
accepted, rejected and pending.

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
