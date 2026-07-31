ALTER TABLE manager.data_profiles
    ADD COLUMN IF NOT EXISTS data_scope JSONB NOT NULL DEFAULT '{"kind":"all"}'::jsonb;
