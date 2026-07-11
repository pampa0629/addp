export const isSparkWorkflowEngine = (engine) => engine?.engine_type === 'spark_workflow'

const PLATFORM_INTERNAL_PARAMETER_NAMES = new Set(['engine_id', 'connection_info'])

export const isStandardWorkflowDefinition = (workflow) => {
  if (!workflow || !Array.isArray(workflow.tasks) || workflow.tasks.length === 0) {
    return false
  }
  const hasValidShape = workflow.tasks.every((task) => (
    task &&
    typeof task.id === 'string' &&
    task.id.trim() &&
    typeof task.operator === 'string' &&
    task.operator.trim() &&
    task.params &&
    typeof task.params === 'object' &&
    !Array.isArray(task.params) &&
    Object.keys(task.params).every((name) => !PLATFORM_INTERNAL_PARAMETER_NAMES.has(name)) &&
    Array.isArray(task.depends_on) &&
    task.depends_on.every((dep) => typeof dep === 'string')
  ))
  if (!hasValidShape) {
    return false
  }

  const taskIds = new Set()
  for (const task of workflow.tasks) {
    if (taskIds.has(task.id)) {
      return false
    }
    taskIds.add(task.id)
  }

  for (const task of workflow.tasks) {
    for (const dep of task.depends_on) {
      if (dep === task.id || !taskIds.has(dep)) {
        return false
      }
    }
  }

  return !hasDependencyCycle(workflow.tasks)
}

const hasDependencyCycle = (tasks) => {
  const dependencies = new Map(tasks.map((task) => [task.id, task.depends_on]))
  const visiting = new Set()
  const visited = new Set()

  const visit = (taskId) => {
    if (visiting.has(taskId)) return true
    if (visited.has(taskId)) return false

    visiting.add(taskId)
    for (const depId of dependencies.get(taskId) || []) {
      if (visit(depId)) return true
    }
    visiting.delete(taskId)
    visited.add(taskId)
    return false
  }

  return tasks.some((task) => visit(task.id))
}

export const buildWorkflowExecutionConfig = ({
  workflowEngineId,
  sparkRuntimeId = null,
  requiresSparkRuntime = false
}) => {
  const config = {
    type: 'workflow',
    engine_id: workflowEngineId
  }

  if (requiresSparkRuntime) {
    config.engine_specific = {
      spark_cluster_id: sparkRuntimeId
    }
  }

  return config
}

export const buildWorkflowContent = ({ workflow, inputs = {} }) => {
  assertStandardWorkflowDefinition(workflow)
  return {
    workflow_definition: workflow,
    inputs
  }
}

export const buildWorkflowDevTaskPayload = ({
  name,
  displayName = '',
  description = '',
  workflow,
  inputs = {},
  workflowEngineId,
  sparkRuntimeId = null,
  requiresSparkRuntime = false,
  includeDevType = true
}) => {
  const payload = {
    name,
    display_name: displayName,
    description,
    execution_config: buildWorkflowExecutionConfig({
      workflowEngineId,
      sparkRuntimeId,
      requiresSparkRuntime
    }),
    content: buildWorkflowContent({ workflow, inputs })
  }

  if (includeDevType) {
    payload.dev_type = 'workflow'
  }

  return payload
}

export const buildWorkflowExportPayload = ({
  workflow,
  workflowEngineId,
  sparkRuntimeId = null,
  requiresSparkRuntime = false
}) => {
  assertStandardWorkflowDefinition(workflow)
  return {
    workflow_definition: workflow,
    execution_config: buildWorkflowExecutionConfig({
      workflowEngineId,
      sparkRuntimeId,
      requiresSparkRuntime
    })
  }
}

const assertStandardWorkflowDefinition = (workflow) => {
  if (!isStandardWorkflowDefinition(workflow)) {
    throw new Error('workflow_definition must contain standard tasks with id, operator, params, and depends_on')
  }
}
