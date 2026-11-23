DROP INDEX notifications_due;
ALTER TABLE notifications DROP COLUMN next_attempt_at;
DROP TRIGGER outbox_notify ON outbox;
DROP FUNCTION notify_outbox();
ALTER TABLE outbox ADD COLUMN published_at timestamptz;
