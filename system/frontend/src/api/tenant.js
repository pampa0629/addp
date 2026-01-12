import client from './client'

export const tenantAPI = {
  // 获取租户列表
  list(params) {
    return client.get('/system/tenants', { params })
  },

  // 获取单个租户
  getById(id) {
    return client.get(`/system/tenants/${id}`)
  },

  // 创建租户
  create(data) {
    return client.post('/system/tenants', data)
  },

  // 更新租户
  update(id, data) {
    return client.put(`/system/tenants/${id}`, data)
  },

  // 删除租户
  delete(id) {
    return client.delete(`/system/tenants/${id}`)
  }
}
