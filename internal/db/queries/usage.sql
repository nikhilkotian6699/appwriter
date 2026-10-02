-- name: UsageHoldingsByUser :many
SELECT u.id AS user_id,
       (SELECT count(*) FROM projects p WHERE p.user_id = u.id)::integer AS projects,
       (SELECT count(*) FROM chapters c WHERE c.user_id = u.id)::integer AS chapters
FROM users u;

-- name: UsageRunsByUser :many
SELECT user_id, count(*)::integer AS runs, max(created_at)::timestamptz AS last_run_at
FROM runs
WHERE (sqlc.narg(since)::timestamptz IS NULL OR created_at >= sqlc.narg(since)::timestamptz)
GROUP BY user_id;

-- name: UsageCallsByUser :many
SELECT user_id, count(*)::integer AS calls,
       coalesce(sum(prompt_tokens), 0)::bigint AS prompt_tokens,
       coalesce(sum(completion_tokens), 0)::bigint AS completion_tokens,
       coalesce(sum(cost_usd), 0)::double precision AS cost_usd,
       coalesce(bool_or(cost_estimated), false)::boolean AS cost_estimated
FROM model_calls
WHERE (sqlc.narg(since)::timestamptz IS NULL OR created_at >= sqlc.narg(since)::timestamptz)
GROUP BY user_id;
