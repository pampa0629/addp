import client from './client'

// 查询执行记录
export function listExecutions(params) {
  return client.get('/monitor/executions', { params })
}

// 获取单条执行记录
export function getExecution(id) {
  return client.get(`/monitor/executions/${id}`)
}

// 按 execution_id 获取单条执行记录
export function getExecutionByExecutionID(executionId) {
  return client.get(`/monitor/executions/by-execution-id/${executionId}`)
}

// 获取执行记录树
export function getExecutionTree(id) {
  return client.get(`/monitor/executions/${id}/tree`)
}

// 按 execution_id 获取执行记录树
export function getExecutionTreeByExecutionID(executionId) {
  return client.get(`/monitor/executions/by-execution-id/${executionId}/tree`)
}

// 获取统计数据
export function getStatistics(params) {
  return client.get('/monitor/executions/stats', { params })
}

// 获取趋势数据
export function getTrendData(params) {
  return client.get('/monitor/executions/trend', { params })
}

// 获取所有模块列表
export function listModules() {
  return client.get('/monitor/modules')
}

// 检查单个模块健康状态
export function checkModuleHealth(module) {
  return client.get(`/monitor/modules/${module}/health`)
}

// 检查所有模块健康状态
export function checkAllModules() {
  return client.get('/monitor/modules/health/all')
}

// 检查所有任务提供者运行态健康状态
export function checkAllProviderHealth() {
  return client.get('/monitor/providers/health')
}

// 获取所有任务提供者
export function listTaskProviders() {
  return client.get('/monitor/task-providers')
}
