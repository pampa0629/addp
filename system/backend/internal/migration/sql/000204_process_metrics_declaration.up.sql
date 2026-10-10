BEGIN;
ALTER TABLE system.module_runtime_instances ADD COLUMN process_metrics jsonb;
ALTER TABLE system.module_runtime_instances ADD CONSTRAINT process_metrics_declaration_shape CHECK (
    process_metrics IS NULL OR (
        jsonb_typeof(process_metrics) = 'object'
        AND process_metrics ?& ARRAY['schema_version','endpoint']
        AND (process_metrics - 'schema_version' - 'endpoint') = '{}'::jsonb
        AND process_metrics->>'schema_version' = 'addp.process-metrics/v1'
        AND jsonb_typeof(process_metrics->'endpoint') = 'string'
        AND length(process_metrics->>'endpoint') BETWEEN 1 AND 512
        AND process_started_at IS NOT NULL
    ) IS TRUE
);
CREATE FUNCTION system.guard_process_metrics_declaration() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.process_metrics IS DISTINCT FROM OLD.process_metrics
       OR (OLD.process_metrics IS NOT NULL AND (
           NEW.role IS DISTINCT FROM OLD.role OR NEW.process_started_at IS DISTINCT FROM OLD.process_started_at
       )) THEN
        RAISE EXCEPTION 'process metrics declaration is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER process_metrics_declaration_immutable BEFORE UPDATE ON system.module_runtime_instances
    FOR EACH ROW EXECUTE FUNCTION system.guard_process_metrics_declaration();
COMMIT;
