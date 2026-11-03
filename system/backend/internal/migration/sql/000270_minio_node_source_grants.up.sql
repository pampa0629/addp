BEGIN;

-- Extend the same immutable node Grant profile to MinIO buckets/prefixes.
-- Existing precise and node Grants and their history are not rewritten.
CREATE OR REPLACE FUNCTION system.valid_source_node_path(path jsonb, engine bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
DECLARE parts jsonb; part jsonb; n integer; i integer; term text;
BEGIN
 IF engine<=0 OR jsonb_typeof(path) IS DISTINCT FROM 'object' THEN RETURN false; END IF;
 IF path->>'version' IS DISTINCT FROM 'catalog.path/v1' OR path->'engine_id' IS DISTINCT FROM to_jsonb(engine)
 OR jsonb_typeof(path->'segments') IS DISTINCT FROM 'array' OR (SELECT count(*) FROM jsonb_object_keys(path))<>3 THEN RETURN false; END IF;
 parts:=path->'segments'; n:=jsonb_array_length(parts);
 IF n<1 OR n>64 THEN RETURN false; END IF;
 IF parts->0=jsonb_build_object('term','server','kind','server','name','') THEN
  RETURN (n=1 OR (n=2 AND parts->1=jsonb_build_object('term','schema','kind','namespace','name',parts->1->>'name')
   AND jsonb_typeof(parts->1->'name')='string' AND length(parts->1->>'name')>0 AND octet_length(parts->1->>'name')<=512)) IS TRUE;
 END IF;
 IF parts->0=jsonb_build_object('term','service','kind','service','name','') THEN
  IF n<2 THEN RETURN false; END IF;
  FOR i IN 1..n-1 LOOP
   part:=parts->i; term:=CASE WHEN i=1 THEN 'bucket' ELSE 'prefix' END;
   IF part IS DISTINCT FROM jsonb_build_object('term',term,'kind',term,'name',part->>'name')
    OR jsonb_typeof(part->'name') IS DISTINCT FROM 'string' OR length(part->>'name')=0 OR octet_length(part->>'name')>4096
    OR part->>'name' IN ('.','..') OR strpos(part->>'name','/')>0 OR strpos(part->>'name',E'\\')>0 THEN RETURN false; END IF;
  END LOOP;
  RETURN true;
 END IF;
 IF parts->0 IS DISTINCT FROM jsonb_build_object('term','root','kind','root','name','') THEN RETURN false; END IF;
 FOR part IN SELECT value FROM jsonb_array_elements(parts) WITH ORDINALITY AS p(value,pos) WHERE pos>1 LOOP
  IF part IS DISTINCT FROM jsonb_build_object('term','directory','kind','directory','name',part->>'name') OR jsonb_typeof(part->'name') IS DISTINCT FROM 'string' OR length(part->>'name')=0
   OR octet_length(part->>'name')>4096 OR part->>'name' IN ('.','..') OR strpos(part->>'name','/')>0 OR strpos(part->>'name',E'\\')>0 THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
END;
$$;

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
    AND target->'segments'->2=jsonb_build_object('term','table','kind','table','name',target->'segments'->2->>'name')
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

CREATE OR REPLACE FUNCTION system.guard_node_source_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.node_recursive IS NOT NULL AND NOT EXISTS (SELECT 1 FROM system.engines e
  WHERE e.tenant_id=NEW.tenant_id AND e.id=NEW.engine_id AND
   ((e.engine_type='postgresql' AND NEW.catalog_path->'segments'->0->>'term'='server') OR
    (e.engine_type='nfs' AND NEW.catalog_path->'segments'->0->>'term'='root') OR
    (e.engine_type='minio' AND NEW.catalog_path->'segments'->0->>'term'='service' AND jsonb_array_length(NEW.catalog_path->'segments')>=2))) THEN
  RAISE EXCEPTION 'node scope requires its admitted engine catalog profile'
   USING ERRCODE='23514',CONSTRAINT='source_grant_node_engine';
 END IF;
 RETURN NEW;
END;
$$;
COMMIT;
