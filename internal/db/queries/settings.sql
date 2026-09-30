-- name: GetSettings :one
SELECT * FROM user_settings WHERE user_id = $1;

-- name: EnsureSettings :exec
INSERT INTO user_settings (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING;

-- name: UpsertSettings :one
INSERT INTO user_settings (user_id, scene_token_limit, autosave_snapshot_minutes)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE
    SET scene_token_limit = EXCLUDED.scene_token_limit,
        autosave_snapshot_minutes = EXCLUDED.autosave_snapshot_minutes,
        updated_at = now()
RETURNING *;
