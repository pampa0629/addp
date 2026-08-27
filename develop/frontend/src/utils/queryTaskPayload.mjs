export function toQueryDevTaskPayload(taskData, includeDevType = true) {
  const payload = {
    name: taskData.name,
    display_name: taskData.display_name,
    content: {
      query: taskData.query,
      query_type: taskData.query_type || 'sql',
      target_locator: taskData.target_locator || undefined,
      query_parameters: Array.isArray(taskData.query_parameters) ? taskData.query_parameters : [],
      relation_inputs: Array.isArray(taskData.relation_inputs) && taskData.relation_inputs.length > 0
        ? taskData.relation_inputs
        : undefined
    },
    execution_config: { engine_id: taskData.engine_id },
    timeout: taskData.timeout,
    description: taskData.description,
    tags: taskData.tags
  }
  if (includeDevType) payload.dev_type = 'query'
  return payload
}
