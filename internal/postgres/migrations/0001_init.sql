-- +goose Up
CREATE TABLE channels (
    telegram_channel_id BIGINT PRIMARY KEY,
    public_username TEXT NOT NULL CHECK (public_username <> ''),
    status TEXT NOT NULL CHECK (status IN (
        'ACTIVE',
        'ACCESS_ERROR',
        'DISCUSSION_UNAVAILABLE',
        'DELIVERY_UNAVAILABLE'
    )),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX channels_status_idx ON channels (status);

CREATE TABLE posts (
    id UUID PRIMARY KEY,
    telegram_channel_id BIGINT NOT NULL REFERENCES channels (telegram_channel_id) ON DELETE CASCADE,
    telegram_message_id BIGINT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL,
    text TEXT NOT NULL CHECK (text <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (telegram_channel_id, telegram_message_id)
);

CREATE INDEX posts_channel_published_idx ON posts (telegram_channel_id, published_at DESC);

CREATE TABLE fact_checks (
    post_id UUID PRIMARY KEY REFERENCES posts (id) ON DELETE CASCADE,
    result JSONB NOT NULL CHECK (jsonb_typeof(result) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE jobs (
    id UUID PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('FACTCHECK_POST', 'PUBLISH_COMMENT')),
    payload JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSING', 'COMPLETED', 'RETRY', 'DEAD')),
    attempts INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    last_error TEXT,
    dedupe_key TEXT NOT NULL CHECK (dedupe_key <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((locked_at IS NULL) = (locked_by IS NULL)),
    CHECK (status = 'PROCESSING' OR (locked_at IS NULL AND locked_by IS NULL)),
    UNIQUE (type, dedupe_key)
);

CREATE INDEX jobs_claim_idx
    ON jobs (available_at, created_at)
    WHERE status IN ('PENDING', 'RETRY');

-- +goose Down
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS fact_checks;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS channels;
