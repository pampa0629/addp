import client from './client'

export const tenantAPI = {
  // 获取租户列表
  list(params) {
    return client.get('/tenants', { params })
  },

  // 获取单个租户
  getById(id) {
    return client.get(`/tenants/${id}`)
  },

  // 创建租户
  create(data) {
    return client.post('/tenants', data)
  },

  // 更新租户
  update(id, data) {
    return client.put(`/tenants/${id}`, data)
  },

  // 删除租户
  delete(id) {
    return client.delete(`/tenants/${id}`)
  }
}
