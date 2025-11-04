import client from './client'

export const localResourcesAPI = {
  list: (resourceType = null) => {
    const params = {}
    if (resourceType) {
      params.resource_type = resourceType
    }
    return client.get('/local-resources', { params })
  },
  get: (id) => client.get(`/local-resources/${id}`),
  create: (data) => client.post('/local-resources', data),
  update: (id, data) => client.put(`/local-resources/${id}`, data),
  delete: (id) => client.delete(`/local-resources/${id}`),
  testConnection: (data) => client.post('/local-resources/test-connection', data),
  testExisting: (id) => client.post(`/local-resources/${id}/test`),
  syncToSystem: (id) => client.post(`/local-resources/${id}/sync`),

  // 本地元数据扫描
  listTables: (id) => client.get(`/local-resources/${id}/tables`),
  listFields: (id, table) => client.get(`/local-resources/${id}/fields`, { params: { table } }),

  // 获取 System 模块的存储引擎（用于任务配置）
  listSystemResources: (resourceType = null) => {
    const params = {}
    if (resourceType) {
      params.resource_type = resourceType
    }
    return client.get('/system-resources', { params })
  }
}
