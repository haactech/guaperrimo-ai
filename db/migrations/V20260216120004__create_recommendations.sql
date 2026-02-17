-- Create recommendations table
-- Track product recommendations for conversion analysis

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

-- Recommendations per session
CREATE INDEX IF NOT EXISTS idx_recommendations_session_rank
    ON recommendations(session_id, rank);

-- Product click-through analysis
CREATE INDEX IF NOT EXISTS idx_recommendations_product_clicked
    ON recommendations(product_id, was_clicked);

-- Product conversion analysis
CREATE INDEX IF NOT EXISTS idx_recommendations_product_purchased
    ON recommendations(product_id, was_purchased);

-- Find all recommendations for a product
CREATE INDEX IF NOT EXISTS idx_recommendations_product
    ON recommendations(product_id, created_at DESC);

COMMENT ON TABLE recommendations IS 'Track which products were recommended and their outcomes';
COMMENT ON COLUMN recommendations.rank IS 'Position in the recommendation list (1-based)';
COMMENT ON COLUMN recommendations.score IS 'RAG relevance score (0.0000 to 1.0000)';
COMMENT ON COLUMN recommendations.context IS 'Recommendation context: query_style_tags, matched_tags, price_in_budget, outfit_role';
