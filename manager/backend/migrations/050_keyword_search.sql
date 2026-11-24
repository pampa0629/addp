ALTER TABLE manager.content_index_outlets
 ADD COLUMN IF NOT EXISTS search_revision bigint NOT NULL DEFAULT 0,
 ADD COLUMN IF NOT EXISTS next_search_ordinal bigint NOT NULL DEFAULT 0;

-- Index metadata, maintained on indexing only. No query state, body or Allow.
CREATE TABLE IF NOT EXISTS manager.search_document_ordinals (
 index_name varchar(128) NOT NULL,
 document_id varchar(128) NOT NULL,
 search_ordinal bigint NOT NULL CHECK(search_ordinal>0),
 PRIMARY KEY(index_name,document_id),
 UNIQUE(index_name,search_ordinal)
);
CREATE TABLE IF NOT EXISTS manager.search_material_generation (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 generation uuid NOT NULL DEFAULT gen_random_uuid()
);
INSERT INTO manager.search_material_generation(singleton) VALUES(true) ON CONFLICT(singleton) DO NOTHING;
CREATE TABLE IF NOT EXISTS manager.search_vector_revisions (
 tenant_id bigint PRIMARY KEY CHECK(tenant_id>0),
 version bigint NOT NULL CHECK(version>0)
);
CREATE OR REPLACE FUNCTION manager.advance_search_index_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN
  UPDATE manager.content_index_outlets SET search_revision=search_revision+1 WHERE index_name=OLD.index_name;
 END IF;
 IF TG_OP<>'DELETE' AND (TG_OP='INSERT' OR NEW.index_name IS DISTINCT FROM OLD.index_name) THEN
  UPDATE manager.content_index_outlets SET search_revision=search_revision+1 WHERE index_name=NEW.index_name;
 END IF;
 RETURN NULL;
END $$;
CREATE OR REPLACE TRIGGER search_index_revision AFTER INSERT OR UPDATE OR DELETE ON manager.content_index_deliveries
 FOR EACH ROW EXECUTE FUNCTION manager.advance_search_index_revision();
CREATE OR REPLACE FUNCTION manager.advance_truncated_search_material() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE manager.search_material_generation SET generation=gen_random_uuid();
 RETURN NULL;
END $$;
CREATE OR REPLACE TRIGGER search_index_truncate AFTER TRUNCATE ON manager.content_index_deliveries
 EXECUTE FUNCTION manager.advance_truncated_search_material();
