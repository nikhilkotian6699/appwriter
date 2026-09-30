-- +goose Up
CREATE TABLE runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id uuid REFERENCES projects(id) ON DELETE SET NULL,
    chapter_id uuid REFERENCES chapters(id) ON DELETE SET NULL,
    kind text NOT NULL CHECK (kind IN ('critique', 'revision', 'bible_update', 'cowrite', 'compare', 'writer_test')),
    status text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    trace_id text NOT NULL,
    params jsonb NOT NULL DEFAULT '{}'::jsonb,
    result jsonb,
    error text NOT NULL DEFAULT '',
    cost_usd double precision NOT NULL DEFAULT 0,
    cost_estimated boolean NOT NULL DEFAULT false,
    prompt_tokens integer NOT NULL DEFAULT 0,
    completion_tokens integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz
);
CREATE INDEX runs_user_created_idx ON runs (user_id, created_at DESC);
CREATE INDEX runs_chapter_created_idx ON runs (chapter_id, created_at DESC);

CREATE TABLE run_events (
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq integer NOT NULL,
    type text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);

CREATE TABLE model_calls (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    run_id uuid REFERENCES runs(id) ON DELETE CASCADE,
    writer_id uuid REFERENCES writers(id) ON DELETE SET NULL,
    generation_name text NOT NULL,
    model_alias text NOT NULL,
    prompt_tokens integer NOT NULL DEFAULT 0,
    completion_tokens integer NOT NULL DEFAULT 0,
    cost_usd double precision NOT NULL DEFAULT 0,
    cost_estimated boolean NOT NULL DEFAULT false,
    latency_ms integer NOT NULL DEFAULT 0,
    status text NOT NULL CHECK (status IN ('ok', 'error')),
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX model_calls_user_created_idx ON model_calls (user_id, created_at DESC);
CREATE INDEX model_calls_writer_idx ON model_calls (writer_id, created_at DESC);
CREATE INDEX model_calls_run_idx ON model_calls (run_id);

-- +goose Down
DROP TABLE model_calls;
DROP TABLE run_events;
DROP TABLE runs;
