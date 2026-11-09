BEGIN;

-- Admit the exact standalone GeoPackage file boundary without rewriting Grants.
CREATE OR REPLACE FUNCTION system.valid_source_file_composition(composition jsonb, primary_path jsonb, engine bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
DECLARE ref jsonb; parent jsonb; prefix jsonb; primary_count integer := 0; total integer;
BEGIN
 IF NOT (system.valid_source_file_path(primary_path, engine) OR system.valid_source_object_path(primary_path, engine)) OR jsonb_typeof(composition) <> 'object'
   OR composition - ARRAY['format','refs'] <> '{}'::jsonb OR composition->>'format' IS NULL OR composition->>'format' NOT IN ('ifc','3ds','dae','obj','stl','glb','gltf','fbx','envi','geopackage')
   OR jsonb_typeof(composition->'refs') IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 total := jsonb_array_length(composition->'refs');
 IF total < 1 OR total > 200 OR (composition->>'format' IN ('ifc','stl','glb','fbx','geopackage') AND total <> 1) THEN RETURN false; END IF;
 IF composition->>'format'='geopackage' AND NOT system.valid_source_file_path(primary_path, engine) THEN RETURN false; END IF;
 SELECT jsonb_agg(value ORDER BY ordinality) INTO parent
 FROM jsonb_array_elements(primary_path->'segments') WITH ORDINALITY
 WHERE ordinality < jsonb_array_length(primary_path->'segments');
 FOR ref IN SELECT value FROM jsonb_array_elements(composition->'refs') LOOP
  IF jsonb_typeof(ref) <> 'object' OR ref - ARRAY['target','role','required','primary'] <> '{}'::jsonb
    OR ref->>'role' IS NULL OR ref->>'role' !~ '^[a-z0-9_]{1,64}$'
    OR jsonb_typeof(ref->'required') IS DISTINCT FROM 'boolean' OR jsonb_typeof(ref->'primary') IS DISTINCT FROM 'boolean'
    OR NOT (system.valid_source_file_path(ref->'target', engine) OR system.valid_source_object_path(ref->'target', engine))
    OR ref->'target'->'segments'->-1->>'term' IS DISTINCT FROM primary_path->'segments'->-1->>'term'
    OR jsonb_array_length(ref->'target'->'segments') < jsonb_array_length(primary_path->'segments') THEN RETURN false; END IF;
  SELECT jsonb_agg(value ORDER BY ordinality) INTO prefix
  FROM jsonb_array_elements(ref->'target'->'segments') WITH ORDINALITY
  WHERE ordinality < jsonb_array_length(primary_path->'segments');
  IF prefix IS DISTINCT FROM parent THEN RETURN false; END IF;
  IF composition->>'format'='envi' AND (total<>2 OR ref->'required'<>'true'::jsonb
    OR jsonb_array_length(ref->'target'->'segments')<>jsonb_array_length(primary_path->'segments')
    OR (ref->'primary'='true'::jsonb AND (ref->>'role'<>'raster' OR lower(ref->'target'->'segments'->-1->>'name') ~ '\.hdr$'))
    OR (ref->'primary'='false'::jsonb AND (ref->>'role'<>'header' OR lower(ref->'target'->'segments'->-1->>'name') !~ '\.hdr$'))) THEN RETURN false; END IF;
  IF composition->>'format'='gltf' AND (ref->'required'<>'true'::jsonb OR
    (ref->'primary'='true'::jsonb AND ref->>'role'<>'manifest') OR
    (ref->'primary'='false'::jsonb AND ref->>'role' !~ '^(buffer|image)(_[1-9][0-9]*)?$')) THEN RETURN false; END IF;
  IF ref->'primary' = 'true'::jsonb THEN
   primary_count := primary_count + 1;
   IF ref->'required' <> 'true'::jsonb OR ref->'target' IS DISTINCT FROM primary_path
     OR (composition->>'format' IN ('glb','fbx') AND ref->>'role' IS DISTINCT FROM 'model')
     OR (composition->>'format'='geopackage' AND ref->>'role' IS DISTINCT FROM 'container') THEN RETURN false; END IF;
  END IF;
 END LOOP;
 IF composition->>'format'='gltf' AND (SELECT count(DISTINCT value->>'role') <> total FROM jsonb_array_elements(composition->'refs')) THEN RETURN false; END IF;
 RETURN primary_count = 1 AND (SELECT count(DISTINCT value->'target') = total FROM jsonb_array_elements(composition->'refs'));
END;
$$;

CREATE OR REPLACE FUNCTION system.guard_engine_access_grant_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE
    receipt system.engine_access_fulfillment_outcomes%ROWTYPE;
BEGIN
    NEW.granted_at := clock_timestamp();
    IF NEW.approval_mode = 'catalog' THEN
        SELECT * INTO receipt FROM system.engine_access_fulfillment_outcomes
        WHERE request_id = NEW.request_id AND outcome = 'accepted';
        IF receipt.deadline IS NULL OR NEW.granted_at < receipt.recorded_at OR NEW.granted_at >= receipt.deadline THEN
            RAISE EXCEPTION 'engine access grant requires an unexpired accepted fulfillment'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_acceptance_window';
        END IF;
        -- Rule facts come from the trusted receipt, never caller overrides.
        NEW.catalog_request_id := receipt.request_id;
        NEW.item_fingerprint := receipt.binding->>'item_fingerprint';
        NEW.boundary_hash := receipt.binding->>'boundary_hash';
        NEW.tenant_id := receipt.tenant_id; NEW.engine_id := receipt.engine_id; NEW.catalog_path := receipt.catalog_path;
        NEW.recipient_type := receipt.binding->>'recipient_type'; NEW.recipient_id := (receipt.binding->>'recipient_id')::bigint;
        NEW.action := receipt.binding->>'action'; NEW.expiry_mode := receipt.expiry_mode; NEW.expires_at := receipt.grant_expires_at;
        NEW.operator_principal_id := (receipt.binding->'operator'->>'principal_id')::bigint;
        NEW.operator_membership_id := (receipt.binding->'operator'->>'tenant_membership_id')::bigint;
        NEW.operator_authorization_version := (receipt.binding->'operator'->>'authorization_version')::bigint;
        NEW.reason := NULL;
    ELSIF NEW.approval_mode = 'independent' THEN
        -- Current IAM, engine scope and boundary are enforced by the canonical command.
        IF NOT EXISTS (SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id = m.principal_id
            JOIN system.tenants t ON t.id = m.tenant_id
            JOIN system.engines e ON e.tenant_id = t.id AND e.id = NEW.engine_id
            WHERE m.id = NEW.operator_membership_id AND p.id = NEW.operator_principal_id AND m.tenant_id = NEW.tenant_id
                AND p.principal_type = 'user' AND p.status = 'active' AND p.authorization_version = NEW.operator_authorization_version
                AND m.status = 'active' AND m.joined_at <= NEW.granted_at AND (m.expires_at IS NULL OR m.expires_at > NEW.granted_at)
                AND t.status = 'active' AND e.lifecycle_state = 'active') THEN
            RAISE EXCEPTION 'independent grant actor must be current in the source tenant'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_actor';
        END IF;
        IF NOT ((NEW.recipient_type = 'user' AND EXISTS (
            SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id = m.principal_id
            WHERE m.tenant_id = NEW.tenant_id AND p.id = NEW.recipient_id AND p.principal_type = 'user'
                AND p.status = 'active' AND m.status = 'active' AND m.joined_at <= NEW.granted_at
                AND (m.expires_at IS NULL OR m.expires_at > NEW.granted_at)))
            OR (NEW.recipient_type = 'department' AND EXISTS (SELECT 1 FROM system.departments d
                WHERE d.tenant_id = NEW.tenant_id AND d.id = NEW.recipient_id AND d.status = 'active'))
            OR (NEW.recipient_type = 'project_group' AND EXISTS (SELECT 1 FROM system.project_groups g
                WHERE g.tenant_id = NEW.tenant_id AND g.id = NEW.recipient_id AND g.status = 'active'))) THEN
            RAISE EXCEPTION 'independent grant recipient must be current in the source tenant'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_recipient';
        END IF;
    END IF;
    IF NEW.file_composition->>'format' = 'geopackage' AND NOT EXISTS (
        SELECT 1 FROM system.engines e WHERE e.id = NEW.engine_id AND e.tenant_id = NEW.tenant_id
            AND e.engine_type = 'nfs' AND e.lifecycle_state = 'active'
    ) THEN
        RAISE EXCEPTION 'GeoPackage source grant requires an active NFS engine'
            USING ERRCODE = '23514', CONSTRAINT = 'source_grant_geopackage_nfs';
    END IF;
    RETURN NEW;
END;
$$;

COMMIT;
