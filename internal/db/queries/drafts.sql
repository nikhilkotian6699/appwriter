-- name: CreateDraft :one
INSERT INTO drafts (user_id, run_id, chapter_id, writer_id, writer_name, writer_slug, model_alias, mode, instruction, selection, notes,
                    context_before, context_after, content_hash, status, position)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 'running', $15)
RETURNING *;

-- name: FinishDraft :one
UPDATE drafts
SET status = $2, text = $3, error = $4, prompt_tokens = $5, completion_tokens = $6, cost_usd = $7, cost_estimated = $8, finished_at = now()
WHERE id = $1
RETURNING *;

-- name: GetDraft :one
SELECT * FROM drafts WHERE id = $1 AND user_id = $2;

-- name: ListRunDrafts :many
SELECT * FROM drafts WHERE run_id = $1 AND user_id = $2 ORDER BY position;

-- name: ListChapterDrafts :many
SELECT * FROM drafts WHERE chapter_id = $1 AND user_id = $2 ORDER BY created_at DESC, position LIMIT sqlc.arg(row_limit);

-- name: SetDraftDecision :one
UPDATE drafts SET decision = $3, decided_at = CASE WHEN $3 = 'pending' THEN NULL ELSE now() END
WHERE id = $1 AND user_id = $2
RETURNING *;
