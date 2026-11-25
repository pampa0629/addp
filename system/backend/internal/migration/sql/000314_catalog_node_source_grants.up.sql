BEGIN;

-- System proves the provider model and the Meta node before insertion.
-- The existing tenant/engine foreign key and typed path checks remain in force.
CREATE OR REPLACE FUNCTION system.guard_node_source_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.node_recursive IS FALSE AND NEW.catalog_path->'segments'->0->>'term'='service'
    AND jsonb_array_length(NEW.catalog_path->'segments')=1 THEN
  RAISE EXCEPTION 'service root requires recursive node scope'
    USING ERRCODE='23514',CONSTRAINT='source_grant_node_engine';
 END IF;
 RETURN NEW;
END;
$$;

COMMIT;
