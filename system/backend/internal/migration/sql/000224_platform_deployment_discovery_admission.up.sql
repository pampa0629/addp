BEGIN;
ALTER TABLE system.engine_deployments DROP CONSTRAINT engine_deployments_inventory_check;
-- Canonicalize only the current representation; immutable declaration/audit payloads remain original.
UPDATE system.engine_deployments
SET inventory=jsonb_set(inventory,'{schema}','"addp.engine-deployment-inventory/v1"'::jsonb)
WHERE inventory->>'schema'='addp.business-deployment-inventory/v1';
ALTER TABLE system.engine_deployments ADD CONSTRAINT engine_deployments_inventory_check
CHECK (COALESCE(jsonb_typeof(inventory)='object'
 AND inventory->>'schema'='addp.engine-deployment-inventory/v1'
 AND inventory->'source'->>'type'='docker_compose'
 AND inventory->'source'->>'runtime' IN ('linux_docker','desktop_linux_vm')
 AND inventory->'source'->>'project' IN ('addp-infra','business')
 AND inventory->'source'->>'docker_engine_id' ~ '^[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}$'
 AND jsonb_typeof(inventory->'services')='array' AND jsonb_array_length(inventory->'services')=1
 AND inventory->'services'->0->>'service'='postgres'
 AND inventory->'services'->0->>'engine_type'='postgresql', false));
COMMIT;
