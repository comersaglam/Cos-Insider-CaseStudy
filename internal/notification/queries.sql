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

-- name: InsertBatch :one
-- Inserts a batch unless its idempotency key is taken, in which case it returns no row.
INSERT INTO batches (id, total, accepted, rejections, idempotency_key, request_hash, correlation_id)
VALUES (@id, @total, @accepted, @rejections, sqlc.narg(idempotency_key), sqlc.narg(request_hash), @correlation_id)
ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING *;

-- name: CopyBatchNotifications :copyfrom
INSERT INTO notifications (
    id, channel, recipient, content, priority, status, correlation_id, batch_id, batch_index
) VALUES (
    @id, @channel, @recipient, @content, @priority, @status, @correlation_id, @batch_id, @batch_index
);

-- name: CopyOutbox :copyfrom
INSERT INTO outbox (notification_id) VALUES (@notification_id);

-- name: GetBatch :one
SELECT * FROM batches WHERE id = @id;

-- name: GetBatchByIdempotencyKey :one
SELECT * FROM batches WHERE idempotency_key = @idempotency_key;

-- name: ListBatchItems :many
SELECT batch_index, id FROM notifications WHERE batch_id = @batch_id ORDER BY batch_index;

-- name: CountBatchStatuses :many
SELECT status, count(*)::integer AS count FROM notifications WHERE batch_id = @batch_id GROUP BY status;
