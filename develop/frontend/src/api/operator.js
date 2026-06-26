import client from './client'

// 按工作流运行时引擎实例获取算子
export const listOperatorsByWorkflowEngine = (workflowEngineId) => client.get(`/develop/workflow-engines/${workflowEngineId}/operators`)
