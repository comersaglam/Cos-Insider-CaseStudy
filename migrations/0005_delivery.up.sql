-- Sending: the claim's lease, the attempt count, the outcome, and a record of every attempt (ADR 0007, 0012, 0014).

ALTER TABLE notifications
    ADD COLUMN attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    -- Until when the worker that claimed it holds it; after that another may claim it (ADR 0012).
    ADD COLUMN claimed_until timestamptz,
    ADD COLUMN failure_reason text
        CHECK (failure_reason IN ('provider_rejected', 'attempts_exhausted', 'unprocessable_message')),
    ADD COLUMN provider_message_id text,
    ADD CONSTRAINT notifications_claimed_until CHECK ((status = 'sending') = (claimed_until IS NOT NULL)),
    ADD CONSTRAINT notifications_failure_reason CHECK ((status = 'failed') = (failure_reason IS NOT NULL));

-- The reaper's query: notifications whose worker's lease ran out.
CREATE INDEX notifications_claimed ON notifications (claimed_until) WHERE status = 'sending';

CREATE TABLE delivery_attempts (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    notification_id uuid        NOT NULL REFERENCES notifications (id),
    attempt         integer     NOT NULL CHECK (attempt >= 1),
    outcome         text        NOT NULL
        CHECK (outcome IN ('success', 'permanent', 'permanent_alert', 'transient', 'unknown')),
    -- The provider's HTTP status; NULL when there was no answer.
    status_code     integer,
    error           text,
    duration_ms     integer     NOT NULL CHECK (duration_ms >= 0),
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX delivery_attempts_notification ON delivery_attempts (notification_id, attempt);
