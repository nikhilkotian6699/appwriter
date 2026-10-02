-- name: CreateBibleProposal :one
INSERT INTO bible_proposals (user_id, run_id, project_id, chapter_id, revision_id, action, entry_id, section, title, fields, rationale, position)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetBibleProposal :one
SELECT * FROM bible_proposals WHERE id = $1 AND user_id = $2;

-- name: GetBibleProposalForUpdate :one
SELECT * FROM bible_proposals WHERE id = $1 AND user_id = $2 FOR UPDATE;

-- name: ListProjectBibleProposals :many
SELECT * FROM bible_proposals
WHERE project_id = $1 AND user_id = $2 AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
ORDER BY created_at DESC, position
LIMIT sqlc.arg(row_limit);

-- name: ListRunBibleProposals :many
SELECT * FROM bible_proposals WHERE run_id = $1 AND user_id = $2 ORDER BY position;

-- name: DecideBibleProposal :one
UPDATE bible_proposals
SET status = $3, title = $4, fields = $5, applied_entry_id = $6, decided_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;
