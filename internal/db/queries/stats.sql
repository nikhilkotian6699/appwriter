-- name: StatsIssuesByWriter :many
SELECT (src->>'writer_id')::uuid AS writer_id, i.decision, count(*)::integer AS count
FROM issues i
JOIN runs r ON r.id = i.run_id
CROSS JOIN LATERAL jsonb_array_elements(i.sources) AS src
WHERE i.user_id = $1
  AND (sqlc.narg(since)::timestamptz IS NULL OR i.created_at >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(project_id)::uuid IS NULL OR r.project_id = sqlc.narg(project_id)::uuid)
GROUP BY 1, 2;

-- name: StatsCritiquesByWriter :many
SELECT c.writer_id, count(*)::integer AS count
FROM critiques c
JOIN runs r ON r.id = c.run_id
WHERE c.user_id = $1 AND c.status = 'succeeded' AND c.writer_id IS NOT NULL
  AND (sqlc.narg(since)::timestamptz IS NULL OR c.created_at >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(project_id)::uuid IS NULL OR r.project_id = sqlc.narg(project_id)::uuid)
GROUP BY c.writer_id;

-- name: StatsDraftsByWriter :many
SELECT d.writer_id, d.decision, count(*)::integer AS count
FROM drafts d
JOIN runs r ON r.id = d.run_id
WHERE d.user_id = $1 AND d.status = 'succeeded' AND d.writer_id IS NOT NULL
  AND (sqlc.narg(since)::timestamptz IS NULL OR d.created_at >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(project_id)::uuid IS NULL OR r.project_id = sqlc.narg(project_id)::uuid)
GROUP BY 1, 2;

-- name: StatsCostByWriterKind :many
SELECT mc.writer_id, coalesce(r.kind, 'other')::text AS kind, count(*)::integer AS calls,
       coalesce(sum(mc.cost_usd), 0)::double precision AS cost_usd,
       coalesce(bool_or(mc.cost_estimated), false)::boolean AS cost_estimated,
       coalesce(sum(mc.prompt_tokens), 0)::bigint AS prompt_tokens,
       coalesce(sum(mc.completion_tokens), 0)::bigint AS completion_tokens
FROM model_calls mc
LEFT JOIN runs r ON r.id = mc.run_id
WHERE mc.user_id = $1
  AND (sqlc.narg(since)::timestamptz IS NULL OR mc.created_at >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(project_id)::uuid IS NULL OR r.project_id = sqlc.narg(project_id)::uuid)
GROUP BY 1, 2;
