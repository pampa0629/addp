DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM common.task_executions
        WHERE module = 'quality' AND status IN ('pending', 'running')
    ) THEN
        RAISE EXCEPTION 'quality rule_key identity migration requires no pending or running quality executions';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM quality.rule_applications AS application
        CROSS JOIN LATERAL jsonb_array_elements(application.rule_config->'rules') AS rules(rule)
        WHERE jsonb_typeof(rule) IS DISTINCT FROM 'object'
           OR jsonb_typeof(rule->'rule_key') IS DISTINCT FROM 'string'
    ) THEN
        RAISE EXCEPTION 'quality.rule_applications contains rules without an existing rule_key';
    END IF;
END $$;

CREATE TEMP TABLE quality_rule_key_remap ON COMMIT DROP AS
WITH expanded AS (
    SELECT
        application.tenant_id,
        application.id AS rule_application_id,
        application.element_id,
        rules.rule,
        rules.ordinal,
        rules.rule->>'rule_key' AS old_rule_key,
        encode(sha256(convert_to((rules.rule - 'rule_key')::text, 'UTF8')), 'hex') AS fingerprint
    FROM quality.rule_applications AS application
    CROSS JOIN LATERAL jsonb_array_elements(application.rule_config->'rules') WITH ORDINALITY AS rules(rule, ordinal)
), numbered AS (
    SELECT *, row_number() OVER (
        PARTITION BY rule_application_id, fingerprint
        ORDER BY ordinal
    ) AS duplicate_occurrence
    FROM expanded
), digested AS (
    SELECT *, sha256(
        decode(replace('f3889a4a-1675-4623-b6e3-773f9125a04d', '-', ''), 'hex') ||
        convert_to(
            format(
                'addp.quality.rule-backfill/v1|tenant_id=%s|element_id=%s|rule_fingerprint=%s|duplicate_occurrence=%s',
                tenant_id, element_id, fingerprint, duplicate_occurrence
            ),
            'UTF8'
        )
    ) AS digest
    FROM numbered
)
SELECT
    tenant_id,
    rule_application_id,
    rule,
    ordinal,
    old_rule_key,
    substr(encode(digest, 'hex'), 1, 8) || '-' ||
    substr(encode(digest, 'hex'), 9, 4) || '-8' ||
    substr(encode(digest, 'hex'), 14, 3) || '-' ||
    substr('89ab', (((get_byte(digest, 8) >> 4) & 3) + 1), 1) ||
    substr(encode(digest, 'hex'), 18, 3) || '-' ||
    substr(encode(digest, 'hex'), 21, 12) AS new_rule_key
FROM digested;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM quality.issues AS issue
        LEFT JOIN quality_rule_key_remap AS remap
          ON remap.tenant_id = issue.tenant_id
         AND remap.rule_application_id = issue.rule_application_id
         AND remap.old_rule_key = issue.rule_key::text
        WHERE remap.rule_application_id IS NULL
    ) THEN
        RAISE EXCEPTION 'quality.issues contains rule identities that cannot be mapped by existing rule_key';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM quality_rule_key_remap
        GROUP BY tenant_id, rule_application_id, new_rule_key
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'quality.rule_applications contains duplicate migrated rule_key values';
    END IF;
END $$;

DROP INDEX quality.uq_quality_issue_rule;

UPDATE quality.issues AS issue
SET rule_key = remap.new_rule_key::UUID
FROM quality_rule_key_remap AS remap
WHERE remap.tenant_id = issue.tenant_id
  AND remap.rule_application_id = issue.rule_application_id
  AND remap.old_rule_key = issue.rule_key::text;

WITH documents AS (
    SELECT rule_application_id, jsonb_agg(
        jsonb_set(rule, '{rule_key}', to_jsonb(new_rule_key), true)
        ORDER BY ordinal
    ) AS rules
    FROM quality_rule_key_remap
    GROUP BY rule_application_id
)
UPDATE quality.rule_applications AS application
SET rule_config = jsonb_set(application.rule_config, '{rules}', documents.rules)
FROM documents
WHERE application.id = documents.rule_application_id;

CREATE UNIQUE INDEX uq_quality_issue_rule
    ON quality.issues (tenant_id, rule_application_id, rule_key);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM quality.issues AS issue
        LEFT JOIN quality.rule_applications AS application
          ON application.tenant_id = issue.tenant_id
         AND application.id = issue.rule_application_id
        LEFT JOIN LATERAL (
            SELECT COUNT(*) AS matching_rules
            FROM jsonb_array_elements(application.rule_config->'rules') AS rules(rule)
            WHERE rule->>'rule_key' = issue.rule_key::text
        ) AS matches ON TRUE
        WHERE application.id IS NULL OR matches.matching_rules <> 1
    ) THEN
        RAISE EXCEPTION 'quality.issues contains rule_key values outside the current rule application snapshot';
    END IF;
END $$;
