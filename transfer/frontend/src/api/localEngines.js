import client from './client'

export const localEnginesAPI = {
  list: (resourceType = null) => {
    const params = {}
    if (resourceType) {
      params.resource_type = resourceType
    }
    return client.get('/local-engines', { params })
  },
  get: (id) => client.get(`/local-engines/${id}`),
  create: (data) => client.post('/local-engines', data),
  update: (id, data) => client.put(`/local-engines/${id}`, data),
  delete: (id) => client.delete(`/local-engines/${id}`),
  testConnection: (data) => client.post('/local-engines/test-connection', data),
  testExisting: (id) => client.post(`/local-engines/${id}/test`),
  syncToSystem: (id) => client.post(`/local-engines/${id}/sync`),

  // 本地元数据扫描
  listTables: (id) => client.get(`/local-engines/${id}/tables`),
  listFields: (id, table) => client.get(`/local-engines/${id}/fields`, { params: { table } }),

  // 获取 System 模块的存储引擎（用于任务配置）
  listSystemEngines: (resourceType = null) => {
    const params = {}
    if (resourceType) {
      params.resource_type = resourceType
    }
    return client.get('/system-engines', { params })
  }
}
