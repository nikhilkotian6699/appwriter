-- name: ListBibleEntries :many
SELECT * FROM bible_entries
WHERE project_id = $1 AND user_id = $2
ORDER BY section, position, created_at;

-- name: GetBibleEntry :one
SELECT * FROM bible_entries WHERE id = $1 AND user_id = $2;

-- name: NextBibleEntryPosition :one
SELECT (coalesce(max(position), -1) + 1)::integer FROM bible_entries WHERE project_id = $1 AND section = $2;

-- name: CreateBibleEntry :one
INSERT INTO bible_entries (user_id, project_id, section, title, fields, chapter_id, position)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateBibleEntry :one
UPDATE bible_entries SET title = $3, fields = $4, chapter_id = $5, position = $6, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteBibleEntry :execrows
DELETE FROM bible_entries WHERE id = $1 AND user_id = $2;
