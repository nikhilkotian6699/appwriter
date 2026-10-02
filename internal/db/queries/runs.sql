-- name: CreateRun :one
INSERT INTO runs (id, user_id, project_id, chapter_id, kind, status, trace_id, params, started_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetRun :one
SELECT * FROM runs WHERE id = $1 AND user_id = $2;

-- name: SetRunStatus :one
UPDATE runs SET status = $2, started_at = coalesce(started_at, now()) WHERE id = $1 RETURNING *;

-- name: FinishRun :one
UPDATE runs SET status = $2, result = $3, error = $4, finished_at = now()
WHERE id = $1
RETURNING *;

-- name: AddRunUsage :exec
UPDATE runs
SET cost_usd = cost_usd + $2,
    cost_estimated = cost_estimated OR $3,
    prompt_tokens = prompt_tokens + $4,
    completion_tokens = completion_tokens + $5
WHERE id = $1;

-- name: FailStaleRuns :execrows
UPDATE runs SET status = 'failed', error = 'interrupted by a server restart', finished_at = now()
WHERE status IN ('queued', 'running');

-- name: AppendRunEvent :one
INSERT INTO run_events (run_id, seq, type, payload) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListRunEvents :many
SELECT * FROM run_events WHERE run_id = $1 AND seq > $2 ORDER BY seq;

-- name: CreateModelCall :one
INSERT INTO model_calls (user_id, run_id, writer_id, generation_name, model_alias, prompt_tokens, completion_tokens,
                         cost_usd, cost_estimated, latency_ms, status, error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: LastRunEventSeq :one
SELECT coalesce(max(seq), 0)::integer FROM run_events WHERE run_id = $1;

-- name: ListChapterRuns :many
SELECT * FROM runs
WHERE chapter_id = $1 AND user_id = $2 AND (sqlc.arg(kind)::text = '' OR kind = sqlc.arg(kind)::text)
ORDER BY created_at DESC
LIMIT sqlc.arg(row_limit);
