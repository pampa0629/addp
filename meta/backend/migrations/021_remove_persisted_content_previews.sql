UPDATE meta.meta_item
SET attributes = attributes
    #- '{plain_text_preview}'
    #- '{text_excerpt}'
    #- '{capabilities,extraction,plain_text_preview}'
    #- '{capabilities,extraction,text_excerpt}'
WHERE attributes ? 'plain_text_preview'
   OR attributes ? 'text_excerpt'
   OR (attributes #> '{capabilities,extraction}') ? 'plain_text_preview'
   OR (attributes #> '{capabilities,extraction}') ? 'text_excerpt';
