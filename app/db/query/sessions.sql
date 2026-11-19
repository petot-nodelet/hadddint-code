-- name: FindSession :one
SELECT data
FROM sessions
WHERE id = ? AND expires_at > ?;

-- name: CommitSession :exec
INSERT INTO sessions (id, data, expires_at)
VALUES (?, ?, ?)
ON CONFLICT (id) DO UPDATE SET data = excluded.data, expires_at = excluded.expires_at;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions
WHERE expires_at <= ?;
