-- name: CreateCritique :one
INSERT INTO critiques (user_id, run_id, chapter_id, writer_id, writer_name, writer_slug, model_alias, status, scene_count)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'running', $8)
RETURNING *;

-- name: FinishCritique :one
UPDATE critiques
SET status = $2, raw_text = $3, critique = $4, error = $5,
    prompt_tokens = $6, completion_tokens = $7, cost_usd = $8, cost_estimated = $9,
    finished_at = now()
WHERE id = $1
RETURNING *;

-- name: ListRunCritiques :many
SELECT * FROM critiques WHERE run_id = $1 AND user_id = $2 ORDER BY created_at, writer_name;

-- name: GetCritique :one
SELECT * FROM critiques WHERE id = $1 AND user_id = $2;
