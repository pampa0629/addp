import client from './client'

// 查询执行记录
export function listExecutions(params) {
  return client.get('/monitor/executions', { params })
}

// 获取单条执行记录
export function getExecution(id) {
  return client.get(`/monitor/executions/${id}`)
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
