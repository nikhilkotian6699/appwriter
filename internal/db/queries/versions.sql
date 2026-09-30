-- name: ListChapterVersions :many
SELECT id, user_id, chapter_id, kind, label, content_hash, created_at,
       length(content_md)::integer AS content_length
FROM chapter_versions
WHERE chapter_id = $1 AND user_id = $2
ORDER BY created_at DESC, id;

-- name: GetChapterVersion :one
SELECT * FROM chapter_versions WHERE id = $1 AND user_id = $2;

-- name: CreateChapterVersion :one
INSERT INTO chapter_versions (user_id, chapter_id, kind, label, content_md, content_hash)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: LatestChapterVersion :one
SELECT * FROM chapter_versions WHERE chapter_id = $1 ORDER BY created_at DESC LIMIT 1;
