-- The relay and the scheduler (ADR 0021).

-- The relay deletes the rows it has published: the outbox is a queue of work to hand over, not a log, and the
-- notifications keep the history.
ALTER TABLE outbox DROP COLUMN published_at;

-- Wakes the relay as soon as rows are committed. One notification per statement, so a batch written with COPY
-- sends one; PostgreSQL delivers it at commit, and merges identical ones within a transaction.
CREATE FUNCTION notify_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('outbox', '');
    RETURN NULL;
END
$$;

CREATE TRIGGER outbox_notify AFTER INSERT ON outbox FOR EACH STATEMENT EXECUTE FUNCTION notify_outbox();

-- When a waiting notification is due: a scheduled one's time, a retrying one's next attempt (ADR 0006).
ALTER TABLE notifications
    ADD COLUMN next_attempt_at timestamptz,
    ADD CONSTRAINT notifications_next_attempt_at
        CHECK ((status IN ('scheduled', 'retrying')) = (next_attempt_at IS NOT NULL));

-- The scheduler's due query.
CREATE INDEX notifications_due ON notifications (next_attempt_at) WHERE status IN ('scheduled', 'retrying');
