-- Clean break: Develop task content only keeps the canonical task fields.
-- query: content.query + content.query_type
-- workflow: content.workflow_definition + content.inputs
-- script: content.notebook_path for Notebook-backed script tasks

UPDATE develop.dev_tasks
SET dev_type = 'script',
    updated_at = NOW()
WHERE dev_type = 'notebook';

UPDATE common.task_executions
SET task_type = 'script',
    updated_at = NOW()
WHERE module = 'develop'
  AND task_type = 'notebook';

UPDATE develop.dev_tasks
SET content = jsonb_set(content - 'sql', '{query}', content->'sql', true),
    updated_at = NOW()
WHERE dev_type = 'query'
  AND content ? 'sql'
  AND NOT content ? 'query';

UPDATE develop.dev_tasks
SET content = content - 'sql',
    updated_at = NOW()
WHERE dev_type = 'query'
  AND content ? 'sql';

UPDATE develop.dev_tasks
SET content = jsonb_set(content, '{query_type}', '"sql"'::jsonb, true),
    updated_at = NOW()
WHERE dev_type = 'query'
  AND NOT content ? 'query_type';

UPDATE develop.dev_tasks
SET content = jsonb_set(content - 'workflow_def', '{workflow_definition}', content->'workflow_def', true),
    updated_at = NOW()
WHERE dev_type = 'workflow'
  AND content ? 'workflow_def'
  AND NOT content ? 'workflow_definition';

UPDATE develop.dev_tasks
SET content = content - 'workflow_def',
    updated_at = NOW()
WHERE dev_type = 'workflow'
  AND content ? 'workflow_def';

UPDATE develop.dev_tasks
SET content = jsonb_set(
        content - 'nodes' - 'edges',
        '{workflow_definition}',
        jsonb_build_object(
            'nodes', COALESCE(content->'nodes', '[]'::jsonb),
            'edges', COALESCE(content->'edges', '[]'::jsonb)
        ),
        true
    ),
    updated_at = NOW()
WHERE dev_type = 'workflow'
  AND NOT content ? 'workflow_definition'
  AND (content ? 'nodes' OR content ? 'edges');

UPDATE develop.dev_tasks
SET content = jsonb_set(content - 'input_data', '{inputs}', content->'input_data', true),
    updated_at = NOW()
WHERE content ? 'input_data'
  AND NOT content ? 'inputs';

UPDATE develop.dev_tasks
SET content = content - 'input_data',
    updated_at = NOW()
WHERE content ? 'input_data';

DELETE FROM develop.dev_tasks
WHERE dev_type NOT IN ('query', 'workflow', 'script');
