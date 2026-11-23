-- Batches of notifications (ADR 0017). A batch keeps the items it accepted as notifications, and the items it
-- rejected as their positions and reasons only: their content is personal data and is not kept.

CREATE TABLE batches (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    total           integer     NOT NULL CHECK (total >= 1),
    accepted        integer     NOT NULL,
    -- [{"index": 3, "fields": [{"field": "to", "code": "invalid", "message": "..."}]}]
    rejections      jsonb       NOT NULL DEFAULT '[]',
    idempotency_key text        CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    request_hash    bytea       CHECK (octet_length(request_hash) = 32),
    correlation_id  text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- A batch with nothing valid is refused, not stored.
    CONSTRAINT batches_accepted CHECK (accepted BETWEEN 1 AND total),
    CONSTRAINT batches_rejections_add_up CHECK (jsonb_array_length(rejections) = total - accepted),
    CONSTRAINT batches_key_has_hash CHECK ((idempotency_key IS NULL) = (request_hash IS NULL))
);

-- A batch's key holds for as long as the batch exists. Batch keys and notification keys are apart.
CREATE UNIQUE INDEX batches_idempotency_key ON batches (idempotency_key) WHERE idempotency_key IS NOT NULL;

ALTER TABLE notifications
    ADD COLUMN batch_id    uuid    REFERENCES batches (id),
    -- The notification's position in its batch's request, counted from zero.
    ADD COLUMN batch_index integer CHECK (batch_index >= 0),
    ADD CONSTRAINT notifications_batch_has_index CHECK ((batch_id IS NULL) = (batch_index IS NULL));

-- One notification per position; reads a batch's notifications in request order and counts them by status.
CREATE UNIQUE INDEX notifications_batch ON notifications (batch_id, batch_index) WHERE batch_id IS NOT NULL;
