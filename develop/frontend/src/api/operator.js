import client from './client'

// 按工作流运行时引擎实例获取算子
export const listOperatorsByWorkflowEngine = (workflowEngineId) => client.get(`/develop/workflow-engines/${workflowEngineId}/operators`)

// 按目标运行时的 Public Operator Spec 校验候选工作流
export const validateWorkflowDefinition = (workflowEngineId, workflowDefinition) => client.post('/develop/workflow-validations', {
  workflow_engine_id: workflowEngineId,
  workflow_definition: workflowDefinition
})
