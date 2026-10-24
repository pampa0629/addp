BEGIN;
-- The same immutable declaration admits one optional, exact kernel identity.
-- This changes no data, permissions, roles, sessions, or lease semantics.
ALTER TABLE system.module_runtime_instances DROP CONSTRAINT process_metrics_declaration_shape;
ALTER TABLE system.module_runtime_instances ADD CONSTRAINT process_metrics_declaration_shape CHECK (
 process_metrics IS NULL OR (
  jsonb_typeof(process_metrics) = 'object'
  AND process_metrics ?& ARRAY['schema_version','endpoint']
  AND (process_metrics - 'schema_version' - 'endpoint' - 'cgroup') = '{}'::jsonb
  AND process_metrics->>'schema_version' = 'addp.process-metrics/v1'
  AND jsonb_typeof(process_metrics->'endpoint') = 'string'
  AND length(process_metrics->>'endpoint') BETWEEN 1 AND 512
  AND process_started_at IS NOT NULL
  AND (NOT process_metrics ? 'cgroup' OR (
   jsonb_typeof(process_metrics->'cgroup') = 'object'
   AND process_metrics->'cgroup' ?& ARRAY['kernel_boot_id','cgroup_id','pid_namespace_id']
   AND ((process_metrics->'cgroup') - 'kernel_boot_id' - 'cgroup_id' - 'pid_namespace_id') = '{}'::jsonb
   AND jsonb_typeof(process_metrics->'cgroup'->'kernel_boot_id') = 'string'
   AND process_metrics->'cgroup'->>'kernel_boot_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   AND process_metrics->'cgroup'->>'kernel_boot_id' <> '00000000-0000-0000-0000-000000000000'
   AND jsonb_typeof(process_metrics->'cgroup'->'cgroup_id') = 'string'
   AND process_metrics->'cgroup'->>'cgroup_id' ~ '^[1-9][0-9]{0,19}$'
   AND (length(process_metrics->'cgroup'->>'cgroup_id') < 20 OR (process_metrics->'cgroup'->>'cgroup_id') COLLATE "C" <= '18446744073709551615')
   AND jsonb_typeof(process_metrics->'cgroup'->'pid_namespace_id') = 'string'
   AND process_metrics->'cgroup'->>'pid_namespace_id' ~ '^[1-9][0-9]{0,19}$'
   AND (length(process_metrics->'cgroup'->>'pid_namespace_id') < 20 OR (process_metrics->'cgroup'->>'pid_namespace_id') COLLATE "C" <= '18446744073709551615')
  ))
 ) IS TRUE
);
COMMIT;
