import client from './client'

export const overviewAPI = { get: params => client.get('/quality/overview', { params }) }

// Read-only provenance via the current user's Standard permission, never a write payload.
export const standardSourceAPI = {
  getRevision: (elementID, revisionID) => client.get(`/standard/elements/${elementID}/revisions/${revisionID}`)
}

export const standardDomainAPI = {
  list: () => client.get('/standard/domains')
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

export const planAPI = {
  run: (id, parameters) => client.post(`/quality/plans/${id}/run`, parameters),
  list: (params) => client.get('/quality/plans', { params }),
  get: (id) => client.get(`/quality/plans/${id}`),
  create: (data) => client.post('/quality/plans', data),
  update: (id, data) => client.put(`/quality/plans/${id}`, data),
  delete: (id, version) => client.delete(`/quality/plans/${id}`, { data: { version } })
}

export const ruleAPI = {
  list: params => client.get('/quality/rules', { params }),
  get: id => client.get(`/quality/rules/${id}`),
  create: data => client.post('/quality/rules', data),
  update: (id, data) => client.put(`/quality/rules/${id}`, data),
  delete: (id, version) => client.delete(`/quality/rules/${id}`, { data: { version } }),
  plans: (id, params) => client.get(`/quality/rules/${id}/plans`, { params }),
  listElementCandidates: params => client.get('/quality/rules/element-candidates', { params })
}

// 执行记录
export const executionAPI = {
  get: (id) => client.get(`/quality/executions/${id}`)
}

// 问题工单
export const issueAPI = {
  list: (params) => client.get('/quality/issues', { params }),
  get: (id) => client.get(`/quality/issues/${id}`),
  updateStatus: (id, version, status, note) => client.put(`/quality/issues/${id}/status`, { version, status, note })
}
