-- Seed data: default PoC retailer
INSERT INTO retailers (slug, name, api_key_hash, config)
VALUES (
    'poc-store',
    'PoC Store',
    -- sha256 of 'poc-dev-key' for local development only
    encode(digest('poc-dev-key', 'sha256'), 'hex'),
    '{"currency":"MXN","country":"MX","style_focus":"smart_casual","budget_tiers":{"low":500,"mid":1500,"high":3000}}'::jsonb
)
ON CONFLICT (slug) DO NOTHING;
