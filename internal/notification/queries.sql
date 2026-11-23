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

-- name: ListNotifications :many
-- A page of notifications, newest first, after the cursor (after_created_at, after_id) when one is given. A NULL
-- filter selects everything.
SELECT * FROM notifications
WHERE (sqlc.narg(statuses)::text[] IS NULL OR status = ANY (sqlc.narg(statuses)::text[]))
  AND (sqlc.narg(channels)::text[] IS NULL OR channel = ANY (sqlc.narg(channels)::text[]))
  AND (sqlc.narg(batch_id)::uuid IS NULL OR batch_id = sqlc.narg(batch_id)::uuid)
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR created_at >= sqlc.narg(created_from)::timestamptz)
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR created_at < sqlc.narg(created_to)::timestamptz)
  AND (sqlc.narg(after_created_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: CancelNotification :one
-- Cancels a notification that is still in one of the given statuses; returns no row otherwise.
UPDATE notifications SET status = 'cancelled', updated_at = now()
WHERE id = @id AND status = ANY (@cancellable::text[])
RETURNING *;

-- name: RequeueFailedNotification :one
-- Puts a failed notification back in the queue; returns no row when it is not failed.
UPDATE notifications SET status = 'queued', failed_at = NULL, updated_at = now()
WHERE id = @id AND status = 'failed'
RETURNING *;

-- name: CountFailedNotifications :one
SELECT count(*)::integer FROM notifications
WHERE status = 'failed' AND failed_at >= sqlc.arg(failed_from)::timestamptz
  AND failed_at < sqlc.arg(failed_to)::timestamptz
  AND (sqlc.narg(channel)::text IS NULL OR channel = sqlc.narg(channel)::text)
  AND (sqlc.narg(batch_id)::uuid IS NULL OR batch_id = sqlc.narg(batch_id)::uuid);

-- name: RequeueFailedNotifications :execrows
-- Puts up to max_rows failed notifications, oldest failure first, back in the queue with their outbox rows, in one
-- statement. SKIP LOCKED keeps two bulk retries from requeueing one notification twice.
WITH picked AS (
    SELECT f.id FROM notifications f
    WHERE f.status = 'failed' AND f.failed_at >= sqlc.arg(failed_from)::timestamptz
      AND f.failed_at < sqlc.arg(failed_to)::timestamptz
      AND (sqlc.narg(channel)::text IS NULL OR f.channel = sqlc.narg(channel)::text)
      AND (sqlc.narg(batch_id)::uuid IS NULL OR f.batch_id = sqlc.narg(batch_id)::uuid)
    ORDER BY f.failed_at, f.id
    LIMIT sqlc.arg(max_rows)::integer
    FOR UPDATE SKIP LOCKED
), requeued AS (
    UPDATE notifications n SET status = 'queued', failed_at = NULL, updated_at = now()
    FROM picked WHERE n.id = picked.id
    RETURNING n.id
)
INSERT INTO outbox (notification_id) SELECT id FROM requeued;
