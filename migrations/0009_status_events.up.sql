-- Status updates (ADR 0033): every committed change of a notification's status is announced on the channel
-- notification_status, whichever process made it, for the API to pass on to its WebSocket subscribers. NOTIFY is
-- sent when the transaction commits, and not at all when it rolls back. The payload, well under NOTIFY's 8000
-- bytes, carries what a subscriber is told.
CREATE FUNCTION notify_status() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('notification_status', json_build_object(
        'notificationId', NEW.id,
        'batchId', NEW.batch_id,
        'status', NEW.status,
        'at', NEW.updated_at
    )::text);
    RETURN NULL;
END
$$;

CREATE TRIGGER notifications_status_notify AFTER UPDATE OF status ON notifications
    FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status) EXECUTE FUNCTION notify_status();
