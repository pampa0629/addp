ADD CONSTRAINT ck_execution_authorization_scope_v3 CHECK (
        source_notebook_session_authorization_id IS NULL AND cleanup_scope IS NULL AND source_read_scope IS NULL
        AND (develop_file_scope IS NULL OR (audience='develop' AND source_type='user' AND internal_task IS NULL
             AND source_execution_attempt IS NULL AND source_execution_lease_token IS NULL))
        AND (develop_file_business_deadline IS NULL OR (develop_file_scope IS NOT NULL AND develop_file_business_deadline<=expires_at))
        AND ((audience <> 'ontology' AND internal_task IS NULL) OR (
            audience='ontology' AND internal_task IS NOT NULL AND source_type='user'
            AND source_execution_attempt IS NULL AND source_execution_lease_token IS NULL
            AND JSON_SCHEMA_VALID('{
                "type":"object","additionalProperties":false,
                "required":["task_type","resource_id","revision","digest","generation"],
                "properties":{
                    "task_type":{"enum":["semantic_projection"]},
                    "resource_id":{"type":"string","pattern":"^[a-z][a-z0-9_]{0,63}$"},
                    "revision":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
                    "digest":{"type":"string","pattern":"^[0-9a-f]{64}$"},
                    "generation":{"type":"string","pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"}
                }
            }', internal_task)
            AND CAST(internal_task->>'$.revision' AS DECIMAL(19,0)) <= 9223372036854775807
            AND internal_task->>'$.generation' <> '00000000-0000-0000-0000-000000000000'
        ))
    )
