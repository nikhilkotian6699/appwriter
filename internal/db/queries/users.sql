-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: GetFirstUser :one
SELECT * FROM users ORDER BY created_at ASC LIMIT 1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: CreateUser :one
INSERT INTO users (username, display_name, password_hash, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2, auth_version = auth_version + 1, updated_at = now()
WHERE id = $1;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
