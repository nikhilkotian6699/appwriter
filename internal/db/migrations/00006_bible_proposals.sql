-- +goose Up
CREATE TABLE bible_proposals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    chapter_id uuid REFERENCES chapters(id) ON DELETE SET NULL,
    revision_id uuid REFERENCES revisions(id) ON DELETE SET NULL,
    action text NOT NULL CHECK (action IN ('add', 'update', 'delete')),
    entry_id uuid REFERENCES bible_entries(id) ON DELETE SET NULL,
    section text NOT NULL CHECK (section IN ('premise', 'character', 'setting', 'timeline', 'style', 'chapter_summary')),
    title text NOT NULL DEFAULT '',
    fields jsonb NOT NULL DEFAULT '{}'::jsonb,
    rationale text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    applied_entry_id uuid,
    position integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz
);
CREATE INDEX bible_proposals_project_idx ON bible_proposals (project_id, status, created_at DESC);
CREATE INDEX bible_proposals_run_idx ON bible_proposals (run_id, position);

-- +goose Down
DROP TABLE bible_proposals;
