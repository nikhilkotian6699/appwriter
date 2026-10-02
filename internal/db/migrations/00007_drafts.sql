-- +goose Up
CREATE TABLE drafts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    chapter_id uuid REFERENCES chapters(id) ON DELETE SET NULL,
    writer_id uuid REFERENCES writers(id) ON DELETE SET NULL,
    writer_name text NOT NULL,
    writer_slug text NOT NULL,
    model_alias text NOT NULL,
    mode text NOT NULL CHECK (mode IN ('selection', 'continue')),
    instruction text NOT NULL,
    selection text NOT NULL DEFAULT '',
    notes text NOT NULL DEFAULT '',
    context_before text NOT NULL DEFAULT '',
    context_after text NOT NULL DEFAULT '',
    content_hash text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    text text NOT NULL DEFAULT '',
    error text NOT NULL DEFAULT '',
    decision text NOT NULL DEFAULT 'pending' CHECK (decision IN ('pending', 'inserted', 'replaced', 'discarded')),
    decided_at timestamptz,
    position integer NOT NULL DEFAULT 0,
    prompt_tokens integer NOT NULL DEFAULT 0,
    completion_tokens integer NOT NULL DEFAULT 0,
    cost_usd double precision NOT NULL DEFAULT 0,
    cost_estimated boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE INDEX drafts_run_idx ON drafts (run_id, position);
CREATE INDEX drafts_chapter_idx ON drafts (chapter_id, created_at DESC);
CREATE INDEX drafts_writer_idx ON drafts (writer_id, created_at DESC);

-- +goose Down
DROP TABLE drafts;
