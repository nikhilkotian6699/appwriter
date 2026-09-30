-- name: ListChapters :many
SELECT id, user_id, project_id, title, position, content_hash, created_at, updated_at,
       length(content_md)::integer AS content_length
FROM chapters
WHERE project_id = $1 AND user_id = $2
ORDER BY position, created_at;

-- name: GetChapter :one
SELECT * FROM chapters WHERE id = $1 AND user_id = $2;

-- name: GetChapterForUpdate :one
SELECT * FROM chapters WHERE id = $1 AND user_id = $2 FOR UPDATE;

-- name: NextChapterPosition :one
SELECT (coalesce(max(position), -1) + 1)::integer FROM chapters WHERE project_id = $1;

-- name: CreateChapter :one
INSERT INTO chapters (user_id, project_id, title, position, content_md, content_hash)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateChapterMeta :one
UPDATE chapters SET title = $3, position = $4, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: UpdateChapterContent :one
UPDATE chapters SET content_md = $3, content_hash = $4, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteChapter :execrows
DELETE FROM chapters WHERE id = $1 AND user_id = $2;
