-- +goose Up
CREATE TABLE critiques (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    chapter_id uuid REFERENCES chapters(id) ON DELETE SET NULL,
    writer_id uuid REFERENCES writers(id) ON DELETE SET NULL,
    writer_name text NOT NULL,
    writer_slug text NOT NULL,
    model_alias text NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    raw_text text NOT NULL DEFAULT '',
    critique jsonb,
    error text NOT NULL DEFAULT '',
    scene_count integer NOT NULL DEFAULT 1,
    prompt_tokens integer NOT NULL DEFAULT 0,
    completion_tokens integer NOT NULL DEFAULT 0,
    cost_usd double precision NOT NULL DEFAULT 0,
    cost_estimated boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE INDEX critiques_run_idx ON critiques (run_id);
CREATE INDEX critiques_chapter_idx ON critiques (chapter_id, created_at DESC);
CREATE INDEX critiques_writer_idx ON critiques (writer_id, created_at DESC);

-- +goose Down
DROP TABLE critiques;
