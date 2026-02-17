-- Create sessions table
-- Session records for analytics. Style profiles stored in Redis with TTL.

CREATE TABLE IF NOT EXISTS sessions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    retailer_id             UUID NOT NULL REFERENCES retailers(id) ON DELETE CASCADE,
    external_session_id     VARCHAR(255),
    user_agent              TEXT,
    ip_hash                 VARCHAR(64),
    started_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at                TIMESTAMPTZ,
    turn_count              INT NOT NULL DEFAULT 0,
    has_image_upload        BOOLEAN NOT NULL DEFAULT false,
    has_purchase            BOOLEAN NOT NULL DEFAULT false,
    total_tokens_used       INT NOT NULL DEFAULT 0
);

-- Query sessions by retailer and date
CREATE INDEX IF NOT EXISTS idx_sessions_retailer_started
    ON sessions(retailer_id, started_at DESC);

-- Conversion analysis
CREATE INDEX IF NOT EXISTS idx_sessions_retailer_purchase
    ON sessions(retailer_id, has_purchase);

-- Find active sessions (no end time)
CREATE INDEX IF NOT EXISTS idx_sessions_active
    ON sessions(retailer_id, started_at) WHERE ended_at IS NULL;

COMMENT ON TABLE sessions IS 'Session records for analytics. Style profiles stored in Redis.';
COMMENT ON COLUMN sessions.external_session_id IS 'Client-provided session identifier for correlation';
COMMENT ON COLUMN sessions.ip_hash IS 'Hashed IP address for analytics (privacy-preserving)';
COMMENT ON COLUMN sessions.turn_count IS 'Number of conversation turns in this session';
COMMENT ON COLUMN sessions.total_tokens_used IS 'Total LLM tokens consumed (for cost tracking)';
