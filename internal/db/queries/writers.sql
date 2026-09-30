-- name: ListWriters :many
SELECT * FROM writers WHERE user_id = $1 ORDER BY is_system, lower(name);

-- name: GetWriter :one
SELECT * FROM writers WHERE id = $1 AND user_id = $2;

-- name: GetWriterBySlug :one
SELECT * FROM writers WHERE user_id = $1 AND slug = $2;

-- name: ListWriterSlugs :many
SELECT slug FROM writers WHERE user_id = $1;

-- name: CreateWriter :one
INSERT INTO writers (user_id, name, slug, model_alias, system_prompt, roles, enabled, temperature, is_system)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateWriter :one
UPDATE writers
SET name = $3, model_alias = $4, system_prompt = $5, roles = $6, enabled = $7, temperature = $8, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteWriter :execrows
DELETE FROM writers WHERE id = $1 AND user_id = $2 AND is_system = false;
