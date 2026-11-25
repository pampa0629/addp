BEGIN;

CREATE OR REPLACE FUNCTION system.guard_node_source_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.node_recursive IS NOT NULL AND NOT EXISTS (SELECT 1 FROM system.engines e
  WHERE e.tenant_id=NEW.tenant_id AND e.id=NEW.engine_id AND
   ((e.engine_type IN ('postgresql','gaussdb') AND NEW.catalog_path->'segments'->0->>'term'='server') OR
    (e.engine_type IN ('nfs','ftp') AND NEW.catalog_path->'segments'->0->>'term'='root') OR
    (e.engine_type IN ('minio','rustfs') AND NEW.catalog_path->'segments'->0->>'term'='service' AND (jsonb_array_length(NEW.catalog_path->'segments')>=2 OR NEW.node_recursive)))) THEN
  RAISE EXCEPTION 'node scope requires its admitted engine catalog profile'
   USING ERRCODE='23514',CONSTRAINT='source_grant_node_engine';
 END IF;
 RETURN NEW;
END;
$$;
COMMIT;
