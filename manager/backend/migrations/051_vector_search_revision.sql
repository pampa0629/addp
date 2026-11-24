INSERT INTO manager.search_vector_revisions(tenant_id,version)
 SELECT DISTINCT tenant_id,1 FROM manager.embeddings WHERE tenant_id>0 ON CONFLICT(tenant_id) DO NOTHING;

CREATE OR REPLACE FUNCTION manager.advance_search_vector_revision() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog,manager AS $$
DECLARE old_tenant bigint; new_tenant bigint;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF ROW(OLD.tenant_id,OLD.engine_id,OLD.item_fingerprint,OLD.item_id,OLD.locator,OLD.source_version,OLD.embedding,OLD.model_profile_id,OLD.profile_version,OLD.deployment_id,OLD.dimension,OLD.status)
 IS NOT DISTINCT FROM ROW(NEW.tenant_id,NEW.engine_id,NEW.item_fingerprint,NEW.item_id,NEW.locator,NEW.source_version,NEW.embedding,NEW.model_profile_id,NEW.profile_version,NEW.deployment_id,NEW.dimension,NEW.status) THEN RETURN NULL; END IF;
 END IF;
 IF TG_OP<>'INSERT' THEN old_tenant:=OLD.tenant_id; END IF;
 IF TG_OP<>'DELETE' THEN new_tenant:=NEW.tenant_id; END IF;
 IF old_tenant IS NOT NULL THEN
  INSERT INTO manager.search_vector_revisions VALUES(old_tenant,1)
  ON CONFLICT(tenant_id) DO UPDATE SET version=manager.search_vector_revisions.version+1;
 END IF;
 IF new_tenant IS NOT NULL AND new_tenant IS DISTINCT FROM old_tenant THEN
  INSERT INTO manager.search_vector_revisions VALUES(new_tenant,1)
  ON CONFLICT(tenant_id) DO UPDATE SET version=manager.search_vector_revisions.version+1;
 END IF;
 RETURN NULL;
END $$;
CREATE OR REPLACE TRIGGER search_vector_membership AFTER INSERT OR DELETE ON manager.embeddings
 FOR EACH ROW EXECUTE FUNCTION manager.advance_search_vector_revision();
CREATE OR REPLACE TRIGGER search_vector_update AFTER UPDATE ON manager.embeddings FOR EACH ROW
 EXECUTE FUNCTION manager.advance_search_vector_revision();
CREATE OR REPLACE TRIGGER search_vector_truncate AFTER TRUNCATE ON manager.embeddings
 EXECUTE FUNCTION manager.advance_truncated_search_material();
