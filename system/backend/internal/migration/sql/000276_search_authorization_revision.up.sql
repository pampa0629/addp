CREATE TABLE system.search_authorization_generation (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    generation uuid NOT NULL DEFAULT gen_random_uuid()
);
INSERT INTO system.search_authorization_generation(singleton) VALUES (true);
CREATE TABLE system.search_authorization_revisions (
    tenant_id bigint PRIMARY KEY CHECK (tenant_id > 0),
    version bigint NOT NULL CHECK (version > 0)
);
INSERT INTO system.search_authorization_revisions(tenant_id,version)
SELECT DISTINCT tenant_id,1 FROM system.engine_access_grants WHERE tenant_id > 0;

CREATE FUNCTION system.advance_search_authorization_revision() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE old_tenant bigint; new_tenant bigint;
BEGIN
    IF TG_TABLE_NAME = 'engine_access_grant_revocations' THEN
        IF TG_OP <> 'INSERT' THEN
            SELECT tenant_id INTO old_tenant FROM system.engine_access_grants WHERE request_id=OLD.request_id;
        END IF;
        IF TG_OP <> 'DELETE' THEN
            SELECT tenant_id INTO new_tenant FROM system.engine_access_grants WHERE request_id=NEW.request_id;
        END IF;
    ELSE
        IF TG_OP <> 'INSERT' THEN old_tenant := OLD.tenant_id; END IF;
        IF TG_OP <> 'DELETE' THEN new_tenant := NEW.tenant_id; END IF;
    END IF;
    IF old_tenant IS NOT NULL THEN
        INSERT INTO system.search_authorization_revisions VALUES(old_tenant,1)
        ON CONFLICT(tenant_id) DO UPDATE
        SET version=system.search_authorization_revisions.version+1;
    END IF;
    IF new_tenant IS NOT NULL AND new_tenant IS DISTINCT FROM old_tenant THEN
        INSERT INTO system.search_authorization_revisions VALUES(new_tenant,1)
        ON CONFLICT(tenant_id) DO UPDATE
        SET version=system.search_authorization_revisions.version+1;
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER search_grant_revision AFTER INSERT OR UPDATE OR DELETE ON system.engine_access_grants
FOR EACH ROW EXECUTE FUNCTION system.advance_search_authorization_revision();
CREATE TRIGGER search_revocation_revision AFTER INSERT OR UPDATE OR DELETE ON system.engine_access_grant_revocations
FOR EACH ROW EXECUTE FUNCTION system.advance_search_authorization_revision();
CREATE TRIGGER search_engine_membership_revision AFTER INSERT OR DELETE ON system.engines
FOR EACH ROW EXECUTE FUNCTION system.advance_search_authorization_revision();
CREATE TRIGGER search_engine_revision AFTER UPDATE ON system.engines FOR EACH ROW
WHEN (ROW(OLD.id,OLD.tenant_id,OLD.engine_type,OLD.lifecycle_state,OLD.version,OLD.deleted_at)
 IS DISTINCT FROM ROW(NEW.id,NEW.tenant_id,NEW.engine_type,NEW.lifecycle_state,NEW.version,NEW.deleted_at))
EXECUTE FUNCTION system.advance_search_authorization_revision();

CREATE FUNCTION system.advance_truncated_search_authorization() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE system.search_authorization_generation SET generation=gen_random_uuid();
    RETURN NULL;
END $$;
CREATE TRIGGER search_grant_truncate AFTER TRUNCATE ON system.engine_access_grants
EXECUTE FUNCTION system.advance_truncated_search_authorization();
CREATE TRIGGER search_revocation_truncate AFTER TRUNCATE ON system.engine_access_grant_revocations
EXECUTE FUNCTION system.advance_truncated_search_authorization();
CREATE TRIGGER search_engine_truncate AFTER TRUNCATE ON system.engines
EXECUTE FUNCTION system.advance_truncated_search_authorization();
