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
UPDATE notifications SET status = 'cancelled', next_attempt_at = NULL, updated_at = now()
WHERE id = @id AND status = ANY (@cancellable::text[])
RETURNING *;

-- name: RequeueFailedNotification :one
-- Puts a failed notification back in the queue; returns no row when it is not failed.
UPDATE notifications
SET status = 'queued', failed_at = NULL, failure_reason = NULL, attempts = 0, updated_at = now()
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
    UPDATE notifications n
    SET status = 'queued', failed_at = NULL, failure_reason = NULL, attempts = 0, updated_at = now()
    FROM picked WHERE n.id = picked.id
    RETURNING n.id
)
INSERT INTO outbox (notification_id) SELECT id FROM requeued;

-- name: ClaimOutbox :many
-- Takes up to batch_size outbox rows, oldest first, with what publishing them needs, and locks them until the
-- transaction ends; SKIP LOCKED leaves rows another relay holds.
SELECT o.id, o.notification_id, n.channel, n.priority, n.correlation_id
FROM outbox o
JOIN notifications n ON n.id = o.notification_id
ORDER BY o.id
LIMIT sqlc.arg(batch_size)::integer
FOR UPDATE OF o SKIP LOCKED;

-- name: DeleteOutbox :exec
DELETE FROM outbox WHERE id = ANY (@ids::bigint[]);

-- name: QueueDueNotifications :execrows
-- Queues up to batch_size waiting notifications whose time has come, soonest first, with their outbox rows, in one
-- statement. SKIP LOCKED keeps two schedulers from queueing one notification twice.
WITH due AS (
    SELECT d.id FROM notifications d
    WHERE d.status IN ('scheduled', 'retrying') AND d.next_attempt_at <= now()
    ORDER BY d.next_attempt_at
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
), queued AS (
    UPDATE notifications n SET status = 'queued', next_attempt_at = NULL, updated_at = now()
    FROM due WHERE n.id = due.id
    RETURNING n.id
)
INSERT INTO outbox (notification_id) SELECT id FROM queued;

-- name: ClaimNotification :one
-- Claims a notification for sending (ADR 0012): one that is queued, or sending under a lease that ran out, while
-- it has attempts left. Spends an attempt and sets the lease. previous_status tells a first claim from a takeover.
WITH current AS (
    SELECT c.id, c.status FROM notifications c WHERE c.id = @id FOR UPDATE
)
UPDATE notifications n
SET status = 'sending', claimed_until = now() + sqlc.arg(lease_ms)::integer * interval '1 millisecond',
    attempts = n.attempts + 1, updated_at = now()
FROM current
WHERE n.id = current.id
  AND n.attempts < sqlc.arg(max_attempts)::integer
  AND (n.status = 'queued' OR (n.status = 'sending' AND n.claimed_until < now()))
RETURNING sqlc.embed(n), current.status AS previous_status;

-- name: InsertDeliveryAttempt :exec
INSERT INTO delivery_attempts (notification_id, attempt, outcome, status_code, error, duration_ms)
VALUES (@notification_id, @attempt, @outcome, sqlc.narg(status_code), sqlc.narg(error), @duration_ms);

-- name: FinishSending :execrows
-- Records what became of a claimed notification, if the claim is still the one that made the attempt.
UPDATE notifications
SET status = @status,
    claimed_until = NULL,
    next_attempt_at = sqlc.narg(next_attempt_at),
    failure_reason = sqlc.narg(failure_reason),
    failed_at = CASE WHEN @status = 'failed' THEN now() END,
    provider_message_id = coalesce(sqlc.narg(provider_message_id), provider_message_id),
    updated_at = now()
WHERE id = @id AND status = 'sending' AND attempts = @attempt;

-- name: MarkUnprocessable :exec
-- Fails a queued notification whose message cannot be processed (ADR 0014).
UPDATE notifications
SET status = 'failed', failure_reason = 'unprocessable_message', failed_at = now(), updated_at = now()
WHERE id = @id AND status = 'queued';

-- name: ReapExpiredClaims :execrows
-- Returns notifications whose worker's lease ran out to retrying, due at once, or to failed when no attempt is
-- left, recording the attempt the lost worker made as unknown (ADR 0012).
WITH expired AS (
    SELECT e.id, e.attempts FROM notifications e
    WHERE e.status = 'sending' AND e.claimed_until < now()
    ORDER BY e.claimed_until
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
), recorded AS (
    INSERT INTO delivery_attempts (notification_id, attempt, outcome, error, duration_ms)
    SELECT expired.id, expired.attempts, 'unknown', 'the worker that claimed it stopped before recording an outcome', 0
    FROM expired
)
UPDATE notifications n
SET status = CASE WHEN expired.attempts >= sqlc.arg(max_attempts)::integer THEN 'failed' ELSE 'retrying' END,
    claimed_until = NULL,
    next_attempt_at = CASE WHEN expired.attempts >= sqlc.arg(max_attempts)::integer THEN NULL ELSE now() END,
    failure_reason = CASE WHEN expired.attempts >= sqlc.arg(max_attempts)::integer THEN 'attempts_exhausted' END,
    failed_at = CASE WHEN expired.attempts >= sqlc.arg(max_attempts)::integer THEN now() END,
    updated_at = now()
FROM expired
WHERE n.id = expired.id;

-- name: ListDeliveryAttempts :many
SELECT * FROM delivery_attempts WHERE notification_id = @notification_id ORDER BY attempt, id;

-- name: ReleaseClaim :exec
-- Undoes a claim whose send never started, giving back its attempt, if the claim is still the one that took it.
UPDATE notifications
SET status = 'queued', claimed_until = NULL, attempts = attempts - 1, updated_at = now()
WHERE id = @id AND status = 'sending' AND attempts = @attempt;
