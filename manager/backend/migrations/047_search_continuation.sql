ALTER TABLE manager.content_index_outlets
 ADD COLUMN IF NOT EXISTS search_revision bigint NOT NULL DEFAULT 0,
 ADD COLUMN IF NOT EXISTS next_search_ordinal bigint NOT NULL DEFAULT 0;

-- Index metadata, maintained on indexing only. No query state, body or Allow.
CREATE TABLE manager.search_document_ordinals (
 index_name varchar(128) NOT NULL,
 document_id varchar(128) NOT NULL,
 search_ordinal bigint NOT NULL CHECK(search_ordinal>0),
 PRIMARY KEY(index_name,document_id),
 UNIQUE(index_name,search_ordinal)
);
CREATE TABLE manager.search_material_generation (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 generation uuid NOT NULL DEFAULT gen_random_uuid()
);
INSERT INTO manager.search_material_generation(singleton) VALUES(true);
CREATE TABLE manager.search_vector_revisions (
 tenant_id bigint PRIMARY KEY CHECK(tenant_id>0),
 version bigint NOT NULL CHECK(version>0)
);
INSERT INTO manager.search_vector_revisions(tenant_id,version)
 SELECT DISTINCT tenant_id,1 FROM manager.embeddings WHERE tenant_id>0;

CREATE FUNCTION manager.advance_search_index_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN
  UPDATE manager.content_index_outlets SET search_revision=search_revision+1 WHERE index_name=OLD.index_name;
 END IF;
 IF TG_OP<>'DELETE' AND (TG_OP='INSERT' OR NEW.index_name IS DISTINCT FROM OLD.index_name) THEN
  UPDATE manager.content_index_outlets SET search_revision=search_revision+1 WHERE index_name=NEW.index_name;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER search_index_revision AFTER INSERT OR UPDATE OR DELETE ON manager.content_index_deliveries
 FOR EACH ROW EXECUTE FUNCTION manager.advance_search_index_revision();
CREATE FUNCTION manager.advance_search_vector_revision() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog,manager AS $$
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
CREATE TRIGGER search_vector_membership AFTER INSERT OR DELETE ON manager.embeddings
 FOR EACH ROW EXECUTE FUNCTION manager.advance_search_vector_revision();
CREATE TRIGGER search_vector_update AFTER UPDATE ON manager.embeddings FOR EACH ROW
 EXECUTE FUNCTION manager.advance_search_vector_revision();
CREATE FUNCTION manager.advance_truncated_search_material() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE manager.search_material_generation SET generation=gen_random_uuid();
 RETURN NULL;
END $$;
CREATE TRIGGER search_vector_truncate AFTER TRUNCATE ON manager.embeddings
 EXECUTE FUNCTION manager.advance_truncated_search_material();
CREATE TRIGGER search_index_truncate AFTER TRUNCATE ON manager.content_index_deliveries
 EXECUTE FUNCTION manager.advance_truncated_search_material();
