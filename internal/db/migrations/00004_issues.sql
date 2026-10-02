-- +goose Up
CREATE TABLE issues (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    chapter_id uuid REFERENCES chapters(id) ON DELETE SET NULL,
    position integer NOT NULL,
    key text NOT NULL,
    severity text NOT NULL CHECK (severity IN ('high', 'medium', 'low')),
    quote text NOT NULL,
    problem text NOT NULL,
    suggested_fix text NOT NULL DEFAULT '',
    quote_start integer NOT NULL DEFAULT 0,
    quote_end integer NOT NULL DEFAULT 0,
    quote_exact boolean NOT NULL DEFAULT true,
    sources jsonb NOT NULL DEFAULT '[]'::jsonb,
    decision text NOT NULL DEFAULT 'pending' CHECK (decision IN ('pending', 'accepted', 'rejected')),
    edited_fix text,
    decided_at timestamptz,
    content_hash text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX issues_run_idx ON issues (run_id, position);
CREATE INDEX issues_chapter_idx ON issues (chapter_id, created_at DESC);

-- +goose Down
DROP TABLE issues;
