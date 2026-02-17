-- Create products table
-- Product catalog metadata. Embeddings stored separately in Qdrant.

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
    sizes           TEXT[] NOT NULL DEFAULT '{}',
    image_url       TEXT,
    metadata        JSONB DEFAULT '{}',
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unique constraint: SKU per retailer
CREATE UNIQUE INDEX IF NOT EXISTS idx_products_retailer_sku
    ON products(retailer_id, sku);

-- Query products by category
CREATE INDEX IF NOT EXISTS idx_products_retailer_category
    ON products(retailer_id, category);

-- Query active products
CREATE INDEX IF NOT EXISTS idx_products_retailer_active
    ON products(retailer_id, is_active) WHERE is_active = true;

-- GIN index for style tag filtering (array contains)
CREATE INDEX IF NOT EXISTS idx_products_style_tags
    ON products USING GIN(style_tags);

-- GIN index for color filtering
CREATE INDEX IF NOT EXISTS idx_products_colors
    ON products USING GIN(colors);

-- Price range queries
CREATE INDEX IF NOT EXISTS idx_products_retailer_price
    ON products(retailer_id, price);

COMMENT ON TABLE products IS 'Product catalog metadata. Embeddings stored in Qdrant with matching IDs.';
COMMENT ON COLUMN products.sku IS 'Retailer-specific product SKU';
COMMENT ON COLUMN products.style_tags IS 'Style descriptors: smart_casual, minimal, streetwear, etc.';
COMMENT ON COLUMN products.fit IS 'Fit type: slim, regular, relaxed, oversized';
COMMENT ON COLUMN products.metadata IS 'Additional product attributes not in fixed columns';
