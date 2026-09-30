# Writers' Guild — product brief

Build a web app called "Writers' Guild": a fiction-writing workspace where a panel of AI co-writers critiques and helps draft my chapters, an editor-in-chief agent synthesizes their notes, and I stay in control of every change. Several people use it, each in a private workspace of their own.

## Stack
- Backend: Go (chi), Postgres (pgx + sqlc, goose migrations embedded in the binary), REST + Server-Sent Events for streaming.
- Frontend: React + TypeScript (Vite), TipTap for the chapter editor, Tailwind. Chapters are stored as Markdown.
- LLM: all model calls go through a LiteLLM gateway using its OpenAI-compatible API (POST {LITELLM_BASE_URL}/v1/chat/completions, streaming supported; GET /v1/models). Env vars: LITELLM_BASE_URL, LITELLM_API_KEY, APP_NAME. Do NOT call any provider SDK directly; the gateway handles provider routing.
- Model aliases on the gateway follow the convention "{APP_NAME}-{writer-slug}" (e.g. "writersguild-hemingway"). The app never hardcodes model IDs; every agent's model is a gateway alias stored in the database.
- Observability: Langfuse is attached to the LiteLLM gateway as a callback, so the app does not trace directly. Instead, pass Langfuse metadata in every request body's "metadata" field: trace_id (one per run), session_id (the chapter id), generation_name (e.g. "critic:hemingway", "editor-in-chief", "lead-writer", "bible-keeper", "cowrite:hemingway", "test:hemingway"), trace_user_id (the username of the account that started the run), and tags [APP_NAME, project name]. Read per-request cost from the gateway's response cost header (x-litellm-response-cost) and store it. When a call carries no cost (streamed replies usually do not), estimate it from the token usage and the prices in the gateway's GET /model/info, and mark it as estimated wherever it is shown.
- Run with docker-compose (app + Postgres). The gateway and Langfuse are external. Bind the Postgres port to 127.0.0.1 only.

## Writers (user-managed agents)
Every agent is a Writer record, owned by an account:
  { id, user_id, name, slug, model_alias, system_prompt, roles: ["critic" | "co-writer"], enabled, temperature, created_at }
- UI to add, edit, duplicate, enable/disable and delete writers.
- When adding a writer: I enter the name; slug is auto-generated; model_alias is pre-filled as "{APP_NAME}-{slug}" but editable. Also show a dropdown of aliases fetched from the gateway's GET /v1/models, filtered to those starting with "{APP_NAME}-".
- "Test writer" button: sends a short sample request through the gateway using that alias and system prompt, and shows the reply or the error (e.g. alias not found on gateway).
- The system prompt is mine to write, describing the author whose voice and craft sensibility the writer should emulate. The app appends a fixed, non-editable suffix to every writer's system prompt: produce original text only, never reproduce passages from any published work, and always follow the required output format.
- Seed five example writers (I'll edit their prompts): Hemingway, García Márquez, le Carré, Le Guin, Stephen King, with short starter prompts describing each one's craft focus.
- System agents are also Writer records with fixed slugs, editable prompts and aliases: "editor-in-chief", "lead-writer" (applies revisions), and "bible-keeper". Every account gets its own set of system agents and example writers.
- If DEFAULT_MODEL_ALIAS is set, seeded writers point at that alias instead of "{APP_NAME}-{slug}", so a new account works at once on a gateway that has no per-writer aliases.

## Core concepts
- Project: one novel.
- Story Bible: per-project structured doc with sections for premise, characters (name, role, voice, arc, key facts), setting/world rules, timeline, tone and style rules, and chapter summaries. Every agent gets the relevant bible context.
- Chapter: rich text with version history (autosave snapshots at most every ten minutes, a snapshot before a revision or a restore, and snapshots I take by hand with a label).

## Workflow 1: Convene the Guild (critique)
1. I click "Convene the Guild" on a chapter and choose which critic-role writers attend (default: all enabled).
2. Selected writers run IN PARALLEL, each with the chapter plus the story bible. Stream each writer's progress into its own panel.
3. Each writer returns strict JSON (use the gateway's response_format json_object where supported; validate either way and retry once on invalid output):
   { "writer": string, "overall": string (max 2 sentences),
     "issues": [ { "id": string, "severity": "high"|"medium"|"low",
                   "quote": string (exact text from the chapter),
                   "problem": string, "suggested_fix": string } ] (max 3),
     "bible_conflicts": [ { "quote": string, "conflicts_with": string } ] }
4. The editor-in-chief merges duplicates, resolves contradictions, and returns a prioritized list (same issue shape plus "sources": which writers raised it, including those whose issues were merged into it).
5. UI: clicking an issue highlights the quoted text in the editor. For each issue I can Accept, Reject, or Edit the fix, and undo a decision.
6. "Revise": the lead-writer applies ONLY the accepted issues, preserving my voice. Show a side-by-side diff with word-level marks; I accept all, accept per hunk, or discard. A revision proposed for a chapter that changed since cannot be applied any more.
7. After I accept a revision, the bible-keeper proposes story bible updates (new entry, update, delete, each with a rationale). I approve, reject, or edit and approve each one before it is saved. Pending proposals also wait at the top of the story bible page.

## Workflow 2: Co-write
1. I select a passage or write scene notes, pick one co-writer-role writer, and give an instruction (e.g. "draft this scene", "continue from here", "rewrite this dialogue", "tighten this passage", "give me 3 alternative openings").
2. The writer streams its draft in its own voice, with the story bible as context. Without a selection it continues from the cursor, with the text around the cursor as context.
3. I can insert it at the cursor, replace the selection, or discard, and take a discarded draft back. "Compare" runs the same instruction through 2–3 writers in parallel and shows the drafts side by side.

## Runs
- Every workflow is a run: critique (with its synthesis), revision, bible update, co-write, compare, writer test. Runs execute in the background with their steps stored as they happen, so the browser can reconnect and replay a run in progress; a run can be cancelled; runs left running by a crash are marked failed at start.
- History per chapter: every run, critique, draft, decision and revision is stored and browsable, newest first, filterable by kind, with totals of runs, cost and tokens.
- Cost per run and per writer (from the gateway cost header, else estimated).
- Writer stats page: acceptance rate of each writer's issues and drafts, per period and per project; an issue on the editor-in-chief's list counts for every critic it cites; cost per writer and per kind of run.
- Long chapters: above a configurable token limit (Settings page), split into scenes at scene breaks and headings and process per scene.
- Resilience: per-writer timeouts, backoff on 429/5xx from the gateway honouring Retry-After, and partial results shown if one writer fails.

## Accounts
- Every account is a workspace of its own: projects, chapters, story bibles, writers, runs, settings and stats belong to it and nobody else sees them, the admin included. There is no sharing.
- Login with username and password (bcrypt). Usernames are lower-case, 3–32 characters. The session is a signed HttpOnly cookie that stops working when the account is disabled or its password changes. After five wrong passwords for a username from one address, further tries wait a minute, then longer, up to a quarter of an hour; a wrong username is refused exactly like a wrong password.
- The first account is created at the first start from APP_USERNAME and APP_PASSWORD, as an admin; after that the password lives in the database. A `-reset-admin-password` flag sets it again from the environment for a lost password. An installation that had a single user before keeps everything: that user becomes the admin.
- Roles: admin and author. Only an admin adds accounts, on a Users page, with a username, an optional display name, a first password and a role; there is no sign-up form. Every account has an Account page to change its display name and its password (at least 8 characters, current password required; a change logs out everywhere else).
- Managing accounts (Users page, admins only): what every account holds and used (projects, chapters, runs, model calls, tokens, cost) for all time or the last 7, 30 or 90 days, with totals; per account: change the display name and the role, set a new password, disable and enable (disabling logs the account out, cancels its runs, keeps its work), delete a disabled account after typing its username (removes everything it owns). Two rules whatever is clicked or called: nobody changes their own role or access, and one active admin always remains, also when two admins act at the same moment.
- The admin sees accounts and usage, never manuscripts.

## Guide for new users
- The app carries a guide for first-time users, written for people who may never have written a story: plain language, a step for every part of the workflow from logging in to a revised chapter, a screenshot of every screen with lettered marks and a legend, a cast of the writers, a sample chapter to paste, a list of where to click, troubleshooting, and a glossary.
- It is served under /guide/ without login, linked as "Guide" in the header (opens in a new tab) and from the login page. It makes no requests to other servers. Keep its words, pictures and build script in the repo so it can be rebuilt, and let the build also produce it as one HTML file to pass on.

## Quality bar
- Clean package structure; the LLM client is an interface so tests use a mock.
- OpenAPI spec; the Go server interface and the TypeScript client are generated from it.
- Unit tests: JSON validation with quote matching, scene splitting, editor-in-chief input assembly, diff computation and hunk application, alias generation, sessions, password rules, the login limiter, the static file handler.
- Integration tests against a real Postgres (TEST_DATABASE_URL; skipped when unset; each test cleans up after itself): Workflow 1 and Workflow 2 against a mocked gateway, history and stats, logging in and lockouts, first-account setup, managing accounts, and an isolation test that walks every route of the router with a second user against the first user's work and fails for any new route until it is covered.
- A small fake gateway in the repo for local development without a real one.
- README: setup, env vars, the alias convention, how to add a writer (on the gateway and in the app), accounts, and how to run a copy on another machine through a Docker context (images copied as built, database restored from a dump).

## Milestones
1. Projects, chapters, editor, story bible CRUD, Writers CRUD with gateway alias listing and "Test writer".
2. Workflow 1 critique with parallel streaming and Langfuse metadata.
3. Editor-in-chief synthesis and accept/reject UI.
4. Revision with diff and bible-keeper updates.
5. Workflow 2 co-writing and compare mode.
6. History, cost and writer stats.
7. Accounts: a workspace per user, login, the admin adds users.
8. Managing accounts: usage per user, roles, passwords, disable, delete.
9. The guide for new users, inside the app.

## How to work
- Plan first and wait for my approval. Then build milestone by milestone and stop after each one so I can test it. Make one git commit per milestone.
- Use TEST_DATABASE_URL for the integration tests.
- The gateway alias to use for testing is the one I give you (on my gateway: lumos-chat). Verify every milestone against the real model through the container, not only against the mock.
- Stop every test server you started once a check is done; leave only the container running.
- Never write the gateway key or a password into the repo, a commit, or the chat.
