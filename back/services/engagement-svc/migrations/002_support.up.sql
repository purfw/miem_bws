
CREATE TABLE IF NOT EXISTS engagement_svc.support_messages (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid,
    name          varchar(255) NOT NULL,
    email         varchar(255) NOT NULL,
    topic         varchar(50)  NOT NULL DEFAULT 'other',
    subject       varchar(500),
    message       text         NOT NULL,
    conference_id uuid,
    ip_address    inet,
    status        varchar(20)  NOT NULL DEFAULT 'new',
    created_at    timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT chk_support_status CHECK (status IN ('new', 'in_progress', 'closed')),
    CONSTRAINT chk_support_message CHECK (length(message) BETWEEN 10 AND 5000)
);

CREATE INDEX IF NOT EXISTS idx_support_status
    ON engagement_svc.support_messages (status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_ip_created
    ON engagement_svc.support_messages (ip_address, created_at DESC);
