BEGIN;

-- A Meta-proven native single object uses the existing exact catalog identity
-- and boundary hash. It is not a node scope and never covers sibling objects.
ALTER TABLE system.engine_access_grants
 DROP CONSTRAINT source_grant_file_composition,
 ADD CONSTRAINT source_grant_file_composition CHECK ((CASE
 WHEN metadata_target IS NOT NULL THEN file_composition IS NULL
 WHEN system.valid_source_object_path(catalog_path,engine_id) AND file_composition IS NULL
 THEN approval_mode='independent'
 WHEN system.valid_source_file_path(catalog_path,engine_id) OR system.valid_source_object_path(catalog_path,engine_id)
 THEN approval_mode='independent' AND file_composition IS NOT NULL
   AND system.valid_source_file_composition(file_composition,catalog_path,engine_id)
 ELSE file_composition IS NULL END) IS TRUE);

COMMIT;
