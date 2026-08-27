import { createAPIClient } from '@common-ui'
import client, { refreshAuthorizationOnForbidden } from './client'
import { useAuthStore } from '../store/auth'

const modelClient = refreshAuthorizationOnForbidden(
  createAPIClient(() => useAuthStore(), { moduleName: 'Model' })
)

const PAGE_SIZE = 100

const listAll = async (apiClient, path, params = {}) => {
  const items = []
  for (let page = 1; ; page += 1) {
    const response = await apiClient.get(path, { params: { ...params, page, page_size: PAGE_SIZE } })
    const pageItems = Array.isArray(response) ? response : response?.data
    if (!Array.isArray(pageItems)) throw new TypeError(`Invalid list response from ${path}`)
    items.push(...pageItems)
    if (pageItems.length < PAGE_SIZE || (!Array.isArray(response) && response.total != null && items.length >= response.total)) return items
  }
}

// 跨模块: 引擎列表（System 模块）
export const systemEngineAPI = {
  list: (params) => client.get('/system/engines', { params })
}

export const systemCatalogAPI = {
  listChildren: (engineId, path = { segments: [] }, options = {}) => client.post(`/system/engines/${engineId}/catalog/children`, {
    path: {
      version: 'catalog.path/v1',
      engine_id: engineId,
      segments: Array.isArray(path?.segments) ? path.segments : []
    },
    options
  }),
  describeFacts: (engineId, path) => client.post(`/system/engines/${engineId}/catalog/facts`, {
    path: {
      version: 'catalog.path/v1',
      engine_id: engineId,
      segments: Array.isArray(path?.segments) ? path.segments : []
    }
  })
}

// 规则应用
export const ruleApplicationAPI = {
  list: (params) => client.get('/quality/rule-applications', { params }),
  listElementCandidates: (params) => client.get('/quality/rule-applications/element-candidates', { params }),
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

// Model 是物化组和逻辑表定义的唯一 owner；Quality 只读消费其 canonical API。
export const modelMaterializationAPI = {
  listGroups: (params) => listAll(modelClient, '/model/materialization-groups', params),
  getGroup: (id) => modelClient.get(`/model/materialization-groups/${id}`),
  listLogicalTables: (params) => listAll(modelClient, '/model/logical-tables', params),
  listLogicalTableFields: (id) => modelClient.get(`/model/logical-tables/${id}/fields`)
}

export const materializationGateAPI = {
  list: (params) => client.get('/quality/materialization-gate-tasks', { params }),
  get: (id) => client.get(`/quality/materialization-gate-tasks/${id}`),
  create: (data) => client.post('/quality/materialization-gate-tasks', data),
  update: (id, data) => client.put(`/quality/materialization-gate-tasks/${id}`, data),
  delete: (id, version) => client.delete(`/quality/materialization-gate-tasks/${id}`, { data: { version } })
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
