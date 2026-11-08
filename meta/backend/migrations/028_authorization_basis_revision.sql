CREATE TABLE meta.authorization_basis_generation (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    generation uuid NOT NULL DEFAULT gen_random_uuid()
);
INSERT INTO meta.authorization_basis_generation(singleton) VALUES (true);

CREATE TABLE meta.authorization_basis_revisions (
    tenant_id bigint PRIMARY KEY CHECK (tenant_id > 0),
    version bigint NOT NULL CHECK (version > 0)
);
INSERT INTO meta.authorization_basis_revisions(tenant_id, version)
SELECT DISTINCT tenant_id, 1 FROM (
    SELECT tenant_id FROM meta.meta_item UNION SELECT tenant_id FROM meta.meta_node
) tenants WHERE tenant_id > 0;

-- Increment in the fact writer's transaction. Sequence allocation and timestamps
-- cannot prove committed order: a lower sequence number may commit later.
CREATE FUNCTION meta.advance_authorization_basis_revision() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE old_tenant bigint; new_tenant bigint;
BEGIN
    IF TG_OP <> 'INSERT' THEN old_tenant := OLD.tenant_id; END IF;
    IF TG_OP <> 'DELETE' THEN new_tenant := NEW.tenant_id; END IF;
    IF old_tenant IS NOT NULL THEN
        INSERT INTO meta.authorization_basis_revisions VALUES (old_tenant, 1)
        ON CONFLICT (tenant_id) DO UPDATE
        SET version = meta.authorization_basis_revisions.version + 1;
    END IF;
    IF new_tenant IS NOT NULL AND new_tenant IS DISTINCT FROM old_tenant THEN
        INSERT INTO meta.authorization_basis_revisions VALUES (new_tenant, 1)
        ON CONFLICT (tenant_id) DO UPDATE
        SET version = meta.authorization_basis_revisions.version + 1;
    END IF;
    RETURN NULL;
END $$;

CREATE TRIGGER authorization_basis_item_membership
AFTER INSERT OR DELETE ON meta.meta_item
FOR EACH ROW EXECUTE FUNCTION meta.advance_authorization_basis_revision();
CREATE TRIGGER authorization_basis_item_update
AFTER UPDATE ON meta.meta_item FOR EACH ROW
WHEN (ROW(OLD.id,OLD.tenant_id,OLD.engine_id,OLD.node_id,OLD.item_type,OLD.name,OLD.full_name,OLD.fingerprint,OLD.attributes,OLD.deleted_at)
   IS DISTINCT FROM ROW(NEW.id,NEW.tenant_id,NEW.engine_id,NEW.node_id,NEW.item_type,NEW.name,NEW.full_name,NEW.fingerprint,NEW.attributes,NEW.deleted_at))
EXECUTE FUNCTION meta.advance_authorization_basis_revision();
CREATE TRIGGER authorization_basis_node_membership
AFTER INSERT OR DELETE ON meta.meta_node
FOR EACH ROW EXECUTE FUNCTION meta.advance_authorization_basis_revision();
CREATE TRIGGER authorization_basis_node_update
AFTER UPDATE ON meta.meta_node FOR EACH ROW
WHEN (ROW(OLD.id,OLD.tenant_id,OLD.engine_id,OLD.parent_node_id,OLD.node_type,OLD.name,OLD.path,OLD.full_name,OLD.attributes,OLD.deleted_at)
   IS DISTINCT FROM ROW(NEW.id,NEW.tenant_id,NEW.engine_id,NEW.parent_node_id,NEW.node_type,NEW.name,NEW.path,NEW.full_name,NEW.attributes,NEW.deleted_at))
EXECUTE FUNCTION meta.advance_authorization_basis_revision();

CREATE FUNCTION meta.advance_truncated_authorization_basis() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE meta.authorization_basis_generation SET generation = gen_random_uuid();
    RETURN NULL;
END $$;
CREATE TRIGGER authorization_basis_item_truncate AFTER TRUNCATE ON meta.meta_item
EXECUTE FUNCTION meta.advance_truncated_authorization_basis();
CREATE TRIGGER authorization_basis_node_truncate AFTER TRUNCATE ON meta.meta_node
EXECUTE FUNCTION meta.advance_truncated_authorization_basis();
