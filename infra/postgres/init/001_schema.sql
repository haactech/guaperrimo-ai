-- StyleRAG PostgreSQL Schema
-- Matches Flyway migrations in db/migrations/

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ============================================================
-- retailers: B2B customers who integrate StyleRAG
-- ============================================================
CREATE TABLE IF NOT EXISTS retailers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(255) NOT NULL,
    slug            VARCHAR(100) NOT NULL UNIQUE,
    api_key_hash    VARCHAR(255) NOT NULL,
    config          JSONB NOT NULL DEFAULT '{}',
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_retailers_slug ON retailers(slug);
CREATE INDEX IF NOT EXISTS idx_retailers_active ON retailers(is_active) WHERE is_active = true;

-- ============================================================
-- products: catalog metadata (embeddings in Qdrant)
-- ============================================================
CREATE TABLE IF NOT EXISTS products (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    retailer_id     UUID NOT NULL REFERENCES retailers(id) ON DELETE CASCADE,
    sku             VARCHAR(100) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    description     TEXT,
    category        VARCHAR(100) NOT NULL,
    subcategory     VARCHAR(100),
    colors          TEXT[] NOT NULL DEFAULT '{}',
    fit             VARCHAR(50),
    style_tags      TEXT[] NOT NULL DEFAULT '{}',
    price           DECIMAL(10, 2) NOT NULL,
    currency        VARCHAR(10) NOT NULL DEFAULT 'MXN',
    sizes           TEXT[] NOT NULL DEFAULT '{}',
    image_url       TEXT,
    metadata        JSONB DEFAULT '{}',
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_products_retailer_sku ON products(retailer_id, sku);
CREATE INDEX IF NOT EXISTS idx_products_retailer_category ON products(retailer_id, category);
CREATE INDEX IF NOT EXISTS idx_products_retailer_active ON products(retailer_id, is_active) WHERE is_active = true;
CREATE INDEX IF NOT EXISTS idx_products_style_tags ON products USING GIN(style_tags);
CREATE INDEX IF NOT EXISTS idx_products_colors ON products USING GIN(colors);
CREATE INDEX IF NOT EXISTS idx_products_retailer_price ON products(retailer_id, price);

-- ============================================================
-- sessions: records for analytics (style profiles in Redis)
-- ============================================================
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

CREATE INDEX IF NOT EXISTS idx_sessions_retailer_started ON sessions(retailer_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_retailer_purchase ON sessions(retailer_id, has_purchase);
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(retailer_id, started_at) WHERE ended_at IS NULL;

-- ============================================================
-- session_events: detailed event log for analytics
-- ============================================================
CREATE TABLE IF NOT EXISTS session_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    event_type      VARCHAR(50) NOT NULL,
    payload         JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_session_events_session_time ON session_events(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_session_events_type_time ON session_events(event_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_session_events_payload ON session_events USING GIN(payload);

-- ============================================================
-- recommendations: track products recommended + conversions
-- ============================================================
CREATE TABLE IF NOT EXISTS recommendations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    product_id      UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    rank            INT NOT NULL,
    score           DECIMAL(5, 4),
    context         JSONB DEFAULT '{}',
    was_clicked     BOOLEAN NOT NULL DEFAULT false,
    was_purchased   BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recommendations_session_rank ON recommendations(session_id, rank);
CREATE INDEX IF NOT EXISTS idx_recommendations_product_clicked ON recommendations(product_id, was_clicked);
CREATE INDEX IF NOT EXISTS idx_recommendations_product_purchased ON recommendations(product_id, was_purchased);
CREATE INDEX IF NOT EXISTS idx_recommendations_product ON recommendations(product_id, created_at DESC);

-- ============================================================
-- updated_at trigger (reusable)
-- ============================================================
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_retailers_updated_at ON retailers;
CREATE TRIGGER trigger_retailers_updated_at
    BEFORE UPDATE ON retailers
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS trigger_products_updated_at ON products;
CREATE TRIGGER trigger_products_updated_at
    BEFORE UPDATE ON products
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
