BEGIN;

ALTER TABLE system.delegated_access_tokens
  ADD COLUMN rule_evaluation_digest text,
  ADD COLUMN rule_evaluation_consumed_at timestamptz,
  ADD CONSTRAINT delegated_rule_evaluation_scope CHECK (
    (scopes = ARRAY['ontology.rule.evaluate']::text[]) = (rule_evaluation_digest IS NOT NULL)
    AND (rule_evaluation_digest IS NULL OR (audience = 'ontology' AND rule_evaluation_digest ~ '^[0-9a-f]{64}$'))
    AND (rule_evaluation_consumed_at IS NULL OR (rule_evaluation_digest IS NOT NULL AND revoked_at IS NOT DISTINCT FROM rule_evaluation_consumed_at))
  );

CREATE OR REPLACE FUNCTION system.validate_delegated_access_token_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.token_hash <> OLD.token_hash
       OR NEW.source_access_token_id <> OLD.source_access_token_id
       OR NEW.audience <> OLD.audience
       OR NEW.scopes <> OLD.scopes
       OR NEW.agent_run_id <> OLD.agent_run_id
       OR NEW.tool_call_id <> OLD.tool_call_id
       OR NEW.expires_at <> OLD.expires_at
       OR NEW.created_at <> OLD.created_at
       OR NEW.rule_evaluation_digest IS DISTINCT FROM OLD.rule_evaluation_digest
       OR OLD.revoked_at IS NOT NULL
       OR NEW.revoked_at IS NULL THEN
        RAISE EXCEPTION 'delegated access token facts are immutable and revocation is one-way'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES
    ('service.ontology_fact.read', 'service', 'read', 'medium', false, ARRAY['tenant']::text[], false,
     'permissions.service.ontology_fact.read.name', 'permissions.service.ontology_fact.read.description', 'active'),
    ('system.ontology_fact.execute', 'system', 'execute', 'medium', false, ARRAY['tenant']::text[], false,
     'permissions.system.ontology_fact.execute.name', 'permissions.system.ontology_fact.execute.description', 'active');

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product' FROM system.roles r JOIN system.permissions p
  ON (r.role_key = 'tenant.ontology_runtime' AND p.permission_key = 'service.ontology_fact.read')
  OR (r.role_key = 'tenant.service_runtime' AND p.permission_key = 'system.ontology_fact.execute')
WHERE r.tenant_id IS NULL;

COMMIT;
