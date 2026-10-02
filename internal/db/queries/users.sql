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

-- name: SetUserRole :exec
UPDATE users SET role = $2, updated_at = now() WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at, username;

-- name: UpdateUserDisplayName :one
UPDATE users SET display_name = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: LockAccounts :exec
SELECT pg_advisory_xact_lock(hashtext('writersguild-accounts'));

-- name: GetUserForUpdate :one
SELECT * FROM users WHERE id = $1 FOR UPDATE;

-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL;

-- name: UpdateUserByAdmin :one
UPDATE users SET display_name = $2, role = $3, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DisableUser :one
UPDATE users SET disabled_at = now(), auth_version = auth_version + 1, updated_at = now() WHERE id = $1 RETURNING *;

-- name: EnableUser :one
UPDATE users SET disabled_at = NULL, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ListActiveRunIDsByUser :many
SELECT id FROM runs WHERE user_id = $1 AND status IN ('queued', 'running');

-- name: CancelRunsByUser :execrows
UPDATE runs SET status = 'cancelled', error = 'the account was disabled', finished_at = now()
WHERE user_id = $1 AND status IN ('queued', 'running');
