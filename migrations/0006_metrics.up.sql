-- The queries behind the metrics (ADR 0026): waiting notifications counted by channel and status on every scrape,
-- and the attempts of the last minutes summarised by GET /v1/metrics.
CREATE INDEX notifications_waiting ON notifications (channel, status)
    WHERE status IN ('scheduled', 'queued', 'retrying', 'sending');

CREATE INDEX delivery_attempts_created ON delivery_attempts (created_at);
