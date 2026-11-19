UPDATE orchestrator.orchestrations
SET editor_layout = editor_layout - 'viewport'
WHERE jsonb_typeof(editor_layout) = 'object' AND jsonb_exists(editor_layout, 'viewport');
