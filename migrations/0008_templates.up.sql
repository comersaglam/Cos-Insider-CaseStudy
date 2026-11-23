-- Templates (ADR 0032): a body with {{.name}} placeholders for one channel, rendered when a notification is created
-- from it. The name rule matches notification.TemplateDraft.Validate.
CREATE TABLE templates (
    id         uuid        PRIMARY KEY,
    name       text        NOT NULL CHECK (char_length(name) <= 64 AND name ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    channel    text        NOT NULL CHECK (channel IN ('sms', 'email', 'push')),
    body       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT templates_name UNIQUE (name)
);

-- Lists are read newest first by (created_at, id).
CREATE INDEX templates_created ON templates (created_at, id);

-- The template a notification was rendered from. It is history, not a link: the notification keeps its rendered
-- content, so deleting the template leaves the ID in place rather than refusing the delete or clearing it.
ALTER TABLE notifications ADD COLUMN template_id uuid;
