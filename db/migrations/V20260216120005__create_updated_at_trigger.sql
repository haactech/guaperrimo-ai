-- Create trigger function for automatic updated_at management
-- Reusable across all tables with updated_at column

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply trigger to retailers table
DROP TRIGGER IF EXISTS trigger_retailers_updated_at ON retailers;
CREATE TRIGGER trigger_retailers_updated_at
    BEFORE UPDATE ON retailers
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Apply trigger to products table
DROP TRIGGER IF EXISTS trigger_products_updated_at ON products;
CREATE TRIGGER trigger_products_updated_at
    BEFORE UPDATE ON products
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

COMMENT ON FUNCTION update_updated_at_column() IS 'Automatically sets updated_at to NOW() on row updates';
