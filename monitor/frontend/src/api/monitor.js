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

export function listAlerts(params) {
  return client.get('/monitor/alerts', { params })
}

export function acknowledgeAlert(id) {
  return client.post(`/monitor/alerts/${id}/acknowledge`)
}

export function suppressAlert(id, suppressedUntil) {
  return client.post(`/monitor/alerts/${id}/suppress`, { suppressed_until: suppressedUntil })
}

export function listAlertRuleTargets() {
  return client.get('/monitor/alert-rule-targets')
}

export function listAlertRules() {
  return client.get('/monitor/alert-rules')
}

export function createAlertRule(payload) {
  return client.post('/monitor/alert-rules', payload)
}

export function updateAlertRule(id, payload) {
  return client.patch(`/monitor/alert-rules/${id}`, payload)
}

export function deleteAlertRule(id) {
  return client.delete(`/monitor/alert-rules/${id}`)
}

export function listWebhookDestinations() {
  return client.get('/monitor/webhook-destinations')
}

export function createWebhookDestination(payload) {
  return client.post('/monitor/webhook-destinations', payload)
}

export function updateWebhookDestination(id, payload) {
  return client.patch(`/monitor/webhook-destinations/${id}`, payload)
}

export function testWebhookDestination(id) {
  return client.post(`/monitor/webhook-destinations/${id}/test`)
}

export function deleteWebhookDestination(id) {
  return client.delete(`/monitor/webhook-destinations/${id}`)
}

export function listWebhookDeliveries(params) {
  return client.get('/monitor/webhook-deliveries', { params })
}

export function retryWebhookDelivery(deliveryId) {
  return client.post(`/monitor/webhook-deliveries/${deliveryId}/retry`)
}

export function listEmailDestinations() {
  return client.get('/monitor/email-destinations')
}

export function createEmailDestination(payload) {
  return client.post('/monitor/email-destinations', payload)
}

export function updateEmailDestination(id, payload) {
  return client.patch(`/monitor/email-destinations/${id}`, payload)
}

export function testEmailDestination(id) {
  return client.post(`/monitor/email-destinations/${id}/test`)
}

export function deleteEmailDestination(id) {
  return client.delete(`/monitor/email-destinations/${id}`)
}

export function listEmailDeliveries(params) {
  return client.get('/monitor/email-deliveries', { params })
}

export function retryEmailDelivery(deliveryId) {
  return client.post(`/monitor/email-deliveries/${deliveryId}/retry`)
}
