-- Links table: the source of truth. Redis is a cache in front of this.
--
-- The code is the primary key — it's a base62-encoded Snowflake id, unique by
-- construction. We keep both the raw int64 (for range analytics on creation
-- time) and the encoded string (for lookup).

CREATE TABLE IF NOT EXISTS links (
    code         VARCHAR(16) PRIMARY KEY,
    snowflake_id BIGINT      NOT NULL UNIQUE,
    original_url TEXT        NOT NULL,
    api_key      VARCHAR(64),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ,
    active       BOOLEAN     NOT NULL DEFAULT true,
    click_count  BIGINT      NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_links_active_expires
    ON links(active, expires_at)
    WHERE active = true;

CREATE INDEX IF NOT EXISTS idx_links_api_key
    ON links(api_key)
    WHERE api_key IS NOT NULL;
