-- name: ListChapterHistory :many
SELECT * FROM runs
WHERE chapter_id = $1 AND user_id = $2
  AND (sqlc.arg(kind)::text = '' OR kind = sqlc.arg(kind)::text)
  AND (sqlc.narg(before)::timestamptz IS NULL OR created_at < sqlc.narg(before)::timestamptz)
ORDER BY created_at DESC, id
LIMIT sqlc.arg(row_limit);

-- name: ChapterRunTotals :one
SELECT count(*)::integer AS runs,
       coalesce(sum(cost_usd), 0)::double precision AS cost_usd,
       coalesce(bool_or(cost_estimated), false)::boolean AS cost_estimated,
       coalesce(sum(prompt_tokens), 0)::bigint AS prompt_tokens,
       coalesce(sum(completion_tokens), 0)::bigint AS completion_tokens
FROM runs
WHERE chapter_id = $1 AND user_id = $2 AND (sqlc.arg(kind)::text = '' OR kind = sqlc.arg(kind)::text);

-- name: ChapterRunKindCounts :many
SELECT kind, count(*)::integer AS count FROM runs WHERE chapter_id = $1 AND user_id = $2 GROUP BY kind ORDER BY kind;

-- name: CritiqueCountsByRun :many
SELECT run_id, count(*)::integer AS critics, count(*) FILTER (WHERE status = 'failed')::integer AS failed
FROM critiques WHERE run_id = ANY(sqlc.arg(run_ids)::uuid[]) GROUP BY run_id;

-- name: IssueCountsByRun :many
SELECT run_id, count(*)::integer AS issues,
       count(*) FILTER (WHERE decision = 'accepted')::integer AS accepted,
       count(*) FILTER (WHERE decision = 'rejected')::integer AS rejected,
       count(*) FILTER (WHERE decision = 'pending')::integer AS pending
FROM issues WHERE run_id = ANY(sqlc.arg(run_ids)::uuid[]) GROUP BY run_id;

-- name: RevisionsByRun :many
SELECT run_id, status, jsonb_array_length(hunks)::integer AS hunks,
       coalesce(jsonb_array_length(applied_hunks), 0)::integer AS applied_hunks, stats
FROM revisions WHERE run_id = ANY(sqlc.arg(run_ids)::uuid[]);

-- name: DraftCountsByRun :many
SELECT run_id, count(*)::integer AS count,
       count(*) FILTER (WHERE decision = 'inserted')::integer AS inserted,
       count(*) FILTER (WHERE decision = 'replaced')::integer AS replaced,
       count(*) FILTER (WHERE decision = 'discarded')::integer AS discarded,
       count(*) FILTER (WHERE decision = 'pending' AND status = 'succeeded')::integer AS pending
FROM drafts WHERE run_id = ANY(sqlc.arg(run_ids)::uuid[]) GROUP BY run_id;

-- name: ProposalCountsByRun :many
SELECT run_id, count(*)::integer AS proposals,
       count(*) FILTER (WHERE status = 'approved')::integer AS approved,
       count(*) FILTER (WHERE status = 'rejected')::integer AS rejected,
       count(*) FILTER (WHERE status = 'pending')::integer AS pending
FROM bible_proposals WHERE run_id = ANY(sqlc.arg(run_ids)::uuid[]) GROUP BY run_id;

-- name: WritersByRun :many
SELECT mc.run_id, count(*)::integer AS calls, array_remove(array_agg(DISTINCT w.name), NULL)::text[] AS writer_names
FROM model_calls mc LEFT JOIN writers w ON w.id = mc.writer_id
WHERE mc.run_id = ANY(sqlc.arg(run_ids)::uuid[]) GROUP BY mc.run_id;

-- name: ListRunModelCalls :many
SELECT mc.*, w.name AS writer_name
FROM model_calls mc LEFT JOIN writers w ON w.id = mc.writer_id
WHERE mc.run_id = $1 AND mc.user_id = $2
ORDER BY mc.created_at, mc.id;
