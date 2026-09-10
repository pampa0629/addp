BEGIN;

-- Tenant custom roles may derive only the short-lived execution scope already
-- allowed by their audience and effect permissions. This mechanism permission
-- does not grant data effects or Engine control-plane access by itself.
UPDATE system.permissions
SET tenant_customizable = true,
    updated_at = transaction_timestamp()
WHERE permission_key = 'system.execution_authorization.create'
  AND status = 'active';

COMMIT;
