BEGIN;

CREATE OR REPLACE FUNCTION system.source_node_covers(node jsonb, recursive boolean, target jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
DECLARE a integer; b integer; i integer; part jsonb;
BEGIN
 IF system.valid_source_node_path(node,(node->>'engine_id')::bigint) IS NOT TRUE
 OR jsonb_typeof(target) IS DISTINCT FROM 'object' THEN RETURN false; END IF;
 IF jsonb_typeof(target->'segments') IS DISTINCT FROM 'array'
 OR node->'engine_id' IS DISTINCT FROM target->'engine_id' OR target->>'version' IS DISTINCT FROM 'catalog.path/v1'
 OR (SELECT count(*) FROM jsonb_object_keys(target))<>3 THEN RETURN false; END IF;
 a:=jsonb_array_length(node->'segments'); b:=jsonb_array_length(target->'segments');
 IF b<=a OR b>64 OR (NOT recursive AND b<>a+1) THEN RETURN false; END IF;
 FOR i IN 0..a-1 LOOP IF node->'segments'->i<>target->'segments'->i THEN RETURN false; END IF; END LOOP;
 IF node->'segments'->0->>'term'='server' THEN
  RETURN (b=3 AND system.valid_source_node_path(jsonb_set(target,'{segments}',(target->'segments')-2),(node->>'engine_id')::bigint)
    AND (target->'segments'->2->>'term'='table' OR
         (target->'segments'->1->>'term'='database' AND target->'segments'->2->>'term'='collection'))
    AND target->'segments'->2=jsonb_build_object('term',target->'segments'->2->>'term','kind',target->'segments'->2->>'term','name',target->'segments'->2->>'name')
    AND jsonb_typeof(target->'segments'->2->'name')='string'
    AND length(target->'segments'->2->>'name')>0 AND octet_length(target->'segments'->2->>'name')<=512) IS TRUE;
 END IF;
 part:=target->'segments'->(b-1);
 IF node->'segments'->0->>'term'='service' THEN
  IF NOT ((part->>'term'='object' AND part->>'kind'='object') OR (part->>'term'='prefix' AND part->>'kind'='prefix')) IS TRUE THEN RETURN false; END IF;
  RETURN system.valid_source_node_path(jsonb_set(target,ARRAY['segments',(b-1)::text],
    jsonb_build_object('term','prefix','kind','prefix','name',part->'name')),(node->>'engine_id')::bigint) IS TRUE
    AND part=jsonb_build_object('term',part->>'term','kind',part->>'kind','name',part->>'name');
 END IF;

 IF part IS DISTINCT FROM jsonb_build_object('term',part->>'term','kind',part->>'kind','name',part->>'name')
 OR NOT ((part->>'term'='file' AND part->>'kind'='file') OR (part->>'term'='directory' AND part->>'kind'='directory')) IS TRUE THEN RETURN false; END IF;
 RETURN system.valid_source_node_path(jsonb_set(target,ARRAY['segments',(b-1)::text],
   jsonb_build_object('term','directory','kind','directory','name',part->'name')),(node->>'engine_id')::bigint) IS TRUE;
END;
$$;

COMMIT;
