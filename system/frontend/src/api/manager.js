import client from './client'

// 环境感知的 Manager 服务 URL
// 开发环境: 直接访问 Manager backend (localhost:8081)
// 生产环境: 通过 Gateway 访问 (/api)
const MANAGER_BASE_URL = import.meta.env.PROD ? '' : 'http://localhost:8081'

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