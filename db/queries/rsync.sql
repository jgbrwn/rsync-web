-- name: CreateRsyncHistory :one
INSERT INTO rsync_history (source, destination, options, full_command, status)
VALUES (?, ?, ?, ?, 'pending')
RETURNING *;

-- name: GetRsyncHistory :one
SELECT * FROM rsync_history WHERE id = ?;

-- name: ListRsyncHistory :many
SELECT * FROM rsync_history ORDER BY created_at DESC LIMIT ?;

-- name: UpdateRsyncStatus :exec
UPDATE rsync_history 
SET status = ?, exit_code = ?, output = ?, started_at = ?, completed_at = ?
WHERE id = ?;

-- name: UpdateRsyncRunning :exec
UPDATE rsync_history SET status = 'running', started_at = ? WHERE id = ?;

-- name: DeleteRsyncHistory :exec
DELETE FROM rsync_history WHERE id = ?;

-- name: ClearAllHistory :exec
DELETE FROM rsync_history;
