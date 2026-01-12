import client from './client'

export const localEnginesAPI = {
  list: (resourceType = null) => {
    const params = {}
    if (resourceType) {
      params.resource_type = resourceType
    }
    return client.get('/transfer/local-engines', { params })
  },
  get: (id) => client.get(`/transfer/local-engines/${id}`),
  create: (data) => client.post('/transfer/local-engines', data),
  update: (id, data) => client.put(`/transfer/local-engines/${id}`, data),
  delete: (id) => client.delete(`/transfer/local-engines/${id}`),
  testConnection: (data) => client.post('/transfer/local-engines/test-connection', data),
  testExisting: (id) => client.post(`/transfer/local-engines/${id}/test`),
  syncToSystem: (id) => client.post(`/transfer/local-engines/${id}/sync`),

  // 本地元数据扫描
  listTables: (id) => client.get(`/transfer/local-engines/${id}/tables`),
  listFields: (id, table) => client.get(`/transfer/local-engines/${id}/fields`, { params: { table } }),

  // 获取 System 模块的存储引擎（用于任务配置）
  listSystemEngines: (resourceType = null) => {
    const params = {}
    if (resourceType) {
      params.resource_type = resourceType
    }
    return client.get('/transfer/system-engines', { params })
  }
}
