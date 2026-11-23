-- Notifications and the outbox that hands them to RabbitMQ (ADR 0003, 0011, 0015).
-- Enumerations are text with a CHECK, which a later migration can change in one statement.

CREATE TABLE notifications (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    channel         text        NOT NULL CHECK (channel IN ('sms', 'email', 'push')),
    -- The API and the provider call it "to"; TO is a reserved word in SQL.
    recipient       text        NOT NULL,
    content         text        NOT NULL,
    priority        text        NOT NULL CHECK (priority IN ('high', 'normal', 'low')),
    status          text        NOT NULL
        CHECK (status IN ('scheduled', 'queued', 'sending', 'retrying', 'sent', 'failed', 'cancelled')),
    idempotency_key text        CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    -- SHA-256 of the request, telling a repeated request from a different one that reuses its key.
    request_hash    bytea       CHECK (octet_length(request_hash) = 32),
    correlation_id  text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notifications_key_has_hash CHECK ((idempotency_key IS NULL) = (request_hash IS NULL))
);

-- A key is unique for as long as its notification exists; requests without one are never compared.
CREATE UNIQUE INDEX notifications_idempotency_key ON notifications (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE outbox (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    notification_id uuid        NOT NULL REFERENCES notifications (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    published_at    timestamptz
);
