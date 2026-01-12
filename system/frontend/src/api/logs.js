import client from './client'

export const logsAPI = {
  // 支持对象参数或传统参数
  list: (paramsOrPage = 1, pageSize = 20, userId = null) => {
    let params

    // 如果第一个参数是对象，直接使用
    if (typeof paramsOrPage === 'object') {
      params = paramsOrPage
    } else {
      // 否则使用传统的参数方式
      params = { page: paramsOrPage, page_size: pageSize }
      if (userId) params.user_id = userId
    }

    return client.get('/system/logs', { params })
  },

  getById: (id) => {
    return client.get(`/system/logs/${id}`)
  },

  // 统计数据（新增）
  getStats: () => {
    return client.get('/system/logs/stats')
  },

  // 时间趋势（新增）
  getTrends: () => {
    return client.get('/system/logs/trends')
  }
}