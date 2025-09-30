import client from './client'

const MANAGER_BASE_URL = 'http://localhost:8081'

export const managerAPI = {
  // 从 System 同步存储引擎到 Manager
  syncDataSources: () => {
    return client.post(`${MANAGER_BASE_URL}/api/datasources/sync`)
  },

  // 获取数据源列表
  getDataSources: (page = 1, pageSize = 10) => {
    return client.get(`${MANAGER_BASE_URL}/api/datasources`, {
      params: { page, page_size: pageSize }
    })
  },

  // 获取单个数据源
  getDataSourceById: (id) => {
    return client.get(`${MANAGER_BASE_URL}/api/datasources/${id}`)
  },

  // 删除数据源
  deleteDataSource: (id) => {
    return client.delete(`${MANAGER_BASE_URL}/api/datasources/${id}`)
  }
}