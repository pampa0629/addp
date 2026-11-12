BEGIN;

-- Extend the applied TIFF composition validator with registered external masks.
-- Migration 285 and its recorded checksum remain immutable.

CREATE OR REPLACE FUNCTION system.valid_source_file_composition(composition jsonb, primary_path jsonb, engine bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
DECLARE ref jsonb; parent jsonb; prefix jsonb; primary_count integer := 0; total integer;
BEGIN
 IF NOT (system.valid_source_file_path(primary_path, engine) OR system.valid_source_object_path(primary_path, engine)) OR jsonb_typeof(composition) <> 'object'
   OR composition - (CASE WHEN composition->>'format'='tiff' THEN ARRAY['format','refs','source_layout'] ELSE ARRAY['format','refs'] END) <> '{}'::jsonb
   OR composition->>'format' IS NULL OR composition->>'format' NOT IN ('ifc','3ds','dae','obj','stl','glb','gltf','fbx','envi','geopackage','tiff','geojson')
   OR jsonb_typeof(composition->'refs') IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 total := jsonb_array_length(composition->'refs');
 IF composition->>'format'='geojson' AND (total<>1 OR lower(primary_path->'segments'->-1->>'name') !~ '\.geojson$') THEN RETURN false; END IF;
 IF total < 1 OR total > 200 OR (composition->>'format' IN ('ifc','stl','glb','fbx','geopackage') AND total <> 1) THEN RETURN false; END IF;
 IF composition->>'format'='geopackage' AND NOT system.valid_source_file_path(primary_path, engine) THEN RETURN false; END IF;
 IF composition->>'format'='tiff' AND (
   (composition->>'source_layout' = CASE WHEN total=1 THEN 'single' ELSE 'multi' END) IS NOT TRUE
   OR lower(primary_path->'segments'->-1->>'name') !~ '\.tiff?$') THEN RETURN false; END IF;
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
  IF composition->>'format'='tiff' AND (
    jsonb_array_length(ref->'target'->'segments')<>jsonb_array_length(primary_path->'segments')
    OR (ref->'primary'='false'::jsonb AND ref->>'role' NOT IN ('world_file','crs','header','auxiliary_metadata','overview','mask'))) THEN RETURN false; END IF;
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
     OR (composition->>'format'='geopackage' AND ref->>'role' IS DISTINCT FROM 'container')
     OR (composition->>'format'='geojson' AND ref->>'role' IS DISTINCT FROM 'data')
     OR (composition->>'format'='tiff' AND ref->>'role' IS DISTINCT FROM 'image') THEN RETURN false; END IF;
  END IF;
 END LOOP;
 IF composition->>'format'='gltf' AND (SELECT count(DISTINCT value->>'role') <> total FROM jsonb_array_elements(composition->'refs')) THEN RETURN false; END IF;
 RETURN primary_count = 1 AND (SELECT count(DISTINCT value->'target') = total FROM jsonb_array_elements(composition->'refs'));
END;
$$;

COMMIT;
