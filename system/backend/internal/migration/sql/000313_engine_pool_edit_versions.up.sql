ALTER TABLE system.engines ADD COLUMN pool_only_since_version bigint
    CHECK (pool_only_since_version >= 1 AND pool_only_since_version < version);
