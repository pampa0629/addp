-- Engine ID identifies the registered physical service across address moves.
-- A deleted tombstone retains its audit facts but no longer reserves an address.
DROP INDEX system.uq_engines_tenant_type_identity;
DROP INDEX system.uq_engines_platform_type_identity;

CREATE UNIQUE INDEX uq_engines_tenant_type_identity
    ON system.engines (tenant_id, lower(engine_type), identity_key)
    WHERE tenant_id IS NOT NULL AND lifecycle_state <> 'deleted';

CREATE UNIQUE INDEX uq_engines_platform_type_identity
    ON system.engines (lower(engine_type), identity_key)
    WHERE tenant_id IS NULL AND lifecycle_state <> 'deleted';
