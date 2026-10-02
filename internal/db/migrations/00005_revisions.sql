-- +goose Up
CREATE TABLE revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    chapter_id uuid NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    critique_run_id uuid REFERENCES runs(id) ON DELETE SET NULL,
    status text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed', 'applied', 'discarded')),
    base_hash text NOT NULL,
    base_content_md text NOT NULL,
    revised_md text NOT NULL,
    hunks jsonb NOT NULL DEFAULT '[]'::jsonb,
    stats jsonb NOT NULL DEFAULT '{}'::jsonb,
    issue_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    skipped jsonb NOT NULL DEFAULT '[]'::jsonb,
    applied_hunks jsonb,
    result_hash text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz
);
CREATE INDEX revisions_chapter_idx ON revisions (chapter_id, created_at DESC);
CREATE INDEX revisions_run_idx ON revisions (run_id);

-- +goose Down
DROP TABLE revisions;
