-- Listing and retrying notifications (ADR 0014, 0018).

-- When a notification failed; bulk retry selects failed notifications by it. A retried notification is queued
-- again and loses it.
ALTER TABLE notifications
    ADD COLUMN failed_at timestamptz,
    ADD CONSTRAINT notifications_failed_at CHECK ((status = 'failed') = (failed_at IS NOT NULL));

-- The list's order, newest first; the index is read backwards.
CREATE INDEX notifications_created ON notifications (created_at, id);

-- Bulk retry's candidates only.
CREATE INDEX notifications_failed ON notifications (failed_at) WHERE status = 'failed';
