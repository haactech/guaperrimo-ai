INSERT INTO retailers (slug, name, config)
VALUES (
  'poc-store',
  'PoC Store',
  '{"currency":"MXN","country":"MX"}'::jsonb
)
ON CONFLICT (slug) DO NOTHING;
