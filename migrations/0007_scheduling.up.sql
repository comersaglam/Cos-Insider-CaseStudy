-- Scheduled notifications (ADR 0031): the time a notification was scheduled for, kept after it is queued, so that
-- the API can show it. While a notification is scheduled, next_attempt_at holds the same time (ADR 0006).
ALTER TABLE notifications
    ADD COLUMN scheduled_at timestamptz,
    ADD CONSTRAINT notifications_scheduled_at CHECK (status <> 'scheduled' OR scheduled_at IS NOT NULL);
