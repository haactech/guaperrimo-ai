-- Create session_events table
-- Detailed event log for analytics and debugging

CREATE TABLE IF NOT EXISTS session_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    event_type      VARCHAR(50) NOT NULL,
    payload         JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Event timeline for a session
CREATE INDEX IF NOT EXISTS idx_session_events_session_time
    ON session_events(session_id, created_at);

-- Event type analysis across all sessions
CREATE INDEX IF NOT EXISTS idx_session_events_type_time
    ON session_events(event_type, created_at DESC);

-- GIN index for payload queries (e.g., finding specific product interactions)
CREATE INDEX IF NOT EXISTS idx_session_events_payload
    ON session_events USING GIN(payload);

COMMENT ON TABLE session_events IS 'Detailed event log for analytics';
COMMENT ON COLUMN session_events.event_type IS 'Event types: message_user, message_agent, image_uploaded, image_analyzed, search_executed, products_shown, product_clicked, product_purchased, voice_transcribed';
COMMENT ON COLUMN session_events.payload IS 'Event-specific data (varies by event_type)';
