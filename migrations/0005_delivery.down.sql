DROP TABLE delivery_attempts;
DROP INDEX notifications_claimed;
ALTER TABLE notifications
    DROP COLUMN provider_message_id,
    DROP COLUMN failure_reason,
    DROP COLUMN claimed_until,
    DROP COLUMN attempts;
