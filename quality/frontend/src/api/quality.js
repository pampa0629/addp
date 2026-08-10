import client from './client'

// 跨模块: 数据元列表（Standard 模块）
export const standardElementAPI = {
  list: (params) => client.get('/standard/elements', { params })
}

// 跨模块: 引擎列表（System 模块）
export const systemEngineAPI = {
  list: (params) => client.get('/system/engines', { params })
}

// 规则应用
export const ruleApplicationAPI = {
  list: (params) => client.get('/quality/rule-applications', { params }),
  get: (id) => client.get(`/quality/rule-applications/${id}`),
  create: (data) => client.post('/quality/rule-applications', data),
  update: (id, data) => client.put(`/quality/rule-applications/${id}`, data),
  delete: (id) => client.delete(`/quality/rule-applications/${id}`)
}

// 检查任务
export const checkTaskAPI = {
  list: (params) => client.get('/quality/check-tasks', { params }),
  get: (id) => client.get(`/quality/check-tasks/${id}`),
  create: (data) => client.post('/quality/check-tasks', data),
  update: (id, data) => client.put(`/quality/check-tasks/${id}`, data),
  delete: (id) => client.delete(`/quality/check-tasks/${id}`),
  run: (id) => client.post(`/quality/check-tasks/${id}/run`)
}

// 执行记录
export const executionAPI = {
  list: (params) => client.get('/quality/executions', { params }),
  get: (id) => client.get(`/quality/executions/${id}`)
}

// 问题工单
export const issueAPI = {
  list: (params) => client.get('/quality/issues', { params }),
  get: (id) => client.get(`/quality/issues/${id}`),
  updateStatus: (id, status, note) => client.put(`/quality/issues/${id}/status`, { status, note })
}
