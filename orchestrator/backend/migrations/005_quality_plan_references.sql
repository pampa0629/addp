BEGIN;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM common.task_executions WHERE module='orchestrator' AND status IN ('pending','running')) THEN
  IF EXISTS (SELECT 1 FROM orchestrator.orchestrations o, jsonb_array_elements(o.steps) s WHERE s->>'provider'='quality' AND s->>'task_type' IN ('check','data_validation')) THEN
   RAISE EXCEPTION 'drain orchestrations before replacing Quality task references';
  END IF;
 END IF;
END $$;
UPDATE orchestrator.orchestrations o
SET steps=(SELECT jsonb_agg(CASE WHEN s->>'provider'='quality' AND s->>'task_type' IN ('check','data_validation')
 THEN jsonb_set(jsonb_set(s,'{task_type}','"quality_plan"'::jsonb),'{task_id}',to_jsonb((s->>'task_id')::bigint*2+CASE WHEN s->>'task_type'='data_validation' THEN 1 ELSE 0 END))
 ELSE s END ORDER BY ordinal)
 FROM jsonb_array_elements(o.steps) WITH ORDINALITY AS entries(s,ordinal)), updated_at=now()
WHERE EXISTS (SELECT 1 FROM jsonb_array_elements(o.steps) s WHERE s->>'provider'='quality' AND s->>'task_type' IN ('check','data_validation'));
COMMIT;
