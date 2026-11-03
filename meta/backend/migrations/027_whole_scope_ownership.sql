-- One-way normalization. No old ownership is accepted by the runtime.
-- Prove coordinates from the persisted parent chain and independent names.
CREATE TEMP TABLE whole_nodes_027 AS
WITH RECURSIVE nodes AS (
    SELECT n.id, n.tenant_id, n.engine_id, n.parent_node_id, n.node_type,
           n.name, n.full_name, n.id::text AS walk_path, 0 AS walk_depth,
           ''::text AS bucket_name
    FROM meta.meta_node n
    WHERE n.deleted_at IS NULL AND n.parent_node_id IS NULL
      AND n.node_type IN ('root','service') AND n.full_name = ''
      AND 1 = (SELECT count(*) FROM meta.meta_node r
               WHERE r.tenant_id=n.tenant_id AND r.engine_id=n.engine_id
                 AND r.parent_node_id IS NULL AND r.deleted_at IS NULL)
    UNION ALL
    SELECT n.id, n.tenant_id, n.engine_id, n.parent_node_id, n.node_type,
           n.name, n.full_name, p.walk_path || '/' || n.id, p.walk_depth+1,
           CASE WHEN n.node_type='bucket' THEN n.name ELSE p.bucket_name END
    FROM meta.meta_node n JOIN nodes p ON n.parent_node_id=p.id
      AND n.tenant_id=p.tenant_id AND n.engine_id=p.engine_id
    WHERE n.deleted_at IS NULL AND p.walk_depth < 62
      AND n.name <> '' AND n.name NOT IN ('.','..')
      AND position('/' IN n.name)=0 AND position(chr(92) IN n.name)=0
      AND n.full_name = CASE WHEN p.full_name='' THEN n.name ELSE p.full_name || '/' || n.name END
      AND ((n.node_type='dir' AND p.node_type IN ('root','dir'))
        OR (n.node_type='bucket' AND p.node_type='service')
        OR (n.node_type='prefix' AND p.node_type IN ('bucket','prefix')))
      AND 1 = (SELECT count(*) FROM meta.meta_node sibling
               WHERE sibling.tenant_id=n.tenant_id AND sibling.engine_id=n.engine_id
                 AND sibling.node_type=n.node_type AND sibling.full_name=n.full_name
                 AND sibling.deleted_at IS NULL)
)
SELECT * FROM nodes;

CREATE TEMP TABLE whole_owners_027 (item_id bigint PRIMARY KEY, node_id bigint NOT NULL);
DO $$
DECLARE
    item record;
    owner record;
    scope meta.meta_node%ROWTYPE;
    scope_count integer;
    scope_id bigint;
    scope_type text;
BEGIN
    FOR item IN
        SELECT i.*, count(*) OVER (PARTITION BY tenant_id,engine_id,full_name) AS same_path_count
        FROM meta.meta_item i
        WHERE i.deleted_at IS NULL AND i.attributes->'item'->>'layout'='whole'
        ORDER BY i.id
    LOOP
        IF item.item_type NOT IN ('file','object') OR item.name IS NULL OR item.name=''
           OR item.name IN ('.','..') OR position('/' IN item.name)>0 OR position(chr(92) IN item.name)>0
           OR item.full_name IS NULL OR octet_length(item.full_name)>4096 OR item.same_path_count<>1
           OR item.fingerprint IS DISTINCT FROM encode(sha256(convert_to(item.engine_id || ':' || item.full_name,'UTF8')),'hex')
           OR item.attributes->'item'->'scope_exclusive' IS DISTINCT FROM 'true'::jsonb
           OR item.attributes->'item'->>'claim_policy' IS DISTINCT FROM 'whole_scope'
           OR COALESCE(item.attributes->'item'->>'format','')=''
           OR COALESCE(item.attributes->'item'->>'data_type','')=''
           OR item.attributes->'storage'->>'physical_path' IS DISTINCT FROM item.full_name
           OR item.attributes->'item' ? 'refs' THEN
            CONTINUE;
        END IF;
        SELECT * INTO owner FROM whole_nodes_027
        WHERE id=item.node_id AND tenant_id=item.tenant_id AND engine_id=item.engine_id;
        IF NOT FOUND THEN CONTINUE; END IF;
        IF owner.name=item.name AND owner.full_name=item.full_name
           AND ((item.item_type='file' AND owner.node_type='dir')
             OR (item.item_type='object' AND owner.node_type='prefix')) THEN
            INSERT INTO whole_owners_027 VALUES (item.id,owner.id);
        ELSIF ((item.item_type='object' AND owner.node_type IN ('bucket','prefix'))
              OR (item.item_type='file' AND owner.node_type IN ('root','dir')))
              AND item.full_name=(CASE WHEN owner.full_name='' THEN item.name ELSE owner.full_name || '/' || item.name END)
              AND owner.walk_depth<62 THEN
            scope_type := CASE WHEN item.item_type='object' THEN 'prefix' ELSE 'dir' END;
            -- Reuse only an unambiguous scope whose actual parent is proved.
            SELECT count(*) INTO scope_count FROM meta.meta_node n
            WHERE n.tenant_id=item.tenant_id AND n.engine_id=item.engine_id
              AND n.node_type=scope_type AND n.full_name=item.full_name;
            IF scope_count>1 THEN CONTINUE; END IF;
            IF scope_count=1 THEN
                SELECT * INTO scope FROM meta.meta_node n
                WHERE n.tenant_id=item.tenant_id AND n.engine_id=item.engine_id
                  AND n.node_type=scope_type AND n.full_name=item.full_name;
                IF scope.parent_node_id IS DISTINCT FROM owner.id OR scope.name<>item.name THEN CONTINUE; END IF;
                scope_id := scope.id;
                UPDATE meta.meta_node SET deleted_at=NULL,
                    path=owner.walk_path || '/' || scope_id, depth=owner.walk_depth+1
                WHERE id=scope_id;
            ELSE
                INSERT INTO meta.meta_node (tenant_id,engine_id,parent_node_id,node_type,name,full_name,
                    depth,path,scan_status,scanned_depth,attributes,created_at)
                VALUES (item.tenant_id,item.engine_id,owner.id,scope_type,item.name,item.full_name,
                    owner.walk_depth+1,'','pending','none',jsonb_build_object('schema_version',1,
                    'storage',CASE WHEN scope_type='prefix' THEN jsonb_build_object('bucket',owner.bucket_name,
                        'path',substr(item.full_name,length(owner.bucket_name)+2) || '/')
                        ELSE jsonb_build_object('path',item.full_name) END),now())
                RETURNING id INTO scope_id;
                UPDATE meta.meta_node SET path=owner.walk_path || '/' || scope_id WHERE id=scope_id;
            END IF;
            UPDATE meta.meta_item SET node_id=scope_id WHERE id=item.id;
            INSERT INTO whole_owners_027 VALUES (item.id,scope_id);
        END IF;
    END LOOP;
END $$;

-- Malformed whole facts are removed, never guessed or retained as a second route.
-- Existing item-change triggers publish missing/upsert facts to their consumers.
CREATE TEMP TABLE whole_invalid_nodes_027 AS
WITH RECURSIVE invalid(id,tenant_id,engine_id) AS (
    SELECT n.id,n.tenant_id,n.engine_id
    FROM meta.meta_item i JOIN meta.meta_node n ON n.id=i.node_id
      AND n.tenant_id=i.tenant_id AND n.engine_id=i.engine_id
      AND n.name=i.name AND n.full_name=i.full_name
      AND ((i.item_type='object' AND n.node_type='prefix') OR (i.item_type='file' AND n.node_type='dir'))
    WHERE i.deleted_at IS NULL AND i.attributes->'item'->>'layout'='whole'
      AND NOT EXISTS (SELECT 1 FROM whole_owners_027 k WHERE k.item_id=i.id)
    UNION
    SELECT n.id,n.tenant_id,n.engine_id FROM meta.meta_node n JOIN invalid p
      ON n.parent_node_id=p.id AND n.tenant_id=p.tenant_id AND n.engine_id=p.engine_id
)
SELECT * FROM invalid;
DELETE FROM meta.meta_item i USING whole_invalid_nodes_027 n
WHERE i.node_id=n.id AND i.tenant_id=n.tenant_id AND i.engine_id=n.engine_id;
DELETE FROM meta.meta_node n USING whole_invalid_nodes_027 d WHERE n.id=d.id;
DELETE FROM meta.meta_item i
WHERE i.deleted_at IS NULL AND i.attributes->'item'->>'layout'='whole'
  AND NOT EXISTS (SELECT 1 FROM whole_owners_027 k WHERE k.item_id=i.id);

CREATE TEMP TABLE whole_members_027 AS
WITH RECURSIVE members(item_id,root_id,id,tenant_id,engine_id) AS (
    SELECT k.item_id,k.node_id,n.id,n.tenant_id,n.engine_id
    FROM whole_owners_027 k JOIN meta.meta_node n ON n.id=k.node_id
      JOIN meta.meta_item i ON i.id=k.item_id
    UNION
    SELECT m.item_id,m.root_id,n.id,n.tenant_id,n.engine_id
    FROM members m JOIN meta.meta_node n ON n.parent_node_id=m.id
      AND n.tenant_id=m.tenant_id AND n.engine_id=m.engine_id
)
SELECT * FROM members;
DELETE FROM meta.meta_item i USING whole_members_027 m
WHERE i.node_id=m.id AND i.tenant_id=m.tenant_id AND i.engine_id=m.engine_id AND i.id<>m.item_id;
DELETE FROM meta.meta_node n USING whole_members_027 m WHERE n.id=m.id AND m.id<>m.root_id;
DROP TABLE whole_members_027,whole_invalid_nodes_027,whole_owners_027,whole_nodes_027;
