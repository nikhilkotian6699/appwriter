-- name: CreateRevision :one
INSERT INTO revisions (user_id, run_id, chapter_id, critique_run_id, base_hash, base_content_md, revised_md, hunks, stats, issue_ids, skipped)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetRevision :one
SELECT * FROM revisions WHERE id = $1 AND user_id = $2;

-- name: GetRevisionForUpdate :one
SELECT * FROM revisions WHERE id = $1 AND user_id = $2 FOR UPDATE;

-- name: GetRevisionByRun :one
SELECT * FROM revisions WHERE run_id = $1 AND user_id = $2;

-- name: ListChapterRevisions :many
SELECT * FROM revisions
WHERE chapter_id = $1 AND user_id = $2 AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
ORDER BY created_at DESC
LIMIT sqlc.arg(row_limit);

-- name: ApplyRevision :one
UPDATE revisions SET status = 'applied', applied_hunks = $3, result_hash = $4, decided_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DiscardRevision :one
UPDATE revisions SET status = 'discarded', decided_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;
