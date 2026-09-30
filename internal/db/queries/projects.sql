-- name: ListProjects :many
SELECT p.*, (SELECT count(*) FROM chapters c WHERE c.project_id = p.id)::integer AS chapter_count
FROM projects p
WHERE p.user_id = $1
ORDER BY p.updated_at DESC;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1 AND user_id = $2;

-- name: CreateProject :one
INSERT INTO projects (user_id, name, description) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateProject :one
UPDATE projects SET name = $3, description = $4, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: TouchProject :exec
UPDATE projects SET updated_at = now() WHERE id = $1;

-- name: DeleteProject :execrows
DELETE FROM projects WHERE id = $1 AND user_id = $2;
