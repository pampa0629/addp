-- Historical releases remain byte-exact audit evidence, not a supported runtime
-- format. NOT VALID retains those rows; every new write must use the v2 contract.
ALTER TABLE ontology.platform_revisions DROP CONSTRAINT platform_payload_identity;
ALTER TABLE ontology.platform_revisions ADD CONSTRAINT platform_payload_identity CHECK (COALESCE(
    jsonb_typeof(payload::jsonb) = 'object' AND
    payload::jsonb->>'contract' = 'addp.platform-definition/v2' AND
    payload::jsonb->>'compiler' = 'addp.platform-compiler/v2' AND
    payload::jsonb#>>'{definition,schema_version}' = 'addp.platform-capability-context/v1' AND
    payload::jsonb#>>'{definition,knowledge_kind}' = 'platform_definition' AND
    payload::jsonb#>>'{definition,capability}' = capability AND
    jsonb_typeof(payload::jsonb#>'{definition,revision}') = 'number' AND
    payload::jsonb#>>'{definition,revision}' = revision::text AND
    payload::jsonb#>>'{definition,digest}' = '' AND
    payload::jsonb#>>'{definition,availability}' = 'not_observed' AND
    NOT (payload::jsonb->'definition' ? 'tenant_id') AND
    jsonb_typeof(payload::jsonb->'review') = 'object' AND
    payload::jsonb#>>'{review,method}' = 'curated' AND
    payload::jsonb#>>'{review,scope}' = 'selected_capability', FALSE
)) NOT VALID;
