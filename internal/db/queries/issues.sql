-- name: CreateIssue :one
INSERT INTO issues (user_id, run_id, chapter_id, position, key, severity, quote, problem, suggested_fix,
                    quote_start, quote_end, quote_exact, sources, content_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: ListRunIssues :many
SELECT * FROM issues WHERE run_id = $1 AND user_id = $2 ORDER BY position;

-- name: GetIssue :one
SELECT * FROM issues WHERE id = $1 AND user_id = $2;

-- name: DeleteRunIssues :exec
DELETE FROM issues WHERE run_id = $1 AND user_id = $2;
