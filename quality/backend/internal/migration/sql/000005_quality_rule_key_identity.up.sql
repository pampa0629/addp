DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM quality.rule_applications
        WHERE jsonb_typeof(rule_config) IS DISTINCT FROM 'object'
           OR rule_config->>'schema_version' IS DISTINCT FROM 'addp.quality.rules/v1'
           OR jsonb_typeof(rule_config->'rules') IS DISTINCT FROM 'array'
    ) THEN
        RAISE EXCEPTION 'quality.rule_applications contains invalid addp.quality.rules/v1 documents';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM quality.rule_applications AS application
        CROSS JOIN LATERAL jsonb_array_elements(application.rule_config->'rules') AS rules(rule)
        WHERE jsonb_typeof(rule) IS DISTINCT FROM 'object'
    ) THEN
        RAISE EXCEPTION 'quality.rule_applications contains non-object rules';
    END IF;
END $$;

UPDATE quality.rule_applications AS application
SET rule_config = jsonb_set(
    application.rule_config,
    '{rules}',
    (
        SELECT COALESCE(jsonb_agg(
            CASE WHEN rule ? 'rule_key' THEN rule ELSE jsonb_set(
                rule,
                '{rule_key}',
                to_jsonb(
                    substr(hash, 1, 8) || '-' || substr(hash, 9, 4) || '-' || substr(hash, 13, 4) || '-' || substr(hash, 17, 4) || '-' || substr(hash, 21, 12)
                ),
                true
            ) END
            ORDER BY ordinal
        ), '[]'::jsonb)
        FROM (
            SELECT rule, ordinal, md5('quality.rule_application:' || application.tenant_id::text || ':' || application.id::text || ':' || ordinal::text) AS hash
            FROM jsonb_array_elements(application.rule_config->'rules') WITH ORDINALITY AS rules(rule, ordinal)
        ) AS keyed_rules
    )
)
WHERE EXISTS (
    SELECT 1 FROM jsonb_array_elements(application.rule_config->'rules') AS rules(rule)
    WHERE NOT (rule ? 'rule_key')
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM quality.rule_applications AS application
        CROSS JOIN LATERAL jsonb_array_elements(application.rule_config->'rules') AS rules(rule)
        WHERE jsonb_typeof(rule->'rule_key') IS DISTINCT FROM 'string'
           OR rule->>'rule_key' !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
           OR rule->>'rule_key' = '00000000-0000-0000-0000-000000000000'
    ) THEN
        RAISE EXCEPTION 'quality.rule_applications contains invalid rule_key values';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM quality.rule_applications AS application
        CROSS JOIN LATERAL jsonb_array_elements(application.rule_config->'rules') AS rules(rule)
        GROUP BY application.id, rule->>'rule_key'
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'quality.rule_applications contains duplicate rule_key values';
    END IF;
END $$;

ALTER TABLE quality.issues ADD COLUMN rule_key UUID;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM quality.issues AS issue
        JOIN quality.rule_applications AS application
          ON application.tenant_id = issue.tenant_id
         AND application.id = issue.rule_application_id
        CROSS JOIN LATERAL (
            SELECT COUNT(*) AS matching_rules
            FROM jsonb_array_elements(application.rule_config->'rules') AS rules(rule)
            WHERE rule->>'type' = issue.rule_type
        ) AS matches
        WHERE matches.matching_rules <> 1
    ) THEN
        RAISE EXCEPTION 'quality.issues contains rule identities that cannot be mapped uniquely to rule_key';
    END IF;
END $$;

UPDATE quality.issues AS issue
SET rule_key = matched.rule_key::UUID
FROM (
    SELECT issue_row.id, rule->>'rule_key' AS rule_key
    FROM quality.issues AS issue_row
    JOIN quality.rule_applications AS application
      ON application.tenant_id = issue_row.tenant_id
     AND application.id = issue_row.rule_application_id
    CROSS JOIN LATERAL jsonb_array_elements(application.rule_config->'rules') AS rules(rule)
    WHERE rule->>'type' = issue_row.rule_type
) AS matched
WHERE issue.id = matched.id;

ALTER TABLE quality.issues ALTER COLUMN rule_key SET NOT NULL;

DROP INDEX quality.uq_quality_issue_rule_application;

CREATE UNIQUE INDEX uq_quality_issue_rule
    ON quality.issues (tenant_id, rule_application_id, rule_key);
