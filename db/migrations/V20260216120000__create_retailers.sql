-- Create retailers table
-- B2B customers who integrate StyleRAG into their e-commerce

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

-- Index for slug lookups (authentication)
CREATE INDEX IF NOT EXISTS idx_retailers_slug ON retailers(slug);

-- Index for active retailers
CREATE INDEX IF NOT EXISTS idx_retailers_active ON retailers(is_active) WHERE is_active = true;

COMMENT ON TABLE retailers IS 'B2B customers who integrate StyleRAG';
COMMENT ON COLUMN retailers.slug IS 'URL-friendly unique identifier for API routing';
COMMENT ON COLUMN retailers.api_key_hash IS 'Hashed API key for authentication (never store plain text)';
COMMENT ON COLUMN retailers.config IS 'Retailer-specific settings: currency, language, style_focus, budget_tiers, features';
