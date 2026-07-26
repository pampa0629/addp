-- Historical row_count values mixed exact counts with catalog estimates and
-- cannot be classified reliably. Clear them once and let subsequent scans
-- repopulate row_count / estimated_row_count under the explicit contract.
UPDATE meta.meta_item
SET row_count = NULL,
    attributes = (COALESCE(attributes, '{}'::jsonb)
        #- '{type_info,table,row_count}'
        #- '{capabilities,statistics,row_count}')
WHERE row_count IS NOT NULL
   OR attributes #> '{type_info,table,row_count}' IS NOT NULL
   OR attributes #> '{capabilities,statistics,row_count}' IS NOT NULL;

-- Removing the obsolete facts can leave empty standard sections behind.
UPDATE meta.meta_item
SET attributes = attributes #- '{capabilities,statistics}'
WHERE attributes #> '{capabilities,statistics}' = '{}'::jsonb;

UPDATE meta.meta_item
SET attributes = attributes - 'capabilities'
WHERE attributes #> '{capabilities}' = '{}'::jsonb;

UPDATE meta.meta_item
SET attributes = attributes #- '{type_info,table}'
WHERE attributes #> '{type_info,table}' = '{}'::jsonb;

UPDATE meta.meta_item
SET attributes = attributes - 'type_info'
WHERE attributes #> '{type_info}' = '{}'::jsonb;
