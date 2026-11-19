-- name: CreateNotification :exec
INSERT INTO notifications (user_id, order_id, kind, message)
VALUES (?, ?, ?, ?);

-- name: ListNotificationsForUser :many
SELECT id, user_id, order_id, kind, message, is_read, created_at
FROM notifications
WHERE user_id = ?
ORDER BY id DESC
LIMIT 20;
