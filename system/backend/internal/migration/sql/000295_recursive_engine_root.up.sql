BEGIN;

-- Admit the recursive MinIO service root in the existing node Grant profile.
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

CREATE OR REPLACE FUNCTION system.guard_node_source_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.node_recursive IS NOT NULL AND NOT EXISTS (SELECT 1 FROM system.engines e
  WHERE e.tenant_id=NEW.tenant_id AND e.id=NEW.engine_id AND
   ((e.engine_type='postgresql' AND NEW.catalog_path->'segments'->0->>'term'='server') OR
    (e.engine_type='nfs' AND NEW.catalog_path->'segments'->0->>'term'='root') OR
    (e.engine_type='minio' AND NEW.catalog_path->'segments'->0->>'term'='service' AND (jsonb_array_length(NEW.catalog_path->'segments')>=2 OR NEW.node_recursive)))) THEN
  RAISE EXCEPTION 'node scope requires its admitted engine catalog profile'
   USING ERRCODE='23514',CONSTRAINT='source_grant_node_engine';
 END IF;
 RETURN NEW;
END;
$$;
COMMIT;
