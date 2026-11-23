-- name: InsertNotification :one
-- Inserts a notification unless its idempotency key is taken, in which case it returns no row.
INSERT INTO notifications (
    id, channel, recipient, content, priority, status, idempotency_key, request_hash, correlation_id
) VALUES (
    @id, @channel, @recipient, @content, @priority, @status,
    sqlc.narg(idempotency_key), sqlc.narg(request_hash), @correlation_id
)
ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING *;

-- name: InsertOutbox :exec
INSERT INTO outbox (notification_id) VALUES (@notification_id);

-- name: GetNotification :one
SELECT * FROM notifications WHERE id = @id;

-- name: GetNotificationByIdempotencyKey :one
SELECT * FROM notifications WHERE idempotency_key = @idempotency_key;
