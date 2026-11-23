-- Tracing (ADR 0034): the W3C traceparent of the request that created a notification, so that the relay, which
-- publishes it later and in another process, continues that trace, and a retry joins it too.
ALTER TABLE notifications
    ADD COLUMN trace_parent text
        CONSTRAINT notifications_trace_parent CHECK (trace_parent ~ '^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$');
